# 运维后端接入包

状态：后端接入材料；测试未执行，前端重构延后。示例只含合成数据；响应中的令牌、ID、时间须使用服务器实际返回值。可解析的请求与响应样例见 [operations_http_examples.json](verification/operations_http_examples.json)，T35-04 将其与实际 Go DTO、解析器及路由核对。目录内九业务接口的完整字段和成功/失败样例以 `GET /admin/v1/apis` 返回的 `request/response/request_example/success_example/failure_example` 为准，目录版本 `2026-10-13.v2`。

## 会话、权限和通用行为

一个 `ADMIN_USERNAME/ADMIN_PASSWORD` 登录后可以使用全部已实现的管理功能。它是应用管理员，不是数据库超级用户；数据库权限由迁移自动装配，无需再配置日志管理 URL、密码或令牌。业务 `X-API-Key` 不能登录或访问管理入口。

```http
POST /console-api/login
Content-Type: application/json
X-SICP-Request: console

{"username":"synthetic-admin","password":"synthetic-password-only"}
```

成功响应 `{"code":"ok","message":"","data":{"operator":"synthetic-admin","csrf":"<csrf>"}}`，并设置 HttpOnly/SameSite=Strict 的 `sicp_session` cookie。`GET /console-api/session` 返回同样的主体和 CSRF；`POST /console-api/logout` 返回 `data:null`，注销会话。会话在 worker 重启或凭据更换后失效，最长 8 小时。所有后续请求携带 cookie；所有写请求，包括业务查询 POST，携带 `Content-Type: application/json`、`X-SICP-Request: console`、`X-SICP-CSRF: <csrf>`。登录尚无 CSRF，不需该头。禁止跨站写入。

管理 JSON 外壳固定 `code/message/data`；失败 `data:null`，成功集合为空时 `items:[]`。请求标识取 `X-Request-ID`；不要把日志中已脱敏的关联 ID 当原始控制 task ID。时间为 RFC3339，日志输入最多微秒、带时区，输出 UTC；未知时间为 `null`。日志 sequence/high_water/floor、revision 和标注为 string 的计数字段用十进制字符串，不能转 JS Number 后再回传。

| HTTP / code | 接入处理 |
|---|---|
| 400 `invalid_argument` | 修正字段、重复/未知 query 参数或损坏游标；不自动无限重试 |
| 401 `unauthenticated` / 登录 `invalid_credentials` | 重新登录 |
| 403 `forbidden` | 会话写入 CSRF/来源不符或作用域不符 |
| 404 `not_found` | 对象不存在；本人作业查询也隐藏其他主体对象 |
| 409 `cursor_expired` | 日志签名过期、epoch/floor 变化，重查历史 |
| 409 `revision_conflict` | 新管理设置/作业载荷或前置条件冲突，重新读取；不要一律当成业务快照失效 |
| 413 `request_too_large` | 新管理 JSON 超过 64 KiB，减少请求；登录另限 4 KiB |
| 429 `resource_exhausted` / 登录 `rate_limited` | 退避；日志和登录按返回的 Retry-After 等待，其他路由不保证该头 |
| 502 `proxy_unavailable` | 日志内部代理失败；业务代理使用 `unavailable` |
| 503 `unavailable` | 依赖/迁移/权限未就绪，不显示空成功 |
| 504 `deadline_exceeded` | 日志执行超时，可退避重查 |

客户端取消 GET/流读取会释放等待资源。已返回 202 的异步作业不会因断开撤销；没有取消端点。网络未拿到 202 时仍可能已提交，必须用同键同载荷重试并查原作业。新增异步入口把 `idempotency_key` 放 JSON，同主体/动作/键、同规范化载荷返回原作业；不同载荷 409。保留至少 7 天且不早于终态，见 `idempotency_expires_at`。GET 作业使用 POST 返回的 `Location`；queued/running 继续轮询，终态再按 summary 判定，202 不代表执行成功。

## 逐端点请求与响应

