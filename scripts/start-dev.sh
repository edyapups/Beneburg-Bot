#!/usr/bin/env bash

set -euo pipefail

project_name="beneburg-dev"
environment_file=".env.dev"
compose_files=(--env-file "$environment_file" -p "$project_name" -f docker-compose.dev.yml)

if [[ $# -gt 1 ]]; then
  echo "Usage: $0 [path-to-backup.dump]" >&2
  exit 2
fi

if [[ ! -f "$environment_file" ]]; then
  echo "Create $environment_file from .env.dev.example and fill it with test credentials." >&2
  exit 2
fi

backup_path=""
if [[ $# -eq 1 ]]; then
  backup_path="$1"
  if [[ ! -f "$backup_path" || ! -r "$backup_path" ]]; then
    echo "Backup file is not readable: $backup_path" >&2
    exit 2
  fi
fi

compose() {
  docker compose "${compose_files[@]}" "$@"
}

cleanup() {
  echo
  echo "Stopping and removing the local development environment..."
  compose down --volumes --remove-orphans
}

# The named project isolates these volumes from production and other Compose stacks.
compose down --volumes --remove-orphans
compose up -d postgres

for attempt in $(seq 1 30); do
  if compose exec -T postgres pg_isready >/dev/null 2>&1; then
    break
  fi
  if [[ "$attempt" -eq 30 ]]; then
    echo "Local PostgreSQL did not become ready." >&2
    exit 1
  fi
  sleep 1
done

if [[ -n "$backup_path" ]]; then
  postgres_container_id="$(compose ps -q postgres)"
  if [[ -z "$postgres_container_id" ]]; then
    echo "Could not locate local PostgreSQL container." >&2
    exit 1
  fi
  docker cp "$backup_path" "$postgres_container_id:/tmp/restore.dump"
  compose exec -T postgres sh -c 'pg_restore --clean --if-exists --no-owner --no-privileges --username="$POSTGRES_USER" --dbname="$POSTGRES_DB" /tmp/restore.dump'
  compose exec -T postgres rm -f /tmp/restore.dump
fi

compose up -d --build server
echo "Local environment is ready at http://127.0.0.1:${LOCAL_HTTP_PORT:-8081}"
echo "Following server logs. Press Ctrl+C to stop and remove the environment."
trap 'cleanup; exit 130' INT
trap 'cleanup; exit 143' TERM
compose logs --follow server
