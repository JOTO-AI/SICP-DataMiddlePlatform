"""Collect interfaces 8 and 9 atomically, checking snapshots and membership counts."""
import argparse
from http.client import HTTPException
import json
import os
from pathlib import Path
import sys
import tempfile
import urllib.error
import urllib.request

from gateway_runtime import client_origin, tls_context

CLASSES = "/api/v1/sicp/staff/teaching-classes/query"
MEMBERS = "/api/v1/sicp/teaching-classes/students/query"
SNAPSHOT = "X-Query-Snapshot"
COMBINATION = "X-SICP-Combination"
MAX_RESPONSE = 16 * 1024 * 1024


class SyncError(Exception):
    """Safe diagnostic: never include source rows, credentials or tokens."""


class VersionConflict(SyncError):
    pass


class NoRedirect(urllib.request.HTTPRedirectHandler):
    def redirect_request(self, *args, **kwargs):
        return None


def http_client(base, key, ca_file=None):
    base = client_origin(None, base)
    if not key:
        raise SyncError("QUERY_API_KEY is required")
    opener = urllib.request.build_opener(
        urllib.request.ProxyHandler({}), NoRedirect(),
        urllib.request.HTTPSHandler(context=tls_context(ca_file)))

    def request(path, body, token):
        headers = {"Content-Type": "application/json", "X-API-Key": key}
        if token:
            headers[SNAPSHOT] = token
        req = urllib.request.Request(base + path, data=json.dumps(body).encode(),
                                     headers=headers, method="POST")
        try:
            response = opener.open(req, timeout=35)
        except urllib.error.HTTPError as error:
            response = error
        with response:
            raw = response.read(MAX_RESPONSE + 1)
            if len(raw) > MAX_RESPONSE:
                raise SyncError("response exceeds size limit")
            if response.status == 409:
                raise VersionConflict("snapshot or membership conflict")
            if response.status != 200:
                raise SyncError("HTTP query failed")
            try:
                envelope = json.loads(raw)
            except (ValueError, UnicodeError) as error:
                raise SyncError("invalid response JSON") from error
            if not isinstance(envelope, dict) or envelope.get("code") != "ok":
                raise SyncError("unsuccessful response")
            return envelope.get("data"), response.headers.get(COMBINATION), response.headers.get(SNAPSHOT)
    return request


def _id(item, fields):
    if not isinstance(item, dict):
        raise SyncError("invalid item")
    key = tuple(item.get(field) for field in fields)
    if any(not isinstance(value, str) or not value.strip() for value in key):
        raise SyncError("missing item identity")
    return key


def _pages(request, path, body, fields, version, page_size, max_items):
    token, expected, items, seen = None, None, [], set()
    for page in range(1, max_items // page_size + 2):
        data, actual, new_token = request(path, dict(body, page=page, page_size=page_size), token)
        if not isinstance(actual, str) or not actual or not isinstance(new_token, str) or not new_token:
            raise SyncError("missing snapshot headers")
        if version is not None and actual != version:
            raise VersionConflict("combination changed")
        version = actual
        # Keep this operation's initial token for all of its pages. Never send
        # the classes token to a members request or to a different class.
        token = token or new_token
        if not isinstance(data, dict):
            raise SyncError("missing response data")
        if any(data.get(key) != value for key, value in body.items()):
            raise SyncError("response identity differs")
        total, batch = data.get("total"), data.get("items")
        if type(total) is not int or not 0 <= total <= max_items or not isinstance(batch, list):
            raise SyncError("invalid total or items")
        if expected is not None and total != expected:
            raise SyncError("total changed within snapshot")
        expected = total
        if len(batch) != min(page_size, total - len(items)):
            raise SyncError("incomplete page")
        for item in batch:
            identity = _id(item, fields)
            if identity in seen:
                raise SyncError("duplicate item")
            seen.add(identity)
            items.append(item)
        if len(items) == total:
            return version, items
    raise SyncError("page limit exceeded")


def synchronize(request, staff_number, academic_year, semester, apply,
                page_size=100, attempts=2, max_items=100000):
    """Only invoke apply once, after one complete, consistent collection.

    Retry conflicts from page one with fresh tokens, at most attempts times.
    Other errors fail immediately. A sink failure is never retried here.
    """
    if (type(page_size) is not int or not 1 <= page_size <= 1000 or
            type(attempts) is not int or not 1 <= attempts <= 3 or
            type(max_items) is not int or not 1 <= max_items <= 1000000):
        raise SyncError("invalid collection limits")
    identity = dict(staff_number=staff_number, academic_year=academic_year, semester=semester)
    for attempt in range(attempts):
        try:
            version, classes = _pages(request, CLASSES, identity,
                                      ("teaching_class_number", "planned_course_code"),
                                      None, page_size, max_items)
            membership, collected = [], len(classes)
            for item in classes:
                count = item.get("student_count")
                if type(count) is not int or count < 0 or collected + count > max_items:
                    raise SyncError("invalid count or collection limit exceeded")
                body = dict(teaching_class_number=item["teaching_class_number"],
                            planned_course_code=item["planned_course_code"],
                            academic_year=academic_year, semester=semester)
                _, members = _pages(request, MEMBERS, body, ("student_number",),
                                     version, page_size, max_items - collected)
                if len(members) != count:
                    raise SyncError("class and roster counts differ")
                collected += len(members)
                membership.append(dict(body, items=members, total=count))
            result = dict(combination=version, **identity, classes=classes, membership=membership)
        except VersionConflict:
            if attempt + 1 == attempts:
                raise
            continue
        break
    apply(result)
    return result


def atomic_json(path, data):
    path = Path(path)
    temporary = None
    try:
        # Restrictive mode is set by mkstemp; keep the destination unchanged
        # if serialization or writing fails before the atomic replace.
        fd, temporary = tempfile.mkstemp(prefix=".sicp-sync-", dir=path.parent)
        with os.fdopen(fd, "w", encoding="utf-8") as stream:
            json.dump(data, stream, ensure_ascii=False, indent=2)
            stream.flush()
            os.fsync(stream.fileno())
        os.replace(temporary, path)
        temporary = None
    finally:
        if temporary is not None:
            os.unlink(temporary)


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--base-url", required=True)
    parser.add_argument("--ca-file")
    parser.add_argument("--staff-number", required=True)
    parser.add_argument("--academic-year", required=True)
    parser.add_argument("--semester", choices=("1", "2"), required=True)
    parser.add_argument("--page-size", type=int, default=100)
    parser.add_argument("--attempts", type=int, default=2)
    parser.add_argument("--max-items", type=int, default=100000)
    parser.add_argument("--output", type=Path, required=True)
    args = parser.parse_args()
    try:
        request = http_client(args.base_url, os.environ.get("QUERY_API_KEY"), args.ca_file)
        result = synchronize(request, args.staff_number, args.academic_year, args.semester,
                             lambda data: atomic_json(args.output, data),
                             args.page_size, args.attempts, args.max_items)
    except (SyncError, OSError, ValueError, HTTPException) as error:
        # Exception strings from network libraries may contain URLs or headers.
        print(f"Synchronization failed ({type(error).__name__}); no complete result written.", file=sys.stderr)
        return 1
    print(f"Collected {len(result['classes'])} classes; complete result written.")
    return 0


if __name__ == "__main__":
    sys.exit(main())
