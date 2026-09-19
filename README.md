# Beneburg

The bot uses PostgreSQL. The Compose service stores data in the `postgres_data`
volume.

## Server configuration

Prepare `/opt/beneburg/.env.local` from
[`scripts/server.env.example`](scripts/server.env.example) and set its mode to
`0600`. Set `POSTGRES_DB`, `POSTGRES_USER`, `POSTGRES_PASSWORD`, and
`DATABASE_URL` to matching values. The application receives `DATABASE_URL`; the
PostgreSQL service receives the three `POSTGRES_*` variables. URL-encode special
password characters in `DATABASE_URL`. The connection URL uses host `postgres`,
the Compose service name; `sslmode=disable` is appropriate only for the private
Compose network.

The application creates its PostgreSQL schema on first start and verifies the
supported schema version on every later start.

## Release

From a clean worktree, run:

```bash
RELEASE_HOST=deploy@example.com scripts/release.sh v1.1.1
```

The script builds the image, streams it to the server, starts PostgreSQL, waits
for its health check, starts the bot, checks
`http://127.0.0.1:8080/healthz`, then creates and pushes the Git tag. Secrets
are read from the server environment file and are not printed in logs.

## DataGrip through SSH

Configure an SSH tunnel to the production host with PostgreSQL host
`127.0.0.1`, port `5432`, and credentials from the server environment file.
Compose binds PostgreSQL only to loopback; keep the firewall closed for port
`5432`.

## Development and checks

Ordinary tests do not require PostgreSQL:

```bash
go test ./...
```

PostgreSQL integration tests create an isolated schema in the database named by
`TEST_POSTGRES_DSN`:

```bash
TEST_POSTGRES_DSN='postgres://.../testdb?sslmode=disable' go test -tags=integration ./pkg/database
```
