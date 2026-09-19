# Beneburg

## Dependencies

- Go >1.19
- SQLite (embedded; no external database service)

## Setup
```bash
$ git clone git@github.com:edyapups/Beneburg.git

$ cd Beneburg

$ echo SQLITE_PATH=/data/beneburg.db >> .env.local
$ echo BOT_TOKEN=your_bot_token >> .env.local
$ echo SERVER_PORT=your_server_port >> .env.local
```

## Run
```bash
$ docker-compose --env-file .env.local -f docker-compose.yml up
```

## Migration from MySQL

On the host that can reach the old MySQL server, run
`MYSQL_DATABASE=... MYSQL_USER=... MYSQL_PASSWORD=... scripts/export_mysql_data.sh export`.
It produces three JSONL files without SQL or credentials. Copy that directory
to the new host and run `scripts/mysql_export_to_sqlite.py export beneburg.db`.
The converter refuses to overwrite an existing database.

## Release without a container registry

Create `/opt/beneburg/.env.local` on the target server with the bot secrets.
Then from a clean local worktree run:

```bash
RELEASE_HOST=deploy@example.com scripts/release.sh v1.0.0
```

The script creates an annotated tag, builds a `linux/amd64` image locally,
streams it to `docker load` over SSH, starts it remotely without rebuilding,
and pushes the tag only after deployment succeeds. To initialise a fresh server
with an imported database, add `RELEASE_SQLITE_DB=$PWD/beneburg.db` to that
first run. The importer writes it to `/data/beneburg.db` without macOS xattrs.
