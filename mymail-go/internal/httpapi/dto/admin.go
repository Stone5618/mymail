// Package dto
// admin.go 定义管理员后台与 API Key 相关的 HTTP 请求/响应 DTO。
//
// 命名约定：
//   - 管理员视角的用户信息含敏感字段（role/storage_limit/is_active），使用 snake_case
//     （与 mail.go 响应一致，管理员后台前端消费 snake_case 字段）
//   - API Key 明文仅在创建时返回一次（CreateAPIKeyResponse.PlainText）
package dto

// AdminStatsResponse 管理员统计响应。
type AdminStatsResponse struct {
	TotalUsers    int64 `json:"total_users"`
	ActiveUsers   int64 `json:"active_users"`
	TodayReceived int64 `json:"today_received"`
	TodaySent     int64 `json:"today_sent"`
}

// DnsStatusResponse DNS 记录检测响应。
type DnsStatusResponse struct {
	MX    string `json:"mx"`
	SPF   string `json:"spf"`
	DMARC string `json:"dmarc"`
}

// AdminUserResponse 管理员视角的用户信息（含敏感字段）。
type AdminUserResponse struct {
	ID           int64  `json:"id"`
	Username     string `json:"username"`
	Email        string `json:"email"`
	DisplayName  string `json:"display_name"`
	Role         string `json:"role"`
	StorageLimit int64  `json:"storage_limit"`
	StorageUsed  int64  `json:"storage_used"`
	IsActive     bool   `json:"is_active"`
	CreatedAt    string `json:"created_at"`
}

// CreateUserRequest 管理员创建用户请求。
type CreateUserRequest struct {
	Username     string `json:"username"`
	Email        string `json:"email"`
	Password     string `json:"password"`
	DisplayName  string `json:"display_name"`
	Role         string `json:"role"`
	StorageLimit int64  `json:"storage_limit"` // 0 表示默认 100MB
}

// UpdateUserRequest 管理员更新用户请求（所有字段指针，nil 不更新）。
type UpdateUserRequest struct {
	Role         *string `json:"role"`
	StorageLimit *int64  `json:"storage_limit"`
	IsActive     *bool   `json:"is_active"`
	Password     *string `json:"password"`
}

// APIKeyResponse API Key 响应（列表展示，不含明文）。
type APIKeyResponse struct {
	ID         int64    `json:"id"`
	Name       string   `json:"name"`
	KeyPrefix  string   `json:"key_prefix"`
	Scopes     []string `json:"scopes"`
	RateLimit  int      `json:"rate_limit"`
	IsActive   bool     `json:"is_active"`
	LastUsedAt *string  `json:"last_used_at"`
	CreatedAt  string   `json:"created_at"`
}

// CreateAPIKeyRequest 创建 API Key 请求。
type CreateAPIKeyRequest struct {
	Name      string   `json:"name"`
	Scopes    []string `json:"scopes"`
	RateLimit int      `json:"rate_limit"`
}

// CreateAPIKeyResponse 创建 API Key 响应（含明文，仅返回一次）。
type CreateAPIKeyResponse struct {
	ID        int64    `json:"id"`
	PlainText string   `json:"plain_text"` // 仅创建时返回
	Name      string   `json:"name"`
	KeyPrefix string   `json:"key_prefix"`
	Scopes    []string `json:"scopes"`
	RateLimit int      `json:"rate_limit"`
	CreatedAt string   `json:"created_at"`
}
