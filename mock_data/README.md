# 开发用源接口 Mock 数据

按项目根目录 `MOCK_DATA_GENERATION_PLAN.md` 生成，2026-09-20 完成交付。全部人员、联系方式及业务名称均为虚构值，邮箱使用 `.example`。不调用真实源接口，不读取真实人员记录，不发送邮件。

## 直接使用

在项目根目录执行，生成器、读取器和校验器仅依赖 Python 3.10+ 标准库；自动化测试使用 pytest。

```powershell
# 数据已生成；以下命令可按同配置复现。
python -m mock_data.generate
python -m mock_data.validate
python tools/audit_mock_required_fields.py
python -m pytest -q tests/test_mock_data.py tests/test_mock_required_audit.py
```

默认数据包括 **14 张表、274 个字段定义、3 轮完整快照、293 个分页响应和 75 个独立场景**。三轮正常快照合计覆盖全部 179 个已实测字段。快照是 JSON 对象数组，不包含响应外壳；响应文件使用 `code/message/data.list/data.total`。

| 快照 | 用途 | 总行数 |
| --- | --- | ---: |
| `initial` | F0，关联完整的首次全量 | 147 |
| `updated` | F1，各表修改、新增学生及附表 | 150 |
| `after_delete` | F2，删除学号 `00202600003` 的通讯行 | 149 |

F0 默认学生 12、教师 6、课程 8、成绩 36、排课 16、学生课表关系 24；另有校区 2、单位 5、专业 2、年级专业 2、行政班 4。学籍与学生通讯各 12、教师通讯 6。

## 在单元测试中加载

```python
from mock_data import MockSource, load_case
from mock_data.source import canonical, row_map, dumps

# 普通业务单测：直接返回 Python int/float，可比较、运算和 json.dumps。
source = MockSource("updated", lossless=False)
body = source.request(
    "dwd_bzksgrtxsjzlb",
    page=1,
    pageSize=5,
    startTime="2026-08-05 07:55:00",
    endTime="2026-08-05 10:00:00",
)
assert body["data"]["total"] == 3
assert [r["XH"] for r in body["data"]["list"]] == [
    "00202600001", "00202600002", "00202600013"
]
assert body["data"]["list"][1]["YDDH"] == "00080000002"

# 用固定的小样例测试你的合并函数：input.json 提供 before/incoming，
# expected.json 提供独立写定的 rows、added、modified 和读取后字段预期。
inputs, expected = load_case("full_row_merge")
# actual = your_merge(inputs["before"], inputs["incoming"])
# assert canonical(row_map(actual, "XH")) == canonical(row_map(expected["rows"], "XH"))

# 模拟 HTTP transport 并测试原始数字表示时，使用默认无损模式。
body = MockSource("updated").request("dwd_bzkskcsjzlb", page=1, pageSize=5)
response_text = dumps(body)
```

`load_case("场景ID")` 按场景索引返回 `(inputs, expected)`，无需拼接路径，不依赖测试进程的当前目录。普通业务场景可以传 `lossless=False`，例如 `load_case("multiple_schedules_teachers", lossless=False)`，这样预期的 `student_count` 可直接与整数比较。每次调用返回独立对象，修改数据不会污染其他测试。

`MockSource` 适合注入采集层或替换 transport。没有启动 HTTP 服务。各请求的源路径、参数和对应文件见 `requests.json`，可直接作为 HTTP 客户端测试替身的返回内容。

命令行读取一个响应：

```powershell
python -m mock_data --snapshot updated --table dwd_bzksgrtxsjzlb --page 1 --page-size 5 --start-time "2026-08-05 07:55:00" --end-time "2026-08-05 10:00:00"
```

## 数字、空值和时间约定

本地存储采用原网页声明，并按实测修正数字字段：256个TEXT（含14个主键）、15个JSON、3个TIMESTAMP WITHOUT TIME ZONE；业务转换留到查询接口，不建立转换表。接口类型列明确写JSON number，对应PG JSON，不转文本或整数。

