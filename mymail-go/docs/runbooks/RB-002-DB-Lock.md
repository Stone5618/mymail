# RB-002：数据库锁死处理

| 项目 | 内容 |
|---|---|
| Runbook ID | RB-002 |
| 故障类型 | SQLite 数据库锁死 |
| 严重级别 | P1（高） |
| 影响范围 | API 响应超时、邮件投递失败、登录失败 |
| 预计恢复时间 | 10-30 分钟 |

---

## 1. 症状

出现以下情况即判定为数据库锁死：

- API 请求大量超时（> 5 秒）或返回 503
- 日志频繁出现 `SQLITE_BUSY`、`database is locked`、`SQLITE_LOCKED` 错误
- 监控告警 `MyMailDBLock` 触发（503 错误率升高）
- `/readyz` 返回 `{"status":"not_ready","checks":{"db":"down"}}`
- 邮件队列停止消费（worker 无法写入 DB）
- 用户登录卡住（bcrypt 验证后无法更新 login_fails）

---

## 2. 诊断步骤

### 2.1 确认锁死

```bash
# 1. 检查就绪探针
curl http://localhost:3000/readyz
# 锁死时预期：{"status":"not_ready","checks":{"db":"down"}}

# 2. 检查日志中的 SQLITE_BUSY 错误
docker logs mymail-app 2>&1 | grep -iE "busy|locked|sqlite" | tail -30

# 3. 检查在途请求数（是否堆积）
curl -s http://localhost:3000/metrics | grep mymail_http_requests_in_flight
# 锁死时预期：数值持续升高不下降

# 4. 检查 DB 连接指标
curl -s http://localhost:3000/metrics | grep mymail_db_connections
```

### 2.2 检查 WAL 文件

```bash
# 查看 WAL 文件大小（过大说明 checkpoint 失败）
ls -lh /app/data/mymail.db*
# 预期：
#   mymail.db        （主库）
#   mymail.db-wal    （WAL，正常 < 100MB）
#   mymail.db-shm    （共享内存索引）

# 若 WAL 文件 > 1GB，说明 checkpoint 长期未执行
# 可能原因：长事务持有读锁，阻止 checkpoint
```

### 2.3 检查连接数

```bash
# Go 版本配置 MaxOpenConns=1，正常只有 1 个连接
# 检查是否有外部进程占用数据库
sudo lsof /app/data/mymail.db
# 预期：只有 mymail 进程

# 若有其他进程（如手动 sqlite3 会话未关闭），会持有锁
# 处理：终止该进程
sudo kill <pid>
```

### 2.4 检查长事务

```bash
# SQLite 不像 PostgreSQL 有 pg_stat_activity
# 通过日志推断长事务
docker logs mymail-app 2>&1 | grep -iE "transaction|tx|begin|commit" | tail -20

# 检查是否有未完成的迁移事务
docker logs mymail-app 2>&1 | grep -iE "migrate|migration" | tail -10

# 检查是否有人在手动执行长 SQL
# 通过 lsof 确认谁在访问数据库
sudo lsof /app/data/mymail.db
```

### 2.5 检查磁盘空间

```bash
# 磁盘满会导致 SQLite 无法写入，表现为锁死
df -h /app/data
# 若 Use% > 95% → 见 RB-003-Disk-Full.md
```

---

## 3. 处理步骤

### 3.1 轻度锁死（偶发 SQLITE_BUSY）

若只是偶发 `SQLITE_BUSY` 且 `busy_timeout=5000` 能自动恢复，无需干预。若频繁出现：

#### Step 1：重置连接池

```bash
# 重启 app 容器，重置数据库连接
docker compose -f deployments/docker/docker-compose.yml restart app

# 等待健康检查
sleep 30
curl http://localhost:3000/healthz
curl http://localhost:3000/readyz
```

#### Step 2：执行 WAL Checkpoint

```bash
# 重启后立即 checkpoint，减小 WAL 文件
sqlite3 /app/data/mymail.db "PRAGMA wal_checkpoint(TRUNCATE);"

# 确认 WAL 文件已收缩
ls -lh /app/data/mymail.db-wal
# 预期：< 10MB
```

### 3.2 重度锁死（数据库完全无响应）

#### Step 1：强制停止所有访问 DB 的进程

```bash
# 停止 app
docker compose -f deployments/docker/docker-compose.yml stop app

# 停止 dovecot（也会访问 DB）
docker compose -f deployments/docker/docker-compose.yml stop dovecot

# 确认无进程占用 DB
sudo lsof /app/data/mymail.db
# 预期：无输出
```

#### Step 2：清理 WAL 与 SHM 文件

```bash
# 备份当前状态（以防万一）
cp /app/data/mymail.db /app/data/mymail.db.locked.bak
cp /app/data/mymail.db-wal /app/data/mymail.db-wal.bak 2>/dev/null || true

# 删除 WAL 和 SHM（数据库会从主库重建）
rm -f /app/data/mymail.db-wal
rm -f /app/data/mymail.db-shm

# ⚠️ 警告：删除 WAL 会丢失未 checkpoint 的写入
# 仅在确认主库数据完整后执行
```

#### Step 3：完整性检查

```bash
# 快速完整性检查
sqlite3 /app/data/mymail.db "PRAGMA quick_check;"
# 预期：ok

# 若报错，尝试恢复
sqlite3 /app/data/mymail.db "PRAGMA integrity_check;"

# 若 integrity_check 报错，从备份恢复
# 见 DEPLOY.md 第 11 节【备份策略】
```

