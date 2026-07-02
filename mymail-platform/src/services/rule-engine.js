const ruleDAO = require('../dao/rule-dao');
const messageDAO = require('../dao/message-dao');
const logger = require('../logger');

/**
 * Evaluate a single condition against a message.
 */
function matchCondition(condition, message) {
  const { field, op, value } = condition;

  let fieldValue;
  switch (field) {
    case 'from':
      fieldValue = (message.from_addr || '').toLowerCase();
      break;
    case 'to':
      fieldValue = (message.to_addr || '').toLowerCase();
      break;
    case 'subject':
      fieldValue = (message.subject || '').toLowerCase();
      break;
    case 'has_attachment':
      return condition.value === true || condition.value === 'true'
        ? message.has_attach === 1
        : message.has_attach !== 1;
    case 'size':
      fieldValue = message.size_bytes || 0;
      break;
    default:
      return false;
  }

  if (field === 'size') {
    const numVal = parseInt(value);
    switch (op) {
      case 'gt': return fieldValue > numVal;
      case 'lt': return fieldValue < numVal;
      case 'equals': return fieldValue === numVal;
      default: return false;
    }
  }

  const strVal = String(value || '').toLowerCase();
  switch (op) {
    case 'contains':
      return fieldValue.includes(strVal);
    case 'equals':
      return fieldValue === strVal;
    case 'ends_with':
      return fieldValue.endsWith(strVal);
    case 'regex':
      try {
        return new RegExp(strVal, 'i').test(fieldValue);
      } catch {
        return false;
      }
    default:
      return false;
  }
}

/**
 * Execute a single action on a message.
 */
function executeAction(action, message, userId) {
  switch (action.type) {
    case 'move':
      if (action.folder) {
        messageDAO.moveToFolder(message.id, action.folder);
        logger.debug({ msgId: message.id, folder: action.folder }, 'Rule: moved message');
      }
      break;
    case 'mark_read':
      messageDAO.markRead(message.id);
      break;
    case 'star':
      if (!message.is_starred) {
        messageDAO.toggleStar(message.id);
      }
      break;
    case 'delete':
      messageDAO.moveToFolder(message.id, 'TRASH');
      break;
    case 'flag':
      // Could add a flag field in future
      break;
    case 'forward':
      // Forwarding would require SMTP send - log for now
      logger.info({ msgId: message.id, forwardTo: action.address }, 'Rule: forward requested');
      break;
    default:
      break;
  }
}

/**
 * Run all active rules for a user against a new message.
 */
function runRulesForMessage(userId, message) {
  try {
    const rules = ruleDAO.findActiveByUserId(userId);
    if (!rules.length) return;

    for (const rule of rules) {
      const allMatch = rule.conditions.every(cond => matchCondition(cond, message));
      if (allMatch) {
        logger.info({ ruleId: rule.id, ruleName: rule.name, msgId: message.id }, 'Rule matched');
        for (const action of rule.actions) {
          executeAction(action, message, userId);
        }
        // Stop after first matching rule (priority-based)
        break;
      }
    }
  } catch (err) {
    logger.error({ err, userId, msgId: message.id }, 'Rule engine error');
  }
}

module.exports = { runRulesForMessage, matchCondition, executeAction };
