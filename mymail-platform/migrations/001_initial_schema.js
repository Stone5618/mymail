/**
 * Initial schema migration.
 *
 * NOTE: For new setups you can run `npm run db:migrate` instead of
 * `node scripts/init-db.js`. The init-db.js script is kept for backward
 * compatibility but knex migrations are the preferred way going forward.
 */

exports.up = function (knex) {
  return knex.schema
    .createTable('users', (table) => {
      table.increments('id').primary();
      table.string('username').notNullable().unique();
      table.string('email').notNullable().unique();
      table.string('password_hash').notNullable();
      table.string('display_name');
      table.string('role').defaultTo('user');
      table.integer('storage_limit').defaultTo(104857600);
      table.integer('storage_used').defaultTo(0);
      table.integer('is_active').defaultTo(1);
      table.integer('login_fails').defaultTo(0);
      table.datetime('locked_until');
      table.text('signature');
      table.datetime('created_at').defaultTo(knex.fn.now());
      table.datetime('updated_at').defaultTo(knex.fn.now());
    })
    .createTable('messages', (table) => {
      table.increments('id').primary();
      table.integer('user_id').notNullable().references('id').inTable('users');
      table.string('folder').defaultTo('INBOX');
      table.string('message_id');
      table.integer('uid');
      table.string('from_addr').notNullable();
      table.string('from_name');
      table.string('to_addr').notNullable();
      table.string('cc_addr');
      table.string('bcc_addr');
      table.string('reply_to');
      table.text('subject');
      table.text('body_text');
      table.text('body_html');
      table.integer('is_read').defaultTo(0);
      table.integer('is_starred').defaultTo(0);
      table.integer('is_deleted').defaultTo(0);
      table.integer('has_attach').defaultTo(0);
      table.integer('attach_count').defaultTo(0);
      table.integer('size_bytes').defaultTo(0);
      table.text('headers_raw');
      table.string('in_reply_to');
      table.text('flags').defaultTo('[]');
      table.datetime('received_at').defaultTo(knex.fn.now());

      table.index(['user_id', 'folder'], 'idx_msg_user_folder');
      table.index('received_at', 'idx_msg_received');
    })
    .createTable('attachments', (table) => {
      table.increments('id').primary();
      table.integer('message_id').notNullable().references('id').inTable('messages').onDelete('CASCADE');
      table.string('filename').notNullable();
      table.string('mime_type');
      table.integer('size_bytes');
      table.string('storage_path').notNullable();
      table.datetime('created_at').defaultTo(knex.fn.now());
    })
    .createTable('send_log', (table) => {
      table.increments('id').primary();
      table.integer('user_id').notNullable().references('id').inTable('users');
      table.string('to_addr').notNullable();
      table.text('subject');
      table.string('status').defaultTo('pending');
      table.text('error_msg');
      table.datetime('sent_at').defaultTo(knex.fn.now());
    })
    .createTable('settings', (table) => {
      table.string('key').primary();
      table.text('value');
      table.datetime('updated_at').defaultTo(knex.fn.now());
    });
};

exports.down = function (knex) {
  return knex.schema
    .dropTableIfExists('send_log')
    .dropTableIfExists('attachments')
    .dropTableIfExists('messages')
    .dropTableIfExists('settings')
    .dropTableIfExists('users');
};
