"""Generate fictional snapshots and response files from checked-in recipes."""

import argparse
import hashlib
import json
import random
from copy import deepcopy
from dataclasses import asdict, dataclass
from pathlib import Path

from .source import (ROOT, TABLES, CAMPUS, ORG, STAFF, STAFF_CONTACT, STUDENT,
    CONTACT, STATUS, MAJOR, GRADE_MAJOR, CLASS, COURSE, GRADE, SCHEDULE, MEMBER,
    Number, response, write)

T0 = "2026-08-05 07:50:00"
T1 = "2026-08-05 09:00:00"
WINDOW = {"start_time": "2026-08-05 07:55:00", "end_time": "2026-08-05 10:00:00"}
VALUE_SHAPES = json.loads((ROOT / "recipes/source_value_shapes.json").read_text(encoding="utf-8"))


def code(table, field, index=0):
    """Use field-specific public code examples, never infer code meanings."""
    values = VALUE_SHAPES["codes"][table][field]["values"]
    return values[index % len(values)]


@dataclass(frozen=True)
class Config:
    seed: int = 20260918
    students: int = 12
    teachers: int = 6
    courses: int = 8
    grades: int = 36
    schedules: int = 16
    page_size: int = 5

    def validate(self):
        minima = {"students": 4, "teachers": 2, "courses": 4, "grades": 4, "schedules": 4, "page_size": 1}
        for name, minimum in minima.items():
            if type(getattr(self, name)) is not int or getattr(self, name) < minimum:
                raise ValueError(f"{name} must be an integer >= {minimum} (scenario coverage)")
        if type(self.seed) is not int:
            raise ValueError("seed must be an integer")


def sid(i):
    return f"002026{i + 1:05d}"


def tid(i):
    return ("001", "1001")[i] if i < 2 else f"T{i + 1:05d}"


def cid(i):
    return f"000C{i + 1:04d}"


def row(**fields):
    return {**fields, "ETL_TIME": T0}


def student_bundle(i):
    group = i % 4
    student = row(XH=sid(i), XM=f"虚构学生{i + 1:03d}", CSRQ="2008-01-15", MZM=code(STUDENT, "MZM"),
        RLSPL=f"https://video.campus.example/student/{sid(i)}", SFZJH=f"MOCK-STUDENT-{i + 1:06d}", SFZJLXM=code(STUDENT, "SFZJLXM"),
        SYDM=code(STUDENT, "SYDM", i), XBM=code(STUDENT, "XBM", i), ZZMMM=code(STUDENT, "ZZMMM"),
        YWXM=f"Fictional Student {i + 1}", XMPY=f"xugouxuesheng{i + 1}", HYZKM="MOCK_UNMARRIED",
        CYM=f"虚构曾用名{i + 1}", SFDSZN="0", GJDQM="MOCK_COUNTRY", JGM=code(STUDENT, "JGM"),
        CCQJ="虚构甲站至虚构乙站", SFZJYXQ="2030-01-15", TC="绘画", AH="阅读",
        YHKH=f"MOCK-BANK-STUDENT-{i + 1:06d}", CSDM="MOCK_BIRTHPLACE", GATQWM="MOCK_REGION",
        JKZKM="MOCK_HEALTH", XXM="MOCK_BLOOD", XYZJM="MOCK_RELIGION")
    status = row(XH=sid(i), SZNJ="2026", SZBH=f"00B{group + 1:02d}", RXNY="2026-09-01",
        SSXQH=f"0{group % 2 + 1}", XZ=3, YXSH=f"00D{group % 2 + 1}", YJBYRQ="2029-06-30",
        PYCCM=code(STATUS, "PYCCM"), XSLBM=code(STATUS, "XSLBM"), XSDQZTM="01", XJZTM=code(STATUS, "XJZTM"),
        ZYH=f"00M{group % 2 + 1}", SFZX="1", XXXSM=code(STATUS, "XXXSM"),
        HDXLFSM="MOCK_EDUCATION", XKMLM="MOCK_DISCIPLINE", SFXFZ="1", ZCXDNX="5",
        PYFSM=code(STATUS, "PYFSM"), KSH=f"MOCK-EXAM-{i + 1:06d}", LDFS="MOCK_SINGLE", SFQRZ="1")
    contact = row(XH=sid(i), YDDH=f"000{i + 1:08d}", DZXX=f" student{i + 1:03d}@campus.example ")
    return student, status, contact


