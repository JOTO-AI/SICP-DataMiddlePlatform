"""Offline source reader and lossless JSON utilities (Python 3.10+)."""

import json
import re
from datetime import datetime, timedelta, timezone
from pathlib import Path

ROOT = Path(__file__).resolve().parent
SHANGHAI = timezone(timedelta(hours=8), name="Asia/Shanghai")
TABLES = dict(zip(
    ("dwd_xqjbsjzlb", "dwd_yxsdwjbsjzlb", "dwd_jzgjcsjzlb", "dwd_grtxfszlb",
     "dwd_bzksjbsjzlb", "dwd_bzksgrtxsjzlb", "dwd_bzksxjjbsjzlb",
     "dwd_bzkszyxxsjzlb", "dwd_bzksnjzysjzlb", "dwd_bzksbjsjzlb",
     "dwd_bzkskcsjzlb", "dwd_bzkscjzlb", "dwd_bzkspksjzlb", "dwd_bzkskbsjzlb"),
    ("XQH", "DWH", "JGH", "JGH", "XH", "XH", "XH", "ZYH", "NJZYDM", "BH", "KCH", "ID", "PKH", "ID"),
))
CAMPUS, ORG, STAFF, STAFF_CONTACT, STUDENT, CONTACT, STATUS, MAJOR, GRADE_MAJOR, CLASS, COURSE, GRADE, SCHEDULE, MEMBER = TABLES


class Number:
    """A JSON number token: preserve 3, 3.0, 3.00 and exponents exactly."""

    def __init__(self, token):
        if not re.fullmatch(r"-?(?:0|[1-9]\d*)(?:\.\d+)?(?:[eE][+-]?\d+)?", token):
            raise ValueError(f"Invalid JSON number: {token}")
        self.token = token

    def __eq__(self, other):
        return isinstance(other, Number) and self.token == other.token

    def __repr__(self):
        return f"Number({self.token!r})"


def loads(text, *, lossless=True):
    """Preserve number tokens by default; opt out for ordinary business tests."""
    def invalid(value):
        raise ValueError(f"Invalid JSON constant: {value}")
    if lossless:
        return json.loads(text, parse_int=Number, parse_float=Number, parse_constant=invalid)
    return json.loads(text, parse_constant=invalid)


def dumps(value, *, pretty=True):
    def encode(item):
        if isinstance(item, Number):
            return item.token
        if isinstance(item, dict):
            return "{" + ",".join(json.dumps(k, ensure_ascii=False) + ":" + encode(v) for k, v in item.items()) + "}"
        if isinstance(item, (list, tuple)):
            return "[" + ",".join(encode(v) for v in item) + "]"
        return json.dumps(item, ensure_ascii=False, allow_nan=False)
    # Formatting must never parse the tokens through float.
    compact = encode(value)
    if not pretty:
        return compact
    output, depth, quoted, escaped = [], 0, False, False
    for char in compact:
        if quoted:
            output.append(char)
            if escaped:
                escaped = False
            elif char == "\\":
                escaped = True
            elif char == '"':
                quoted = False
        elif char == '"':
            quoted = True
            output.append(char)
        elif char in "{[":
            depth += 1
            output.append(char + "\n" + "  " * depth)
        elif char in "}]":
            depth -= 1
            output.append("\n" + "  " * depth + char)
        elif char == ",":
            output.append(",\n" + "  " * depth)
        elif char == ":":
            output.append(": ")
        else:
            output.append(char)
    return "".join(output) + "\n"


def read(path, *, lossless=True):
    return loads(Path(path).read_text(encoding="utf-8"), lossless=lossless)


def write(path, value):
    path = Path(path)
    path.parent.mkdir(parents=True, exist_ok=True)
    path.write_text(dumps(value), encoding="utf-8", newline="\n")


def canonical(value):
    """Ignore object order, but preserve numeric spelling and all string bytes."""
    def ordered(item):
        if isinstance(item, dict):
            return {key: ordered(item[key]) for key in sorted(item)}
        if isinstance(item, list):
            return [ordered(v) for v in item]
        return item
    return dumps(ordered(value), pretty=False)


def row_map(rows, primary_key):
    result = {}
    for row in rows:
        key = row.get(primary_key)
        if not isinstance(key, str) or not key.strip():
            raise ValueError(f"Invalid primary key {primary_key}")
        if key in result:
            raise ValueError(f"Duplicate primary key {primary_key}={key}")
        result[key] = row
    return result


def parse_time(value):
    if not isinstance(value, str) or not re.fullmatch(r"\d{4}-\d{2}-\d{2}(?: \d{2}:\d{2}:\d{2})?", value):
        raise ValueError("Time must be YYYY-MM-DD or YYYY-MM-DD HH:MM:SS")
    return datetime.strptime(value, "%Y-%m-%d" if len(value) == 10 else "%Y-%m-%d %H:%M:%S").replace(tzinfo=SHANGHAI)


def response(rows, primary_key, *, page=1, page_size=5, start_time=None, end_time=None):
    if type(page) is not int or type(page_size) is not int or page < 1 or page_size < 1:
        raise ValueError("page and page_size must be positive integers")
    if (start_time is None) != (end_time is None):
        raise ValueError("Supply both start_time and end_time, or neither")
    row_map(rows, primary_key)
    selected = list(rows)
    if start_time is not None:
        start, end = parse_time(start_time), parse_time(end_time)
        if start >= end:
            raise ValueError("start_time must precede end_time")
        selected = [r for r in selected if start <= parse_time(r["ETL_TIME"]) < end]
    selected.sort(key=lambda r: r[primary_key])
    offset = (page - 1) * page_size
    return {"code": 200, "message": "请求成功", "data": {"list": selected[offset:offset + page_size], "total": len(selected)}}


class MockSource:
    """Load an explicit fixed snapshot; source request names match the upstream."""

    def __init__(self, snapshot="initial", root=ROOT, *, lossless=True):
        if snapshot not in ("initial", "updated", "after_delete"):
            raise ValueError("Unknown snapshot")
        self.root, self.snapshot = Path(root), snapshot
        self.lossless = lossless

    def rows(self, table):
        if table not in TABLES:
            raise ValueError("Unknown source table")
        return read(self.root / "snapshots" / self.snapshot / f"{table}.json", lossless=self.lossless)

    def request(self, table, *, page=1, pageSize=5, startTime=None, endTime=None):
        return response(self.rows(table), TABLES[table], page=page, page_size=pageSize,
                        start_time=startTime, end_time=endTime)


def load_case(name, *, root=ROOT, lossless=True):
    """Return (input, expected) by scenario ID, independently of the working directory."""
    root = Path(root).resolve()
    cases = read(root / "expected/scenarios.json", lossless=False)
    matches = [case for case in cases if case["id"] == name]
    if len(matches) != 1:
        raise ValueError(f"Expected one scenario named {name!r}; found {len(matches)}")
    paths = [(root / matches[0][key]).resolve() for key in ("input", "expected")]
    if not all(path.is_relative_to(root) for path in paths):
        raise ValueError("Scenario files must stay within the dataset directory")
    return tuple(read(path, lossless=lossless) for path in paths)
