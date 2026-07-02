const path = require('path');
const fs = require('fs');
const os = require('os');

// Create temp directory for test data
const testDir = path.join(os.tmpdir(), 'mymail-test-' + Date.now());
fs.mkdirSync(testDir, { recursive: true });
const dataDir = path.join(testDir, 'data');
const maildirDir = path.join(dataDir, 'maildir');
const attachDir = path.join(dataDir, 'attachments');
fs.mkdirSync(dataDir, { recursive: true });
fs.mkdirSync(maildirDir, { recursive: true });
fs.mkdirSync(attachDir, { recursive: true });

const dbPath = path.join(dataDir, 'mymail.db');

// Set env vars before anything else
process.env.DB_PATH = dbPath;
process.env.MAILDIR_PATH = maildirDir;
process.env.ATTACHMENT_PATH = attachDir;
process.env.JWT_SECRET = 'test-secret-key-for-testing';
process.env.PORT = '0';
process.env.HOST = '127.0.0.1';
process.env.DOMAIN = 'test.local';
process.env.NODE_ENV = 'test';
process.env.SPAM_FILTER_ENABLED = 'false';

// Initialize database with schema BEFORE app loads
const Database = require('better-sqlite3');
const db = new Database(dbPath);
db.pragma('journal_mode = WAL');
db.pragma('foreign_keys = ON');

db.exec(`
  CREATE TABLE IF NOT EXISTS users (
    id            INTEGER PRIMARY KEY AUTOINCREMENT,
    username      TEXT NOT NULL UNIQUE,
    email         TEXT NOT NULL UNIQUE,
    password_hash TEXT NOT NULL,
    display_name  TEXT,
    role          TEXT DEFAULT 'user',
    storage_limit INTEGER DEFAULT 104857600,
    storage_used  INTEGER DEFAULT 0,
    is_active     INTEGER DEFAULT 1,
    login_fails   INTEGER DEFAULT 0,
    locked_until  DATETIME,
    signature     TEXT,
    is_default_password INTEGER DEFAULT 0,
    created_at    DATETIME DEFAULT CURRENT_TIMESTAMP,
    updated_at    DATETIME DEFAULT CURRENT_TIMESTAMP
  );

  CREATE TABLE IF NOT EXISTS messages (
    id            INTEGER PRIMARY KEY AUTOINCREMENT,
    user_id       INTEGER NOT NULL,
    folder        TEXT DEFAULT 'INBOX',
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
    is_read       INTEGER DEFAULT 0,
    is_starred    INTEGER DEFAULT 0,
    is_deleted    INTEGER DEFAULT 0,
    has_attach    INTEGER DEFAULT 0,
    attach_count  INTEGER DEFAULT 0,
    size_bytes    INTEGER DEFAULT 0,
    headers_raw   TEXT,
    in_reply_to   TEXT,
    flags         TEXT DEFAULT '[]',
    received_at   DATETIME DEFAULT CURRENT_TIMESTAMP,
    FOREIGN KEY (user_id) REFERENCES users(id)
  );

  CREATE INDEX IF NOT EXISTS idx_msg_user_folder ON messages(user_id, folder);
  CREATE INDEX IF NOT EXISTS idx_msg_received ON messages(received_at);

  CREATE TABLE IF NOT EXISTS attachments (
    id            INTEGER PRIMARY KEY AUTOINCREMENT,
    message_id    INTEGER NOT NULL,
    filename      TEXT NOT NULL,
    mime_type     TEXT,
    size_bytes    INTEGER,
    storage_path  TEXT NOT NULL,
    created_at    DATETIME DEFAULT CURRENT_TIMESTAMP,
    FOREIGN KEY (message_id) REFERENCES messages(id) ON DELETE CASCADE
  );

  CREATE TABLE IF NOT EXISTS send_log (
    id            INTEGER PRIMARY KEY AUTOINCREMENT,
    user_id       INTEGER NOT NULL,
    to_addr       TEXT NOT NULL,
    subject       TEXT,
    status        TEXT DEFAULT 'pending',
    error_msg     TEXT,
    sent_at       DATETIME DEFAULT CURRENT_TIMESTAMP,
    FOREIGN KEY (user_id) REFERENCES users(id)
  );

  CREATE TABLE IF NOT EXISTS settings (
    key           TEXT PRIMARY KEY,
    value         TEXT,
    updated_at    DATETIME DEFAULT CURRENT_TIMESTAMP
  );
`);
db.close();

global.__TEST_DIR__ = testDir;
