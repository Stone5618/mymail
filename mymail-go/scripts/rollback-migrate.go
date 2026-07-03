// rollback-migrate.go — 从备份恢复 MyMail 数据库（迁移回滚）。
//
// 用法：
//
//	go run scripts/rollback-migrate.go -db /path/to/mymail.db -backup /path/to/mymail-backup-YYYYMMDD-HHMMSS.db
//
// 功能：
//  1. 验证备份文件存在且有效
// 2. 关闭正在使用数据库的服务（提示，不自动执行）
// 3. 将当前数据库文件重命名为 .failed-migration 后缀
// 4. 从备份恢复数据库
// 5. 验证恢复后的数据库完整性
// 6. 清理 WAL/SHM 文件
//
// 注意：回滚前请先停止 MyMail 服务，避免数据库被占用。
//
//go:build ignore

package main

import (
	"database/sql"
	"flag"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"time"

	_ "modernc.org/sqlite"
)

var (
	dbPath     = flag.String("db", "", "当前数据库路径（必填）")
	backupPath = flag.String("backup", "", "备份文件路径（必填）")
	skipCheck  = flag.Bool("skip-check", false, "跳过完整性检查")
)

func main() {
	flag.Parse()
	log.SetFlags(log.LstdFlags | log.Lshortfile)

	if *dbPath == "" || *backupPath == "" {
		fmt.Fprintln(os.Stderr, "错误：请通过 -db 和 -backup 指定路径")
		flag.Usage()
		os.Exit(1)
	}

	absDB, _ := filepath.Abs(*dbPath)
	absBackup, _ := filepath.Abs(*backupPath)

	log.Printf("=== MyMail 数据库回滚工具 ===")
	log.Printf("当前数据库: %s", absDB)
	log.Printf("备份文件:   %s", absBackup)

	// 1. 验证备份文件
	if _, err := os.Stat(absBackup); os.IsNotExist(err) {
		log.Fatalf("备份文件不存在: %s", absBackup)
	}

	if !*skipCheck {
		log.Printf("[步骤 1/4] 验证备份文件完整性...")
		if err := verifyDatabase(absBackup); err != nil {
			log.Fatalf("备份文件无效: %v", err)
		}
		log.Printf("  ✓ 备份文件有效")
	} else {
		log.Printf("[步骤 1/4] 跳过完整性检查")
	}

	// 2. 提示停止服务
	log.Printf("[步骤 2/4] 请确保 MyMail 服务已停止！")
	log.Printf("  停止命令: docker-compose down 或 systemctl stop mymail")
	log.Printf("  按 Enter 继续，或 Ctrl+C 取消...")
	fmt.Scanln()

	// 3. 重命名当前数据库
	log.Printf("[步骤 3/4] 备份当前（迁移后的）数据库...")
	timestamp := time.Now().Format("20060102-150405")
	failedPath := absDB + ".failed-migration-" + timestamp

	if _, err := os.Stat(absDB); err == nil {
		if err := os.Rename(absDB, failedPath); err != nil {
			log.Fatalf("重命名当前数据库失败: %v", err)
		}
		log.Printf("  ✓ 当前数据库已重命名: %s", failedPath)

		// 同时重命名 WAL/SHM
		for _, suffix := range []string{"-wal", "-shm"} {
			oldPath := absDB + suffix
			if _, err := os.Stat(oldPath); err == nil {
				os.Rename(oldPath, failedPath+suffix)
			}
		}
	}

	// 4. 从备份恢复
	log.Printf("[步骤 4/4] 从备份恢复数据库...")
	if err := copyFile(absBackup, absDB); err != nil {
		log.Fatalf("恢复数据库失败: %v", err)
	}
	log.Printf("  ✓ 数据库已恢复")

	// 验证恢复后的数据库
	if !*skipCheck {
		if err := verifyDatabase(absDB); err != nil {
			log.Fatalf("恢复后的数据库无效: %v\n当前数据库已备份在: %s", err, failedPath)
		}
		log.Printf("  ✓ 恢复后的数据库完整性验证通过")
	}

	log.Printf("")
	log.Printf("=== 回滚完成 ===")
	log.Printf("数据库已恢复到备份状态: %s", absBackup)
	log.Printf("迁移后的数据库保留在: %s（确认无误后可手动删除）", failedPath)
	log.Printf("")
	log.Printf("下一步：重新启动 MyMail 服务")
}

// verifyDatabase 验证 SQLite 数据库完整性。
func verifyDatabase(dbPath string) error {
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		return fmt.Errorf("打开数据库失败: %w", err)
	}
	defer db.Close()

	var ok string
	if err := db.QueryRow("PRAGMA integrity_check").Scan(&ok); err != nil {
		return fmt.Errorf("完整性检查失败: %w", err)
	}
	if ok != "ok" {
		return fmt.Errorf("完整性检查返回: %s", ok)
	}

	// 检查关键表是否存在
	tables := []string{"users", "messages", "attachments", "send_log", "settings"}
	for _, t := range tables {
		var count int
		err := db.QueryRow(`SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name=?`, t).Scan(&count)
		if err != nil || count == 0 {
			return fmt.Errorf("缺少关键表: %s", t)
		}
	}

	return nil
}

// copyFile 复制文件。
func copyFile(src, dst string) error {
	srcFile, err := os.Open(src)
	if err != nil {
		return err
	}
	defer srcFile.Close()

	dstFile, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer dstFile.Close()

	buf := make([]byte, 32*1024)
	for {
		n, err := srcFile.Read(buf)
		if n > 0 {
			if _, werr := dstFile.Write(buf[:n]); werr != nil {
				return werr
			}
		}
		if err != nil {
			break
		}
	}
	return nil
}
