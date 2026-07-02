const db = require('./database');

class SpamLogDAO {
  create({ senderIp, senderAddr, recipientAddr, spamScore, reasons, action = 'delivered' }) {
    const stmt = db.prepare(`
      INSERT INTO spam_log (sender_ip, sender_addr, recipient_addr, spam_score, reasons, action)
      VALUES (?, ?, ?, ?, ?, ?)
    `);
    return stmt.run(senderIp, senderAddr, recipientAddr, spamScore, JSON.stringify(reasons), action);
  }

  findRecent({ limit = 50, page = 1 }) {
    const offset = (page - 1) * limit;
    const total = db.prepare('SELECT COUNT(*) as count FROM spam_log').get().count;
    const rows = db.prepare(
      'SELECT * FROM spam_log ORDER BY created_at DESC LIMIT ? OFFSET ?'
    ).all(limit, offset);
    return { rows, total, page, limit };
  }

  findBySenderIp(ip, limit = 20) {
    return db.prepare(
      'SELECT * FROM spam_log WHERE sender_ip = ? ORDER BY created_at DESC LIMIT ?'
    ).all(ip, limit);
  }

  statsByAction() {
    return db.prepare(
      'SELECT action, COUNT(*) as count FROM spam_log GROUP BY action'
    ).all();
  }
}

module.exports = new SpamLogDAO();
