// Package dto
// mail.go 定义邮件相关 HTTP 请求/响应 DTO。
//
// 字段命名约定（与原 Node.js 后端 100% 兼容）：
//   - 请求 body：camelCase（前端 ComposeView.vue 用 bodyHtml/bodyText/replyTo）
//   - 响应 JSON：messages 表字段 snake_case（前端 MailView.vue 用 mail.is_read/mail.from_addr）
//   - 响应包装字段：camelCase（unreadCount/messageId）
package dto

// ============ 请求 DTO ============

// SaveDraftRequest 保存草稿请求（JSON body）。
// 字段为 camelCase，与前端一致。
type SaveDraftRequest struct {
	To       string `json:"to"`
	Cc       string `json:"cc"`
	Bcc      string `json:"bcc"`
	Subject  string `json:"subject"`
	BodyHTML string `json:"bodyHtml"`
	BodyText string `json:"bodyText"`
}

// BatchOperationRequest 批量操作请求（JSON body）。
// 用于 batch/mark-read、batch/move、batch/delete。
// IDs 为邮件 ID 列表；Folder 仅 batch/move 用。
type BatchOperationRequest struct {
	IDs    []int64 `json:"ids" binding:"required"`
	Folder string  `json:"folder,omitempty"`
}

// ============ 响应 DTO ============

// MailDetailResponse 邮件详情响应（snake_case，与原 Node.js SELECT * 一致）。
// 前端 MailDetailView.vue 直接消费这些字段名。
// 命名为 MailDetailResponse 避免与 dto/auth.go 的通用 MessageResponse 冲突。
type MailDetailResponse struct {
	ID            int64                 `json:"id"`
	UserID        int64                 `json:"user_id"`
	Folder        string                `json:"folder"`
	MessageID     string                `json:"message_id"`
	UID           *int64                `json:"uid"`
	FromAddr      string                `json:"from_addr"`
	FromName      string                `json:"from_name"`
	FromAvatarURL string                `json:"from_avatar_url"`
	ToAddr        string                `json:"to_addr"`
	CcAddr      string                `json:"cc_addr"`
	BccAddr     string                `json:"bcc_addr"`
	ReplyTo     string                `json:"reply_to"`
	Subject     string                `json:"subject"`
	BodyText    string                `json:"body_text"`
	BodyHTML    string                `json:"body_html"`
	IsRead      bool                  `json:"is_read"`
	IsStarred   bool                  `json:"is_starred"`
	IsDeleted   bool                  `json:"is_deleted"`
	HasAttach   bool                  `json:"has_attach"`
	AttachCount int                   `json:"attach_count"`
	SizeBytes   int64                 `json:"size_bytes"`
	InReplyTo   string                `json:"in_reply_to,omitempty"`
	Flags       string                `json:"flags"`
	SpamScore   int                   `json:"spam_score"`
	SpamReasons string                `json:"spam_reasons,omitempty"`
	ReceivedAt  string                `json:"received_at"`
	Attachments []*AttachmentResponse `json:"attachments,omitempty"`
}

// AttachmentResponse 附件响应（snake_case）。
// 注意：storage_path 不暴露给前端（安全考虑）。
type AttachmentResponse struct {
	ID        int64  `json:"id"`
	MessageID int64  `json:"message_id"`
	Filename  string `json:"filename"`
	MimeType  string `json:"mime_type"`
	SizeBytes int64  `json:"size_bytes"`
	CreatedAt string `json:"created_at"`
}

// ListResponse 邮件列表响应。
// 包装字段 unreadCount 为 camelCase（与原 Node.js 一致）。
type ListResponse struct {
	Messages    []*MailDetailResponse `json:"messages"`
	Total       int64                 `json:"total"`
	Page        int                   `json:"page"`
	Limit       int                   `json:"limit"`
	UnreadCount int64                 `json:"unreadCount"`
}

// UnreadCountResponse 各文件夹未读数响应。
type UnreadCountResponse struct {
	INBOX  int64 `json:"INBOX"`
	SENT   int64 `json:"SENT"`
	DRAFTS int64 `json:"DRAFTS"`
	TRASH  int64 `json:"TRASH"`
}

// SendResponse 发送邮件响应。
type SendResponse struct {
	Message   string `json:"message"`
	MessageID string `json:"messageId"`
}

// SaveDraftResponse 保存草稿响应。
type SaveDraftResponse struct {
	Message string `json:"message"`
	ID      int64  `json:"id"`
}

// BatchOperationResponse 批量操作响应。
type BatchOperationResponse struct {
	Message  string `json:"message"`
	Affected int64  `json:"affected"`
}

// UploadResponse 上传附件响应（用于 upload 端点）。
type UploadResponse struct {
	Message string             `json:"message"`
	File    *UploadedFileDTO   `json:"file,omitempty"`
}

// UploadedFileDTO 已上传文件元信息。
type UploadedFileDTO struct {
	Filename  string `json:"filename"`
	MimeType  string `json:"mime_type"`
	SizeBytes int64  `json:"size_bytes"`
	Path      string `json:"path"` // 相对路径，供前端引用
}