下表路径中的 ID 均为前一个响应所得；JSON 文件提供每行可解析请求及响应数据样例。复杂响应明确标为节选，不能将节选字段当完整响应。除表中列出的 query 外不要增加参数。

| 方法与路径 | 请求样例 / 响应关键字段 |
|---|---|
| GET `/admin/v1/logs/platform/events?mode=live&page_size=50` | `data={items,filter,source,epoch,high_water,retained_through,next_cursor,stream_cursor,expires_at}`；两源分别查询 |
| GET `/admin/v1/logs/upstream/events?mode=history&from=2026-10-12T00:00:00Z&to=2026-10-13T00:00:00Z` | history 不提供 stream_cursor；from 含、to 不含，最大 7 天 |
| GET `/admin/v1/logs/{source}/events/{event_id}` | 64 位十六进制 event_id；返回概要及 entity_id/metadata/response/redaction；没有原始报文 |
| GET `/admin/v1/logs/{source}/stream?cursor=<stream_cursor>` | SSE，详见下节；source 为 platform/upstream |
| GET `/admin/v1/update-events?page_size=50` | `{items:[{id,task_id,mode,status,predecessor_id,accepted_at,started_at,ready_at,published_at,checked_at,finished_at}],next_cursor}` |
| GET `/admin/v1/update-events/{id}` | 与 task 详情相同；控制事实与诊断可用性分别显示 |
| GET `/admin/v1/update-events/{id}/operations?page_size=50` | `{items,next_cursor,parent_operation_id}`；根 parent 为空字符串；展开时加 `parent_operation_id=publication` 或实际返回 ID |
| GET `/admin/v1/sources` | `{items:[{id,primary_key,confirmed_until,checked_at,task_id,log_task_id,window,window_task_id,last_failure,failure_state}]}`，固定十四源 |
| GET `/admin/v1/overview` | `{observed_at,tasks,slots,active,active_state,actions}`；动作状态仅供提示，提交仍重新校验 |
| GET `/admin/v1/apis` | `{version,items}`，九业务接口加十四源接口，共 23 项 |
| GET `/admin/v1/apis/dwd_bzkscjzlb` | 返回该目录项及请求/响应字段、合成样例；未知项 404 |
| POST `/admin/v1/api-tests` | `{"api_id":"dwd_bzkscjzlb","version":"2026-10-13.v2","page":1,"page_size":10,"idempotency_key":"synthetic-test-001"}` → 202 source_test 作业 |
| GET `/admin/v1/api-tests/{id}` | `{job,result_state,result}`；结果失效时 result_state=result_unavailable、result=null |
| GET `/admin/v1/settings/notifications` | `{settings,channel_state,channel_diagnostics}`；未观测通道 last_result=not_observed、observed_at=null |
| PUT `/admin/v1/settings/notifications` | `{"revision":"1","enabled":false,"recipients":[],"rules":{"source_schema":true,"task_failed":false,"source_test_failed":false}}` → 新 revision 设置；保存不发信 |
| POST `/admin/v1/notifications/test` | `{"revision":"2","idempotency_key":"synthetic-mail-001"}` → 202 notification_test 作业；仅当前已保存地址，须通道/配置允许 |
| GET `/admin/v1/notifications/tests/{id}` | notification_test 作业；与发送投递记录分别查询 |
| GET `/admin/v1/notifications/decisions?page_size=50` | `{items:[{id,code,decision,revision,occurred_at,deliveries,accepted,pending,unknown}],next_cursor}`；关闭时的事实仍可查 |
| GET `/admin/v1/notifications?page_size=50` | `{items:[delivery],next_cursor}`；逐收件人投递 |
| GET `/admin/v1/notifications/{id}` | `{delivery,attempts:[{number,started_at,finished_at,outcome}]}`；最多 16 条历史尝试 |
| GET `/admin/v1/system/status` | 双源配置/探测、缓冲、存储、保留设置、runtime_fields/deployment_fields；unknown/null 不能画成健康/零占用 |
| GET `/admin/v1/settings/retention` | `{"revision":"1","platform_days":30,"upstream_days":30}` |
| PUT `/admin/v1/settings/retention` | 同上，天数 1—3650；成功 revision 增加，失败不生效 |
| POST `/admin/v1/maintenance/cleanup/preview` | `{"source":"platform"}` → `{id,source,revision,epoch,floor,through,count,cutoff,expires_at,protection}`；upstream 另做预览 |
| POST `/admin/v1/maintenance/cleanup` | `{"preview_id":"<id>","revision":"1","idempotency_key":"synthetic-cleanup-001"}` → 202 cleanup 作业 |
| GET `/admin/v1/maintenance/cleanup/{id}` | cleanup 作业及实际 summary；无法确认提交时保持待核对，不显示 deleted=0 |

