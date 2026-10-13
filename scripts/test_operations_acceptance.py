"""Synthetic evidence tests; no service, network, or email access."""
import json
import hashlib
from pathlib import Path
import tempfile
import unittest

import operations_acceptance as acceptance


class AcceptanceTests(unittest.TestCase):
    def test_T35_01_missing_and_duplicate_cases_fail(self):
        with self.assertRaises(ValueError):
            acceptance.check_discovery([])
        names = sorted(acceptance.REQUIRED)
        with self.assertRaises(ValueError):
            acceptance.check_discovery(names + [names[0] + "Duplicate"])
        self.assertEqual(len(acceptance.check_discovery(names)), 32)

    def events(self):
        events = []
        for name in sorted(acceptance.REQUIRED):
            events.extend([{"Action": "run", "Test": name}, {"Action": "pass", "Test": name}])
        events.append({"Action": "pass", "Package": "synthetic"})
        return events

    def test_T35_02_skip_failure_and_missing_execution_fail(self):
        names = acceptance.check_discovery(sorted(acceptance.REQUIRED))
        for extra in [{"Action": "skip", "Test": "sub/case"}, {"Action": "fail"}]:
            with self.assertRaises(ValueError):
                acceptance.check_events([json.dumps(e) for e in self.events() + [extra]], names, 0)
        with self.assertRaises(ValueError):
            acceptance.check_events([json.dumps(e) for e in self.events()[2:]], names, 0)

    def test_T35_03_success_requires_zero_exit_and_complete_evidence(self):
        names = acceptance.check_discovery(sorted(acceptance.REQUIRED))
        lines = [json.dumps(e) for e in self.events()]
        with self.assertRaises(ValueError):
            acceptance.check_events(lines, names, 1)
        self.assertEqual(acceptance.check_events(lines, names, 0)["passed"], 32)

    def test_T35_03_missing_or_changed_artifact_is_rejected(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            evidence = root / "units.jsonl"
            evidence.write_bytes(b'{"synthetic":"unit evidence"}\n')
            digest = hashlib.sha256(evidence.read_bytes()).hexdigest()
            value = {"sha": "synthetic-sha", "backend_P27_P35": "complete", "T27_T35": "passed", "artifacts": {f"T{n}": {"path": "units.jsonl", "sha256": digest} for n in range(27, 36)}}
            self.assertEqual(len(acceptance.check_prerequisites(value, "synthetic-sha", root)), 9)
            with self.assertRaises(ValueError):
                acceptance.check_prerequisites(value, "different-sha", root)
            evidence.write_bytes(b"changed")
            with self.assertRaises(ValueError):
                acceptance.check_prerequisites(value, "synthetic-sha", root)
            evidence.unlink()
            with self.assertRaises(ValueError):
                acceptance.check_prerequisites(value, "synthetic-sha", root)

    def test_T35_03_duplicate_or_out_of_order_result_is_rejected(self):
        names = acceptance.check_discovery(sorted(acceptance.REQUIRED))
        events = self.events()
        events[0], events[1] = events[1], events[0]
        with self.assertRaises(ValueError):
            acceptance.check_events([json.dumps(e) for e in events], names, 0)
