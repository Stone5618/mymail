const db = require('./database');

class SendLogDAO {
  create({ userId, toAddr, subject, status = 'pending', errorMsg }) {
    const stmt = db.prepare(`
      INSERT INTO send_log (user_id, to_addr, subject, status, error_msg)
      VALUES (?, ?, ?, ?, ?)
    `);
    return stmt.run(userId, toAddr, subject, status, errorMsg || null);
  }

  updateStatus(id, status, errorMsg) {
    db.prepare('UPDATE send_log SET status = ?, error_msg = ? WHERE id = ?').run(status, errorMsg || null, id);
  }

  recentCount(userId, windowMs = 60000) {
    const since = new Date(Date.now() - windowMs).toISOString();
    return db.prepare("SELECT COUNT(*) as count FROM send_log WHERE user_id = ? AND sent_at >= ? AND status = 'sent'").get(userId, since).count;
  }
}

module.exports = new SendLogDAO();
