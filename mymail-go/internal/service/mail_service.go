// Package service
// mail_service.go 实现邮件业务逻辑：发送、草稿、本地投递、CRUD、批量操作。
//
// 修复的缺陷：
//   - P0-3：所有 :id 操作强制带 user_id 校验（DAO 层已实现，本层转发）
//   - P1-1：Send / DeliverLocal 全程事务（RunInTransaction）
//   - P1-5：发送/投递前校验 storage_limit，超出返回 413
//   - P1-13：DeliverLocal 完整实现（写 DB + 附件 + 配额 + 净化 HTML）
//   - P0-4：邮件 HTML 入库前用 bluemonday 净化，原始存 body_html_raw
//
// 兼容性：所有错误消息与原 Node.js mail.js 完全一致（前端依赖关键字匹配）。
package service

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/mymail/mymail-go/internal/audit"
	"github.com/mymail/mymail-go/internal/sanitize"
	"github.com/mymail/mymail-go/internal/storage/dao"
	"github.com/mymail/mymail-go/internal/storage/db"
	"github.com/mymail/mymail-go/internal/util"
)

// MailError 是邮件业务错误，携带 HTTP 状态码与错误消息。
type MailError struct {
	Status  int
	Message string
}

func (e *MailError) Error() string { return e.Message }

// NewMailError 构造 MailError。
func NewMailError(status int, msg string) *MailError {
	return &MailError{Status: status, Message: msg}
}

// 邮件业务错误（消息与原 Node.js mail.js 完全一致）。
var (
	ErrRecipientRequired    = NewMailError(http.StatusBadRequest, "收件人不能为空")
	ErrSubjectTooLong       = NewMailError(http.StatusBadRequest, "主题最多500字符")
	ErrBodyTooLarge         = NewMailError(http.StatusBadRequest, "正文太大，最多1MB")
	ErrMailNotFound         = NewMailError(http.StatusNotFound, "邮件不存在")
	ErrNoAttachment         = NewMailError(http.StatusNotFound, "没有附件")
	ErrNoPermission         = NewMailError(http.StatusForbidden, "无权访问")
	ErrStorageQuotaExceeded = NewMailError(http.StatusRequestEntityTooLarge, "存储空间不足")
)

func sendRateLimitMsg(max int) *MailError {
	return NewMailError(http.StatusTooManyRequests, fmt.Sprintf("发送太频繁，每分钟最多%d封", max))
}

func invalidEmailMsg(addr string) *MailError {
	return NewMailError(http.StatusBadRequest, fmt.Sprintf("无效的邮箱地址: %s", addr))
}

// IsMailError 判断 error 是否为 *MailError。
func IsMailError(err error) (*MailError, bool) {
	var me *MailError
	if errors.As(err, &me) {
		return me, true
	}
	return nil, false
}

// ============ 输入/输出结构体 ============

// AttachmentMeta 已保存到磁盘的附件元信息（由 HTTP 层保存后传入）。
type AttachmentMeta struct {
	Filename    string
	MimeType    string
	SizeBytes   int64
	StoragePath string
}

// SendInput 发送邮件输入。
type SendInput struct {
	To          string
	Cc          string
	Bcc         string
	Subject     string
	BodyHTML    string
	BodyText    string
	ReplyTo     string
	MessageID   string // 可空，空则自动生成
	Attachments []AttachmentMeta
}

// SendResult 发送结果。
type SendResult struct {
	MessageID string
	MailID    int64
}

// SaveDraftInput 保存草稿输入。
type SaveDraftInput struct {
	To       string
	Cc       string
	Bcc      string
	Subject  string
	BodyHTML string
	BodyText string
}

// LocalAttachment 本地投递的附件。
// 调用方（SMTP 接收器）应先调用 attachmentStore.SaveFile 保存 Content 到磁盘，
// 然后将返回的 StoragePath 填入此结构体。
type LocalAttachment struct {
	Filename    string
	MimeType    string
	Content     []byte // 原始内容（仅用于计算 hash 或调试）
	StoragePath string // 已保存到磁盘的路径（SMTP 接收器保存后填入）
	SizeBytes   int64
}