def initial_snapshot(config):
    rng = random.Random(config.seed)
    data = {table: [] for table in TABLES}
    for i in range(2):
        data[CAMPUS].append(row(XQH=f"0{i + 1}", XQMC=f"虚构校区{i + 1}", XQCX="0"))
    data[ORG] = [row(DWH="10001", DWMC="虚构测试学院", LSDWH="0", DWYXBSM="1", SFSC="0", ZHXGSJ=T0)]
    for i in range(4):
        organization = row(DWH=f"00D{i + 1}", DWMC=f"虚构单位{i + 1}", LSDWH="10001" if i < 2 else f"00D{i - 1}",
            DWLBM=code(ORG, "DWLBM", i), DWYXBSM="1" if i != 3 else "0", SFSC="0", ZHXGSJ=T0, ZJXGRJGH=tid(0))
        if i >= 1:
            organization.update(JLRQ="20200101", CJR=tid(0))
        if i >= 2:
            organization["FGXLD"] = tid(0)
        data[ORG].append(organization)
    # Fields absent from observations still follow the original required contract.
    # These values are fictional examples, not inferred upstream dictionaries/formats.
    for i, organization in enumerate(data[ORG]):
        organization.update(DWYWMC=f"Fictional Organization {i + 1}", DWJC=f"虚构单位简称{i + 1}",
            DWYWJC=f"FO{i + 1}", DWJP=f"xgdw{i + 1}", DWDZ=f"虚构校区测试路{i + 1}号",
            DWBBM="MOCK_PUBLIC", SXRQ="2099-12-31", SFST="1", JLNY="2020-01",
            DWFZRJGH=tid(0), SFGK="0", LSDZZBH="MOCK-PARTY-001", DZZFZRJGH=tid(0), LSXQH="01")
        for field, value in {"DWLBM": code(ORG, "DWLBM"), "FGXLD": tid(1), "JLRQ": "20200101",
                             "CJR": tid(0), "ZJXGRJGH": tid(0)}.items():
            organization.setdefault(field, value)
    extras = ("CSRQ", "SFSJT", "SFSSX", "YHKH", "WPJS", "MZM", "ZZMMM", "ZGXLM", "LXRQ", "CJNY", "ZGXWM", "JKZKM")
    for i in range(config.teachers):
        teacher = row(JGH=tid(i), DWH=f"00D{i % 2 + 1}", XM=f"虚构教师{i + 1:03d}", XBM=code(STAFF, "XBM", i),
            SFZJLXM=code(STAFF, "SFZJLXM"), SFZJH=f"MOCK-STAFF-{i + 1:06d}", HYZKM=code(STAFF, "HYZKM", i),
            DQZTM=code(STAFF, "DQZTM"), JZBM=code(STAFF, "JZBM"))
        for field in extras[:(0, 5, 11, 12)[i % 4]]:
            if field == "CSRQ":
                teacher[field] = "1990-01-01"
            elif field in ("LXRQ", "CJNY"):
                teacher[field] = "01-SEP-20"
            elif field == "YHKH":
                teacher[field] = f"MOCK-BANK-{i + 1:06d}"
            else:
                teacher[field] = code(STAFF, field, i)
        if i == 4:
            teacher.update({k: code(STAFF, k) for k in ("BZLBM", "GWMCM", "JZGLBM", "ZGZCM")})
            teacher.update(CYM="虚构教师曾用名", XMPY="xugoujiaoshi", YWXM="Fictional Teacher")
        data[STAFF].append(teacher)
        contact = row(JGH=tid(i), YDDH=f"0009{i + 1:07d}", DZXX=f"teacher{i + 1:03d}@campus.example")
        if i == 1:
            contact.update(YDDH="00090000001", DZXX=" teacher002@campus.example ")
        data[STAFF_CONTACT].append(contact)
    for i in range(2):
        data[MAJOR].append(row(ZYH=f"00M{i + 1}", ZYMC=f"虚构专业{i + 1}", DWH=f"00D{i + 1}",
            PYCCM=code(MAJOR, "PYCCM"), PYFSM=code(MAJOR, "PYFSM"), XSLBM=code(MAJOR, "XSLBM"), XZ=3,
            YXZTM=code(MAJOR, "YXZTM"), ZYFLM=code(MAJOR, "ZYFLM"), ZYJC=f"测试专业{i + 1}",
            BZKZYM=f"MOCK-MAJOR-{i + 1}", XKMLM=code(MAJOR, "XKMLM"), SSZYHDL=f"00M{i + 1}",
            ZYCCM="MOCK_LEVEL", QSXQM="1", ZYYWMC=f"Fictional Major {i + 1}",
            JLNY="2020-01", PZSZNY="2020-02", SCZSNY="2020-09", SYXWLBM="MOCK_DEGREE",
            SSZYQH=f"MOCK-MAJOR-GROUP-{i + 1}", SFSFLZY="0", SFZZZY="1", FZRJGH=tid(i), ZSZTM="MOCK_OPEN"))
        data[GRADE_MAJOR].append(row(NJZYDM=f"00N{i + 1}", NJZYMC=f"2026虚构专业{i + 1}", NJ="2026",
            DWH=f"00D{i + 1}", XQH=f"0{i + 1}", ZYH=f"00M{i + 1}", ZYMC=f"虚构专业{i + 1}",
            XSLBM=code(GRADE_MAJOR, "XSLBM"), PYCCM=code(GRADE_MAJOR, "PYCCM"), XZ=3))
    for i in range(4):
        data[CLASS].append(row(BH=f"00B{i + 1:02d}", BJMC=f"虚构班级{i + 1}", FDYJGH=tid(i % config.teachers),
            JBNY="2026-09-01", NJZYDM=f"00N{i % 2 + 1}", SSDWH=f"00D{i % 2 + 1}", SSNJ="2026", SSXQH=f"0{i % 2 + 1}", XZ=3,
            BJJC=f"测试班{i + 1}", BJLBM="MOCK_CLASS", BJRS=str(sum(j % 4 == i for j in range(config.students))),
            YXZTM="1", BZRJGH=tid(i % config.teachers), BZXH=sid(i), SSZYH=f"00M{i % 2 + 1}",
            SFDDB="0", XQH=f"0{i % 2 + 1}", NJZYMC=f"2026虚构专业{i % 2 + 1}"))
    for i in range(config.students):
        student, status, contact = student_bundle(i)
        data[STUDENT].append(student)
        data[STATUS].append(status)
        data[CONTACT].append(contact)
    for i in range(config.courses):
        course = row(KCH=cid(i), KCMC=f"虚构课程{i + 1:03d}", XF=3 if i % 2 == 0 else Number("2.5"), ZXS=48,
            KCLBM=code(COURSE, "KCLBM"), KCSXM=code(COURSE, "KCSXM"), KCXZM=code(COURSE, "KCXZM"),
            SFXQHZKFKC=code(COURSE, "SFXQHZKFKC"), SFKZRTKC=code(COURSE, "SFKZRTKC"),
            KCLXM=code(COURSE, "KCLXM"), KKXY=f"00D{i % 2 + 1}", SFYX=code(COURSE, "SFYX"),
            SFWLK=code(COURSE, "SFWLK"), SFCXKC=code(COURSE, "SFCXKC"),
            KCYWMC=f"Fictional Course {i + 1}", KCJJ=f"虚构课程{i + 1}介绍", ZHXS=3,
            KCBQ=code(COURSE, "KCBQ"), CXKCH=cid(0), CXKCMC="虚构课程001")
        if i % 4 >= 1:
            course["ZHXS"] = Number("1.5") if i % 2 else 2
        if i % 4 >= 2:
            course["KCBQ"] = code(COURSE, "KCBQ")
        if i % 4 == 3:
            course.update(CXKCH=cid(0), CXKCMC="虚构课程001")
        if i == 4:
            course["KCJJ"] = "虚构课程介绍"
        if i == 2:
            course["KCYWMC"] = "Fictional Course"
        data[COURSE].append(course)
    for i in range(config.grades):
        data[GRADE].append(row(ID=f"G{i + 1:06d}", XH=sid(i % config.students), XN="2026-2027", XQM="1",
            KCH=cid(i % config.courses), XF=3 if i % 2 == 0 else Number("2.5"), JD=3 if i % 2 == 0 else Number("3.5"),
            KCCJ=rng.randint(60, 89) if i % 2 == 0 else Number(f"{rng.randint(60, 89)}.5")))
    # Fixed independent oracle: the newest term fails >=90, despite an older 95.
    data[GRADE][0].update(XH=sid(0), KCH=cid(0), XN="2025-2026", XQM="2", KCCJ=95)
    data[GRADE][1].update(XH=sid(0), KCH=cid(0), XN="2026-2027", XQM="1", KCCJ=Number("70.5"))
    groups = (config.schedules + 1) // 2
    for i in range(config.schedules):
        group = i // 2
        data[SCHEDULE].append(row(PKH=f"P{i + 1:06d}", JHKCH=cid(group % config.courses), JXBH=f"J2026-{group + 1:04d}",
            JXBMC=f"虚构教学班{group + 1}", JSJGH="001,1001" if group == 0 else tid(group % config.teachers),
            JSXM="虚构教师001,虚构教师002" if group == 0 else f"虚构教师{group % config.teachers + 1:03d}",
            KKXND="2026-2027", KKXQM="1", KCXZM=code(SCHEDULE, "KCXZM"), SKRQ="20260907", SKJC=str(i % 2 + 1), XQJ="星期一",
            SKSJ=("08:20:00", "09:00:00")[i % 2], JXZY="虚构教学资源", JXTZ="课堂讲授",
            RKJSM="MOCK_TEACHER", KKSM="虚构课程正常开课", PKYQ="使用多媒体教室",
            SKDDLBM="MOCK_CLASSROOM", XYJCCBH="MOCK-TEXTBOOK-001",
            KRL=max(40, config.students), XDRS=0, ZC="1", XKNJ="2026", SKBJH="00B01", SKJSH="00101",
            XKRSXD=max(40, config.students), QSZ=1, ZZZ=16, JSSZXQH="01", XKXQH="01", KCKSDWH=f"00D{group % config.courses % 2 + 1}", JXDD="虚构教学楼101", JSLXM=code(SCHEDULE, "JSLXM")))
    for i in range(config.students):
        for offset in range(2):
            group = (i + offset) % groups
            data[MEMBER].append(row(ID=f"R{2 * i + offset + 1:06d}", XH=sid(i), JHKCH=cid(group % config.courses), JXBH=f"J2026-{group + 1:04d}"))
    for schedule in data[SCHEDULE]:
        schedule["XDRS"] = len({m["XH"] for m in data[MEMBER] if (m["JHKCH"], m["JXBH"]) == (schedule["JHKCH"], schedule["JXBH"])})
    return data


