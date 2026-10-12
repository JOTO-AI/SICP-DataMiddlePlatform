"""Linux acceptance runner: no matching tests, skips and missing evidence fail closed."""
import argparse
import json
import os
from pathlib import Path
import re
import subprocess
import sys

ROOT = Path(__file__).resolve().parents[1]
MANIFEST = ROOT / "development_docs/examples/phase2_acceptance.json"


def verify_events(lines, expected):
    started, passed = set(), set()
    package_pass = False
    for line in lines:
        event = json.loads(line)
        action, name = event.get("Action"), event.get("Test")
        if action in ("fail", "skip"):
            raise ValueError("failed or skipped test/package")
        if action == "run" and name:
            started.add(name)
        if action == "pass":
            if name:
                passed.add(name)
            else:
                package_pass = True
    if not package_pass or not set(expected).issubset(started & passed):
        raise ValueError("missing started/passed acceptance cases")
    return sorted(set(expected))


def run(group):
    manifest = json.loads(MANIFEST.read_text(encoding="utf-8"))
    expected = manifest["groups"][group]
    if (sys.platform != "linux" or os.environ.get("SICP_INTEGRATION") != "1" or
            os.environ.get("GITHUB_ACTIONS") != "true"):
        raise ValueError("isolated GitHub Actions Linux environment required")
    commit = subprocess.check_output(["git", "rev-parse", "HEAD"], cwd=ROOT, text=True).strip()
    if not re.fullmatch(r"[0-9a-f]{40}", commit):
        raise ValueError("cannot bind evidence to a commit")
    artifact = ROOT / "artifacts/integration"
    artifact.mkdir(parents=True, exist_ok=True)
    # I11 measures normal executable cost; lifecycle races run in unit/I07-I10.
    command = ["go", "test", "-tags=integration", "-count=1", "-parallel=1", "-timeout=15m", "-json"]
    if group != "I11":
        command.append("-race")
    command += ["-run", "^(" + "|".join(re.escape(name) for name in expected) + ")$", "./tests/integration"]
    report = {"schema":"phase2-execution-v1", "group":group,
              "commit":commit, "expected":expected,
              "status":"incomplete", "scope":"automated cases only; manual evidence still required"}
    result = artifact / f"{group}-execution.json"
    # Clear a previous pass before starting. Preserve JSONL plus stderr for
    # failed compilation and interrupted runs without interpreting either as pass.
    result.write_text(json.dumps(report, indent=2) + "\n", encoding="utf-8")
    with (artifact / f"{group}.jsonl").open("w", encoding="utf-8") as out, (artifact / f"{group}-stderr.txt").open("w", encoding="utf-8") as errors:
        completed = subprocess.run(command, cwd=ROOT, stdout=out, stderr=errors, timeout=930, check=False)
    report["exit_code"] = completed.returncode
    report["status"] = "failed"
    try:
        if completed.returncode != 0:
            raise ValueError("Go acceptance process failed")
        with (artifact / f"{group}.jsonl").open(encoding="utf-8") as stream:
            report["executed"] = verify_events(stream, expected)
        report["status"] = "automated_pass"
    finally:
        result.write_text(json.dumps(report, indent=2) + "\n", encoding="utf-8")


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("group", choices=("I07", "I08", "I09", "I10", "I11"))
    args = parser.parse_args()
    try:
        run(args.group)
    except (ValueError, OSError, subprocess.SubprocessError):
        print("Acceptance incomplete or failed; inspect artifacts/integration.", file=sys.stderr)
        return 1
    print(f"{args.group} automated cases passed; manual evidence is separate.")
    return 0


if __name__ == "__main__":
    sys.exit(main())
