#!/usr/bin/env bash
set -euo pipefail

usage() {
  cat >&2 <<'EOF'
Usage: RELEASE_HOST=user@host scripts/release.sh [--sqlite-to-postgres-migrate] TAG

The migration flag is required exactly once for the first SQLite to PostgreSQL
release. Stop the old bot container manually before running it. Later releases
use scripts/release.sh TAG. Secrets stay in the server's RELEASE_ENV_FILE.
EOF
}

migration_mode=normal
if [[ "${1:-}" == --sqlite-to-postgres-migrate ]]; then
  migration_mode=migrate
  shift
fi
if [[ $# -ne 1 || "$1" == --* ]]; then
  usage
  exit 2
fi
tag=$1
[[ "$tag" =~ ^[A-Za-z0-9][A-Za-z0-9._-]*$ ]] || { usage; exit 2; }
: "${RELEASE_HOST:?RELEASE_HOST is required}"

release_dir=${RELEASE_DIR:-/opt/beneburg}
release_env_file=${RELEASE_ENV_FILE:-.env.local}
image_repo=${RELEASE_IMAGE_REPO:-beneburg}
release_platform=${RELEASE_PLATFORM:-linux/amd64}
image="$image_repo:$tag"

[[ "$release_dir" =~ ^/[A-Za-z0-9_./-]+$ && "$release_dir" != *..* ]] || { echo "Invalid RELEASE_DIR" >&2; exit 2; }
[[ "$release_env_file" =~ ^[A-Za-z0-9_.-]+$ ]] || { echo "Invalid RELEASE_ENV_FILE" >&2; exit 2; }
[[ "$image_repo" =~ ^[A-Za-z0-9_./-]+$ ]] || { echo "Invalid RELEASE_IMAGE_REPO" >&2; exit 2; }
[[ "$release_platform" =~ ^[A-Za-z0-9_/-]+$ ]] || { echo "Invalid RELEASE_PLATFORM" >&2; exit 2; }
[[ -z "${RELEASE_SQLITE_DB:-}" ]] || { echo "RELEASE_SQLITE_DB is unsupported for PostgreSQL releases" >&2; exit 2; }
[[ -f docker-compose.yml && -f scripts/release-remote.sh ]] || { echo "Run from the repository root" >&2; exit 2; }
[[ -z "$(git status --porcelain)" ]] || { echo "Refusing to release a dirty worktree" >&2; exit 1; }

if git show-ref --verify --quiet "refs/tags/$tag"; then
  [[ "$(git rev-list -n 1 "$tag")" == "$(git rev-parse HEAD)" ]] || { echo "Tag $tag points to another commit" >&2; exit 1; }
fi

remote_command="bash -s -- '$migration_mode' '$image' '$release_dir' '$release_env_file' '$release_platform'"
ssh "$RELEASE_HOST" "$remote_command preflight" < scripts/release-remote.sh

candidate="$release_dir/docker-compose.yml.next-$tag"
scp docker-compose.yml "$RELEASE_HOST:$candidate"
DOCKER_DEFAULT_PLATFORM="$release_platform" PLATFORM="$release_platform" IMAGE="$image" \
  POSTGRES_DB=build POSTGRES_USER=build POSTGRES_PASSWORD=build docker compose build server
docker save "$image" | gzip | ssh "$RELEASE_HOST" 'gunzip | docker load'
ssh "$RELEASE_HOST" "$remote_command deploy" < scripts/release-remote.sh

if ! git show-ref --verify --quiet "refs/tags/$tag"; then
  git tag -a "$tag" -m "Release $tag"
fi
remote_tag=$(git ls-remote --tags origin "refs/tags/$tag^{}" | awk '{print $1}')
if [[ -n "$remote_tag" && "$remote_tag" != "$(git rev-parse HEAD)" ]]; then
  echo "Remote tag $tag points to another commit" >&2
  exit 1
fi
if [[ -z "$remote_tag" ]]; then
  git push origin "$tag"
fi
echo "Released $image to $RELEASE_HOST"
