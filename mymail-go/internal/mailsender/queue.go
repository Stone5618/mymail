// queue.go 实现持久化发送队列 worker。
//
// 原理：
//   - 用户通过 HTTP /api/mail/send 发送邮件时，MailService.Send 将邮件入队（status=pending）
//   - Worker 后台轮询 ClaimNextPending，领取 → 处理 → 标记成功/失败
//   - 本域收件人：LocalDeliverer 完成本地投递
//   - 外域收件人：SMTPSender 通过外部 SMTP 服务器发送（含熔断器 + 指数退避重试）
//   - 失败自动重试（retry_delay_min 分钟后重试），超过 max_attempts 标记为 failed
//   - 崩溃恢复：启动时 RequeueStale 将卡死的 sending 项重置为 pending
//
// 修复的缺陷：
//   - 原 Node.js 无队列，sendMail 失败即丢；本队列提供持久化重试能力。
package mailsender

import (
	"context"
	"encoding/json"
	"log/slog"
	"sync"
	"time"

	"github.com/mymail/mymail-go/internal/service"
	"github.com/mymail/mymail-go/internal/storage/dao"
)

// Sender 邮件发送器接口（依赖倒置，便于 mock 测试）。
// *SMTPSender 自动满足此接口。
type Sender interface {
	Send(ctx context.Context, in SendInput) error
}

// QueueWorker 持久化发送队列 worker。
type QueueWorker struct {
	local      *LocalDeliverer
	queueDAO   *dao.MailQueueDAO
	smtpSender Sender // 外部 SMTP 发送器（处理外域收件人；nil 时跳过外域发送）
	pollIntvl  time.Duration
	retryMin   int // 重试间隔（分钟）

	ctx    context.Context
	cancel context.CancelFunc
	wg     sync.WaitGroup
}

// NewQueueWorker 创建队列 worker。
//
// 参数：
//   - local: 本地投递器（处理本域收件人）
//   - queueDAO: 队列 DAO
//   - pollInterval: 轮询间隔（默认 5 秒）
//   - retryDelayMin: 重试间隔分钟数（默认 5）
//   - smtpSender: 外部 SMTP 发送器（处理外域收件人；可为 nil 表示跳过外域发送）
func NewQueueWorker(local *LocalDeliverer, queueDAO *dao.MailQueueDAO, pollInterval time.Duration, retryDelayMin int, smtpSender Sender) *QueueWorker {
	if pollInterval <= 0 {
		pollInterval = 5 * time.Second
	}
	if retryDelayMin <= 0 {
		retryDelayMin = 5
	}
	ctx, cancel := context.WithCancel(context.Background())
	return &QueueWorker{
		local:      local,
		queueDAO:   queueDAO,
		smtpSender: smtpSender,
		pollIntvl:  pollInterval,
		retryMin:   retryDelayMin,
		ctx:        ctx,
		cancel:     cancel,
	}
}

// Start 启动 worker（非阻塞，在后台 goroutine 中运行）。
func (w *QueueWorker) Start() {
	// 1. 崩溃恢复：重置卡死的 sending 项
	w.recoverStale()

	// 2. 启动轮询
	w.wg.Add(1)
	go w.run()
	slog.Info("队列 worker 已启动", "poll_interval", w.pollIntvl, "retry_delay_min", w.retryMin)
}

// Stop 停止 worker。
func (w *QueueWorker) Stop() {
	slog.Info("队列 worker 停止中...")
	w.cancel()
	w.wg.Wait()
	slog.Info("队列 worker 已停止")
}

// run 是 worker 主循环。
func (w *QueueWorker) run() {
	defer w.wg.Done()
	ticker := time.NewTicker(w.pollIntvl)
	defer ticker.Stop()

	for {
		select {
		case <-w.ctx.Done():
			return
		case <-ticker.C:
			w.processNext()
		}
	}
}

// processNext 尝试领取并处理下一个队列项。
func (w *QueueWorker) processNext() {
	item, err := w.queueDAO.ClaimNextPending(w.ctx)
	if err != nil {
		slog.Error("领取队列项失败", "error", err)
		return
	}
	if item == nil {
		return // 无待处理项
	}
	w.handleItem(item)
}

