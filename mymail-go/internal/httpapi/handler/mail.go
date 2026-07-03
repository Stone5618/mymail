// Package handler
// mail.go 实现邮件相关 HTTP 端点。
//
// 端点清单（与原 Node.js 路径完全一致 + 补齐前端期望的批量/上传端点）：
//   GET    /api/mail/list                          - 列表（folder/page/limit/search/unread）
//   GET    /api/mail/unread-count                  - 各文件夹未读数
//   GET    /api/mail/:id                           - 邮件详情（含附件，副作用 markRead）
//   POST   /api/mail/send                          - 发送（multipart/form-data）
//   POST   /api/mail/save-draft                    - 保存草稿（JSON）
//   PUT    /api/mail/:id/read                      - 标记已读（P0-3 修复归属校验）
//   PUT    /api/mail/:id/unread                    - 标记未读（P0-3 修复归属校验）
//   PUT    /api/mail/:id/star                      - 切换星标（P0-3 修复归属校验）
//   DELETE /api/mail/:id                           - 删除（TRASH 或永久）
//   PUT    /api/mail/:id/restore                   - 恢复
//   POST   /api/mail/empty-trash                   - 清空回收站
//   GET    /api/mail/:id/attachments/:aid/download - 下载单附件
//   GET    /api/mail/:id/attachments/download-all  - 批量下载 ZIP
//   POST   /api/mail/batch/mark-read               - 批量标记已读（补齐）
//   POST   /api/mail/batch/move                    - 批量移动（补齐）
//   POST   /api/mail/batch/delete                  - 批量删除（补齐）
//
// 兼容性：错误消息与原 Node.js 完全一致；响应字段 snake_case。
// 修复 P0-3：所有 :id 端点通过 RequireOwnedMail 中间件校验归属。
package handler

import (
	"errors"
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"

	"github.com/mymail/mymail-go/internal/httpapi/dto"
	"github.com/mymail/mymail-go/internal/httpapi/middleware"
	"github.com/mymail/mymail-go/internal/service"
	"github.com/mymail/mymail-go/internal/storage/attachment"
	"github.com/mymail/mymail-go/internal/storage/dao"
	"github.com/mymail/mymail-go/internal/util"
)

// MailHandler 处理邮件端点。
type MailHandler struct {
	svc         *service.MailService
	attachStore *attachment.Store
}

// NewMailHandler 创建 MailHandler。
func NewMailHandler(svc *service.MailService, attachStore *attachment.Store) *MailHandler {
	return &MailHandler{svc: svc, attachStore: attachStore}
}

// ============ 列表/详情 ============

// List GET /api/mail/list
// 查询参数：folder（默认 INBOX）、page（默认 1）、limit（默认 20）、search、unread（字符串 "true"）
func (h *MailHandler) List(c *gin.Context) {
	user := middleware.CurrentUser(c)
	if user == nil {
		c.JSON(http.StatusUnauthorized, dto.ErrorResponse{Error: "未登录"})
		return
	}

	folder := c.DefaultQuery("folder", "INBOX")
	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	limit, _ := strconv.Atoi(c.DefaultQuery("limit", "20"))
	search := c.Query("search")
	unreadOnly := c.Query("unread") == "true"

	result, err := h.svc.List(c.Request.Context(), user.ID, folder, page, limit, search, unreadOnly)
	if err != nil {
		writeMailError(c, err)
		return
	}

	// 转换为 DTO
	messages := make([]*dto.MailDetailResponse, 0, len(result.Messages))
	for _, m := range result.Messages {
		messages = append(messages, toMessageResponse(m, nil))
	}

	c.JSON(http.StatusOK, dto.ListResponse{
		Messages:    messages,
		Total:       result.Total,
		Page:        result.Page,
		Limit:       result.Limit,
		UnreadCount: result.UnreadCount,
	})
}

// UnreadCount GET /api/mail/unread-count
// 返回 INBOX/SENT/DRAFTS/TRASH 四个文件夹的未读数。
func (h *MailHandler) UnreadCount(c *gin.Context) {
	user := middleware.CurrentUser(c)
	if user == nil {
		c.JSON(http.StatusUnauthorized, dto.ErrorResponse{Error: "未登录"})
		return
	}

	folders := []string{"INBOX", "SENT", "DRAFTS", "TRASH"}
	counts, err := h.svc.UnreadCountByFolders(c.Request.Context(), user.ID, folders)
	if err != nil {
		writeMailError(c, err)
		return
	}

	c.JSON(http.StatusOK, dto.UnreadCountResponse{
		INBOX:  counts["INBOX"],
		SENT:   counts["SENT"],
		DRAFTS: counts["DRAFTS"],
		TRASH:  counts["TRASH"],
	})
}

