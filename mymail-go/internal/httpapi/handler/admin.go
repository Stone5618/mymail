// Package handler
// admin.go 实现管理员后台与 API Key 相关 HTTP 端点。
//
// 端点清单：
//   管理员（/api/admin/*，需 Authenticate + RequireAdmin 中间件）：
//     GET    /api/admin/stats           - 用户统计
//     GET    /api/admin/users           - 用户列表
//     GET    /api/admin/users/:id       - 用户详情
//     POST   /api/admin/users           - 创建用户
//     PUT    /api/admin/users/:id       - 更新用户
//     DELETE /api/admin/users/:id       - 软删除用户
//     GET    /api/admin/settings        - 查询全局设置
//     PUT    /api/admin/settings        - 更新全局设置
//
//   API Key（/api/auth/api-keys/*，需 Authenticate 中间件，用户维度）：
//     POST   /api/auth/api-keys         - 创建 API Key（明文仅返回一次）
//     GET    /api/auth/api-keys         - 列出当前用户的 API Key
//     DELETE /api/auth/api-keys/:id     - 删除 API Key（校验归属）
//
// 错误处理：
//   - service 返回 *AuthError → writeAuthError（复用 auth.go）
//   - service 返回其他 error → 500
//   - bind 失败 → 400 "请求参数无效"
//   - :id 解析失败 → 400 "无效的用户 ID" / "无效的 API Key ID"
package handler

import (
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"

	"github.com/mymail/mymail-go/internal/httpapi/dto"
	"github.com/mymail/mymail-go/internal/httpapi/middleware"
	"github.com/mymail/mymail-go/internal/service"
	"github.com/mymail/mymail-go/internal/storage/dao"
)

// ============ AdminHandler ============

// AdminHandler 处理 /api/admin/* 端点。
// 中间件层（Authenticate + RequireAdmin）已在 router 注册时配置，handler 不再校验权限。
type AdminHandler struct {
	adminSvc *service.AdminService
}

// NewAdminHandler 创建 AdminHandler。
func NewAdminHandler(adminSvc *service.AdminService) *AdminHandler {
	return &AdminHandler{adminSvc: adminSvc}
}

// Stats GET /api/admin/stats
func (h *AdminHandler) Stats(c *gin.Context) {
	stats, err := h.adminSvc.Stats(c.Request.Context())
	if err != nil {
		writeAuthError(c, err)
		return
	}
	c.JSON(http.StatusOK, dto.AdminStatsResponse{
		TotalUsers:  stats.TotalUsers,
		ActiveUsers: stats.ActiveUsers,
	})
}

// ListUsers GET /api/admin/users
func (h *AdminHandler) ListUsers(c *gin.Context) {
	users, err := h.adminSvc.ListUsers(c.Request.Context())
	if err != nil {
		writeAuthError(c, err)
		return
	}
	resp := make([]dto.AdminUserResponse, 0, len(users))
	for _, u := range users {
		resp = append(resp, toAdminUserResponse(u))
	}
	c.JSON(http.StatusOK, resp)
}

// GetUser GET /api/admin/users/:id
func (h *AdminHandler) GetUser(c *gin.Context) {
	id, ok := parseUserID(c)
	if !ok {
		c.JSON(http.StatusBadRequest, dto.ErrorResponse{Error: "无效的用户 ID"})
		return
	}
	u, err := h.adminSvc.GetUser(c.Request.Context(), id)
	if err != nil {
		writeAuthError(c, err)
		return
	}
	c.JSON(http.StatusOK, toAdminUserResponse(u))
}

// CreateUser POST /api/admin/users
func (h *AdminHandler) CreateUser(c *gin.Context) {
	var req dto.CreateUserRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, dto.ErrorResponse{Error: "请求参数无效"})
		return
	}
	u, err := h.adminSvc.CreateUser(c.Request.Context(), service.CreateUserAdminInput{
		Username:     req.Username,
		Email:        req.Email,
		Password:     req.Password,
		DisplayName:  req.DisplayName,
		Role:         req.Role,
		StorageLimit: req.StorageLimit,
	})
	if err != nil {
		writeAuthError(c, err)
		return
	}
	c.JSON(http.StatusCreated, toAdminUserResponse(u))
}

// UpdateUser PUT /api/admin/users/:id
func (h *AdminHandler) UpdateUser(c *gin.Context) {
	id, ok := parseUserID(c)
	if !ok {
		c.JSON(http.StatusBadRequest, dto.ErrorResponse{Error: "无效的用户 ID"})
		return
	}
	var req dto.UpdateUserRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, dto.ErrorResponse{Error: "请求参数无效"})
		return
	}
	if err := h.adminSvc.UpdateUser(c.Request.Context(), id, service.UpdateUserAdminInput{
		Role:         req.Role,
		StorageLimit: req.StorageLimit,
		IsActive:     req.IsActive,
		Password:     req.Password,
	}); err != nil {
		writeAuthError(c, err)
		return
	}
	c.JSON(http.StatusOK, dto.MessageResponse{Message: "更新成功"})
}

// DeleteUser DELETE /api/admin/users/:id（软删除）
func (h *AdminHandler) DeleteUser(c *gin.Context) {
	id, ok := parseUserID(c)
	if !ok {
		c.JSON(http.StatusBadRequest, dto.ErrorResponse{Error: "无效的用户 ID"})
		return
	}
	if err := h.adminSvc.DeleteUser(c.Request.Context(), id); err != nil {
		writeAuthError(c, err)
		return
	}
	c.JSON(http.StatusOK, dto.MessageResponse{Message: "删除成功"})
}

