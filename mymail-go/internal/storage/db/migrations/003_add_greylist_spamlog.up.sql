-- 003_add_greylist_spamlog.up.sql
-- 灰名单 + 垃圾邮件日志 + 发送队列
-- 修复：
--   - P0-9：统一 greylist 表结构（替代 init-db.js 与 migrations 不一致）
--   - spam_log 加索引（修复索引缺失）
--   - 新增 mail_queue 表（解决无队列导致邮件丢失问题）

-- 灰名单表
CREATE TABLE greylist (
    key         TEXT PRIMARY KEY,
    first_seen  INTEGER NOT NULL,
    allowed     INTEGER NOT NULL DEFAULT 0
);

CREATE INDEX idx_greylist_first_seen ON greylist(first_seen);

-- 垃圾邮件日志表
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

-- 发送队列表（解决原项目无队列、邮件失败即丢的问题）
CREATE TABLE mail_queue (
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
