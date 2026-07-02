const nodemailer = require('nodemailer');
const config = require('../config');

const sendPort = config.smtp.sendPort;
const transporter = nodemailer.createTransport({
  host: config.smtp.sendHost,
  port: sendPort,
  // Port 465 = implicit TLS, Port 587 = STARTTLS
  secure: sendPort === 465,
  requireTLS: sendPort === 587,
  tls: {
    rejectUnauthorized: config.smtp.tlsRejectUnauthorized,
  },
});

async function sendMail({ from, to, cc, bcc, subject, html, text, replyTo, attachments }) {
  const mailOptions = {
    from: `"${from.name}" <${from.address}>`,
    to: to.join(', '),
    cc: cc && cc.length ? cc.join(', ') : undefined,
    bcc: bcc && bcc.length ? bcc.join(', ') : undefined,
    subject,
    html,
    text,
    replyTo,
    attachments: attachments.map(a => ({
      filename: a.filename,
      path: a.path,
      contentType: a.mimeType,
    })),
  };

  const info = await transporter.sendMail(mailOptions);
  return { messageId: info.messageId };
}

// Send to local (same domain) - write directly to maildir
async function sendLocal({ from, to, subject, html, text, attachments }) {
  const fs = require('fs');
  const path = require('path');
  const { simpleParser } = require('mailparser');
  const { v4: uuidv4 } = require('uuid');

  for (const addr of to) {
    const [username, domain] = addr.split('@');
    if (domain === config.domain) {
      // Local delivery
      const maildir = path.join(config.mail.dirPath, domain, username, 'new');
      fs.mkdirSync(maildir, { recursive: true });
      
      const filename = `${Date.now()}.${uuidv4().slice(0, 8)}.${config.domain}`;
      const emailContent = [
        `From: "${from.name}" <${from.address}>`,
        `To: ${addr}`,
        `Subject: ${subject}`,
        `Date: ${new Date().toUTCString()}`,
        `Message-ID: <${uuidv4()}@${config.domain}>`,
        `MIME-Version: 1.0`,
        `Content-Type: text/html; charset=utf-8`,
        '',
        html || text || '',
      ].join('\r\n');

      fs.writeFileSync(path.join(maildir, filename), emailContent);
    }
  }
}

module.exports = { sendMail, sendLocal };
