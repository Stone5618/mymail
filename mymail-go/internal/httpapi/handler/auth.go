// Package handler
// auth.go 实现认证相关 HTTP 端点。
//
// 端点清单（与原 Node.js 路径完全一致）：
//   POST /api/auth/register               - 注册
//   POST /api/auth/login                  - 登录
//   GET  /api/auth/me                     - 获取当前用户
//   PUT  /api/auth/profile                - 更新个人资料
//   PUT  /api/auth/password               - 修改密码
//   POST /api/auth/change-default-password - 强制改密
//
// 响应格式：与原 Node.js 100% 兼容
//   成功：{"message":"...", "token":"...", "user":{...}}
//   失败：{"error":"..."}
package handler

import (
	"errors"
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/mymail/mymail-go/internal/httpapi/dto"
	"github.com/mymail/mymail-go/internal/httpapi/middleware"
	"github.com/mymail/mymail-go/internal/service"
	"github.com/mymail/mymail-go/internal/storage/dao"
)

// AuthHandler 处理认证端点。
type AuthHandler struct {
	svc *service.AuthService
}

// NewAuthHandler 创建 AuthHandler。
func NewAuthHandler(svc *service.AuthService) *AuthHandler {
	return &AuthHandler{svc: svc}
}

// Register POST /api/auth/register
func (h *AuthHandler) Register(c *gin.Context) {
	var req dto.RegisterRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, dto.ErrorResponse{Error: "用户名和密码不能为空"})
		return
	}

	result, err := h.svc.Register(c.Request.Context(), req.Username, req.Password, req.DisplayName)
	if err != nil {
		writeAuthError(c, err)
		return
	}

	c.JSON(http.StatusCreated, dto.AuthResponse{
		Message: "注册成功",
		Token:   result.Token,
		User:    toUserPublic(result.User),
	})
}

// Login POST /api/auth/login
func (h *AuthHandler) Login(c *gin.Context) {
	var req dto.LoginRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, dto.ErrorResponse{Error: "邮箱和密码不能为空"})
		return
	}

	email := service.SanitizeEmail(req.Email)
	result, err := h.svc.Login(c.Request.Context(), email, req.Password, req.Remember, c.ClientIP())
	if err != nil {
		writeAuthError(c, err)
		return
	}

	if result.RequirePasswordChange {
		c.JSON(http.StatusOK, dto.AuthResponse{
			Message:               "需要修改默认密码",
			Token:                 result.Token,
			User:                  toUserPublic(result.User),
			RequirePasswordChange: true,
		})
		return
	}

	c.JSON(http.StatusOK, dto.AuthResponse{
		Message: "登录成功",
		Token:   result.Token,
		User:    toUserPublic(result.User),
	})
}

// Me GET /api/auth/me
func (h *AuthHandler) Me(c *gin.Context) {
	user := middleware.CurrentUser(c)
	if user == nil {
		c.JSON(http.StatusUnauthorized, dto.ErrorResponse{Error: "未登录"})
		return
	}

	var sig *string
	if user.Signature.Valid {
		s := user.Signature.String
		sig = &s
	}

	c.JSON(http.StatusOK, dto.UserMe{
		ID:           user.ID,
		Username:     user.Username,
		Email:        user.Email,
		DisplayName:  user.DisplayName,
		Role:         user.Role,
		Signature:    sig,
		StorageLimit: user.StorageLimit,
		StorageUsed:  user.StorageUsed,
		Preferences:  user.Preferences,
		CreatedAt:    user.CreatedAt,
	})
}

// UpdateProfile PUT /api/auth/profile
func (h *AuthHandler) UpdateProfile(c *gin.Context) {
	user := middleware.CurrentUser(c)
	if user == nil {
		c.JSON(http.StatusUnauthorized, dto.ErrorResponse{Error: "未登录"})
		return
	}

	var req dto.UpdateProfileRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, dto.ErrorResponse{Error: "请求格式错误"})
		return
	}

	if err := h.svc.UpdateProfile(c.Request.Context(), user.ID, req.DisplayName, req.Signature, req.Preferences); err != nil {
		writeAuthError(c, err)
		return
	}

	c.JSON(http.StatusOK, dto.MessageResponse{Message: "更新成功"})
}

// ChangePassword PUT /api/auth/password
func (h *AuthHandler) ChangePassword(c *gin.Context) {
	user := middleware.CurrentUser(c)
	if user == nil {
		c.JSON(http.StatusUnauthorized, dto.ErrorResponse{Error: "未登录"})
		return
	}

	var req dto.ChangePasswordRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, dto.ErrorResponse{Error: "请填写当前密码和新密码"})
		return
	}

	if err := h.svc.ChangePassword(c.Request.Context(), user, req.CurrentPassword, req.NewPassword); err != nil {
		writeAuthError(c, err)
		return
	}

	c.JSON(http.StatusOK, dto.MessageResponse{Message: "密码修改成功"})
}

// ChangeDefaultPassword POST /api/auth/change-default-password
func (h *AuthHandler) ChangeDefaultPassword(c *gin.Context) {
	user := middleware.CurrentUser(c)
	if user == nil {
		c.JSON(http.StatusUnauthorized, dto.ErrorResponse{Error: "未登录"})
		return
	}

	var req dto.ChangeDefaultPasswordRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, dto.ErrorResponse{Error: "请填写新密码"})
		return
	}

	if err := h.svc.ChangeDefaultPassword(c.Request.Context(), user, req.NewPassword); err != nil {
		writeAuthError(c, err)
		return
	}

	c.JSON(http.StatusOK, dto.MessageResponse{Message: "密码修改成功"})
}

// toUserPublic 将 dao.User 转为 dto.UserPublic。
func toUserPublic(u *dao.User) dto.UserPublic {
	return dto.UserPublic{
		ID:          u.ID,
		Username:    u.Username,
		Email:       u.Email,
		DisplayName: u.DisplayName,
		Role:        u.Role,
	}
}

// writeAuthError 处理 service 层返回的错误，写入 HTTP 响应。
// 若为 *AuthError，使用其携带的 Status 与 Message；
// 否则视为内部错误，返回 500。
func writeAuthError(c *gin.Context, err error) {
	var ae *service.AuthError
	if errors.As(err, &ae) {
		c.JSON(ae.Status, dto.ErrorResponse{Error: ae.Message})
		return
	}
	c.JSON(http.StatusInternalServerError, dto.ErrorResponse{Error: "服务器内部错误"})
}
