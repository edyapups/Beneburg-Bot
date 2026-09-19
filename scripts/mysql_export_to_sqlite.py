#!/usr/bin/env python3
"""Create a SQLite database from export_mysql_data.sh output.

Usage: scripts/mysql_export_to_sqlite.py mysql-export beneburg.db
The destination must not exist, preventing accidental replacement.
"""
import json
import sqlite3
import sys
from pathlib import Path

SCHEMA = '''
PRAGMA foreign_keys = ON;
CREATE TABLE users (id INTEGER PRIMARY KEY, created_at DATETIME NOT NULL, updated_at DATETIME NOT NULL, deleted_at DATETIME, telegram_id INTEGER NOT NULL UNIQUE, username TEXT, first_name TEXT NOT NULL DEFAULT '', last_name TEXT, status TEXT NOT NULL DEFAULT 'new');
CREATE TABLE tokens (uuid TEXT NOT NULL UNIQUE, user_telegram_id INTEGER PRIMARY KEY, expire_at DATETIME NOT NULL, FOREIGN KEY(user_telegram_id) REFERENCES users(telegram_id));
CREATE TABLE forms (id INTEGER PRIMARY KEY, created_at DATETIME NOT NULL, updated_at DATETIME NOT NULL, deleted_at DATETIME, user_telegram_id INTEGER NOT NULL, name TEXT NOT NULL, age INTEGER, gender TEXT NOT NULL DEFAULT 'undefined', about TEXT, hobbies TEXT, work TEXT, education TEXT, cover_letter TEXT, contacts TEXT, status TEXT NOT NULL DEFAULT 'new', FOREIGN KEY(user_telegram_id) REFERENCES users(telegram_id));
CREATE INDEX forms_user_created_at_idx ON forms(user_telegram_id, created_at DESC);
CREATE INDEX tokens_uuid_idx ON tokens(uuid);
'''
COLUMNS = {
    'users': ['id','created_at','updated_at','deleted_at','telegram_id','username','first_name','last_name','status'],
    'tokens': ['uuid','user_telegram_id','expire_at'],
    'forms': ['id','created_at','updated_at','deleted_at','user_telegram_id','name','age','gender','about','hobbies','work','education','cover_letter','contacts','status'],
}
FALLBACK_TIMESTAMP = '1970-01-01T00:00:00Z'

def normalized_row(table, row):
    """Keep legacy rows importable when old MySQL contains NULL timestamps."""
    if table in ('users', 'forms'):
        row = dict(row)
        row['created_at'] = row.get('created_at') or row.get('updated_at') or FALLBACK_TIMESTAMP
        row['updated_at'] = row.get('updated_at') or row['created_at']
    return row

def records(path):
    with path.open(encoding='utf-8') as source:
        for line in source:
            if line.strip(): yield json.loads(line)
def main():
    if len(sys.argv) != 3: raise SystemExit('usage: mysql_export_to_sqlite.py EXPORT_DIR DATABASE_PATH')
    source, destination = Path(sys.argv[1]), Path(sys.argv[2])
    if destination.exists(): raise SystemExit(f'refusing to overwrite {destination}')
    for table in COLUMNS:
        if not (source / f'{table}.jsonl').is_file(): raise SystemExit(f'missing {table}.jsonl')
    db = sqlite3.connect(destination)
    try:
        db.executescript(SCHEMA)
        for table, columns in COLUMNS.items():
            placeholders = ', '.join('?' for _ in columns)
            statement = f"INSERT INTO {table} ({', '.join(columns)}) VALUES ({placeholders})"
            db.executemany(statement, ([row.get(column) for column in columns] for row in (normalized_row(table, item) for item in records(source / f'{table}.jsonl'))))
        db.commit()
    finally: db.close()
if __name__ == '__main__': main()
