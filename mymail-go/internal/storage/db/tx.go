// Package db
// tx.go 提供事务辅助函数。
// 修复 P1-1：DAO 层无事务保护，关键多步操作必须用 RunInTransaction 包裹。
package db

import (
	"context"
	"database/sql"
	"fmt"
)

// RunInTransaction 在事务中执行 fn，出错自动回滚，成功自动提交。
// 若 fn 返回 error，事务回滚并返回该 error。
// 若事务提交失败，返回提交错误。
//
// 用法：
//
//	err := db.RunInTransaction(ctx, func(tx *sql.Tx) error {
//	    if _, err := tx.ExecContext(ctx, "UPDATE ..."); err != nil {
//	        return err
//	    }
//	    return nil
//	})
func (d *DB) RunInTransaction(ctx context.Context, fn func(tx *sql.Tx) error) error {
	if d.DB == nil {
		return fmt.Errorf("数据库未初始化")
	}
	tx, err := d.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("开启事务失败: %w", err)
	}
	defer func() {
		// panic 时强制回滚
		if r := recover(); r != nil {
			_ = tx.Rollback()
			panic(r)
		}
	}()
	if err := fn(tx); err != nil {
		if rbErr := tx.Rollback(); rbErr != nil {
			return fmt.Errorf("事务执行失败: %w（回滚失败: %v）", err, rbErr)
		}
		return err
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("提交事务失败: %w", err)
	}
	return nil
}
