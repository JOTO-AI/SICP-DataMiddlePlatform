# F00-02 · 前端端点接入基线

核对日期：2026-10-14。代码基线 `b83ac1d`（F00-01 文档提交 `283dd68` 未改后端）。以下按当前路由、DTO、解析器与既有接入包核对；没有执行后端 T/OI，也不将接口存在等同于环境已就绪。

## 协议与错误域

- 除登录外均使用同源 HttpOnly `sicp_session`；POST/PUT 带 `X-SICP-Request: console`、`X-SICP-CSRF`、JSON Content-Type。浏览器不接触上游凭据或业务 API key。认证实现为 `internal/admin/session.go`。
- 管理返回 `{code:string,message,data}`；业务返回 `{code:string,message,request_id,data}`，成功均为 `ok`，管理异步受理为 `accepted`。上游响应中的 code 保留其数值语义。接入包“本地业务数值 code”属于已确认文档错误，以 `internal/query/transport/security.go` 为准。
- 响应头按实际值保留 `X-Request-ID`、`X-Query-Snapshot`、`X-SICP-Combination`、`Location`、`Retry-After`；任何头都不以浏览器时间或随机值补造。
- 全部端点可能返回认证 401/403 或网关非 JSON 错误。下表错误域列出领域行为，不把所有 409、429、503 归为同一可重试错误。

| 错误域 | 代码与处理 | 代码依据 |
| --- | --- | --- |
| A 认证 | 400 invalid_argument；401 invalid_credentials/unauthenticated；403 forbidden；429 rate_limited（Retry-After）；503 unavailable | admin/session.go |
| Q 业务 | 400 invalid_argument；401/403；409 conflict（业务快照）；422 resource_exhausted；503 not_ready/dependency_failure；504 deadline_exceeded；500 internal；代理 502 unavailable | query/transport/security.go、admin/query_proxy.go |
| T 旧任务 | 400 invalid_argument；404 not_found；409 busy/initialize_required/already_initialized/not_failed/idempotency_conflict；503 unavailable。受理不明保留原键与载荷 | admin/admission.go、status.go |
| O 运维 | 400 invalid_argument；403 forbidden；404 not_found；409 revision_conflict；429 resource_exhausted；503 unavailable；JSON 解码可返回 413 request_too_large | admin/operations.go、logquery/dto.go |
| L 日志 | 400 invalid_argument；403 forbidden；404 not_found；409 cursor_expired；413 request_too_large；429 resource_exhausted；502 proxy_unavailable；503 unavailable；504 deadline_exceeded | admin/logs.go、logquery/contract.go |
| H 健康 | JSON 健康对象，无 code 外壳；503 表示未就绪，仍读取实际状态；查询健康代理另可能 502 unavailable | application/application.go、admin/query_proxy.go |

## 会话、健康与任务

下表所有路径均为浏览器同源路径。`无`表示不发送请求体；任务列表末尾 `/` 必须保留。响应列为字段摘要，完整 DTO 保持原文件为唯一字段定义。

| 方法 | 路径 | 请求 | 响应/分页 | 可用条件；错误域 |
| --- | --- | --- | --- | --- |
| POST | /console-api/login | username,password，最多 4 KiB；无需 CSRF | operator,csrf；设置会话 cookie | 配置了管理员账号；A |
| GET | /console-api/session | 无 | operator,csrf | 有效会话；A |
| POST | /console-api/logout | 无；会话与 CSRF | data:null | 有效会话；A |
| GET | /console-api/query/livez | 无 | live | 已登录，查询服务可达；H |
| GET | /console-api/query/readyz | 无 | ready 与实际依赖状态 | 已登录，业务就绪与进程存活分开；H |
| GET | /console-api/worker/livez | 无 | live | 已登录，worker 可达；H |
| GET | /console-api/worker/readyz | 无 | ready=sync_ready 与其他实际状态 | 已登录；H |
| GET | /admin/v1/tasks/ | 无 | TaskOverview：最近 50 条、初始化与调度状态；不分页 | 控制存储可读；T |
| GET | /admin/v1/tasks/{task_id} | 无 | TaskView：状态、六类时间、confirmed_until、counts、诊断、acquisition、work_progress | 原始 task_id，不用脱敏日志 ID；T |
| POST | /admin/v1/tasks/incremental | {}；header Idempotency-Key | 202 Receipt：task_id,status,accepted_at,created；Location | 初始化完成，提交时复核忙碌与恢复状态；T |
| POST | /admin/v1/tasks/force-full | {}；header Idempotency-Key | 同上 | 已初始化且无冲突动作；T |
| POST | /admin/v1/tables/{table}/force-full | {}；header Idempotency-Key | 同上 | 固定十四表之一，服务端复核允许条件；T |
| POST | /admin/v1/tasks/{task_id}/rerun | {}；header Idempotency-Key | 新任务 Receipt，关联原任务 | 原任务失败，服务端复核；T |

