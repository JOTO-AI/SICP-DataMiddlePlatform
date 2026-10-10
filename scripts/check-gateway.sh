#!/usr/bin/env bash
# Linux CI only. Synthetic certificates live outside the uploaded evidence tree.
set -euo pipefail
cd "$(dirname "${BASH_SOURCE[0]}")/.."
test "${GITHUB_ACTIONS:-}" = true
test "${SICP_PROJECT:-}" = sicp-gateway-http
mkdir -p artifacts/gateway
record() {
  local name=$1
  shift
  set +e
  "$@" 2>&1 | python3 scripts/gateway_evidence_filter.py | tee "artifacts/gateway/$name.txt"
  local statuses=("${PIPESTATUS[@]}")
  set -e
  printf '%s\n' "${statuses[0]}" > "artifacts/gateway/$name-exit-code.txt"
  test "${statuses[0]}" = 0 && test "${statuses[1]}" = 0 && test "${statuses[2]}" = 0
}
contracts() {
  docker build -f deploy/QueryGateway.Dockerfile -t sicp-gateway:contracts deploy
  record contracts python3 scripts/gateway_container_checks.py
}
deployment() { record deployment python3 scripts/gateway_deployment_checks.py setup; }
replicas() {
  export SICP_BASE_URL=http://sicp-gateway.test:28080
  record replicas python3 scripts/check-query-replicas.py
}
browser() {
  # Trust Chromium's test CA without disabling certificate verification.
  mkdir -p "$HOME/.pki/nssdb"
  certutil -N -d "sql:$HOME/.pki/nssdb" --empty-password
  certutil -A -d "sql:$HOME/.pki/nssdb" -n sicp-gateway-test -t 'C,,' -i "$RUNNER_TEMP/sicp-gateway-integration-certs/ca.pem"
  (
    cd frontend
    SICP_WORKER_TARGET=http://127.0.0.1:28080 npm run dev -- --host sicp-gateway.test > /dev/null 2>&1
  ) &
  vite_pid=$!
  trap 'kill "$vite_pid" 2>/dev/null || true' EXIT
  for ((attempt=0; attempt<30; attempt++)); do
    if curl --noproxy '*' --fail --silent http://sicp-gateway.test:5173/console/ > /dev/null; then break; fi
    sleep 1
  done
  record browser bash -c 'cd frontend && npx playwright test --config playwright.gateway.config.ts'
}
rollback() { record rollback python3 scripts/gateway_rollback_check.py; }
case "${1:-all}" in
  contracts|deployment|replicas|browser|rollback) "$1" ;;
  all) contracts; deployment; replicas; browser; rollback; python3 scripts/gateway_acceptance.py ;;
  *) printf '%s\n' 'Unknown gateway verification phase' >&2; exit 2 ;;
esac
