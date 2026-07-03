# RB-003：磁盘空间不足处理

| 项目 | 内容 |
|---|---|
| Runbook ID | RB-003 |
| 故障类型 | 磁盘空间不足 |
| 严重级别 | P1（高） |
| 影响范围 | 附件上传失败、邮件投递失败、数据库写入失败 |
| 预计恢复时间 | 15-60 分钟（取决于清理量） |

---

## 1. 症状

出现以下情况即判定为磁盘空间不足：

- 附件上传返回 507 Insufficient Storage 或 500 错误
- SMTP 接收邮件失败，日志报 `no space left on device`
- 数据库写入失败，日志报 `SQLITE_FULL` 或 `database disk image is malformed`
- 邮件队列停止消费（无法写入 mail_queue）
- `df -h` 显示 Use% ≥ 95%
- 监控告警 `node_filesystem_avail_bytes` 触发（可用空间 < 5%）
- 容器日志报 `write error: No space left on device`

---

## 2. 诊断步骤

### 2.1 检查磁盘使用

```bash
# 1. 查看各挂载点使用情况
df -h
# 关注 /app/data 所在分区，以及 /var/lib/docker（Docker 存储）

# 2. 查看 /app/data 各子目录大小
du -sh /app/data/*
# 预期输出示例：
#   50M   /app/data/mymail.db
#   8.0G  /app/data/maildir
#   15G   /app/data/attachments
#   200M  /app/data/audit.log

# 3. 查看磁盘 inode 使用（小文件过多耗尽 inode）
df -i
```

### 2.2 定位大文件

```bash
# 查找 /app/data 下大于 100MB 的文件
find /app/data -type f -size +100M -exec ls -lh {} \; | sort -k5 -h

# 查找 Docker 层占用
docker system df
# 预期：
#   TYPE        TOTAL  ACTIVE  SIZE      RECLAIMABLE
#   Images      4      4       2.5GB     500MB
#   Containers  4      4       100MB     0B
#   Volumes     4      4       20GB      0B
#   BuildCache  0      0       0B        0B

# 查看容器日志大小
docker inspect --format='{{.LogPath}}' mymail-app | xargs ls -lh
# 日志文件路径通常在 /var/lib/docker/containers/<id>/<id>-json.log
```

### 2.3 检查附件目录

```bash
# 附件目录通常是最大占用
du -sh /app/data/attachments/

# 统计附件文件数
find /app/data/attachments -type f | wc -l

# 查看最大的 20 个附件
find /app/data/attachments -type f -exec ls -lh {} \; | sort -k5 -h | tail -20
```

### 2.4 检查 maildir 目录

```bash
# maildir 目录存储邮件正文
du -sh /app/data/maildir/

# 按用户查看占用
du -sh /app/data/maildir/*/* | sort -h | tail -20

# 查看邮件数据库统计
sqlite3 -header -column /app/data/mymail.db \
  "SELECT u.username, u.email, u.storage_used, u.storage_limit,
          ROUND(u.storage_used*100.0/u.storage_limit, 2) AS usage_pct
   FROM users u ORDER BY u.storage_used DESC LIMIT 10;"
```

### 2.5 检查日志文件

```bash
# 审计日志大小
ls -lh /app/data/audit.log

# Docker 容器日志总大小
du -sh /var/lib/docker/containers/*/

# journald 日志大小（裸机）
journalctl --disk-usage

# Nginx 日志
ls -lh /var/log/nginx/
```

### 2.6 检查数据库临时文件

```bash
# SQLite 临时文件（排序、索引重建产生）
ls -lh /app/data/mymail.db*
# 关注 mymail.db-wal 是否过大（WAL 未 checkpoint）

# 强制 checkpoint 释放 WAL 空间
sqlite3 /app/data/mymail.db "PRAGMA wal_checkpoint(TRUNCATE);"
ls -lh /app/data/mymail.db-wal
```

---

## 3. 处理步骤

### 3.1 紧急清理（恢复服务优先）

#### Step 1：清理 Docker 资源

```bash
# 清理未使用的镜像、停止的容器、悬空卷（谨慎，确认无用后执行）
docker system prune -a --volumes
# 注意：会删除所有未运行的容器和未使用的镜像

# 仅清理悬空镜像（更安全）
docker image prune

# 清理构建缓存
docker builder prune

# 查看清理效果
df -h
```

#### Step 2：清理容器日志

