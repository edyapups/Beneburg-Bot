package database

type migration struct {
	version int
	sql     string
}

// Migrations are deliberately hand-written and additive. Append a migration;
// never edit one that may already have been applied in production.
var migrations = []migration{
	{1, `CREATE TABLE users (id INTEGER PRIMARY KEY AUTOINCREMENT, created_at DATETIME NOT NULL, updated_at DATETIME NOT NULL, deleted_at DATETIME, telegram_id INTEGER NOT NULL UNIQUE, username TEXT, first_name TEXT NOT NULL DEFAULT '', last_name TEXT, status TEXT NOT NULL DEFAULT 'new')`},
	{2, `CREATE TABLE tokens (uuid TEXT NOT NULL UNIQUE, user_telegram_id INTEGER PRIMARY KEY, expire_at DATETIME NOT NULL, FOREIGN KEY(user_telegram_id) REFERENCES users(telegram_id))`},
	{3, `CREATE TABLE forms (id INTEGER PRIMARY KEY AUTOINCREMENT, created_at DATETIME NOT NULL, updated_at DATETIME NOT NULL, deleted_at DATETIME, user_telegram_id INTEGER NOT NULL, name TEXT NOT NULL, age INTEGER, gender TEXT NOT NULL DEFAULT 'undefined', about TEXT, hobbies TEXT, work TEXT, education TEXT, cover_letter TEXT, contacts TEXT, status TEXT NOT NULL DEFAULT 'new', FOREIGN KEY(user_telegram_id) REFERENCES users(telegram_id))`},
	{4, `CREATE INDEX forms_user_created_at_idx ON forms(user_telegram_id, created_at DESC); CREATE INDEX tokens_uuid_idx ON tokens(uuid)`},
}
