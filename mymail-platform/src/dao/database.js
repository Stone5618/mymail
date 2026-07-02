const Database = require('better-sqlite3');
const config = require('../config');
const fs = require('fs');
const path = require('path');

const dbDir = path.dirname(config.db.path);
if (!fs.existsSync(dbDir)) {
  fs.mkdirSync(dbDir, { recursive: true });
}

const db = new Database(config.db.path);
db.pragma('journal_mode = WAL');
db.pragma('foreign_keys = ON');

module.exports = db;
