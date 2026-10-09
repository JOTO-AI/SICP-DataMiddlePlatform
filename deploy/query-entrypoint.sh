#!/bin/sh
set -eu
umask 077
# Slot 1 preserves the pre-replica log directory. Locks live outside it because
# the log journal requires a flat directory containing regular files only.
legacy=${LOG_DIRECTORY:?LOG_DIRECTORY is required}
root=${QUERY_REPLICA_DIRECTORY:-/var/lib/sicp/query-replicas}
mkdir -p "$root"
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