// Get GET /api/mail/:id
// 含副作用：获取详情后自动标记为已读（与原 Node.js 一致）。
func (h *MailHandler) Get(c *gin.Context) {
	user := middleware.CurrentUser(c)
	if user == nil {
		c.JSON(http.StatusUnauthorized, dto.ErrorResponse{Error: "未登录"})
		return
	}

	// RequireOwnedMail 中间件已校验归属并注入 mail
	msg := middleware.OwnedMail(c)
	if msg == nil {
		c.JSON(http.StatusNotFound, dto.ErrorResponse{Error: "邮件不存在"})
		return
	}

	// 查询附件（中间件已查过一次，此处复用 service.Get 的结果更佳
	// 但 middleware 调用的是 svc.Get 已返回 attachments，这里 OwnedMail 只存了 msg）
	// 为简化，重新查附件
	attachments, err := h.svc.ListAttachmentsByMessage(c.Request.Context(), user.ID, msg.ID)
	if err != nil {
		writeMailError(c, err)
		return
	}

	// 副作用：标记已读（与原 Node.js 一致）
	if !msg.IsRead {
		_ = h.svc.MarkRead(c.Request.Context(), user.ID, msg.ID)
		msg.IsRead = true
	}

	c.JSON(http.StatusOK, toMessageResponse(msg, attachments))
}

// ============ 发送/草稿 ============

// Send POST /api/mail/send
// Content-Type: multipart/form-data
// 字段：to、cc、bcc、subject、bodyHtml、bodyText、replyTo + attachments（多文件）
func (h *MailHandler) Send(c *gin.Context) {
	user := middleware.CurrentUser(c)
	if user == nil {
		c.JSON(http.StatusUnauthorized, dto.ErrorResponse{Error: "未登录"})
		return
	}

	// 读取表单字段（camelCase，与前端一致）
	to := c.PostForm("to")
	cc := c.PostForm("cc")
	bcc := c.PostForm("bcc")
	subject := c.PostForm("subject")
	bodyHTML := c.PostForm("bodyHtml")
	bodyText := c.PostForm("bodyText")
	replyTo := c.PostForm("replyTo")

	// 处理附件
	var attachments []service.AttachmentMeta
	if form, err := c.MultipartForm(); err == nil && form != nil {
		files := form.File["attachments"]
		for _, fh := range files {
			// 保存附件文件到磁盘（P0-7：内部含 MIME 白名单 + 魔数校验）
			storagePath, _, err := h.attachStore.SaveFromMultipartFile(user.ID, fh)
			if err != nil {
				c.JSON(http.StatusBadRequest, dto.ErrorResponse{Error: err.Error()})
				return
			}
			attachments = append(attachments, service.AttachmentMeta{
				Filename:    fh.Filename,
				MimeType:    fh.Header.Get("Content-Type"),
				SizeBytes:   fh.Size,
				StoragePath: storagePath,
			})
		}
	}

	result, err := h.svc.Send(c.Request.Context(), user.ID, service.SendInput{
		To:          to,
		Cc:          cc,
		Bcc:         bcc,
		Subject:     subject,
		BodyHTML:    bodyHTML,
		BodyText:    bodyText,
		ReplyTo:     replyTo,
		Attachments: attachments,
	})
	if err != nil {
		writeMailError(c, err)
		return
	}

	c.JSON(http.StatusOK, dto.SendResponse{
		Message:   "发送成功",
		MessageID: result.MessageID,
	})
}

// SaveDraft POST /api/mail/save-draft
// Content-Type: application/json
func (h *MailHandler) SaveDraft(c *gin.Context) {
	user := middleware.CurrentUser(c)
	if user == nil {
		c.JSON(http.StatusUnauthorized, dto.ErrorResponse{Error: "未登录"})
		return
	}

	var req dto.SaveDraftRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, dto.ErrorResponse{Error: "请求格式错误"})
		return
	}

	id, err := h.svc.SaveDraft(c.Request.Context(), user.ID, service.SaveDraftInput{
		To:       req.To,
		Cc:       req.Cc,
		Bcc:      req.Bcc,
		Subject:  req.Subject,
		BodyHTML: req.BodyHTML,
		BodyText: req.BodyText,
	})
	if err != nil {
		writeMailError(c, err)
		return
	}

	c.JSON(http.StatusOK, dto.SaveDraftResponse{
		Message: "草稿已保存",
		ID:      id,
	})
}

