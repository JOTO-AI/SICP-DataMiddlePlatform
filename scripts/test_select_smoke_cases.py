import importlib.util
import json
from pathlib import Path
import tempfile
import unittest

spec = importlib.util.spec_from_file_location("selector", Path(__file__).with_name("select-smoke-cases.py"))
selector = importlib.util.module_from_spec(spec)
spec.loader.exec_module(selector)


class SelectorTests(unittest.TestCase):
    def test_nullable_rows_and_ambiguous_memberships(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            tables = root / "tables"
            tables.mkdir()
            data = {
                "dwd_bzkscjzlb": [{"XH": "S1"}, {"XH": "S2"}],
                "dwd_bzkspksjzlb": [
                    {"KKXND": None, "KKXQM": None, "JSJGH": None},
                    {"JXBH": "J", "JHKCH": "C", "KKXND": "2025-2026", "KKXQM": "1", "JSJGH": "T1"},
                    {"JXBH": "A", "JHKCH": "C", "KKXND": "2024-2025", "KKXQM": "1", "JSJGH": "T2"},
                    {"JXBH": "A", "JHKCH": "C", "KKXND": "2025-2026", "KKXQM": "1", "JSJGH": "T2"},
                ],
                "dwd_bzkskbsjzlb": [
                    {"XH": "S0", "JXBH": "A", "JHKCH": "C"},
                    {"XH": "S1", "JXBH": "J", "JHKCH": None},
                    {"XH": "S1", "JXBH": "J", "JHKCH": "C"},
                ],
            }
            for table, rows in data.items():
                (tables / (table + ".json")).write_text(json.dumps(rows), encoding="utf-8")
            cases = selector.select(root)
            self.assertEqual([len(v) for v in cases.values()], [4, 3, 1, 2])
            self.assertEqual(cases["students.timetable.query"][0]["student_number"], "S1")
            self.assertEqual(cases, selector.select(root))
            data["dwd_bzkskbsjzlb"] = []
            (tables / "dwd_bzkskbsjzlb.json").write_text("[]", encoding="utf-8")
            with self.assertRaises(ValueError):
                selector.select(root)

    def test_full_response_reader(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            table = root / "responses" / "table"
            table.mkdir(parents=True)
            (table / "page00001.json").write_text(json.dumps({"data": {"list": [{"ID": "synthetic"}]}}))
            self.assertEqual(list(selector.rows(root, "table")), [{"ID": "synthetic"}])
