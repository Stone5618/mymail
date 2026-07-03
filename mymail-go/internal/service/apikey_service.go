// Package service 实现业务逻辑层。
// apikey_service.go 实现 API Key 业务：创建、列表、删除、校验。
//
// 设计：
//   - 创建时明文仅返回一次（与原 Node.js 一致）
//   - 每用户最大 key 数限制（默认 10）
//   - 校验通过 key_prefix 索引 O(1) 定位候选行，再 bcrypt 比对（P1-3 修复）
//   - 删除校验归属（防越权）
package service

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/http"

	"github.com/mymail/mymail-go/internal/audit"
	"github.com/mymail/mymail-go/internal/crypto"
	"github.com/mymail/mymail-go/internal/storage/dao"
)

// APIKeyService API Key 业务层。
type APIKeyService struct {
	apiKeyDAO  *dao.APIKeyDAO
	audit      *audit.Logger
	maxPerUser int // 每用户最大 key 数，默认 10
}

// CreateAPIKeyInput 创建 API Key 的输入。
type CreateAPIKeyInput struct {
	UserID    int64
	Name      string
	Scopes    []string // nil 时默认 ["send"]
	RateLimit int      // 0 时默认 60
}

// CreateAPIKeyResult 创建结果（明文仅返回一次）。
type CreateAPIKeyResult struct {
	ID        int64
	PlainText string
	Name      string
	KeyPrefix string
	Scopes    []string
	RateLimit int
	CreatedAt string
}

// NewAPIKeyService 创建 APIKeyService。
// maxPerUser<=0 时默认 10。
func NewAPIKeyService(apiKeyDAO *dao.APIKeyDAO, auditLogger *audit.Logger, maxPerUser int) *APIKeyService {
	if maxPerUser <= 0 {
		maxPerUser = 10
	}
	return &APIKeyService{
		apiKeyDAO:  apiKeyDAO,
		audit:      auditLogger,
		maxPerUser: maxPerUser,
	}
}

// Create 创建 API Key。
// 业务规则：
//  1. 校验 Name 非空
//  2. 每用户 key 数量不超过 maxPerUser
//  3. crypto.GenerateAPIKey 生成明文 + 哈希 + 前缀
//  4. 持久化到 DB
//  5. 审计记录（action="api_key.create"）
//  6. 返回 CreateAPIKeyResult（明文仅此一次返回）
func (s *APIKeyService) Create(ctx context.Context, in CreateAPIKeyInput) (*CreateAPIKeyResult, error) {
	// 1. 校验 Name
	if in.Name == "" {
		return nil, NewAuthError(http.StatusBadRequest, "API Key 名称不能为空")
	}

	// 2. 数量上限校验
	count, err := s.apiKeyDAO.CountByUserID(ctx, in.UserID)
	if err != nil {
		return nil, fmt.Errorf("统计 API Key 失败: %w", err)
	}
	if count >= int64(s.maxPerUser) {
		return nil, NewAuthError(http.StatusBadRequest, "API Key 数量已达上限")
	}

	// 3. 生成 key
	keyResult, err := crypto.GenerateAPIKey()
	if err != nil {
		return nil, fmt.Errorf("生成 API Key 失败: %w", err)
	}

	// 4. 持久化（Scopes 为 nil 时由 DAO 默认 ["send"]，RateLimit 为 0 时默认 60）
	id, err := s.apiKeyDAO.Create(ctx, dao.CreateAPIKeyInput{
		UserID:    in.UserID,
		Name:      in.Name,
		KeyHash:   keyResult.KeyHash,
		KeyPrefix: keyResult.KeyPrefix,
		Scopes:    in.Scopes,
		RateLimit: in.RateLimit,
	})
	if err != nil {
		return nil, fmt.Errorf("持久化 API Key 失败: %w", err)
	}

	// 5. 查询完整记录（获取 CreatedAt 与默认值后的 Scopes/RateLimit）
	created, err := s.apiKeyDAO.FindByID(ctx, id)
	if err != nil {
		return nil, fmt.Errorf("查询新 API Key 失败: %w", err)
	}
	if created == nil {
		return nil, fmt.Errorf("新 API Key 查询返回 nil")
	}

	// 6. 审计
	if s.audit != nil {
		s.audit.Record(ctx, audit.Entry{
			ActorType:    audit.ActorUser,
			ActorID:      &in.UserID,
			Action:       "api_key.create",
			ResourceType: "api_key",
			ResourceID:   fmt.Sprintf("%d", id),
			Result:       audit.ResultSuccess,
		})
	}

	return &CreateAPIKeyResult{
		ID:        id,
		PlainText: keyResult.PlainText,
		Name:      created.Name,
		KeyPrefix: created.KeyPrefix,
		Scopes:    created.Scopes,
		RateLimit: created.RateLimit,
		CreatedAt: created.CreatedAt,
	}, nil
}

