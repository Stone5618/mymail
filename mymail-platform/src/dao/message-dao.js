const db = require('./database');

class MessageDAO {
  create(msg) {
    const stmt = db.prepare(`
      INSERT INTO messages (user_id, folder, message_id, uid, from_addr, from_name, to_addr, cc_addr, bcc_addr, reply_to, subject, body_text, body_html, has_attach, attach_count, size_bytes, headers_raw, in_reply_to)
      VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
    `);
    const result = stmt.run(
      msg.userId, msg.folder || 'INBOX', msg.messageId, msg.uid,
      msg.fromAddr, msg.fromName, msg.toAddr, msg.ccAddr || null, msg.bccAddr || null,
      msg.replyTo || null, msg.subject, msg.bodyText, msg.bodyHtml,
      msg.hasAttach ? 1 : 0, msg.attachCount || 0, msg.sizeBytes || 0,
      msg.headersRaw || null, msg.inReplyTo || null
    );
    return result.lastInsertRowid;
  }

  findById(id) {
    return db.prepare('SELECT * FROM messages WHERE id = ?').get(id);
  }

  findByUserId(userId, { folder = 'INBOX', page = 1, limit = 20, search, unreadOnly }) {
    const offset = (page - 1) * limit;
    let where = 'WHERE user_id = ? AND folder = ? AND is_deleted = 0';
    const params = [userId, folder];

    if (unreadOnly) {
      where += ' AND is_read = 0';
    }
    if (search) {
      where += ' AND (from_addr LIKE ? OR from_name LIKE ? OR subject LIKE ? OR body_text LIKE ?)';
      const s = `%${search}%`;
      params.push(s, s, s, s);
    }

    const total = db.prepare(`SELECT COUNT(*) as count FROM messages ${where}`).get(...params).count;
    const messages = db.prepare(`SELECT * FROM messages ${where} ORDER BY received_at DESC LIMIT ? OFFSET ?`).all(...params, limit, offset);
    return { messages, total, page, limit };
  }

  unreadCount(userId, folder = 'INBOX') {
    return db.prepare('SELECT COUNT(*) as count FROM messages WHERE user_id = ? AND folder = ? AND is_read = 0 AND is_deleted = 0').get(userId, folder).count;
  }

  markRead(id) {
    db.prepare('UPDATE messages SET is_read = 1 WHERE id = ?').run(id);
  }

  markUnread(id) {
    db.prepare('UPDATE messages SET is_read = 0 WHERE id = ?').run(id);
  }

  toggleStar(id) {
    db.prepare('UPDATE messages SET is_starred = CASE WHEN is_starred = 1 THEN 0 ELSE 1 END WHERE id = ?').run(id);
  }

  moveToFolder(id, folder) {
    db.prepare('UPDATE messages SET folder = ? WHERE id = ?').run(folder, id);
  }

  softDelete(id) {
    db.prepare("UPDATE messages SET is_deleted = 1, folder = 'TRASH' WHERE id = ?").run(id);
  }

  permanentDelete(id) {
    db.prepare('DELETE FROM messages WHERE id = ?').run(id);
  }

  emptyTrash(userId) {
    const msgs = db.prepare("SELECT id FROM messages WHERE user_id = ? AND folder = 'TRASH'").all(userId);
    for (const msg of msgs) {
      db.prepare('DELETE FROM attachments WHERE message_id = ?').run(msg.id);
    }
    db.prepare("DELETE FROM messages WHERE user_id = ? AND folder = 'TRASH'").run(userId);
  }

  cleanOldTrash(userId, days = 30) {
    const msgs = db.prepare(`SELECT id FROM messages WHERE user_id = ? AND folder = 'TRASH' AND received_at < datetime('now', '-${days} days')`).all(userId);
    for (const msg of msgs) {
      db.prepare('DELETE FROM attachments WHERE message_id = ?').run(msg.id);
    }
    db.prepare(`DELETE FROM messages WHERE user_id = ? AND folder = 'TRASH' AND received_at < datetime('now', '-${days} days')`).run(userId);
  }

  totalSize(userId) {
    return db.prepare('SELECT COALESCE(SUM(size_bytes), 0) as total FROM messages WHERE user_id = ?').get(userId).total;
  }

  todaySentCount() {
    return db.prepare("SELECT COUNT(*) as count FROM messages WHERE folder = 'SENT' AND received_at >= date('now')").get().count;
  }

  todayReceivedCount() {
    return db.prepare("SELECT COUNT(*) as count FROM messages WHERE folder = 'INBOX' AND received_at >= date('now')").get().count;
  }

  getNextUid(userId) {
    const row = db.prepare('SELECT COALESCE(MAX(uid), 0) + 1 as next_uid FROM messages WHERE user_id = ?').get(userId);
    return row.next_uid;
  }

  // For Dovecot integration - get message by UID
  findByUid(userId, uid) {
    return db.prepare('SELECT * FROM messages WHERE user_id = ? AND uid = ?').get(userId, uid);
  }

  // Update flags for IMAP
  updateFlags(id, flags) {
    db.prepare('UPDATE messages SET flags = ? WHERE id = ?').run(JSON.stringify(flags), id);
  }
}

module.exports = new MessageDAO();