manifest格式版本为3。场景的 `database_columns`（部分按主键嵌套）及 `database_before` / `database_after` 只列需要检查的字段。TEXT列预期是普通文本；JSON列预期是待绑定的JSON编码文本。Python None表示SQL NULL。TEXT字段缺键和显式null在库里都为NULL，输入中的键存在性仍单独保留；JSON字段可区分两者。

```python
from mock_data import load_case

_, missing = load_case("empty_missing")
_, explicit_null = load_case("empty_json_null")
assert missing["database_columns"]["YDDH"] is None
assert explicit_null["database_columns"]["YDDH"] is None  # YDDH为TEXT
assert missing["field_present"] is False
assert explicit_null["field_present"] is True

_, numeric = load_case("numeric_integer_string")
assert numeric["database_before"]["XF"] == "3"          # JSON数字3
assert numeric["database_after"]["XF"] == '"3"'         # JSON字符串3
assert numeric["schema_change_required"] is False
```

预期是供后续数据库单测断言使用的常量，尚未执行PG写入。`recipes/source_shapes.json` 的 `document_type` 保留原声明，`current_document_type` 为当前采用类型（数字明确写JSON number），`pg_storage_type` 为数据库类型；`normal_types` / `normal_number_form` 只描述正常源响应样例，不能直接作为未来采集器的全部拒收规则。

- `MockSource`、`load_case`、`read/loads` 默认使用 `lossless=True`，把 JSON 数字读取为 `Number`，其 `.token` 保存 `3`、`3.0`、`3.00` 等原始表示。`dumps` 写回 JSON number；不要直接用 `json.dumps` 序列化 `Number`。
- 普通业务单测可显式传 `lossless=False`，得到标准 Python int/float。这个模式会丢失部分原始数字表示和精度，不能用于原始对象内容比较、数字表示变化或高精度测试；这些测试继续使用默认无损模式。
- 接口代码在读取后做成绩筛选可用 `Decimal(record["KCCJ"].token)` 派生数字；源对象和比较依据继续保留 token。`canonical` 忽略对象键顺序，表级比较先用 `row_map` 忽略记录顺序。
- `MockSource.request()` 的响应外壳 `code/total` 是 Python int；使用 `read()` 读取已保存的整个响应时，外壳数字也会变成 `Number`。只查看条数或辅助配置时可用标准 `json.load`；源值比较使用无损读取器。
- 正常快照按原 MHTML 的必填标记生成：必填字段必须出现，且不得为 JSON null、空字符串或纯空白；非必填字段仍可缺失。缺键、JSON null、空字符串、字符串 `"null"` 的区分保留在独立兼容性场景中，不能用这些场景代替符合必填规则的正常数据。
- 时间按 Asia/Shanghai 解释；日期参数等于当日零点。全量不传起止时间，增量必须两端都传且起点小于终点，采用 `[start_time, end_time)`。
- 先过滤，再按字符串主键升序排序，最后分页。超末页为空且保留筛选总数。这些边界是本地约定，不宣称上游已验证。
- `supplemental/full_row_merge` 等兼容性场景仍检查 `YDDH` 消失时整行替换和 TEXT 列写 SQL NULL；正常快照的 `YDDH` 更新保持非空。删除快照不生成虚构墓碑；空增量保留旧行。

## 数据目录和覆盖范围

| 位置 | 内容 |
| --- | --- |
| `recipes/source_shapes.json` | 全字段目录、原页面类型`document_type`、当前文档类型`current_document_type`、必填标记、首批与全量统计及证据 |
| `recipes/source_value_shapes.json` | 按源字段保留的日期格式，以及默认生成器参考的公共代码样例；代码样例不是校验白名单 |
| `snapshots/*/` | 各轮十四表原始对象及独立 manifest |
| `responses/`、`requests.json` | 全量、增量、重复、重叠、空窗口和超末页响应 |
| `changes.json` | 各表主键、操作、变化前后对象；删除说明不混入源行 |
| `expected/*/` | 完整快照的条目和主键清单，用于全量对照 |
| `expected/scenarios.json` | 75 个场景的用途、证据分类、输入与预期文件索引 |
| `supplemental/*/` | 独立小样例：输入、固定预期、说明各一个文件 |
| `relation_variants/*/` | 实测缺口的独立完整变体，以及稀疏排课小样例 |
| `invalid_responses/*/` | 主键、类型、结构、冲突和分页故障，明确预期失败原因 |
| `manifest.json` | 生成配置、数据分类、所有本次生成 JSON 的 SHA-256 |

