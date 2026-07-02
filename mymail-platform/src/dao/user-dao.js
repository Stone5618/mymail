const db = require('./database');

class UserDAO {
  findByEmail(email) {
    return db.prepare('SELECT * FROM users WHERE email = ?').get(email);
  }

  findById(id) {
    return db.prepare('SELECT * FROM users WHERE id = ?').get(id);
  }

  findByUsername(username) {
    return db.prepare('SELECT * FROM users WHERE username = ?').get(username);
  }

  create({ username, email, passwordHash, displayName, role = 'user' }) {
    const stmt = db.prepare(`
      INSERT INTO users (username, email, password_hash, display_name, role)
      VALUES (?, ?, ?, ?, ?)
    `);
    const result = stmt.run(username, email, passwordHash, displayName || username, role);
    return result.lastInsertRowid;
  }

  updateLoginFails(userId, fails) {
    db.prepare('UPDATE users SET login_fails = ?, updated_at = CURRENT_TIMESTAMP WHERE id = ?').run(fails, userId);
  }

  lockUser(userId, until) {
    db.prepare('UPDATE users SET locked_until = ?, updated_at = CURRENT_TIMESTAMP WHERE id = ?').run(until, userId);
  }

  resetLoginFails(userId) {
    db.prepare('UPDATE users SET login_fails = 0, locked_until = NULL, updated_at = CURRENT_TIMESTAMP WHERE id = ?').run(userId);
  }

  updateProfile(userId, { displayName, signature }) {
    const fields = [];
    const values = [];
    if (displayName !== undefined) { fields.push('display_name = ?'); values.push(displayName); }
    if (signature !== undefined) { fields.push('signature = ?'); values.push(signature); }
    if (fields.length === 0) return;
    fields.push('updated_at = CURRENT_TIMESTAMP');
    values.push(userId);
    db.prepare(`UPDATE users SET ${fields.join(', ')} WHERE id = ?`).run(...values);
  }

  updatePassword(userId, passwordHash) {
    db.prepare('UPDATE users SET password_hash = ?, updated_at = CURRENT_TIMESTAMP WHERE id = ?').run(passwordHash, userId);
  }

  updateStorageUsed(userId, bytes) {
    db.prepare('UPDATE users SET storage_used = storage_used + ?, updated_at = CURRENT_TIMESTAMP WHERE id = ?').run(bytes, userId);
  }

  list({ search, page = 1, limit = 20 }) {
    const offset = (page - 1) * limit;
    let where = 'WHERE 1=1';
    const params = [];
    if (search) {
      where += ' AND (username LIKE ? OR email LIKE ? OR display_name LIKE ?)';
      const s = `%${search}%`;
      params.push(s, s, s);
    }
    const total = db.prepare(`SELECT COUNT(*) as count FROM users ${where}`).get(...params).count;
    const users = db.prepare(`SELECT id, username, email, display_name, role, storage_limit, storage_used, is_active, created_at FROM users ${where} ORDER BY created_at DESC LIMIT ? OFFSET ?`).all(...params, limit, offset);
    return { users, total, page, limit };
  }

  setActive(userId, isActive) {
    db.prepare('UPDATE users SET is_active = ?, updated_at = CURRENT_TIMESTAMP WHERE id = ?').run(isActive ? 1 : 0, userId);
  }

  count() {
    return db.prepare('SELECT COUNT(*) as count FROM users').get().count;
  }

  setDefaultPassword(userId, value) {
    db.prepare('UPDATE users SET is_default_password = ?, updated_at = CURRENT_TIMESTAMP WHERE id = ?').run(value, userId);
  }

  activeCount() {
    return db.prepare('SELECT COUNT(*) as count FROM users WHERE is_active = 1').get().count;
  }
}

module.exports = new UserDAO();
