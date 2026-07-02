const express = require('express');
const bcrypt = require('bcrypt');
const router = express.Router();
const config = require('../config');
const userDAO = require('../dao/user-dao');
const messageDAO = require('../dao/message-dao');
const settingsDAO = require('../dao/settings-dao');
const { authenticate, requireAdmin } = require('../middleware/auth');
const fs = require('fs');
const path = require('path');
const logger = require('../logger');

router.use(authenticate, requireAdmin);

// GET /api/admin/stats
router.get('/stats', (req, res) => {
  res.json({
    totalUsers: userDAO.count(),
    activeUsers: userDAO.activeCount(),
    todaySent: messageDAO.todaySentCount(),
    todayReceived: messageDAO.todayReceivedCount(),
  });
});

// GET /api/admin/users
router.get('/users', (req, res) => {
  const { search, page, limit } = req.query;
  res.json(userDAO.list({ search, page: parseInt(page) || 1, limit: parseInt(limit) || 20 }));
});

// POST /api/admin/users
router.post('/users', async (req, res) => {
  try {
    const { username, password, displayName, storageLimit } = req.body;
    if (!username || !password) {
      return res.status(400).json({ error: '用户名和密码不能为空' });
    }
    if (userDAO.findByUsername(username)) {
      return res.status(409).json({ error: '用户名已存在' });
    }
    const email = `${username}@${config.domain}`;
    const passwordHash = await bcrypt.hash(password, 12);
    const userId = userDAO.create({ username, email, passwordHash, displayName });
    if (storageLimit) {
      userDAO.updateStorageUsed(userId, 0); // just to have the user created
    }
    // Create maildir
    const userMaildir = path.join(config.mail.dirPath, config.domain, username);
    for (const dir of ['cur', 'new', 'tmp']) {
      fs.mkdirSync(path.join(userMaildir, dir), { recursive: true });
    }
    res.status(201).json({ message: '用户创建成功', userId });
  } catch (err) {
    logger.error({ err }, 'Admin create user error');
    res.status(500).json({ error: '创建失败' });
  }
});

// PUT /api/admin/users/:id
router.put('/users/:id', async (req, res) => {
  try {
    const userId = parseInt(req.params.id);
    const { displayName, storageLimit, isActive, password } = req.body;
    if (displayName !== undefined || storageLimit !== undefined) {
      userDAO.updateProfile(userId, { displayName });
    }
    if (isActive !== undefined) {
      userDAO.setActive(userId, isActive);
    }
    if (password) {
      const hash = await bcrypt.hash(password, 12);
      userDAO.updatePassword(userId, hash);
    }
    res.json({ message: '更新成功' });
  } catch (err) {
    res.status(500).json({ error: '更新失败' });
  }
});

// GET /api/admin/settings
router.get('/settings', (req, res) => {
  res.json(settingsDAO.getAll());
});

const ALLOWED_SETTINGS = [
  'smtp_port_enabled', 'max_attachment_size', 'rate_limit_max',
  'send_rate_limit_per_min', 'spam_filter_enabled', 'spam_threshold',
  'spam_suspicious_threshold', 'dnsbl_enabled', 'spf_enabled',
  'greylist_delay_ms', 'greylist_ttl_ms',
];

// PUT /api/admin/settings
router.put('/settings', (req, res) => {
  const settings = req.body;
  for (const [key, value] of Object.entries(settings)) {
    if (!ALLOWED_SETTINGS.includes(key)) {
      logger.warn({ key }, 'Rejected unknown settings key');
      continue;
    }
    settingsDAO.set(key, String(value));
  }
  res.json({ message: '配置已保存' });
});

// GET /api/admin/dns-status
router.get('/dns-status', async (req, res) => {
  const dns = require('dns').promises;
  const [mxResult, txtResult] = await Promise.allSettled([
    dns.resolveMx(config.domain),
    dns.resolveTxt(config.domain),
  ]);
  const mx = mxResult.status === 'fulfilled' && mxResult.value.length > 0;
  const spfText = txtResult.status === 'fulfilled' ? txtResult.value.flat().join('') : '';
  res.json({
    mx: mx ? 'ok' : 'missing',
    spf: spfText.includes('v=spf1') ? 'ok' : 'missing',
  });
});

// POST /api/admin/smtp-port
router.post('/smtp-port', (req, res) => {
  const { enabled } = req.body;
  settingsDAO.set('smtp_port_enabled', enabled ? 'true' : 'false');
  res.json({ message: enabled ? '端口25已开启（需重启服务生效）' : '端口25已关闭（需重启服务生效）' });
});

module.exports = router;
