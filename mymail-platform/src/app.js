const express = require('express');
const helmet = require('helmet');
const cors = require('cors');
const rateLimit = require('express-rate-limit');
const path = require('path');
const config = require('./config');
const logger = require('./logger');

const app = express();

// Security middleware
app.use(helmet({
  contentSecurityPolicy: {
    directives: {
      defaultSrc: ["'self'"],
      scriptSrc: ["'self'", "'unsafe-inline'", "cdn.quilljs.com", "cdn.jsdelivr.net"],
      scriptSrcAttr: null,
      styleSrc: ["'self'", "'unsafe-inline'", "fonts.googleapis.com", "cdn.quilljs.com", "cdn.jsdelivr.net"],
      fontSrc: ["'self'", "fonts.gstatic.com", "cdn.quilljs.com"],
      imgSrc: ["'self'", "data:", "blob:"],
      connectSrc: ["'self'", "ws:", "wss:"],
      objectSrc: ["'none'"],
      baseUri: ["'self'"],
      formAction: ["'self'"],
      frameAncestors: ["'none'"],
    },
  },
  crossOriginEmbedderPolicy: false,
  crossOriginResourcePolicy: { policy: "cross-origin" },
}));
app.use(cors());
app.use(express.json({ limit: '50mb' }));
app.use(express.urlencoded({ extended: true, limit: '50mb' }));

// Request logging middleware
app.use((req, res, next) => {
  const start = Date.now();
  res.on('finish', () => {
    logger.info({
      method: req.method,
      path: req.path,
      status: res.statusCode,
      responseTime: Date.now() - start,
    }, 'request');
  });
  next();
});

// Rate limiting
app.use('/api/', rateLimit({
  windowMs: config.rateLimit.windowMs,
  max: config.rateLimit.max,
  message: { error: '请求太频繁，请稍后再试' },
}));

// Static files
app.use(express.static(path.join(__dirname, '../public')));

// API routes
app.use('/api/auth', require('./routes/auth'));
app.use('/api', require('./routes/api-v1'));
app.use('/api/mail', require('./routes/mail'));
app.use('/api/admin', require('./routes/admin'));
app.use('/api/rules', require('./routes/rules'));

// SPA fallback
app.get('*', (req, res) => {
  if (req.path.startsWith('/api/')) {
    return res.status(404).json({ error: 'API not found' });
  }
  res.sendFile(path.join(__dirname, '../public/index.html'));
});

// Error handler
app.use((err, req, res, next) => {
  logger.error({ err }, 'Unhandled error');
  if (err.code === 'LIMIT_FILE_SIZE') {
    return res.status(413).json({ error: '文件太大，最大 25MB' });
  }
  res.status(500).json({ error: err.message || '服务器内部错误' });
});

module.exports = app;
