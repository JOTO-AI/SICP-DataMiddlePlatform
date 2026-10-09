#!/bin/sh
set -eu
case "${QUERY_REPLICAS-1}" in
    ''|*[!0-9]*) echo 'QUERY_REPLICAS must be an integer from 1 to 32' >&2; exit 1 ;;
esac
if [ "${QUERY_REPLICAS-1}" -lt 1 ] || [ "${QUERY_REPLICAS-1}" -gt 32 ]; then
    echo 'QUERY_REPLICAS must be an integer from 1 to 32' >&2
    exit 1
fi
exec nginx -g 'daemon off;'
