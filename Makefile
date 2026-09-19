.PHONY: server_local server_remote dev dev_from_backup

server_local:
	DOCKER_CONTEXT=local ENV=local zsh ./scripts/rebuild_server.sh

server_remote:
	DOCKER_CONTEXT=remote ENV=prod zsh ./scripts/rebuild_server.sh

# Recreates the isolated local development environment with an empty database.
dev:
	./scripts/start-dev.sh

# Usage: make dev_from_backup BACKUP=/absolute/or/relative/path/to/backup.dump
dev_from_backup:
	@test -n "$(BACKUP)" || { echo "Usage: make dev_from_backup BACKUP=/path/to/backup.dump" >&2; exit 2; }
	./scripts/start-dev.sh "$(BACKUP)"
