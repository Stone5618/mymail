const request = require('supertest');

// Mock smtp-sender to avoid real SMTP connections
jest.mock('../src/services/smtp-sender', () => ({
  sendMail: jest.fn().mockResolvedValue({ messageId: '<test-msg-id@test.local>' }),
  sendLocal: jest.fn().mockResolvedValue(undefined),
}));

const { getApp, registerUser, loginUser, getAuthToken, cleanup } = require('./helpers');

const app = getApp();

afterAll(() => cleanup());

describe('Auth API', () => {
  describe('POST /api/auth/register', () => {
    it('should register a new user successfully', async () => {
      const res = await registerUser(request(app));
      expect(res.status).toBe(201);
      expect(res.body).toHaveProperty('token');
      expect(res.body.user).toHaveProperty('username', 'testuser');
      expect(res.body.user).toHaveProperty('email', 'testuser@test.local');
    });

    it('should reject duplicate username', async () => {
      await registerUser(request(app), { username: 'dupeuser' });
      const res = await registerUser(request(app), { username: 'dupeuser' });
      expect(res.status).toBe(409);
      expect(res.body.error).toMatch(/已存在/);
    });

    it('should reject invalid username format', async () => {
      const res = await registerUser(request(app), { username: 'ab' }); // too short
      expect(res.status).toBe(400);
    });

    it('should reject short password', async () => {
      const res = await registerUser(request(app), { username: 'shortpw', password: '123' });
      expect(res.status).toBe(400);
    });
  });

  describe('POST /api/auth/login', () => {
    it('should login successfully', async () => {
      await registerUser(request(app), { username: 'logintest' });
      const res = await loginUser(request(app), { email: 'logintest@test.local' });
      expect(res.status).toBe(200);
      expect(res.body).toHaveProperty('token');
      expect(res.body.user).toHaveProperty('email', 'logintest@test.local');
    });

    it('should reject wrong password', async () => {
      await registerUser(request(app), { username: 'wrongpw' });
      const res = await loginUser(request(app), { email: 'wrongpw@test.local', password: 'wrongpassword' });
      expect(res.status).toBe(401);
    });

    it('should lock account after 5 failed attempts', async () => {
      await registerUser(request(app), { username: 'locktest' });
      for (let i = 0; i < 5; i++) {
        await loginUser(request(app), { email: 'locktest@test.local', password: 'wrong' });
      }
      const res = await loginUser(request(app), { email: 'locktest@test.local', password: 'wrong' });
      expect(res.status).toBe(423);
    });
  });

  describe('GET /api/auth/me', () => {
    it('should return current user info', async () => {
      const token = await getAuthToken(request(app), { username: 'metest' });
      const res = await request(app)
        .get('/api/auth/me')
        .set('Authorization', `Bearer ${token}`);
      expect(res.status).toBe(200);
      expect(res.body).toHaveProperty('email', 'metest@test.local');
      expect(res.body).toHaveProperty('role');
    });

    it('should reject unauthenticated request', async () => {
      const res = await request(app).get('/api/auth/me');
      expect(res.status).toBe(401);
    });
  });

  describe('PUT /api/auth/password', () => {
    it('should change password successfully', async () => {
      const token = await getAuthToken(request(app), { username: 'pwchange' });
      const res = await request(app)
        .put('/api/auth/password')
        .set('Authorization', `Bearer ${token}`)
        .send({ currentPassword: 'password123', newPassword: 'newpassword123' });
      expect(res.status).toBe(200);

      // Can login with new password
      const loginRes = await loginUser(request(app), { email: 'pwchange@test.local', password: 'newpassword123' });
      expect(loginRes.status).toBe(200);
    });

    it('should reject wrong current password', async () => {
      const token = await getAuthToken(request(app), { username: 'pwfail' });
      const res = await request(app)
        .put('/api/auth/password')
        .set('Authorization', `Bearer ${token}`)
        .send({ currentPassword: 'wrongpassword', newPassword: 'newpassword123' });
      expect(res.status).toBe(401);
    });
  });
});