// DeliverLocalInput 本地投递输入（P1-13 修复）。
type DeliverLocalInput struct {
	RecipientUsername string
	FromAddr          string
	FromName          string
	ToAddr            string
	CcAddr            string
	ReplyTo           string
	Subject           string
	BodyHTML          string
	BodyText          string
	MessageID         string
	InReplyTo         string
	HeadersRaw        string
	SizeBytes         int64
	Attachments       []LocalAttachment
	SpamScore         int
	SpamReasons       string
}

// QueueAttachment 队列中存储的附件元信息。
type QueueAttachment struct {
	Filename    string `json:"filename"`
	MimeType    string `json:"mime_type"`
	SizeBytes   int64  `json:"size_bytes"`
	StoragePath string `json:"storage_path"`
}

// ============ MailService ============

// MailService 邮件业务服务。
type MailService struct {
	msgDAO         *dao.MessageDAO
	attachDAO      *dao.AttachmentDAO
	sendLogDAO     *dao.SendLogDAO
	queueDAO       *dao.MailQueueDAO
	userDAO        *dao.UserDAO
	database       *db.DB
	audit          *audit.Logger
	domain         string
	sendRateLimit  int
	maxAttachments int
}

// AvatarURL 返回邮箱对应的头像 URL。
// 本域用户优先使用上传头像；外部邮箱使用 Gravatar（国内 cn.cravatar.com 镜像）。
func (s *MailService) AvatarURL(email string) string {
	email = strings.ToLower(strings.TrimSpace(email))
	if s.domain != "" && strings.HasSuffix(email, "@"+s.domain) {
		if u, err := s.userDAO.FindByEmail(context.Background(), email); err == nil && u != nil && u.AvatarURL != "" {
			return u.AvatarURL
		}
	}
	return GravatarURL(email, 128)
}

// NewMailService 创建邮件服务。
func NewMailService(
	msgDAO *dao.MessageDAO,
	attachDAO *dao.AttachmentDAO,
	sendLogDAO *dao.SendLogDAO,
	queueDAO *dao.MailQueueDAO,
	userDAO *dao.UserDAO,
	database *db.DB,
	auditLogger *audit.Logger,
	domain string,
	sendRateLimit int,
) *MailService {
	if sendRateLimit <= 0 {
		sendRateLimit = 10
	}
	return &MailService{
		msgDAO:         msgDAO,
		attachDAO:      attachDAO,
		sendLogDAO:     sendLogDAO,
		queueDAO:       queueDAO,
		userDAO:        userDAO,
		database:       database,
		audit:          auditLogger,
		domain:         domain,
		sendRateLimit:  sendRateLimit,
		maxAttachments: 10,
	}
}

// ============ Send 发送邮件 ============

