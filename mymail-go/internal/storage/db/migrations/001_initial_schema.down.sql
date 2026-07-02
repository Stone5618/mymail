-- 001_initial_schema.down.sql
-- 回滚初始 schema
DROP TABLE IF EXISTS audit_log;
DROP TABLE IF EXISTS send_log;
DROP TABLE IF EXISTS attachments;
DROP TABLE IF EXISTS messages;
DROP TABLE IF EXISTS settings;
DROP TABLE IF EXISTS users;
