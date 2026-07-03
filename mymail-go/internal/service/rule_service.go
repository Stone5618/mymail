// Package service 实现业务逻辑层。
// rule_service.go 实现规则 CRUD 业务：列表、创建、更新、删除。
//
// 设计：
//   - 规则为用户私有，更新/删除均校验归属（防越权）
//   - 不存在返回 AuthError(404)，归属不匹配返回 AuthError(403)
//   - Create/Update 直接复用 DAO 输入类型，不重复封装
package service

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/http"

	"github.com/mymail/mymail-go/internal/storage/dao"
)

// RuleService 规则 CRUD 业务层。
type RuleService struct {
	ruleDAO *dao.RuleDAO
}

// NewRuleService 创建 RuleService。
func NewRuleService(ruleDAO *dao.RuleDAO) *RuleService {
	return &RuleService{ruleDAO: ruleDAO}
}

// List 列出用户的所有规则。
func (s *RuleService) List(ctx context.Context, userID int64) ([]*dao.Rule, error) {
	return s.ruleDAO.FindByUserID(ctx, userID)
}

// Create 创建规则，返回规则 ID。
func (s *RuleService) Create(ctx context.Context, in dao.CreateRuleInput) (int64, error) {
	return s.ruleDAO.Create(ctx, in)
}

// Update 更新规则（校验存在 + 归属防越权）。
// 不存在返回 AuthError(404)，归属不匹配返回 AuthError(403)。
func (s *RuleService) Update(ctx context.Context, id, userID int64, in dao.UpdateRuleInput) error {
	r, err := s.ruleDAO.FindByID(ctx, id)
	if err != nil {
		// rule.go 的 FindByID 未单独处理 sql.ErrNoRows，需在此检测
		if errors.Is(err, sql.ErrNoRows) {
			return NewAuthError(http.StatusNotFound, "规则不存在")
		}
		return fmt.Errorf("查询规则失败: %w", err)
	}
	if r == nil {
		return NewAuthError(http.StatusNotFound, "规则不存在")
	}
	if r.UserID != userID {
		return NewAuthError(http.StatusForbidden, "无权操作此规则")
	}
	return s.ruleDAO.Update(ctx, id, in)
}

// Delete 删除规则（校验存在 + 归属防越权）。
// 不存在返回 AuthError(404)，归属不匹配返回 AuthError(403)。
func (s *RuleService) Delete(ctx context.Context, id, userID int64) error {
	r, err := s.ruleDAO.FindByID(ctx, id)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return NewAuthError(http.StatusNotFound, "规则不存在")
		}
		return fmt.Errorf("查询规则失败: %w", err)
	}
	if r == nil {
		return NewAuthError(http.StatusNotFound, "规则不存在")
	}
	if r.UserID != userID {
		return NewAuthError(http.StatusForbidden, "无权操作此规则")
	}
	return s.ruleDAO.Delete(ctx, id)
}
