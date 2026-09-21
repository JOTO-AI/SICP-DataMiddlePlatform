"""Validate fixture shape, relationships, evolution, pages and independent cases."""

import argparse
import hashlib
import json
import re
from collections import defaultdict
from datetime import datetime
from decimal import Decimal
from pathlib import Path

from .source import (ROOT, TABLES, CAMPUS, ORG, STAFF, STAFF_CONTACT, STUDENT,
    CONTACT, STATUS, MAJOR, GRADE_MAJOR, CLASS, COURSE, GRADE, SCHEDULE, MEMBER,
    Number, canonical, parse_time, read, row_map)


class FixtureError(ValueError):
    pass


def require(condition, message):
    if not condition:
        raise FixtureError(message)


def integer(value):
    if type(value) is int:
        return value
    if isinstance(value, Number) and re.fullmatch(r"-?(?:0|[1-9]\d*)", value.token):
        return int(value.token)
    raise FixtureError("total_not_integer")


def shape(table, rows, catalog, *, unique=True, required=True):
    """Check documented required values and source JSON types, not DB column types."""
    require(isinstance(rows, list), "list_not_array")
    key = TABLES[table]
    seen = set()
    fields = catalog["tables"][table]["fields"]
    for record in rows:
        require(isinstance(record, dict), "row_not_object")
        require(key in record, "missing_primary_key")
        require(isinstance(record[key], str), "primary_key_type")
        require(bool(record[key].strip()), "empty_primary_key")
        if unique:
            require(record[key] not in seen, "duplicate_primary_key_conflict")
        seen.add(record[key])
        if required:
            for field, spec in fields.items():
                if spec["document_required"]:
                    require(field in record, f"missing_required_field:{table}.{field}")
                    value = record[field]
                    require(value is not None and not (isinstance(value, str) and not value.strip()),
                            f"empty_required_field:{table}.{field}")
        for field, value in record.items():
            require(field in fields, "unknown_business_field")
            kind = "string" if isinstance(value, str) else "number" if isinstance(value, Number) or type(value) in (int, float) else "unsupported"
            require(kind in fields[field]["normal_types"], "field_type")
            require(value != "", "empty_string_in_normal_fixture")
            if kind == "number":
                numeric = Decimal(value.token if isinstance(value, Number) else str(value))
                require(numeric.is_finite(), "non_finite_number")
                if fields[field].get("normal_number_form") == "integer_value":
                    require(numeric == numeric.to_integral_value(), "integer_field_fraction")
        require(isinstance(record.get("ETL_TIME"), str) and len(record["ETL_TIME"]) == 19, "invalid_etl_time")
        parse_time(record["ETL_TIME"])


def check_date_values(table, rows):
    """Preserve each source field's date representation and check calendar validity."""
    date_formats = {
        STAFF: {"CSRQ": "%Y-%m-%d", "LXRQ": "DD-MON-YY", "CJNY": "DD-MON-YY"},
        STUDENT: {"CSRQ": "%Y-%m-%d"},
        STATUS: {"RXNY": "%Y-%m-%d", "YJBYRQ": "%Y-%m-%d"},
        CLASS: {"JBNY": "%Y-%m-%d"}, ORG: {"JLRQ": "%Y%m%d"},
    }.get(table, {})
    patterns = {"%Y-%m-%d": r"[0-9]{4}-[0-9]{2}-[0-9]{2}",
                "%Y%m%d": r"[0-9]{8}", "DD-MON-YY": r"[0-9]{2}-[A-Z]{3}-[0-9]{2}"}
    months = {m: i for i, m in enumerate(
        ("JAN", "FEB", "MAR", "APR", "MAY", "JUN", "JUL", "AUG", "SEP", "OCT", "NOV", "DEC"), 1)}
    for record in rows:
        for field, format_string in date_formats.items():
            if field not in record:
                continue  # Optional fields need not be present.
            value = record[field]
            require(isinstance(value, str), "field_type")
            try:
                if not re.fullmatch(patterns[format_string], value):
                    raise ValueError("Date representation does not match the source field")
                if format_string == "DD-MON-YY":
                    day, month, year = value.split("-")
                    # Calendar check only; leave the source string and century policy untouched.
                    datetime(2000 + int(year), months[month], int(day))
                else:
                    datetime.strptime(value, format_string)
            except (ValueError, KeyError) as error:
                raise FixtureError(f"invalid_date_value:{table}.{field}") from error


def check_response(body, table, catalog):
    require(isinstance(body, dict), "response_not_object")
    require(integer(body.get("code")) == 200, "non_success_response")
    require(isinstance(body.get("message"), str), "message_not_string")
    require(isinstance(body.get("data"), dict), "missing_data")
    data = body["data"]
    total = integer(data.get("total"))
    require(total >= 0, "negative_total")
    shape(table, data.get("list"), catalog)
    require(len(data["list"]) <= total, "page_exceeds_total")
    return data["list"], total