#### Step 4：重启服务

```bash
docker compose -f deployments/docker/docker-compose.yml up -d app dovecot

# 等待健康检查
sleep 30
docker inspect --format='{{.State.Health.Status}}' mymail-app
curl http://localhost:3000/readyz
# 预期：{"status":"ready","checks":{"db":"up"}}
```

### 3.3 VACUUM 优化（恢复后执行）

锁死后数据库可能有碎片，执行 VACUUM 优化：

```bash
# 停服后执行（需独占锁）
docker compose -f deployments/docker/docker-compose.yml stop app

sqlite3 /app/data/mymail.db "VACUUM;"

# 查看优化前后大小
ls -lh /app/data/mymail.db

# 重启
docker compose -f deployments/docker/docker-compose.yml start app
```

### 3.4 调整 busy_timeout

若锁死频繁发生，可临时调大 `busy_timeout`：

```bash
# 修改 internal/storage/db/db.go 中的 PRAGMA
# 将 busy_timeout(5000) 改为 busy_timeout(10000)（10 秒）
# 同时修改 pragmas 列表

# 重新构建
make build

# 或在 .env 中添加（需代码支持读取该配置）
# DB_BUSY_TIMEOUT_MS=10000
```

> ⚠️ 调大 busy_timeout 只是缓解症状，根本解决需排查长事务来源。

---

## 4. 预防措施

### 4.1 WAL 模式（已启用）

Go 版本默认启用 WAL 模式（`db.go` 中 `PRAGMA journal_mode=WAL`），读不阻塞写，写不阻塞读。这是 SQLite 并发的最佳实践。

```bash
# 验证 WAL 模式
sqlite3 /app/data/mymail.db "PRAGMA journal_mode;"
# 预期：wal
```

### 4.2 连接池配置（已优化）

```go
// db.go 当前配置
db.SetMaxOpenConns(1)    // 单连接，避免多连接写冲突
db.SetMaxIdleConns(1)
db.SetConnMaxLifetime(0)
```

> SQLite 写锁是数据库级（非行级），多连接并发写必然冲突。单连接串行化写，配合 WAL 的并发读，是 SQLite 的最优配置。

### 4.3 定期 WAL Checkpoint

```bash
# 定时任务：每小时 checkpoint 一次
crontab -e
# 添加：
0 * * * * sqlite3 /app/data/mymail.db "PRAGMA wal_checkpoint(PASSIVE);" >> /var/log/mymail-wal.log 2>&1
```

### 4.4 监控 WAL 文件大小

```bash
# 告警脚本：WAL > 500MB 报警
cat > /usr/local/bin/check-wal.sh << 'EOF'
#!/bin/bash
WAL_SIZE=$(stat -c%s /app/data/mymail.db-wal 2>/dev/null || echo 0)
if [ $WAL_SIZE -gt 524288000 ]; then
    echo "WARNING: WAL file size $((WAL_SIZE/1024/1024))MB exceeds 500MB"
    exit 1
fi
EOF
chmod +x /usr/local/bin/check-wal.sh

# crontab 每 10 分钟检查
*/10 * * * * /usr/local/bin/check-wal.sh >> /var/log/wal-alert.log
```

### 4.5 避免长事务

代码层面避免长事务：

- 邮件发送不要放在 DB 事务内（SMTP 网络IO 慢）
- 批量操作分批提交（每批 100 条）
- 避免在事务中调用外部服务（DNS 查询、HTTP 调用）

### 4.6 避免外部进程访问

- 不要在 mymail 运行时用 `sqlite3` CLI 长时间连接数据库
- 备份用 `.backup` 命令（在线备份，不持锁）而非 `cp`

```bash
# 在线备份（不持锁，推荐）
sqlite3 /app/data/mymail.db ".backup /tmp/mymail-backup.db"
```

---

## 5. 验证恢复

```bash
# 1. 就绪检查
curl http://localhost:3000/readyz
# 预期：{"status":"ready","checks":{"db":"up"}}

# 2. 登录测试
curl -X POST http://localhost:3000/api/auth/login \
  -H "Content-Type: application/json" \
  -d '{"username":"admin","password":"your-password"}'
# 预期：返回 JWT token

# 3. 邮件列表查询
curl http://localhost:3000/api/mails \
  -H "Authorization: Bearer <jwt-token>"
# 预期：正常返回

# 4. 队列消费恢复
sqlite3 /app/data/mymail.db "SELECT status, COUNT(*) FROM mail_queue GROUP BY status;"
curl -s http://localhost:3000/metrics | grep mymail_queue_depth

# 5. SQLITE_BUSY 错误消失
docker logs mymail-app 2>&1 --since 5m | grep -iE "busy|locked"
# 预期：无输出

# 6. WAL 文件大小正常
ls -lh /app/data/mymail.db-wal
# 预期：< 100MB
```

---

## 6. 事后复盘

记录以下信息：

- 锁死发生时间与持续时间
- WAL 文件大小峰值
- 是否有外部进程占用 DB
- 是否有长事务（迁移、批量操作）
- 磁盘空间是否充足
- 改进措施：
  - 增加 WAL 文件大小监控告警
  - 优化长事务代码
  - 调整 busy_timeout
  - 考虑是否需要迁移到 PostgreSQL（若锁死频繁）
