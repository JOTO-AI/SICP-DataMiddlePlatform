#!/usr/bin/env bash
set -euo pipefail
cd "$(dirname "${BASH_SOURCE[0]}")/.."
expected="go$(awk '$1 == "go" { print $2 }' go.mod)"
test "$(go env GOVERSION)" = "$expected"
test -z "$(gofmt -l cmd internal tests/integration)"
for script in scripts/sicp.sh scripts/check-compose.sh scripts/check-build-performance.sh deploy/init-postgres.sh deploy/worker-entrypoint.sh; do
  bash -n "$script"
done
python3 -c 'import ast; from pathlib import Path; [ast.parse(Path(p).read_text()) for p in ("deploy/mock.py", "scripts/check-compose-config.py", "scripts/build-runner-spec.py")]'
go vet ./...
python3 -m unittest discover -s scripts -p 'test_build_acceptance.py'
mkdir -p artifacts
go test -race -count=1 -coverprofile=artifacts/coverage.out -json ./... | tee artifacts/unit-tests.jsonl
CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -trimpath -o artifacts/query-service ./cmd/query-service
CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -trimpath -o artifacts/sync-worker ./cmd/sync-worker
CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -trimpath -o artifacts/db-migrate ./cmd/db-migrate