def check_pages(bodies, table, page_size, catalog):
    require(bool(bodies), "incomplete_pagination")
    rows, expected_total, keys = [], None, set()
    for body in bodies:
        page, total = check_response(body, table, catalog)
        if expected_total is None:
            expected_total = total
        require(total == expected_total, "inconsistent_total")
        remaining = max(0, total - len(rows))
        require(len(page) == min(page_size, remaining), "incomplete_pagination")
        for record in page:
            key = record[TABLES[table]]
            require(key not in keys, "duplicate_across_pages")
            keys.add(key)
            rows.append(record)
    require(len(rows) == expected_total, "incomplete_pagination")
    return rows


def check_relations(data):
    maps = {t: row_map(rows, TABLES[t]) for t, rows in data.items()}
    require("10001" in maps[ORG], "missing_school_root")
    require(maps[ORG]["10001"].get("LSDWH") == "0", "root_parent_sentinel")
    for key, record in maps[ORG].items():
        path = set()
        while key != "10001":
            require(key not in path, "organization_cycle")
            path.add(key)
            key = maps[ORG][key].get("LSDWH")
            require(key in maps[ORG], "missing_parent")
    for source, field, target in (
        (STAFF, "DWH", ORG), (STAFF_CONTACT, "JGH", STAFF), (CONTACT, "XH", STUDENT),
        (STATUS, "XH", STUDENT), (STATUS, "SZBH", CLASS), (STATUS, "YXSH", ORG),
        (STATUS, "SSXQH", CAMPUS), (STATUS, "ZYH", MAJOR), (MAJOR, "DWH", ORG),
        (GRADE_MAJOR, "DWH", ORG), (GRADE_MAJOR, "ZYH", MAJOR), (GRADE_MAJOR, "XQH", CAMPUS),
        (CLASS, "NJZYDM", GRADE_MAJOR), (CLASS, "FDYJGH", STAFF), (CLASS, "SSDWH", ORG),
        (CLASS, "SSXQH", CAMPUS), (COURSE, "KKXY", ORG), (GRADE, "XH", STUDENT),
        (GRADE, "KCH", COURSE), (SCHEDULE, "JHKCH", COURSE), (MEMBER, "XH", STUDENT), (MEMBER, "JHKCH", COURSE),
    ):
        for record in data[source]:
            require(record.get(field) in maps[target], f"missing_relation:{source}.{field}")
    require(set(maps[STUDENT]) == set(maps[STATUS]), "student_status_mismatch")
    for table in (STUDENT, STAFF):
        require(all(r.get("XM", "").strip() for r in data[table]), "missing_person_name")
    terms = defaultdict(set)
    for record in data[SCHEDULE]:
        require(all(record.get(f) for f in ("JHKCH", "JXBH", "KKXND", "KKXQM", "SKRQ", "SKJC", "XQJ")), "schedule_not_ready")
        require(bool(re.fullmatch(r"\d{8}", record["SKRQ"])), "schedule_date_shape")
        try:
            day = datetime.strptime(record["SKRQ"], "%Y%m%d")
        except ValueError as error:
            raise FixtureError("schedule_date_value") from error
        weekdays = ("星期一", "星期二", "星期三", "星期四", "星期五", "星期六", "星期日")
        require(record["XQJ"] in weekdays, "weekday_shape")
        require(record["XQJ"] == weekdays[day.weekday()], "schedule_weekday_mismatch")
        require(bool(re.fullmatch(r"[1-9][0-9]*", record["SKJC"])), "schedule_section_shape")
        tokens = record["JSJGH"].split(",")
        require(len(tokens) == len(set(tokens)) and all(t == t.strip() and t in maps[STAFF] for t in tokens), "teacher_tokens")
        terms[(record["JHKCH"], record["JXBH"])].add((record["KKXND"], record["KKXQM"]))
    require(all(len(values) == 1 for values in terms.values()), "cross_term_ambiguity")
    for record in data[MEMBER]:
        require((record["JHKCH"], record["JXBH"]) in terms, "member_missing_schedule")