```bash
# 截断容器日志（不停止容器）
# 找到日志文件
LOG_FILE=$(docker inspect --format='{{.LogPath}}' mymail-app)
sudo truncate -s 0 $LOG_FILE

# 对所有 mymail 容器执行
for c in mymail-app mymail-dovecot mymail-postfix mymail-nginx; do
    LOG=$(docker inspect --format='{{.LogPath}}' $c 2>/dev/null)
    [ -n "$LOG" ] && sudo truncate -s 0 $LOG
done

# 配置 Docker 日志轮转（永久解决）
# /etc/docker/daemon.json
{
  "log-driver": "json-file",
  "log-opts": {
    "max-size": "50m",
    "max-file": "5"
  }
}
sudo systemctl restart docker
```

#### Step 3：清理审计日志

```bash
# 归档并删除 90 天前的审计日志
sqlite3 /app/data/mymail.db << 'EOF'
.mode csv
.output /tmp/audit_archive.csv
SELECT * FROM audit_log WHERE timestamp < datetime('now', '-90 days');
.output stdout
DELETE FROM audit_log WHERE timestamp < datetime('now', '-90 days');
EOF
gzip /tmp/audit_archive.csv

# 截断 JSONL 审计文件（保留最近 30 天）
# 需停服后操作
docker compose -f deployments/docker/docker-compose.yml stop app
# 保留最近 30 天
awk -v cutoff="$(date -d '30 days ago' -Iseconds)" \
    '$0 ~ /"timestamp":"[0-9T:-]+/ { match($0, /"timestamp":"([^"]+)"/, ts); if (ts[1] > cutoff) print }' \
    /app/data/audit.log > /app/data/audit.log.new
mv /app/data/audit.log.new /app/data/audit.log
docker compose -f deployments/docker/docker-compose.yml start app
```

#### Step 4：清理垃圾邮件日志

```bash
# 删除 30 天前的 spam_log
sqlite3 /app/data/mymail.db \
  "DELETE FROM spam_log WHERE created_at < datetime('now', '-30 days');"

# VACUUM 回收空间
sqlite3 /app/data/mymail.db "VACUUM;"
```

#### Step 5：清理已删除邮件的物理文件

```bash
# 查找 is_deleted=1 的邮件对应的 maildir 文件
sqlite3 /app/data/mymail.db \
  "SELECT id, user_id, folder FROM messages WHERE is_deleted=1 LIMIT 100;" | while read line; do
    # 解析并删除对应 maildir 文件（需根据实际 maildir 结构）
    echo "Would delete maildir file for: $line"
done

# 清理 attachments 中无引用的文件
# 先导出有效 storage_path 列表
sqlite3 /app/data/mymail.db "SELECT DISTINCT storage_path FROM attachments;" > /tmp/valid_attachments.txt
# 遍历附件目录，删除不在列表中的文件
find /app/data/attachments -type f | while read f; do
    if ! grep -q "$f" /tmp/valid_attachments.txt; then
        echo "Removing orphan: $f"
        rm -f "$f"
    fi
done
```

### 3.2 归档旧邮件

若清理后仍不足，归档旧邮件到外部存储：

```bash
# 导出 1 年前的邮件到归档文件
sqlite3 /app/data/mymail.db << 'EOF'
.mode csv
.headers on
.output /tmp/mail_archive_$(date +%Y%m%d).csv
SELECT * FROM messages WHERE received_at < datetime('now', '-1 year');
.output stdout
EOF
gzip /tmp/mail_archive_*.csv

# 备份对应附件
mkdir -p /tmp/old_attachments
sqlite3 /app/data/mymail.db \
  "SELECT DISTINCT a.storage_path
   FROM attachments a JOIN messages m ON a.message_id=m.id
   WHERE m.received_at < datetime('now', '-1 year');" | while read path; do
    cp "$path" /tmp/old_attachments/ 2>/dev/null
done
tar -czf /tmp/old_attachments.tar.gz -C /tmp old_attachments

# 将归档移到外部存储后，删除数据库记录
sqlite3 /app/data/mymail.db \
  "DELETE FROM attachments WHERE message_id IN
   (SELECT id FROM messages WHERE received_at < datetime('now', '-1 year'));"
sqlite3 /app/data/mymail.db \
  "DELETE FROM messages WHERE received_at < datetime('now', '-1 year');"

# VACUUM 回收空间
sqlite3 /app/data/mymail.db "VACUUM;"
```

### 3.3 扩容磁盘

若清理后仍不足，需扩容：

