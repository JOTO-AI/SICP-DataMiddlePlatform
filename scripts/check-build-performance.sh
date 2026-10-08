#!/usr/bin/env bash
set -euo pipefail
cd "$(dirname "${BASH_SOURCE[0]}")/.."
test "${GITHUB_ACTIONS:-}" = true
test "${SICP_INTEGRATION:-}" = 1
test "${SICP_BUILD_PERFORMANCE:-}" = 1
test "${SICP_PROJECT:-}" = sicp-build-baseline
if [[ ${SICP_BUILD_ACCEPTANCE:-0} == 1 && ${SICP_BUILD_PROFILE:-0} == 1 ]]; then
  printf 'Profiler runs cannot be acceptance samples\n' >&2
  exit 1
fi
mkdir -p artifacts/build-performance
python3 scripts/build-runner-spec.py > artifacts/build-performance/runner.json

# Compile outside /usr/bin/time. This is a GitHub-only integration binary,
# using production acquisition, worker, PostgreSQL and query HTTP handlers.
go test -tags=integration -c -o artifacts/build-performance/integration.test ./tests/integration
sample_resources() {
  while true; do
    date -u +%FT%TZ
    docker stats --no-stream --format '{{json .}}' $(docker compose -p "$SICP_PROJECT" ps -q)
    df -B1 artifacts/build-performance
    sleep 5
  done
}
sample_resources > artifacts/build-performance/resources.log &
sampler=$!
trap 'kill "$sampler" 2>/dev/null || true; wait "$sampler" 2>/dev/null || true' EXIT
set +e
(
  cd tests/integration
  /usr/bin/time -v -o ../../artifacts/build-performance/process-resources.txt \
    ../../artifacts/build-performance/integration.test -test.run '^TestB01_Performance$' -test.v -test.timeout=110m
) 2>&1 | tee artifacts/build-performance/baseline.log
result=${PIPESTATUS[0]}
set -e
printf '%s\n' "$result" > artifacts/build-performance/exit-code.txt
# A completed diagnostic run still does not establish B10 acceptance.
python3 - <<'PY'
import json, os
from pathlib import Path
for p in sorted(Path('artifacts/build-performance').rglob('stages.json')):
    report = json.loads(p.read_text())
    lines = [f"Build measurements: {report.get('Scenario') or 'diagnostic baseline'}, attempt {report.get('Attempt', 1)} (not overall B10 acceptance)", '']
    for scenario in report['Scenarios']:
        duration = f"check={scenario.get('CheckedNS', 0)/1e9:.3f}s" if scenario['State'] == 'unchanged_verified' else f"T={scenario['BuildToUsableNS']/1e9:.3f}s"
        lines.append(f"- {scenario['Name']}: {scenario['State']}, {duration}, setup={scenario.get('Setup', False)}")
    with open(os.environ['GITHUB_STEP_SUMMARY'], 'a') as out:
        out.write('\n'.join(lines) + '\n')
PY
exit "$result"
