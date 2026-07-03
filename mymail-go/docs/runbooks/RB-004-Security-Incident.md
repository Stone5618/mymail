# RB-004：安全事件响应

| 项目 | 内容 |
|---|---|
| Runbook ID | RB-004 |
| 事件类型 | 安全事件 |
| 严重级别 | P0（最高） |
| 影响范围 | 账户泄露 / API Key 泄露 / XSS 攻击 / 未授权访问 |
| 响应时效 | 立即响应（< 15 分钟） |

---

## 1. 症状与触发条件

出现以下任一情况即触发安全事件响应：

### 1.1 异常登录

- 短时间内大量登录失败（暴力破解，同一 IP 5 分钟内失败 > 5 次）
- 异地登录（用户习惯地区外的 IP 登录成功）
- 非工作时间管理员登录
- 监控告警 `MyMailAuthFailures` 触发

### 1.2 API Key 泄露

- API Key 出现在公开代码仓库（GitHub/GitLab）
- API Key 调用来源 IP 异常（与历史使用 IP 不符）
- API Key 调用量激增（被滥用发信）

### 1.3 XSS 攻击

- 邮件正文包含 `<script>` 标签或可疑 JavaScript
- 用户反馈打开邮件后浏览器异常
- 审计日志出现 HTML 注入尝试

### 1.4 其他

- 未授权的管理员操作（audit_log 中异常 admin 操作）
- 数据库被非法修改（用户角色被提升、密码被改）
- 服务器被入侵迹象（异常进程、未知 SSH key）

---

## 2. 响应流程

按 **隔离 → 取证 → 清除 → 恢复 → 复盘** 五步执行。

### 2.1 Step 1：隔离（立即，< 5 分钟）

#### 2.1.1 阻断攻击源 IP

```bash
# 通过防火墙封禁攻击 IP
sudo iptables -A INPUT -s <attacker-ip> -j DROP

# 持久化规则（Ubuntu/Debian）
sudo apt-get install -y iptables-persistent
sudo netfilter-persistent save

# 或使用 Nginx deny（更灵活）
# 在 config/nginx/default.conf 添加：
#   deny <attacker-ip>;
sudo nginx -t && sudo systemctl reload nginx
```

#### 2.1.2 锁定受影响账户

```bash
# 锁定受影响的用户账户
sqlite3 /app/data/mymail.db \
  "UPDATE users SET is_active=0, locked_until=datetime('now', '+24 hours')
   WHERE email='compromised@example.com';"

# 锁定所有管理员账户（如管理后台被入侵）
sqlite3 /app/data/mymail.db \
  "UPDATE users SET is_active=0 WHERE role='admin';"
```

#### 2.1.3 吊销受影响的 API Key

```bash
# 吊销特定用户的全部 API Key
sqlite3 -header -column /app/data/mymail.db \
  "SELECT id, name, key_prefix, is_active, last_used_at FROM api_keys WHERE user_id=<uid>;"

sqlite3 /app/data/mymail.db "UPDATE api_keys SET is_active=0 WHERE user_id=<uid>;"

# 吊销全部 API Key（紧急，仅在被大规模滥用时）
sqlite3 /app/data/mymail.db "UPDATE api_keys SET is_active=0;"
```

#### 2.1.4 必要时停止服务

若攻击正在进行且无法阻断（如 0day 漏洞），停止服务：

```bash
docker compose -f deployments/docker/docker-compose.yml stop app

# 保留数据库和日志供取证（不要删除）
```

### 2.2 Step 2：取证（15-30 分钟）

> ⚠️ 取证原则：先备份再分析，保留原始证据，所有操作记录在案。

#### 2.2.1 备份当前状态

```bash
# 立即备份数据库和日志（快照）
export BACKUP_PASSWORD='forensic-backup-password'
cd /path/to/mymail/mymail-go
./scripts/backup.sh

# 额外备份审计日志和容器日志
cp /app/data/audit.log /app/data/audit.forensic.$(date +%Y%m%d%H%M%S).log
docker logs mymail-app > /tmp/mymail-app-forensic-$(date +%Y%m%d%H%M%S).log 2>&1
docker logs mymail-nginx > /tmp/mymail-nginx-forensic-$(date +%Y%m%d%H%M%S).log 2>&1
```

#### 2.2.2 查询审计日志

