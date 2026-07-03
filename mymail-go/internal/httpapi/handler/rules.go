// Package handler
// rules.go 实现用户邮件规则 CRUD HTTP 端点。
//
// 端点清单：
//   GET    /api/rules     - 列出当前用户的所有规则
//   POST   /api/rules     - 创建规则
//   PUT    /api/rules/:id - 更新规则（校验归属）
//   DELETE /api/rules/:id - 删除规则（校验归属）
//
// 认证：JWT（路由层配置 Authenticate 中间件），handler 通过 middleware.CurrentUser 取用户。
// 错误处理：service 层返回 *service.AuthError（404/403），由 writeAuthError 统一处理。
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

// RuleHandler 处理 /api/rules/* 端点（用户维度，JWT 认证）。
type RuleHandler struct {
	svc *service.RuleService
}

// NewRuleHandler 创建 RuleHandler。
func NewRuleHandler(svc *service.RuleService) *RuleHandler {
	return &RuleHandler{svc: svc}
}

// ============ DTO ============

// RuleConditionDTO 规则条件 DTO（与 dao.RuleCondition 字段一致）。
type RuleConditionDTO struct {
	Field string `json:"field"`
	Op    string `json:"op"`
	Value any    `json:"value"`
}

// RuleActionDTO 规则动作 DTO（与 dao.RuleAction 字段一致）。
type RuleActionDTO struct {
	Type    string `json:"type"`
	Folder  string `json:"folder,omitempty"`
	Address string `json:"address,omitempty"`
	Flag    string `json:"flag,omitempty"`
}

// RuleResponse 规则响应（snake_case，与表字段一致）。
type RuleResponse struct {
	ID         int64              `json:"id"`
	UserID     int64              `json:"user_id"`
	Name       string             `json:"name"`
	Priority   int                `json:"priority"`
	Conditions []RuleConditionDTO `json:"conditions"`
	Actions    []RuleActionDTO    `json:"actions"`
	IsActive   bool               `json:"is_active"`
	CreatedAt  string             `json:"created_at"`
}

// CreateRuleRequest 创建规则请求。
type CreateRuleRequest struct {
	Name       string             `json:"name"`
	Priority   int                `json:"priority"`
	Conditions []RuleConditionDTO `json:"conditions"`
	Actions    []RuleActionDTO    `json:"actions"`
}

// UpdateRuleRequest 更新规则请求。
// 所有字段为指针，nil 表示不更新。
type UpdateRuleRequest struct {
	Name       *string             `json:"name"`
	Priority   *int                `json:"priority"`
	Conditions *[]RuleConditionDTO `json:"conditions"`
	Actions    *[]RuleActionDTO    `json:"actions"`
	IsActive   *bool               `json:"is_active"`
}

// ============ 端点 ============

// List GET /api/rules → 当前用户的所有规则。
func (h *RuleHandler) List(c *gin.Context) {
	user := middleware.CurrentUser(c)
	if user == nil {
		c.JSON(http.StatusUnauthorized, dto.ErrorResponse{Error: "未登录"})
		return
	}

	rules, err := h.svc.List(c.Request.Context(), user.ID)
	if err != nil {
		writeAuthError(c, err)
		return
	}

	resp := make([]RuleResponse, 0, len(rules))
	for _, r := range rules {
		resp = append(resp, toRuleResponse(r))
	}
	c.JSON(http.StatusOK, resp)
}

// Create POST /api/rules → 创建规则。
func (h *RuleHandler) Create(c *gin.Context) {
	user := middleware.CurrentUser(c)
	if user == nil {
		c.JSON(http.StatusUnauthorized, dto.ErrorResponse{Error: "未登录"})
		return
	}

	var req CreateRuleRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, dto.ErrorResponse{Error: "请求格式错误"})
		return
	}

	id, err := h.svc.Create(c.Request.Context(), dao.CreateRuleInput{
		UserID:     user.ID,
		Name:       req.Name,
		Priority:   req.Priority,
		Conditions: toConditions(req.Conditions),
		Actions:    toActions(req.Actions),
	})
	if err != nil {
		writeAuthError(c, err)
		return
	}

	c.JSON(http.StatusCreated, RuleResponse{
		ID:         id,
		UserID:     user.ID,
		Name:       req.Name,
		Priority:   req.Priority,
		Conditions: req.Conditions,
		Actions:    req.Actions,
		IsActive:   true,
	})
}

