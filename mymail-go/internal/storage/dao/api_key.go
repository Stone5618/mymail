// api_key.go 实现 APIKeyDAO，对应 api_keys 表。
//
// 用途：
//   - API Key CRUD（用户维度的发信凭证）
//   - 认证时通过 key_prefix 索引 O(1) 定位候选行（P1-3 修复）
//
// P1-3 修复：
//   - 原 Node.js api-key-dao.js 用 findByKeyHash 全表遍历 + bcrypt.compare（O(n)）
//   - Go 版改用 FindByPrefix（WHERE key_prefix=? 命中索引），再 bcrypt 比对
package dao

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/mymail/mymail-go/internal/storage/db"
)

// APIKey 表示 api_keys 表的完整行。
type APIKey struct {
	ID         int64    `json:"id"`
	UserID     int64    `json:"user_id"`
	Name       string   `json:"name"`
	KeyHash    string   `json:"-"`          // 不暴露哈希
	KeyPrefix  string   `json:"key_prefix"` // 用于展示（如 mk_a1b2c3d4e5）
	Scopes     []string `json:"scopes"`
	RateLimit  int      `json:"rate_limit"` // 每分钟最大请求数
	IsActive   bool     `json:"is_active"`
	LastUsedAt *string  `json:"last_used_at"` // 可空
	CreatedAt  string   `json:"created_at"`
}

// APIKeyDAO 封装 api_keys 表的所有数据库操作。
type APIKeyDAO struct {
	db *db.DB
}

// NewAPIKeyDAO 创建 APIKeyDAO。
func NewAPIKeyDAO(database *db.DB) *APIKeyDAO {
	return &APIKeyDAO{db: database}
}

// CreateAPIKeyInput 创建 API Key 的输入参数。
type CreateAPIKeyInput struct {
	UserID    int64
	Name      string
	KeyHash   string
	KeyPrefix string
	Scopes    []string // 默认 ["send"]
	RateLimit int      // 默认 60，0 时使用 60
}

// Create 插入新 API Key，返回 ID。
func (d *APIKeyDAO) Create(ctx context.Context, in CreateAPIKeyInput) (int64, error) {
	scopes := in.Scopes
	if len(scopes) == 0 {
		scopes = []string{"send"}
	}
	rateLimit := in.RateLimit
	if rateLimit == 0 {
		rateLimit = 60
	}
	scopesJSON, err := json.Marshal(scopes)
	if err != nil {
		return 0, fmt.Errorf("序列化 scopes 失败: %w", err)
	}

	res, err := d.db.ExecContext(ctx,
		`INSERT INTO api_keys (user_id, name, key_hash, key_prefix, scopes, rate_limit, is_active)
		 VALUES (?, ?, ?, ?, ?, ?, 1)`,
		in.UserID, in.Name, in.KeyHash, in.KeyPrefix, string(scopesJSON), rateLimit,
	)
	if err != nil {
		return 0, fmt.Errorf("插入 API Key 失败: %w", err)
	}
	id, err := res.LastInsertId()
	if err != nil {
		return 0, fmt.Errorf("获取 API Key ID 失败: %w", err)
	}
	return id, nil
}

// FindByID 按 ID 查询 API Key。
func (d *APIKeyDAO) FindByID(ctx context.Context, id int64) (*APIKey, error) {
	row := d.db.QueryRowContext(ctx,
		`SELECT id, user_id, name, key_hash, key_prefix, scopes, rate_limit, is_active, last_used_at, created_at
		 FROM api_keys WHERE id = ?`, id)
	return scanAPIKey(row)
}

// FindByPrefix 通过 key_prefix 查找活跃的 API Key（P1-3 修复：索引查找）。
// 返回候选列表（通常 0 或 1 条），调用方需对候选行逐一 bcrypt 比对。
func (d *APIKeyDAO) FindByPrefix(ctx context.Context, prefix string) ([]*APIKey, error) {
	rows, err := d.db.QueryContext(ctx,
		`SELECT id, user_id, name, key_hash, key_prefix, scopes, rate_limit, is_active, last_used_at, created_at
		 FROM api_keys WHERE key_prefix = ? AND is_active = 1`,
		prefix)
	if err != nil {
		return nil, fmt.Errorf("按 prefix 查询 API Key 失败: %w", err)
	}
	defer rows.Close()
	return scanAPIKeys(rows)
}