def snapshots(config):
    initial = initial_snapshot(config)
    updated = deepcopy(initial)
    changes = []

    def change(table, index, *, values=None, remove=(), time=T1):
        before = deepcopy(initial[table][index])
        target = updated[table][index]
        for field in remove:
            del target[field]
        target.update(values or {})
        target["ETL_TIME"] = time
        changes.append({"table": table, "primary_key": target[TABLES[table]], "operation": "modified", "before": before, "after": deepcopy(target)})

    change(CAMPUS, 0, values={"XQMC": "虚构校区一（更新）"})
    change(ORG, 1, values={"DWMC": "虚构单位一（更新）", "FGXLD": tid(0)})
    change(STAFF, 0, values={"CYM": "虚构曾用名"})
    change(STAFF_CONTACT, 0, values={"DZXX": "teacher001.updated@campus.example"})
    change(STUDENT, 0, values={"CYM": "虚构学生曾用名", "JGM": code(STUDENT, "JGM"), "XMPY": "fictional"})
    change(CONTACT, 0, values={"DZXX": "student001@campus.example"})
    change(CONTACT, 1, values={"YDDH": "00080000002"})
    change(STATUS, 0, values={"PYFSM": code(STATUS, "PYFSM")})
    change(MAJOR, 0, values={"ZYJC": "虚构专业简称", "XKMLM": code(MAJOR, "XKMLM")})
    change(GRADE_MAJOR, 0)  # ETL_TIME only.
    class_index = config.students % 4
    class_values = {"BJMC": "虚构班级一（更新）"}
    if class_index == 0:
        class_values["BJRS"] = str(int(initial[CLASS][0]["BJRS"]) + 1)
    change(CLASS, 0, values=class_values)
    change(COURSE, 1, values={"ZHXS": 3})
    change(GRADE, 2, values={"KCCJ": Number("88.5")})
    change(SCHEDULE, 0, values={"SKJSH": "00102"})
    change(MEMBER, 0)
    for table, record in zip((STUDENT, STATUS, CONTACT), student_bundle(config.students)):
        record["ETL_TIME"] = "2026-08-05 09:10:00"
        updated[table].append(record)
        changes.append({"table": table, "primary_key": record[TABLES[table]], "operation": "added", "after": deepcopy(record)})
    # Keep the newly populated class count consistent with the new student's status.
    if class_index != 0:
        change(CLASS, class_index, values={"BJRS": str(sum(r["SZBH"] == f"00B{class_index + 1:02d}" for r in updated[STATUS]))})
    deleted = deepcopy(updated)
    del deleted[CONTACT][2]
    deletion = {"table": CONTACT, "primary_key": sid(2), "operation": "deleted", "source_deleted_at": "2026-08-05 11:00:00", "before": deepcopy(updated[CONTACT][2]), "incremental_tombstone": False}
    return {"initial": initial, "updated": updated, "after_delete": deleted}, changes, deletion


