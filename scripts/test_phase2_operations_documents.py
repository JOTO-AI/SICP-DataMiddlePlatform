"""P35 document contracts, discovered by the unchanged check.sh entry point.

Import the evidence-runner cases here so the existing test_phase2_*.py discovery
also includes T35-01--03. Tests use only repository files and synthetic fixtures.
"""
import json
from pathlib import Path
import re
import unittest

from test_operations_acceptance import AcceptanceTests  # noqa: F401
from operations_acceptance import REQUIRED

ROOT = Path(__file__).resolve().parents[1]


class OperationsDocumentTests(unittest.TestCase):
    def test_T35_04_examples_are_unique_and_document_links_resolve(self):
        examples = json.loads((ROOT / "development_docs/verification/operations_http_examples.json").read_text(encoding="utf-8"))
        names = [e["name"] for e in examples["examples"]]
        self.assertEqual(len(names), len(set(names)))
        for item in examples["examples"]:
            self.assertIn(item["response_scope"], {"full", "selected_fields", "selected_items", "selected_items_and_fields", "sse_event_data"})
            self.assertNotIn("Authorization", item["request_headers"])
            if item["request"] and "password" in item["request"]:
                self.assertEqual(item["request"]["password"], "synthetic-password-only")
        for file in ["OPERATIONS_API_EXAMPLES.md", "OPERATIONS_UPGRADE_RUNBOOK.md", "PHASE2_OPERATIONS_BACKEND_HANDOFF.md"]:
            path = ROOT / "development_docs" / file
            content = path.read_text(encoding="utf-8")
            for target in re.findall(r"\]\(([^)]+)\)", content):
                if not target.startswith(("https:", "http:", "#")):
                    self.assertTrue((path.parent / target.split("#")[0]).is_file(), target)
            for block in re.findall(r"```json\s*\n(.*?)\n```", content, re.S):
                json.loads(block)

    def test_T35_05_upgrade_rejection_evidence_maps_to_real_cases(self):
        # Fail if a renamed/deleted regression silently breaks the runbook's
        # rejection evidence; these cases execute in their own unit groups.
        cases = {
            "internal/config/documentation_test.go": ["TestT35_05UpgradeCommandsVersionsAndConfigurationGates"],
            "internal/storage/migrate/migrate_test.go": ["TestT02_06MigrationReplayConflictAndRollback"],
            "internal/storage/migrate/commit_order_test.go": ["TestT29_04BackfillAndNoLegacyWriterBypass"],
            "internal/observability/cutover_test.go": ["TestT28_13CutoverPreservesOriginalTargetUntilAcknowledged", "TestT28_13UnmarkedOrOrphanedJournalCannotCompleteSwitch"],
        }
        runbook = (ROOT / "development_docs/OPERATIONS_UPGRADE_RUNBOOK.md").read_text(encoding="utf-8")
        for path, names in cases.items():
            content = (ROOT / path).read_text(encoding="utf-8")
            for name in names:
                self.assertIn(name, runbook)
                self.assertRegex(content, r"func " + re.escape(name) + r"\(t \*testing.T\)")
        template = (ROOT / ".env.example").read_text(encoding="utf-8")
        keys = set(re.findall(r"(?m)^([A-Z][A-Z0-9_]+)=", template))
        self.assertTrue({"ADMIN_USERNAME", "ADMIN_PASSWORD", "DATABASE_LOG_URL", "DATABASE_UPSTREAM_LOG_URL", "SNAPSHOT_KEY", "AUDIT_KEY"} <= keys)
        self.assertFalse({"LOG_CURSOR_KEYS", "LOG_CURSOR_ACTIVE", "LOG_INTERNAL_TOKEN", "DATABASE_LOG_READ_URL", "DATABASE_UPSTREAM_LOG_READ_URL", "DATABASE_LOG_MAINTENANCE_URL", "DATABASE_UPSTREAM_LOG_MAINTENANCE_URL"} & keys)

    def test_T35_06_paused_evidence_and_frontend_are_not_marked_passed(self):
        plan = (ROOT / "development_docs/PHASE2_OPERATIONS_DEVELOPMENT_PLAN.md").read_text(encoding="utf-8")
        report = (ROOT / "development_docs/PHASE2_OPERATIONS_ACCEPTANCE.md").read_text(encoding="utf-8")
        self.assertIn("模板，未验收", report)
        self.assertIn("状态：未执行", report)
        self.assertNotRegex(plan, r"(?m)^\s*- \[x\] (?:\*\*)?(?:T(?:27|28|29|3[0-5])|OI0[1-7])(?:[-：])")
        self.assertIn("前端重构", report)
        self.assertIn("不计成功", report)
        guide = (ROOT / "development_docs/OPERATIONS_API_EXAMPLES.md").read_text(encoding="utf-8")
        self.assertIn("前端重构延后", guide)
        self.assertIn("不代表整个 Phase 2 完成", guide)
        names = []
        for path in (ROOT / "tests/integration").glob("operations_*_test.go"):
            names += re.findall(r"(?m)^func (TestOI\d{2}_\d{2})\(", path.read_text(encoding="utf-8"))
        self.assertEqual(set(names), REQUIRED)
        self.assertEqual(len(names), len(REQUIRED))
        self.assertIn("operations_capacity_baseline.json", report)
