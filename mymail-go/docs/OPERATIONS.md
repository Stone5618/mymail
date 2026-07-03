# MyMail 运维手册

本文档涵盖 MyMail Go 后端的日常运维任务、服务管理、数据库维护、性能调优与故障排查。

- HTTP 端口：3000（Go 后端）
- SMTP 端口：25（入站接收）
- WebSocket 路径：`/ws`
- 健康检查：`GET /healthz`、`GET /readyz`、`GET /startupz`
- 指标端点：`GET /metrics`（Prometheus 格式）
- 数据库：SQLite，路径 `/app/data/mymail.db`（Docker）或 `./data/mymail.db`（裸机）

---

## 1. 日常运维任务

### 1.1 备份（每日）

```bash
# 设置加密密码（建议存入 /root/.backup-env 并 chmod 600）
export BACKUP_PASSWORD='your-strong-backup-password'

# 执行备份
cd /path/to/mymail/mymail-go
./scripts/backup.sh

# 验证最新备份
ls -lht backups/ | head -5
```

定时任务（crontab）：

```cron
# 每日凌晨 3 点备份
0 3 * * * . /root/.backup-env && /path/to/mymail/mymail-go/scripts/backup.sh >> /var/log/mymail-backup.log 2>&1
```

### 1.2 日志轮转

Go 后端输出 JSON 结构化日志到 stdout。Docker 环境用 `docker logs`，裸机用 systemd journal。

```bash
# Docker：日志默认无上限，需配置 log rotation
# /etc/docker/daemon.json
{
  "log-driver": "json-file",
  "log-opts": {
    "max-size": "50m",
    "max-file": "5"
  }
}
# 重载 Docker
sudo systemctl restart docker

# 裸机：journald 自动轮转，可配置保留期
# /etc/systemd/journald.conf
[Journal]
SystemMaxUse=500M
MaxRetentionSec=30day
# 应用
sudo systemctl restart systemd-journald
```

审计日志单独写入 JSONL 文件 `/app/data/audit.log`，需手动轮转：

```bash
# 每周轮转审计日志
cat > /etc/logrotate.d/mymail-audit << 'EOF'
/app/data/audit.log {
    weekly
    rotate 12
    compress
    delaycompress
    missingok
    notifempty
    copytruncate
}
EOF
```

### 1.3 磁盘监控

```bash
# 查看磁盘使用
df -h

# 查看 data 目录各子目录大小
du -sh /app/data/*
# 预期：
#   /app/data/mymail.db    （数据库，通常 < 1GB）
#   /app/data/maildir/     （邮件正文，主要占用）
#   /app/data/attachments/ （附件，主要占用）
#   /app/data/audit.log    （审计日志）

# 查找大文件（> 100MB）
find /app/data -type f -size +100M -exec ls -lh {} \;

# 设置磁盘告警（> 80% 报警）
# 配合监控系统的 node_filesystem_avail_bytes 指标
```

---

## 2. 服务管理

### 2.1 Docker 环境

```bash
cd /path/to/mymail/mymail-go

# 启动全部服务
make docker-up
# 等价：docker compose -f deployments/docker/docker-compose.yml up -d

# 停止
make docker-down

# 重启单个服务
docker compose -f deployments/docker/docker-compose.yml restart app
docker compose -f deployments/docker/docker-compose.yml restart dovecot
docker compose -f deployments/docker/docker-compose.yml restart postfix
docker compose -f deployments/docker/docker-compose.yml restart nginx

# 查看日志（实时跟踪）
make docker-logs
# 单个服务
docker logs -f --tail 100 mymail-app
docker logs -f --tail 100 mymail-dovecot
docker logs -f --tail 100 mymail-postfix

# 进入容器
docker exec -it mymail-app sh
docker exec -it mymail-dovecot sh

# 查看健康状态
docker inspect --format='{{.State.Health.Status}}' mymail-app
```

### 2.2 裸机环境（systemd）

