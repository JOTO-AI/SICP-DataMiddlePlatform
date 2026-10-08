"""Validate core capacity evidence. This is deliberately not full BP01-BP10 acceptance."""
import argparse
from datetime import datetime
import json
from pathlib import Path
from statistics import median

SCENARIOS = ("BP01", "BP02", "BP03", "BP04", "BP05")
LIMIT_NS = 1_800_000_000_000


def summarize(root, sha):
    errors, samples, shapes = [], {}, set()
    for runner_path in sorted(Path(root).rglob("runner.json")):
        label = runner_path.parent.name
        try:
            runner = json.loads(runner_path.read_text())
            if runner["commit"] != sha or runner["profile_enabled"]:
                raise ValueError("wrong SHA or profiler enabled")
            fingerprint = runner["environment_fingerprint"]
            if not isinstance(fingerprint, str) or len(fingerprint) != 64:
                raise ValueError("missing runner fingerprint")
            shapes.add(fingerprint)
            if (runner_path.parent / "exit-code.txt").read_text().strip() != "0":
                raise ValueError("failed or timed-out test process")
            reports = list(runner_path.parent.rglob("stages.json"))
            if len(reports) != 1:
                raise ValueError("missing or ambiguous stage report")
            report = json.loads(reports[0].read_text())
            scenario, attempt = report["Scenario"], report["Attempt"]
            if scenario not in SCENARIOS or type(attempt) is not int or attempt not in (1, 2, 3):
                raise ValueError("unsupported scenario or attempt")
            if report["DiagnosticOnly"] or report["Scale"] != "capacity" or report["Seed"] != 73 or not report["Stages"]:
                raise ValueError("diagnostic, non-capacity or missing stage evidence")
            if any(s["State"] != "usable" for s in report["Scenarios"]):
                raise ValueError("setup or measurement failed")
            measured = [s for s in report["Scenarios"] if not s["Setup"]]
            if len(measured) != 1:
                raise ValueError("exactly one complete end-to-end sample required")
            sample = measured[0]
            ns = sample["BuildToUsableNS"]
            if type(ns) is not int or not 0 < ns < LIMIT_NS:
                raise ValueError("T must be positive and strictly below 1800 seconds")
            timing = sample["Timing"]
            start = datetime.fromisoformat(timing["build_started_at"].replace("Z", "+00:00"))
            end = datetime.fromisoformat(timing["usable_at"].replace("Z", "+00:00"))
            if start.tzinfo is None or end.tzinfo is None or not timing["bundle_id"] or abs((end-start).total_seconds()*1e9-ns) > 2000:
                raise ValueError("missing or inconsistent persisted timing boundary")
            expected = sample["Expected"]
            if len(expected) != 14 or len({t["table"] for t in expected}) != 14 or sum(t["rows"] for t in expected) != 2592390:
                raise ValueError("incomplete fourteen-table capacity")
            if not 480000000 <= sum(t["bytes"] for t in expected) <= 600000000 or any(len(t["sha256"]) != 64 for t in expected):
                raise ValueError("fixture width or digest missing")
            expected_mode = {"BP01": "initialize", "BP02": "incremental", "BP03": "incremental", "BP04": "incremental", "BP05": "force_full"}[scenario]
            if sample["Name"] != expected_mode:
                raise ValueError("wrong execution mode")
            stages = [event for event in report["Stages"] if event.get("phase") == "finished" and
                      event.get("identity", {}).get("task_id") == "b01-" + expected_mode]
            if any(event["metadata"]["build"]["metrics"].get("invalid") for event in stages):
                raise ValueError("invalid resource counters")
            successful = [event for event in stages if event.get("outcome") == "succeeded"]
            completed = {event["metadata"]["build"]["stage"] for event in successful}
            if not {"sealing", "prepare_target", "usable_probe"} <= completed:
                raise ValueError("missing full-path stage evidence")
            copies = {event["identity"].get("instance") for event in successful if
                      event["metadata"]["build"]["stage"] == "validate_copy" and
                      event["metadata"]["build"].get("combination") == timing["bundle_id"]}
            if copies != {"M", "S"}:
                raise ValueError("both independently validated copies required")
            for table in expected:
                changes = {"BP01": 0, "BP02": 1, "BP03": max(1, (table["rows"]+99)//100), "BP04": table["rows"], "BP05": table["rows"]}[scenario]
                if table["changed"] != changes:
                    raise ValueError("wrong per-table change distribution")
            key = (scenario, attempt)
            if key in samples:
                raise ValueError("duplicate sample")
            samples[key] = ns / 1e9
        except (OSError, ValueError, KeyError, TypeError, AttributeError) as error:
            errors.append(f"{label}: {error}")
    if len(shapes) != 1:
        errors.append("one consistent runner specification is required")
    stats = {}
    for scenario in SCENARIOS:
        values = [samples[(scenario, n)] for n in (1, 2, 3) if (scenario, n) in samples]
        if len(values) != 3:
            errors.append(f"{scenario}: three independent successful attempts required")
        stats[scenario] = {"seconds": values, "min": min(values) if values else None,
                           "median": median(values) if values else None, "max": max(values) if values else None}
    return {"sha": sha, "core_passed": not errors, "full_acceptance_passed": False,
            "remaining_gates": ["all BT/BI correctness", "BP06-BP10", "resource budgets", "online latency", "before/after baseline"],
            "scenarios": stats, "errors": errors}


if __name__ == "__main__":
    parser = argparse.ArgumentParser()
    parser.add_argument("root")
    parser.add_argument("--sha", required=True)
    parser.add_argument("--output", required=True)
    args = parser.parse_args()
    result = summarize(args.root, args.sha)
    output = Path(args.output)
    output.parent.mkdir(parents=True, exist_ok=True)
    output.write_text(json.dumps(result, ensure_ascii=False, indent=2) + "\n")
    raise SystemExit(0 if result["core_passed"] else 1)
