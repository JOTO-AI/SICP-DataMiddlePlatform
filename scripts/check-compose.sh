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
passwords = {"POSTGRES_PASSWORD", "QUERY_PASSWORD", "WORKER_PASSWORD", "LOGGER_PASSWORD"}
lines = path.read_text(encoding="utf-8").replace("QUERY_REPLICAS=1", "QUERY_REPLICAS=2").splitlines()
lines = [f"{key}='compose-{key}-@:/?#%$ space'" if (key := line.split("=", 1)[0]) in passwords else line for line in lines]
path.write_text("\n".join(lines) + "\n", encoding="utf-8")
PY
docker compose config --quiet
docker compose up -d --build --wait --wait-timeout 300

console="http://127.0.0.1:${SICP_CONSOLE_PORT:-18082}"
mkdir -p artifacts/compose
cookies="artifacts/compose/session.cookies"
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
curl --fail --silent --show-error "$console/console/" > /dev/null
login
curl --fail --silent --show-error -b "$cookies" --retry 30 --retry-delay 2 "$console/console-api/query/readyz"
curl --fail --silent --show-error -b "$cookies" --retry 30 --retry-delay 2 "$console/console-api/worker/readyz"
rm -f "$cookies"
