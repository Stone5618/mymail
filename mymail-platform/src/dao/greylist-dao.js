const db = require('./database');

class GreylistDAO {
  get(key) {
    return db.prepare('SELECT * FROM greylist WHERE key = ?').get(key);
  }

  upsert(key, firstSeen, allowed) {
    db.prepare(`
      INSERT INTO greylist (key, first_seen, allowed)
      VALUES (?, ?, ?)
      ON CONFLICT(key) DO UPDATE SET first_seen = ?, allowed = ?
    `).run(key, firstSeen, allowed ? 1 : 0, firstSeen, allowed ? 1 : 0);
  }

  setAllowed(key) {
    db.prepare('UPDATE greylist SET allowed = 1 WHERE key = ?').run(key);
  }

  cleanup(ttlMs) {
    const cutoff = Date.now() - ttlMs * 2;
    db.prepare('DELETE FROM greylist WHERE first_seen < ?').run(cutoff);
  }
}

module.exports = new GreylistDAO();
