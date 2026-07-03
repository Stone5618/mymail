// migrate-data.go — 从 Node.js MyMail 数据库迁移到 Go 版本。
//
// 用法：
//
//	go run scripts/migrate-data.go -db /path/to/mymail.db [-backup /path/to/backup.db]
//
// 功能：
//  1. 自动备份数据库
//  2. 添加 Go 版本新增的列（body_html_raw, spam_score, spam_reasons, key_prefix, is_default_password）
//  3. 复制 body_html → body_html_raw（保留原始 HTML）
//  4. 用 bluemonday 净化 body_html（P0-4 XSS 修复）
//  5. 邮箱统一小写化（与 Go 版 SanitizeEmail 一致）
//  6. spam_log 列名变更（sender_ip→ip, sender_addr→sender, recipient_addr→recipient, spam_score→score）
//  7. 创建 Go 版新增表（audit_log, greylist, mail_queue）
//  8. 补全缺失索引
//  9. API Key key_prefix 不可恢复（标记为 inactive，需重建）
//
// 注意：此脚本幂等——重复运行不会损坏数据。
//
//go:build ignore

package main

import (
	"context"
	"database/sql"
	"flag"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"time"

	"net/url"

	"github.com/mymail/mymail-go/internal/sanitize"

	_ "modernc.org/sqlite"
)

var (
	dbPath    = flag.String("db", "", "Node.js MyMail 数据库路径（必填）")
	backupDir = flag.String("backup", "", "备份目录（默认数据库同目录）")
	dryRun    = flag.Bool("dry-run", false, "仅检查不执行变更")
)

func main() {
	flag.Parse()
	log.SetFlags(log.LstdFlags | log.Lshortfile)

	if *dbPath == "" {
		fmt.Fprintln(os.Stderr, "错误：请通过 -db 指定数据库路径")
		flag.Usage()
		os.Exit(1)
	}

	// 1. 检查数据库文件存在
	absPath, err := filepath.Abs(*dbPath)
	if err != nil {
		log.Fatalf("解析路径失败: %v", err)
	}
	if _, err := os.Stat(absPath); os.IsNotExist(err) {
		log.Fatalf("数据库文件不存在: %s", absPath)
	}

	log.Printf("=== MyMail 数据迁移工具 ===")
	log.Printf("数据库: %s", absPath)
	log.Printf("Dry-run: %v", *dryRun)

	// 2. 备份数据库
	backupPath := ""
	if !*dryRun {
		backupPath = backupDatabase(absPath)
		log.Printf("备份已创建: %s", backupPath)
	}

	// 3. 打开数据库
	params := url.Values{}
	params.Add("_pragma", "journal_mode(WAL)")
	params.Add("_pragma", "busy_timeout(5000)")
	dsn := absPath + "?" + params.Encode()
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		log.Fatalf("打开数据库失败: %v", err)
	}
	defer db.Close()

	ctx := context.Background()
	stats := &migrationStats{}

	// 4. 执行迁移步骤
	if *dryRun {
		log.Printf("--- Dry-run 模式：仅检查，不执行变更 ---")
		checkSchema(ctx, db, stats)
		stats.print()
		return
	}

	// 步骤 1：添加缺失列
	log.Printf("[步骤 1/7] 添加缺失列...")
	addMissingColumns(ctx, db, stats)

	// 步骤 2：复制 body_html → body_html_raw
	log.Printf("[步骤 2/7] 复制 body_html → body_html_raw...")
	copyBodyHTMLRaw(ctx, db, stats)

	// 步骤 3：净化 body_html（XSS 修复）
	log.Printf("[步骤 3/7] 净化 body_html（bluemonday UGCPolicy）...")
	sanitizeBodyHTML(ctx, db, stats)

	// 步骤 4：邮箱小写化
	log.Printf("[步骤 4/7] 邮箱统一小写化...")
	normalizeEmails(ctx, db, stats)

	// 步骤 5：spam_log 列名变更
	log.Printf("[步骤 5/7] 迁移 spam_log 列名...")
	migrateSpamLog(ctx, db, stats)

	// 步骤 6：创建新表
	log.Printf("[步骤 6/7] 创建 Go 版新增表...")
	createNewTables(ctx, db, stats)

	// 步骤 7：补全索引 + 处理 API Key
	log.Printf("[步骤 7/7] 补全索引 + 标记旧 API Key...")
	addIndexesAndHandleAPIKeys(ctx, db, stats)

	// 打印报告
	stats.print()

	log.Printf("=== 迁移完成 ===")
	log.Printf("备份文件: %s", backupPath)
	log.Printf("如有问题，可用备份恢复：go run scripts/rollback-migrate.go -backup %s -db %s", backupPath, absPath)

	// API Key 警告
	if stats.apiKeysMarkedInactive > 0 {
		log.Printf("")
		log.Printf("⚠️  警告：%d 个 API Key 因 key_prefix 不可恢复已被标记为 inactive。", stats.apiKeysMarkedInactive)
		log.Printf("   用户需登录后重新创建 API Key。")
	}
}

