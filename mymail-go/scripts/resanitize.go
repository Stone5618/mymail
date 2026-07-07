// resanitize.go — 用新的净化策略重新处理所有邮件的 body_html。
//
// 用法：
//
//	go run scripts/resanitize.go -db /path/to/mymail.db
//
// 功能：
//  1. 查询所有 body_html_raw 不为空的邮件
//  2. 用当前 sanitize 策略重新净化 body_html_raw
//  3. 更新 body_html 字段
//
// 注意：此脚本幂等——重复运行不会损坏数据。
//
//go:build ignore

package main

import (
	"database/sql"
	"flag"
	"fmt"
	"log"

	"github.com/mymail/mymail-go/internal/sanitize"

	_ "modernc.org/sqlite"
)

var dbPath = flag.String("db", "", "mymail.db 路径（必填）")

func main() {
	flag.Parse()
	log.SetFlags(log.LstdFlags | log.Lshortfile)

	if *dbPath == "" {
		log.Fatal("必须指定 -db 参数")
	}

	dsn := fmt.Sprintf("file:%s?_pragma=journal_mode(WAL)&_pragma=busy_timeout(5000)", *dbPath)
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		log.Fatalf("打开数据库失败: %v", err)
	}
	defer db.Close()

	// 查询所有有 body_html_raw 的邮件
	rows, err := db.Query(`SELECT id, body_html_raw FROM messages WHERE body_html_raw IS NOT NULL AND body_html_raw != ''`)
	if err != nil {
		log.Fatalf("查询失败: %v", err)
	}
	defer rows.Close()

	type task struct {
		id      int64
		rawHTML string
	}
	var tasks []task
	for rows.Next() {
		var t task
		var raw sql.NullString
		if err := rows.Scan(&t.id, &raw); err != nil {
			log.Fatalf("扫描行失败: %v", err)
		}
		if raw.Valid && raw.String != "" {
			t.rawHTML = raw.String
			tasks = append(tasks, t)
		}
	}
	rows.Close()

	log.Printf("找到 %d 封需要重新净化的邮件", len(tasks))

	// 逐个净化并更新
	tx, err := db.Begin()
	if err != nil {
		log.Fatalf("开启事务失败: %v", err)
	}
	stmt, err := tx.Prepare(`UPDATE messages SET body_html = ? WHERE id = ?`)
	if err != nil {
		log.Fatalf("预备语句失败: %v", err)
	}
	defer stmt.Close()

	updated := 0
	for _, t := range tasks {
		cleaned := sanitize.SanitizeHTML(t.rawHTML)
		if _, err := stmt.Exec(cleaned, t.id); err != nil {
			log.Printf("更新邮件 %d 失败: %v", t.id, err)
			continue
		}
		updated++
	}

	if err := tx.Commit(); err != nil {
		log.Fatalf("提交事务失败: %v", err)
	}

	log.Printf("成功更新 %d / %d 封邮件的 body_html", updated, len(tasks))
}