// ============ 标记/星标/删除/恢复 ============

// MarkRead PUT /api/mail/:id/read
func (h *MailHandler) MarkRead(c *gin.Context) {
	user := middleware.CurrentUser(c)
	if user == nil {
		c.JSON(http.StatusUnauthorized, dto.ErrorResponse{Error: "未登录"})
		return
	}
	msg := middleware.OwnedMail(c)
	if msg == nil {
		c.JSON(http.StatusNotFound, dto.ErrorResponse{Error: "邮件不存在"})
		return
	}
	if err := h.svc.MarkRead(c.Request.Context(), user.ID, msg.ID); err != nil {
		writeMailError(c, err)
		return
	}
	c.JSON(http.StatusOK, dto.MessageResponse{Message: "ok"})
}

// MarkUnread PUT /api/mail/:id/unread
func (h *MailHandler) MarkUnread(c *gin.Context) {
	user := middleware.CurrentUser(c)
	if user == nil {
		c.JSON(http.StatusUnauthorized, dto.ErrorResponse{Error: "未登录"})
		return
	}
	msg := middleware.OwnedMail(c)
	if msg == nil {
		c.JSON(http.StatusNotFound, dto.ErrorResponse{Error: "邮件不存在"})
		return
	}
	if err := h.svc.MarkUnread(c.Request.Context(), user.ID, msg.ID); err != nil {
		writeMailError(c, err)
		return
	}
	c.JSON(http.StatusOK, dto.MessageResponse{Message: "ok"})
}

// ToggleStar PUT /api/mail/:id/star
func (h *MailHandler) ToggleStar(c *gin.Context) {
	user := middleware.CurrentUser(c)
	if user == nil {
		c.JSON(http.StatusUnauthorized, dto.ErrorResponse{Error: "未登录"})
		return
	}
	msg := middleware.OwnedMail(c)
	if msg == nil {
		c.JSON(http.StatusNotFound, dto.ErrorResponse{Error: "邮件不存在"})
		return
	}
	if err := h.svc.ToggleStar(c.Request.Context(), user.ID, msg.ID); err != nil {
		writeMailError(c, err)
		return
	}
	c.JSON(http.StatusOK, dto.MessageResponse{Message: "ok"})
}

// Delete DELETE /api/mail/:id
// 若邮件已在 TRASH → 永久删除；否则软删除（移到 TRASH）。
func (h *MailHandler) Delete(c *gin.Context) {
	user := middleware.CurrentUser(c)
	if user == nil {
		c.JSON(http.StatusUnauthorized, dto.ErrorResponse{Error: "未登录"})
		return
	}
	msg := middleware.OwnedMail(c)
	if msg == nil {
		c.JSON(http.StatusNotFound, dto.ErrorResponse{Error: "邮件不存在"})
		return
	}

	var err error
	if msg.Folder == "TRASH" {
		err = h.svc.PermanentDelete(c.Request.Context(), user.ID, msg.ID)
	} else {
		err = h.svc.SoftDelete(c.Request.Context(), user.ID, msg.ID)
	}
	if err != nil {
		writeMailError(c, err)
		return
	}
	c.JSON(http.StatusOK, dto.MessageResponse{Message: "ok"})
}

// Restore PUT /api/mail/:id/restore
// 从垃圾箱恢复到 INBOX。
func (h *MailHandler) Restore(c *gin.Context) {
	user := middleware.CurrentUser(c)
	if user == nil {
		c.JSON(http.StatusUnauthorized, dto.ErrorResponse{Error: "未登录"})
		return
	}
	msg := middleware.OwnedMail(c)
	if msg == nil {
		c.JSON(http.StatusNotFound, dto.ErrorResponse{Error: "邮件不存在"})
		return
	}
	if err := h.svc.Restore(c.Request.Context(), user.ID, msg.ID); err != nil {
		writeMailError(c, err)
		return
	}
	c.JSON(http.StatusOK, dto.MessageResponse{Message: "已恢复"})
}

// EmptyTrash POST /api/mail/empty-trash
func (h *MailHandler) EmptyTrash(c *gin.Context) {
	user := middleware.CurrentUser(c)
	if user == nil {
		c.JSON(http.StatusUnauthorized, dto.ErrorResponse{Error: "未登录"})
		return
	}
	if err := h.svc.EmptyTrash(c.Request.Context(), user.ID); err != nil {
		writeMailError(c, err)
		return
	}
	c.JSON(http.StatusOK, dto.MessageResponse{Message: "垃圾箱已清空"})
}