// migrationStats 迁移统计。
type migrationStats struct {
	columnsAdded          int
	bodyHTMLRawCopied     int
	bodyHTMLSanitized     int
	emailsNormalized      int
	spamLogMigrated       int
	tablesCreated         int
	indexesCreated        int
	apiKeysMarkedInactive int
	errors                []string
}

func (s *migrationStats) print() {
	log.Printf("")
	log.Printf("=== 迁移统计 ===")
	log.Printf("  新增列:           %d", s.columnsAdded)
	log.Printf("  body_html_raw 复制: %d", s.bodyHTMLRawCopied)
	log.Printf("  body_html 净化:   %d", s.bodyHTMLSanitized)
	log.Printf("  邮箱小写化:       %d", s.emailsNormalized)
	log.Printf("  spam_log 迁移:    %d", s.spamLogMigrated)
	log.Printf("  新建表:           %d", s.tablesCreated)
	log.Printf("  新建索引:         %d", s.indexesCreated)
	log.Printf("  API Key 标记失效: %d", s.apiKeysMarkedInactive)
	if len(s.errors) > 0 {
		log.Printf("  错误: %d", len(s.errors))
		for _, e := range s.errors {
			log.Printf("    - %s", e)
		}
	}
}

// backupDatabase 备份数据库文件。
func backupDatabase(dbPath string) string {
	dir := filepath.Dir(dbPath)
	if *backupDir != "" {
		dir = *backupDir
	}
	timestamp := time.Now().Format("20060102-150405")
	backupPath := filepath.Join(dir, fmt.Sprintf("mymail-backup-%s.db", timestamp))

	src, err := os.Open(dbPath)
	if err != nil {
		log.Fatalf("打开源数据库失败: %v", err)
	}
	defer src.Close()

	dst, err := os.Create(backupPath)
	if err != nil {
		log.Fatalf("创建备份文件失败: %v", err)
	}
	defer dst.Close()

	if _, err := copyFile(src, dst); err != nil {
		log.Fatalf("复制数据库失败: %v", err)
	}

	// 同时备份 WAL 和 SHM 文件（如果存在）
	for _, suffix := range []string{"-wal", "-shm"} {
		srcPath := dbPath + suffix
		if _, err := os.Stat(srcPath); err == nil {
			dstPath := backupPath + suffix
			s, _ := os.Open(srcPath)
			d, _ := os.Create(dstPath)
			copyFile(s, d)
			s.Close()
			d.Close()
		}
	}

	return backupPath
}

func copyFile(src *os.File, dst *os.File) (int64, error) {
	buf := make([]byte, 32*1024)
	var total int64
	for {
		n, err := src.Read(buf)
		if n > 0 {
			if _, werr := dst.Write(buf[:n]); werr != nil {
				return total, werr
			}
			total += int64(n)
		}
		if err != nil {
			break
		}
	}
	return total, nil
}