```bash
# Go 后端
sudo systemctl start mymail
sudo systemctl stop mymail
sudo systemctl restart mymail
sudo systemctl status mymail

# Dovecot
sudo systemctl start dovecot
sudo systemctl stop dovecot
sudo systemctl restart dovecot

# Nginx
sudo systemctl reload nginx     # 平滑重载配置
sudo systemctl restart nginx

# Postfix
sudo systemctl restart postfix

# 查看日志
sudo journalctl -u mymail -f --since "10 min ago"
sudo journalctl -u mymail --since today
sudo tail -f /var/log/dovecot.log
```

### 2.3 优雅关闭

Go 后端接收到 `SIGINT`（Ctrl+C）或 `SIGTERM` 后执行优雅关闭：

1. HTTP 服务停止接收新请求
2. SMTP 接收器停止接受新连接
3. 队列 worker 停止
4. SMTP 限流器后台清理停止
5. WebSocket Hub 关闭所有连接（draining）
6. OpenTelemetry exporter flush
7. 审计日志器刷盘
8. 关闭数据库连接

关闭窗口：30 秒。超时则强制退出。

```bash
# Docker：发送 SIGTERM（默认 stop 行为）
docker compose stop app

# 裸机
sudo systemctl stop mymail
```

---

## 3. 数据库维护

### 3.1 VACUUM（空间回收）

删除大量邮件后，SQLite 文件不会自动收缩。需手动 VACUUM：

```bash
# 停服后执行（VACUUM 需要独占锁）
sqlite3 /app/data/mymail.db "VACUUM;"

# 查看前后大小
ls -lh /app/data/mymail.db
```

> ⚠️ VACUUM 会重写整个数据库文件，耗时与数据库大小成正比。建议在低峰期执行，且磁盘空间 ≥ 数据库大小的 2 倍。

### 3.2 索引重建（REINDEX）

```bash
# 重建所有索引（解决索引碎片化）
sqlite3 /app/data/mymail.db "REINDEX;"

# 重建特定索引
sqlite3 /app/data/mymail.db "REINDEX idx_msg_user_received;"
```

### 3.3 WAL Checkpoint

SQLite WAL 模式下，写入先到 `-wal` 文件，定期 checkpoint 合并到主库。

```bash
# 查看 WAL 状态
sqlite3 /app/data/mymail.db "PRAGMA wal_checkpoint;"

# 强制 checkpoint（PASSIVE 模式，不阻塞）
sqlite3 /app/data/mymail.db "PRAGMA wal_checkpoint(PASSIVE);"

# 强制 checkpoint（TRUNCATE 模式，截断 WAL 文件）
sqlite3 /app/data/mymail.db "PRAGMA wal_checkpoint(TRUNCATE);"

# 查看 WAL 文件大小（过大说明 checkpoint 不及时）
ls -lh /app/data/mymail.db-wal
```

### 3.4 完整性检查

```bash
# 快速完整性检查
sqlite3 /app/data/mymail.db "PRAGMA quick_check;"

# 完整完整性检查（较慢）
sqlite3 /app/data/mymail.db "PRAGMA integrity_check;"

# 外键约束检查
sqlite3 /app/data/mymail.db "PRAGMA foreign_key_check;"
```

### 3.5 数据库统计

```bash
# 各表行数
sqlite3 /app/data/mymail.db << 'EOF'
.mode column
.headers on
SELECT 'users' AS table_name, COUNT(*) AS rows FROM users
UNION ALL SELECT 'messages', COUNT(*) FROM messages
UNION ALL SELECT 'attachments', COUNT(*) FROM attachments
UNION ALL SELECT 'send_log', COUNT(*) FROM send_log
UNION ALL SELECT 'api_keys', COUNT(*) FROM api_keys
UNION ALL SELECT 'mail_rules', COUNT(*) FROM mail_rules
UNION ALL SELECT 'mail_queue', COUNT(*) FROM mail_queue
UNION ALL SELECT 'spam_log', COUNT(*) FROM spam_log
UNION ALL SELECT 'greylist', COUNT(*) FROM greylist
UNION ALL SELECT 'audit_log', COUNT(*) FROM audit_log;
EOF

# 索引使用情况
sqlite3 /app/data/mymail.db "SELECT * FROM sqlite_stat1;" 2>/dev/null || echo "需先 ANALYZE"
```