// handleItem 处理单个队列项。
//
// 流程：
//  1. 本域投递（LocalDeliverer）—— 返回 localRecipients + externalRecipients
//  2. 外域发送（SMTPSender）—— 若有外域收件人且 smtpSender 非 nil
//  3. 全部成功后标记 sent；任一失败标记 failed（含重试计数）
func (w *QueueWorker) handleItem(item *dao.MailQueueItem) {
	ctx, cancel := context.WithTimeout(w.ctx, 30*time.Second)
	defer cancel()

	// 1. 本域投递
	_, externalRecipients, err := w.local.DeliverFromQueue(ctx, item)
	if err != nil {
		slog.Error("队列项本地投递失败", "queue_id", item.ID, "error", err)
		if markErr := w.queueDAO.MarkFailed(ctx, item.ID, err.Error(), w.retryMin); markErr != nil {
			slog.Error("标记队列项失败状态出错", "queue_id", item.ID, "error", markErr)
		}
		return
	}

	// 2. 外域发送（阶段 4）
	if len(externalRecipients) > 0 {
		if w.smtpSender == nil {
			slog.Warn("存在外域收件人但 SMTPSender 未配置，跳过外域发送",
				"queue_id", item.ID,
				"external_count", len(externalRecipients),
			)
		} else {
			if sendErr := w.smtpSender.Send(ctx, SendInput{
				From:        item.FromAddr,
				To:          externalRecipients,
				Cc:          splitAddresses(item.CcAddrs),
				Bcc:         splitAddresses(item.BccAddrs),
				Subject:     item.Subject,
				BodyHTML:    item.BodyHTML,
				BodyText:    item.BodyText,
				ReplyTo:     item.ReplyTo,
				Attachments: parseQueueAttachments(item),
			}); sendErr != nil {
				slog.Error("外域 SMTP 发送失败",
					"queue_id", item.ID,
					"external_count", len(externalRecipients),
					"error", sendErr,
				)
				if markErr := w.queueDAO.MarkFailed(ctx, item.ID, sendErr.Error(), w.retryMin); markErr != nil {
					slog.Error("标记队列项失败状态出错", "queue_id", item.ID, "error", markErr)
				}
				return
			}
			slog.Info("外域 SMTP 发送成功",
				"queue_id", item.ID,
				"external_count", len(externalRecipients),
			)
		}
	}

	// 3. 全部成功：标记 sent
	if markErr := w.queueDAO.MarkSent(ctx, item.ID); markErr != nil {
		slog.Error("标记队列项成功状态出错", "queue_id", item.ID, "error", markErr)
	}
}

// parseQueueAttachments 解析队列项的附件 JSON，转换为外发附件列表。
// 解析失败时返回 nil（不阻断发送，仅丢失附件并记录日志）。
func parseQueueAttachments(item *dao.MailQueueItem) []SendAttachment {
	if item.Attachments == "" {
		return nil
	}
	var qas []service.QueueAttachment
	if err := json.Unmarshal([]byte(item.Attachments), &qas); err != nil {
		slog.Warn("解析队列附件 JSON 失败", "queue_id", item.ID, "error", err)
		return nil
	}
	out := make([]SendAttachment, 0, len(qas))
	for _, qa := range qas {
		out = append(out, SendAttachment{
			Filename:    qa.Filename,
			MimeType:    qa.MimeType,
			StoragePath: qa.StoragePath,
		})
	}
	return out
}

// recoverStale 崩溃恢复：重置卡死的 sending 项。
func (w *QueueWorker) recoverStale() {
	n, err := w.queueDAO.RequeueStale(w.ctx, 10) // 10 分钟前仍 sending 的视为卡死
	if err != nil {
		slog.Error("崩溃恢复重置卡死队列项失败", "error", err)
		return
	}
	if n > 0 {
		slog.Info("崩溃恢复：重置卡死队列项", "count", n)
	}
}

// ProcessOnce 立即处理一个队列项（用于测试，不需要等待 ticker）。
// 返回 true 表示处理了一项，false 表示队列空。
func (w *QueueWorker) ProcessOnce() bool {
	item, err := w.queueDAO.ClaimNextPending(w.ctx)
	if err != nil || item == nil {
		return false
	}
	w.handleItem(item)
	return true
}
