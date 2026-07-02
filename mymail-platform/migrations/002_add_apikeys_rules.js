/**
 * Migration 002: Add is_default_password and api_keys table
 */

exports.up = function (knex) {
  return knex.schema
    .alterTable('users', (table) => {
      table.integer('is_default_password').defaultTo(0);
    })
    .createTable('api_keys', (table) => {
      table.increments('id').primary();
      table.integer('user_id').notNullable().references('id').inTable('users').onDelete('CASCADE');
      table.string('name').notNullable();
      table.string('key_hash').notNullable();
      table.text('scopes').defaultTo('["send"]');
      table.integer('rate_limit').defaultTo(10);
      table.integer('is_active').defaultTo(1);
      table.datetime('last_used_at');
      table.datetime('created_at').defaultTo(knex.fn.now());
    })
    .createTable('mail_rules', (table) => {
      table.increments('id').primary();
      table.integer('user_id').notNullable().references('id').inTable('users').onDelete('CASCADE');
      table.string('name').notNullable();
      table.integer('priority').defaultTo(0);
      table.text('conditions').notNullable();
      table.text('actions').notNullable();
      table.integer('is_active').defaultTo(1);
      table.datetime('created_at').defaultTo(knex.fn.now());
    })
    .createTable('spam_log', (table) => {
      table.increments('id').primary();
      table.string('sender_ip');
      table.string('sender_addr');
      table.string('recipient_addr');
      table.integer('spam_score').defaultTo(0);
      table.text('reasons');
      table.string('action').defaultTo('delivered');
      table.datetime('created_at').defaultTo(knex.fn.now());
    });
};

exports.down = function (knex) {
  return knex.schema
    .dropTableIfExists('spam_log')
    .dropTableIfExists('mail_rules')
    .dropTableIfExists('api_keys')
    .alterTable('users', (table) => {
      table.dropColumn('is_default_password');
    });
};
