module.exports = {
  testEnvironment: 'node',
  setupFiles: ['./tests/setup.js'],
  testTimeout: 15000,
  // Run test files sequentially to avoid DB conflicts
  maxWorkers: 1,
};
