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

## Local environment from a backup

Copy `.env.dev.example` to `.env.dev` and fill it with a **test** bot token,
test administrator and group IDs, and local PostgreSQL credentials. Do not use
production Telegram credentials in this file.

Start a clean local environment with an empty database:

```bash
make dev
```

The bot is then reachable with the test token. Send `/get_backup` to it from
the private chat of the configured test administrator, download the returned
`.dump` document, and create a new local environment restored from it:

```bash
make dev_from_backup BACKUP=/path/to/beneburg-YYYYMMDDTHHMMSSZ.dump
```

Each command recreates only the `beneburg-dev` Compose project's PostgreSQL
volume. It never reads `.env.local` and does not modify production containers
or volumes.