// ListByUser 列出用户的所有 API Key。
func (s *APIKeyService) ListByUser(ctx context.Context, userID int64) ([]*dao.APIKey, error) {
	return s.apiKeyDAO.FindByUserID(ctx, userID)
}

// Delete 删除 API Key（校验归属防越权）。
// 不存在返回 AuthError(404)，归属不匹配返回 AuthError(403)。
func (s *APIKeyService) Delete(ctx context.Context, userID, keyID int64) error {
	key, err := s.apiKeyDAO.FindByID(ctx, keyID)
	if err != nil {
		// api_key.go 的 FindByID 未单独处理 sql.ErrNoRows，需在此检测
		if errors.Is(err, sql.ErrNoRows) {
			return NewAuthError(http.StatusNotFound, "API Key 不存在")
		}
		return fmt.Errorf("查询 API Key 失败: %w", err)
	}
	if key == nil {
		return NewAuthError(http.StatusNotFound, "API Key 不存在")
	}
	if key.UserID != userID {
		return NewAuthError(http.StatusForbidden, "无权操作此 API Key")
	}

	if err := s.apiKeyDAO.Delete(ctx, keyID); err != nil {
		return fmt.Errorf("删除 API Key 失败: %w", err)
	}

	if s.audit != nil {
		s.audit.Record(ctx, audit.Entry{
			ActorType:    audit.ActorUser,
			ActorID:      &userID,
			Action:       "api_key.delete",
			ResourceType: "api_key",
			ResourceID:   fmt.Sprintf("%d", keyID),
			Result:       audit.ResultSuccess,
		})
	}
	return nil
}

// Verify 校验明文 API Key，返回匹配的 APIKey 记录。
// 流程（P1-3 修复）：
//  1. crypto.IsAPIKeyFormat 校验格式
//  2. crypto.ExtractKeyPrefix 提取前缀
//  3. apiKeyDAO.FindByPrefix 索引查找候选行
//  4. 遍历候选行 crypto.VerifyAPIKey 比对
//  5. 匹配后更新 LastUsed 并返回
//  6. 无匹配返回 AuthError(401, "无效的 API Key")
func (s *APIKeyService) Verify(ctx context.Context, plaintext string) (*dao.APIKey, error) {
	// 1. 格式校验
	if !crypto.IsAPIKeyFormat(plaintext) {
		return nil, fmt.Errorf("API Key 格式错误")
	}

	// 2. 提取前缀
	prefix := crypto.ExtractKeyPrefix(plaintext)

	// 3. 索引查找候选行
	candidates, err := s.apiKeyDAO.FindByPrefix(ctx, prefix)
	if err != nil {
		return nil, fmt.Errorf("查询 API Key 失败: %w", err)
	}

	// 4. 遍历候选行比对
	for _, k := range candidates {
		if crypto.VerifyAPIKey(k.KeyHash, plaintext) == nil {
			// 5. 匹配，更新 LastUsed（忽略错误，不影响校验结果）
			_ = s.apiKeyDAO.UpdateLastUsed(ctx, k.ID)
			return k, nil
		}
	}

	// 6. 无匹配
	return nil, NewAuthError(http.StatusUnauthorized, "无效的 API Key")
}
