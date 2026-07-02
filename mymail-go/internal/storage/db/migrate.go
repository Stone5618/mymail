// Package db
// migrate.go 实现自定义数据库迁移器。
// 修复 P0-9：废弃 init-db.js，统一用 SQL 迁移文件管理 schema。
//
// 迁移文件命名约定：
//   NNN_description.up.sql   - 升级
//   NNN_description.down.sql - 回滚
//
// 迁移记录表 schema_migrations 跟踪当前版本。
package db

import (
	"context"
	"embed"
	"fmt"
	"log/slog"
	"regexp"
	"sort"
	"strconv"
	"time"
)

//go:embed migrations/*.sql
var migrationsFS embed.FS

// migrationFile 表示一个迁移文件。
type migrationFile struct {
	version int
	name    string
	path    string
	content string
}

// Migrate 执行所有未应用的迁移。
// 实现幂等：已应用的迁移不会重复执行。
func (d *DB) Migrate() error {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	// 1. 确保 schema_migrations 表存在
	if _, err := d.ExecContext(ctx, `
		CREATE TABLE IF NOT EXISTS schema_migrations (
			version INTEGER PRIMARY KEY,
			applied_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
		)
	`); err != nil {
		return fmt.Errorf("创建 schema_migrations 表失败: %w", err)
	}

	// 2. 读取已应用的版本
	applied, err := d.getAppliedVersions(ctx)
	if err != nil {
		return fmt.Errorf("读取已应用版本失败: %w", err)
	}

	// 3. 读取所有 up 迁移文件
	migrations, err := loadMigrations("up")
	if err != nil {
		return fmt.Errorf("加载迁移文件失败: %w", err)
	}

	// 4. 按版本号顺序执行未应用的迁移
	for _, m := range migrations {
		if applied[m.version] {
			continue
		}

		slog.Info("执行迁移", "version", m.version, "name", m.name)

		// 用事务保证迁移原子性
		tx, err := d.BeginTx(ctx, nil)
		if err != nil {
			return fmt.Errorf("开始迁移事务失败 (v%d): %w", m.version, err)
		}

		if _, err := tx.ExecContext(ctx, m.content); err != nil {
			tx.Rollback()
			return fmt.Errorf("执行迁移 SQL 失败 (v%d): %w", m.version, err)
		}

		if _, err := tx.ExecContext(ctx,
			`INSERT INTO schema_migrations (version) VALUES (?)`,
			m.version,
		); err != nil {
			tx.Rollback()
			return fmt.Errorf("记录迁移版本失败 (v%d): %w", m.version, err)
		}

		if err := tx.Commit(); err != nil {
			return fmt.Errorf("提交迁移事务失败 (v%d): %w", m.version, err)
		}

		slog.Info("迁移完成", "version", m.version, "name", m.name)
	}

	return nil
}

// getAppliedVersions 返回已应用的迁移版本集合。
func (d *DB) getAppliedVersions(ctx context.Context) (map[int]bool, error) {
	rows, err := d.QueryContext(ctx, `SELECT version FROM schema_migrations`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	applied := make(map[int]bool)
	for rows.Next() {
		var v int
		if err := rows.Scan(&v); err != nil {
			return nil, err
		}
		applied[v] = true
	}
	return applied, rows.Err()
}

// loadMigrations 从嵌入文件系统加载指定方向（up/down）的迁移文件。
func loadMigrations(direction string) ([]migrationFile, error) {
	entries, err := migrationsFS.ReadDir("migrations")
	if err != nil {
		return nil, err
	}

	// 匹配 NNN_name.direction.sql
	pattern := regexp.MustCompile(`^(\d+)_(.+)\.` + direction + `\.sql$`)

	var files []migrationFile
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		matches := pattern.FindStringSubmatch(entry.Name())
		if matches == nil {
			continue
		}

		version, err := strconv.Atoi(matches[1])
		if err != nil {
			continue
		}

		content, err := migrationsFS.ReadFile("migrations/" + entry.Name())
		if err != nil {
			return nil, fmt.Errorf("读取迁移文件 %s 失败: %w", entry.Name(), err)
		}

		files = append(files, migrationFile{
			version: version,
			name:    matches[2],
			path:    "migrations/" + entry.Name(),
			content: string(content),
		})
	}

	// 按版本号升序排序
	sort.Slice(files, func(i, j int) bool {
		return files[i].version < files[j].version
	})

	return files, nil
}
