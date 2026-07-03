// Package handler
// apiv1.go 实现 /api/v1/* 端点（API Key 认证，供外部程序发信）。
//
// 端点清单：
//   POST /api/v1/send - 外部发信（API Key + RequireScope("send")）
//
// 认证：APIKeyAuth + RequireScope（路由层配置），handler 通过 middleware.CurrentUser /
// middleware.CurrentAPIKey 取认证上下文。
//
// 适配说明：MailService.Send 返回 *SendResult{MessageID, MailID}（无 QueueID），
// 本端点以 MailID 作为 APISendResponse.QueueID 返回。
package handler

import (
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"

	"github.com/mymail/mymail-go/internal/httpapi/dto"
	"github.com/mymail/mymail-go/internal/httpapi/middleware"
	"github.com/mymail/mymail-go/internal/service"
)

// APIV1Handler 处理 /api/v1/* 端点（API Key 认证，外部发信）。
type APIV1Handler struct {
	mailSvc *service.MailService
}

// NewAPIV1Handler 创建 APIV1Handler。
func NewAPIV1Handler(mailSvc *service.MailService) *APIV1Handler {
	return &APIV1Handler{mailSvc: mailSvc}
}

// ============ DTO ============

// APISendRequest 外部发信请求（JSON body）。
type APISendRequest struct {
	To       []string `json:"to"`
	Cc       []string `json:"cc"`
	Bcc      []string `json:"bcc"`
	Subject  string   `json:"subject"`
	BodyHTML string   `json:"body_html"`
	BodyText string   `json:"body_text"`
	ReplyTo  string   `json:"reply_to"`
}

// APISendResponse 发信响应。
// QueueID 对应 MailService.Send 返回的 MailID（邮件记录 ID）。
type APISendResponse struct {
	Message string `json:"message"`
	QueueID int64  `json:"queue_id"`
}

// ============ 端点 ============

// Send POST /api/v1/send → 外部发信。
// 认证（APIKeyAuth + RequireScope("send")）在路由层配置；
// handler 直接使用 CurrentUser / CurrentAPIKey。
func (h *APIV1Handler) Send(c *gin.Context) {
	user := middleware.CurrentUser(c)
	if user == nil {
		c.JSON(http.StatusUnauthorized, dto.ErrorResponse{Error: "未认证"})
		return
	}
	// API Key 须由路由层 RequireScope 校验；此处仅做防御性检查。
	if middleware.CurrentAPIKey(c) == nil {
		c.JSON(http.StatusUnauthorized, dto.ErrorResponse{Error: "未通过 API Key 认证"})
		return
	}

	var req APISendRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, dto.ErrorResponse{Error: "请求格式错误"})
		return
	}

	// 收件人非空校验（与 MailService.ErrRecipientRequired 一致，提前返回 400）。
	if len(req.To) == 0 {
		c.JSON(http.StatusBadRequest, dto.ErrorResponse{Error: "收件人不能为空"})
		return
	}

	// SendInput.To/Cc/Bcc 为逗号分隔字符串，将 []string 拼接。
	result, err := h.mailSvc.Send(c.Request.Context(), user.ID, service.SendInput{
		To:       strings.Join(req.To, ", "),
		Cc:       strings.Join(req.Cc, ", "),
		Bcc:      strings.Join(req.Bcc, ", "),
		Subject:  req.Subject,
		BodyHTML: req.BodyHTML,
		BodyText: req.BodyText,
		ReplyTo:  req.ReplyTo,
	})
	if err != nil {
		writeMailError(c, err)
		return
	}

	c.JSON(http.StatusOK, APISendResponse{
		Message: "发送成功",
		QueueID: result.MailID,
	})
}
