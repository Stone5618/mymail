const express = require('express');
const bcrypt = require('bcrypt');
const router = express.Router();
const config = require('../config');
const userDAO = require('../dao/user-dao');
const { generateToken, authenticate } = require('../middleware/auth');
const logger = require('../logger');

// POST /api/auth/register
router.post('/register', async (req, res) => {
  try {
    const { username, password, displayName } = req.body;

    if (!username || !password) {
      return res.status(400).json({ error: '用户名和密码不能为空' });
    }

    if (!/^[a-zA-Z0-9._]{3,20}$/.test(username)) {
      return res.status(400).json({ error: '用户名仅允许字母、数字、点、下划线，3-20字符' });
    }

    if (password.length < 6) {
      return res.status(400).json({ error: '密码至少6位' });
    }

    if (displayName && (displayName.length < 1 || displayName.length > 50)) {
      return res.status(400).json({ error: '显示名称长度1-50字符' });
    }

    if (userDAO.findByUsername(username)) {
      return res.status(409).json({ error: '用户名已存在' });
    }

    const email = `${username}@${config.domain}`;
    if (userDAO.findByEmail(email)) {
      return res.status(409).json({ error: '邮箱已被注册' });
    }

    const passwordHash = await bcrypt.hash(password, 12);
    const isFirst = userDAO.count() === 0;
    const role = isFirst ? 'admin' : 'user';

    const userId = userDAO.create({
      username,
      email,
      passwordHash,
      displayName: displayName || username,
      role,
    });

    // Create maildir for user
    const fs = require('fs');
    const path = require('path');
    const userMaildir = path.join(config.mail.dirPath, config.domain, username);
    for (const dir of ['cur', 'new', 'tmp']) {
      fs.mkdirSync(path.join(userMaildir, dir), { recursive: true });
    }

    const user = userDAO.findById(userId);
    const token = generateToken(user);

    res.status(201).json({
      message: '注册成功',
      token,
      user: { id: user.id, username: user.username, email: user.email, displayName: user.display_name, role: user.role },
    });
  } catch (err) {
    logger.error({ err }, 'Register error');
    res.status(500).json({ error: '注册失败' });
  }
});

// POST /api/auth/login
router.post('/login', async (req, res) => {
  try {
    const { email, password, remember } = req.body;

    if (!email || !password) {
      return res.status(400).json({ error: '邮箱和密码不能为空' });
    }

    const user = userDAO.findByEmail(email);
    if (!user) {
      return res.status(401).json({ error: '邮箱或密码错误' });
    }

    if (!user.is_active) {
      return res.status(403).json({ error: '账号已被禁用' });
    }

    if (user.locked_until && new Date(user.locked_until) > new Date()) {
      const remain = Math.ceil((new Date(user.locked_until) - new Date()) / 60000);
      return res.status(423).json({ error: `账号已锁定，请 ${remain} 分钟后重试` });
    }

    const valid = await bcrypt.compare(password, user.password_hash);
    if (!valid) {
      const fails = user.login_fails + 1;
      if (fails >= 5) {
        const lockUntil = new Date(Date.now() + 15 * 60000).toISOString();
        userDAO.lockUser(user.id, lockUntil);
        userDAO.updateLoginFails(user.id, fails);
        return res.status(423).json({ error: '连续失败5次，账号已锁定15分钟' });
      }
      userDAO.updateLoginFails(user.id, fails);
      return res.status(401).json({ error: `邮箱或密码错误（还剩${5 - fails}次机会）` });
    }

    userDAO.resetLoginFails(user.id);
    const token = generateToken(user, remember);

    // Check if user needs to change default password
    if (user.is_default_password === 1) {
      return res.json({
        message: '需要修改默认密码',
        token,
        user: { id: user.id, username: user.username, email: user.email, displayName: user.display_name, role: user.role },
        requirePasswordChange: true,
      });
    }

    res.json({
      message: '登录成功',
      token,
      user: { id: user.id, username: user.username, email: user.email, displayName: user.display_name, role: user.role },
    });
  } catch (err) {
    logger.error({ err }, 'Login error');
    res.status(500).json({ error: '登录失败' });
  }
});

// GET /api/auth/me
router.get('/me', authenticate, (req, res) => {
  const user = req.user;
  res.json({
    id: user.id,
    username: user.username,
    email: user.email,
    displayName: user.display_name,
    role: user.role,
    signature: user.signature,
    storageLimit: user.storage_limit,
    storageUsed: user.storage_used,
    createdAt: user.created_at,
  });
});

// PUT /api/auth/profile
router.put('/profile', authenticate, async (req, res) => {
  try {
    const { displayName, signature } = req.body;
    userDAO.updateProfile(req.user.id, { displayName, signature });
    res.json({ message: '更新成功' });
  } catch (err) {
    res.status(500).json({ error: '更新失败' });
  }
});

// PUT /api/auth/password
router.put('/password', authenticate, async (req, res) => {
  try {
    const { currentPassword, newPassword } = req.body;
    if (!currentPassword || !newPassword) {
      return res.status(400).json({ error: '请填写当前密码和新密码' });
    }
    if (newPassword.length < 6) {
      return res.status(400).json({ error: '新密码至少6位' });
    }
    const valid = await bcrypt.compare(currentPassword, req.user.password_hash);
    if (!valid) {
      return res.status(401).json({ error: '当前密码错误' });
    }
    const hash = await bcrypt.hash(newPassword, 12);
    userDAO.updatePassword(req.user.id, hash);
    res.json({ message: '密码修改成功' });
  } catch (err) {
    res.status(500).json({ error: '修改失败' });
  }
});

// POST /api/auth/change-default-password
router.post('/change-default-password', authenticate, async (req, res) => {
  try {
    const { newPassword } = req.body;
    if (!newPassword) {
      return res.status(400).json({ error: '请填写新密码' });
    }
    if (newPassword.length < 6) {
      return res.status(400).json({ error: '新密码至少6位' });
    }
    const hash = await bcrypt.hash(newPassword, 12);
    userDAO.updatePassword(req.user.id, hash);
    userDAO.setDefaultPassword(req.user.id, 0);
    res.json({ message: '密码修改成功' });
  } catch (err) {
    logger.error({ err }, 'Change default password error');
    res.status(500).json({ error: '修改失败' });
  }
});

module.exports = router;