依据：`internal/application/application.go`、`internal/admin/routes.go`、`admission.go`、`task_list.go`、`status.go`。无手动首次初始化、取消、任意恢复或回滚端点。

## 九个业务查询

全部为 POST；下表完整路径前统一添加 `/console-api/query`。禁止 URL Query 参数；Body 最大 64 KiB。完整字段/空值使用 `internal/query/contract/dto.go`、`phase2_dto.go`，合法性使用 `internal/query/transport/parse.go` 及业务验证器；不是只按下面的必填摘要构造表单。九接口均属错误域 Q，均须查询服务已就绪。

| 方法 | 路径 | 请求 DTO / 必填摘要 | 响应 DTO | 分页 |
| --- | --- | --- | --- | --- |
| POST | /api/v1/sicp/students/grades/query | StudentGradesRequest；student_number；学年可省略或 all，GradeFilters | GradesResponse | 快照；默认 page_size=100 |
| POST | /api/v1/sicp/students/grades/batch-query | BatchGradesRequest；academic_year；学生数组/院系/行政班及 GradeFilters | GradesResponse | 快照；默认 100；total 为成绩记录数 |
| POST | /api/v1/sicp/students/timetable/query | StudentTimetableRequest；student_number,academic_year,semester | StudentTimetableResponse | 无，不传 page/page_size/快照 |
| POST | /api/v1/sicp/staff/timetable/query | StaffTimetableRequest；staff_number,academic_year,semester | StaffTimetableResponse | 无，不传 page/page_size/快照 |
| POST | /api/v1/sicp/organizations/query | OrganizationsRequest；筛选可全省略 | OrganizationsResponse | 快照；默认 1000 |
| POST | /api/v1/sicp/students/query | StudentsRequest；筛选可全省略 | StudentsResponse | 快照；默认 1000 |
| POST | /api/v1/sicp/staff/query | StaffRequest；筛选可全省略 | StaffResponse | 快照；默认 1000 |
| POST | /api/v1/sicp/staff/teaching-classes/query | StaffTeachingClassesRequest；staff_number,academic_year,semester | StaffTeachingClassesResponse | 快照；默认 100 |
| POST | /api/v1/sicp/teaching-classes/students/query | TeachingClassStudentsRequest；teaching_class_number,planned_course_code,academic_year,semester | TeachingClassStudentsResponse | 快照；默认 100 |

七个分页接口 page 默认 1、最大 1000000；page_size 1 至 1000。前端当前显式 20 不改变服务端默认值。续页使用本接口自己的快照；跨接口教学班/名单只比较数据组合，不共用快照。所有 ID 保留字符串和前导零；空数组不是 null；0/false 不作为缺失。注意 `Organization.is_active` 当前为 bool，只有嵌套 `Department.is_active` 为可空 bool；计划的泛化“三态”需在 F00-03 单独登记。

## 运维、目录和源测试

以下均为错误域 O（日志另见下节），身份仍遵守 A。游标列表默认 50、最大 200、有效期 24 小时，无总页数；续页绑定主体、范围和页大小。仅发送各行明确列出的参数；更新列表已在 F07-02 增补服务端筛选。响应完整定义位于 `internal/operations/`。