```bash
# 云环境：扩容云盘（AWS EBS / 阿里云云盘）
# 1. 在云控制台扩容磁盘
# 2. 扩容文件系统
sudo resize2fs /dev/sda1        # ext4
sudo xfs_growfs /               # xfs

# Docker 环境：迁移 data 卷到更大磁盘
docker compose -f deployments/docker/docker-compose.yml stop app dovecot
# 备份
docker run --rm -v mymail-data:/data -v /mnt/backup:/backup alpine \
    tar -czf /backup/mymail-data.tar.gz -C /data .
# 创建新卷并恢复（在新磁盘上）
# 更新 docker-compose.yml 使用新卷
```

### 3.4 验证恢复

```bash
# 1. 磁盘空间确认
df -h /app/data
# 预期：Use% < 80%

# 2. 健康检查
curl http://localhost:3000/healthz
curl http://localhost:3000/readyz
# 预期：{"status":"ready"}

# 3. 附件上传测试
curl -X POST http://localhost:3000/api/mails/send \
  -H "Authorization: Bearer <jwt-token>" \
  -F "to=test@example.com" \
  -F "subject=Disk Test" \
  -F "body=Test" \
  -F "attachment=@/tmp/test.txt"
# 预期：发送成功

# 4. SMTP 接收测试
telnet mail.example.com 25
# 预期：220 ESMTP MyMail

# 5. 数据库写入测试
sqlite3 /app/data/mymail.db "INSERT INTO settings (key, value) VALUES ('disk_test', 'ok');"
sqlite3 /app/data/mymail.db "SELECT value FROM settings WHERE key='disk_test';"
sqlite3 /app/data/mymail.db "DELETE FROM settings WHERE key='disk_test';"
```

---

## 4. 预防措施

### 4.1 用户配额设置

```bash
# 查看所有用户配额
sqlite3 -header -column /app/data/mymail.db \
  "SELECT username, email, storage_limit, storage_used,
          ROUND(storage_used*100.0/storage_limit, 2) AS pct
   FROM users ORDER BY pct DESC;"

# 设置默认配额（如 100MB）
sqlite3 /app/data/mymail.db \
  "UPDATE users SET storage_limit=104857600 WHERE storage_limit > 104857600;"

# 对超配额用户发送提醒
sqlite3 /app/data/mymail.db \
  "SELECT email FROM users WHERE storage_used*100.0/storage_limit > 90;"
```

### 4.2 监控告警

```yaml
# Prometheus 告警规则
groups:
  - name: disk
    rules:
      - alert: DiskSpaceLow
        expr: node_filesystem_avail_bytes{mountpoint="/app/data"} / node_filesystem_size_bytes{mountpoint="/app/data"} < 0.2
        for: 5m
        annotations:
          summary: "磁盘可用空间低于 20%"

      - alert: DiskSpaceCritical
        expr: node_filesystem_avail_bytes{mountpoint="/app/data"} / node_filesystem_size_bytes{mountpoint="/app/data"} < 0.1
        for: 1m
        annotations:
          summary: "磁盘可用空间低于 10%（紧急）"

      - alert: UserQuotaExceeded
        expr: mymail_user_storage_usage_ratio > 0.9
        for: 10m
        annotations:
          summary: "用户存储使用率超过 90%"
```

### 4.3 定期清理任务

```cron
# crontab 每周清理
# 每周日凌晨 4 点清理垃圾邮件日志和过期审计日志
0 4 * * 0 sqlite3 /app/data/mymail.db "DELETE FROM spam_log WHERE created_at < datetime('now', '-30 days'); DELETE FROM audit_log WHERE timestamp < datetime('now', '-180 days');"

# 每周日凌晨 4:30 VACUUM
30 4 * * 0 sqlite3 /app/data/mymail.db "VACUUM;"

# 每天凌晨 3 点备份（备份会清理旧备份，保留 7 份）
0 3 * * * . /root/.backup-env && /path/to/mymail/mymail-go/scripts/backup.sh
```

### 4.4 Docker 日志轮转

```bash
# /etc/docker/daemon.json
{
  "log-driver": "json-file",
  "log-opts": {
    "max-size": "50m",
    "max-file": "5"
  }
}

# 应用配置
sudo systemctl restart docker
```

### 4.5 审计日志轮转

```bash
# /etc/logrotate.d/mymail-audit
/app/data/audit.log {
    weekly
    rotate 12
    compress
    delaycompress
    missingok
    notifempty
    copytruncate
}
```

---

## 5. 事后复盘

记录以下信息：

- 磁盘占满的根本原因（附件 / maildir / 日志 / Docker 层）
- 清理释放的空间量
- 用户影响（是否有邮件丢失或上传失败）
- 改进措施：
  - 配额上限是否合理
  - 清理任务是否生效
  - 告警是否提前触发
  - 是否需要扩容磁盘
  - 是否需要对象存储分离附件
