# MyMail 迁移指南：Node.js → Go

本文档描述 MyMail 邮件平台从 Node.js 版本（`mymail-platform`）迁移到 Go 版本（`mymail-go`）的完整流程。

- 源项目：`mymail-platform`（Node.js + better-sqlite3 + Express + Knex）
- 目标项目：`mymail-go`（Go 1.25 + modernc.org/sqlite + Gin + SQL 迁移）
- 数据库：SQLite（路径 `/app/data/mymail.db`，Docker 环境）

> ⚠️ 迁移是不可逆的方向性操作。务必先完成【迁移前检查清单】并验证备份可恢复后再开始。

---

## 1. 迁移前检查清单

开始迁移前，逐项确认以下条件已满足：

### 1.1 环境准备

- [ ] 原 Node.js 服务仍在运行，且数据完整可读
- [ ] 已部署 Go 版本二进制或 Docker 镜像（参见 [DEPLOY.md](./DEPLOY.md)）
- [ ] Go 版本与 Node.js 版本使用相同的 `DOMAIN`、`MAIL_HOST` 配置
- [ ] Dovecot/Postfix/Nginx 配置已切换为 Go 版本（`mymail-go/config/`）

### 1.2 版本确认

- [ ] Node.js 版本 schema 为 `002_add_apikeys_rules` 已应用（含 `is_default_password` 列）
- [ ] Go 版本迁移文件 001-004 就绪（`internal/storage/db/migrations/`）
- [ ] BcryptCost 确认一致：Go 版本 `BcryptCost=12`，与 Node.js 版本兼容（`$2a$` / `$2b$` / `$2y$` 前缀 bcrypt 均可互通）

### 1.3 备份验证

- [ ] 已设置 `BACKUP_PASSWORD` 环境变量
- [ ] 已执行 `./scripts/backup.sh` 生成加密备份
- [ ] 已验证备份可解密恢复（见下方【验证备份可恢复】）

### 1.4 磁盘空间

- [ ] 可用磁盘空间 ≥ 当前 `data/` 目录大小的 3 倍（备份 + 迁移临时文件 + WAL 增长）
- [ ] 检查命令：

```bash
# 查看 data 目录大小
du -sh /app/data/

# 查看磁盘可用空间
df -h /app/data
```

### 1.5 维护窗口

- [ ] 已通知用户维护时间窗口（建议 ≥ 30 分钟）
- [ ] 已在 Nginx 配置维护页面或关闭外部访问
- [ ] 已停止 SMTP 接收（避免迁移期间新邮件写入）

### 验证备份可恢复

```bash
# 1. 解密备份到临时文件
openssl enc -d -aes-256-cbc -pbkdf2 \
  -in backups/mymail_backup_YYYYMMDD_HHMMSS.tar.gz.enc \
  -out /tmp/verify_backup.tar.gz \
  -pass env:BACKUP_PASSWORD

# 2. 校验完整性
sha256sum -c backups/mymail_backup_YYYYMMDD_HHMMSS.tar.gz.sha256

# 3. 列出内容（不解压，仅确认）
tar -tzf /tmp/verify_backup.tar.gz | head -20

# 4. 验证完毕清理
rm /tmp/verify_backup.tar.gz
```

---

## 2. Schema 差异说明

Go 版本相对 Node.js 版本的 schema 变更如下。迁移脚本需处理这些差异。

### 2.1 新增列

| 表 | 新增列 | 类型 | 默认值 | 说明 |
|---|---|---|---|---|
| `messages` | `body_html_raw` | TEXT | NULL | 原始 HTML（未经 bluemonday 净化），用于查看源码 |
| `messages` | `spam_score` | INTEGER | 0 | 垃圾邮件评分 |
| `messages` | `spam_reasons` | TEXT | NULL | 评分原因（JSON 数组） |
| `api_keys` | `key_prefix` | TEXT | NOT NULL | API Key 前 12 字符，用于 O(1) 索引查找（P1-3 修复） |

### 2.2 新增表

| 表 | 迁移文件 | 说明 |
|---|---|---|
| `audit_log` | 001 | 审计日志（actor/action/resource/result），企业级安全审计 |
| `greylist` | 003 | 灰名单（原 Node.js 在 `init-db.js` 中创建，未纳入 Knex 迁移；Go 版统一到 003） |
| `spam_log` | 003 | 垃圾邮件日志（列名与 Node.js 版本不同，见下方） |
| `mail_queue` | 003 | 发送队列（Node.js 版本无此表，邮件失败即丢；Go 版增加队列重试） |

