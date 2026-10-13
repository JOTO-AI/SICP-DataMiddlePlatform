#!/usr/bin/env bash
set -euo pipefail
root="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/.." && pwd)"
compose=(docker compose --project-directory "$root" --env-file "$root/.env" --file "$root/compose.yaml")
operation="${1:-help}"
if (($#)); then shift; fi
case "$operation" in
  prepare)
    umask 077
    if [[ ! -e "$root/.env" ]]; then cp -- "$root/.env.example" "$root/.env"; fi
    printf '%s\n' 'Prepared root .env; existing configuration was preserved.'
    ;;
  validate)
    python3 "$root/scripts/gateway.py" validate
    ;;
  build)
    "${compose[@]}" build
    ;;
  infra)
    "${compose[@]}" run --rm --no-deps volumes
    "${compose[@]}" up -d --wait --wait-timeout 120 db-m db-s db-log redis
    "${compose[@]}" exec -T db-log bash /docker-entrypoint-initdb.d/10-sicp.sh --upstream-only
    for service in $("${compose[@]}" config --services); do
      if [[ "$service" == mock ]]; then "${compose[@]}" up -d --wait --wait-timeout 120 mock; fi
    done
    ;;
  migrate) "${compose[@]}" run --rm --no-deps migrate ;;
  log-register|log-cutover)
    overrides=()
    if [[ "$operation" == log-register ]]; then
      # Only this one-off preparation uses legacy mode. Normal services are dual.
      overrides=(-e LOG_STORAGE_MODE=single_legacy
        -e DATABASE_UPSTREAM_LOG_URL= -e DATABASE_UPSTREAM_LOG_USER= -e DATABASE_UPSTREAM_LOG_PASSWORD=
        -e DATABASE_UPSTREAM_LOG_READ_URL= -e DATABASE_UPSTREAM_LOG_MAINTENANCE_URL=
        -e UPSTREAM_LOG_DIRECTORY= -e UPSTREAM_LOG_MAX_BYTES= -e UPSTREAM_LOG_MAX_FILES=)
    fi
    "${compose[@]}" stop query query-gateway worker console
    "${compose[@]}" run --rm --no-deps "${overrides[@]}" query /usr/local/bin/service --prepare-log-cutover
    "${compose[@]}" run --rm --no-deps "${overrides[@]}" worker /usr/local/bin/service --prepare-log-cutover
    ;;
  start) "${compose[@]}" up -d --wait --wait-timeout 90 query-gateway worker console ;;
  deploy)
    "$0" prepare
    "$0" build
    "$0" validate
    "${compose[@]}" stop query query-gateway worker console
    "$0" infra
    "$0" migrate
    "$0" log-cutover
    "$0" start
    python3 "$root/scripts/gateway.py" verify "$@"
    ;;
  status)
    "${compose[@]}" ps
    ;;
  stop) "${compose[@]}" stop ;;
  restart) "${compose[@]}" restart query query-gateway worker ;;
  down) "${compose[@]}" down ;; # Volumes deliberately retained.
  gateway-apply|gateway-renew) python3 "$root/scripts/gateway.py" "$operation" "$@" ;;
  *) printf '%s\n' 'Usage: scripts/sicp.sh prepare|build|validate|infra|migrate|log-register|log-cutover|start|deploy|status|stop|restart|down|gateway-apply --url ORIGIN [--ca-file FILE]|gateway-renew --url ORIGIN [--ca-file FILE]'; exit 2 ;;
esac
