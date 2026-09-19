#!/usr/bin/env bash
set -euo pipefail

mode=$1
image=$2
release_dir=$3
env_file=$4
platform=$5
action=$6
cd "$release_dir"

compose() {
  IMAGE="$image" PLATFORM="$platform" docker compose --env-file "$env_file" "$@"
}

source_volume() {
  if docker container inspect server >/dev/null 2>&1; then
    running=$(docker inspect --format '{{.State.Running}}' server)
    current_image=$(docker inspect --format '{{.Config.Image}}' server)
    legacy_volume=$(docker inspect --format '{{range .Mounts}}{{if eq .Destination "/data"}}{{.Name}}{{end}}{{end}}' server)
    if [[ "$running" == true && "$current_image" != "$image" ]]; then
      echo "Old server container is still running; stop it manually before migration" >&2
      exit 1
    fi
    if [[ "$running" == true && -n "$legacy_volume" ]]; then
      echo "SQLite server container is still running" >&2
      exit 1
    fi
    if [[ -n "$legacy_volume" ]]; then
      printf '%s\n' "$legacy_volume"
      return
    fi
  fi
  if [[ -f legacy-sqlite-volume ]]; then
    read -r legacy_volume < legacy-sqlite-volume
    printf '%s\n' "$legacy_volume"
    return
  fi
  echo "Cannot find the old SQLite volume; refusing migration" >&2
  exit 1
}

preflight() {
  [[ -f "$env_file" ]] || { echo "Server environment file is missing" >&2; exit 1; }
  command -v docker >/dev/null
  command -v curl >/dev/null
  command -v flock >/dev/null
  if [[ "$mode" == migrate ]]; then
    legacy_volume=$(source_volume)
    [[ -n "$legacy_volume" ]] || { echo "SQLite volume name is empty" >&2; exit 1; }
    docker volume inspect "$legacy_volume" >/dev/null
  else
    if docker container inspect server >/dev/null 2>&1; then
      legacy_volume=$(docker inspect --format '{{range .Mounts}}{{if eq .Destination "/data"}}{{.Name}}{{end}}{{end}}' server)
      [[ -z "$legacy_volume" ]] || { echo "SQLite container detected; use --sqlite-to-postgres-migrate" >&2; exit 1; }
    fi
  fi
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

if [[ "$mode" == migrate ]]; then
  legacy_volume=$(source_volume)
  if docker container inspect server >/dev/null 2>&1; then
    old_mount=$(docker inspect --format '{{range .Mounts}}{{if eq .Destination "/data"}}{{.Name}}{{end}}{{end}}' server)
    if [[ -n "$old_mount" ]]; then
      docker update --restart=no server >/dev/null
      if [[ ! -f legacy-server-image ]]; then
        docker inspect --format '{{.Config.Image}}' server > legacy-server-image
        chmod 600 legacy-server-image
      fi
    fi
  fi
  if [[ ! -f docker-compose.yml.before-postgres ]]; then
    [[ ! -f legacy-sqlite-volume ]] || { echo "Previous Compose backup is missing" >&2; exit 1; }
    cp docker-compose.yml docker-compose.yml.before-postgres
  fi
  [[ -f legacy-server-image ]] || { echo "Old server image reference is missing" >&2; exit 1; }
  printf '%s\n' "$legacy_volume" > legacy-sqlite-volume
  chmod 600 legacy-sqlite-volume
fi
mv "$candidate" docker-compose.yml
compose up -d --no-build postgres
wait_for_postgres

if [[ "$mode" == migrate ]]; then
  legacy_volume=$(source_volume)
  archive_dir="$release_dir/sqlite-archive"
  mkdir -p "$archive_dir"
  chmod 700 "$archive_dir"
  postgres_container=$(compose ps -q postgres)
  network=$(docker inspect --format '{{range $name, $_ := .NetworkSettings.Networks}}{{$name}}{{end}}' "$postgres_container")
  [[ -n "$network" ]] || { echo "PostgreSQL network not found" >&2; exit 1; }
  docker run --rm --env-file "$release_dir/$env_file" --network "$network" \
    --mount "type=volume,source=$legacy_volume,target=/legacy,readonly" \
    --mount "type=bind,source=$archive_dir,target=/archive" \
    "$image" migrate-sqlite --source /legacy/beneburg.db --archive /archive
fi

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
