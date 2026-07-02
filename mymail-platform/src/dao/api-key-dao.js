const db = require('./database');

class ApiKeyDAO {
  create({ userId, name, keyHash, scopes = '["send"]', rateLimit = 10 }) {
    const stmt = db.prepare(`
      INSERT INTO api_keys (user_id, name, key_hash, scopes, rate_limit)
      VALUES (?, ?, ?, ?, ?)
    `);
    const result = stmt.run(userId, name, keyHash, scopes, rateLimit);
    return result.lastInsertRowid;
  }

  findById(id) {
    return db.prepare('SELECT * FROM api_keys WHERE id = ?').get(id);
  }

  findByKeyHash(keyHash) {
    return db.prepare('SELECT * FROM api_keys WHERE key_hash = ? AND is_active = 1').get(keyHash);
  }

  findByUserId(userId) {
    return db.prepare(
      'SELECT id, name, scopes, rate_limit, is_active, last_used_at, created_at FROM api_keys WHERE user_id = ? ORDER BY created_at DESC'
    ).all(userId);
  }

  updateLastUsed(id) {
    db.prepare('UPDATE api_keys SET last_used_at = CURRENT_TIMESTAMP WHERE id = ?').run(id);
  }

  deactivate(id) {
    db.prepare('UPDATE api_keys SET is_active = 0 WHERE id = ?').run(id);
  }

  delete(id) {
    db.prepare('DELETE FROM api_keys WHERE id = ?').run(id);
  }

  countByUserId(userId) {
    return db.prepare('SELECT COUNT(*) as count FROM api_keys WHERE user_id = ? AND is_active = 1').get(userId).count;
  }
}

module.exports = new ApiKeyDAO();
