const request = require('supertest');

let _app;

function getApp() {
  if (!_app) {
    jest.isolateModules(() => {
      _app = require('../src/app');
    });
  }
  return _app;
}

async function registerUser(agent, { username = 'testuser', password = 'password123', displayName = 'Test User' } = {}) {
  return agent
    .post('/api/auth/register')
    .send({ username, password, displayName });
}

async function loginUser(agent, { email = 'testuser@test.local', password = 'password123' } = {}) {
  return agent
    .post('/api/auth/login')
    .send({ email, password });
}

async function getAuthToken(agent, userOpts = {}) {
  const username = userOpts.username || 'testuser';
  const password = userOpts.password || 'password123';
  const email = `${username}@test.local`;

  await registerUser(agent, { username, password, displayName: userOpts.displayName || username });
  const loginRes = await loginUser(agent, { email, password });
  return loginRes.body.token;
}

async function createAdminToken(agent) {
  // First user becomes admin
  return getAuthToken(agent, { username: 'admin', password: 'admin123', displayName: 'Admin' });
}

async function sendTestMail(agent, token, overrides = {}) {
  return agent
    .post('/api/mail/send')
    .set('Authorization', `Bearer ${token}`)
    .field('to', overrides.to || 'recipient@test.local')
    .field('subject', overrides.subject || 'Test Subject')
    .field('bodyHtml', overrides.bodyHtml || '<p>Test body</p>')
    .field('bodyText', overrides.bodyText || 'Test body');
}

function cleanup() {
  _app = null;
}

module.exports = {
  getApp,
  registerUser,
  loginUser,
  getAuthToken,
  createAdminToken,
  sendTestMail,
  cleanup,
};