---

## 4. 邮件队列管理

`mail_queue` 表存储待发送邮件。队列 worker 每 5 秒轮询一次，失败自动重试（最多 3 次，指数退避）。

### 4.1 查看队列

```bash
# 查看队列状态汇总
sqlite3 /app/data/mymail.db << 'EOF'
.mode column
.headers on
SELECT status, COUNT(*) AS count, MIN(created_at) AS oldest, MAX(created_at) AS newest
FROM mail_queue
GROUP BY status;
EOF

# 预期状态：
#   pending    - 待发送
#   sending    - 发送中
#   sent       - 已发送
#   failed     - 发送失败（已达最大重试次数）

# 查看待发送邮件详情
sqlite3 -header -column /app/data/mymail.db \
  "SELECT id, user_id, from_addr, to_addrs, subject, attempts, next_retry_at, error_msg
   FROM mail_queue WHERE status IN ('pending','failed') ORDER BY created_at DESC LIMIT 20;"

# 查看失败邮件的错误原因
sqlite3 -header -column /app/data/mymail.db \
  "SELECT id, to_addrs, subject, attempts, error_msg
   FROM mail_queue WHERE status='failed' ORDER BY created_at DESC;"
```

### 4.2 重试失败邮件

```bash
# 重置所有失败邮件为 pending（手动触发重试）
sqlite3 /app/data/mymail.db \
  "UPDATE mail_queue SET status='pending', attempts=0, next_retry_at=NULL, error_msg=NULL
   WHERE status='failed';"

# 重置特定邮件
sqlite3 /app/data/mymail.db \
  "UPDATE mail_queue SET status='pending', attempts=0, next_retry_at=NULL
   WHERE id=123;"

# 调整最大重试次数（针对特定邮件）
sqlite3 /app/data/mymail.db \
  "UPDATE mail_queue SET max_attempts=5 WHERE id=123;"
```

### 4.3 清空队列

```bash
# 清空已发送邮件（清理历史）
sqlite3 /app/data/mymail.db "DELETE FROM mail_queue WHERE status='sent';"

# 清空全部队列（谨慎！会丢失待发送邮件）
sqlite3 /app/data/mymail.db "DELETE FROM mail_queue;"

# 清空 7 天前的已发送邮件
sqlite3 /app/data/mymail.db \
  "DELETE FROM mail_queue WHERE status='sent' AND sent_at < datetime('now', '-7 days');"
```

### 4.4 队列深度监控

```bash
# 通过 Prometheus 指标查看队列深度
curl -s http://localhost:3000/metrics | grep mymail_queue_depth
# 预期：mymail_queue_depth <数值>

# 告警规则示例（Prometheus）
# queue_depth > 100 持续 5 分钟 → 报警
```

---

## 5. 用户管理

### 5.1 创建管理员

```bash
# 方式 A：通过 API（需已有管理员 token）
curl -X POST http://localhost:3000/api/admin/users \
  -H "Authorization: Bearer <admin-jwt-token>" \
  -H "Content-Type: application/json" \
  -d '{
    "username": "newadmin",
    "email": "newadmin@example.com",
    "password": "strong-password",
    "role": "admin"
  }'

# 方式 B：直接操作数据库（紧急情况）
# 生成 bcrypt hash（需用 Go 工具，因为 cost=12）
go run -tags=tools ./scripts/hash-password.go "strong-password"
# 输出：$2a$12$...

sqlite3 /app/data/mymail.db << 'EOF'
INSERT INTO users (username, email, password_hash, role, is_active)
VALUES ('newadmin', 'newadmin@example.com', '$2a$12$...', 'admin', 1);
EOF
```

### 5.2 重置密码

