#!/usr/bin/env bash
# Run only in an isolated Compose project; CI owns teardown and log collection.
set -euo pipefail
cd "$(dirname "${BASH_SOURCE[0]}")/.."
: "${SICP_PROJECT:?Supply an isolated Compose project name}"
test ! -e .env
python3 scripts/check-compose-config.py --write-development-env .env
# Verify one password edit reaches PostgreSQL bootstrap, migrations and both
# services, including characters that cannot be pasted unescaped into a URL.
python3 - <<'PY'
from pathlib import Path
path = Path(".env")
passwords = {"POSTGRES_PASSWORD", "QUERY_PASSWORD", "WORKER_PASSWORD", "LOGGER_PASSWORD", "UPSTREAM_LOGGER_PASSWORD"}
lines = path.read_text(encoding="utf-8").replace("QUERY_REPLICAS=1", "QUERY_REPLICAS=2").splitlines()
lines = [f"{key}='compose-{key}-@:/?#%$ space'" if (key := line.split("=", 1)[0]) in passwords else line for line in lines]
path.write_text("\n".join(lines) + "\n", encoding="utf-8")
PY
docker compose config --quiet
bash scripts/sicp.sh build
bash scripts/sicp.sh infra
bash scripts/sicp.sh migrate
bash scripts/sicp.sh log-cutover
bash scripts/sicp.sh start

check_database_limits() {
  for spec in db-m:256 db-s:128 db-log:100; do
    service=${spec%%:*}
    expected=${spec#*:}
    actual=$(docker compose exec -T "$service" psql -U postgres -d postgres -Atc 'SHOW max_connections')
    test "$actual" = "$expected"
    printf '%s max_connections=%s verified\n' "$service" "$actual"
  done
}
check_database_limits

console=$(python3 scripts/gateway.py origin)
mkdir -p artifacts/compose
cookies=$(mktemp)
trap 'rm -f "$cookies"' EXIT
login() {
  curl --fail --silent --show-error -c "$cookies" -H 'Content-Type: application/json' -H 'X-SICP-Request: console' \
    -d '{"username":"synthetic-admin","password":"synthetic-admin-password"}' "$console/console-api/login" > /dev/null
}
curl --fail --silent --show-error "$console/console/" > /dev/null
login
curl --fail --silent --show-error -b "$cookies" "$console/console-api/query/livez"
# A fresh deployment imports source data automatically, without a management request.
for ((attempt=0; attempt<120; attempt++)); do
  if curl --fail --silent -b "$cookies" "$console/console-api/query/readyz" > /dev/null; then
    break
  fi
  sleep 2
done
curl --fail --silent --show-error -b "$cookies" "$console/console-api/query/readyz"

# Exercise the real console proxy, session/CSRF, worker and PostgreSQL together.
python3 scripts/check-table-update.py "$console"

python3 scripts/check-query-replicas.py

# Exercise repeated up and a full container recreation with retained volumes.
docker compose up -d --wait --wait-timeout 300
docker compose down
docker compose up -d --wait --wait-timeout 300
check_database_limits
curl --fail --silent --show-error "$console/console/" > /dev/null
login
curl --fail --silent --show-error -b "$cookies" --retry 30 --retry-delay 2 "$console/console-api/query/readyz"
curl --fail --silent --show-error -b "$cookies" --retry 30 --retry-delay 2 "$console/console-api/worker/readyz"
rm -f "$cookies"
