// settings.go 实现 SettingsDAO，对应 settings 表。
//
// 用途：
//   - 管理员全局配置（key-value），如域名、SMTP 端口、特性开关等
//   - 管理后台 /api/admin/settings 读写
package dao

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/mymail/mymail-go/internal/storage/db"
)

// Setting 表示 settings 表的行。
type Setting struct {
	Key       string `json:"key"`
	Value     string `json:"value"`
	UpdatedAt string `json:"updated_at"`
}

// SettingsDAO 封装 settings 表操作。
type SettingsDAO struct {
	db *db.DB
}

// NewSettingsDAO 创建 SettingsDAO。
func NewSettingsDAO(database *db.DB) *SettingsDAO {
	return &SettingsDAO{db: database}
}

// GetAll 查询所有设置项（按 key 升序）。
func (d *SettingsDAO) GetAll(ctx context.Context) ([]*Setting, error) {
	rows, err := d.db.QueryContext(ctx,
		`SELECT key, value, updated_at FROM settings ORDER BY key`)
	if err != nil {
		return nil, fmt.Errorf("查询所有设置失败: %w", err)
	}
	defer rows.Close()

	var settings []*Setting
	for {
		if !rows.Next() {
			break
		}
		var s Setting
		if err := rows.Scan(&s.Key, &s.Value, &s.UpdatedAt); err != nil {
			return nil, fmt.Errorf("扫描设置行失败: %w", err)
		}
		settings = append(settings, &s)
	}
	return settings, nil
}

// Get 按 key 查询单个设置。
// 返回 (nil, nil) 表示不存在。
func (d *SettingsDAO) Get(ctx context.Context, key string) (*Setting, error) {
	var s Setting
	err := d.db.QueryRowContext(ctx,
		`SELECT key, value, updated_at FROM settings WHERE key = ?`, key).
		Scan(&s.Key, &s.Value, &s.UpdatedAt)
	if err != nil && errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("查询设置失败: %w", err)
	}
	return &s, nil
}

// Set 设置键值（UPSERT：存在则更新，不存在则插入）。
func (d *SettingsDAO) Set(ctx context.Context, key, value string) error {
	_, err := d.db.ExecContext(ctx,
		`INSERT INTO settings (key, value, updated_at)
		 VALUES (?, ?, CURRENT_TIMESTAMP)
		 ON CONFLICT(key) DO UPDATE SET value = excluded.value, updated_at = CURRENT_TIMESTAMP`,
		key, value)
	if err != nil {
		return fmt.Errorf("更新设置失败: %w", err)
	}
	return nil
}

// Delete 删除设置项。
func (d *SettingsDAO) Delete(ctx context.Context, key string) error {
	_, err := d.db.ExecContext(ctx, `DELETE FROM settings WHERE key = ?`, key)
	if err != nil {
		return fmt.Errorf("删除设置失败: %w", err)
	}
	return nil
}
