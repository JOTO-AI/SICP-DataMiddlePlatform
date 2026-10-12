# 五接口联调与同版本名单同步

正式入口均为 POST，`Content-Type: application/json`、`X-API-Key: <QUERY_API_KEY>`。合成首请求、续页、空结果、参数错误共 20 组完整请求/响应见 [phase2_queries.json](examples/phase2_queries.json)。尖括号值均为占位符；快照 token 必须来自真实成功响应，不能直接使用示例占位符。

| 接口 | 路径（前缀 /api/v1/sicp） | 最小请求 | 默认 page_size |
| --- | --- | --- | --- |
| 1 组织 | /organizations/query | `{}` | 1000 |
| 2 学生 | /students/query | `{}` | 1000 |
| 3 教职工 | /staff/query | `{}` | 1000 |
| 8 教师教学班 | /staff/teaching-classes/query | `{"staff_number":"T001","academic_year":"2026-2027","semester":"1"}` | 100 |
| 9 班级成员 | /teaching-classes/students/query | `{"teaching_class_number":"J001","planned_course_code":"C001","academic_year":"2026-2027","semester":"1"}` | 100 |

响应固定 `code/message/request_id/data`，成功 `code=ok`。完整字段、筛选与 null 规则见 [字段矩阵](P16_API_FIELD_MATRIX.md)。不传状态包含全部状态；缺关系档案允许保留编号和 null，但学生基本/学籍集合不一致会使人员查询失败。组织空结果要求源组织图本身合法。班级成员空结果只用于“组合存在但零人”，组合不存在为 400，跨学期不可判定为 409。

同一个查询首请求保存 `X-Query-Snapshot` 和 `X-SICP-Combination`；续页只改变 page，并携带原 token。保持 page_size、筛选、operation 不变。成功续页也必须核对组合和 total。过期/不可读快照及条件不符为 409，放弃这一轮，重新从第一页开始。不要把任意 HTTP 200 当成功，仍检查 `code=ok`、完整 data 和响应头。401/403 为鉴权，422 为资源上限，503 为依赖不可用，504 为超时，非法源值/完整性错误为 500；所有失败 data=null。

接口 8 的 token 绑定 operation，**不能用于接口 9**。先收齐教师班级所有页，再逐班以独立首请求取得接口 9 token；全部接口 9 页的组合必须等于接口 8 的组合，各班唯一学号数量必须等于 student_count。版本变化不能混合后应用，应整轮重来；任一缺页、重复身份、计数不符或失败都不应用本轮数据。

标准库脚本提供上述流程，默认冲突重试最多 2 轮、可设 1—3 轮；非冲突失败立即结束。默认整轮最多 100000 条班级与成员对象，页大小 100（最大 1000）。凭据只读环境变量，不放命令行；禁止重定向转发 key，使用正常 TLS 证书校验，可指定 --ca-file。

```bash
# QUERY_API_KEY 由当前安全环境注入
python3 scripts/sync_teaching_classes.py \
  --base-url https://sicp.example.invalid \
  --staff-number T001 --academic-year 2026-2027 --semester 1 \
  --output /secure/path/complete-roster.json
```

脚本在全部校验完成后原子替换指定 JSON 文件；失败保持旧文件，输出不包含 token/key。输出包含人员数据，应保存到调用方管理的目录。业务系统可调用 synchronize 并传入自己的事务写入函数：此函数只在完整一致的一轮后调用一次，写入失败不自动重试。脚本不直接修改上游服务或第三方系统。

测试代码：Go `TestT25_01DeliveryExamplesParseAndKeepFullResponseFields`、Python `test_phase2_sync.py`，覆盖三页班级和三页名单、operation 独立 token、最后一页失败零应用、有限重试、原子文件失败保护；已接入 scripts/check.sh，当前未执行。
