#!/bin/sh
set -eu

# /data is normally a bind mount, and a bind mount hides the ownership the
# image set on that path. Repair it once, then drop privileges so the server
# itself keeps running as the unprivileged vandyke user.
if [ "$(id -u)" = "0" ]; then
	data_dir=$(dirname "${DB_PATH:-/data/vandyke.db}")
	mkdir -p "$data_dir"
	if [ "$(stat -c %u "$data_dir")" != "10001" ]; then
		echo "vandyke: taking ownership of $data_dir" >&2
		chown -R vandyke:vandyke "$data_dir"
	fi
	exec su-exec vandyke:vandyke "$@"
fi

exec "$@"
