const http = require('http');
const fs = require('fs');
const path = require('path');
const config = require('./config');
const logger = require('./logger');
const app = require('./app');

// Ensure data directories exist
for (const dir of [config.mail.dirPath, config.mail.attachmentPath, path.dirname(config.db.path)]) {
  fs.mkdirSync(dir, { recursive: true });
}

// Initialize database
require('./dao/database');

const server = http.createServer(app);

// WebSocket
const wsService = require('./services/ws-service');
wsService.init(server);

// Start SMTP receiver
const { startSmtpReceiver, setWsService } = require('./services/smtp-receiver');
setWsService(wsService);
const smtpServer = startSmtpReceiver();

// Start server
server.listen(config.port, config.host, () => {
  logger.info(`📧 MyMail Platform running at http://${config.host}:${config.port}`);
  logger.info(`   Domain: ${config.domain}`);
  logger.info(`   SMTP Receiver: port ${config.smtp.port}`);
  logger.info(`   Database: ${config.db.path}`);
});

// Graceful shutdown
function gracefulShutdown(signal) {
  logger.info({ signal }, 'Received shutdown signal');

  // Close SMTP server
  if (smtpServer && typeof smtpServer.close === 'function') {
    smtpServer.close(() => logger.info('SMTP server closed'));
  }

  // Close WebSocket connections
  wsService.shutdown();

  // Close HTTP server
  server.close(() => logger.info('HTTP server closed'));

  // Close database
  try {
    const db = require('./dao/database');
    if (typeof db.close === 'function') db.close();
  } catch (_) {}

  setTimeout(() => {
    process.exit(1);
  }, 10000);
}

process.on('SIGTERM', () => gracefulShutdown('SIGTERM'));
process.on('SIGINT', () => gracefulShutdown('SIGINT'));
process.on('uncaughtException', (err) => {
  logger.fatal({ err }, 'Uncaught');
  gracefulShutdown('uncaughtException');
});
process.on('unhandledRejection', (reason) => {
  logger.error({ reason }, 'Unhandled rejection');
});

module.exports = { app, server };
