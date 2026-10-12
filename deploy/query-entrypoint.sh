#!/bin/sh
set -eu
umask 077
# Slot 1 preserves the pre-replica log directory. Locks live outside it because
# the log journal requires a flat directory containing regular files only.
legacy=${LOG_DIRECTORY:?LOG_DIRECTORY is required}
root=${QUERY_REPLICA_DIRECTORY:-/var/lib/sicp/query-replicas}
mkdir -p "$root"
if [ "${1:-}" = /usr/local/bin/service ] && [ "${2:-}" = --prepare-log-cutover ] && [ "$#" -eq 2 ]; then
    original_upstream=${UPSTREAM_LOG_DIRECTORY:-}
    slot=1
    while [ "$slot" -le 32 ]; do
        LOG_DIRECTORY=$legacy
        if [ "$slot" -ne 1 ]; then LOG_DIRECTORY="$root/$slot"; fi
        if [ -d "$LOG_DIRECTORY" ]; then
            exec 9>"$root/$slot.lock"
            if ! flock -n 9; then echo 'A query writer still owns a log slot' >&2; exit 1; fi
            if [ "${LOG_STORAGE_MODE:-single_legacy}" = dual ]; then
                UPSTREAM_LOG_DIRECTORY=$original_upstream
                if [ "$slot" -ne 1 ]; then UPSTREAM_LOG_DIRECTORY="$root/upstream-$slot"; fi
                : "${UPSTREAM_LOG_DIRECTORY:?}"
                mkdir -p "$UPSTREAM_LOG_DIRECTORY"
                export UPSTREAM_LOG_DIRECTORY
            fi
            export LOG_DIRECTORY
            /usr/local/bin/service --prepare-log-cutover
            printf 'Prepared query log slot %s\n' "$slot"
            exec 9>&-
        fi
        slot=$((slot + 1))
    done
    exit 0
fi
slot=1
while [ "$slot" -le 32 ]; do
    exec 9>"$root/$slot.lock"
    if flock -n 9; then
        if [ "$slot" -eq 1 ]; then
            LOG_DIRECTORY=$legacy
        else
            LOG_DIRECTORY="$root/$slot"
        fi
        mkdir -p "$LOG_DIRECTORY"
        export LOG_DIRECTORY
		if [ "${LOG_STORAGE_MODE:-single_legacy}" = dual ]; then
			: "${UPSTREAM_LOG_DIRECTORY:?UPSTREAM_LOG_DIRECTORY is required}"
			if [ "$slot" -ne 1 ]; then UPSTREAM_LOG_DIRECTORY="$root/upstream-$slot"; fi
			mkdir -p "$UPSTREAM_LOG_DIRECTORY"
			export UPSTREAM_LOG_DIRECTORY
		fi
        # exec preserves FD 9: the kernel releases the lock only on process exit.
        # Never delete the lock file; a replacement inode would permit two owners.
        if [ "$#" -eq 0 ]; then set -- /usr/local/bin/service; fi
        case "$1" in -*) set -- /usr/local/bin/service "$@" ;; esac
        exec "$@"
    fi
    exec 9>&-
    slot=$((slot + 1))
done
echo 'No free query log directory (maximum 32 simultaneous processes)' >&2
exit 1
