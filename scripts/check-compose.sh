#!/usr/bin/env bash
# Run only in an isolated Compose project; CI owns teardown and log collection.
set -euo pipefail
cd "$(dirname "${BASH_SOURCE[0]}")/.."
: "${SICP_PROJECT:?Supply an isolated Compose project name}"
test ! -e .env
bash scripts/sicp.sh prepare
# Verify one password edit reaches PostgreSQL bootstrap, migrations and both
# services, including characters that cannot be pasted unescaped into a URL.
python3 - <<'PY'
from pathlib import Path
path = Path(".env")
passwords = {"POSTGRES_PASSWORD", "QUERY_PASSWORD", "WORKER_PASSWORD", "LOGGER_PASSWORD"}
lines = path.read_text(encoding="utf-8").splitlines()
lines = [f"{key}='compose-{key}-@:/?#%$ space'" if (key := line.split("=", 1)[0]) in passwords else line for line in lines]
path.write_text("\n".join(lines) + "\n", encoding="utf-8")
PY
docker compose config --quiet
docker compose up -d --build --wait --wait-timeout 300

console="http://127.0.0.1:${SICP_CONSOLE_PORT:-18082}"
curl --fail --silent --show-error "$console/console/" > /dev/null
curl --fail --silent --show-error "$console/gateway/query/livez"
curl --fail --silent --show-error --retry 30 --retry-delay 2 "$console/gateway/worker/readyz"
# A fresh deployment must not silently import source data.
test "$(curl --silent --output /dev/null --write-out '%{http_code}' "$console/gateway/query/readyz")" = 503
docker compose run --rm --no-deps ctl initialize compose-smoke-initialization
for ((attempt=0; attempt<120; attempt++)); do
  if curl --fail --silent "$console/gateway/query/readyz" > /dev/null; then
    break
  fi
  sleep 2
done
curl --fail --silent --show-error "$console/gateway/query/readyz"

# Exercise repeated up and a full container recreation with retained volumes.
docker compose up -d --wait --wait-timeout 300
docker compose down
docker compose up -d --wait --wait-timeout 300
curl --fail --silent --show-error "$console/console/" > /dev/null
curl --fail --silent --show-error --retry 30 --retry-delay 2 "$console/gateway/query/readyz"
curl --fail --silent --show-error --retry 30 --retry-delay 2 "$console/gateway/worker/readyz"
