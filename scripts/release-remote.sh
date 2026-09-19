#!/usr/bin/env bash
set -euo pipefail

image=$1
release_dir=$2
env_file=$3
platform=$4
action=$5
cd "$release_dir"

compose() {
  IMAGE="$image" PLATFORM="$platform" docker compose --env-file "$env_file" "$@"
}

preflight() {
  [[ -f "$env_file" ]] || { echo "Server environment file is missing" >&2; exit 1; }
  command -v docker >/dev/null
  command -v curl >/dev/null
  command -v flock >/dev/null
}

wait_for_postgres() {
  postgres_container=$(compose ps -q postgres)
  [[ -n "$postgres_container" ]] || { echo "PostgreSQL container did not start" >&2; exit 1; }
  for attempt in $(seq 1 40); do
    status=$(docker inspect --format '{{.State.Health.Status}}' "$postgres_container")
    [[ "$status" == healthy ]] && return
    [[ "$status" == unhealthy ]] && { echo "PostgreSQL health check failed" >&2; exit 1; }
    sleep 3
  done
  echo "PostgreSQL did not become healthy" >&2
  exit 1
}

preflight
if [[ "$action" == preflight ]]; then
  exit 0
fi
[[ "$action" == deploy ]] || { echo "Unknown release action" >&2; exit 2; }
exec 9> .release.lock
flock -n 9 || { echo "Another release is running on this server" >&2; exit 1; }
candidate="docker-compose.yml.next-${image##*:}"
[[ -f "$candidate" ]] || { echo "Staged Compose file is missing" >&2; exit 1; }

mv "$candidate" docker-compose.yml
compose up -d --no-build postgres
wait_for_postgres

cleanup_server() {
  compose stop server >/dev/null 2>&1 || true
}
trap cleanup_server ERR
compose up -d --no-build --force-recreate server
for attempt in $(seq 1 30); do
  if curl --silent --show-error --fail --max-time 2 http://127.0.0.1:8080/healthz >/dev/null 2>&1; then
    trap - ERR
    echo "Bot health check passed"
    exit 0
  fi
  sleep 2
done
echo "Bot health check failed; stopping the new bot" >&2
cleanup_server
exit 1
