-- 004_add_indexes.up.sql
-- 补全所有缺失索引，优化查询性能
-- 这些索引在 001-003 中部分已创建，此处补全遗漏的

-- messages 表补充索引（用于搜索与统计）
CREATE INDEX IF NOT EXISTS idx_msg_user_starred ON messages(user_id, is_starred) WHERE is_starred = 1;
CREATE INDEX IF NOT EXISTS idx_msg_user_deleted ON messages(user_id, is_deleted) WHERE is_deleted = 1;
CREATE INDEX IF NOT EXISTS idx_msg_from_addr ON messages(from_addr);
CREATE INDEX IF NOT EXISTS idx_msg_folder ON messages(folder);

-- users 表补充索引
CREATE INDEX IF NOT EXISTS idx_users_role ON users(role);
CREATE INDEX IF NOT EXISTS idx_users_active ON users(is_active) WHERE is_active = 1;
CREATE INDEX IF NOT EXISTS idx_users_locked ON users(locked_until) WHERE locked_until IS NOT NULL;

-- send_log 表补充索引
CREATE INDEX IF NOT EXISTS idx_send_log_status ON send_log(status);

-- api_keys 表补充索引
CREATE INDEX IF NOT EXISTS idx_api_keys_active ON api_keys(is_active) WHERE is_active = 1;
CREATE INDEX IF NOT EXISTS idx_api_keys_last_used ON api_keys(last_used_at);

-- mail_rules 表补充索引
CREATE INDEX IF NOT EXISTS idx_rules_user_priority ON mail_rules(user_id, priority);