// Update PUT /api/rules/:id → 更新规则（校验归属）。
func (h *RuleHandler) Update(c *gin.Context) {
	user := middleware.CurrentUser(c)
	if user == nil {
		c.JSON(http.StatusUnauthorized, dto.ErrorResponse{Error: "未登录"})
		return
	}

	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, dto.ErrorResponse{Error: "无效的规则 ID"})
		return
	}

	var req UpdateRuleRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, dto.ErrorResponse{Error: "请求格式错误"})
		return
	}

	in := dao.UpdateRuleInput{
		Name:     req.Name,
		Priority: req.Priority,
		IsActive: req.IsActive,
	}
	if req.Conditions != nil {
		cs := toConditions(*req.Conditions)
		in.Conditions = &cs
	}
	if req.Actions != nil {
		as := toActions(*req.Actions)
		in.Actions = &as
	}

	if err := h.svc.Update(c.Request.Context(), id, user.ID, in); err != nil {
		writeAuthError(c, err)
		return
	}

	c.JSON(http.StatusOK, dto.MessageResponse{Message: "更新成功"})
}

// Delete DELETE /api/rules/:id → 删除规则（校验归属）。
func (h *RuleHandler) Delete(c *gin.Context) {
	user := middleware.CurrentUser(c)
	if user == nil {
		c.JSON(http.StatusUnauthorized, dto.ErrorResponse{Error: "未登录"})
		return
	}

	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, dto.ErrorResponse{Error: "无效的规则 ID"})
		return
	}

	if err := h.svc.Delete(c.Request.Context(), id, user.ID); err != nil {
		writeAuthError(c, err)
		return
	}

	c.JSON(http.StatusOK, dto.MessageResponse{Message: "删除成功"})
}

// ============ 辅助函数 ============

// toRuleResponse 将 dao.Rule 转为 RuleResponse。
func toRuleResponse(r *dao.Rule) RuleResponse {
	return RuleResponse{
		ID:         r.ID,
		UserID:     r.UserID,
		Name:       r.Name,
		Priority:   r.Priority,
		Conditions: fromConditions(r.Conditions),
		Actions:    fromActions(r.Actions),
		IsActive:   r.IsActive,
		CreatedAt:  r.CreatedAt,
	}
}

// toConditions 将 DTO 切片转为 dao 结构体切片。
func toConditions(dtos []RuleConditionDTO) []dao.RuleCondition {
	out := make([]dao.RuleCondition, 0, len(dtos))
	for _, d := range dtos {
		out = append(out, dao.RuleCondition{Field: d.Field, Op: d.Op, Value: d.Value})
	}
	return out
}

// toActions 将 DTO 切片转为 dao 结构体切片。
func toActions(dtos []RuleActionDTO) []dao.RuleAction {
	out := make([]dao.RuleAction, 0, len(dtos))
	for _, d := range dtos {
		out = append(out, dao.RuleAction{Type: d.Type, Folder: d.Folder, Address: d.Address, Flag: d.Flag})
	}
	return out
}

// fromConditions 将 dao 结构体切片转为 DTO 切片。
func fromConditions(conds []dao.RuleCondition) []RuleConditionDTO {
	out := make([]RuleConditionDTO, 0, len(conds))
	for _, c := range conds {
		out = append(out, RuleConditionDTO{Field: c.Field, Op: c.Op, Value: c.Value})
	}
	return out
}

// fromActions 将 dao 结构体切片转为 DTO 切片。
func fromActions(acts []dao.RuleAction) []RuleActionDTO {
	out := make([]RuleActionDTO, 0, len(acts))
	for _, a := range acts {
		out = append(out, RuleActionDTO{Type: a.Type, Folder: a.Folder, Address: a.Address, Flag: a.Flag})
	}
	return out
}
