"""Strict P35 evidence runner. Execution is restricted to Linux GitHub Actions.

This tool intentionally fails while any of the 32 required OI cases is absent.
It neither supplies placeholder tests nor counts compilation as acceptance.
"""
from __future__ import annotations

import argparse
import hashlib
import json
import os
from pathlib import Path
import platform
import re
import subprocess
import sys
import time

COUNTS = {1: 4, 2: 4, 3: 5, 4: 4, 5: 5, 6: 5, 7: 5}
REQUIRED = {f"TestOI{group:02}_{case:02}" for group, count in COUNTS.items() for case in range(1, count + 1)}
PATTERN = re.compile(r"^(TestOI\d{2}_\d{2})(?:[A-Za-z_].*)?$")


def check_prerequisites(value: dict, sha: str, directory: Path) -> dict[str, str]:
    if value.get("sha") != sha or value.get("backend_P27_P35") != "complete" or value.get("T27_T35") != "passed":
        raise ValueError("same-SHA completed backend and passing unit evidence required")
    artifacts = value.get("artifacts")
    required = {f"T{n}" for n in range(27, 36)}
    if not isinstance(artifacts, dict) or set(artifacts) != required:
        raise ValueError("unit evidence artifacts are incomplete")
    checked = {}
    for name, item in artifacts.items():
        if not isinstance(item, dict) or not isinstance(item.get("path"), str):
            raise ValueError("invalid unit evidence artifact")
        path = (directory / item["path"]).resolve()
        # Evidence is a local package, not an arbitrary filesystem manifest.
        if not path.is_relative_to(directory.resolve()) or not path.is_file() or path.stat().st_size == 0:
            raise ValueError("missing unit evidence artifact")
        digest = hashlib.sha256(path.read_bytes()).hexdigest()
        if digest != item.get("sha256"):
            raise ValueError("unit evidence digest mismatch")
        checked[name] = digest
    return checked


def check_capacity(value: dict) -> None:
    if not isinstance(value.get("machine"), dict) or not value["machine"]:
        raise ValueError("capacity machine description is missing")
    fields = ("records_per_source", "event_bytes", "write_rate", "history_clients", "sse_clients", "retention_days", "p95_ms", "max_memory_bytes", "max_backlog")
    for key in fields:
        number = value.get(key)
        if isinstance(number, bool) or not isinstance(number, (int, float)) or not 0 < number < float("inf"):
            raise ValueError("capacity baseline contains invalid bounds")


def check_discovery(names: list[str]) -> dict[str, str]:
    found = {}
    for name in names:
        match = PATTERN.fullmatch(name)
        if not match:
            if name.startswith("TestOI"):
                raise ValueError("malformed OI case number")
            continue
        key = match.group(1)
        if key not in REQUIRED or key in found:
            raise ValueError("unexpected or duplicate OI case")
        found[key] = name
    if set(found) != REQUIRED:
        raise ValueError("missing OI cases: " + ", ".join(sorted(REQUIRED - set(found))))
    return found


def check_events(lines: list[str], names: dict[str, str], exit_code: int) -> dict:
    started, passed = set(), set()
    package_pass = False
    failures, skipped = [], []
    for line in lines:
        try:
            event = json.loads(line)
        except (ValueError, TypeError):
            raise ValueError("malformed Go JSON evidence") from None
        action, name = event.get("Action"), event.get("Test")
        if action == "fail":
            failures.append(name or "package")
        if action == "skip":
            skipped.append(name or "package")
        if name in names.values():
            if action == "run":
                if name in started:
                    raise ValueError("duplicate test execution")
                started.add(name)
            elif action == "pass":
                if name not in started or name in passed:
                    raise ValueError("out-of-order or duplicate test result")
                passed.add(name)
        if not name and action == "pass":
            package_pass = True
    expected = set(names.values())
    if exit_code != 0 or failures or skipped or not package_pass or started != expected or passed != expected:
        raise ValueError("failed, skipped, missing, or incomplete OI execution")
    return {"expected": len(expected), "executed": len(started), "passed": len(passed), "failed": 0, "skipped": 0}


def main() -> int:
    parser = argparse.ArgumentParser()
    parser.add_argument("--prerequisites", required=True, type=Path)
    parser.add_argument("--capacity", required=True, type=Path)
    parser.add_argument("--output", required=True, type=Path)
    parser.add_argument("--seed", required=True, type=int)
    args = parser.parse_args()
    if platform.system() != "Linux" or os.environ.get("GITHUB_ACTIONS") != "true":
        raise ValueError("execution requires Linux GitHub Actions; local tests remain paused")
    root = Path(__file__).resolve().parents[1]
    sha = subprocess.check_output(["git", "rev-parse", "HEAD"], cwd=root, text=True).strip()
    prerequisites = json.loads(args.prerequisites.read_text(encoding="utf-8"))
    unit_artifacts = check_prerequisites(prerequisites, sha, args.prerequisites.parent)
    capacity_raw = args.capacity.read_bytes()
    capacity = json.loads(capacity_raw)
    check_capacity(capacity)
    output = args.output.resolve()
    if output.exists() and any(output.iterdir()):
        raise ValueError("output must be a new or empty directory")
    output.mkdir(parents=True, exist_ok=True)
    (output / "capacity.json").write_bytes(capacity_raw)
    report = {"sha": sha, "seed": args.seed, "status": "failed", "unit_artifacts": unit_artifacts, "capacity_sha256": hashlib.sha256(capacity_raw).hexdigest(), "started_at": time.strftime("%Y-%m-%dT%H:%M:%SZ", time.gmtime())}
    try:
        discovery = subprocess.run(["go", "test", "-tags=integration", "-list", "^TestOI", "./tests/integration"], cwd=root, capture_output=True, text=True, timeout=180, check=False)
        (output / "discovery.txt").write_text(discovery.stdout, encoding="utf-8")
        if discovery.returncode:
            raise ValueError("test discovery failed")
        names = check_discovery(discovery.stdout.splitlines())
        command = ["go", "test", "-json", "-tags=integration", "-count=1", "-timeout=20m", "-run", "^TestOI(01|02|03|04|05|06|07)_", "./tests/integration"]
        report["command"] = command
        env = dict(os.environ, SICP_OI_SEED=str(args.seed), SICP_OI_CAPACITY=str(args.capacity.resolve()), SICP_OI_OUTPUT=str(output))
        with (output / "tests.jsonl").open("w", encoding="utf-8") as log:
            result = subprocess.run(command, cwd=root, env=env, stdout=log, stderr=subprocess.DEVNULL, text=True, timeout=1260, check=False)
        report["exit_code"] = result.returncode
        report["counts"] = check_events((output / "tests.jsonl").read_text(encoding="utf-8").splitlines(), names, result.returncode)
        report["status"] = "passed"
    except (ValueError, OSError, subprocess.TimeoutExpired) as exc:
        report["reason"] = "timeout" if isinstance(exc, subprocess.TimeoutExpired) else str(exc)
    finally:
        report["artifacts"] = {p.name: hashlib.sha256(p.read_bytes()).hexdigest() for p in output.iterdir() if p.is_file() and p.name != "report.json"}
        report["finished_at"] = time.strftime("%Y-%m-%dT%H:%M:%SZ", time.gmtime())
        (output / "report.json").write_text(json.dumps(report, indent=2) + "\n", encoding="utf-8")
    return 0 if report["status"] == "passed" else 1


if __name__ == "__main__":
    try:
        sys.exit(main())
    except (ValueError, OSError, subprocess.SubprocessError):
        print("operations acceptance prerequisites or execution unavailable", file=sys.stderr)
        sys.exit(1)