```bash
# 查询攻击 IP 的所有操作
sqlite3 -header -column /app/data/mymail.db << 'EOF
SELECT timestamp, actor_type, actor_id, actor_ip, action, resource_type, resource_id, result, detail
FROM audit_log
WHERE actor_ip='<attacker-ip>'
ORDER BY timestamp;
EOF

# 查询受影响用户的所有操作
sqlite3 -header -column /app/data/mymail.db << 'EOF
SELECT timestamp, actor_type, actor_ip, action, resource_type, result
FROM audit_log
WHERE actor_id=<user_id>
ORDER BY timestamp DESC LIMIT 100;
EOF

# 查询管理员操作（排查权限提升）
sqlite3 -header -column /app/data/mymail.db << 'EOF
SELECT timestamp, actor_id, actor_ip, action, resource_type, resource_id, result, detail
FROM audit_log
WHERE actor_type='admin'
  AND timestamp > datetime('now', '-7 days')
ORDER BY timestamp DESC;
EOF

# 查询登录成功记录（异常 IP）
sqlite3 -header -column /app/data/mymail.db << 'EOF
SELECT timestamp, actor_id, actor_ip, detail
FROM audit_log
WHERE action='login' AND result='success'
  AND actor_ip NOT IN ('<已知合法IP1>', '<已知合法IP2>')
ORDER BY timestamp DESC LIMIT 50;
EOF

# 查询 API Key 使用记录
sqlite3 -header -column /app/data/mymail.db << 'EOF
SELECT timestamp, actor_id, actor_ip, action, resource_type, result
FROM audit_log
WHERE actor_type='api'
  AND timestamp > datetime('now', '-24 hours')
ORDER BY timestamp DESC;
EOF
```

#### 2.2.3 检查数据完整性

```bash
# 检查是否有用户角色被非法提升
sqlite3 -header -column /app/data/mymail.db \
  "SELECT id, username, email, role, updated_at FROM users WHERE role='admin' ORDER BY updated_at DESC;"

# 检查是否有异常的用户创建
sqlite3 -header -column /app/data/mymail.db \
  "SELECT id, username, email, role, created_at FROM users WHERE created_at > datetime('now', '-7 days');"

# 检查是否有异常的设置变更
sqlite3 -header -column /app/data/mymail.db \
  "SELECT key, value, updated_at FROM settings ORDER BY updated_at DESC LIMIT 20;"

# 检查 spam_log 中是否有 XSS 注入尝试
sqlite3 -header -column /app/data/mymail.db \
  "SELECT created_at, sender, reasons FROM spam_log
   WHERE reasons LIKE '%script%' OR reasons LIKE '%javascript%'
   ORDER BY created_at DESC LIMIT 20;"
```

#### 2.2.4 检查 Nginx 访问日志

```bash
# 查询攻击 IP 的 HTTP 请求
docker logs mymail-nginx 2>&1 | grep "<attacker-ip>" | tail -50

# 查询异常的 API 调用（如批量拉取用户列表）
docker logs mymail-nginx 2>&1 | grep -E "GET /api/admin" | tail -30

# 查询 SQL 注入尝试
docker logs mymail-nginx 2>&1 | grep -iE "union|select|drop|insert|--" | tail -20
```

### 2.3 Step 3：清除（30-60 分钟）

#### 2.3.1 强制改密

```bash
# 强制重置受影响用户密码（生成新 hash）
go run -tags=tools ./scripts/hash-password.py "new-temp-password"
# 输出：$2a$12$...

sqlite3 /app/data/mymail.db \
  "UPDATE users SET password_hash='\$2a\$12\$...', is_default_password=1, login_fails=0, locked_until=NULL
   WHERE email='compromised@example.com';"

# 强制所有用户改密（大规模泄露时）
sqlite3 /app/data/mymail.db \
  "UPDATE users SET is_default_password=1;"
```

#### 2.3.2 轮换 JWT Secret

> ⚠️ 轮换 JWT Secret 会使所有现有 token 失效，所有用户需重新登录。

```bash
# 生成新 secret
NEW_SECRET=$(openssl rand -hex 32)
echo "New JWT_SECRET: $NEW_SECRET"

# 更新 .env
sed -i "s/^JWT_SECRET=.*/JWT_SECRET=$NEW_SECRET/" /path/to/mymail/mymail-go/.env

# 重启服务使新 secret 生效
docker compose -f deployments/docker/docker-compose.yml restart app
```

#### 2.3.3 重新生成 API Key

```bash
# 已吊销的 Key 无法恢复，需用户重新创建
# 通过 API 创建新 Key（用户登录后）
curl -X POST http://localhost:3000/api/v1/apikeys \
  -H "Authorization: Bearer <new-jwt-token>" \
  -H "Content-Type: application/json" \
  -d '{"name":"replacement-key","scopes":["send"]}'
# 返回明文 key（仅此一次）
```

#### 2.3.4 清除恶意邮件

