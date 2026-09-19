#!/usr/bin/env bash
# Export the old GORM/MySQL tables into portable JSONL files.
# Required: MYSQL_DATABASE. Authentication uses the normal mysql client
# variables (MYSQL_HOST, MYSQL_PORT, MYSQL_USER, MYSQL_PASSWORD) or MYSQL_CNF.
set -euo pipefail

output_dir="${1:-mysql-export}"
: "${MYSQL_DATABASE:?MYSQL_DATABASE is required}"
mkdir -p "$output_dir"

mysql_args=(--batch --raw --skip-column-names --default-character-set=utf8mb4)
[[ -n "${MYSQL_HOST:-}" ]] && mysql_args+=(--host "$MYSQL_HOST")
[[ -n "${MYSQL_PORT:-}" ]] && mysql_args+=(--port "$MYSQL_PORT")
[[ -n "${MYSQL_USER:-}" ]] && mysql_args+=(--user "$MYSQL_USER")
[[ -n "${MYSQL_PASSWORD:-}" ]] && mysql_args+=(--password="$MYSQL_PASSWORD")
[[ -n "${MYSQL_CNF:-}" ]] && mysql_args+=(--defaults-extra-file="$MYSQL_CNF")

export_table() {
  local table="$1" columns="$2"
  mysql "${mysql_args[@]}" "$MYSQL_DATABASE" --execute "SELECT JSON_OBJECT($columns) FROM \`$table\` ORDER BY 1" > "$output_dir/$table.jsonl"
}

export_table users "'id', id, 'created_at', DATE_FORMAT(created_at, '%Y-%m-%dT%H:%i:%s.%fZ'), 'updated_at', DATE_FORMAT(updated_at, '%Y-%m-%dT%H:%i:%s.%fZ'), 'deleted_at', IF(deleted_at IS NULL, NULL, DATE_FORMAT(deleted_at, '%Y-%m-%dT%H:%i:%s.%fZ')), 'telegram_id', telegram_id, 'username', username, 'first_name', first_name, 'last_name', last_name, 'status', status"
export_table tokens "'uuid', uuid, 'user_telegram_id', user_telegram_id, 'expire_at', DATE_FORMAT(expire_at, '%Y-%m-%dT%H:%i:%s.%fZ')"
export_table forms "'id', id, 'created_at', DATE_FORMAT(created_at, '%Y-%m-%dT%H:%i:%s.%fZ'), 'updated_at', DATE_FORMAT(updated_at, '%Y-%m-%dT%H:%i:%s.%fZ'), 'deleted_at', IF(deleted_at IS NULL, NULL, DATE_FORMAT(deleted_at, '%Y-%m-%dT%H:%i:%s.%fZ')), 'user_telegram_id', user_telegram_id, 'name', name, 'age', age, 'gender', gender, 'about', about, 'hobbies', hobbies, 'work', work, 'education', education, 'cover_letter', cover_letter, 'contacts', contacts, 'status', status"

printf 'Exported users, tokens and forms to %s\n' "$output_dir"
