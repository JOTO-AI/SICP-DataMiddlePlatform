import json
from pathlib import Path
import re
import unittest

from phase2_acceptance import MANIFEST, ROOT, verify_events


def event(action, name=None):
    value = dict(Action=action)
    if name:
        value["Test"] = name
    return json.dumps(value)


class AcceptanceEvidence(unittest.TestCase):
    def test_T25_06_missing_skipped_failed_or_unfinished_never_pass(self):
        name = "TestI07_Synthetic"
        good = [event("run", name), event("pass", name), event("pass")]
        self.assertEqual(verify_events(good, [name]), [name])
        for lines in ([], [event("pass")], good[:-1], good[1:],
                      [event("run",name),event("skip",name),event("pass")],
                      good + [event("fail")], good + [event("skip",name+"/subcase")],
                      ["not-json"]):
            with self.subTest(lines=lines):
                with self.assertRaises(ValueError): verify_events(lines,[name])

    def test_T25_06_every_manifest_case_exists_and_ci_runs_all_groups(self):
        manifest = json.loads(MANIFEST.read_text())
        self.assertEqual(set(manifest["groups"]), {"I07","I08","I09","I10","I11"})
        self.assertTrue(manifest["manual_evidence_required"])
        source = "\n".join(path.read_text(encoding="utf-8") for path in (ROOT/"tests/integration").glob("*_test.go"))
        cases = re.findall(r"func (Test\w+)\(t \*testing.T\)", source)
        for names in manifest["groups"].values():
            self.assertTrue(names)
            self.assertEqual(len(names),len(set(names)))
            for name in names:
                self.assertEqual(cases.count(name),1,name)
        workflow = (ROOT/".github/workflows/ci.yml").read_text()
        self.assertIn("for group in I07 I08 I09 I10 I11",workflow)
        self.assertIn('python3 scripts/phase2_acceptance.py "$group"',workflow)


if __name__ == "__main__":
    unittest.main()