新增列表默认 50/最大 200，下一页用 `cursor=<next_cursor>`。没有 next_cursor 即结束；操作子树续页须同时保留原 parent_operation_id。游标最长 4 KiB、24 小时，绑定接口/主体/筛选，不能跨列表复用。日志首次固定高水位 H，后续历史页不包含 H 之后新增的记录；省略筛选条件继承原窗口。日志可选筛选为 mode/from/to/kind/phase/outcome/operation/task_id/request_id/test_run_id/id_format/page_size/cursor；原始 ID 默认 raw，拿到 log_task_id 时用 id_format=correlation，避免二次哈希。

源测试限定目录中的 source 项，仅一页，page 1—1000000/page_size 1—100；不接受 URL、header、环境、operator 或凭据覆盖，不推进任务、业务槽位或源进度。总期限 30 秒，持久队列上限 20，受共享采集限流和冷却约束。原始合成结果只在执行实例内存最多保留 10 分钟/20 MiB/1000 条；重启、淘汰、跨实例或过期 result_unavailable，不能当空响应成功，也不能自动重新执行。

通知 enabled=false 或清空 recipients 关闭投递；仅关闭页面不会关闭通知。规则为 source_schema/task_failed/source_test_failed，关闭时仍记事实。queued/retry/sending 为处理中，accepted 表示 SMTP 接受（不是送达）；rejected/unknown/cancelled/exhausted 为相应终态。每人最多 3 次、入队后 24 小时、退避 1/5 分钟；unknown 不重发。关闭/移除地址取消尚未开始的投递，正在发送的按实际结果记录。

清理仅删除满足保留期且安全退休的连续前缀，按 received_at 判断；最多预览 1000 条，10 分钟过期。request/task/step/call/attempt/audit 均在对象白名单内，但在用任务、槽位、读者/快照、资源、恢复/补投、通知仍保护。策略变化需新预览。两逻辑库各自作业，不是跨库原子删除。控制退休栅栏可能先于日志删除持久化，不得手工清空栅栏复活引用；日志永久 receipts 不删除。

## SSE 衔接与取消

先读 live 首次历史页，保留 stream_cursor，同时可用 next_cursor 翻旧页。live 的 from 首次固定，to=null；晚到但 occurred_at 早于 from 的记录不匹配。history 模式只看固定区间。

```http
GET /admin/v1/logs/platform/stream?cursor=<stream_cursor>
Cookie: sicp_session=<session>
Accept: text/event-stream
Last-Event-ID: <last_event_or_checkpoint_token>
```

首次连接省略 Last-Event-ID；重连时它优先于 URL cursor，非空但损坏时拒绝，不回退。客户端用可携带 header 的流式 fetch，取消 AbortController 或退出登录后关闭读取。收到完整 event/checkpoint 帧才保存其 id；断在半帧时用上一个完整 id 重连。

```text
event: event
id: <event_token>
data: {"source":"platform","epoch":"<epoch>","event_id":"<64-hex>","sequence":"21",...}

event: checkpoint
id: <checkpoint_token>
data: {"sequence":"22"}

: heartbeat

event: control
data: {"code":"connection_expired","retryable":true,"rebuild_required":false}

```

