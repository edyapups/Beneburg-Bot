#!/usr/bin/env bash
# Build and deploy an image directly to a Docker host over SSH, without a registry.
# Usage: RELEASE_HOST=user@example.com scripts/release.sh v1.2.3
set -euo pipefail

usage() {
  cat <<'EOF'
Usage: RELEASE_HOST=user@host scripts/release.sh TAG

Required environment:
  RELEASE_HOST       SSH destination, for example deploy@example.com

Optional environment:
  RELEASE_DIR        Directory with docker-compose.yml on the server (default: /opt/beneburg)
  RELEASE_ENV_FILE   Environment file in RELEASE_DIR on the server (default: .env.local)
  RELEASE_IMAGE_REPO Image name to create (default: beneburg)
  RELEASE_PLATFORM   Target image platform (default: linux/amd64)
  RELEASE_SQLITE_DB  Local path to a SQLite database to seed into the volume.
                     Use only on the first deployment; it is never overwritten.

The tag is pushed to origin only after the image is successfully deployed.
EOF
}

[[ $# -eq 1 ]] || { usage >&2; exit 2; }
tag="$1"
[[ "$tag" =~ ^[A-Za-z0-9][A-Za-z0-9._-]*$ ]] || { echo "Invalid image/tag name: $tag" >&2; exit 2; }
: "${RELEASE_HOST:?RELEASE_HOST is required}"

release_dir="${RELEASE_DIR:-/opt/beneburg}"
release_env_file="${RELEASE_ENV_FILE:-.env.local}"
image_repo="${RELEASE_IMAGE_REPO:-beneburg}"
release_platform="${RELEASE_PLATFORM:-linux/amd64}"
image="$image_repo:$tag"

[[ -z "$(git status --porcelain)" ]] || { echo "Refusing to release a dirty worktree" >&2; exit 1; }
git rev-parse --verify "refs/tags/$tag" >/dev/null 2>&1 && { echo "Tag already exists: $tag" >&2; exit 1; }
[[ -f docker-compose.yml ]] || { echo "Run from the repository root" >&2; exit 1; }

# A remote server needs only this Compose file and its own secret .env.local.
ssh "$RELEASE_HOST" "mkdir -p '$release_dir'"
scp docker-compose.yml "$RELEASE_HOST:$release_dir/docker-compose.yml"

git tag -a "$tag" -m "Release $tag"
tag_created=1
cleanup() {
  status=$?
  if (( status != 0 )) && [[ "${tag_created:-}" == 1 ]]; then
    echo "Deployment failed; local tag $tag was kept and was not pushed." >&2
  fi
}
trap cleanup EXIT

DOCKER_DEFAULT_PLATFORM="$release_platform" PLATFORM="$release_platform" IMAGE="$image" docker compose build server
docker save "$image" | gzip | ssh "$RELEASE_HOST" 'gunzip | docker load'

if [[ -n "${RELEASE_SQLITE_DB:-}" ]]; then
  [[ -f "$RELEASE_SQLITE_DB" ]] || { echo "SQLite file not found: $RELEASE_SQLITE_DB" >&2; exit 1; }
  # Create a stopped container to create/attach the named volume, then copy
  # the database before the application has a chance to initialise an empty one.
  # Python's tar writer deliberately does not include macOS extended attributes
  # (such as com.apple.provenance), which Linux Docker volumes cannot accept.
  # It also gives the imported database the application’s expected file name.
  python3 - "$RELEASE_SQLITE_DB" <<'PY' | ssh "$RELEASE_HOST" "cd '$release_dir' && IMAGE='$image' PLATFORM='$release_platform' docker compose --env-file '$release_env_file' create server >/dev/null && container=\$(IMAGE='$image' PLATFORM='$release_platform' docker compose --env-file '$release_env_file' ps --all -q server) && test -n \"\$container\" && if docker cp \"\$container:/data/beneburg.db\" - >/dev/null 2>&1; then echo 'Refusing to overwrite /data/beneburg.db' >&2; exit 1; fi; docker cp - \"\$container:/data/\""
import os
import sys
import tarfile

path = sys.argv[1]
stat = os.stat(path)
info = tarfile.TarInfo("beneburg.db")
info.size = stat.st_size
info.mode = 0o600
info.mtime = int(stat.st_mtime)
with tarfile.open(fileobj=sys.stdout.buffer, mode="w|") as archive:
    with open(path, "rb") as database_file:
        archive.addfile(info, database_file)
PY
fi

ssh "$RELEASE_HOST" "cd '$release_dir' && test -f '$release_env_file' && IMAGE='$image' PLATFORM='$release_platform' docker compose --env-file '$release_env_file' up -d --no-build --force-recreate server"
git push origin "$tag"
trap - EXIT
echo "Released $image to $RELEASE_HOST"
