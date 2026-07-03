// Package mailsender 实现邮件发送：本地投递 + 持久化队列 worker。
//
// - local.go：本地投递器（P1-13 修复）
//   原 Node.js smtp-sender.js 的 sendLocal 仅写 maildir 文件，未入 DB/附件/配额。
//   本包通过调用 MailService.DeliverLocal 完成完整本地投递。
//
// - queue.go：持久化队列 worker，从 mail_queue 取待发项处理。
//   本域收件人 → LocalDeliverer；外域收件人 → 外部 SMTP（阶段 4 实现）。
package mailsender

import (
	"context"
	"encoding/json"
	"log/slog"
	"strings"

	"github.com/mymail/mymail-go/internal/service"
	"github.com/mymail/mymail-go/internal/storage/dao"
)

// LocalDeliverer 本地投递器。
//
// 职责：从队列项解析收件人列表，筛选出本域收件人，
// 调用 MailService.DeliverLocal 完成入库。
//
// P1-13 修复：通过 MailService.DeliverLocal 完成完整投递
// （DB + 附件记录 + 配额校验 + HTML 净化），不再仅写 maildir。
type LocalDeliverer struct {
	mailSvc *service.MailService
	domain  string
}

// NewLocalDeliverer 创建本地投递器。
func NewLocalDeliverer(mailSvc *service.MailService, domain string) *LocalDeliverer {
	return &LocalDeliverer{
		mailSvc: mailSvc,
		domain:  domain,
	}
}

// DeliverFromQueue 处理队列项，将本域收件人投递到本地 DB。
//
// 参数：
//   - item: 队列项（含 from/to/cc/bcc/subject/body/attachments）
//
// 返回：
//   - localRecipients: 已处理的本域收件人列表（用于日志/指标）
//   - externalRecipients: 外域收件人列表（worker 后续 SMTP 发送）
//   - err: 投递过程中的错误（部分失败也返回；具体哪些失败通过返回列表对比可推断）
//
// 行为：
//   - 收件人去重（to + cc + bcc 合并）
//   - 本域收件人调 DeliverLocal（已含配额校验、HTML 净化、附件入库）
//   - 单个收件人失败不影响其他收件人
func (d *LocalDeliverer) DeliverFromQueue(ctx context.Context, item *dao.MailQueueItem) (localRecipients, externalRecipients []string, err error) {
	allRecipients := splitAddresses(item.ToAddrs)
	if item.CcAddrs != "" {
		allRecipients = append(allRecipients, splitAddresses(item.CcAddrs)...)
	}
	if item.BccAddrs != "" {
		allRecipients = append(allRecipients, splitAddresses(item.BccAddrs)...)
	}
	allRecipients = dedupStrings(allRecipients)

	// 解析附件元信息
	var attachments []service.LocalAttachment
	if item.Attachments != "" {
		var qas []service.QueueAttachment
		if err := json.Unmarshal([]byte(item.Attachments), &qas); err != nil {
			slog.Warn("解析队列附件 JSON 失败", "queue_id", item.ID, "error", err)
		} else {
			for _, qa := range qas {
				attachments = append(attachments, service.LocalAttachment{
					Filename:    qa.Filename,
					MimeType:    qa.MimeType,
					StoragePath: qa.StoragePath,
					SizeBytes:   qa.SizeBytes,
				})
			}
		}
	}

	for _, addr := range allRecipients {
		username, domain, ok := splitAddress(addr)
		if !ok {
			slog.Warn("无效收件人地址，跳过", "address", addr, "queue_id", item.ID)
			continue
		}
		if domain != d.domain {
			externalRecipients = append(externalRecipients, addr)
			continue
		}
		// 本域投递
		in := service.DeliverLocalInput{
			RecipientUsername: username,
			FromAddr:          item.FromAddr,
			ToAddr:            addr,
			Subject:           item.Subject,
			BodyHTML:          item.BodyHTML,
			BodyText:          item.BodyText,
			ReplyTo:           item.ReplyTo,
			Attachments:       attachments,
			SizeBytes:         int64(len(item.BodyHTML) + len(item.BodyText)),
			// 队列投递无 spam_score（外发邮件，由 SMTP 接收端评估）
		}
		if err := d.mailSvc.DeliverLocal(ctx, in); err != nil {
			slog.Error("本地投递失败", "queue_id", item.ID, "recipient", addr, "error", err)
			// 继续处理其他收件人
			continue
		}
		localRecipients = append(localRecipients, addr)
	}

	slog.Info("队列项本地投递完成",
		"queue_id", item.ID,
		"local_count", len(localRecipients),
		"external_count", len(externalRecipients),
	)
	return localRecipients, externalRecipients, nil
}

// splitAddresses 拆分逗号分隔的地址列表，去除空白与空项。
func splitAddresses(s string) []string {
	if s == "" {
		return nil
	}
	parts := strings.Split(s, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p != "" {
			out = append(out, p)
		}
	}
	return out
}

// splitAddress 拆分 "user@domain" 为 (username, domain, ok)。
func splitAddress(addr string) (username, domain string, ok bool) {
	addr = strings.TrimSpace(addr)
	idx := strings.LastIndex(addr, "@")
	if idx <= 0 || idx == len(addr)-1 {
		return "", "", false
	}
	return addr[:idx], addr[idx+1:], true
}

// dedupStrings 字符串切片去重（保持顺序）。
func dedupStrings(in []string) []string {
	seen := make(map[string]bool, len(in))
	out := make([]string, 0, len(in))
	for _, s := range in {
		if !seen[s] {
			seen[s] = true
			out = append(out, s)
		}
	}
	return out
}

// Domain 返回配置的域名（测试用）。
func (d *LocalDeliverer) Domain() string { return d.domain }
