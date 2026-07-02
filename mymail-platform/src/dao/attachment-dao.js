const db = require('./database');

class AttachmentDAO {
  create({ messageId, filename, mimeType, sizeBytes, storagePath }) {
    const stmt = db.prepare(`
      INSERT INTO attachments (message_id, filename, mime_type, size_bytes, storage_path)
      VALUES (?, ?, ?, ?, ?)
    `);
    const result = stmt.run(messageId, filename, mimeType, sizeBytes, storagePath);
    return result.lastInsertRowid;
  }

  findByMessageId(messageId) {
    return db.prepare('SELECT * FROM attachments WHERE message_id = ?').all(messageId);
  }

  findById(id) {
    return db.prepare('SELECT * FROM attachments WHERE id = ?').get(id);
  }

  deleteByMessageId(messageId) {
    db.prepare('DELETE FROM attachments WHERE message_id = ?').run(messageId);
  }
}

module.exports = new AttachmentDAO();
