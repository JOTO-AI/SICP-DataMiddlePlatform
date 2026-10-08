import json
from pathlib import Path
import tempfile
import unittest

from build_acceptance import summarize, SCENARIOS, LIMIT_NS


class EvidenceTests(unittest.TestCase):
    def fixture(self, root):
        for scenario in SCENARIOS:
            for attempt in (1, 2, 3):
                path = root / f"{scenario}-{attempt}"
                path.mkdir()
                (path / "runner.json").write_text(json.dumps({"commit": "sha", "profile_enabled": False, "environment_fingerprint": "a"*64}))
                (path / "exit-code.txt").write_text("0")
                tables = [{"table": str(i), "rows": 1 if i else 2592377, "bytes": 35000000, "sha256": "a"*64} for i in range(14)]
                for table in tables:
                    table["changed"] = {"BP01": 0, "BP02": 1, "BP03": max(1, (table["rows"]+99)//100), "BP04": table["rows"], "BP05": table["rows"]}[scenario]
                sample = {"State": "usable", "Setup": False, "Name": "initialize" if scenario == "BP01" else "force_full" if scenario == "BP05" else "incremental",
                          "BuildToUsableNS": 1_000_000_000, "Expected": tables,
                          "Timing": {"build_started_at": "2026-10-08T00:00:00Z", "usable_at": "2026-10-08T00:00:01Z", "bundle_id": "bundle"}}
                stages = [{"phase": "finished", "outcome": "succeeded", "identity": {"task_id": "b01-"+sample["Name"], "instance": instance},
                           "metadata": {"build": {"stage": stage, "combination": "bundle", "metrics": {}}}}
                          for stage, instance in [("sealing", ""), ("prepare_target", ""), ("usable_probe", ""), ("validate_copy", "M"), ("validate_copy", "S")]]
                (path / "stages.json").write_text(json.dumps({"Scenario": scenario, "Attempt": attempt, "DiagnosticOnly": False, "Scale": "capacity", "Seed": 73, "Stages": stages, "Scenarios": [sample]}))

    def test_complete_core_is_not_full_acceptance(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            self.fixture(root)
            report = summarize(root, "sha")
            self.assertTrue(report["core_passed"], report["errors"])
            self.assertFalse(report["full_acceptance_passed"])

    def test_missing_failed_profiled_mixed_and_boundary_samples_rejected(self):
        for mode in ("missing", "failure", "profile", "sha", "shape", "boundary", "diagnostic", "distribution", "copy"):
            with self.subTest(mode=mode), tempfile.TemporaryDirectory() as directory:
                root = Path(directory)
                self.fixture(root)
                path = root / "BP03-2"
                if mode == "missing":
                    (path / "stages.json").unlink()
                elif mode == "failure":
                    (path / "exit-code.txt").write_text("1")
                elif mode in ("profile", "sha", "shape"):
                    file = path / "runner.json"
                    data = json.loads(file.read_text())
                    data[{"profile": "profile_enabled", "sha": "commit", "shape": "environment_fingerprint"}[mode]] = True if mode == "profile" else "b"*64
                    file.write_text(json.dumps(data))
                else:
                    file = path / "stages.json"
                    data = json.loads(file.read_text())
                    if mode == "boundary": data["Scenarios"][0]["BuildToUsableNS"] = LIMIT_NS
                    if mode == "diagnostic": data["DiagnosticOnly"] = True
                    if mode == "distribution": data["Scenarios"][0]["Expected"][0]["changed"] = 0
                    if mode == "copy": data["Stages"].pop()
                    file.write_text(json.dumps(data))
                self.assertFalse(summarize(root, "sha")["core_passed"])


if __name__ == "__main__":
    unittest.main()