// GetSettings GET /api/admin/settings
func (h *AdminHandler) GetSettings(c *gin.Context) {
	settings, err := h.adminSvc.GetSettings(c.Request.Context())
	if err != nil {
		writeAuthError(c, err)
		return
	}
	items := make([]dto.SettingItem, 0, len(settings))
	for _, s := range settings {
		items = append(items, dto.SettingItem{
			Key:       s.Key,
			Value:     s.Value,
			UpdatedAt: s.UpdatedAt,
		})
	}
	c.JSON(http.StatusOK, dto.SettingsResponse{Settings: items})
}

// UpdateSettings PUT /api/admin/settings
func (h *AdminHandler) UpdateSettings(c *gin.Context) {
	var req dto.UpdateSettingsRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, dto.ErrorResponse{Error: "请求参数无效"})
		return
	}
	if err := h.adminSvc.UpdateSettings(c.Request.Context(), req.Settings); err != nil {
		writeAuthError(c, err)
		return
	}
	c.JSON(http.StatusOK, dto.MessageResponse{Message: "更新成功"})
}

// ============ APIKeyHandler ============

// APIKeyHandler 处理 /api/auth/api-keys/* 端点（用户维度，非管理员）。
// 中间件层（Authenticate）已在 router 注册时配置，handler 通过 middleware.CurrentUser 取当前用户。
type APIKeyHandler struct {
	svc *service.APIKeyService
}

// NewAPIKeyHandler 创建 APIKeyHandler。
func NewAPIKeyHandler(svc *service.APIKeyService) *APIKeyHandler {
	return &APIKeyHandler{svc: svc}
}

// CreateKey POST /api/auth/api-keys
// 创建成功返回明文（仅此一次）。
func (h *APIKeyHandler) CreateKey(c *gin.Context) {
	user := middleware.CurrentUser(c)
	if user == nil {
		c.JSON(http.StatusUnauthorized, dto.ErrorResponse{Error: "未登录"})
		return
	}
	var req dto.CreateAPIKeyRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, dto.ErrorResponse{Error: "请求参数无效"})
		return
	}
	result, err := h.svc.Create(c.Request.Context(), service.CreateAPIKeyInput{
		UserID:    user.ID,
		Name:      req.Name,
		Scopes:    req.Scopes,
		RateLimit: req.RateLimit,
	})
	if err != nil {
		writeAuthError(c, err)
		return
	}
	c.JSON(http.StatusCreated, dto.CreateAPIKeyResponse{
		ID:        result.ID,
		PlainText: result.PlainText,
		Name:      result.Name,
		KeyPrefix: result.KeyPrefix,
		Scopes:    result.Scopes,
		RateLimit: result.RateLimit,
		CreatedAt: result.CreatedAt,
	})
}

// ListKeys GET /api/auth/api-keys
func (h *APIKeyHandler) ListKeys(c *gin.Context) {
	user := middleware.CurrentUser(c)
	if user == nil {
		c.JSON(http.StatusUnauthorized, dto.ErrorResponse{Error: "未登录"})
		return
	}
	keys, err := h.svc.ListByUser(c.Request.Context(), user.ID)
	if err != nil {
		writeAuthError(c, err)
		return
	}
	resp := make([]dto.APIKeyResponse, 0, len(keys))
	for _, k := range keys {
		resp = append(resp, toAPIKeyResponse(k))
	}
	c.JSON(http.StatusOK, resp)
}

// DeleteKey DELETE /api/auth/api-keys/:id
// service 层校验归属防越权（非本人 key 返回 403）。
func (h *APIKeyHandler) DeleteKey(c *gin.Context) {
	user := middleware.CurrentUser(c)
	if user == nil {
		c.JSON(http.StatusUnauthorized, dto.ErrorResponse{Error: "未登录"})
		return
	}
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, dto.ErrorResponse{Error: "无效的 API Key ID"})
		return
	}
	if err := h.svc.Delete(c.Request.Context(), user.ID, id); err != nil {
		writeAuthError(c, err)
		return
	}
	c.JSON(http.StatusOK, dto.MessageResponse{Message: "删除成功"})
}

// ============ 辅助函数 ============

// parseUserID 从 :id 路径参数解析 int64。解析失败返回 (0, false)。
func parseUserID(c *gin.Context) (int64, bool) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		return 0, false
	}
	return id, true
}

// toAdminUserResponse 将 dao.User 转为 dto.AdminUserResponse（含全部字段）。
func toAdminUserResponse(u *dao.User) dto.AdminUserResponse {
	return dto.AdminUserResponse{
		ID:           u.ID,
		Username:     u.Username,
		Email:        u.Email,
		DisplayName:  u.DisplayName,
		Role:         u.Role,
		StorageLimit: u.StorageLimit,
		StorageUsed:  u.StorageUsed,
		IsActive:     u.IsActive,
		CreatedAt:    u.CreatedAt,
	}
}

// toAPIKeyResponse 将 dao.APIKey 转为 dto.APIKeyResponse（不含 KeyHash）。
func toAPIKeyResponse(k *dao.APIKey) dto.APIKeyResponse {
	return dto.APIKeyResponse{
		ID:         k.ID,
		Name:       k.Name,
		KeyPrefix:  k.KeyPrefix,
		Scopes:     k.Scopes,
		RateLimit:  k.RateLimit,
		IsActive:   k.IsActive,
		LastUsedAt: k.LastUsedAt,
		CreatedAt:  k.CreatedAt,
	}
}