### 2.3 spam_log 列名变更

> ⚠️ 这是迁移中最易出错的地方。Node.js 版本与 Go 版本的 `spam_log` 表列名不同。

| Node.js 版本 | Go 版本 | 说明 |
|---|---|---|
| `sender_ip` | `ip` | 发送方 IP |
| `sender_addr` | `sender` | 发件人地址 |
| `recipient_addr` | `recipient` | 收件人地址 |
| `spam_score` | `score` | 垃圾评分 |
| `reasons` | `reasons` | 未变 |
| `action` | `action` | 未变 |

迁移时需做列名映射，否则数据错位。

### 2.4 索引补全

迁移 004 补全了大量索引（`idx_msg_user_starred`、`idx_msg_from_addr`、`idx_users_role` 等）。这些索引在迁移时自动创建，无需手动处理。

### 2.5 外键级联

Go 版本在 `messages`、`attachments`、`send_log`、`api_keys`、`mail_rules`、`mail_queue` 表均添加了 `ON DELETE CASCADE`。Node.js 版本部分表缺失级联删除。迁移后删除用户会自动清理关联数据。

---

## 3. 迁移流程

整体流程：**备份 → 停服 → 迁移数据 → 验证 → 切换流量 → 启动 Go 服务**

### 3.1 Step 1：备份原数据

```bash
# 在 Node.js 项目目录下执行
cd /path/to/mymail-platform

# 设置加密密码
export BACKUP_PASSWORD='your-strong-backup-password'

# 执行备份（生成 AES-256-CBC 加密文件 + SHA256 校验和）
./scripts/backup.sh

# 确认备份文件
ls -lh backups/
# 预期输出：
#   mymail_backup_YYYYMMDD_HHMMSS.tar.gz.enc
#   mymail_backup_YYYYMMDD_HHMMSS.tar.gz.sha256
```

### 3.2 Step 2：停止 Node.js 服务

```bash
# 停止 Node.js 后端
sudo systemctl stop mymail

# 停止 Dovecot（避免迁移期间 IMAP 客户端写入）
sudo systemctl stop dovecot

# 确认进程已退出
ps aux | grep -E 'node.*server|dovecot' | grep -v grep
```

### 3.3 Step 3：运行数据迁移

Go 项目内置的迁移器会在首次启动时自动执行 SQL 迁移文件（001-004），将 schema 升级到 Go 版本。但**数据迁移**（原表数据搬运、列名映射、API Key 重建）需要手动执行。

#### 方式 A：使用 migrate-data.go 辅助脚本（推荐）

将原数据库复制到 Go 项目后，运行迁移辅助脚本：

```bash
# 1. 复制原数据库到 Go 项目（保留原件不动）
cp /path/to/mymail-platform/data/mymail.db /path/to/mymail-go/data/mymail.db
cp -r /path/to/mymail-platform/data/maildir/* /path/to/mymail-go/data/maildir/
cp -r /path/to/mymail-platform/data/attachments/* /path/to/mymail-go/data/attachments/

# 2. 运行数据迁移脚本（处理列名变更、补全新增列默认值）
cd /path/to/mymail-go
go run ./scripts/migrate-data.go -source data/mymail.db
```

`migrate-data.go` 执行以下操作：

1. **补列**：为 `messages` 表添加 `body_html_raw`（从 `body_html` 复制）、`spam_score`（默认 0）、`spam_reasons`（默认 NULL）
2. **补列**：为 `api_keys` 表添加 `key_prefix`（旧数据无法恢复，填占位值 `mk_legacy_`）
3. **重命名 spam_log 列**：SQLite 不支持直接 RENAME COLUMN（3.25 之前），脚本通过创建新表 + INSERT SELECT 迁移
4. **创建新表**：`audit_log`、`mail_queue`（空表，无需迁移数据）

#### 方式 B：手动 SQL 迁移

若无 `migrate-data.go`，可手动执行以下 SQL（要求 SQLite ≥ 3.35）：

```bash
# 进入 sqlite3 CLI
sqlite3 /path/to/mymail-go/data/mymail.db
```