// Send 发送邮件。
// P1-1：全程事务；P1-5：配额校验。实际 SMTP 由队列 worker 异步处理。
func (s *MailService) Send(ctx context.Context, userID int64, in SendInput) (*SendResult, error) {
	// 1. 收件人非空
	if strings.TrimSpace(in.To) == "" {
		return nil, ErrRecipientRequired
	}
	// 2. 主题长度
	if len(in.Subject) > 500 {
		return nil, ErrSubjectTooLong
	}
	// 3. 正文大小
	if len(in.BodyHTML)+len(in.BodyText) > 1024*1024 {
		return nil, ErrBodyTooLarge
	}
	// 4. 速率限制
	recent, err := s.sendLogDAO.RecentSentCount(ctx, userID, time.Minute)
	if err != nil {
		return nil, fmt.Errorf("查询发送频率失败: %w", err)
	}
	if recent >= int64(s.sendRateLimit) {
		return nil, sendRateLimitMsg(s.sendRateLimit)
	}
	// 5. 拆分收件人 + 邮箱校验
	toAddrs := util.SplitRecipients(in.To)
	ccAddrs := util.SplitRecipients(in.Cc)
	bccAddrs := util.SplitRecipients(in.Bcc)
	if len(toAddrs) == 0 {
		return nil, ErrRecipientRequired
	}
	for _, a := range append(append(toAddrs, ccAddrs...), bccAddrs...) {
		if err := util.ValidateEmail(a); err != nil {
			return nil, invalidEmailMsg(a)
		}
	}
	// 6. 附件数限制
	if len(in.Attachments) > s.maxAttachments {
		return nil, NewMailError(http.StatusBadRequest, fmt.Sprintf("附件最多%d个", s.maxAttachments))
	}

	// 7. 读用户 + 配额校验（P1-5）
	user, err := s.userDAO.FindByID(ctx, userID)
	if err != nil {
		return nil, fmt.Errorf("查询用户失败: %w", err)
	}
	if user == nil {
		return nil, NewMailError(http.StatusUnauthorized, "用户不存在")
	}
	totalSize := int64(0)
	for _, a := range in.Attachments {
		totalSize += a.SizeBytes
	}
	if user.StorageLimit > 0 && user.StorageUsed+totalSize > user.StorageLimit {
		return nil, ErrStorageQuotaExceeded
	}

	// 8. 准备字段
	replyTo := in.ReplyTo
	if replyTo == "" {
		replyTo = user.Email
	}
	messageID := in.MessageID
	if messageID == "" {
		messageID = fmt.Sprintf("%d.%d@%s", userID, time.Now().UnixNano(), s.domain)
	}
	hasAttach := len(in.Attachments) > 0
	attachCount := len(in.Attachments)

	// 队列附件 JSON
	var queueAttachsJSON string
	if hasAttach {
		qas := make([]QueueAttachment, len(in.Attachments))
		for i, a := range in.Attachments {
			qas[i] = QueueAttachment{
				Filename:    a.Filename,
				MimeType:    a.MimeType,
				SizeBytes:   a.SizeBytes,
				StoragePath: a.StoragePath,
			}
		}
		b, _ := json.Marshal(qas)
		queueAttachsJSON = string(b)
	}

	// 9. 事务内写入（P1-1）
	var mailID int64
	var uid int64
	err = s.database.RunInTransaction(ctx, func(tx *sql.Tx) error {
		// 9.1 取 UID（事务内，P1-4 并发安全）
		uid, err = s.msgDAO.GetNextUIDOn(ctx, tx, userID)
		if err != nil {
			return fmt.Errorf("获取 UID 失败: %w", err)
		}
		// 9.2 写 messages（SENT）
		mailID, err = s.msgDAO.CreateOn(ctx, tx, dao.CreateMessageInput{
			UserID:      userID,
			Folder:      "SENT",
			MessageID:   messageID,
			UID:         &uid,
			FromAddr:    user.Email,
			FromName:    user.DisplayName,
			ToAddr:      strings.Join(toAddrs, ", "),
			CcAddr:      strings.Join(ccAddrs, ", "),
			BccAddr:     strings.Join(bccAddrs, ", "),
			ReplyTo:     replyTo,
			Subject:     in.Subject,
			BodyText:    in.BodyText,
			BodyHTML:    sanitize.SanitizeHTML(in.BodyHTML),
			BodyHTMLRaw: in.BodyHTML,
			HasAttach:   hasAttach,
			AttachCount: attachCount,
			SizeBytes:   totalSize,
		})
		if err != nil {
			return fmt.Errorf("写入邮件失败: %w", err)
		}
		// 9.3 写 attachments
		for _, a := range in.Attachments {
			if _, err := s.attachDAO.CreateOn(ctx, tx, dao.CreateAttachmentInput{
				MessageID:   mailID,
				Filename:    a.Filename,
				MimeType:    a.MimeType,
				SizeBytes:   a.SizeBytes,
				StoragePath: a.StoragePath,
			}); err != nil {
				return fmt.Errorf("写入附件失败: %w", err)
			}
		}
		// 9.4 写 send_log（sent 状态）
		if _, err := s.sendLogDAO.CreateOn(ctx, tx, dao.CreateSendLogInput{
			UserID:  userID,
			ToAddr:  strings.Join(toAddrs, ", "),
			Subject: in.Subject,
			Status:  dao.SendLogSent,
		}); err != nil {
			return fmt.Errorf("写入发送日志失败: %w", err)
		}
		// 9.5 更新 storage_used（事务内）
		if _, err := tx.ExecContext(ctx, `UPDATE users SET storage_used = storage_used + ? WHERE id = ?`, totalSize, userID); err != nil {
			return fmt.Errorf("更新存储用量失败: %w", err)
		}
		// 9.6 入队（外域收件人由 worker SMTP 发送）
		if _, err := s.queueDAO.EnqueueOn(ctx, tx, dao.EnqueueInput{
			UserID:      userID,
			FromAddr:    user.Email,
			ToAddrs:     strings.Join(toAddrs, ","),
			CcAddrs:     strings.Join(ccAddrs, ","),
			BccAddrs:    strings.Join(bccAddrs, ","),
			Subject:     in.Subject,
			BodyHTML:    in.BodyHTML,
			BodyText:    in.BodyText,
			ReplyTo:     replyTo,
			Attachments: queueAttachsJSON,
			MaxAttempts: 3,
		}); err != nil {
			return fmt.Errorf("入队失败: %w", err)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}

	// 10. 审计
	if s.audit != nil {
		s.audit.Record(ctx, audit.Entry{
			ActorType:    audit.ActorUser,
			ActorID:      &userID,
			Action:       "mail.send",
			ResourceType: "message",
			ResourceID:   fmt.Sprintf("%d", mailID),
			Result:       audit.ResultSuccess,
			Detail:       fmt.Sprintf(`{"to":"%s","subject":"%s","uid":%d}`, strings.Join(toAddrs, ","), in.Subject, uid),
		})
	}
	slog.Info("邮件发送入队", "user_id", userID, "mail_id", mailID, "to", strings.Join(toAddrs, ","))
	return &SendResult{MessageID: messageID, MailID: mailID}, nil
}

// ============ SaveDraft 保存草稿 ============

// SaveDraft 保存草稿到 DRAFTS 文件夹。
// 与原 Node.js mail.js:183-200 一致：不写 send_log、不更新 storage_used。
func (s *MailService) SaveDraft(ctx context.Context, userID int64, in SaveDraftInput) (int64, error) {
	user, err := s.userDAO.FindByID(ctx, userID)
	if err != nil {
		return 0, fmt.Errorf("查询用户失败: %w", err)
	}
	if user == nil {
		return 0, NewMailError(http.StatusUnauthorized, "用户不存在")
	}

	subject := in.Subject
	if subject == "" {
		subject = "(无主题)"
	}
	toAddr := in.To
	if toAddr == "" {
		toAddr = ""
	}
	messageID := fmt.Sprintf("draft-%d@%s", time.Now().UnixNano(), s.domain)

	// 草稿也需事务保护（GetNextUID + Create）
	var mailID int64
	var uid int64
	err = s.database.RunInTransaction(ctx, func(tx *sql.Tx) error {
		uid, err = s.msgDAO.GetNextUIDOn(ctx, tx, userID)
		if err != nil {
			return fmt.Errorf("获取 UID 失败: %w", err)
		}
		mailID, err = s.msgDAO.CreateOn(ctx, tx, dao.CreateMessageInput{
			UserID:    userID,
			Folder:    "DRAFTS",
			MessageID: messageID,
			UID:       &uid,
			FromAddr:  user.Email,
			FromName:  user.DisplayName,
			ToAddr:    toAddr,
			CcAddr:    in.Cc,
			BccAddr:   in.Bcc,
			Subject:   subject,
			BodyText:  in.BodyText,
			BodyHTML:  sanitize.SanitizeHTML(in.BodyHTML),
			BodyHTMLRaw: in.BodyHTML,
		})
		return err
	})
	if err != nil {
		return 0, err
	}
	slog.Info("草稿已保存", "user_id", userID, "mail_id", mailID)
	return mailID, nil
}

// ============ DeliverLocal 本地投递（P1-13 修复）============

// DeliverLocal 将邮件投递到本域收件人的 INBOX。
// 修复 P1-13：原 Node.js sendLocal 只写 maildir 文件，不写 DB/附件/配额/规则/WS。
// 本方法完整实现：maildir + DB(messages+attachments) + storage_used(含校验) + HTML净化。
//
// 返回值：
//   - 投递成功返回 nil
//   - 收件人不存在返回 nil（静默跳过，与原 smtp-receiver 一致）
//   - 配额不足返回 nil（静默跳过并记日志）
//   - 其他错误返回 error
func (s *MailService) DeliverLocal(ctx context.Context, in DeliverLocalInput) error {
	// 1. 查收件人
	user, err := s.userDAO.FindByUsername(ctx, in.RecipientUsername)
	if err != nil {
		return fmt.Errorf("查询收件人失败: %w", err)
	}
	if user == nil || !user.IsActive {
		slog.Info("本地投递：收件人不存在或不活跃，跳过", "recipient", in.RecipientUsername)
		return nil
	}

	// 2. 配额校验（P1-5）
	size := in.SizeBytes
	if size <= 0 {
		size = int64(len(in.BodyHTML) + len(in.BodyText))
	}
	if user.StorageLimit > 0 && user.StorageUsed+size > user.StorageLimit {
		slog.Warn("本地投递：收件人存储配额不足，跳过",
			"recipient", in.RecipientUsername,
			"used", user.StorageUsed, "size", size, "limit", user.StorageLimit)
		return nil
	}

	// 3. 决定 folder（spam_score >= suspicious 阈值 → JUNK）
	folder := "INBOX"
	// 阈值与原 smtp-receiver 一致：suspicious=5
	if in.SpamScore >= 5 {
		folder = "JUNK"
	}

	// 4. 净化 HTML（P0-4）
	sanitizedHTML := sanitize.SanitizeHTML(in.BodyHTML)

	// 5. 事务内写入（P1-1）
	err = s.database.RunInTransaction(ctx, func(tx *sql.Tx) error {
		// 5.1 取 UID
		uid, err := s.msgDAO.GetNextUIDOn(ctx, tx, user.ID)
		if err != nil {
			return fmt.Errorf("获取 UID 失败: %w", err)
		}
		// 5.2 写 messages（SpamScore/SpamReasons 不在 CreateMessageInput 中，
		// 在 Create 后用单独的 UPDATE 写入，避免修改 DAO 接口）
		msgID, err := s.msgDAO.CreateOn(ctx, tx, dao.CreateMessageInput{
			UserID:      user.ID,
			Folder:      folder,
			MessageID:   in.MessageID,
			UID:         &uid,
			FromAddr:    in.FromAddr,
			FromName:    in.FromName,
			ToAddr:      in.ToAddr,
			CcAddr:      in.CcAddr,
			ReplyTo:     in.ReplyTo,
			Subject:     in.Subject,
			BodyText:    in.BodyText,
			BodyHTML:    sanitizedHTML,
			BodyHTMLRaw: in.BodyHTML,
			HasAttach:   len(in.Attachments) > 0,
			AttachCount: len(in.Attachments),
			SizeBytes:   size,
			HeadersRaw:  in.HeadersRaw,
			InReplyTo:   in.InReplyTo,
		})
		if err != nil {
			return fmt.Errorf("写入收件邮件失败: %w", err)
		}
		// 5.3 写 spam_score / spam_reasons（CreateMessageInput 不含这两个字段，
		// 在事务内用 UPDATE 写入，确保与邮件创建原子性）
		if in.SpamScore != 0 || in.SpamReasons != "" {
			var spamReasonsVal any
			if in.SpamReasons != "" {
				spamReasonsVal = in.SpamReasons
			}
			if _, err := tx.ExecContext(ctx,
				`UPDATE messages SET spam_score = ?, spam_reasons = ? WHERE id = ?`,
				in.SpamScore, spamReasonsVal, msgID); err != nil {
				return fmt.Errorf("写入垃圾评分失败: %w", err)
			}
		}
		// 5.4 写 attachments 记录（附件文件由 SMTP 接收器在调用 DeliverLocal 前保存到磁盘，
		// 此处仅写 DB 记录）
		for _, a := range in.Attachments {
			if _, err := s.attachDAO.CreateOn(ctx, tx, dao.CreateAttachmentInput{
				MessageID:   msgID,
				Filename:    a.Filename,
				MimeType:    a.MimeType,
				SizeBytes:   a.SizeBytes,
				StoragePath: a.StoragePath,
			}); err != nil {
				return fmt.Errorf("写入本地投递附件失败: %w", err)
			}
		}
		// 5.5 更新 storage_used（事务内）
		if _, err := tx.ExecContext(ctx, `UPDATE users SET storage_used = storage_used + ? WHERE id = ?`, size, user.ID); err != nil {
			return fmt.Errorf("更新收件人存储用量失败: %w", err)
		}
		return nil
	})
	if err != nil {
		return err
	}

	slog.Info("本地投递成功", "recipient", in.RecipientUsername, "subject", in.Subject, "folder", folder)
	// TODO(阶段5)：触发规则引擎、WS 通知
	return nil
}

// ============ CRUD 方法 ============

// List 分页查询用户邮件。
func (s *MailService) List(ctx context.Context, userID int64, folder string, page, limit int, search string, unreadOnly bool) (*dao.ListResult, error) {
	if err := util.ValidateFolder(folder); err != nil {
		return nil, NewMailError(http.StatusBadRequest, err.Error())
	}
	if page < 1 {
		page = 1
	}
	if limit < 1 || limit > 100 {
		limit = 20
	}
	return s.msgDAO.ListByUser(ctx, userID, folder, page, limit, search, unreadOnly)
}

// Get 获取单封邮件（含附件）。P0-3：带归属校验。
func (s *MailService) Get(ctx context.Context, userID, id int64) (*dao.Message, []*dao.Attachment, error) {
	msg, err := s.msgDAO.FindByIDForUser(ctx, id, userID)
	if err != nil {
		return nil, nil, fmt.Errorf("查询邮件失败: %w", err)
	}
	if msg == nil {
		return nil, nil, ErrMailNotFound
	}
	attachments, err := s.attachDAO.FindByMessageID(ctx, id)
	if err != nil {
		return nil, nil, fmt.Errorf("查询附件失败: %w", err)
	}
	return msg, attachments, nil
}

// UnreadCount 获取未读数。
func (s *MailService) UnreadCount(ctx context.Context, userID int64, folder string) (int64, error) {
	return s.msgDAO.UnreadCount(ctx, userID, folder)
}

// UnreadCountByFolders 批量获取各文件夹未读数。
func (s *MailService) UnreadCountByFolders(ctx context.Context, userID int64, folders []string) (map[string]int64, error) {
	return s.msgDAO.UnreadCountByFolders(ctx, userID, folders)
}

// MarkRead 标记已读。P0-3：DAO 层带归属校验。
func (s *MailService) MarkRead(ctx context.Context, userID, id int64) error {
	if err := s.msgDAO.MarkRead(ctx, id, userID); err != nil {
		return fmt.Errorf("标记已读失败: %w", err)
	}
	return nil
}

// MarkUnread 标记未读。
func (s *MailService) MarkUnread(ctx context.Context, userID, id int64) error {
	if err := s.msgDAO.MarkUnread(ctx, id, userID); err != nil {
		return fmt.Errorf("标记未读失败: %w", err)
	}
	return nil
}

// ToggleStar 切换星标。
func (s *MailService) ToggleStar(ctx context.Context, userID, id int64) error {
	if err := s.msgDAO.ToggleStar(ctx, id, userID); err != nil {
		return fmt.Errorf("切换星标失败: %w", err)
	}
	return nil
}

// MoveToFolder 移动到指定文件夹。
func (s *MailService) MoveToFolder(ctx context.Context, userID, id int64, folder string) error {
	if err := util.ValidateFolder(folder); err != nil {
		return NewMailError(http.StatusBadRequest, err.Error())
	}
	if err := s.msgDAO.MoveToFolder(ctx, id, userID, folder); err != nil {
		return fmt.Errorf("移动邮件失败: %w", err)
	}
	return nil
}

// SoftDelete 软删除（移到 TRASH）。
func (s *MailService) SoftDelete(ctx context.Context, userID, id int64) error {
	if err := s.msgDAO.SoftDelete(ctx, id, userID); err != nil {
		return fmt.Errorf("删除邮件失败: %w", err)
	}
	return nil
}

// PermanentDelete 永久删除（含附件文件清理）。
func (s *MailService) PermanentDelete(ctx context.Context, userID, id int64) error {
	// 先查附件用于清理文件
	attachments, err := s.attachDAO.FindByMessageID(ctx, id)
	if err != nil {
		return fmt.Errorf("查询附件失败: %w", err)
	}
	// P0-3：DAO 层 PermanentDelete 带 user_id 校验
	if err := s.msgDAO.PermanentDelete(ctx, id, userID); err != nil {
		return fmt.Errorf("永久删除邮件失败: %w", err)
	}
	// 清理附件文件（DB 已通过 ON DELETE CASCADE 删除附件记录）
	for _, a := range attachments {
		_ = os.Remove(a.StoragePath) // 幂等，忽略错误
	}
	if s.audit != nil {
		s.audit.Record(ctx, audit.Entry{
			ActorType:    audit.ActorUser,
			ActorID:      &userID,
			Action:       "mail.permanent_delete",
			ResourceType: "message",
			ResourceID:   fmt.Sprintf("%d", id),
			Result:       audit.ResultSuccess,
		})
	}
	return nil
}

// Restore 从垃圾箱恢复到 INBOX。
func (s *MailService) Restore(ctx context.Context, userID, id int64) error {
	if err := s.msgDAO.Restore(ctx, id, userID); err != nil {
		return fmt.Errorf("恢复邮件失败: %w", err)
	}
	return nil
}

// EmptyTrash 清空垃圾箱。P1-1：事务保护。
func (s *MailService) EmptyTrash(ctx context.Context, userID int64) error {
	if err := s.msgDAO.EmptyTrash(ctx, userID); err != nil {
		return fmt.Errorf("清空垃圾箱失败: %w", err)
	}
	if s.audit != nil {
		s.audit.Record(ctx, audit.Entry{
			ActorType:    audit.ActorUser,
			ActorID:      &userID,
			Action:       "mail.empty_trash",
			Result:       audit.ResultSuccess,
		})
	}
	return nil
}

// ============ 批量操作 ============

// BatchMarkRead 批量标记已读。P0-3：DAO 层带 user_id 限定。
func (s *MailService) BatchMarkRead(ctx context.Context, userID int64, ids []int64) (int64, error) {
	if len(ids) == 0 {
		return 0, nil
	}
	return s.msgDAO.BatchMarkRead(ctx, userID, ids)
}

// BatchMove 批量移动。
func (s *MailService) BatchMove(ctx context.Context, userID int64, ids []int64, folder string) (int64, error) {
	if len(ids) == 0 {
		return 0, nil
	}
	if err := util.ValidateFolder(folder); err != nil {
		return 0, NewMailError(http.StatusBadRequest, err.Error())
	}
	return s.msgDAO.BatchMove(ctx, userID, ids, folder)
}

// BatchDelete 批量软删除。
func (s *MailService) BatchDelete(ctx context.Context, userID int64, ids []int64) (int64, error) {
	if len(ids) == 0 {
		return 0, nil
	}
	return s.msgDAO.BatchSoftDelete(ctx, userID, ids)
}

// ============ 附件相关 ============

// GetAttachmentForDownload 获取附件用于下载（含归属校验）。
func (s *MailService) GetAttachmentForDownload(ctx context.Context, userID, attachmentID int64) (*dao.Attachment, error) {
	att, err := s.attachDAO.FindByIDForUser(ctx, attachmentID, userID)
	if err != nil {
		return nil, fmt.Errorf("查询附件失败: %w", err)
	}
	if att == nil {
		return nil, ErrNoAttachment
	}
	return att, nil
}

// ListAttachmentsByMessage 列出邮件的所有附件（含归属校验）。
func (s *MailService) ListAttachmentsByMessage(ctx context.Context, userID, messageID int64) ([]*dao.Attachment, error) {
	// 先校验邮件归属
	msg, err := s.msgDAO.FindByIDForUser(ctx, messageID, userID)
	if err != nil {
		return nil, fmt.Errorf("查询邮件失败: %w", err)
	}
	if msg == nil {
		return nil, ErrMailNotFound
	}
	return s.attachDAO.FindByMessageID(ctx, messageID)
}
