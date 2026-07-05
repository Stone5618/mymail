-- 修复历史用户：将 Web 登录用的 bcrypt hash 同步到 dovecot_password_hash。
--
-- 原因：早期版本注册时未生成 dovecot_password_hash，导致这些用户无法通过
-- IMAP 登录。Dovecot 2.3+ 的 BLF-CRYPT 兼容 Go bcrypt 生成的 $2a$ 前缀哈希，
-- 因此可以直接复用 password_hash 字段。
UPDATE users
SET dovecot_password_hash = password_hash
WHERE (dovecot_password_hash = '' OR dovecot_password_hash IS NULL)
  AND password_hash != '';
