const express = require('express');
const bcrypt = require('bcrypt');
const crypto = require('crypto');
const router = express.Router();
const apiKeyDAO = require('../dao/api-key-dao');
const { authenticate } = require('../middleware/auth');
const { apiAuth, requireScope } = require('../middleware/api-auth');
const { sendMail, sendLocal } = require('../services/smtp-sender');
const messageDAO = require('../dao/message-dao');
const sendLogDAO = require('../dao/send-log-dao');
const userDAO = require('../dao/user-dao');
const config = require('../config');
const logger = require('../logger');

// ── API Key Management (JWT auth required) ──

// POST /api/auth/api-keys — Create API Key
router.post('/api-keys', authenticate, async (req, res) => {
  try {
    const { name, scopes, rateLimit } = req.body;
    if (!name || name.length < 1 || name.length > 50) {
      return res.status(400).json({ error: '名称长度1-50字符' });
    }

    // Limit API keys per user
    if (apiKeyDAO.countByUserId(req.user.id) >= 10) {
      return res.status(400).json({ error: '最多创建10个API Key' });
    }

    // Generate API key
    const rawKey = 'mk_' + crypto.randomBytes(32).toString('hex');
    const keyHash = await bcrypt.hash(rawKey, 10);

    const validScopes = ['send', 'read'];
    const keyScopes = Array.isArray(scopes)
      ? scopes.filter(s => validScopes.includes(s))
      : ['send'];

    const id = apiKeyDAO.create({
      userId: req.user.id,
      name,
      keyHash,
      scopes: JSON.stringify(keyScopes),
      rateLimit: rateLimit || 10,
    });

    // Return raw key only once
    res.status(201).json({
      id,
      name,
      key: rawKey,
      scopes: keyScopes,
      message: '请保存好 API Key，它只会显示一次',
    });
  } catch (err) {
    logger.error({ err }, 'Create API key error');
    res.status(500).json({ error: '创建失败' });
  }
});

// GET /api/auth/api-keys — List API Keys
router.get('/api-keys', authenticate, (req, res) => {
  const keys = apiKeyDAO.findByUserId(req.user.id);
  res.json({ keys });
});

// DELETE /api/auth/api-keys/:id — Delete API Key
router.delete('/api-keys/:id', authenticate, (req, res) => {
  const key = apiKeyDAO.findById(parseInt(req.params.id));
  if (!key || key.user_id !== req.user.id) {
    return res.status(404).json({ error: 'API Key不存在' });
  }
  apiKeyDAO.delete(key.id);
  res.json({ message: '已删除' });
});

// ── API v1 Endpoints (API Key auth) ──

// POST /api/v1/send — Send mail via API Key
router.post('/v1/send', apiAuth, requireScope('send'), async (req, res) => {
  try {
    const { to, cc, bcc, subject, html, text, replyTo } = req.body;
    if (!to || !subject) {
      return res.status(400).json({ error: 'to and subject are required' });
    }

    // Rate limiting per API key
    const recentCount = sendLogDAO.recentCount(req.user.id, 60000);
    const limit = req.apiKey.rateLimit || 10;
    if (recentCount >= limit) {
      return res.status(429).json({ error: `Rate limit exceeded (${limit}/min)` });
    }

    const toAddrs = Array.isArray(to) ? to : [to];
    const ccAddrs = cc ? (Array.isArray(cc) ? cc : [cc]) : [];
    const bccAddrs = bcc ? (Array.isArray(bcc) ? bcc : [bcc]) : [];
    const allRecipients = [...toAddrs, ...ccAddrs, ...bccAddrs];

    const emailRegex = /^[^\s@]+@[^\s@]+\.[^\s@]+$/;
    for (const addr of allRecipients) {
      if (!emailRegex.test(addr)) {
        return res.status(400).json({ error: `Invalid email: ${addr}` });
      }
    }

    // Check for local delivery
    const externalRecipients = allRecipients.filter(a => !a.endsWith(`@${config.domain}`));
    const localRecipients = allRecipients.filter(a => a.endsWith(`@${config.domain}`));

    let result = { messageId: `api-${Date.now()}@${config.domain}` };

    // Send external
    if (externalRecipients.length > 0) {
      result = await sendMail({
        from: { name: req.user.display_name, address: req.user.email },
        to: externalRecipients.filter(a => !ccAddrs.includes(a) && !bccAddrs.includes(a)),
        cc: ccAddrs.filter(a => !a.endsWith(`@${config.domain}`)),
        bcc: bccAddrs.filter(a => !a.endsWith(`@${config.domain}`)),
        subject,
        html: html || text,
        text,
        replyTo: replyTo || req.user.email,
        attachments: [],
      });
    }

    // Send local
    if (localRecipients.length > 0) {
      await sendLocal({
        from: { name: req.user.display_name, address: req.user.email },
        to: localRecipients,
        subject,
        html: html || text,
        text,
        attachments: [],
      });
    }

    // Save to SENT
    messageDAO.create({
      userId: req.user.id,
      folder: 'SENT',
      messageId: result.messageId,
      uid: messageDAO.getNextUid(req.user.id),
      fromAddr: req.user.email,
      fromName: req.user.display_name,
      toAddr: toAddrs.join(', '),
      ccAddr: ccAddrs.join(', ') || null,
      bccAddr: bccAddrs.join(', ') || null,
      subject,
      bodyHtml: html || '',
      bodyText: text || '',
    });

    sendLogDAO.create({ userId: req.user.id, toAddr: toAddrs.join(', '), subject, status: 'sent' });

    res.json({ messageId: result.messageId, status: 'sent' });
  } catch (err) {
    logger.error({ err }, 'API v1 send error');
    sendLogDAO.create({ userId: req.user.id, toAddr: req.body.to, subject: req.body.subject, status: 'failed', errorMsg: err.message });
    res.status(500).json({ error: err.message });
  }
});

module.exports = router;