```sql
-- 1. 为 messages 表补列（SQLite 3.35+ 支持 ADD COLUMN）
ALTER TABLE messages ADD COLUMN body_html_raw TEXT;
ALTER TABLE messages ADD COLUMN spam_score INTEGER NOT NULL DEFAULT 0;
ALTER TABLE messages ADD COLUMN spam_reasons TEXT;

-- 2. 将已有 body_html 复制到 body_html_raw（保留原始 HTML）
UPDATE messages SET body_html_raw = body_html WHERE body_html_raw IS NULL AND body_html IS NOT NULL;

-- 3. 为 api_keys 表补列（key_prefix 无法从 hash 恢复，填占位值）
ALTER TABLE api_keys ADD COLUMN key_prefix TEXT NOT NULL DEFAULT 'mk_legacy_';
CREATE INDEX IF NOT EXISTS idx_api_keys_prefix ON api_keys(key_prefix) WHERE is_active = 1;

-- 4. 迁移 spam_log（列名变更）
-- 4.1 重命名旧表
ALTER TABLE spam_log RENAME TO spam_log_old;
-- 4.2 创建新表（与 Go 版 003 迁移一致）
CREATE TABLE spam_log (
    id          INTEGER PRIMARY KEY AUTOINCREMENT,
    sender      TEXT,
    recipient   TEXT,
    ip          TEXT,
    score       INTEGER NOT NULL,
    reasons     TEXT,
    action      TEXT NOT NULL,
    created_at  DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);
CREATE INDEX idx_spam_log_created ON spam_log(created_at);
CREATE INDEX idx_spam_log_sender ON spam_log(sender);
CREATE INDEX idx_spam_log_action ON spam_log(action);
-- 4.3 搬运数据（列名映射）
INSERT INTO spam_log (id, sender, recipient, ip, score, reasons, action, created_at)
SELECT id, sender_addr, recipient_addr, sender_ip, spam_score, reasons, action, created_at
FROM spam_log_old;
-- 4.4 删除旧表
DROP TABLE spam_log_old;

-- 5. 创建 audit_log 表（空表）
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
);
CREATE INDEX idx_audit_timestamp ON audit_log(timestamp);
CREATE INDEX idx_audit_actor ON audit_log(actor_type, actor_id);
CREATE INDEX idx_audit_action ON audit_log(action);

-- 6. 创建 mail_queue 表（空表，Node.js 版本无此表）
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
);
CREATE INDEX idx_queue_status ON mail_queue(status, next_retry_at);
CREATE INDEX idx_queue_user ON mail_queue(user_id);

-- 7. 创建 schema_migrations 表，标记 001-003 已应用（避免 Go 启动时重复执行）
CREATE TABLE IF NOT EXISTS schema_migrations (
    version INTEGER PRIMARY KEY,
    applied_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);
INSERT INTO schema_migrations (version) VALUES (1), (2), (3);
-- 注意：004_add_indexes 会让 Go 自动补全索引，这里不标记 version=4

.quit
```

### 3.4 Step 4：启动 Go 服务（自动完成 schema 迁移）

```bash
cd /path/to/mymail-go

# 启动 Go 服务（自动执行未应用的迁移，即 004_add_indexes）
./mymail
# 或开发模式
make dev
```

观察日志，确认迁移完成：

```
INFO 数据库已就绪 path=/app/data/mymail.db
INFO 执行迁移 version=4 name=add_indexes
INFO 迁移完成 version=4 name=add_indexes
INFO MyMail 已启动 port=3000
```

### 3.5 Step 5：验证数据完整性

```bash
# 1. 对比用户数
sqlite3 /path/to/mymail-go/data/mymail.db "SELECT COUNT(*) FROM users;"

# 2. 对比邮件数
sqlite3 /path/to/mymail-go/data/mymail.db "SELECT COUNT(*) FROM messages;"

# 3. 对比附件数
sqlite3 /path/to/mymail-go/data/mymail.db "SELECT COUNT(*) FROM attachments;"

# 4. 对比 API Key 数
sqlite3 /path/to/mymail-go/data/mymail.db "SELECT COUNT(*) FROM api_keys;"

# 5. 对比规则数
sqlite3 /path/to/mymail-go/data/mymail.db "SELECT COUNT(*) FROM mail_rules;"

# 6. 对比 spam_log 数（验证列名迁移未丢数据）
sqlite3 /path/to/mymail-go/data/mymail.db "SELECT COUNT(*) FROM spam_log;"

# 7. 验证新表存在且有正确结构
sqlite3 /path/to/mymail-go/data/mymail.db ".schema audit_log"
sqlite3 /path/to/mymail-go/data/mymail.db ".schema mail_queue"

# 8. 验证 schema_migrations 记录
sqlite3 /path/to/mymail-go/data/mymail.db "SELECT * FROM schema_migrations ORDER BY version;"
```