```bash
# 查找包含 XSS 的邮件
sqlite3 -header -column /app/data/mymail.db \
  "SELECT id, from_addr, subject, received_at FROM messages
   WHERE body_html_raw LIKE '%<script>%' OR body_html_raw LIKE '%javascript:%'
   ORDER BY received_at DESC;"

# 标记为已删除（不物理删除，保留证据）
sqlite3 /app/data/mymail.db \
  "UPDATE messages SET is_deleted=1, folder='Junk'
   WHERE body_html_raw LIKE '%<script>%' OR body_html_raw LIKE '%javascript:%';"

# 验证 Go 版 XSS 净化（bluemonday）已生效
# body_html 应为净化后版本，body_html_raw 是原始版本
sqlite3 -header -column /app/data/mymail.db \
  "SELECT id, substr(body_html, 1, 100) AS sanitized, substr(body_html_raw, 1, 100) AS raw
   FROM messages WHERE body_html_raw LIKE '%<script>%' LIMIT 5;"
# 预期：sanitized 中无 <script>，raw 中有
```

#### 2.3.5 修复漏洞

根据取证结果修复根本漏洞：

- **XSS**：确认 bluemonday 净化对所有用户输入生效
- **SQL 注入**：确认所有 DAO 用参数化查询（`?` 占位符）
- **IDOR**：确认 ownership 中间件对所有资源访问校验所有权
- **暴力破解**：增加登录限流（Nginx zone=login 已配置）
- **API Key 泄露**：加强 key 管理，定期轮换

### 2.4 Step 4：恢复

```bash
# 1. 解锁合法用户
sqlite3 /app/data/mymail.db \
  "UPDATE users SET is_active=1, locked_until=NULL WHERE email='legitimate@example.com';"

# 2. 确认服务运行
curl http://localhost:3000/healthz
curl http://localhost:3000/readyz

# 3. 移除防火墙阻断（确认攻击已停止后）
sudo iptables -D INPUT -s <attacker-ip> -j DROP

# 4. 通知受影响用户
# - 密码已被重置，需用临时密码登录并修改
# - API Key 已吊销，需重新创建
# - 检查发件箱是否有未授权邮件
```

### 2.5 Step 5：复盘

见第 4 节【报告模板】。

---

## 3. 工具集

### 3.1 audit_log 查询工具

```bash
# 按时间范围查询
sqlite3 -header -column /app/data/mymail.db \
  "SELECT * FROM audit_log
   WHERE timestamp BETWEEN '2026-07-01 00:00:00' AND '2026-07-01 23:59:59'
   ORDER BY timestamp;"

# 按操作类型查询
sqlite3 -header -column /app/data/mymail.db \
  "SELECT timestamp, actor_ip, actor_id, result, detail
   FROM audit_log WHERE action='password_change' ORDER BY timestamp DESC;"

# 导出审计日志为 CSV（供调查报告）
sqlite3 -csv -header /app/data/mymail.db \
  "SELECT * FROM audit_log WHERE timestamp > datetime('now', '-7 days');" \
  > /tmp/audit-export-$(date +%Y%m%d).csv
```

### 3.2 强制改密工具

```bash
# 生成 bcrypt hash（Go 工具，cost=12）
cat > /tmp/hash-pw.go << 'EOF'
package main

import (
    "fmt"
    "golang.org/x/crypto/bcrypt"
    "os"
)

func main() {
    pw := os.Args[1]
    h, _ := bcrypt.GenerateFromPassword([]byte(pw), 12)
    fmt.Print(string(h))
}
EOF
cd /path/to/mymail/mymail-go
go run /tmp/hash-pw.go "new-password"
# 输出：$2a$12$...
```

### 3.3 吊销 API Key

```bash
# 吊销单把 Key
sqlite3 /app/data/mymail.db "UPDATE api_keys SET is_active=0 WHERE id=<key_id>;"

# 吊销用户全部 Key
sqlite3 /app/data/mymail.db "UPDATE api_keys SET is_active=0 WHERE user_id=<uid>;"

# 吊销全部 Key（紧急）
sqlite3 /app/data/mymail.db "UPDATE api_keys SET is_active=0;"

# 查看活跃 Key 列表
sqlite3 -header -column /app/data/mymail.db \
  "SELECT k.id, u.email, k.name, k.key_prefix, k.last_used_at, k.created_at
   FROM api_keys k JOIN users u ON k.user_id=u.id
   WHERE k.is_active=1 ORDER BY k.last_used_at DESC;"
```

### 3.4 封禁 IP

```bash
# iptables 封禁
sudo iptables -A INPUT -s <ip> -j DROP
sudo netfilter-persistent save

# Nginx 封禁（编辑 config/nginx/default.conf）
# 在 server 块内添加：
#   deny <ip>;
sudo nginx -t && sudo systemctl reload nginx

# 批量封禁（从日志提取攻击 IP）
for ip in $(docker logs mymail-nginx 2>&1 | grep "login.*failure" | awk '{print $1}' | sort -u); do
    sudo iptables -A INPUT -s $ip -j DROP
done
```

