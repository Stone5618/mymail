const express = require('express');
const multer = require('multer');
const path = require('path');
const fs = require('fs');
const router = express.Router();
const config = require('../config');
const messageDAO = require('../dao/message-dao');
const attachmentDAO = require('../dao/attachment-dao');
const sendLogDAO = require('../dao/send-log-dao');
const userDAO = require('../dao/user-dao');
const { authenticate } = require('../middleware/auth');
const { sendMail } = require('../services/smtp-sender');
const archiver = require('archiver');
const logger = require('../logger');

// Multer config for attachments
const storage = multer.diskStorage({
  destination: (req, file, cb) => {
    const userDir = path.join(config.mail.attachmentPath, String(req.user.id));
    fs.mkdirSync(userDir, { recursive: true });
    cb(null, userDir);
  },
  filename: (req, file, cb) => {
    const unique = Date.now() + '-' + Math.round(Math.random() * 1e9);
    cb(null, unique + '-' + Buffer.from(file.originalname, 'latin1').toString('utf8'));
  },
});

const upload = multer({
  storage,
  limits: { fileSize: config.mail.maxAttachmentSize },
  fileFilter: (req, file, cb) => {
    if (config.allowedAttachmentTypes.includes(file.mimetype)) {
      cb(null, true);
    } else {
      cb(new Error('不支持的文件类型: ' + file.mimetype));
    }
  },
});

router.use(authenticate);

// GET /api/mail/list
router.get('/list', (req, res) => {
  const { folder, page, limit, search, unread } = req.query;
  const result = messageDAO.findByUserId(req.user.id, {
    folder: folder || 'INBOX',
    page: parseInt(page) || 1,
    limit: parseInt(limit) || 20,
    search,
    unreadOnly: unread === 'true',
  });
  res.json({
    ...result,
    unreadCount: messageDAO.unreadCount(req.user.id, folder || 'INBOX'),
  });
});

// GET /api/mail/unread-count
router.get('/unread-count', (req, res) => {
  const folders = ['INBOX', 'SENT', 'DRAFTS', 'TRASH'];
  const counts = {};
  for (const f of folders) {
    counts[f] = messageDAO.unreadCount(req.user.id, f);
  }
  res.json(counts);
});

// GET /api/mail/:id
router.get('/:id', (req, res) => {
  const msg = messageDAO.findById(parseInt(req.params.id));
  if (!msg || msg.user_id !== req.user.id) {
    return res.status(404).json({ error: '邮件不存在' });
  }
  const attachments = attachmentDAO.findByMessageId(msg.id);
  messageDAO.markRead(msg.id);
  res.json({ ...msg, attachments });
});

// POST /api/mail/send
router.post('/send', upload.array('attachments', 10), async (req, res) => {
  try {
    const { to, cc, bcc, subject, bodyHtml, bodyText, replyTo } = req.body;
    if (!to) {
      return res.status(400).json({ error: '收件人不能为空' });
    }

    // Validate subject length
    if (subject && subject.length > 500) {
      return res.status(400).json({ error: '主题最多500字符' });
    }

    // Validate body size (1MB)
    const bodySize = (bodyHtml || '').length + (bodyText || '').length;
    if (bodySize > 1024 * 1024) {
      return res.status(400).json({ error: '正文太大，最多1MB' });
    }

    // Rate limiting
    const recentCount = sendLogDAO.recentCount(req.user.id, 60000);
    if (recentCount >= config.rateLimit.sendPerMin) {
      return res.status(429).json({ error: `发送太频繁，每分钟最多${config.rateLimit.sendPerMin}封` });
    }

    const toAddrs = to.split(',').map(s => s.trim()).filter(Boolean);
    const ccAddrs = cc ? cc.split(',').map(s => s.trim()).filter(Boolean) : [];
    const bccAddrs = bcc ? bcc.split(',').map(s => s.trim()).filter(Boolean) : [];
    const allRecipients = [...toAddrs, ...ccAddrs, ...bccAddrs];

    // Validate email format
    const emailRegex = /^[^\s@]+@[^\s@]+$/;
    for (const addr of allRecipients) {
      if (!emailRegex.test(addr)) {
        return res.status(400).json({ error: `无效的邮箱地址: ${addr}` });
      }
    }

    const files = (req.files || []).map(f => ({
      filename: Buffer.from(f.originalname, 'latin1').toString('utf8'),
      path: f.path,
      size: f.size,
      mimeType: f.mimetype,
    }));

    // Send via SMTP
    const result = await sendMail({
      from: { name: req.user.display_name, address: req.user.email },
      to: toAddrs,
      cc: ccAddrs,
      bcc: bccAddrs,
      subject,
      html: bodyHtml || bodyText,
      text: bodyText,
      replyTo: replyTo || req.user.email,
      attachments: files,
    });

    // Save to SENT
    const totalSize = files.reduce((sum, f) => sum + f.size, 0);
    const msgId = messageDAO.create({
      userId: req.user.id,
      folder: 'SENT',
      messageId: result.messageId,
      uid: messageDAO.getNextUid(req.user.id),
      fromAddr: req.user.email,
      fromName: req.user.display_name,
      toAddr: toAddrs.join(', '),
      ccAddr: ccAddrs.join(', ') || null,
      bccAddr: bccAddrs.join(', ') || null,
      replyTo: replyTo || req.user.email,
      subject,
      bodyText: bodyText || '',
      bodyHtml: bodyHtml || '',
      hasAttach: files.length > 0,
      attachCount: files.length,
      sizeBytes: totalSize,
    });

    // Save attachment records
    for (const file of files) {
      attachmentDAO.create({
        messageId: msgId,
        filename: file.filename,
        mimeType: file.mimeType,
        sizeBytes: file.size,
        storagePath: file.path,
      });
    }

    // Log
    sendLogDAO.create({ userId: req.user.id, toAddr: toAddrs.join(', '), subject, status: 'sent' });
    userDAO.updateStorageUsed(req.user.id, totalSize);

    res.json({ message: '发送成功', messageId: result.messageId });
  } catch (err) {
    logger.error({ err }, 'Send error');
    sendLogDAO.create({ userId: req.user.id, toAddr: req.body.to, subject: req.body.subject, status: 'failed', errorMsg: err.message });
    res.status(500).json({ error: '发送失败: ' + err.message });
  }
});

