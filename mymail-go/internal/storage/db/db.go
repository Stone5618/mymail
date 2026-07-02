// Package db 提供 SQLite 数据库连接管理与迁移。
// 使用 modernc.org/sqlite（纯 Go，无 CGO 依赖）。
package db

import (
	"context"
	"database/sql"
	"fmt"
	"log/slog"
	"net/url"
	"os"
	"path/filepath"
	"time"

	// 注册 modernc.org/sqlite 驱动（纯 Go，无 CGO）
	_ "modernc.org/sqlite"
)

// DB 封装数据库连接，提供迁移与生命周期管理。
type DB struct {
	*sql.DB
}

// Open 打开 SQLite 数据库并设置 PRAGMA。
// PRAGMA 配置：
//   - journal_mode=WAL：并发读写性能更优
//   - foreign_keys=ON：启用外键约束
//   - busy_timeout=5000：写冲突时等待 5 秒
func Open(path string) (*DB, error) {
	// 确保数据目录存在
	if dir := filepath.Dir(path); dir != "" && dir != "." {
		if err := os.MkdirAll(dir, 0755); err != nil {
			return nil, fmt.Errorf("创建数据目录失败: %w", err)
		}
	}

	// 构建 DSN（含 PRAGMA 参数）
	dsn := buildDSN(path)

	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("打开数据库失败: %w", err)
	}

	// SQLite 写串行，连接池设为 1 即可（避免 write lock 冲突）
	// 读可以并发，但为简化，统一 1
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)
	db.SetConnMaxLifetime(0) // 长连接

	// 验证连接
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := db.PingContext(ctx); err != nil {
		db.Close()
		return nil, fmt.Errorf("数据库连通性检查失败: %w", err)
	}

	// 显式设置 PRAGMA（DSN 中已设，这里二次确认）
	pragmas := []string{
		"PRAGMA journal_mode=WAL",
		"PRAGMA foreign_keys=ON",
		"PRAGMA busy_timeout=5000",
		"PRAGMA synchronous=NORMAL",
	}
	for _, p := range pragmas {
		if _, err := db.ExecContext(ctx, p); err != nil {
			db.Close()
			return nil, fmt.Errorf("执行 %s 失败: %w", p, err)
		}
	}

	slog.Info("数据库已打开", "path", path, "journal_mode", "WAL")

	return &DB{db}, nil
}

// buildDSN 构建 SQLite 连接字符串，含 PRAGMA 参数。
func buildDSN(path string) string {
	// modernc.org/sqlite 支持查询参数形式的 PRAGMA
	params := url.Values{}
	params.Add("_pragma", "journal_mode(WAL)")
	params.Add("_pragma", "foreign_keys(ON)")
	params.Add("_pragma", "busy_timeout(5000)")
	params.Add("_pragma", "synchronous(NORMAL)")
	return path + "?" + params.Encode()
}

// Close 关闭数据库连接。
func (d *DB) Close() error {
	if d.DB == nil {
		return nil
	}
	slog.Info("关闭数据库连接")
	return d.DB.Close()
}
