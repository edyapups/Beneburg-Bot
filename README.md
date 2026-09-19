# Beneburg

The production bot uses PostgreSQL. SQLite is retained only as a read-only
source and archive for the one-time migration. The PostgreSQL service runs in
its own Compose container and persists data in `postgres_data`.

## First PostgreSQL release

Prepare `/opt/beneburg/.env.local` on the server using
[`scripts/server.env.example`](scripts/server.env.example). Keep permissions at
`0600`. Set `POSTGRES_DB`, `POSTGRES_USER`, `POSTGRES_PASSWORD`, and
`DATABASE_URL` to matching values. The application receives `DATABASE_URL`;
the PostgreSQL service receives the three `POSTGRES_*` values. Put the password
only in this server file. URL-encode special password characters in
`DATABASE_URL`. The URL uses host `postgres`, the Compose service name. The
example uses `sslmode=disable` only on the private Compose network.

Commit the release changes, then stop the existing `server` container manually
on the production host. Check that it is stopped before the local release:

```bash
ssh deploy@example.com 'docker stop server && docker inspect -f "{{.State.Running}}" server'
RELEASE_HOST=deploy@example.com scripts/release.sh --sqlite-to-postgres-migrate v1.1.0
```

`release.sh` checks the stopped container again before importing. It records
`restart=no` on that old container so a Docker daemon restart cannot revive it
during the transfer. It records
the original SQLite volume name in `/opt/beneburg/legacy-sqlite-volume`, keeps
the previous Compose file in `/opt/beneburg/docker-compose.yml.before-postgres`
and its image reference in `/opt/beneburg/legacy-server-image`,
and stores the verified snapshot in
`/opt/beneburg/sqlite-archive/beneburg-sqlite-archive.db`. Preserve both the
original SQLite volume and this archive. The importer mounts the original
volume read-only and never gives it to the new bot. A failed import leaves the
PostgreSQL transaction uncommitted and does not start the new bot.

If SSH or the tag push fails, rerun the same command and tag from the same
commit. A completed import is recognized by its PostgreSQL marker and archive
hash; rows are not imported again. If the archive is missing but the original
SQLite volume is intact, the importer recreates and verifies it. A changed
source, populated PostgreSQL without a matching marker, or running old bot
causes a refusal.
Once the bot is healthy, future releases use:

```bash
RELEASE_HOST=deploy@example.com scripts/release.sh v1.1.1
```

The local worktree must be clean. The script builds and sends the image over
SSH, starts the separate PostgreSQL container, imports only in migration mode,
starts the bot, checks `http://127.0.0.1:8080/healthz` on the server, then
creates and pushes the Git tag. Production secrets are neither passed as
command arguments nor printed in logs.

## DataGrip through SSH

Configure an SSH tunnel to the production host, with PostgreSQL host
`127.0.0.1`, port `5432`, and the database/user/password from the server env
file. Compose binds PostgreSQL only to the server's loopback address. Keep the
server firewall closed for port 5432 as an additional precaution; Docker's
published ports cannot be protected by UFW rules alone. Authentication stays
enabled because local processes and containers on the Compose network can
otherwise connect to PostgreSQL.

## Recovery

If import fails, the old bot remains stopped, the source volume is unchanged,
and PostgreSQL contains no partial application import. Fix the cause and rerun
the migration command. If the new bot fails its health check, the script stops
it and preserves PostgreSQL and the SQLite archive. Inspect
`docker compose --env-file .env.local logs server postgres` on the server,
correct the issue, and rerun the same release command.

Once the new bot has accepted writes, do not restart the SQLite version: doing
so would split the data history. For the first PostgreSQL release, deploy a
corrected PostgreSQL-compatible build while preserving its volume. Later
releases can roll back to an earlier PostgreSQL-compatible tag.

Only if no PostgreSQL writes have occurred, an operator can stop the new bot,
restore `docker-compose.yml.before-postgres`, and start the old image recorded
in `legacy-server-image` against the original SQLite volume. Check that the
old Compose project still resolves `sqlite_data` to the volume recorded in
`legacy-sqlite-volume` before starting it. A subsequent migration attempt must
use a fresh, deliberately prepared PostgreSQL target; the completed import
marker prevents copying the changed SQLite data into the old target.

On the server, the guarded manual steps are:

```bash
cd /opt/beneburg
docker compose --env-file .env.local stop server
cp docker-compose.yml docker-compose.yml.postgres-suspended
cp docker-compose.yml.before-postgres docker-compose.yml
IMAGE="$(cat legacy-server-image)" docker compose --env-file .env.local create --no-build --force-recreate server
test "$(docker inspect -f '{{range .Mounts}}{{if eq .Destination "/data"}}{{.Name}}{{end}}{{end}}' server)" = "$(cat legacy-sqlite-volume)"
docker start server
```

Stop at the `test` failure; it means the old bot would read the wrong volume.

## Development and checks

Ordinary `go test ./...` requires no PostgreSQL. Integration tests use an
isolated schema in a PostgreSQL database named by `TEST_POSTGRES_DSN` and run
only when explicitly requested:

```bash
go test ./...
TEST_POSTGRES_DSN='postgres://.../testdb?sslmode=disable' go test -tags=integration ./pkg/database
```

The previous MySQL-to-SQLite conversion tools remain available under
`scripts/` for historical data recovery. They are not part of the production
PostgreSQL release path.