// columnExists 检查列是否存在。
func columnExists(ctx context.Context, db *sql.DB, table, column string) bool {
	q := fmt.Sprintf(`SELECT COUNT(*) FROM pragma_table_info('%s') WHERE name = ?`, table)
	var count int
	if err := db.QueryRowContext(ctx, q, column).Scan(&count); err != nil {
		return false
	}
	return count > 0
}

// tableExists 检查表是否存在。
func tableExists(ctx context.Context, db *sql.DB, table string) bool {
	var count int
	err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name=?`, table).Scan(&count)
	if err != nil {
		return false
	}
	return count > 0
}

// addMissingColumns 添加 Go 版新增的列。
func addMissingColumns(ctx context.Context, db *sql.DB, stats *migrationStats) {
	type colDef struct {
		table  string
		column string
		ddl    string
	}
	columns := []colDef{
		{"messages", "body_html_raw", "TEXT"},
		{"messages", "spam_score", "INTEGER NOT NULL DEFAULT 0"},
		{"messages", "spam_reasons", "TEXT"},
		{"users", "is_default_password", "INTEGER NOT NULL DEFAULT 0"},
		{"api_keys", "key_prefix", "TEXT NOT NULL DEFAULT ''"},
	}

	for _, c := range columns {
		if columnExists(ctx, db, c.table, c.column) {
			continue
		}
		q := fmt.Sprintf("ALTER TABLE %s ADD COLUMN %s %s", c.table, c.column, c.ddl)
		if _, err := db.ExecContext(ctx, q); err != nil {
			stats.errors = append(stats.errors, fmt.Sprintf("添加列 %s.%s 失败: %v", c.table, c.column, err))
			continue
		}
		log.Printf("  ✓ 添加列: %s.%s", c.table, c.column)
		stats.columnsAdded++
	}
}

// copyBodyHTMLRaw 将 body_html 复制到 body_html_raw（保留原始 HTML）。
func copyBodyHTMLRaw(ctx context.Context, db *sql.DB, stats *migrationStats) {
	if !columnExists(ctx, db, "messages", "body_html_raw") {
		stats.errors = append(stats.errors, "messages.body_html_raw 列不存在，跳过")
		return
	}

	// 只复制 body_html_raw 为空但 body_html 非空的记录
	result, err := db.ExecContext(ctx,
		`UPDATE messages SET body_html_raw = body_html WHERE body_html_raw IS NULL OR body_html_raw = ''`)
	if err != nil {
		stats.errors = append(stats.errors, fmt.Sprintf("复制 body_html_raw 失败: %v", err))
		return
	}
	n, _ := result.RowsAffected()
	stats.bodyHTMLRawCopied = int(n)
	log.Printf("  ✓ 复制 %d 条记录", n)
}

// sanitizeBodyHTML 用 bluemonday 净化 body_html。
func sanitizeBodyHTML(ctx context.Context, db *sql.DB, stats *migrationStats) {
	rows, err := db.QueryContext(ctx, `SELECT id, body_html FROM messages WHERE body_html IS NOT NULL AND body_html != ''`)
	if err != nil {
		stats.errors = append(stats.errors, fmt.Sprintf("查询 body_html 失败: %v", err))
		return
	}
	defer rows.Close()

	type update struct {
		id   int64
		html string
	}
	var updates []update

	for rows.Next() {
		var id int64
		var html string
		if err := rows.Scan(&id, &html); err != nil {
			continue
		}
		sanitized := sanitize.SanitizeHTML(html)
		if sanitized != html {
			updates = append(updates, update{id, sanitized})
		}
	}

	for _, u := range updates {
		if _, err := db.ExecContext(ctx, `UPDATE messages SET body_html = ? WHERE id = ?`, u.html, u.id); err != nil {
			stats.errors = append(stats.errors, fmt.Sprintf("净化 message %d 失败: %v", u.id, err))
			continue
		}
		stats.bodyHTMLSanitized++
	}
	log.Printf("  ✓ 净化 %d 条记录", stats.bodyHTMLSanitized)
}

// normalizeEmails 将所有邮箱地址统一为小写。
func normalizeEmails(ctx context.Context, db *sql.DB, stats *migrationStats) {
	result, err := db.ExecContext(ctx,
		`UPDATE users SET email = LOWER(email) WHERE email != LOWER(email)`)
	if err != nil {
		stats.errors = append(stats.errors, fmt.Sprintf("邮箱小写化失败: %v", err))
		return
	}
	n, _ := result.RowsAffected()
	stats.emailsNormalized = int(n)
	log.Printf("  ✓ 小写化 %d 个邮箱", n)
}

// migrateSpamLog 迁移 spam_log 表（列名变更）。
//
// Node.js schema: sender_ip, sender_addr, recipient_addr, spam_score, reasons, action
// Go schema:      ip, sender, recipient, score, reasons, action
func migrateSpamLog(ctx context.Context, db *sql.DB, stats *migrationStats) {
	if !tableExists(ctx, db, "spam_log") {
		// 表不存在，跳过（Go 迁移会创建）
		return
	}

	// 检查是否已经是 Go schema（有 ip 列）
	if columnExists(ctx, db, "spam_log", "ip") {
		log.Printf("  ✓ spam_log 已是 Go schema，跳过")
		return
	}

	// 检查是否有 Node.js schema（有 sender_ip 列）
	if !columnExists(ctx, db, "spam_log", "sender_ip") {
		log.Printf("  ✓ spam_log schema 未知，跳过")
		return
	}

	// 重命名旧表
	if _, err := db.ExecContext(ctx, `ALTER TABLE spam_log RENAME TO spam_log_nodejs`); err != nil {
		stats.errors = append(stats.errors, fmt.Sprintf("重命名 spam_log 失败: %v", err))
		return
	}

	// 创建 Go schema 表
	_, err := db.ExecContext(ctx, `
CREATE TABLE spam_log (
    id          INTEGER PRIMARY KEY AUTOINCREMENT,
    sender      TEXT,
    recipient   TEXT,
    ip          TEXT,
    score       INTEGER NOT NULL,
    reasons     TEXT,
    action      TEXT NOT NULL,
    created_at  DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
)`)
	if err != nil {
		stats.errors = append(stats.errors, fmt.Sprintf("创建 spam_log 新表失败: %v", err))
		return
	}

	// 迁移数据
	result, err := db.ExecContext(ctx, `
INSERT INTO spam_log (id, sender, recipient, ip, score, reasons, action, created_at)
SELECT id, sender_addr, recipient_addr, sender_ip, spam_score, reasons, action, created_at
FROM spam_log_nodejs`)
	if err != nil {
		stats.errors = append(stats.errors, fmt.Sprintf("迁移 spam_log 数据失败: %v", err))
		return
	}
	n, _ := result.RowsAffected()
	stats.spamLogMigrated = int(n)

	// 删除旧表
	db.ExecContext(ctx, `DROP TABLE spam_log_nodejs`)

	// 创建索引
	db.ExecContext(ctx, `CREATE INDEX IF NOT EXISTS idx_spam_log_created ON spam_log(created_at)`)
	db.ExecContext(ctx, `CREATE INDEX IF NOT EXISTS idx_spam_log_sender ON spam_log(sender)`)
	db.ExecContext(ctx, `CREATE INDEX IF NOT EXISTS idx_spam_log_action ON spam_log(action)`)

	log.Printf("  ✓ 迁移 %d 条 spam_log 记录", n)
}

// createNewTables 创建 Go 版新增的表。
func createNewTables(ctx context.Context, db *sql.DB, stats *migrationStats) {
	tables := []struct {
		name string
		ddl  string
	}{
		{"audit_log", `
CREATE TABLE IF NOT EXISTS audit_log (
    id            INTEGER PRIMARY KEY AUTOINCREMENT,
    timestamp     DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    actor_type    TEXT NOT NULL,
    actor_id      INTEGER,
    actor_ip      TEXT,
    action        TEXT NOT NULL,
    resource_type TEXT,
    resource_id   TEXT,
    result        TEXT NOT NULL,
    detail        TEXT,
    request_id    TEXT
)`},
		{"greylist", `
CREATE TABLE IF NOT EXISTS greylist (
    key         TEXT PRIMARY KEY,
    first_seen  INTEGER NOT NULL,
    allowed     INTEGER NOT NULL DEFAULT 0
)`},
		{"mail_queue", `
CREATE TABLE IF NOT EXISTS mail_queue (
    id            INTEGER PRIMARY KEY AUTOINCREMENT,
    user_id       INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    from_addr     TEXT NOT NULL,
    to_addrs      TEXT NOT NULL,
    cc_addrs      TEXT,
    bcc_addrs     TEXT,
    subject       TEXT,
    body_html     TEXT,
    body_text     TEXT,
    reply_to      TEXT,
    attachments   TEXT,
    status        TEXT NOT NULL DEFAULT 'pending',
    attempts      INTEGER NOT NULL DEFAULT 0,
    max_attempts  INTEGER NOT NULL DEFAULT 3,
    next_retry_at DATETIME,
    error_msg     TEXT,
    created_at    DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    sent_at       DATETIME
)`},
	}

	for _, t := range tables {
		if tableExists(ctx, db, t.name) {
			continue
		}
		if _, err := db.ExecContext(ctx, t.ddl); err != nil {
			stats.errors = append(stats.errors, fmt.Sprintf("创建表 %s 失败: %v", t.name, err))
			continue
		}
		log.Printf("  ✓ 创建表: %s", t.name)
		stats.tablesCreated++
	}

	// audit_log 索引
	for _, idx := range []string{
		`CREATE INDEX IF NOT EXISTS idx_audit_timestamp ON audit_log(timestamp)`,
		`CREATE INDEX IF NOT EXISTS idx_audit_actor ON audit_log(actor_type, actor_id)`,
		`CREATE INDEX IF NOT EXISTS idx_audit_action ON audit_log(action)`,
	} {
		db.ExecContext(ctx, idx)
	}

	// greylist 索引
	db.ExecContext(ctx, `CREATE INDEX IF NOT EXISTS idx_greylist_first_seen ON greylist(first_seen)`)

	// mail_queue 索引
	db.ExecContext(ctx, `CREATE INDEX IF NOT EXISTS idx_queue_status ON mail_queue(status, next_retry_at)`)
	db.ExecContext(ctx, `CREATE INDEX IF NOT EXISTS idx_queue_user ON mail_queue(user_id)`)
}

// addIndexesAndHandleAPIKeys 补全索引 + 处理旧 API Key。
func addIndexesAndHandleAPIKeys(ctx context.Context, db *sql.DB, stats *migrationStats) {
	// 补全 messages 表索引
	indexes := []string{
		`CREATE INDEX IF NOT EXISTS idx_msg_user_read ON messages(user_id, is_read)`,
		`CREATE INDEX IF NOT EXISTS idx_msg_user_received ON messages(user_id, received_at DESC)`,
		`CREATE INDEX IF NOT EXISTS idx_msg_message_id ON messages(message_id)`,
		`CREATE INDEX IF NOT EXISTS idx_msg_user_starred ON messages(user_id, is_starred) WHERE is_starred = 1`,
		`CREATE INDEX IF NOT EXISTS idx_msg_user_deleted ON messages(user_id, is_deleted) WHERE is_deleted = 1`,
		`CREATE INDEX IF NOT EXISTS idx_msg_from_addr ON messages(from_addr)`,
		`CREATE INDEX IF NOT EXISTS idx_msg_folder ON messages(folder)`,
		// users 索引
		`CREATE INDEX IF NOT EXISTS idx_users_role ON users(role)`,
		`CREATE INDEX IF NOT EXISTS idx_users_active ON users(is_active) WHERE is_active = 1`,
		// send_log 索引
		`CREATE INDEX IF NOT EXISTS idx_send_log_status ON send_log(status)`,
		// api_keys 索引
		`CREATE INDEX IF NOT EXISTS idx_api_keys_prefix ON api_keys(key_prefix) WHERE is_active = 1`,
		`CREATE INDEX IF NOT EXISTS idx_api_keys_user ON api_keys(user_id)`,
		`CREATE INDEX IF NOT EXISTS idx_api_keys_active ON api_keys(is_active) WHERE is_active = 1`,
		// mail_rules 索引
		`CREATE INDEX IF NOT EXISTS idx_rules_user_active ON mail_rules(user_id, is_active)`,
		`CREATE INDEX IF NOT EXISTS idx_rules_priority ON mail_rules(priority)`,
		`CREATE INDEX IF NOT EXISTS idx_rules_user_priority ON mail_rules(user_id, priority)`,
	}

	for _, idx := range indexes {
		result, err := db.ExecContext(ctx, idx)
		if err != nil {
			continue
		}
		if n, _ := result.RowsAffected(); n > 0 {
			stats.indexesCreated++
		}
	}
	log.Printf("  ✓ 索引补全完成")

	// 处理 API Key：key_prefix 为空的标记为 inactive
	if tableExists(ctx, db, "api_keys") && columnExists(ctx, db, "api_keys", "key_prefix") {
		result, err := db.ExecContext(ctx,
			`UPDATE api_keys SET is_active = 0 WHERE key_prefix = '' AND is_active = 1`)
		if err != nil {
			stats.errors = append(stats.errors, fmt.Sprintf("标记旧 API Key 失败: %v", err))
			return
		}
		n, _ := result.RowsAffected()
		stats.apiKeysMarkedInactive = int(n)
		if n > 0 {
			log.Printf("  ⚠ %d 个 API Key key_prefix 为空，已标记为 inactive", n)
		}
	}
}

// checkSchema 在 dry-run 模式下检查 schema 差异。
func checkSchema(ctx context.Context, db *sql.DB, stats *migrationStats) {
	// 检查缺失列
	checks := []struct {
		table  string
		column string
	}{
		{"messages", "body_html_raw"},
		{"messages", "spam_score"},
		{"messages", "spam_reasons"},
		{"users", "is_default_password"},
		{"api_keys", "key_prefix"},
	}
	for _, c := range checks {
		if tableExists(ctx, db, c.table) {
			if !columnExists(ctx, db, c.table, c.column) {
				log.Printf("  [缺失] %s.%s", c.table, c.column)
				stats.columnsAdded++
			}
		}
	}

	// 检查缺失表
	for _, t := range []string{"audit_log", "greylist", "mail_queue"} {
		if !tableExists(ctx, db, t) {
			log.Printf("  [缺失] 表 %s", t)
			stats.tablesCreated++
		}
	}

	// 检查 spam_log schema
	if tableExists(ctx, db, "spam_log") {
		if columnExists(ctx, db, "spam_log", "sender_ip") && !columnExists(ctx, db, "spam_log", "ip") {
			log.Printf("  [需迁移] spam_log 列名变更")
			stats.spamLogMigrated++
		}
	}

	// 统计需净化的邮件数
	var htmlCount int
	db.QueryRowContext(ctx, `SELECT COUNT(*) FROM messages WHERE body_html IS NOT NULL AND body_html != ''`).Scan(&htmlCount)
	log.Printf("  [需净化] %d 条邮件 HTML 需检查", htmlCount)

	// 统计需小写化的邮箱数
	var emailCount int
	db.QueryRowContext(ctx, `SELECT COUNT(*) FROM users WHERE email != LOWER(email)`).Scan(&emailCount)
	log.Printf("  [需小写化] %d 个邮箱", emailCount)

	// 统计需处理的 API Key
	var apiKeyCount int
	if tableExists(ctx, db, "api_keys") && columnExists(ctx, db, "api_keys", "key_prefix") {
		db.QueryRowContext(ctx, `SELECT COUNT(*) FROM api_keys WHERE key_prefix = '' AND is_active = 1`).Scan(&apiKeyCount)
	}
	log.Printf("  [需处理] %d 个 API Key 需重建", apiKeyCount)
}