```bash
# 方式 A：通过 API
curl -X POST http://localhost:3000/api/admin/users/<user_id>/reset-password \
  -H "Authorization: Bearer <admin-jwt-token>" \
  -H "Content-Type: application/json" \
  -d '{"new_password": "new-strong-password"}'

# 方式 B：直接数据库（紧急）
# 先生成 hash
go run -tags=tools ./scripts/hash-password.go "new-strong-password"

sqlite3 /app/data/mymail.db \
  "UPDATE users SET password_hash='\$2a\$12\$...', is_default_password=1, login_fails=0, locked_until=NULL WHERE email='user@example.com';"
```

> 重置密码后 `is_default_password` 设为 1，用户首次登录后会被强制改密。

### 5.3 调整配额

```bash
# 查看用户当前配额使用
sqlite3 -header -column /app/data/mymail.db \
  "SELECT id, username, email, storage_limit, storage_used,
          ROUND(storage_used*100.0/storage_limit, 2) AS usage_pct
   FROM users ORDER BY usage_pct DESC LIMIT 10;"

# 调整配额（单位：字节，默认 100MB = 104857600）
sqlite3 /app/data/mymail.db \
  "UPDATE users SET storage_limit=524288000 WHERE email='user@example.com';"
# 设置为 500MB

# 通过 API 调整
curl -X PUT http://localhost:3000/api/admin/users/<user_id> \
  -H "Authorization: Bearer <admin-jwt-token>" \
  -H "Content-Type: application/json" \
  -d '{"storage_limit": 524288000}'
```

### 5.4 锁定/解锁用户

```bash
# 锁定用户（设为不活跃）
sqlite3 /app/data/mymail.db "UPDATE users SET is_active=0 WHERE email='user@example.com';"

# 解锁用户（清除登录失败计数和锁定时间）
sqlite3 /app/data/mymail.db \
  "UPDATE users SET is_active=1, login_fails=0, locked_until=NULL WHERE email='user@example.com';"
```

### 5.5 删除用户（级联清理）

```bash
# Go 版 schema 带 ON DELETE CASCADE，删除用户会自动清理：
#   messages, attachments, send_log, api_keys, mail_rules, mail_queue
sqlite3 /app/data/mymail.db "DELETE FROM users WHERE id=<user_id>;"

# 注意：maildir 文件和附件文件不会自动删除，需手动清理
rm -rf /app/data/maildir/example.com/username
# 附件文件需按 storage_path 单独删除
```

---

## 6. 安全审计

### 6.1 audit_log 查询

审计日志双写：DB（`audit_log` 表）+ JSONL 文件（`/app/data/audit.log`）。

```bash
# 查看最近 20 条审计日志
sqlite3 -header -column /app/data/mymail.db \
  "SELECT timestamp, actor_type, actor_id, actor_ip, action, resource_type, result
   FROM audit_log ORDER BY timestamp DESC LIMIT 20;"

# 查询登录失败记录（异常检测）
sqlite3 -header -column /app/data/mymail.db \
  "SELECT timestamp, actor_ip, actor_id, detail
   FROM audit_log
   WHERE action='login' AND result='failure'
   ORDER BY timestamp DESC LIMIT 50;"

# 查询特定 IP 的所有操作
sqlite3 -header -column /app/data/mymail.db \
  "SELECT timestamp, action, resource_type, result
   FROM audit_log WHERE actor_ip='1.2.3.4' ORDER BY timestamp DESC;"

# 查询管理员操作
sqlite3 -header -column /app/data/mymail.db \
  "SELECT timestamp, actor_id, action, resource_type, resource_id, result
   FROM audit_log WHERE actor_type='admin' ORDER BY timestamp DESC LIMIT 50;"

# 统计各操作类型计数
sqlite3 -header -column /app/data/mymail.db \
  "SELECT action, result, COUNT(*) AS count
   FROM audit_log
   WHERE timestamp > datetime('now', '-24 hours')
   GROUP BY action, result ORDER BY count DESC;"
```

### 6.2 异常检测