def generate(root=ROOT, config=Config()):
    config.validate()
    root = Path(root)
    previous = root / "manifest.json"
    if previous.exists():
        previous_config = json.loads(previous.read_text(encoding="utf-8")).get("config")
        if previous_config != asdict(config):
            raise ValueError("Use a new --output directory when changing configuration; existing fixtures are preserved")
    # No raw source records, credentials, database or network are read here.
    catalog = json.loads((ROOT / "recipes/source_shapes.json").read_text(encoding="utf-8"))
    all_snapshots, changes, deletion = snapshots(config)
    written = set()

    def emit(path, value):
        write(path, value)
        written.add(path)

    emit(root / "recipes/source_shapes.json", catalog)
    emit(root / "recipes/source_value_shapes.json", VALUE_SHAPES)
    requests = []
    for name, tables in all_snapshots.items():
        manifest = {"snapshot": name, "source_snapshot": {"initial": None, "updated": "initial", "after_delete": "updated"}[name],
            "purpose": "补充边界：源端物理删除" if name == "after_delete" else "业务可用基准", "config": asdict(config), "timezone": "Asia/Shanghai", "tables": {}}
        for table, rows in tables.items():
            rows.sort(key=lambda r: r[TABLES[table]])
            emit(root / "snapshots" / name / f"{table}.json", rows)
            manifest["tables"][table] = {"primary_key": TABLES[table], "row_count": len(rows), "fields": catalog["tables"][table]["fields"]}
            expected = {"primary_keys": [r[TABLES[table]] for r in rows], "rows": rows}
            emit(root / "expected" / name / f"{table}.json", expected)
            modes = {"full": {}}
            if name == "updated":
                modes.update(incremental=WINDOW, repeat=WINDOW,
                    overlap={"start_time": "2026-08-05 08:55:00", "end_time": "2026-08-05 10:00:00"},
                    empty={"start_time": "2026-08-05 10:00:00", "end_time": "2026-08-05 12:00:00"})
            if name == "after_delete":
                modes["deletion_window"] = {"start_time": "2026-08-05 10:00:00", "end_time": "2026-08-05 12:00:00"}
            for mode, window in modes.items():
                first = response(rows, TABLES[table], page_size=config.page_size, **window)
                count = first["data"]["total"]
                page_count = max(1, (count + config.page_size - 1) // config.page_size)
                # Includes one out-of-range page, preserving filtered total.
                for page in range(1, page_count + 2):
                    path = f"responses/{name}/{mode}/{table}/page{page:04d}.json"
                    body = response(rows, TABLES[table], page=page, page_size=config.page_size, **window)
                    emit(root / path, body)
                    params = {"page": page, "pageSize": config.page_size}
                    if window:
                        params.update(startTime=window["start_time"], endTime=window["end_time"])
                    requests.append({"snapshot": name, "table": table, "mode": mode, "path": "/apizt/openapi/invoke/sjzc/release/" + table,
                        "params": params, "response": path, "expected_total": count,
                        "expected_primary_keys": [r[TABLES[table]] for r in body["data"]["list"]]})
        emit(root / "snapshots" / name / "manifest.json", manifest)
    emit(root / "changes.json", {"initial_to_updated": changes, "updated_to_after_delete": [deletion]})
    emit(root / "requests.json", requests)
    from .scenarios import generate_scenarios
    generate_scenarios(root, all_snapshots["initial"], catalog, writer=emit)
    paths = sorted(written)
    manifest = {"format_version": 3, "config": asdict(config), "timezone": "Asia/Shanghai", "fictional_values": True,
        "normal_generator_reads_personal_samples": False, "window": WINDOW,
        "groups": {"snapshots/initial": "业务可用基准", "snapshots/updated": "业务可用基准", "relation_variants": "实测缺口变体", "supplemental": "补充边界", "invalid_responses": "故意异常"},
        "comparison": "Object/row order ignored; numeric spelling, string whitespace and ETL_TIME retained.",
        "files": {p.relative_to(root).as_posix(): hashlib.sha256(p.read_bytes()).hexdigest() for p in paths}}
    write(root / "manifest.json", manifest)
    return manifest


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--output", type=Path, default=ROOT)
    for field, default in asdict(Config()).items():
        parser.add_argument("--" + field.replace("_", "-"), type=int, default=default)
    args = vars(parser.parse_args())
    output = args.pop("output")
    result = generate(output, Config(**args))
    print(f"Generated {len(result['files'])} JSON files in {output.resolve()}")


if __name__ == "__main__":
    main()