// FindByUserID 查询用户的所有 API Key（按创建时间降序）。
func (d *APIKeyDAO) FindByUserID(ctx context.Context, userID int64) ([]*APIKey, error) {
	rows, err := d.db.QueryContext(ctx,
		`SELECT id, user_id, name, key_hash, key_prefix, scopes, rate_limit, is_active, last_used_at, created_at
		 FROM api_keys WHERE user_id = ?
		 ORDER BY created_at DESC`, userID)
	if err != nil {
		return nil, fmt.Errorf("查询用户 API Key 失败: %w", err)
	}
	defer rows.Close()
	return scanAPIKeys(rows)
}

// UpdateLastUsed 更新最后使用时间。
func (d *APIKeyDAO) UpdateLastUsed(ctx context.Context, id int64) error {
	_, err := d.db.ExecContext(ctx,
		`UPDATE api_keys SET last_used_at = CURRENT_TIMESTAMP WHERE id = ?`, id)
	if err != nil {
		return fmt.Errorf("更新 last_used_at 失败: %w", err)
	}
	return nil
}

// Deactivate 停用 API Key（软删除）。
func (d *APIKeyDAO) Deactivate(ctx context.Context, id int64) error {
	_, err := d.db.ExecContext(ctx,
		`UPDATE api_keys SET is_active = 0 WHERE id = ?`, id)
	if err != nil {
		return fmt.Errorf("停用 API Key 失败: %w", err)
	}
	return nil
}

// Delete 删除 API Key（硬删除）。
func (d *APIKeyDAO) Delete(ctx context.Context, id int64) error {
	_, err := d.db.ExecContext(ctx, `DELETE FROM api_keys WHERE id = ?`, id)
	if err != nil {
		return fmt.Errorf("删除 API Key 失败: %w", err)
	}
	return nil
}

// CountByUserID 统计用户的 API Key 数量（用于限制每用户最多 N 个）。
func (d *APIKeyDAO) CountByUserID(ctx context.Context, userID int64) (int64, error) {
	var count int64
	err := d.db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM api_keys WHERE user_id = ?`, userID).Scan(&count)
	if err != nil {
		return 0, fmt.Errorf("统计 API Key 失败: %w", err)
	}
	return count, nil
}

// ============ 内部辅助 ============

// apiKeyScanner 抽象 *sql.Row 和 *sql.Rows 的 Scan 方法。
type apiKeyScanner interface {
	Scan(dest ...any) error
}

// scanAPIKey 扫描单行。
func scanAPIKey(s apiKeyScanner) (*APIKey, error) {
	var k APIKey
	var (
		scopesJSON string
		isActive   int
		lastUsed   *string
	)
	if err := s.Scan(&k.ID, &k.UserID, &k.Name, &k.KeyHash, &k.KeyPrefix,
		&scopesJSON, &k.RateLimit, &isActive, &lastUsed, &k.CreatedAt); err != nil {
		return nil, fmt.Errorf("扫描 API Key 行失败: %w", err)
	}
	k.IsActive = isActive == 1
	k.LastUsedAt = lastUsed
	if err := json.Unmarshal([]byte(scopesJSON), &k.Scopes); err != nil {
		return nil, fmt.Errorf("解析 scopes JSON 失败: %w", err)
	}
	return &k, nil
}

// scanAPIKeys 扫描多行。
func scanAPIKeys(rows interface {
	Next() bool
}) ([]*APIKey, error) {
	var keys []*APIKey
	for {
		if !rows.Next() {
			break
		}
		k, err := scanAPIKey(rows.(apiKeyScanner))
		if err != nil {
			return nil, err
		}
		keys = append(keys, k)
	}
	return keys, nil
}
