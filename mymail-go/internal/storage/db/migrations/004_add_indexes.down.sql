-- 004_add_indexes.down.sql
-- 索引回滚（SQLite 不支持 DROP INDEX IF EXISTS 的 IF EXISTS 部分在旧版本，这里直接 DROP）
DROP INDEX IF EXISTS idx_rules_user_priority;
DROP INDEX IF EXISTS idx_api_keys_last_used;
DROP INDEX IF EXISTS idx_api_keys_active;
DROP INDEX IF EXISTS idx_send_log_status;
DROP INDEX IF EXISTS idx_users_locked;
DROP INDEX IF EXISTS idx_users_active;
DROP INDEX IF EXISTS idx_users_role;
DROP INDEX IF EXISTS idx_msg_folder;
DROP INDEX IF EXISTS idx_msg_from_addr;
DROP INDEX IF EXISTS idx_msg_user_deleted;
DROP INDEX IF EXISTS idx_msg_user_starred;
