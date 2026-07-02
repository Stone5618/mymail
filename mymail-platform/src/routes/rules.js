const express = require('express');
const router = express.Router();
const ruleDAO = require('../dao/rule-dao');
const { authenticate } = require('../middleware/auth');
const logger = require('../logger');

router.use(authenticate);

// GET /api/rules — List user's rules
router.get('/', (req, res) => {
  const rules = ruleDAO.findByUserId(req.user.id);
  res.json({ rules });
});

// POST /api/rules — Create rule
router.post('/', (req, res) => {
  try {
    const { name, priority, conditions, actions } = req.body;
    if (!name || !conditions || !actions) {
      return res.status(400).json({ error: 'name, conditions, actions are required' });
    }
    if (!Array.isArray(conditions) || conditions.length === 0) {
      return res.status(400).json({ error: 'conditions must be a non-empty array' });
    }
    if (!Array.isArray(actions) || actions.length === 0) {
      return res.status(400).json({ error: 'actions must be a non-empty array' });
    }

    const validFields = ['from', 'to', 'subject', 'has_attachment', 'size'];
    const validOps = ['contains', 'equals', 'ends_with', 'gt', 'lt', 'regex'];
    const validActions = ['move', 'mark_read', 'star', 'delete', 'forward', 'flag'];

    for (const cond of conditions) {
      if (!validFields.includes(cond.field)) {
        return res.status(400).json({ error: `Invalid condition field: ${cond.field}` });
      }
      if (cond.op && !validOps.includes(cond.op)) {
        return res.status(400).json({ error: `Invalid condition operator: ${cond.op}` });
      }
    }

    for (const act of actions) {
      if (!validActions.includes(act.type)) {
        return res.status(400).json({ error: `Invalid action type: ${act.type}` });
      }
    }

    const id = ruleDAO.create({
      userId: req.user.id,
      name,
      priority: priority || 0,
      conditions,
      actions,
    });

    const rule = ruleDAO.findById(id);
    res.status(201).json({ rule });
  } catch (err) {
    logger.error({ err }, 'Create rule error');
    res.status(500).json({ error: '创建失败' });
  }
});

// PUT /api/rules/:id — Update rule
router.put('/:id', (req, res) => {
  const rule = ruleDAO.findById(parseInt(req.params.id));
  if (!rule || rule.user_id !== req.user.id) {
    return res.status(404).json({ error: '规则不存在' });
  }

  const { name, priority, conditions, actions, isActive } = req.body;
  ruleDAO.update(rule.id, { name, priority, conditions, actions, isActive });
  const updated = ruleDAO.findById(rule.id);
  res.json({ rule: updated });
});

// DELETE /api/rules/:id — Delete rule
router.delete('/:id', (req, res) => {
  const rule = ruleDAO.findById(parseInt(req.params.id));
  if (!rule || rule.user_id !== req.user.id) {
    return res.status(404).json({ error: '规则不存在' });
  }
  ruleDAO.delete(rule.id);
  res.json({ message: '已删除' });
});

module.exports = router;
