const request = require('supertest');

// Mock smtp-sender to avoid real SMTP connections
jest.mock('../src/services/smtp-sender', () => ({
  sendMail: jest.fn().mockResolvedValue({ messageId: '<test-msg-id@test.local>' }),
  sendLocal: jest.fn().mockResolvedValue(undefined),
}));

const { getApp, getAuthToken, sendTestMail, cleanup } = require('./helpers');

const app = getApp();

afterAll(() => cleanup());

describe('Mail API', () => {
  let token;

  beforeAll(async () => {
    token = await getAuthToken(request(app), { username: 'mailuser' });
  });

  describe('POST /api/mail/send', () => {
    it('should send a mail successfully', async () => {
      const res = await sendTestMail(request(app), token);
      expect(res.status).toBe(200);
      expect(res.body).toHaveProperty('messageId');
    });

    it('should reject sending without recipient', async () => {
      const res = await request(app)
        .post('/api/mail/send')
        .set('Authorization', `Bearer ${token}`)
        .field('subject', 'No recipient')
        .field('bodyHtml', '<p>Test</p>');
      expect(res.status).toBe(400);
    });
  });

  describe('GET /api/mail/list', () => {
    it('should list sent mail', async () => {
      // Send a mail first
      await sendTestMail(request(app), token, { subject: 'List Test' });

      const res = await request(app)
        .get('/api/mail/list?folder=SENT')
        .set('Authorization', `Bearer ${token}`);
      expect(res.status).toBe(200);
      expect(res.body).toHaveProperty('messages');
      expect(res.body).toHaveProperty('total');
      expect(Array.isArray(res.body.messages)).toBe(true);
      expect(res.body.messages.length).toBeGreaterThan(0);
    });

    it('should list inbox mail', async () => {
      const res = await request(app)
        .get('/api/mail/list?folder=INBOX')
        .set('Authorization', `Bearer ${token}`);
      expect(res.status).toBe(200);
      expect(res.body).toHaveProperty('messages');
    });
  });

  describe('PUT /api/mail/:id/read', () => {
    it('should mark mail as read', async () => {
      await sendTestMail(request(app), token, { subject: 'Read Test' });
      const listRes = await request(app)
        .get('/api/mail/list?folder=SENT')
        .set('Authorization', `Bearer ${token}`);
      expect(listRes.body.messages.length).toBeGreaterThan(0);
      const msgId = listRes.body.messages[0].id;

      const res = await request(app)
        .put(`/api/mail/${msgId}/read`)
        .set('Authorization', `Bearer ${token}`);
      expect(res.status).toBe(200);
    });
  });

  describe('PUT /api/mail/:id/unread', () => {
    it('should mark mail as unread', async () => {
      await sendTestMail(request(app), token, { subject: 'Unread Test' });
      const listRes = await request(app)
        .get('/api/mail/list?folder=SENT')
        .set('Authorization', `Bearer ${token}`);
      expect(listRes.body.messages.length).toBeGreaterThan(0);
      const msgId = listRes.body.messages[0].id;

      await request(app).put(`/api/mail/${msgId}/read`).set('Authorization', `Bearer ${token}`);
      const res = await request(app)
        .put(`/api/mail/${msgId}/unread`)
        .set('Authorization', `Bearer ${token}`);
      expect(res.status).toBe(200);
    });
  });

  describe('PUT /api/mail/:id/star', () => {
    it('should toggle star on mail', async () => {
      await sendTestMail(request(app), token, { subject: 'Star Test' });
      const listRes = await request(app)
        .get('/api/mail/list?folder=SENT')
        .set('Authorization', `Bearer ${token}`);
      expect(listRes.body.messages.length).toBeGreaterThan(0);
      const msgId = listRes.body.messages[0].id;

      const res = await request(app)
        .put(`/api/mail/${msgId}/star`)
        .set('Authorization', `Bearer ${token}`);
      expect(res.status).toBe(200);
    });
  });

  describe('DELETE /api/mail/:id and restore', () => {
    it('should move mail to trash and restore', async () => {
      await sendTestMail(request(app), token, { subject: 'Delete Restore Test' });
      const listRes = await request(app)
        .get('/api/mail/list?folder=SENT')
        .set('Authorization', `Bearer ${token}`);
      expect(listRes.body.messages.length).toBeGreaterThan(0);
      const msgId = listRes.body.messages[0].id;

      // Delete (move to trash)
      const delRes = await request(app)
        .delete(`/api/mail/${msgId}`)
        .set('Authorization', `Bearer ${token}`);
      expect(delRes.status).toBe(200);

      // Restore
      const restoreRes = await request(app)
        .put(`/api/mail/${msgId}/restore`)
        .set('Authorization', `Bearer ${token}`);
      expect(restoreRes.status).toBe(200);
    });
  });

  describe('POST /api/mail/empty-trash', () => {
    it('should empty the trash', async () => {
      await sendTestMail(request(app), token, { subject: 'Trash Test' });
      const listRes = await request(app)
        .get('/api/mail/list?folder=SENT')
        .set('Authorization', `Bearer ${token}`);
      if (listRes.body.messages.length > 0) {
        const msgId = listRes.body.messages[0].id;
        await request(app).delete(`/api/mail/${msgId}`).set('Authorization', `Bearer ${token}`);
      }

      const res = await request(app)
        .post('/api/mail/empty-trash')
        .set('Authorization', `Bearer ${token}`);
      expect(res.status).toBe(200);
    });
  });
});
