const { SMTPServer } = require('smtp-server');
const { simpleParser } = require('mailparser');
const fs = require('fs');
const path = require('path');
const { v4: uuidv4 } = require('uuid');
const config = require('../config');
const userDAO = require('../dao/user-dao');
const messageDAO = require('../dao/message-dao');
const attachmentDAO = require('../dao/attachment-dao');
const { checkSPF, checkDNSBL, calculateScore } = require('./smtp-validator');
const { runRulesForMessage } = require('./rule-engine');
const greylistDAO = require('../dao/greylist-dao');
const logger = require('../logger');

let wsService = null;

function setWsService(ws) {
  wsService = ws;
}

// Connection rate limiter: Map<string, { count: number, windowStart: number }>
const connectionRates = new Map();

function getGreylistKey(senderIp, sender, recipient) {
  return `${senderIp}:${sender}:${recipient}`;
}

function checkGreylist(senderIp, sender, recipient) {
  const key = getGreylistKey(senderIp, sender, recipient);
  const now = Date.now();
  const entry = greylistDAO.get(key);

  if (!entry) {
    // First time - record and reject
    greylistDAO.upsert(key, now, false);
    return { allowed: false, reason: 'greylist_first_attempt' };
  }

  if (now - entry.first_seen > config.smtp.greylist.ttlMs) {
    // Expired - treat as new
    greylistDAO.upsert(key, now, false);
    return { allowed: false, reason: 'greylist_expired' };
  }

  if (!entry.allowed && (now - entry.first_seen) >= config.smtp.greylist.delayMs) {
    // Enough time passed - allow
    greylistDAO.setAllowed(key);
    return { allowed: true, reason: 'greylist_passed' };
  }

  if (entry.allowed) {
    return { allowed: true, reason: 'greylist_cached' };
  }

  return { allowed: false, reason: 'greylist_wait' };
}

function checkConnectionRate(ip) {
  const now = Date.now();
  const entry = connectionRates.get(ip);

  if (!entry || now - entry.windowStart > config.smtp.rateWindowMs) {
    connectionRates.set(ip, { count: 1, windowStart: now });
    return true;
  }

  if (entry.count >= config.smtp.maxConnectionsPerIp) {
    return false;
  }

  entry.count++;
  return true;
}

// Clean up old entries periodically
setInterval(() => {
  greylistDAO.cleanup(config.smtp.greylist.ttlMs);
  const now = Date.now();
  for (const [ip, entry] of connectionRates) {
    if (now - entry.windowStart > config.smtp.rateWindowMs * 2) {
      connectionRates.delete(ip);
    }
  }
}, 60000);