```bash
# 同一 IP 短时间内多次登录失败（暴力破解）
sqlite3 -header -column /app/data/mymail.db << 'EOF'
SELECT actor_ip, COUNT(*) AS fail_count, MIN(timestamp) AS first, MAX(timestamp) AS last
FROM audit_log
WHERE action='login' AND result='failure'
  AND timestamp > datetime('now', '-1 hour')
GROUP BY actor_ip
HAVING fail_count > 5
ORDER BY fail_count DESC;
EOF

# 非工作时间的管理员操作（潜在未授权访问）
sqlite3 -header -column /app/data/mymail.db \
  "SELECT timestamp, actor_id, action, actor_ip
   FROM audit_log
   WHERE actor_type='admin'
     AND (strftime('%H', timestamp) < '06' OR strftime('%H', timestamp) > '22')
   ORDER BY timestamp DESC LIMIT 30;"
```

### 6.3 审计日志归档

```bash
# 归档 90 天前的审计日志
sqlite3 /app/data/mymail.db << 'EOF'
-- 导出到归档文件
.mode csv
.output /tmp/audit_archive_$(date +%Y%m%d).csv
SELECT * FROM audit_log WHERE timestamp < datetime('now', '-90 days');
.output stdout

-- 删除已归档记录
DELETE FROM audit_log WHERE timestamp < datetime('now', '-90 days');
EOF

# 压缩归档
gzip /tmp/audit_archive_*.csv
```

---

## 7. 性能调优

### 7.1 SQLite PRAGMA

当前配置（`internal/storage/db/db.go`）：

```go
PRAGMA journal_mode=WAL         // WAL 模式，并发读不阻塞写
PRAGMA foreign_keys=ON          // 启用外键约束
PRAGMA busy_timeout=5000        // 写冲突等待 5 秒
PRAGMA synchronous=NORMAL       // WAL 下 NORMAL 足够安全且更快
```

调优建议：

| 场景 | 调整 | 说明 |
|---|---|---|
| 写密集 | `PRAGMA wal_autocheckpoint=1000` | 默认 1000 页，可降低 checkpoint 频率 |
| 极致性能（容忍数据丢失） | `PRAGMA synchronous=OFF` | ⚠️ 不推荐生产，断电可能丢数据 |
| 大数据库（> 1GB） | `PRAGMA cache_size=-65536` | 64MB 缓存（负数表示 KB） |
| 慢查询排查 | `PRAGMA temp_store=MEMORY` | 临时表存内存 |

### 7.2 连接池

当前配置（`db.go`）：

```go
db.SetMaxOpenConns(1)    // SQLite 写串行，单连接避免 write lock 冲突
db.SetMaxIdleConns(1)
db.SetConnMaxLifetime(0) // 长连接
```

> SQLite 的写锁是数据库级别的（非行级），多连接并发写会触发 `SQLITE_BUSY`。Go 版本用单连接串行化写，配合 `busy_timeout=5000` 容错。

### 7.3 Bcrypt Cost

当前 `BcryptCost=12`，登录延迟约 250ms。

| Cost | 单次哈希耗时 | 安全性 | 建议 |
|---|---|---|---|
| 10 | ~60ms | 中 | 仅测试环境 |
| 12 | ~250ms | 高 | **生产推荐**（当前配置） |
| 14 | ~1s | 极高 | 高安全场景，但影响并发 |

调优原则：

- cost 每加 1，耗时翻倍
- 登录是低频操作，250ms 延迟可接受
- API Key 校验也用 bcrypt，高频 API 调用需评估（Go 版已用 `key_prefix` 索引优化为 O(1) 查找 + 单次 bcrypt）

### 7.4 慢查询排查

```bash
# 开启 SQLite 慢查询日志（需停服重启）
# 临时在 db.go 中添加（不推荐生产长期开启）
# db.Exec("PRAGMA temp_store=MEMORY")

# 使用 EXPLAIN QUERY PLAN 分析慢查询
sqlite3 /app/data/mymail.db << 'EOF'
EXPLAIN QUERY PLAN
SELECT * FROM messages WHERE user_id=1 AND folder='INBOX' ORDER BY received_at DESC LIMIT 20;
EOF
# 预期：使用 idx_msg_user_received 索引

# 检查索引使用统计（需先 ANALYZE）
sqlite3 /app/data/mymail.db "ANALYZE;"
sqlite3 /app/data/mymail.db "SELECT * FROM sqlite_stat1 ORDER BY tbl;"
```

