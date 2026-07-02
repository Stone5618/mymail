const jwt = require('jsonwebtoken');
const config = require('../config');
const userDAO = require('../dao/user-dao');

function generateToken(user, remember = false) {
  const expiresIn = remember ? config.jwt.rememberExpiresIn : config.jwt.expiresIn;
  return jwt.sign(
    { id: user.id, email: user.email, role: user.role },
    config.jwt.secret,
    { expiresIn }
  );
}

function authenticate(req, res, next) {
  const authHeader = req.headers.authorization;
  if (!authHeader || !authHeader.startsWith('Bearer ')) {
    return res.status(401).json({ error: '未登录' });
  }

  try {
    const token = authHeader.split(' ')[1];
    const decoded = jwt.verify(token, config.jwt.secret);
    const user = userDAO.findById(decoded.id);
    if (!user || !user.is_active) {
      return res.status(401).json({ error: '账号已禁用' });
    }
    req.user = user;
    next();
  } catch (err) {
    return res.status(401).json({ error: 'Token 已过期，请重新登录' });
  }
}

function requireAdmin(req, res, next) {
  if (req.user.role !== 'admin') {
    return res.status(403).json({ error: '需要管理员权限' });
  }
  next();
}

module.exports = { generateToken, authenticate, requireAdmin };