### 3.5 会话清理

```bash
# JWT 是无状态的，无法主动失效单会话
# 唯一方式：轮换 JWT_SECRET（使所有 token 失效）
NEW_SECRET=$(openssl rand -hex 32)
sed -i "s/^JWT_SECRET=.*/JWT_SECRET=$NEW_SECRET/" /path/to/mymail/mymail-go/.env
docker compose -f deployments/docker/docker-compose.yml restart app

# WebSocket 连接可通过重启服务断开
# 或通过 admin API 强制断开（若实现）
```

---

## 4. 报告模板

事件处理完成后，填写以下报告：

```markdown
# 安全事件报告

## 事件基本信息
- 事件编号：SEC-YYYY-NNN
- 报告时间：YYYY-MM-DD HH:MM
- 报告人：<姓名>
- 事件严重级别：P0 / P1 / P2
- 事件状态：已解决 / 调查中 / 待跟进

## 事件描述
- 发现时间：YYYY-MM-DD HH:MM
- 发现方式：监控告警 / 用户报告 / 安全扫描 / 其他
- 事件类型：账户泄露 / API Key 泄露 / XSS 攻击 / SQL 注入 / 未授权访问 / 其他
- 影响范围：<受影响的用户数、数据量、服务>

## 攻击时间线
| 时间 | 事件 |
|---|---|
| YYYY-MM-DD HH:MM | 攻击者首次从 IP <x.x.x.x> 登录失败 |
| YYYY-MM-DD HH:MM | 攻击者登录成功 |
| YYYY-MM-DD HH:MM | 执行了 <action> 操作 |
| YYYY-MM-DD HH:MM | 被监测发现 |
| YYYY-MM-DD HH:MM | 完成隔离 |

## 根本原因
<详细描述漏洞或薄弱环节>

## 影响评估
- 受影响用户：<数量>
- 受影响数据：<数据类型与量级>
- 数据泄露：<是否泄露、泄露内容>
- 服务中断时长：<时长>

## 处理措施
1. 隔离：<封禁 IP、锁定账户、吊销 Key>
2. 取证：<备份、日志分析、数据完整性检查>
3. 清除：<改密、轮换 secret、删恶意数据>
4. 恢复：<解锁合法用户、重启服务>
5. 漏洞修复：<具体修复措施>

## 改进措施
- [ ] 短期：<1 周内完成，如加强监控、调整配置>
- [ ] 中期：<1 月内完成，如代码审计、渗透测试>
- [ ] 长期：<3 月内完成，如架构改进、安全培训>

## 证据归档
- 数据库快照：<备份文件路径>
- 审计日志导出：<文件路径>
- 容器日志导出：<文件路径>
- 取证报告：<文件路径>

## 相关人员
- 事件负责人：<姓名>
- 技术支持：<姓名>
- 管理层通知：<姓名/时间>
```

---

## 5. 附录：安全机制参考

MyMail Go 版本内置的安全机制：

| 机制 | 实现位置 | 说明 |
|---|---|---|
| bcrypt 密码哈希 | `internal/crypto/bcrypt.go` | BcryptCost=12，兼容 `$2a$/2b$/2y$` |
| JWT 认证 | `internal/crypto/jwt.go` | access 24h + refresh 720h，P0-8 强制校验 secret |
| API Key | `internal/crypto/apikey.go` | mk_ 前缀 + key_prefix 索引（P1-3 O(1) 查找） |
| XSS 净化 | `internal/sanitize/html.go` | bluemonday 过滤危险 HTML |
| SQL 参数化 | `internal/storage/dao/` | 全部用 `?` 占位符，无字符串拼接 |
| IDOR 防护 | `internal/httpapi/middleware/ownership.go` | 资源访问校验所有权 |
| 审计日志 | `internal/audit/audit.go` | 双写 DB + JSONL，记录 actor/action/result |
| ReDoS 防护 | `internal/rules/` | pattern 长度限制 + 编译超时（P1-7） |
| CORS 白名单 | `internal/httpapi/middleware/cors.go` | 生产环境强制配置（P1-2） |
| 限流 | `internal/httpapi/middleware/api_rate_limit.go` | 登录端点 5r/m，通用 10r/s |
| SMTP 灰名单 | `internal/smtp/greylist.go` | 5 分钟延迟重试 |
| SPF/DNSBL | `internal/spam/` | 反垃圾过滤 |
| 熔断器 | `internal/resilience/circuit_breaker.go` | 出站 SMTP 故障隔离 |
| 输入校验 | `internal/util/validator.go` | 请求参数校验 |
| 安全头 | `config/nginx/default.conf` | HSTS / X-Frame-Options / nosniff |