### 3.6 Step 6：功能验证

```bash
# 1. 健康检查
curl http://localhost:3000/healthz
# 预期：{"status":"alive","time":"..."}

# 2. 就绪检查（含 DB 连通性）
curl http://localhost:3000/readyz
# 预期：{"status":"ready","checks":{"startup":"ready","db":"up"}}

# 3. 登录验证（用原管理员账号）
curl -X POST http://localhost:3000/api/auth/login \
  -H "Content-Type: application/json" \
  -d '{"username":"admin","password":"your-password"}'
# 预期：返回 JWT token

# 4. 邮件列表验证
curl http://localhost:3000/api/mails?page=1&limit=10 \
  -H "Authorization: Bearer <your-jwt-token>"
# 预期：返回原有邮件列表

# 5. IMAP 登录验证（Dovecot 读取 Go 版数据库）
# 用邮件客户端连接 IMAP 993 端口，登录验证邮件可见
```

### 3.7 Step 7：切换流量

验证全部通过后，切换流量到 Go 服务：

```bash
# 1. 启动 Dovecot（指向 Go 版数据库）
sudo systemctl start dovecot

# 2. 重载 Nginx（代理到 Go 服务端口 3000）
sudo nginx -t && sudo systemctl reload nginx

# 3. 观察 5 分钟，确认无异常
sudo journalctl -u mymail -f --since "1 min ago"
```

---

## 4. API Key 迁移注意事项

> ⚠️ **API Key 的 `key_prefix` 不可恢复，必须重建。**

### 4.1 原因

Go 版本采用 P1-3 修复方案：API Key 查找通过 `key_prefix` 索引（O(1)）而非全表 bcrypt 比对（O(n)）。但 `key_prefix` 是明文 key 的前 12 字符，而数据库只存 bcrypt hash，**无法从 hash 反推明文前缀**。

因此，Node.js 版本创建的 API Key 迁移后 `key_prefix` 只能填占位值（`mk_legacy_`），这些 Key 将无法通过索引查找。

### 4.2 处理方案

**方案 A（推荐）：强制所有用户重建 API Key**

1. 迁移后，将旧 API Key 标记为失效：

```sql
UPDATE api_keys SET is_active = 0 WHERE key_prefix = 'mk_legacy_';
```

2. 通知用户登录 Web 界面或调用 API 重新生成 Key：

```bash
curl -X POST http://localhost:3000/api/v1/apikeys \
  -H "Authorization: Bearer <jwt-token>" \
  -H "Content-Type: application/json" \
  -d '{"name":"my-key","scopes":["send"]}'
```

3. 用户更新客户端配置使用新 Key。

**方案 B：保留旧 Key 但接受性能下降**

不执行上述 `UPDATE`，旧 Key 仍可通过全表扫描验证（Go 版本兼容此路径），但每次 API 调用延迟增加（bcrypt 比对次数 = 该用户 Key 数量）。

> 建议在迁移完成后 7 天内完成所有 API Key 轮换，然后执行方案 A 的清理。

### 4.3 验证 API Key

```bash
# 用新 Key 调用 API
curl http://localhost:3000/api/v1/mails \
  -H "X-API-Key: mk_<新生成的key>"
# 预期：返回邮件列表
```

---

## 5. 回滚流程

若迁移后发现问题需要回滚到 Node.js 版本：

### 5.1 前提

- 原始加密备份完整可用
- Node.js 版本代码与配置未删除

### 5.2 回滚步骤

