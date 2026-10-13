#!/usr/bin/env bash
set -euo pipefail
provision_upstream() {
  : "${UPSTREAM_LOGGER_PASSWORD:?}"
  psql -v ON_ERROR_STOP=1 --username "$POSTGRES_USER" --dbname postgres \
    --set=upstream_password="$UPSTREAM_LOGGER_PASSWORD" <<'SQL'
SELECT format('CREATE ROLE sicp_upstream_logger LOGIN NOSUPERUSER NOCREATEDB NOCREATEROLE PASSWORD %L', :'upstream_password')
WHERE NOT EXISTS (SELECT 1 FROM pg_roles WHERE rolname='sicp_upstream_logger') \gexec
SELECT 'CREATE ROLE sicp_upstream_reader NOLOGIN NOSUPERUSER NOCREATEDB NOCREATEROLE'
WHERE NOT EXISTS (SELECT 1 FROM pg_roles WHERE rolname='sicp_upstream_reader') \gexec
SELECT 'CREATE DATABASE upstream_logs'
WHERE NOT EXISTS (SELECT 1 FROM pg_database WHERE datname='upstream_logs') \gexec
SQL
}
# Existing pg-log volumes do not rerun Docker's first-start initialization.
# This explicit provisioning path adds missing objects without changing passwords
# or existing data; it is also used during first-start initialization below.
if [[ "${1:-}" == --upstream-only ]]; then
  [[ "${SICP_CLUSTER:-}" == log || "${SICP_CLUSTER:-}" == upstream-log ]] || exit 1
  provision_upstream
  exit 0
fi
if [[ "${SICP_CLUSTER:-}" == upstream-log ]]; then
  provision_upstream
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
  log)
    createdb --username "$POSTGRES_USER" logs
    provision_upstream
    ;;
  *) printf '%s\n' 'Unknown cluster role' >&2; exit 1 ;;
esac
