# RB-001：SMTP 服务中断处理

| 项目 | 内容 |
|---|---|
| Runbook ID | RB-001 |
| 故障类型 | SMTP 服务中断 |
| 严重级别 | P1（高） |
| 影响范围 | 外部邮件无法接收 / 本域邮件无法发送 |
| 预计恢复时间 | 15-30 分钟 |

---

## 1. 症状

出现以下任一情况即判定为 SMTP 服务中断：

- 外部发件人向本域发邮件，收到退信（bounce）或邮件长期不到达
- 本域用户发送邮件后，收件人长期未收到
- 监控告警 `MyMailSMTPDown` 触发（`mymail_smtp_connections_active == 0`）
- `telnet mail.example.com 25` 连接超时或被拒绝
- 队列深度告警 `MyMailQueueBacklog` 触发（`mymail_queue_depth > 100`）
- 用户反馈"邮件发不出去"

---

## 2. 诊断步骤

### 2.1 确认服务状态

```bash
# 1. 检查 app 容器是否运行
docker ps | grep mymail-app
# 若未运行 → 启动：docker compose -f deployments/docker/docker-compose.yml up -d app

# 2. 检查健康状态
docker inspect --format='{{.State.Health.Status}}' mymail-app
# 预期：healthy

# 3. 检查 Go 后端日志是否有 SMTP 启动信息
docker logs mymail-app 2>&1 | grep -i "smtp" | head -20
# 预期：包含 "SMTP 接收器" 启动日志
```

### 2.2 检查端口监听

```bash
# 检查 25 端口是否监听
docker exec mymail-app netstat -tlnp 2>/dev/null | grep :25
# 或宿主机
ss -tlnp | grep :25
# 预期：LISTEN 0.0.0.0:25

# 检查 Postfix 587 端口（出站中继）
docker exec mymail-postfix netstat -tlnp 2>/dev/null | grep :587
```

### 2.3 外部连通性测试

```bash
# 从外部测试 SMTP 25 端口
telnet mail.example.com 25
# 预期：220 <hostname> ESMTP MyMail
# 若超时 → 防火墙或端口未暴露

# 手动 SMTP 会话测试
telnet mail.example.com 25
EHLO test.com
MAIL FROM:<test@test.com>
RCPT TO:<admin@example.com>
DATA
Subject: Test

Test message
.
QUIT
# 预期：250 OK（接收成功）
# 若 450/550 → 被灰名单拒绝或被反垃圾拦截
```

### 2.4 检查熔断器状态

出站 SMTP（发往外部）使用 gobreaker 熔断器。若熔断器处于 open 状态，所有外发请求快速失败。

```bash
# 查看熔断器相关日志
docker logs mymail-app 2>&1 | grep -iE "circuit|breaker|open|half" | tail -20

# 检查出站失败率
docker logs mymail-app 2>&1 | grep -iE "smtp.*send.*fail|relay.*fail" | tail -20

# 熔断器配置（.env）
# SMTP_CIRCUIT_BREAKER_ENABLED=true
# SMTP_CIRCUIT_BREAKER_FAILURE_RATIO=0.6   （失败率 > 60% 触发熔断）
# SMTP_CIRCUIT_BREAKER_TIMEOUT_MS=30000    （open 状态 30 秒后半开）
```

### 2.5 检查队列堆积

```bash
# 队列状态汇总
sqlite3 -header -column /app/data/mymail.db \
  "SELECT status, COUNT(*) AS count, MIN(created_at) AS oldest
   FROM mail_queue GROUP BY status;"

# 查看失败邮件的错误原因
sqlite3 -header -column /app/data/mymail.db \
  "SELECT id, to_addrs, subject, attempts, error_msg
   FROM mail_queue WHERE status='failed' ORDER BY created_at DESC LIMIT 10;"
```

### 2.6 检查 DNS 与 MX 记录

```bash
# MX 记录
dig MX example.com +short
# 预期：10 mail.example.com.

# A 记录
dig A mail.example.com +short
# 预期：服务器公网 IP

# PTR 反向解析（外发邮件易被拒的常见原因）
dig -x <服务器IP> +short
# 预期：mail.example.com.

# SPF
dig TXT example.com +short
# 预期：包含 v=spf1 ip4:<服务器IP>
```

### 2.7 检查反垃圾拦截

```bash
# 查看被拦截的邮件
sqlite3 -header -column /app/data/mymail.db \
  "SELECT created_at, sender, recipient, ip, score, reasons, action
   FROM spam_log WHERE action != 'delivered'
   ORDER BY created_at DESC LIMIT 20;"

# 检查灰名单拦截
sqlite3 -header -column /app/data/mymail.db \
  "SELECT key, first_seen, allowed FROM greylist WHERE allowed=0 ORDER BY first_seen DESC LIMIT 20;"
```

---

## 3. 处理步骤

### 3.1 入站 SMTP 中断（外部邮件进不来）

#### 情况 A：app 容器未运行或端口 25 未监听

```bash
# 重启 app 服务
docker compose -f deployments/docker/docker-compose.yml restart app

# 等待健康检查通过（约 30 秒）
sleep 30
docker inspect --format='{{.State.Health.Status}}' mymail-app

# 验证端口
docker exec mymail-app netstat -tlnp 2>/dev/null | grep :25
```

#### 情况 B：防火墙阻断

