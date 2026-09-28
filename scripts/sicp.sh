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
    "${compose[@]}" config --quiet
    "${compose[@]}" run --rm --no-deps --entrypoint /usr/local/bin/service query --check-config
    "${compose[@]}" run --rm --no-deps worker /usr/local/bin/service --check-config
    ;;
  build)
    "${compose[@]}" build
    ;;
  infra)
    "${compose[@]}" run --rm --no-deps volumes
    "${compose[@]}" up -d --wait --wait-timeout 120 db-m db-s db-log redis
    for service in $("${compose[@]}" config --services); do
      if [[ "$service" == mock ]]; then "${compose[@]}" up -d --wait --wait-timeout 120 mock; fi
    done
    ;;
  migrate) "${compose[@]}" run --rm --no-deps migrate ;;
  start) "${compose[@]}" up -d --wait --wait-timeout 90 query worker console ;;
  deploy)
    "$0" prepare
    "$0" build
    "$0" validate
    "$0" infra
    "$0" migrate
    "$0" start
    ;;
  status)
    "${compose[@]}" ps
    ;;
  stop) "${compose[@]}" stop ;;
  restart) "${compose[@]}" restart query worker ;;
  down) "${compose[@]}" down ;; # Volumes deliberately retained.
  *) printf '%s\n' 'Usage: scripts/sicp.sh prepare|build|validate|infra|migrate|start|deploy|status|stop|restart|down'; exit 2 ;;
esac