字段配方中，`document_type`保留原页面声明，`current_document_type`记录当前本地文档类型，样本实际类型另存于统计字段。各单表文档已在对应字段内说明三者的差异和核验依据。

场景覆盖四类空值、数字类型/字面表示变化、重排、空白、ETL_TIME 单独变化、时间边界及共享时间跨页；通讯/学籍的最新版本、迟到、同时间冲突；多排课、多教师、成员去重、跨学期歧义；缺班级、缺课程、缺学籍、未匹配排课及缺 JHKCH；全量/增量未知字段重试恢复、持续有值/显式 null、已有列首次出现及邮件替身断言。

业务基准覆盖九个接口所需的源关联，并补齐所有文档必填字段，包括单位名称、排课 `SKSJ` 和班级 `SSZYH/XQH/BJRS`。班级人数随新增学籍同步更新。关系缺口变体分别记录业务就绪或关联预期；缺必填字段的变体明确标为 `document_required_valid=false`、`source_valid=false`，仅用于兼容性测试。

2026-09-20 源样本复核后已修正：学校根组织保留原始 `LSDWH="0"`，建树时将它视为终止标记；必填字段 `FGXLD` 在正常快照中始终有值，更新时由一个虚构负责人变为另一个。学生/教师生日、入学、预计毕业及建班日期使用 `YYYY-MM-DD`，教师来校/从教日期保留 `DD-MON-YY`，单位建立日期和排课日期使用 `YYYYMMDD`。两位年份的世纪解释不在源保存层进行。

原文仅声明 String、且没有样本格式证据的新补字段采用明确的虚构示例：年月字段用 `YYYY-MM`，失效日期/证件有效期用 `YYYY-MM-DD`，`SKSJ` 用 `HH:MM:SS`，默认第 1、2 节分别为 `08:20:00`、`09:00:00`。这些是测试取值，不冒充原接口规定。原 MHTML 未定义节次最大值，校验接受正整数字符串，不设 9 或 13 的上限，也不推算表外节次的时间。

默认代码值参考样本，人员、标识和名称继续使用虚构值。没有明确代码字典限制时，正常 mock 也允许虚构代码，不因样本中未出现而拒绝。同名代码不跨表强行统一，也不据其字面值推断含义。`unknown_business_code` 保留未知代码原文的示例。

`supplemental` 中同时保存合成演变与独立查询样例。稀疏排课的字段数量参考统计，具体键组合为合成选择；只需满足对应场景的结构、类型及逻辑，不以匹配真实行的键集合为验收条件。

小样例的标准答案在 `scenarios.py` 中独立写定，不调用待开发的合并或查询函数。完整快照的 `expected` 是生成结果的对照副本；`changes.json`、独立小样例及测试共同检查变化，不能把同源副本单独当作正确性证明。实际业务 API、数据库读写、日志和邮件发送仍由后续实现，并可使用这里的输入和预期编写模块单测。

## 扩量与复现

```powershell
python -m mock_data.generate --output mock_runs/scale_01 --seed 42 --students 100 --teachers 10 --courses 20 --grades 500 --schedules 80 --page-size 25
python -m mock_data.validate --root mock_runs/scale_01
```

相同配置生成相同文件字节。为保留已有样例，改变配置必须使用另一个输出目录；同配置可原地重新生成。学生/课程/成绩/排课最少 4，教师最少 2，页大小最少 1，以保留核心场景。生成器面向开发测试，将数据保存在内存；未设生产性能指标。

