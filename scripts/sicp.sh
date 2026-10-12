#!/usr/bin/env bash
set -euo pipefail
root="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/.." && pwd)"
compose=(docker compose --project-directory "$root" --env-file "$root/.env" --file "$root/compose.yaml")
if [[ "${SICP_DUAL_LOGS:-0}" == 1 ]]; then compose+=(--file "$root/compose.dual-logs.yaml"); fi
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
    if [[ "${SICP_DUAL_LOGS:-0}" == 1 ]]; then "${compose[@]}" up -d --wait --wait-timeout 120 db-upstream-log; fi
    for service in $("${compose[@]}" config --services); do
      if [[ "$service" == mock ]]; then "${compose[@]}" up -d --wait --wait-timeout 120 mock; fi
    done
    ;;
  migrate) "${compose[@]}" run --rm --no-deps migrate ;;
  start) "${compose[@]}" up -d --wait --wait-timeout 90 query-gateway worker console ;;
  deploy)
    "$0" prepare
    "$0" build
    "$0" validate
    "$0" infra
    "$0" migrate
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
  *) printf '%s\n' 'Usage: scripts/sicp.sh prepare|build|validate|infra|migrate|start|deploy|status|stop|restart|down|gateway-apply --url ORIGIN [--ca-file FILE]|gateway-renew --url ORIGIN [--ca-file FILE]'; exit 2 ;;
esac
