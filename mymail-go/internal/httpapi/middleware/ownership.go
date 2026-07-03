// Package middleware
// ownership.go 实现 RequireOwnedMail 中间件。
//
// 修复 P0-3：原 Node.js 后端 PUT /:id/read、PUT /:id/unread、PUT /:id/star
// 均不校验邮件归属权，任意登录用户可改任意邮件状态（IDOR 越权）。
// 本中间件统一在 :id 端点前置校验：邮件必须归属于当前用户。
//
// 用法：
//
//	mailGroup.PUT("/:id/read",
//	    middleware.Authenticate(jwtMgr, userDAO),
//	    middleware.RequireOwnedMail(mailSvc),
//	    mailH.MarkRead)
//
// 中间件将 *dao.Message 注入到 gin.Context（key="mail"），
// 后续 handler 可通过 OwnedMail(c) 取用，避免重复查询。
package middleware

import (
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"

	"github.com/mymail/mymail-go/internal/service"
	"github.com/mymail/mymail-go/internal/storage/dao"
)

// ContextKeyMail 是 gin.Context 中存储已校验归属的邮件的 key。
const ContextKeyMail = "mail"

// RequireOwnedMail 校验路径参数 :id 指代的邮件归属于当前登录用户。
// 必须在 Authenticate 之后使用。
//
// 行为：
//   - 解析 :id（parseInt 失败 → 400）
//   - 调用 mailService.Get(userID, id) 校验归属
//   - 邮件不存在或不属于该用户 → 404 {"error":"邮件不存在"}（不区分，防枚举）
//   - 校验通过：将 *dao.Message 注入 context，调用 c.Next()
func RequireOwnedMail(mailSvc *service.MailService) gin.HandlerFunc {
	return func(c *gin.Context) {
		user := CurrentUser(c)
		if user == nil {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "未登录"})
			return
		}

		idStr := c.Param("id")
		id, err := strconv.ParseInt(idStr, 10, 64)
		if err != nil || id <= 0 {
			c.AbortWithStatusJSON(http.StatusBadRequest, gin.H{"error": "无效的邮件 ID"})
			return
		}

		msg, _, err := mailSvc.Get(c.Request.Context(), user.ID, id)
		if err != nil {
			// service 层返回 MailError(404) 表示不存在/无权
			if me, ok := service.IsMailError(err); ok {
				c.AbortWithStatusJSON(me.Status, gin.H{"error": me.Message})
				return
			}
			c.AbortWithStatusJSON(http.StatusInternalServerError, gin.H{"error": "服务器内部错误"})
			return
		}
		if msg == nil {
			c.AbortWithStatusJSON(http.StatusNotFound, gin.H{"error": "邮件不存在"})
			return
		}

		c.Set(ContextKeyMail, msg)
		c.Next()
	}
}

// OwnedMail 从 gin.Context 取出 RequireOwnedMail 注入的邮件。
// 未注入时返回 nil。
func OwnedMail(c *gin.Context) *dao.Message {
	v, ok := c.Get(ContextKeyMail)
	if !ok {
		return nil
	}
	msg, _ := v.(*dao.Message)
	return msg
}
