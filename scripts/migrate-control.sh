#!/usr/bin/env bash
# Run on the Linux Docker host, from the checkout used by this deployment.
set -euo pipefail
root="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/.." && pwd)"
exec python3 "$root/scripts/migrate_control.py" "$@"
