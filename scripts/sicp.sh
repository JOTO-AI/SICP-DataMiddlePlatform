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
    "${compose[@]}" run --rm --no-deps --entrypoint /usr/local/bin/sicpctl query validate query-service
    "${compose[@]}" run --rm --no-deps worker /usr/local/bin/sicpctl validate sync-worker
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
  initialize|sync|force-full|rerun)
    [[ $# -ge 1 ]] || { printf '%s\n' 'Supply a stable idempotency key; rerun also needs the old task ID.' >&2; exit 1; }
    "${compose[@]}" run --rm --no-deps ctl "$operation" "$@"
    ;;
  task) [[ $# == 1 ]] || exit 1; "${compose[@]}" run --rm --no-deps ctl status "$1" ;;
  status)
    "${compose[@]}" ps
    for service in query worker; do
      port=8080; if [[ "$service" == worker ]]; then port=8081; fi
      "${compose[@]}" exec -T "$service" /usr/local/bin/sicpctl health "http://127.0.0.1:$port/readyz" || true
    done
    ;;
  stop) "${compose[@]}" stop ;;
  restart) "${compose[@]}" restart query worker ;;
  down) "${compose[@]}" down ;; # Volumes deliberately retained.
  *) printf '%s\n' 'Usage: scripts/sicp.sh prepare|build|validate|infra|migrate|start|deploy|initialize KEY|sync KEY|force-full KEY|rerun KEY TASK|task TASK|status|stop|restart|down'; exit 2 ;;
esac
