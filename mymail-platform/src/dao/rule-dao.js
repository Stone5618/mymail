const db = require('./database');

class RuleDAO {
  create({ userId, name, priority = 0, conditions, actions }) {
    const stmt = db.prepare(`
      INSERT INTO mail_rules (user_id, name, priority, conditions, actions)
      VALUES (?, ?, ?, ?, ?)
    `);
    const result = stmt.run(userId, name, priority, JSON.stringify(conditions), JSON.stringify(actions));
    return result.lastInsertRowid;
  }

  findById(id) {
    const row = db.prepare('SELECT * FROM mail_rules WHERE id = ?').get(id);
    if (row) {
      row.conditions = JSON.parse(row.conditions);
      row.actions = JSON.parse(row.actions);
    }
    return row;
  }

  findByUserId(userId) {
    const rows = db.prepare(
      'SELECT * FROM mail_rules WHERE user_id = ? ORDER BY priority DESC, created_at DESC'
    ).all(userId);
    return rows.map(r => ({
      ...r,
      conditions: JSON.parse(r.conditions),
      actions: JSON.parse(r.actions),
    }));
  }

  findActiveByUserId(userId) {
    const rows = db.prepare(
      'SELECT * FROM mail_rules WHERE user_id = ? AND is_active = 1 ORDER BY priority DESC'
    ).all(userId);
    return rows.map(r => ({
      ...r,
      conditions: JSON.parse(r.conditions),
      actions: JSON.parse(r.actions),
    }));
  }

  update(id, { name, priority, conditions, actions, isActive }) {
    const fields = [];
    const values = [];
    if (name !== undefined) { fields.push('name = ?'); values.push(name); }
    if (priority !== undefined) { fields.push('priority = ?'); values.push(priority); }
    if (conditions !== undefined) { fields.push('conditions = ?'); values.push(JSON.stringify(conditions)); }
    if (actions !== undefined) { fields.push('actions = ?'); values.push(JSON.stringify(actions)); }
    if (isActive !== undefined) { fields.push('is_active = ?'); values.push(isActive ? 1 : 0); }
    if (fields.length === 0) return;
    values.push(id);
    db.prepare(`UPDATE mail_rules SET ${fields.join(', ')} WHERE id = ?`).run(...values);
  }

  delete(id) {
    db.prepare('DELETE FROM mail_rules WHERE id = ?').run(id);
  }
}

module.exports = new RuleDAO();
