const pino = require('pino');

const isTest = process.env.NODE_ENV === 'test';

module.exports = pino({
  level: isTest ? 'silent' : (process.env.LOG_LEVEL || 'info'),
  transport: !isTest && process.env.NODE_ENV !== 'production'
    ? { target: 'pino-pretty', options: { colorize: true } }
    : undefined,
});
