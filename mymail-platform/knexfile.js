const path = require('path');

module.exports = {
  development: {
    client: 'better-sqlite3',
    connection: {
      filename: path.resolve('./data/mymail.db'),
    },
    migrations: {
      directory: path.resolve('./migrations'),
    },
    useNullAsDefault: true,
  },

  production: {
    client: 'better-sqlite3',
    connection: {
      filename: path.resolve(process.env.DB_PATH || './data/mymail.db'),
    },
    migrations: {
      directory: path.resolve('./migrations'),
    },
    useNullAsDefault: true,
  },
};
