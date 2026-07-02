const request = require('supertest');

// Mock smtp-sender to avoid real SMTP connections
jest.mock('../src/services/smtp-sender', () => ({
  sendMail: jest.fn().mockResolvedValue({ messageId: '<test-msg-id@test.local>' }),
  sendLocal: jest.fn().mockResolvedValue(undefined),
}));

const { getApp, createAdminToken, getAuthToken, cleanup } = require('./helpers');

const app = getApp();

afterAll(() => cleanup());

describe('Admin API', () => {
  let adminToken;
  let userToken;

  beforeAll(async () => {
    adminToken = await createAdminToken(request(app));
    userToken = await getAuthToken(request(app), { username: 'regularuser' });
  });

  describe('GET /api/admin/users', () => {
    it('should list users for admin', async () => {
      const res = await request(app)
        .get('/api/admin/users')
        .set('Authorization', `Bearer ${adminToken}`);
      expect(res.status).toBe(200);
      expect(res.body).toHaveProperty('users');
      expect(res.body).toHaveProperty('total');
      expect(Array.isArray(res.body.users)).toBe(true);
    });

    it('should return 403 for non-admin user', async () => {
      const res = await request(app)
        .get('/api/admin/users')
        .set('Authorization', `Bearer ${userToken}`);
      expect(res.status).toBe(403);
    });
  });

  describe('POST /api/admin/users', () => {
    it('should create a new user for admin', async () => {
      const res = await request(app)
        .post('/api/admin/users')
        .set('Authorization', `Bearer ${adminToken}`)
        .send({ username: 'newuser', password: 'password123', displayName: 'New User' });
      expect(res.status).toBe(201);
      expect(res.body).toHaveProperty('userId');
    });

    it('should return 403 for non-admin creating user', async () => {
      const res = await request(app)
        .post('/api/admin/users')
        .set('Authorization', `Bearer ${userToken}`)
        .send({ username: 'failuser', password: 'password123' });
      expect(res.status).toBe(403);
    });
  });
});