上方 event data 为字段节选，完整可解析 event 在 JSON 文件中。event 与历史 items 同 DTO，按 source/epoch/event_id 去重；checkpoint 即使没有匹配事件也推进扫描位置；心跳和 control 不推进。rebuild_required=true 必须重新历史查询，false 可按最后 id 退避续传。会话每 15 秒复核、心跳 15 秒、写超时 5 秒，连接 4 分 55 秒尝试发 connection_expired 并在 5 分钟内结束；断流不保证一定拿到 control。每实例 16 流/8 读取、每源 2 扫描，每批 200 条/2 MiB；这是保护预算，实测容量尚未验收。

## 复用已有任务和九业务接口

任务入口仍用 **header** `Idempotency-Key: synthetic-task-001` 和空 JSON `{}`，不是新增作业的 JSON idempotency_key。POST `/admin/v1/tasks/incremental`、`/admin/v1/tasks/force-full`、`/admin/v1/tables/{table}/force-full`、`/admin/v1/tasks/{task_id}/rerun` 返回 202 和 `{task_id,status,accepted_at,created}`；Location 为 `/admin/v1/tasks/{task_id}`。GET 查询该位置或 update-events 同 ID。首次初始化自动执行，无手动初始化 API。旧错误码保留 busy/initialize_required/not_failed/idempotency_conflict；成功未变化不冒充发布。

九接口均 POST `/console-api/query` 加目录提供的 path。示例请求如下；响应使用目录中每接口的 success_example/failure_example；当前本地业务 transport 的 code 是字符串（成功为 "ok"），与上游源响应的数值 code 分开解释，不做全局转换。此处已按 internal/query/transport/security.go 修正历史描述。

| 业务 path（省略 `/api/v1/sicp`） | 合成 JSON 请求 |
|---|---|
| `/students/grades/query` | `{"student_number":"S001","academic_year":"all"}` |
| `/students/grades/batch-query` | `{"student_numbers":["S001"],"academic_year":"all"}` |
| `/students/timetable/query` | `{"student_number":"S001","academic_year":"2026-2027","semester":"1"}` |
| `/staff/timetable/query` | `{"staff_number":"T001","academic_year":"2026-2027","semester":"1"}` |
| `/organizations/query` | `{}` |
| `/students/query` | `{"student_number":"S001"}` |
| `/staff/query` | `{"staff_number":"T001"}` |
| `/staff/teaching-classes/query` | `{"staff_number":"T001","academic_year":"2026-2027","semester":"1","page":1,"page_size":100}` |
| `/teaching-classes/students/query` | `{"teaching_class_number":"J001","planned_course_code":"C001","academic_year":"2026-2027","semester":"1","page":1,"page_size":100}` |

七个分页接口续页带自身首次响应的 `X-Query-Snapshot`，两个课表无分页。教学班列表与成员查询各用自己的 token，不可互换；每页比对同一 `X-SICP-Combination`，整轮去重成员数与班级人数一致后再展示为完整。不一致/快照过期须整轮重查，不能拼接版本。业务错误、空值、字段和时间还遵守 [业务接口设计](../sicp-api-design.md)。

## 部署、升级和证据

标准配置仍使用根 `.env.example`/`compose.yaml`，同一 db-log 实例内 logs/upstream_logs 两个逻辑库；沿用独立 writer 密码，管理读写自动复用 query/worker 服务身份与现有 SNAPSHOT_KEY。完整停写、登记、迁移、排空和恢复边界见 [升级与回退手册](OPERATIONS_UPGRADE_RUNBOOK.md)。

冻结容量在 [operations_capacity_baseline.json](verification/operations_capacity_baseline.json)，32 项场景在 [OI_CASE_MAP](verification/OI_CASE_MAP.md)。测试执行仍暂停；没有实测通过数。恢复执行前须同 SHA 完成 T27—T35，通过后在 Linux Actions 调用验收运行器，报告模板见 [验收报告](PHASE2_OPERATIONS_ACCEPTANCE.md)。后端开发完成不代表整个 Phase 2 完成，前端重构与页面验收仍单独待办。
