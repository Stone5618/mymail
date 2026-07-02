const bcrypt = require('bcrypt');
const apiKeyDAO = require('../dao/api-key-dao');
const userDAO = require('../dao/user-dao');
const logger = require('../logger');

/**
 * API Key authentication middleware.
 * Supports: Authorization: Bearer mk_xxxxxxxxxxxxxxxx
 */
async function apiAuth(req, res, next) {
  const authHeader = req.headers.authorization;
  if (!authHeader || !authHeader.startsWith('Bearer mk_')) {
    return res.status(401).json({ error: 'Missing or invalid API key' });
  }

  const keyPlain = authHeader.substring(7); // Remove "Bearer "

  try {
    // Find all active API keys and check hash
    // For efficiency, we store a prefix of the hash for lookup
    const allKeys = apiKeyDAO.findByUserId(0); // Can't search by user, need different approach

    // Better approach: store key prefix in DB for lookup
    // For now, iterate (acceptable for low-volume API key usage)
    const db = require('../dao/database');
    const keys = db.prepare('SELECT * FROM api_keys WHERE is_active = 1').all();

    let matchedKey = null;
    for (const key of keys) {
      if (await bcrypt.compare(keyPlain, key.key_hash)) {
        matchedKey = key;
        break;
      }
    }

    if (!matchedKey) {
      return res.status(401).json({ error: 'Invalid API key' });
    }

    const user = userDAO.findById(matchedKey.user_id);
    if (!user || !user.is_active) {
      return res.status(403).json({ error: 'Account disabled' });
    }

    // Check scope
    const scopes = JSON.parse(matchedKey.scopes || '["send"]');
    req.apiKey = {
      id: matchedKey.id,
      name: matchedKey.name,
      scopes,
      rateLimit: matchedKey.rate_limit,
    };
    req.user = user;
    req.authMethod = 'api-key';

    // Update last used
    apiKeyDAO.updateLastUsed(matchedKey.id);

    next();
  } catch (err) {
    logger.error({ err }, 'API key auth error');
    res.status(500).json({ error: 'Authentication error' });
  }
}

/**
 * Check if API key has required scope
 */
function requireScope(scope) {
  return (req, res, next) => {
    if (!req.apiKey || !req.apiKey.scopes.includes(scope)) {
      return res.status(403).json({ error: `API key missing scope: ${scope}` });
    }
    next();
  };
}

module.exports = { apiAuth, requireScope };
