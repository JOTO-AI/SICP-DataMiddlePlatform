"""Select private, stable query conditions from a retained source snapshot.

No historical responses or personal identifiers are printed. The output is
local deployment configuration, never a fixture to commit to the repository.
"""
import argparse
import json
import os
from pathlib import Path
import re


def rows(root, table):
    compact = root / "tables" / (table + ".json")
    paths = [compact] if compact.exists() else sorted((root / "responses" / table).glob("page*.json"))
    if not paths:
        raise ValueError("required source table is absent: " + table)
    for path in paths:
        payload = json.loads(path.read_text(encoding="utf-8-sig"))
        data = payload if isinstance(payload, list) else payload["data"]["list"]
        yield from data


def term(row):
    year, semester = row.get("KKXND"), row.get("KKXQM")
    if not isinstance(year, str) or not re.fullmatch(r"[0-9]{4}-[0-9]{4}", year):
        return None
    if int(year[:4]) < 1000 or int(year[5:]) != int(year[:4]) + 1 or semester not in ("1", "2"):
        return None
    return year, semester


def identifier(value):
    return isinstance(value, str) and 0 < len(value.encode()) <= 512 and bool(re.fullmatch(r"[\w.-]+", value))


def select(root):
    students = []
    for row in rows(root, "dwd_bzkscjzlb"):
        student = row.get("XH")
        if identifier(student) and student not in students:
            students.append(student)
            if len(students) == 2:
                break
    # Keep all terms for each join key: a membership has no term of its own.
    terms = {}
    staff = []
    for row in rows(root, "dwd_bzkspksjzlb"):
        when = term(row)
        if not when:
            continue
        key = row.get("JXBH"), row.get("JHKCH")
        if all(identifier(v) for v in key):
            terms.setdefault(key, set()).add(when)
        teacher_ids = row.get("JSJGH")
        if not isinstance(teacher_ids, str):
            continue
        for teacher in teacher_ids.split(","):
            teacher = teacher.strip()
            request = dict(staff_number=teacher, academic_year=when[0], semester=when[1])
            if identifier(teacher) and request not in staff and len(staff) < 2:
                staff.append(request)
    timetable = []
    for row in rows(root, "dwd_bzkskbsjzlb"):
        student = row.get("XH")
        matching = terms.get((row.get("JXBH"), row.get("JHKCH")), set())
        if not identifier(student) or len(matching) != 1:
            continue
        when = next(iter(matching))
        request = dict(student_number=student, academic_year=when[0], semester=when[1])
        if request not in timetable:
            timetable.append(request)
        if len(timetable) == 2:
            break
    if not students or not staff or not timetable:
        raise ValueError("snapshot cannot supply all four operations; use a complete retained snapshot")
    grades = []
    for student in students:
        grades.append(dict(student_number=student, academic_year="all", page=1, page_size=1))
        grades.append(dict(student_number=student, page=1, page_size=1))
    batch = [dict(student_numbers=[student], academic_year="all", page=1, page_size=1) for student in students]
    if len(students) > 1:
        batch.append(dict(student_numbers=students, academic_year="all", page=1, page_size=2))
    return {"students.grades.query": grades, "students.grades.batch-query": batch,
            "students.timetable.query": timetable, "staff.timetable.query": staff}


def main():
    os.umask(0o077)
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--samples", type=Path, required=True)
    parser.add_argument("--output", type=Path, default=Path("deploy/runtime/build-smoke-cases.json"))
    parser.add_argument("--env", type=Path, help="also update this private dotenv file")
    args = parser.parse_args()
    cases = select(args.samples)
    encoded = json.dumps(cases, ensure_ascii=False, separators=(",", ":"))
    args.output.parent.mkdir(parents=True, exist_ok=True)
    args.output.write_text(encoded + "\n", encoding="utf-8")
    if args.env:
        content = args.env.read_text(encoding="utf-8-sig")
        lines = [line for line in content.splitlines() if not line.startswith("BUILD_SMOKE_CASES=")]
        # Identifiers are restricted to letters/numbers/underscore/dot/hyphen.
        lines.append("BUILD_SMOKE_CASES='" + encoded + "'")
        args.env.write_text("\n".join(lines) + "\n", encoding="utf-8")
    print("Private fixed cases written; requests per operation: " + ", ".join(str(len(v)) for v in cases.values()))


if __name__ == "__main__":
    main()
