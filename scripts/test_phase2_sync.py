import copy
import json
from pathlib import Path
import tempfile
import unittest
from unittest.mock import patch
from io import BytesIO

import sync_teaching_classes as sync


class Fixture:
    def __init__(self, fault=None):
        self.calls = []
        self.fault = fault
        self.starts = 0

    def __call__(self, path, body, token):
        page = body["page"]
        self.calls.append((path, copy.deepcopy(body), token))
        if path == sync.CLASSES and page == 1:
            self.starts += 1
        identity = {k: v for k, v in body.items() if k not in ("page", "page_size")}
        if path == sync.CLASSES:
            all_items = [dict(teaching_class_number=f"J{i}", planned_course_code="C1", student_count=3) for i in range(3)]
            self_token = "classes-token"
        else:
            all_items = [dict(student_number=f"S{i}") for i in range(3)]
            self_token = "members-" + body["teaching_class_number"]
        if page == 1:
            assert token is None
        else:
            assert token == self_token
        size = body["page_size"]
        result = [dict(identity, total=len(all_items), items=all_items[(page-1)*size:page*size]), "C1", self_token]
        if self.fault:
            self.fault(self, path, body, result)
        return tuple(result)


class SyncTests(unittest.TestCase):
    def collect(self, request, sink, **kwargs):
        return sync.synchronize(request, "T001", "2026-2027", "1", sink.append, page_size=1, **kwargs)

    def test_T25_02_three_pages_and_operation_scoped_tokens(self):
        request, sink = Fixture(), []
        result = self.collect(request, sink)
        self.assertEqual(len(request.calls), 12)
        self.assertEqual(len(sink), 1)
        self.assertEqual(result["combination"], "C1")
        self.assertEqual(len(result["membership"]), 3)
        self.assertTrue(all(len(row["items"]) == 3 for row in result["membership"]))
        self.assertEqual(sync.SNAPSHOT, "X-Query-Snapshot")

    def test_T25_03_late_failure_never_applies(self):
        def fault(kind):
            def mutate(fixture, path, body, result):
                if path != sync.MEMBERS or body["teaching_class_number"] != "J2" or body["page"] != 3:
                    return
                if kind == "version": result[1] = "C2"
                elif kind == "missing_header": result[2] = None
                elif kind == "count": result[0]["total"] = 4
                elif kind == "duplicate": result[0]["items"] = [dict(student_number="S0")]
                elif kind == "missing_page": result[0]["items"] = []
                elif kind == "wrong_identity": result[0]["planned_course_code"] = "WRONG"
                elif kind == "bool_total": result[0]["total"] = True
                else: raise sync.SyncError("late HTTP failure")
            return mutate
        for kind in ("version", "missing_header", "count", "duplicate", "missing_page", "wrong_identity", "bool_total", "http"):
            with self.subTest(kind=kind):
                request, sink = Fixture(fault(kind)), []
                with self.assertRaises(sync.SyncError): self.collect(request, sink)
                self.assertEqual(sink, [])
                self.assertEqual(request.starts, 2 if kind == "version" else 1)

    def test_T25_03_retry_conflict_from_beginning(self):
        def fault(fixture, path, body, result):
            if path == sync.MEMBERS and fixture.starts == 1:
                raise sync.VersionConflict("synthetic conflict")
        request, sink = Fixture(fault), []
        self.collect(request, sink)
        self.assertEqual(request.starts, 2)
        self.assertEqual(len(sink), 1)

    def test_T25_03_counts_limits_empty_and_sink_failure(self):
        for count in (2, True, -1, 100001):
            def fault(fixture, path, body, result):
                if path == sync.CLASSES: result[0]["items"][0]["student_count"] = count
            sink = []
            with self.assertRaises(sync.SyncError): self.collect(Fixture(fault), sink)
            self.assertEqual(sink, [])
        request = Fixture()
        with self.assertRaises(sync.SyncError): self.collect(request, [], max_items=5)
        for attempts in (0, 4, True):
            with self.assertRaises(sync.SyncError): self.collect(Fixture(), [], attempts=attempts)
        def empty(fixture, path, body, result):
            result[0].update(total=0, items=[])
        sink = []
        self.collect(Fixture(empty), sink)
        self.assertEqual(sink[0]["classes"], [])
        calls = []
        def failing_sink(result):
            calls.append(result)
            raise sync.VersionConflict("sink cannot trigger network retry")
        request = Fixture()
        with self.assertRaises(sync.VersionConflict):
            sync.synchronize(request,"T001","2026-2027","1",failing_sink,page_size=1)
        self.assertEqual(len(calls),1)
        self.assertEqual(request.starts,1)

    def test_T25_03_atomic_file_preserves_previous_result_on_failure(self):
        with tempfile.TemporaryDirectory() as folder:
            path = Path(folder) / "result.json"
            path.write_text("previous", encoding="utf-8")
            with self.assertRaises(TypeError): sync.atomic_json(path, {"bad":object()})
            self.assertEqual(path.read_text(), "previous")
            self.assertEqual(list(Path(folder).iterdir()), [path])
            sync.atomic_json(path, {"complete":True})
            self.assertEqual(json.loads(path.read_text()), {"complete":True})

    def test_T25_01_http_headers_and_failure_envelopes(self):
        class Response(BytesIO):
            status = 200
            headers = {"X-Query-Snapshot":"opaque", "X-SICP-Combination":"C1"}
        for status, payload, expected in [
                (200, b'{"code":"ok","data":{"total":0,"items":[]}}', None),
                (200, b'{"code":"internal","data":null}', sync.SyncError),
                (200, b'not-json', sync.SyncError),
                (409, b'', sync.VersionConflict), (503, b'', sync.SyncError)]:
            with self.subTest(status=status, payload=payload):
                response = Response(payload)
                response.status = status
                with patch.object(sync.urllib.request, "build_opener") as build:
                    build.return_value.open.return_value = response
                    client = sync.http_client("https://sicp.example.invalid", "synthetic-key")
                    if expected:
                        with self.assertRaises(expected): client(sync.CLASSES, {"page":2}, "opaque")
                    else:
                        data, combination, token = client(sync.CLASSES, {"page":2}, "opaque")
                        self.assertEqual((combination, token), ("C1", "opaque"))
                        self.assertEqual(data["items"], [])
                    request = build.return_value.open.call_args.args[0]
                    self.assertEqual(request.get_header("X-query-snapshot"), "opaque")
                    self.assertEqual(request.get_header("X-api-key"), "synthetic-key")
                    self.assertEqual(json.loads(request.data), {"page":2})
                    self.assertEqual(request.full_url, "https://sicp.example.invalid" + sync.CLASSES)
                    self.assertTrue(any(isinstance(handler, sync.NoRedirect) for handler in build.call_args.args))


if __name__ == "__main__":
    unittest.main()