```bash
# 1. 停止 Go 服务
sudo systemctl stop mymail

# 2. 停止 Dovecot
sudo systemctl stop dovecot

# 3. 恢复原始数据库（从备份）
cd /path/to/mymail-platform
export BACKUP_PASSWORD='your-backup-password'

# 解密备份
openssl enc -d -aes-256-cbc -pbkdf2 \
  -in backups/mymail_backup_YYYYMMDD_HHMMSS.tar.gz.enc \
  -out /tmp/restore.tar.gz \
  -pass env:BACKUP_PASSWORD

# 校验完整性
sha256sum -c backups/mymail_backup_YYYYMMDD_HHMMSS.tar.gz.sha256

# 恢复数据（覆盖 Go 版修改过的文件）
tar -xzf /tmp/restore.tar.gz -C /path/to/mymail-platform/

# 清理临时文件
rm /tmp/restore.tar.gz

# 4. 启动 Node.js 服务
sudo systemctl start mymail

# 5. 启动 Dovecot（指向 Node.js 数据库）
sudo systemctl start dovecot

# 6. 重载 Nginx（代理回 Node.js 端口）
sudo nginx -t && sudo systemctl reload nginx

# 7. 验证恢复
curl http://localhost:3000/api/auth/login \
  -H "Content-Type: application/json" \
  -d '{"username":"admin","password":"your-password"}'
```

### 5.3 回滚后数据丢失说明

回滚会丢失迁移窗口期间的所有变更（新邮件、新用户、设置变更等）。因此回滚应尽快决策，避免长时间数据分歧。

---

## 6. 常见问题 FAQ

### Q1：迁移后登录失败，提示密码错误？

**A**：检查 `password_hash` 列是否完整迁移。Go 版本用 `bcrypt.CompareHashAndPassword`，兼容 `$2a$` / `$2b$` / `$2y$` 前缀。若 Node.js 用了其他哈希算法（如 argon2），需重新哈希所有密码。

```sql
-- 检查 password_hash 格式
SELECT id, username, substr(password_hash, 1, 4) FROM users LIMIT 5;
-- 预期：所有行返回 $2a$ 或 $2b$ 或 $2y$
```

### Q2：迁移后 IMAP 登录失败？

**A**：Dovecot 的 `dovecot-sql.conf` 中 `default_pass_scheme = BLF-CRYPT`，要求 `password_hash` 是 bcrypt 格式。确认 Dovecot 指向 Go 版数据库路径（`/app/data/mymail.db`），且 UID/GID 与 Dockerfile 一致（Docker 环境 1000，裸机 setup.sh 环境 5000）。

### Q3：迁移后邮件可见但附件打不开？

**A**：附件存储在文件系统（`data/attachments/`），数据库只存 `storage_path`。确认附件文件已复制到 Go 项目对应目录，且路径相对/绝对一致。

```bash
# 检查附件文件是否存在
ls -la /path/to/mymail-go/data/attachments/

# 对比数据库中的路径
sqlite3 /path/to/mymail-go/data/mymail.db "SELECT storage_path FROM attachments LIMIT 5;"
```

### Q4：迁移后 `spam_log` 数据为空？

**A**：检查列名映射是否正确执行。Node.js 版本列名是 `sender_ip`/`sender_addr`/`recipient_addr`/`spam_score`，Go 版本是 `ip`/`sender`/`recipient`/`score`。若直接 `INSERT INTO spam_log SELECT * FROM spam_log_old` 会导致列错位或失败。

### Q5：Go 服务启动报 "JWT_SECRET 必须设置"？

**A**：Go 版本强制校验 `JWT_SECRET`（P0-8 修复），必须设置且长度 ≥ 32 字符，不能是占位符。在 `.env` 中设置：

```bash
# 生成 32 字符随机串
openssl rand -hex 32
# 填入 .env
JWT_SECRET=<生成的串>
```

### Q6：迁移后旧 API Key 报 401？

**A**：见【API Key 迁移注意事项】。旧 Key 的 `key_prefix` 为占位值，无法索引查找。需重建 Key 或接受全表扫描性能下降。

### Q7：迁移脚本执行中报 "database is locked"？

**A**：确保已停止所有访问数据库的进程（Node.js 服务、Dovecot）。SQLite 写锁是串行的。若仍报错，删除 WAL/SHM 文件后重试：

```bash
# 停服后删除 WAL 文件（数据库会自动重建）
rm /path/to/mymail-go/data/mymail.db-wal
rm /path/to/mymail-go/data/mymail.db-shm
```

### Q8：能否不停服迁移？

**A**：不推荐。SQLite 单文件数据库不支持在线 schema 变更的并发安全。停服窗口约 10-30 分钟（取决于数据量）。若必须不停服，可先用 `sqlite3 .backup` 创建热备份，再在备份上执行迁移，最后切换。
