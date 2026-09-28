#!/usr/bin/env bash
# Run only in an isolated Compose project; CI owns teardown and log collection.
set -euo pipefail
cd "$(dirname "${BASH_SOURCE[0]}")/.."
: "${SICP_PROJECT:?Supply an isolated Compose project name}"
test ! -d deploy/runtime
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