| 方法 | 路径 | 请求 | 响应/分页 | 可用条件 |
| --- | --- | --- | --- | --- |
| GET | /admin/v1/update-events | cursor,page_size,from,to,mode,status,task_id | EventPage：items,next_cursor,filter | 控制数据库可读；按 accepted_at 筛选，默认最近 24 小时，自定义最多 7 天，左闭右开；续页继承签名游标条件，禁止改条件；旧无筛选游标需重查 |
| GET | /admin/v1/update-events/{id} | 无 | TaskView | 原始事件/任务 ID 有效 |
| GET | /admin/v1/update-events/{id}/operations | cursor,page_size,parent_operation_id | items,next_cursor,parent_operation_id | 根节点省略 parent 参数，续页保留原 parent |
| GET | /admin/v1/sources | 无 | items：十四源主键、检查/确认时间、window、last_failure、failure_state、关联 ID | 控制数据库可读 |
| GET | /admin/v1/overview | 无 | observed_at,tasks,slots,active,active_state,actions | 5 秒读取预算；active_state 可为 empty/unavailable/slot_unavailable/available |
| GET | /admin/v1/apis | 无 | version,items：九业务十四源 | 目录代码已实现；readiness 均 unknown |
| GET | /admin/v1/apis/{id} | 无 | API：request/response Field、示例、notes 等 | ID 来自目录，缺项 404 |
| POST | /admin/v1/api-tests | api_id,version,page,page_size,idempotency_key | 202 Job + Location | 仅 source 项与当前目录版本；受共享限流/冷却/队列预算约束 |
| GET | /admin/v1/api-tests/{id} | 无 | job,result_state,result | 本主体 source_test 作业；available/result_unavailable 不混淆 |
| GET | /admin/v1/notifications/decisions | cursor,page_size | items,next_cursor；通知决策与聚合 | 通知仓储可用，关闭通知仍有决策事实 |
| GET | /admin/v1/notifications | cursor,page_size | items,next_cursor；逐人 Delivery | 同上 |
| GET | /admin/v1/notifications/{id} | 无 | delivery,attempts | 有效投递 ID |
| GET | /admin/v1/settings/notifications | 无 | settings,channel_state,channel_diagnostics | 配置状态和最近实际观察分开 |
| PUT | /admin/v1/settings/notifications | revision 字符串、enabled、recipients、rules | 保存后的 NotificationSettings | revision 匹配；最多 20 地址、3 类规则；启用需通道 |
| POST | /admin/v1/notifications/test | revision 字符串、idempotency_key | 202 Job + Location | 仅当前已保存且允许的地址；不接受临时收件人 |
| GET | /admin/v1/notifications/tests/{id} | 无 | Job | 本主体 notification_test 作业 |
| GET | /admin/v1/system/status | 无 | 依赖/双源/缓冲/存储、retention_state、runtime_fields、deployment_fields | 5 秒读取预算；局部不可用保持原状态 |
| GET | /admin/v1/settings/retention | 无 | revision 字符串、platform_days、upstream_days | 保留设置可读 |
| PUT | /admin/v1/settings/retention | 同上 | 保存后的 Retention | revision 匹配；天数 1 至 3650 |
| POST | /admin/v1/maintenance/cleanup/preview | source：platform 或 upstream | CleanupPreview：id,source,revision,epoch,floor,through,count,cutoff,expires_at,protection | 每次一个源；最多 1000 条连续可退休前缀 |
| POST | /admin/v1/maintenance/cleanup | preview_id,revision 字符串,idempotency_key | 202 Job + Location | 10 分钟内同主体有效预览，策略与保护条件重新核对 |
| GET | /admin/v1/maintenance/cleanup/{id} | 无 | Job，summary 为实际结果 | 本主体 cleanup 作业；待核对不填 deleted=0 |

