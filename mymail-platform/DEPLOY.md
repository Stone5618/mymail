# 📧 MyMail 部署文档

## 快速部署

```bash
# 快速部署
cd mail-platform
sudo bash scripts/setup.sh
# 访问 Web: https://{YOUR_DOMAIN}
# Admin: stone@{YOUR_DOMAIN} / {YOUR_PASSWORD}
```

## 手动部署

### 1. 安装依赖

```bash
# Node.js 依赖
npm install

# 系统依赖
apt install nginx dovecot-core dovecot-imapd sqlite3 openssl
```

### 2. 配置环境

```bash
cp .env.example .env
# 编辑 .env 填入你的配置
```

### 3. 初始化数据库

```bash
node scripts/init-db.js
```

### 4. 构建 CSS

```bash
npx tailwindcss -i ./public/css/input.css -o ./public/css/style.css --minify
```

### 5. 配置 Nginx

参见 `scripts/setup.sh` 中的 Nginx 配置段。

### 6. 配置 Dovecot

参见 `scripts/setup.sh` 中的 Dovecot 配置段。

### 7. 启动服务

```bash
# 开发模式
npm run dev

# 生产模式
npm start

# 或使用 systemd
sudo systemctl start mymail
```

## DNS 配置

在 DuckDNS 或你的 DNS 管理面板中添加：

```dns
{YOUR_DOMAIN}.        MX   10  mail.{YOUR_DOMAIN}.
mail.{YOUR_DOMAIN}.   A    {YOUR_SERVER_IP}
{YOUR_DOMAIN}.        TXT  "v=spf1 ip4:{YOUR_SERVER_IP} mx ~all"
_dmarc.{YOUR_DOMAIN}. TXT  "v=DMARC1; p=none"
default._domainkey.{YOUR_DOMAIN}. TXT  "v=DKIM1; k=rsa; p={YOUR_PUBLIC_KEY}"
```

### 生成 DKIM 密钥

```bash
bash scripts/gen-dkim.sh
```

## DuckDNS 自动更新

```bash
# 添加到 crontab
crontab -e

# 每 5 分钟更新 IP
*/5 * * * * curl -s "https://www.duckdns.org/update?domains={YOUR_SUBDOMAIN}&token={YOUR_TOKEN}&ip=" > /dev/null
```

## 备份

```bash
bash scripts/backup.sh
```

备份文件保存在 `backups/` 目录，自动保留最近 7 份。

## 客户端配置

### Outlook / Foxmail

| 设置 | 值 |
|------|-----|
| 收件服务器 | mail.{YOUR_DOMAIN} |
| IMAP 端口 | 993 (SSL) |
| 发件服务器 | mail.{YOUR_DOMAIN} |
| SMTP 端口 | 465 (SSL) |
| 用户名 | 完整邮箱地址 |
| 密码 | 网页登录密码 |

## 故障排除

### 端口 25 被封

```bash
# 检测端口 25
telnet smtp.qq.com 25

# 如果被封，联系云厂商解封，或使用 SMTP 中继
```

### 邮件发送失败

1. 检查 DNS 记录是否正确
2. 检查 SSL 证书是否有效
3. 查看日志: `journalctl -u mymail -f`

### Dovecot 连接失败

```bash
# 检查 Dovecot 状态
systemctl status dovecot

# 测试 IMAP
openssl s_client -connect mail.{YOUR_DOMAIN}:993
```
