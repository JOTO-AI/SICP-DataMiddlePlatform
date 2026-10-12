#!/usr/bin/env bash
set -euo pipefail
if [[ "${SICP_CLUSTER:-}" == upstream-log ]]; then
  : "${UPSTREAM_LOGGER_PASSWORD:?}"
  psql -v ON_ERROR_STOP=1 --username "$POSTGRES_USER" --dbname postgres \
    --set=upstream_password="$UPSTREAM_LOGGER_PASSWORD" <<'SQL'
CREATE ROLE sicp_upstream_logger LOGIN NOSUPERUSER NOCREATEDB NOCREATEROLE PASSWORD :'upstream_password';
CREATE ROLE sicp_upstream_reader NOLOGIN NOSUPERUSER NOCREATEDB NOCREATEROLE;
SQL
  createdb --username "$POSTGRES_USER" upstream_logs
  exit 0
fi
: "${QUERY_PASSWORD:?}" "${WORKER_PASSWORD:?}" "${LOGGER_PASSWORD:?}" "${SICP_CLUSTER:?}"
psql -v ON_ERROR_STOP=1 --username "$POSTGRES_USER" --dbname postgres \
  --set=query_password="$QUERY_PASSWORD" --set=worker_password="$WORKER_PASSWORD" --set=logger_password="$LOGGER_PASSWORD" <<'SQL'
CREATE ROLE sicp_query LOGIN PASSWORD :'query_password';
CREATE ROLE sicp_worker LOGIN PASSWORD :'worker_password';
CREATE ROLE sicp_logger LOGIN PASSWORD :'logger_password';
SQL
case "$SICP_CLUSTER" in
  M|S)
    createdb --username "$POSTGRES_USER" slot0
    createdb --username "$POSTGRES_USER" slot1
    if [[ "$SICP_CLUSTER" == M ]]; then createdb --username "$POSTGRES_USER" control; fi
    ;;
  log) createdb --username "$POSTGRES_USER" logs ;;
  *) printf '%s\n' 'Unknown cluster role' >&2; exit 1 ;;
esac