function startSmtpReceiver() {
  const serverOptions = {
    authOptional: true,
    allowInsecureAuth: false,
    size: config.mail.maxAttachmentSize,

    // Enable STARTTLS if certs configured
    disabledCommands: [],
    secure: false,

    onConnect(session, callback) {
      const ip = session.remoteAddress;

      // Connection rate limiting
      if (!checkConnectionRate(ip)) {
        logger.warn({ ip }, 'SMTP connection rate limit exceeded');
        return callback(new Error('Too many connections from your IP'));
      }

      logger.info({ ip }, 'SMTP connection');
      callback();
    },

    onMailFrom(address, session, callback) {
      logger.debug({ from: address.address, ip: session.remoteAddress }, 'SMTP MAIL FROM');
      callback();
    },

    async onRcptTo(address, session, callback) {
      const recipient = address.address;
      const [username, domain] = recipient.split('@');

      // Only accept mail for our domain
      if (domain !== config.domain) {
        logger.info({ recipient }, 'Rejecting mail for unknown domain');
        return callback(new Error('Not our domain'));
      }

      // Check if recipient exists
      const user = userDAO.findByUsername(username);
      if (!user || !user.is_active) {
        logger.info({ recipient }, 'Rejecting mail for unknown/inactive user');
        return callback(new Error('User not found'));
      }

      // Greylisting
      const sender = session.envelope.mailFrom ? session.envelope.mailFrom.address : 'unknown';
      const greyResult = checkGreylist(session.remoteAddress, sender, recipient);
      if (!greyResult.allowed) {
        logger.info({ recipient, reason: greyResult.reason }, 'Greylist temporary rejection');
        return callback(new Error('Temporary failure - please try again later'));
      }

      callback();
    },

    onData(stream, session, callback) {
      let data = Buffer.alloc(0);
      stream.on('data', (chunk) => {
        data = Buffer.concat([data, chunk]);
      });

      stream.on('end', async () => {
        try {
          const parsed = await simpleParser(data);
          const recipients = session.envelope.rcptTo.map(r => r.address);
          const sender = session.envelope.mailFrom ? session.envelope.mailFrom.address : 'unknown';
          const senderDomain = sender.includes('@') ? sender.split('@')[1] : '';

          // SPF + DNSBL checks
          let spamScore = { score: 0, reasons: [] };
          if (config.spam.enabled) {
            const spfResult = config.spam.spfEnabled
              ? await checkSPF(session.remoteAddress, senderDomain, session.clientHostname)
              : { result: 'skip' };

            const dnsblResult = config.spam.dnsblEnabled
              ? await checkDNSBL(session.remoteAddress)
              : { listed: false, zones: [] };

            spamScore = calculateScore(spfResult, dnsblResult);

            if (spamScore.score >= config.spam.spamThreshold) {
              logger.warn({
                sender,
                ip: session.remoteAddress,
                score: spamScore.score,
                reasons: spamScore.reasons,
              }, 'Mail rejected - spam score too high');
              return callback(new Error('Message rejected as spam'));
            }

            if (spamScore.score >= config.spam.suspiciousThreshold) {
              logger.info({
                sender,
                score: spamScore.score,
                reasons: spamScore.reasons,
              }, 'Mail flagged as suspicious but delivered');
            }
          }

          for (const recipient of recipients) {
            const [username, domain] = recipient.split('@');

            const user = userDAO.findByUsername(username);
            if (!user || domain !== config.domain) {
              logger.info({ recipient }, 'Rejecting mail for unknown user');
              continue;
            }

            // Save to maildir
            const maildir = path.join(config.mail.dirPath, domain, username, 'new');
            fs.mkdirSync(maildir, { recursive: true });
            const filename = `${Date.now()}.${uuidv4().slice(0, 8)}.${config.domain}`;
            fs.writeFileSync(path.join(maildir, filename), data);

            // Save attachments
            const attachments = parsed.attachments || [];
            const attachRecords = [];
            if (attachments.length > 0) {
              const attachDir = path.join(config.mail.attachmentPath, String(user.id));
              fs.mkdirSync(attachDir, { recursive: true });
              for (const att of attachments) {
                const uniqueName = `${Date.now()}-${Math.round(Math.random() * 1e9)}-${att.filename}`;
                const attachPath = path.join(attachDir, uniqueName);
                fs.writeFileSync(attachPath, att.content);
                attachRecords.push({
                  filename: att.filename,
                  mimeType: att.contentType,
                  sizeBytes: att.size,
                  storagePath: attachPath,
                });
              }
            }

            // Save to database
            const folder = spamScore.score >= config.spam.suspiciousThreshold ? 'JUNK' : 'INBOX';
            const msgId = messageDAO.create({
              userId: user.id,
              folder,
              messageId: parsed.messageId || `<${uuidv4()}@${config.domain}>`,
              uid: messageDAO.getNextUid(user.id),
              fromAddr: sender,
              fromName: parsed.from ? parsed.from.text : sender,
              toAddr: recipient,
              ccAddr: parsed.cc ? parsed.cc.text : null,
              replyTo: parsed.replyTo ? parsed.replyTo.text : null,
              subject: parsed.subject || '(无主题)',
              bodyText: parsed.text || '',
              bodyHtml: parsed.html || parsed.text || '',
              hasAttach: attachRecords.length > 0,
              attachCount: attachRecords.length,
              sizeBytes: data.length,
              headersRaw: JSON.stringify(Object.fromEntries(parsed.headers || [])),
              inReplyTo: parsed.inReplyTo || null,
            });

            // Save attachment records
            for (const att of attachRecords) {
              attachmentDAO.create({ messageId: msgId, ...att });
            }

            // Update storage
            userDAO.updateStorageUsed(user.id, data.length);

            // Run user mail rules
            runRulesForMessage(user.id, { id: msgId, ...messageDAO.findById(msgId) });

            // Notify via WebSocket
            if (wsService && folder === 'INBOX') {
              wsService.notifyNewMail(user.id, {
                id: msgId,
                from: sender,
                subject: parsed.subject || '(无主题)',
                time: new Date().toISOString(),
              });
            }

            logger.info({
              sender,
              recipient,
              subject: parsed.subject,
              spamScore: spamScore.score,
              folder,
            }, 'Mail delivered');
          }

          callback();
        } catch (err) {
          logger.error({ err }, 'SMTP receive error');
          callback(err);
        }
      });
    },
  };

  // Configure TLS if certs are provided
  if (config.smtp.tls.cert && config.smtp.tls.key) {
    try {
      serverOptions.secure = true;
      serverOptions.key = fs.readFileSync(config.smtp.tls.key);
      serverOptions.cert = fs.readFileSync(config.smtp.tls.cert);
      logger.info('SMTP TLS enabled with provided certificates');
    } catch (err) {
      logger.warn({ err }, 'Failed to load SMTP TLS certs, falling back to STARTTLS');
      serverOptions.secure = false;
      serverOptions.disabledCommands = [];
    }
  } else {
    // No TLS certs - allow STARTTLS opportunistically
    serverOptions.disabledCommands = [];
    logger.info('SMTP running without TLS (configure SMTP_TLS_CERT/SMTP_TLS_KEY for encryption)');
  }

  const server = new SMTPServer(serverOptions);

  server.on('error', (err) => {
    logger.error({ err }, 'SMTP server error');
  });

  server.listen(config.smtp.port, '0.0.0.0', () => {
    logger.info(`SMTP receiver listening on port ${config.smtp.port}`);
  });

  return server;
}

module.exports = { startSmtpReceiver, setWsService };
