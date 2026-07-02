const dns = require('dns').promises;
const logger = require('../logger');

// DNSBL zones to check
const DNSBL_ZONES = [
  'zen.spamhaus.org',
  'bl.spamcop.net',
  'b.barracudacentral.org',
];

/**
 * Check SPF record for sender domain
 * Returns: { pass, softfail, neutral, fail, none, error }
 */
async function checkSPF(senderIp, senderDomain, heloDomain) {
  try {
    const txtRecords = await dns.resolveTxt(senderDomain);
    const spfRecord = txtRecords
      .map(r => r.join(''))
      .find(r => r.startsWith('v=spf1'));

    if (!spfRecord) {
      return { result: 'none', detail: 'No SPF record found' };
    }

    // Basic SPF mechanism check
    // Check if sender IP matches common mechanisms
    const mechanisms = spfRecord.split(' ');

    // Check ip4 mechanism
    for (const mech of mechanisms) {
      if (mech.startsWith('ip4:')) {
        const cidr = mech.substring(4);
        if (matchCIDR(senderIp, cidr)) {
          return { result: 'pass', detail: `IP matches ${mech}` };
        }
      }
      // Check mx mechanism (simplified - just check if domain has MX)
      if (mech === 'mx' || mech.startsWith('mx:')) {
        // For simplicity, we'll accept mx mechanism as pass
        // Full implementation would resolve MX and check IPs
      }
      // Check include mechanism
      if (mech.startsWith('include:')) {
        const includedDomain = mech.substring(8);
        const subResult = await checkSPF(senderIp, includedDomain, heloDomain);
        if (subResult.result === 'pass') {
          return { result: 'pass', detail: `Matched include:${includedDomain}` };
        }
      }
    }

    // Check for -all (hard fail) or ~all (softfail)
    const lastMech = mechanisms[mechanisms.length - 1];
    if (lastMech === '-all') {
      return { result: 'fail', detail: 'SPF hard fail (-all)' };
    }
    if (lastMech === '~all') {
      return { result: 'softfail', detail: 'SPF softfail (~all)' };
    }
    if (lastMech === '?all') {
      return { result: 'neutral', detail: 'SPF neutral (?all)' };
    }

    return { result: 'softfail', detail: 'No matching mechanism found' };
  } catch (err) {
    if (err.code === 'ENOTFOUND' || err.code === 'ENODATA') {
      return { result: 'none', detail: 'Domain has no DNS records' };
    }
    logger.debug({ err, senderDomain }, 'SPF check error');
    return { result: 'error', detail: err.message };
  }
}

/**
 * Check if IP is in DNSBL blacklists
 * Returns: { listed: boolean, zones: string[] }
 */
async function checkDNSBL(ip) {
  const reversed = ip.split('.').reverse().join('.');
  const listed = [];

  const checks = DNSBL_ZONES.map(async (zone) => {
    try {
      await dns.resolve4(`${reversed}.${zone}`);
      listed.push(zone);
    } catch {
      // Not listed (NXDOMAIN = good)
    }
  });

  await Promise.all(checks);

  return {
    listed: listed.length > 0,
    zones: listed,
  };
}

/**
 * Simple CIDR matching (supports /8, /16, /24, /32)
 */
function matchCIDR(ip, cidr) {
  if (!cidr.includes('/')) {
    return ip === cidr;
  }
  const [network, bits] = cidr.split('/');
  const mask = ~(2 ** (32 - parseInt(bits)) - 1);
  const ipNum = ipToNum(ip);
  const netNum = ipToNum(network);
  return (ipNum & mask) === (netNum & mask);
}

function ipToNum(ip) {
  return ip.split('.').reduce((acc, octet) => (acc << 8) + parseInt(octet), 0) >>> 0;
}

/**
 * Calculate spam score based on checks
 * Returns: { score, reasons }
 */
function calculateScore(spfResult, dnsblResult) {
  let score = 0;
  const reasons = [];

  // SPF scoring
  if (spfResult.result === 'fail') {
    score += 8;
    reasons.push('SPF hard fail');
  } else if (spfResult.result === 'softfail') {
    score += 3;
    reasons.push('SPF softfail');
  } else if (spfResult.result === 'none') {
    score += 2;
    reasons.push('No SPF record');
  } else if (spfResult.result === 'error') {
    score += 1;
    reasons.push('SPF check error');
  }

  // DNSBL scoring
  if (dnsblResult.listed) {
    score += 10;
    reasons.push(`IP blacklisted (${dnsblResult.zones.join(', ')})`);
  }

  return { score, reasons };
}

module.exports = { checkSPF, checkDNSBL, calculateScore };
