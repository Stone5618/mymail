// Package dto 定义 HTTP 请求/响应的数据传输对象。
// 字段使用 JSON 标签，与原 Node.js 后端 API 契约保持 100% 兼容。
package dto

// RegisterRequest 注册请求。
// 字段顺序与原 Node.js 校验顺序一致。
type RegisterRequest struct {
	Username    string `json:"username" binding:"required"`
	Password    string `json:"password" binding:"required"`
	DisplayName string `json:"displayName"`
}

// LoginRequest 登录请求。
type LoginRequest struct {
	Email    string `json:"email" binding:"required"`
	Password string `json:"password" binding:"required"`
	Remember bool   `json:"remember"`
}

// UpdateProfileRequest 更新个人资料请求。
// 所有字段可选（指针类型，nil 表示不更新）。
type UpdateProfileRequest struct {
	DisplayName *string `json:"displayName"`
	Signature   *string `json:"signature"`
}

// ChangePasswordRequest 修改密码请求（需校验当前密码）。
type ChangePasswordRequest struct {
	CurrentPassword string `json:"currentPassword" binding:"required"`
	NewPassword     string `json:"newPassword" binding:"required"`
}

// ChangeDefaultPasswordRequest 强制改密请求（不校验当前密码）。
type ChangeDefaultPasswordRequest struct {
	NewPassword string `json:"newPassword" binding:"required"`
}

// UserPublic 用户公开信息（login/register 响应中的 user 字段）。
// 与原 Node.js 响应完全一致：仅 5 字段，camelCase。
type UserPublic struct {
	ID          int64  `json:"id"`
	Username    string `json:"username"`
	Email       string `json:"email"`
	DisplayName string `json:"displayName"`
	Role        string `json:"role"`
}

// UserMe /api/auth/me 响应（字段最全，camelCase）。
type UserMe struct {
	ID           int64  `json:"id"`
	Username     string `json:"username"`
	Email        string `json:"email"`
	DisplayName  string `json:"displayName"`
	Role         string `json:"role"`
	Signature    *string `json:"signature"`
	StorageLimit int64  `json:"storageLimit"`
	StorageUsed  int64  `json:"storageUsed"`
	CreatedAt    string `json:"createdAt"`
}

// AuthResponse 认证成功响应（login/register）。
type AuthResponse struct {
	Message string    `json:"message"`
	Token   string    `json:"token"`
	User    UserPublic `json:"user"`
	// RequirePasswordChange 仅在登录且 is_default_password=1 时为 true
	RequirePasswordChange bool `json:"requirePasswordChange,omitempty"`
}

// MessageResponse 通用消息响应。
type MessageResponse struct {
	Message string `json:"message"`
}

// ErrorResponse 错误响应（与原 Node.js 一致：仅 error 字段）。
type ErrorResponse struct {
	Error string `json:"error"`
}