```bash
# 检查 iptables
sudo iptables -L -n | grep 25

# 放行 25 端口
sudo iptables -A INPUT -p tcp --dport 25 -j ACCEPT

# 云厂商安全组也需放行 25 端口（AWS/阿里云等通常默认封禁 25）
```

#### 情况 C：被反垃圾误杀

```bash
# 临时关闭反垃圾（紧急恢复）
# 编辑 .env：
#   FEATURE_SPAM_FILTER=false
#   FEATURE_GREYLIST=false
docker compose -f deployments/docker/docker-compose.yml restart app

# 或将误杀的发送方加入白名单（通过管理界面或直接操作 settings 表）
sqlite3 /app/data/mymail.db \
  "INSERT OR REPLACE INTO settings (key, value) VALUES ('spam_whitelist', '[\"trusted-sender.com\"]');"
```

### 3.2 出站 SMTP 中断（本域邮件发不出）

#### 情况 A：Postfix 中继故障

```bash
# 重启 Postfix
docker compose -f deployments/docker/docker-compose.yml restart postfix

# 检查 Postfix 日志
docker logs mymail-postfix 2>&1 | tail -30
# 常见错误：
#   - "Connection refused" → 上游 MX 不可达
#   - "Relay access denied" → ALLOWED_SENDER_DOMAINS 未配置
#   - "554 Spam" → 被 upstream 反垃圾拦截
```

#### 情况 B：熔断器 open 状态

熔断器 open 后会持续 30 秒快速失败，然后进入半开状态试探。

```bash
# 等待 30 秒让熔断器自动进入半开
sleep 35

# 若仍不恢复，检查 Postfix 是否正常
docker logs mymail-postfix 2>&1 | tail -20

# 临时关闭熔断器（紧急）
# 编辑 .env：SMTP_CIRCUIT_BREAKER_ENABLED=false
docker compose -f deployments/docker/docker-compose.yml restart app
```

#### 情况 C：外域 MX 不可达

```bash
# 测试目标域 MX
dig MX gmail.com +short
# 预期：5 gmail-smtp-in.l.google.com.

# 测试到目标 MX 的连通性
telnet gmail-smtp-in.l.google.com 25
# 若超时 → 你的服务器 IP 被目标域封禁，或出口 25 被运营商封禁
```

### 3.3 清理积压队列

```bash
# 重置失败邮件为 pending，触发重试
sqlite3 /app/data/mymail.db \
  "UPDATE mail_queue SET status='pending', attempts=0, next_retry_at=NULL, error_msg=NULL
   WHERE status='failed';"

# 队列 worker 每 5 秒轮询，观察队列深度下降
watch -n 5 "sqlite3 /app/data/mymail.db \"SELECT status, COUNT(*) FROM mail_queue GROUP BY status;\""

# 通过指标监控
watch -n 5 "curl -s http://localhost:3000/metrics | grep mymail_queue_depth"
```

### 3.4 DNS 问题修复

```bash
# MX 记录缺失或错误 → 在 DNS 管理面板修复
# 验证修复生效
dig MX example.com +short

# PTR 反向解析缺失 → 联系 ISP/云厂商配置
# 验证
dig -x <服务器IP> +short

# 注意：DNS 传播需时间（TTL），最长 48 小时
```

---

## 4. 验证恢复

### 4.1 入站验证

```bash
# 1. telnet 测试
telnet mail.example.com 25
# 预期：220 <hostname> ESMTP MyMail

# 2. 发送测试邮件
# 从外部邮箱（如 Gmail）发邮件到 admin@example.com
# 在 Web 界面或 IMAP 客户端确认收到

# 3. 检查日志确认接收
docker logs mymail-app 2>&1 | grep -i "received\|delivered" | tail -10
```

### 4.2 出站验证

```bash
# 1. 通过 API 发送测试邮件
curl -X POST http://localhost:3000/api/mails/send \
  -H "Authorization: Bearer <jwt-token>" \
  -H "Content-Type: application/json" \
  -d '{
    "to": ["test@gmail.com"],
    "subject": "SMTP Recovery Test",
    "body": "Test from MyMail"
  }'

# 2. 检查队列是否正常消费
sqlite3 /app/data/mymail.db \
  "SELECT id, status, attempts, sent_at FROM mail_queue ORDER BY id DESC LIMIT 5;"
# 预期：status='sent'

# 3. 在外部邮箱确认收到测试邮件

# 4. 指标确认
curl -s http://localhost:3000/metrics | grep mymail_mails_sent
# 预期：mymail_mails_sent_total{status="sent"} 数值增长
```

### 4.3 队列清空验证

```bash
# 等待 5 分钟，确认队列深度回归正常
sqlite3 /app/data/mymail.db \
  "SELECT status, COUNT(*) FROM mail_queue GROUP BY status;"
# 预期：pending/sending 数量为 0 或个位数

curl -s http://localhost:3000/metrics | grep mymail_queue_depth
# 预期：mymail_queue_depth < 10
```

---

## 5. 事后复盘

故障恢复后，记录以下信息供复盘：

- 故障发生时间与持续时间
- 影响范围（多少用户/邮件受影响）
- 根本原因
- 诊断过程与耗时
- 处理步骤有效性
- 改进措施（监控告警优化、配置调整、容灾改进）

常见改进方向：

- 增加 SMTP 端口外部探测告警（从外部定时 telnet 25 端口）
- DNS 记录变更后自动验证
- 队列深度告警阈值调低（如 > 50 即告警）
- 配置备用出站 SMTP 中继（避免单点 Postfix 故障）