默认生成只读取已保存的字段和取值配方，不访问 `source_samples`。需要核对更新字段目录时，可显式执行 `python tools/build_mock_catalog.py`，它只提取接口 Markdown 和已有统计 JSON 的字段、计数、类型等元数据，再重新生成数据。取值配方中的代码样例来自已保存的全量数据公共代码统计；独立复核可执行 `python tools/audit_mock_against_sources.py`，该命令会读取真实样本，但只输出字段、格式及汇总统计。

重新生成的完整验收顺序与失败反例见[生成计划第 9 节](../MOCK_DATA_GENERATION_PLAN.md#9-重新生成与验收清单)。`python tools/audit_mock_required_fields.py --root <生成目录>` 使用标准库直接解析原 MHTML，独立检查必填标记、正常快照和分页响应；发现问题返回非零退出码。`required_but_missing` 仅记录历史样本差异，不能豁免正常 mock 的必填检查。扩量时必须同时对新目录执行该核对与 `validate --root`。

## 校验范围

`validate` 检查 274 字段目录、主键、原文必填字段存在且非空、正常字段类型和 ETL_TIME、组织树和业务基准关联、快照清单、变化说明、全量/增量分页总数与逐行内容、增量合并还原、故障样例预期错误、产物哈希。学制、总学时、容量及周次等整数值字段拒绝小数值（仍保留 `3` 与 `3.0` 的表示区别）；业务基准的上课日期必须真实有效且与星期一致。pytest 另测独立合并答案、无损数字、边界/重复/空页、版本归并、重试通知、查询关系及不同配置的确定性。

校验以文档结构、字段类型和场景逻辑为准，不比较具体值、字段组合或取值分布是否与真实样本一致。日期格式属于接口兼容要求，须按字段保持：学生/教师生日、学籍入学及预计毕业日期、班级建班日期使用 `YYYY-MM-DD`；教师来校/从教日期使用 `DD-MON-YY`；单位建立日期和排课日期使用 `YYYYMMDD`；ETL_TIME 使用 `YYYY-MM-DD HH:MM:SS`。具体日期可以虚构，但格式和日历日期必须正确。例如入学日期 `2026-09-03` 合法，`202609` 和 `2026-02-30` 不合法。可选日期字段仍允许缺失。

已实测字段的覆盖不是每份合法 mock 必须达到的样本覆盖门槛。必填字段则无论是否在样本中出现，都必须有非空值；允许文档内未观测字段及新的可选字段组合，继续拒绝文档外未知字段或类型错误。根组织的 `LSDWH="0"`、有效日期、主键及关联规则继续保留。

当前验收流程及本次重生成结果见[生成方案第 8、9 节](../MOCK_DATA_GENERATION_PLAN.md#8-当前实现状态2026-09-20)，独立必填核对见[最新报告](../audit_reports/mock_required_fields_latest.json)。此前修改经过保留在[必填规则修正记录](../audit_reports/mock_required_fields_20260920.md)。测试通过仅表示所覆盖的生成规则和工具检查通过，不表示尚未开发的后端模块已通过验收。

## 业务接口正常数据验收

新增五接口正常路径必须直接加载 `snapshots/initial` 的十四表，使用原始记录、日期格式和关联。入口为 `internal/query/handlers/source_mock_test.go`（真实 Pipeline）和 `tests/integration/phase2_source_mock_test.go`（真实 PostgreSQL/Redis/HTTP），预期输出按接口文档固定在 `internal/testkit/querymock`。I07 显式执行全部五个 `TestI07_OriginalMock*`。

不要通过改写正常 Mock、清空表或叠加另一份手写夹具来使正常路径通过。小型手写夹具用于边界/异常测试；`phase2EdgeRuntime` 是此类补充入口，不代表正常 Mock 验收。改变生成器或接口时，需复核这些正常路径的字段、分页和 8→9 人数对应，不能仅验证 HTTP 200。测试执行遵守当前开发计划的环境约定。
