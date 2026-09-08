#!/bin/sh
set -e

# MemeIndex ships a non-root user (memeindex, uid 10001), but the container
# starts as root so it can take ownership of a data directory that a previously
# root-running image - or the host - created. It then drops to memeindex before
# exec'ing the app, so a deployment with no host shell access needs no manual
# `chown` of the bind mount.
#
# If the runtime already forced a non-root user (Kubernetes runAsNonRoot,
# `docker run --user`, a compose `user:` override), there is nothing to take and
# nothing to drop - just exec the command.

DATA_DIR="${MEMEINDEX_DATA_DIR:-data}"
case "$DATA_DIR" in
	/*) ;;
	*) DATA_DIR="/app/$DATA_DIR" ;;
esac

if [ "$(id -u)" = "0" ]; then
	mkdir -p "$DATA_DIR"
	if [ "$(stat -c '%u' "$DATA_DIR")" != "10001" ]; then
		echo "docker-entrypoint: taking ownership of $DATA_DIR for uid 10001"
		chown -R memeindex:memeindex "$DATA_DIR"
	fi
	exec gosu memeindex:memeindex "$@"
fi

exec "$@"
