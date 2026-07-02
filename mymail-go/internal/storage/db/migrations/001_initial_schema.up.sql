-- 001_initial_schema.up.sql
-- 初始 schema：users + messages + attachments + send_log + settings
-- 修复：
--   - password 列改名为 password_hash（P0-1 关联）
--   - is_default_password 默认值 0（修复评估报告缺陷）
--   - messages 表加 ON DELETE CASCADE（修复外键级联缺失）
--   - 补全索引（idx_msg_user_read, idx_msg_user_received, idx_msg_message_id）

-- 用户表
CREATE TABLE users (
    id                  INTEGER PRIMARY KEY AUTOINCREMENT,
    username            TEXT NOT NULL UNIQUE,
    email               TEXT NOT NULL UNIQUE,
    password_hash       TEXT NOT NULL,
    display_name        TEXT,
    role                TEXT NOT NULL DEFAULT 'user',
    storage_limit       INTEGER NOT NULL DEFAULT 104857600,
    storage_used        INTEGER NOT NULL DEFAULT 0,
    is_active           INTEGER NOT NULL DEFAULT 1,
    login_fails         INTEGER NOT NULL DEFAULT 0,
    locked_until        DATETIME,
    is_default_password INTEGER NOT NULL DEFAULT 0,
    signature           TEXT,
    created_at          DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at          DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX idx_users_email ON users(email);
CREATE INDEX idx_users_username ON users(username);

-- 邮件表
CREATE TABLE messages (
    id            INTEGER PRIMARY KEY AUTOINCREMENT,
    user_id       INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    folder        TEXT NOT NULL DEFAULT 'INBOX',
    message_id    TEXT,
    uid           INTEGER,
    from_addr     TEXT NOT NULL,
    from_name     TEXT,
    to_addr       TEXT NOT NULL,
    cc_addr       TEXT,
    bcc_addr      TEXT,
    reply_to      TEXT,
    subject       TEXT,
    body_text     TEXT,
    body_html     TEXT,
    body_html_raw TEXT,
    is_read       INTEGER NOT NULL DEFAULT 0,
    is_starred    INTEGER NOT NULL DEFAULT 0,
    is_deleted    INTEGER NOT NULL DEFAULT 0,
    has_attach    INTEGER NOT NULL DEFAULT 0,
    attach_count  INTEGER NOT NULL DEFAULT 0,
    size_bytes    INTEGER NOT NULL DEFAULT 0,
    headers_raw   TEXT,
    in_reply_to   TEXT,
    flags         TEXT NOT NULL DEFAULT '[]',
    spam_score    INTEGER NOT NULL DEFAULT 0,
    spam_reasons  TEXT,
    received_at   DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX idx_msg_user_folder ON messages(user_id, folder);
CREATE INDEX idx_msg_received ON messages(received_at);
CREATE INDEX idx_msg_user_read ON messages(user_id, is_read);
CREATE INDEX idx_msg_user_received ON messages(user_id, received_at DESC);
CREATE INDEX idx_msg_message_id ON messages(message_id);

-- 附件表
CREATE TABLE attachments (
    id           INTEGER PRIMARY KEY AUTOINCREMENT,
    message_id   INTEGER NOT NULL REFERENCES messages(id) ON DELETE CASCADE,
    filename     TEXT NOT NULL,
    mime_type    TEXT,
    size_bytes   INTEGER,
    storage_path TEXT NOT NULL,
    created_at   DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX idx_attach_message ON attachments(message_id);

-- 发送日志表
CREATE TABLE send_log (
    id         INTEGER PRIMARY KEY AUTOINCREMENT,
    user_id    INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    to_addr    TEXT NOT NULL,
    subject    TEXT,
    status     TEXT NOT NULL DEFAULT 'pending',
    error_msg  TEXT,
    sent_at    DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX idx_send_log_user ON send_log(user_id);
CREATE INDEX idx_send_log_sent ON send_log(sent_at);

-- 系统配置表
CREATE TABLE settings (
    key         TEXT PRIMARY KEY,
    value       TEXT,
    updated_at  DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);

-- 审计日志表（企业级）
CREATE TABLE audit_log (
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
