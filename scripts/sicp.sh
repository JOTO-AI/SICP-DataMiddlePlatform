#!/usr/bin/env bash
set -euo pipefail
root="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/.." && pwd)"
project="${SICP_PROJECT:-sicp-phase1}"
[[ "$project" =~ ^[a-z0-9][a-z0-9_-]*$ ]] || { printf '%s\n' 'Invalid SICP_PROJECT' >&2; exit 1; }
compose=(docker compose --project-name "$project" --file "$root/deploy/compose.yaml")
operation="${1:-help}"
if (($#)); then shift; fi
case "$operation" in
  prepare)
    umask 077
    mkdir -p "$root/deploy/runtime"
    for name in query worker stack migrate admin; do
      if [[ ! -e "$root/deploy/runtime/$name.env" ]]; then cp -- "$root/deploy/examples/$name.env.example" "$root/deploy/runtime/$name.env"; fi
    done
    printf '%s\n' 'Prepared synthetic local configuration; existing files were preserved.'
    ;;
  validate)
    "${compose[@]}" config --quiet
    "${compose[@]}" run --rm --no-deps --entrypoint /usr/local/bin/sicpctl query validate query-service
    "${compose[@]}" run --rm --no-deps --entrypoint /usr/local/bin/sicpctl worker validate sync-worker
    ;;
  build) "${compose[@]}" build query worker mock ;;
  infra)
    "${compose[@]}" run --rm --no-deps volumes
    "${compose[@]}" up -d --wait --wait-timeout 120 db-m db-s db-log redis
    "${compose[@]}" up -d mock
    ;;
  migrate) "${compose[@]}" run --rm --no-deps migrate ;;
  start) "${compose[@]}" up -d --wait --wait-timeout 90 query worker ;;
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
