require('dotenv').config();
const path = require('path');

const config = {
  port: parseInt(process.env.PORT) || 3000,
  host: process.env.HOST || '0.0.0.0',
  
  domain: process.env.DOMAIN || 'localhost',
  mailHost: process.env.MAIL_HOST || 'mail.localhost',
  
  duckdns: {
    token: process.env.DUCKDNS_TOKEN,
    domain: process.env.DUCKDNS_DOMAIN,
  },
  
  jwt: {
    secret: process.env.JWT_SECRET || 'change-me-in-production',
    expiresIn: process.env.JWT_EXPIRES_IN || '24h',
    rememberExpiresIn: process.env.JWT_REMEMBER_EXPIRES_IN || '30d',
  },
  
  db: {
    path: path.resolve(process.env.DB_PATH || './data/mymail.db'),
  },
  
  mail: {
    dirPath: path.resolve(process.env.MAILDIR_PATH || './data/maildir'),
    attachmentPath: path.resolve(process.env.ATTACHMENT_PATH || './data/attachments'),
    maxAttachmentSize: parseInt(process.env.MAX_ATTACHMENT_SIZE) || 25 * 1024 * 1024,
  },
  
  admin: {
    username: process.env.ADMIN_USERNAME || 'admin',
    password: process.env.ADMIN_PASSWORD || 'CHANGE_ME',
    email: process.env.ADMIN_EMAIL || 'admin@localhost',
  },
  
  smtp: {
    port: parseInt(process.env.SMTP_PORT) || 25,
    sendHost: process.env.SMTP_SEND_HOST || process.env.MAIL_HOST || 'localhost',
    sendPort: parseInt(process.env.SMTP_SEND_PORT) || 587,
    tls: {
      cert: process.env.SMTP_TLS_CERT || null,
      key: process.env.SMTP_TLS_KEY || null,
    },
    tlsRejectUnauthorized: process.env.SMTP_TLS_REJECT_UNAUTHORIZED !== 'false',
    greylist: {
      delayMs: parseInt(process.env.GREYLIST_DELAY_MS) || 5 * 60 * 1000,
      ttlMs: parseInt(process.env.GREYLIST_TTL_MS) || 60 * 60 * 1000,
    },
    maxConnectionsPerIp: parseInt(process.env.SMTP_MAX_CONNECTIONS_PER_IP) || 10,
    rateWindowMs: parseInt(process.env.SMTP_RATE_WINDOW_MS) || 60000,
  },
  
  spam: {
    enabled: process.env.SPAM_FILTER_ENABLED !== 'false',   // enabled by default
    spamThreshold: parseInt(process.env.SPAM_THRESHOLD) || 10,
    suspiciousThreshold: parseInt(process.env.SPAM_SUSPICIOUS_THRESHOLD) || 5,
    dnsblEnabled: process.env.DNSBL_ENABLED !== 'false',    // enabled by default
    spfEnabled: process.env.SPF_ENABLED !== 'false',        // enabled by default
  },

  rateLimit: {
    windowMs: parseInt(process.env.RATE_LIMIT_WINDOW_MS) || 60000,
    max: parseInt(process.env.RATE_LIMIT_MAX) || 100,
    sendPerMin: parseInt(process.env.SEND_RATE_LIMIT_PER_MIN) || 10,
  },
  
  // Allowed attachment types
  allowedAttachmentTypes: [
    'application/zip', 'application/x-rar-compressed', 'application/x-7z-compressed',
    'application/pdf', 'application/msword', 'application/vnd.openxmlformats-officedocument.wordprocessingml.document',
    'application/vnd.ms-excel', 'application/vnd.openxmlformats-officedocument.spreadsheetml.sheet',
    'application/vnd.ms-powerpoint', 'application/vnd.openxmlformats-officedocument.presentationml.presentation',
    'image/jpeg', 'image/png', 'image/gif', 'image/webp', 'image/svg+xml',
    'text/plain', 'text/csv', 'application/json',
  ],
};

module.exports = config;
