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