def validate(root=ROOT):
    root = Path(root)
    manifest = json.loads((root / "manifest.json").read_text(encoding="utf-8"))
    for name, digest in manifest["files"].items():
        path = (root / name).resolve()
        require(path.is_relative_to(root.resolve()), "manifest_path_outside_root")
        require(path.is_file() and hashlib.sha256(path.read_bytes()).hexdigest() == digest, f"file_hash:{name}")
    catalog = json.loads((root / "recipes/source_shapes.json").read_text(encoding="utf-8"))
    require(set(catalog["tables"]) == set(TABLES), "table_catalog")
    require(sum(len(t["fields"]) for t in catalog["tables"].values()) == 274, "field_catalog")
    for table, key in TABLES.items():
        for field, spec in catalog["tables"][table]["fields"].items():
            expected_storage = "JSON" if spec["current_document_type"] == "JSON number" else {
                "String": "TEXT", "Integer": "INTEGER",
                "LocalDateTime": "TIMESTAMP WITHOUT TIME ZONE"}[spec["document_type"]]
            require(spec.get("pg_storage_type") == expected_storage,
                    f"pg_storage_type:{table}.{field}")
    snapshots = {}
    for snapshot in ("initial", "updated", "after_delete"):
        snapshot_manifest = json.loads((root / "snapshots" / snapshot / "manifest.json").read_text(encoding="utf-8"))
        tables = {}
        for table, key in TABLES.items():
            rows = read(root / "snapshots" / snapshot / f"{table}.json")
            shape(table, rows, catalog)
            check_date_values(table, rows)
            require(len(rows) == snapshot_manifest["tables"][table]["row_count"], "snapshot_row_count")
            require([r[key] for r in rows] == sorted(r[key] for r in rows), "snapshot_sort")
            expected = read(root / "expected" / snapshot / f"{table}.json")
            require([r[key] for r in rows] == expected["primary_keys"], "snapshot_expected_keys")
            require(canonical(rows) == canonical(expected["rows"]), "snapshot_expected_rows")
            tables[table] = rows
        check_relations(tables)
        snapshots[snapshot] = tables

    change_sets = read(root / "changes.json")
    for before_name, after_name, change_name in (("initial", "updated", "initial_to_updated"), ("updated", "after_delete", "updated_to_after_delete")):
        actual = {}
        for table, key in TABLES.items():
            before = row_map(snapshots[before_name][table], key)
            after = row_map(snapshots[after_name][table], key)
            for pk in set(before) | set(after):
                if pk not in before:
                    actual[(table, pk)] = "added"
                elif pk not in after:
                    actual[(table, pk)] = "deleted"
                elif canonical(before[pk]) != canonical(after[pk]):
                    actual[(table, pk)] = "modified"
        changes = change_sets[change_name]
        require(len(changes) == len(actual), "change_count")
        require(actual == {(c["table"], c["primary_key"]): c["operation"] for c in changes}, "change_keys")
        for change in changes:
            for label, snapshot in (("before", before_name), ("after", after_name)):
                if label in change:
                    record = row_map(snapshots[snapshot][change["table"]], TABLES[change["table"]])[change["primary_key"]]
                    require(canonical(record) == canonical(change[label]), "change_payload")
    grouped = defaultdict(list)
    requests = json.loads((root / "requests.json").read_text(encoding="utf-8"))
    for request in requests:
        params, table = request["params"], request["table"]
        body = read(root / request["response"])
        page, total = check_response(body, table, catalog)
        selected = snapshots[request["snapshot"]][table]
        if "startTime" in params:
            start, end = parse_time(params["startTime"]), parse_time(params["endTime"])
            selected = [r for r in selected if start <= parse_time(r["ETL_TIME"]) < end]
        require(total == len(selected) == request["expected_total"], "filtered_total")
        offset = (params["page"] - 1) * params["pageSize"]
        require(canonical(page) == canonical(selected[offset:offset + params["pageSize"]]), "page_payload")
        require([r[TABLES[table]] for r in page] == request["expected_primary_keys"], "page_keys")
        grouped[(request["snapshot"], request["mode"], table)].append((params["page"], params["pageSize"], body))
    for (_, _, table), pages in grouped.items():
        pages.sort(key=lambda p: p[0])
        require([p[0] for p in pages] == list(range(1, len(pages) + 1)), "page_sequence")
        check_pages([p[2] for p in pages], table, pages[0][1], catalog)
    # Independently reconcile each table by replacement, not field-wise update.
    for table, key in TABLES.items():
        merged = row_map(snapshots["initial"][table], key)
        for record in snapshots["updated"][table]:
            if manifest["window"]["start_time"] <= record["ETL_TIME"] < manifest["window"]["end_time"]:
                merged[record[key]] = record
        require(canonical(merged) == canonical(row_map(snapshots["updated"][table], key)), "incremental_reconciliation")
    scenarios = json.loads((root / "expected/scenarios.json").read_text(encoding="utf-8"))
    for case in scenarios:
        inputs, expected = read(root / case["input"]), read(root / case["expected"])
        if case["group"] == "invalid_responses":
            try:
                if "responses" in inputs:
                    check_pages(inputs["responses"], inputs["table"], integer(inputs["page_size"]), catalog)
                else:
                    check_response(inputs["response"], inputs["table"], catalog)
            except FixtureError as error:
                require(str(error) == expected["error"], f"wrong_invalid_error:{case['id']}:{error}")
            else:
                raise FixtureError("invalid_case_accepted:" + case["id"])
        elif case["group"] == "relation_variants":
            for table, rows in inputs.get("tables", {inputs.get("table"): inputs.get("rows")}).items():
                shape(table, rows, catalog, required=expected.get("document_required_valid", True))
                check_date_values(table, rows)
    return {"tables": len(TABLES), "snapshots": len(snapshots), "source_rows": {n: sum(len(r) for r in t.values()) for n, t in snapshots.items()},
        "response_files": len(requests), "scenarios": len(scenarios), "checked_files": len(manifest["files"])}


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--root", type=Path, default=ROOT)
    args = parser.parse_args()
    print(json.dumps(validate(args.root), ensure_ascii=False, indent=2))


if __name__ == "__main__":
    main()
