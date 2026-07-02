const Database = require('better-sqlite3');
const path = require('path');
const fs = require('fs');
require('dotenv').config({ path: path.resolve(__dirname, '../.env') });

const dbPath = path.resolve(process.env.DB_PATH || './data/mymail.db');
const dbDir = path.dirname(dbPath);

if (!fs.existsSync(dbDir)) {
  fs.mkdirSync(dbDir, { recursive: true });
}

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

  CREATE TABLE IF NOT EXISTS mail_rules (
    id          INTEGER PRIMARY KEY AUTOINCREMENT,
    user_id     INTEGER NOT NULL,
    name        TEXT NOT NULL,
    priority    INTEGER DEFAULT 0,
    conditions  TEXT NOT NULL,
    actions     TEXT NOT NULL,
    is_active   INTEGER DEFAULT 1,
    created_at  DATETIME DEFAULT CURRENT_TIMESTAMP,
    FOREIGN KEY (user_id) REFERENCES users(id) ON DELETE CASCADE
  );

  CREATE INDEX IF NOT EXISTS idx_rules_user ON mail_rules(user_id);

  CREATE TABLE IF NOT EXISTS api_keys (
    id            INTEGER PRIMARY KEY AUTOINCREMENT,
    user_id       INTEGER NOT NULL,
    name          TEXT NOT NULL,
    key_hash      TEXT NOT NULL,
    scopes        TEXT DEFAULT '["send"]',
    rate_limit    INTEGER DEFAULT 10,
    is_active     INTEGER DEFAULT 1,
    last_used_at  DATETIME,
    created_at    DATETIME DEFAULT CURRENT_TIMESTAMP,
    FOREIGN KEY (user_id) REFERENCES users(id) ON DELETE CASCADE
  );

  CREATE INDEX IF NOT EXISTS idx_apikeys_user ON api_keys(user_id);

  CREATE TABLE IF NOT EXISTS greylist (
    key         TEXT PRIMARY KEY,
    first_seen  INTEGER NOT NULL,
    allowed     INTEGER DEFAULT 0
  );
`);

console.log('Database initialized at:', dbPath);
db.close();
