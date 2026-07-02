/**
 * Spam Filter — System-level spam scoring engine
 *
 * Scoring:
 *   SPF check:      +0 pass, +5 softfail, +10 fail
 *   DNSBL check:    +15 if listed
 *   Sender domain:  +5 if no MX/A record
 *   Content check:  +3 for suspicious patterns
 *
 * Thresholds (configurable via .env):
 *   score >= spamThreshold (10)   → reject with 550
 *   score >= suspiciousThreshold (5) → add X-Spam-Score header, deliver
 *   score < suspiciousThreshold    → deliver normally
 */
const { checkSPF, checkDNSBL, validateSenderDomain } = require('./smtp-validator');
const config = require('../config');

// Suspicious content patterns (common spam indicators)
const SUSPICIOUS_PATTERNS = [
  /viagra|cialis|pharmacy/i,
  /you('ve)? (been selected|won|have been chosen)/i,
  /click (here|below) (to|and) (claim|receive|get)/i,
  /act (now|immediately)|limited time offer/i,
  /nigerian|prince|inheritance|beneficiary/i,
  /make money (fast|from home|online)/i,
  /weight loss|lose weight fast/i,
  /casino|lottery|winner/i,
  /buy (now|cheap)|discount (viagra|cialis)/i,
  /urgent.{0,20}response.{0,20}required/i,
];

/**
 * Run the full spam scoring pipeline on a parsed email.
 * @param {object} params
 * @param {string} params.senderEmail - envelope sender (e.g. "user@example.com")
 * @param {string} params.senderIP - connecting IP address
 * @param {string} params.subject - email subject
 * @param {string} params.bodyText - plain text body
 * @param {string} params.bodyHtml - HTML body
 * @param {number} params.sizeBytes - message size
 * @returns {Promise<{score: number, details: object, isSpam: boolean, isSuspicious: boolean}>}
 */
async function scoreMessage({ senderEmail, senderIP, subject, bodyText, bodyHtml, sizeBytes }) {
  const details = {};
  let score = 0;

  // --- 1. SPF Check ---
  const spfResult = await checkSPF(senderEmail, senderIP);
  details.spf = spfResult;
  if (spfResult === 'fail') score += 10;
  else if (spfResult === 'softfail') score += 5;
  // pass (+0) and none/temperror (+0) — don't penalize on temp errors

  // --- 2. DNSBL Check ---
  const dnsblListed = await checkDNSBL(senderIP);
  details.dnsbl = dnsblListed;
  if (dnsblListed) score += 15;

  // --- 3. Sender Domain Validation ---
  const domain = senderEmail.split('@')[1];
  const domainValid = domain ? await validateSenderDomain(domain) : false;
  details.domainValid = domainValid;
  if (!domainValid) score += 5;

  // --- 4. Content Analysis ---
  const content = `${subject || ''} ${bodyText || ''} ${bodyHtml || ''}`;
  const suspiciousMatches = SUSPICIOUS_PATTERNS.filter(re => re.test(content));
  details.contentSuspicious = suspiciousMatches.length > 0;
  details.contentMatches = suspiciousMatches.length;
  if (suspiciousMatches.length > 0) score += 3;

  // --- Build result ---
  const isSpam = score >= config.spam.spamThreshold;
  const isSuspicious = !isSpam && score >= config.spam.suspiciousThreshold;

  return {
    score,
    details,
    isSpam,
    isSuspicious,
  };
}

module.exports = { scoreMessage };