// ============ 附件下载 ============

// DownloadAttachment GET /api/mail/:id/attachments/:aid/download
// :id 由 RequireOwnedMail 校验（确保邮件归属），:aid 由本 handler 校验属于该邮件。
func (h *MailHandler) DownloadAttachment(c *gin.Context) {
	user := middleware.CurrentUser(c)
	if user == nil {
		c.JSON(http.StatusUnauthorized, dto.ErrorResponse{Error: "未登录"})
		return
	}
	msg := middleware.OwnedMail(c)
	if msg == nil {
		c.JSON(http.StatusNotFound, dto.ErrorResponse{Error: "邮件不存在"})
		return
	}

	aidStr := c.Param("aid")
	aid, err := strconv.ParseInt(aidStr, 10, 64)
	if err != nil || aid <= 0 {
		c.JSON(http.StatusBadRequest, dto.ErrorResponse{Error: "无效的附件 ID"})
		return
	}

	att, err := h.svc.GetAttachmentForDownload(c.Request.Context(), user.ID, aid)
	if err != nil {
		writeMailError(c, err)
		return
	}
	// 二次校验：附件必须属于该邮件（防 IDOR）
	if att.MessageID != msg.ID {
		c.JSON(http.StatusForbidden, dto.ErrorResponse{Error: "无权访问"})
		return
	}

	// 打开文件流式下载
	f, err := h.attachStore.OpenFile(att.StoragePath)
	if err != nil {
		c.JSON(http.StatusNotFound, dto.ErrorResponse{Error: "附件文件不存在"})
		return
	}
	defer f.Close()

	c.Header("Content-Type", "application/octet-stream")
	c.Header("Content-Disposition", `attachment; filename="`+att.Filename+`"`)
	c.Status(http.StatusOK)
	c.Writer.Flush()
	// 流式写入
	buf := make([]byte, 32*1024)
	for {
		n, err := f.Read(buf)
		if n > 0 {
			if _, werr := c.Writer.Write(buf[:n]); werr != nil {
				return
			}
		}
		if err != nil {
			break
		}
	}
}

// DownloadAllAttachments GET /api/mail/:id/attachments/download-all
// 将邮件所有附件打包为 zip 流式下载。
func (h *MailHandler) DownloadAllAttachments(c *gin.Context) {
	user := middleware.CurrentUser(c)
	if user == nil {
		c.JSON(http.StatusUnauthorized, dto.ErrorResponse{Error: "未登录"})
		return
	}
	msg := middleware.OwnedMail(c)
	if msg == nil {
		c.JSON(http.StatusNotFound, dto.ErrorResponse{Error: "邮件不存在"})
		return
	}

	attachments, err := h.svc.ListAttachmentsByMessage(c.Request.Context(), user.ID, msg.ID)
	if err != nil {
		writeMailError(c, err)
		return
	}
	if len(attachments) == 0 {
		c.JSON(http.StatusNotFound, dto.ErrorResponse{Error: "没有附件"})
		return
	}

	// 设置响应头（与原 Node.js archiver 一致）
	c.Header("Content-Type", "application/zip")
	c.Header("Content-Disposition", `attachment; filename="attachments.zip"`)
	c.Status(http.StatusOK)
	c.Writer.Flush()

	// 构造 zip 条目
	entries := make([]util.ZipEntry, 0, len(attachments))
	for _, a := range attachments {
		entries = append(entries, util.ZipEntry{
			Filename:   a.Filename,
			SourcePath: a.StoragePath,
		})
	}

	// 流式写入 zip（util.ZipAttachments 内部处理重名）
	if _, err := util.ZipAttachments(util.ZipAttachmentsInput{
		Entries: entries,
		Writer:  c.Writer,
	}); err != nil {
		// 此时响应头已发送，只能记日志，无法改状态码
		_ = err
		return
	}
}

// ============ 批量操作（补齐前端期望的端点）============