### 7.5 内存与 goroutine

```bash
# 通过 metrics 查看运行时状态
curl -s http://localhost:3000/metrics | grep -E "go_|process_"

# 关注指标：
#   go_goroutines          - goroutine 数（正常 < 50）
#   go_memstats_alloc_bytes - 堆内存
#   process_resident_memory_bytes - RSS
```

---

## 8. 故障排查

### 8.1 邮件不送达

**症状**：外部邮件发不到本域 / 本域邮件发不出去

**诊断**：

```bash
# 1. 检查 SMTP 端口 25 是否监听
docker exec mymail-app netstat -tlnp | grep :25
# 或
ss -tlnp | grep :25

# 2. 从外部 telnet 测试 SMTP
telnet mail.example.com 25
# 预期：220 <hostname> ESMTP MyMail

# 3. 检查 Go 后端 SMTP 日志
docker logs mymail-app 2>&1 | grep -i "smtp\|receive" | tail -30

# 4. 检查 Postfix 出站日志
docker logs mymail-postfix 2>&1 | tail -30

# 5. 检查邮件队列是否堆积
sqlite3 /app/data/mymail.db \
  "SELECT status, COUNT(*) FROM mail_queue GROUP BY status;"

# 6. 检查 DNS/MX 记录
dig MX example.com +short

# 7. 检查熔断器状态（出站 SMTP）
docker logs mymail-app 2>&1 | grep -i "circuit\|breaker"
```

详见 [RB-001-SMTP-Outage.md](./runbooks/RB-001-SMTP-Outage.md)。

### 8.2 登录失败

**症状**：用户无法登录 Web 或 IMAP

**诊断**：

```bash
# 1. 检查用户是否被锁定
sqlite3 -header -column /app/data/mymail.db \
  "SELECT id, username, email, is_active, login_fails, locked_until
   FROM users WHERE email='user@example.com';"

# 2. 检查审计日志中的登录失败记录
sqlite3 -header -column /app/data/mymail.db \
  "SELECT timestamp, actor_ip, detail
   FROM audit_log WHERE action='login' AND result='failure'
   ORDER BY timestamp DESC LIMIT 10;"

# 3. 解锁用户
sqlite3 /app/data/mymail.db \
  "UPDATE users SET is_active=1, login_fails=0, locked_until=NULL
   WHERE email='user@example.com';"

# 4. 验证密码哈希格式
sqlite3 /app/data/mymail.db \
  "SELECT username, substr(password_hash, 1, 4) FROM users WHERE email='user@example.com';"
# 预期：$2a$ 或 $2b$ 或 $2y$

# 5. IMAP 登录失败还需检查 Dovecot
docker logs mymail-dovecot 2>&1 | grep -i "auth\|fail" | tail -20
sudo tail -f /var/log/dovecot.log
```

### 8.3 WebSocket 断连

**症状**：新邮件到达时前端不实时刷新

**诊断**：

```bash
# 1. 检查 WebSocket 连接数指标
curl -s http://localhost:3000/metrics | grep mymail_ws
# 预期：mymail_ws_connections_active > 0

# 2. 检查 Nginx WebSocket 代理配置
# config/nginx/default.conf 中 location /ws 必须有：
#   proxy_http_version 1.1;
#   proxy_set_header Upgrade $http_upgrade;
#   proxy_set_header Connection "upgrade";
#   proxy_read_timeout 86400;

# 3. 浏览器控制台检查 WS 连接
# 在浏览器 DevTools Console 执行：
#   new WebSocket('wss://mail.example.com/ws')
# 观察是否连接成功

# 4. 检查 JWT 是否过期（WS 连接需带 token）
# 前端会自动用 refresh token 续期，若 refresh 也过期需重新登录

# 5. 检查 CORS 配置
# WebSocket 不受 CORS 限制，但 Origin header 会被校验
```

### 8.4 API 响应慢