`Job` 完整字段：id,action,status,accepted_at,deadline,idempotency_expires_at,started_at,finished_at,summary；其定义在 `jobs.go`。subject/request/owner 不通过此 DTO 返回。新异步请求使用 JSON 幂等键，旧任务使用 header；受理不明重用原键/原载荷/Location，没有通用自动重试副作用的许可。

源 API 的 GET 路径是文档中的固定上游目标，不是浏览器直连入口。源测试 page 默认 1（1 至 1000000），page_size 默认 10（1 至 100）；不接受任意 URL/header/startTime/endTime/身份覆盖，最多一页、2 MiB、30 秒。结果缓存最多 10 分钟、总计 20 MiB/1000 条，缺失时不自动重测。字段基线为 `internal/storage/source/catalog.go` 与独立 `internal/operations/testdata/reviewed_catalog.json`（14 表 274 字段），固定路径前缀为 `/apizt/openapi/invoke/sjzc/release/`。

| 源目录 ID（路径后缀） | 主键 |
| --- | --- |
| dwd_bzksbjsjzlb | BH |
| dwd_bzkscjzlb | ID |
| dwd_bzksgrtxsjzlb | XH |
| dwd_bzksjbsjzlb | XH |
| dwd_bzkskbsjzlb | ID |
| dwd_bzkskcsjzlb | KCH |
| dwd_bzksnjzysjzlb | NJZYDM |
| dwd_bzkspksjzlb | PKH |
| dwd_bzksxjjbsjzlb | XH |
| dwd_bzkszyxxsjzlb | ZYH |
| dwd_grtxfszlb | JGH |
| dwd_jzgjcsjzlb | JGH |
| dwd_xqjbsjzlb | XQH |
| dwd_yxsdwjbsjzlb | DWH |

## 双源日志与 SSE

错误域 L。source 仅 platform/upstream；同 db-log 实例内两逻辑库。完整 DTO 在 `internal/logquery/dto.go`，参数解析与游标约束在 `contract.go`、`cursor.go`。

| 方法 | 路径 | 请求 | 响应/分页 | 可用条件 |
| --- | --- | --- | --- | --- |
| GET | /admin/v1/logs/{source}/events | mode/from/to/kind/phase/outcome/operation/task_id/request_id/test_run_id/id_format/page_size/cursor | items,filter,source,epoch,high_water,retained_through,next_cursor,stream_cursor,expires_at | 已配置签名与日志代理，对应源可读；默认 50 最大 200 |
| GET | /admin/v1/logs/{source}/events/{event_id} | 无 | Detail：EventItem + entity_id,metadata,response,redaction | 64 位小写十六进制 event_id，按源定位 |
| GET | /admin/v1/logs/{source}/stream | cursor；重连 Last-Event-ID 优先 | text/event-stream：event/checkpoint/control/heartbeat | live 首批响应的 stream_cursor，不能用 next_cursor |

history 时间范围 `[from,to)` 最多 7 天，固定高水位；live 的 to=null。时间输入带时区、最多微秒。已脱敏关联 ID 必须使用 id_format=correlation，不能当 raw 再次哈希。sequence/high_water/floor/retained_through、duration_ns 和容量等声明为字符串的字段原样保留。

只在完整 event/checkpoint 帧后更新恢复 token；半帧、心跳和 control 不推进。会话每 15 秒复核，连接到期/断线退避重连；cursor_expired 或 rebuild_required 重新获取历史并提示缺口。完整事件按 source/epoch/event_id 去重，不能只按 event_id。

## 样例及核对范围

现有可解析管理样例：`development_docs/verification/operations_http_examples.json`。九业务序列化夹具：`internal/query/contract/testdata/`；正常业务验收数据：`mock_data/snapshots/initial` 与 `internal/testkit/querymock`。本步骤不生成新的人员/查询结果，也未发送更新、测试邮件或清理请求。

运行 `node frontend/scripts/check-endpoint-baseline.mjs` 检查本文对实际显式路由、九业务路径、十四源的覆盖；该检查只证明登记无缺项，不能代替后端执行验收或字段语义复核。