// BatchMarkRead POST /api/mail/batch/mark-read
func (h *MailHandler) BatchMarkRead(c *gin.Context) {
	user := middleware.CurrentUser(c)
	if user == nil {
		c.JSON(http.StatusUnauthorized, dto.ErrorResponse{Error: "未登录"})
		return
	}

	var req dto.BatchOperationRequest
	if err := c.ShouldBindJSON(&req); err != nil || len(req.IDs) == 0 {
		c.JSON(http.StatusBadRequest, dto.ErrorResponse{Error: "ids 不能为空"})
		return
	}

	affected, err := h.svc.BatchMarkRead(c.Request.Context(), user.ID, req.IDs)
	if err != nil {
		writeMailError(c, err)
		return
	}
	c.JSON(http.StatusOK, dto.BatchOperationResponse{
		Message:  "ok",
		Affected: affected,
	})
}

// BatchMove POST /api/mail/batch/move
func (h *MailHandler) BatchMove(c *gin.Context) {
	user := middleware.CurrentUser(c)
	if user == nil {
		c.JSON(http.StatusUnauthorized, dto.ErrorResponse{Error: "未登录"})
		return
	}

	var req dto.BatchOperationRequest
	if err := c.ShouldBindJSON(&req); err != nil || len(req.IDs) == 0 {
		c.JSON(http.StatusBadRequest, dto.ErrorResponse{Error: "ids 不能为空"})
		return
	}

	affected, err := h.svc.BatchMove(c.Request.Context(), user.ID, req.IDs, req.Folder)
	if err != nil {
		writeMailError(c, err)
		return
	}
	c.JSON(http.StatusOK, dto.BatchOperationResponse{
		Message:  "ok",
		Affected: affected,
	})
}

// BatchDelete POST /api/mail/batch/delete
func (h *MailHandler) BatchDelete(c *gin.Context) {
	user := middleware.CurrentUser(c)
	if user == nil {
		c.JSON(http.StatusUnauthorized, dto.ErrorResponse{Error: "未登录"})
		return
	}

	var req dto.BatchOperationRequest
	if err := c.ShouldBindJSON(&req); err != nil || len(req.IDs) == 0 {
		c.JSON(http.StatusBadRequest, dto.ErrorResponse{Error: "ids 不能为空"})
		return
	}

	affected, err := h.svc.BatchDelete(c.Request.Context(), user.ID, req.IDs)
	if err != nil {
		writeMailError(c, err)
		return
	}
	c.JSON(http.StatusOK, dto.BatchOperationResponse{
		Message:  "ok",
		Affected: affected,
	})
}

// ============ 辅助函数 ============

// toMessageResponse 将 dao.Message 转为 dto.MailDetailResponse。
func toMessageResponse(m *dao.Message, attachments []*dao.Attachment) *dto.MailDetailResponse {
	resp := &dto.MailDetailResponse{
		ID:          m.ID,
		UserID:      m.UserID,
		Folder:      m.Folder,
		MessageID:   m.MessageID,
		UID:         m.UID,
		FromAddr:    m.FromAddr,
		FromName:    m.FromName,
		ToAddr:      m.ToAddr,
		CcAddr:      m.CcAddr,
		BccAddr:     m.BccAddr,
		ReplyTo:     m.ReplyTo,
		Subject:     m.Subject,
		BodyText:    m.BodyText,
		BodyHTML:    m.BodyHTML,
		IsRead:      m.IsRead,
		IsStarred:   m.IsStarred,
		IsDeleted:   m.IsDeleted,
		HasAttach:   m.HasAttach,
		AttachCount: m.AttachCount,
		SizeBytes:   m.SizeBytes,
		InReplyTo:   m.InReplyTo,
		Flags:       m.Flags,
		SpamScore:   m.SpamScore,
		SpamReasons: m.SpamReasons,
		ReceivedAt:  m.ReceivedAt,
	}
	if attachments != nil {
		resp.Attachments = make([]*dto.AttachmentResponse, 0, len(attachments))
		for _, a := range attachments {
			resp.Attachments = append(resp.Attachments, &dto.AttachmentResponse{
				ID:        a.ID,
				MessageID: a.MessageID,
				Filename:  a.Filename,
				MimeType:  a.MimeType,
				SizeBytes: a.SizeBytes,
				CreatedAt: a.CreatedAt,
			})
		}
	}
	return resp
}

// writeMailError 处理 service 层返回的错误，写入 HTTP 响应。
// 若为 *MailError，使用其携带的 Status 与 Message；
// 否则视为内部错误，返回 500。
func writeMailError(c *gin.Context, err error) {
	var me *service.MailError
	if errors.As(err, &me) {
		c.JSON(me.Status, dto.ErrorResponse{Error: me.Message})
		return
	}
	c.JSON(http.StatusInternalServerError, dto.ErrorResponse{Error: "服务器内部错误"})
}
