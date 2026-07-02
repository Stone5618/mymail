-- 002_add_apikeys_rules.up.sql
-- API Key + 邮件规则
-- 修复：
--   - api_keys 加 key_prefix 列 + 索引（P1-3：解决 O(n) bcrypt 性能瓶颈）
--   - mail_rules 加复合索引（修复索引缺失）

-- API Key 表
CREATE TABLE api_keys (
    id            INTEGER PRIMARY KEY AUTOINCREMENT,
    user_id       INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    name          TEXT NOT NULL,
    key_hash      TEXT NOT NULL,
    key_prefix    TEXT NOT NULL,
    scopes        TEXT NOT NULL DEFAULT '["send"]',
    rate_limit    INTEGER NOT NULL DEFAULT 60,
    is_active     INTEGER NOT NULL DEFAULT 1,
    last_used_at  DATETIME,
    created_at    DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);

-- P1-3 修复：用 key_prefix 索引加速查找（O(1) 而非 O(n)）
CREATE INDEX idx_api_keys_prefix ON api_keys(key_prefix) WHERE is_active = 1;
CREATE INDEX idx_api_keys_user ON api_keys(user_id);

-- 邮件规则表
CREATE TABLE mail_rules (
    id          INTEGER PRIMARY KEY AUTOINCREMENT,
    user_id     INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    name        TEXT NOT NULL,
    priority    INTEGER NOT NULL DEFAULT 0,
    conditions  TEXT NOT NULL,
    actions     TEXT NOT NULL,
    is_active   INTEGER NOT NULL DEFAULT 1,
    created_at  DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);

-- 修复索引缺失
CREATE INDEX idx_rules_user_active ON mail_rules(user_id, is_active);
CREATE INDEX idx_rules_priority ON mail_rules(priority);