// POST /api/mail/save-draft
router.post('/save-draft', (req, res) => {
  const { to, cc, bcc, subject, bodyHtml, bodyText } = req.body;
  const msgId = messageDAO.create({
    userId: req.user.id,
    folder: 'DRAFTS',
    messageId: `draft-${Date.now()}@${config.domain}`,
    uid: messageDAO.getNextUid(req.user.id),
    fromAddr: req.user.email,
    fromName: req.user.display_name,
    toAddr: to || '',
    ccAddr: cc || null,
    bccAddr: bcc || null,
    subject: subject || '(无主题)',
    bodyText: bodyText || '',
    bodyHtml: bodyHtml || '',
  });
  res.json({ message: '草稿已保存', id: msgId });
});

// PUT /api/mail/:id/read
router.put('/:id/read', (req, res) => {
  messageDAO.markRead(parseInt(req.params.id));
  res.json({ message: 'ok' });
});

// PUT /api/mail/:id/unread
router.put('/:id/unread', (req, res) => {
  messageDAO.markUnread(parseInt(req.params.id));
  res.json({ message: 'ok' });
});

// PUT /api/mail/:id/star
router.put('/:id/star', (req, res) => {
  messageDAO.toggleStar(parseInt(req.params.id));
  res.json({ message: 'ok' });
});

// DELETE /api/mail/:id
router.delete('/:id', (req, res) => {
  const msg = messageDAO.findById(parseInt(req.params.id));
  if (!msg || msg.user_id !== req.user.id) return res.status(404).json({ error: '邮件不存在' });
  if (msg.folder === 'TRASH') {
    messageDAO.permanentDelete(msg.id);
  } else {
    messageDAO.moveToFolder(msg.id, 'TRASH');
  }
  res.json({ message: 'ok' });
});

// PUT /api/mail/:id/restore
router.put('/:id/restore', (req, res) => {
  const msg = messageDAO.findById(parseInt(req.params.id));
  if (!msg || msg.user_id !== req.user.id) return res.status(404).json({ error: '邮件不存在' });
  const folder = msg.in_reply_to ? 'INBOX' : 'INBOX';
  messageDAO.moveToFolder(msg.id, 'INBOX');
  res.json({ message: '已恢复' });
});

// POST /api/mail/empty-trash
router.post('/empty-trash', (req, res) => {
  messageDAO.emptyTrash(req.user.id);
  res.json({ message: '垃圾箱已清空' });
});

// GET /api/mail/:id/attachments/:aid/download
router.get('/:id/attachments/:aid/download', (req, res) => {
  const att = attachmentDAO.findById(parseInt(req.params.aid));
  if (!att) return res.status(404).json({ error: '附件不存在' });
  const msg = messageDAO.findById(att.message_id);
  if (!msg || msg.user_id !== req.user.id) return res.status(403).json({ error: '无权访问' });
  res.download(att.storage_path, att.filename);
});

// GET /api/mail/:id/attachments/download-all
router.get('/:id/attachments/download-all', (req, res) => {
  const msgId = parseInt(req.params.id);
  const msg = messageDAO.findById(msgId);
  if (!msg || msg.user_id !== req.user.id) return res.status(403).json({ error: '无权访问' });
  const attachments = attachmentDAO.findByMessageId(msgId);
  if (!attachments.length) return res.status(404).json({ error: '没有附件' });

  res.setHeader('Content-Type', 'application/zip');
  res.setHeader('Content-Disposition', 'attachment; filename="attachments.zip"');
  
  const archive = archiver('zip', { zlib: { level: 9 } });
  archive.pipe(res);
  for (const att of attachments) {
    archive.file(att.storage_path, { name: att.filename });
  }
  archive.finalize();
});

module.exports = router;