```bash
# 1. 查看 HTTP 请求延迟分布
curl -s http://localhost:3000/metrics | grep mymail_http_request_duration

# 2. 查看在途请求数
curl -s http://localhost:3000/metrics | grep mymail_http_requests_in_flight

# 3. 检查数据库连接
curl -s http://localhost:3000/metrics | grep mymail_db_connections

# 4. 检查是否 SQLITE_BUSY
docker logs mymail-app 2>&1 | grep -i "busy\|locked" | tail -10

# 5. 检查磁盘 IO
iostat -x 1 5
```

---

## 9. 监控指标说明

`/metrics` 端点输出 Prometheus 格式指标，命名空间为 `mymail_*`。

### 9.1 HTTP 指标

| 指标 | 类型 | 标签 | 说明 |
|---|---|---|---|
| `mymail_http_requests_total` | Counter | method, path, status | HTTP 请求总数 |
| `mymail_http_request_duration_seconds` | Histogram | method, path | HTTP 请求延迟（秒） |
| `mymail_http_requests_in_flight` | Gauge | - | 当前在途 HTTP 请求数 |

### 9.2 业务指标

| 指标 | 类型 | 标签 | 说明 |
|---|---|---|---|
| `mymail_mails_received_total` | Counter | folder, spam_action | SMTP 接收邮件总数 |
| `mymail_mails_sent_total` | Counter | status | 发送邮件总数（sent/failed） |
| `mymail_queue_depth` | Gauge | - | 当前发送队列深度 |

### 9.3 SMTP 指标

| 指标 | 类型 | 标签 | 说明 |
|---|---|---|---|
| `mymail_smtp_connections_active` | Gauge | - | 活跃 SMTP 连接数 |
| `mymail_smtp_connections_total` | Counter | result | SMTP 连接总数（accepted/rejected） |

### 9.4 基础设施指标

| 指标 | 类型 | 标签 | 说明 |
|---|---|---|---|
| `mymail_db_connections_active` | Gauge | - | 活跃数据库连接数 |
| `mymail_auth_attempts_total` | Counter | method, result | 认证尝试总数（login/apikey） |

### 9.5 WebSocket 指标

| 指标 | 类型 | 标签 | 说明 |
|---|---|---|---|
| `mymail_ws_connections_active` | Gauge | - | 活跃 WebSocket 连接数 |
| `mymail_ws_connections_total` | Counter | result | WebSocket 连接总数 |
| `mymail_ws_messages_sent_total` | Counter | type | WebSocket 发送消息总数 |

### 9.6 Go 运行时指标（自动）

| 指标 | 说明 |
|---|---|
| `go_goroutines` | goroutine 数量 |
| `go_memstats_alloc_bytes` | 堆内存分配 |
| `go_memstats_sys_bytes` | 系统内存 |
| `go_gc_duration_seconds` | GC 耗时 |
| `process_resident_memory_bytes` | 进程 RSS |
| `process_cpu_seconds_total` | CPU 使用 |

### 9.7 推荐告警规则

```yaml
# Prometheus告警规则示例
groups:
  - name: mymail
    rules:
      - alert: MyMailSMTPDown
        expr: mymail_smtp_connections_active == 0 and up{job="mymail"} == 1
        for: 5m
        annotations:
          summary: "SMTP 服务无连接，可能已中断"

      - alert: MyMailQueueBacklog
        expr: mymail_queue_depth > 100
        for: 10m
        annotations:
          summary: "邮件队列堆积超过 100"

      - alert: MyMailHighErrorRate
        expr: rate(mymail_http_requests_total{status=~"5.."}[5m]) / rate(mymail_http_requests_total[5m]) > 0.05
        for: 5m
        annotations:
          summary: "HTTP 5xx 错误率超过 5%"

      - alert: MyMailDBLock
        expr: rate(mymail_http_requests_total{status="503"}[5m]) > 0.1
        for: 5m
        annotations:
          summary: "疑似数据库锁死（503 增多）"

      - alert: MyMailAuthFailures
        expr: rate(mymail_auth_attempts_total{result="failure"}[5m]) > 1
        for: 10m
        annotations:
          summary: "认证失败率过高（疑似暴力破解）"
```
