# P29—P35 后端开发交接（进行中）

日期：2026-10-13。本轮没有执行测试、部署、迁移、真实上游请求或邮件发送，没有修改前端和 CI 工作流。编译成功不能代替测试或阶段验收。

## 当前代码

| 阶段 | 已加入的代码 | 尚缺的交付内容 |
| --- | --- | --- |
| P29 | 两库提交计数/epoch/去重标记、受限 writer、独立 reader、历史/详情、签名游标、预算、内部鉴权及会话代理 | 升级排空与权限的真实验证、完整配置和故障回归证据 |
| P30 | 持久位置扫描、按位置共享批次、逐订阅筛选、checkpoint、写截止时间、连接/读取预算、会话复核及网关关闭缓冲；退出登录/客户端取消测试源码 | 生命周期源码已补齐；尚缺真实跨实例代理验收 |
| P31 | task ID 作为事件身份、有界列表、既有详情复用、控制事实操作节点、BuildTrace 分阶段尝试树、十四源成功进度/当前窗口、槽位/活动位置和受理前置条件总览 | P31 后端开发与对应源码/复核完成；T31/OI04 未执行 |
| P32 | 23 项目录、DTO 字段元数据/可经真实解析器校验的合成示例、固定源单页测试、持久管理作业、共享采集限流/冷却及低优先级准入、短期内存结果、主体隔离 | 请求契约与受控执行/队列/结果/九代理源码已补；响应/源字段复核已完成；8→9 代理组合回归源码已完成；实际多实例场景仍归 P35.01 |
| P33 | revision 设置、规范化地址、空地址关闭、规则决策查询、逐人投递、有限重试、unknown 不重试、显式测试与查询、旧发送尝试迁移 | 旧配置/投递历史及通道观察源码已完成；多实例发送竞争/恢复源码已补齐；记录权限及装配待按序收尾；旧已尝试且未确认状态保守保留 unknown |
| P34 | 系统/保留配置、缓冲及载荷/去重回执容量、预览、幂等清理作业、事务性 floor/删除/清理回执、过期作业回执恢复、审计补投 | 全部引用登记入口与 retirement 协调、组合资源清理。当前只删除连续前缀中的无任务关联 request 日志；其他事件永久保护，不能算完整 P34 |
| P35 | 32 个 OI 用例的严格发现/计数/证据执行器、同 SHA 的 T 工件摘要核对、输出工件摘要、执行器测试源码、本接入/升级说明 | OI01—OI07 的 32 个真实场景测试尚未实现；新增的 PostgreSQL 协议回归不替代完整 OI，执行器因此会拒绝验收，报告仍为模板 |

在 `b33b417` 上收尾 P31.01 后，P30 后端总框、P31.01、P31.03、P32.03 和 P32.04 已补齐开发产物并勾选。其他新增源码/修复的完成项与剩余项已拆开，完成产物单独勾选；P31 后端总框已勾选，P32 后端总框已勾选，P33—P35 总框继续保留待办；T29—T35、OI01—OI07 均未实测。完整引用退休协调、实际 OI 场景及余下契约/组合回归尚未完成，本文件不是验收报告。

## 日志部署调整（2026-10-13）

统一使用 compose.yaml；一个 db-log PostgreSQL 服务承载 logs / upstream_logs 两个逻辑库，共用 pg-log 卷。两库保留各自的 writer/reader、迁移 ledger、提交顺序、缓冲及配额。实例故障和连接额度由两库共同承担；仍与 M/S 隔离。旧单库升级见 [P28 切换说明](P28_LOG_STORAGE_CONFIGURATION.md)。新增 upstream_logs_005 将上游维护权限分配给 sicp_upstream_maintenance，保持旧迁移 checksum。相关测试源码已调整，执行仍暂停。

## 入口与权限

所有外部入口均在 worker 的现有管理会话下；GET 逐次鉴权，写请求要求 `X-SICP-Request: console` 和会话返回的 `X-SICP-CSRF`。业务 key 不授予这些权限。内部日志路由只在 query-service 的 `/internal/admin/v1/logs/...` 下，使用独立 Bearer 凭据；网关不公开该前缀。worker 丢弃调用方凭据/主体/转发头，固定目标并从可信会话派生主体。

新增入口：

- `GET /admin/v1/logs/{platform|upstream}/events`、`.../events/{event_id}`、`.../stream`。
- `GET /admin/v1/update-events`、`.../{id}`、`.../{id}/operations`、`/admin/v1/sources`、`/admin/v1/overview`。
- `GET /admin/v1/apis`、`.../{id}`；`POST /admin/v1/api-tests`、`GET .../{id}`。
- `GET/PUT /admin/v1/settings/notifications`；`POST /admin/v1/notifications/test`；`GET /admin/v1/notifications`、`.../decisions`、`.../{id}`、`.../tests/{id}`。
- `GET /admin/v1/system/status`；`GET/PUT /admin/v1/settings/retention`；`POST /admin/v1/maintenance/cleanup/preview`、`POST .../cleanup`、`GET .../cleanup/{id}`。

继续复用九业务接口的 `/console-api/query` 代理。七个分页接口使用 `X-Query-Snapshot`，两课表无分页；教学班列表与成员查询各用独立 token，每页核对同一 X-SICP-Combination 并校验整轮人数，未建立第二套业务查询实现。

操作树的 `publication` 分组中，子节点 kind 保留 `route_shift` / `bundle_publish`，确认转移不等于发布新组合。`acquisition` 从采集 checkpoint 读取逐源观察状态，`progress` 从本任务的不可变计划区分 staged/confirmed；`cleanup` 包含实际持久化的 `maintenance/` 记录。未知起止时间仍为 null，checkpoint 观察时间不冒充边界。步骤游标支持带斜杠的持久名称，按数据库顺序续页。

操作树根节点中的 `trace` 展开为 `trace:<stage>`，再展开为稳定的步骤尝试 ID。每次尝试保留 session/owner_generation/attempt、物理副本、开始/完成边界及构建计数，不把多个重试或恢复会话相加。开始/完成未入库时对应字段为 null。日志子树读取失败返回 503；控制区任务详情与节点仍可独立读取。总览 actions 是观察时的受理前置条件，提交端仍重新校验；冷却时间单独返回，现有受理接口允许受理后等待冷却。

系统状态将配置与探测分开：buffer_bytes/buffer_files 或日志 storage 探测失败时为 null/unknown/unavailable，不填 0；日志容量包含 events 与永久 receipts 的实际关系大小。SMTP 当前仅报告是否配置，连通性保持 unknown。查询容量默认使用 worker 自动配置的维护连接；数据库权限尚未迁移时返回不可用，不会回退到 writer。

管理响应仍为 `code/message/data`，请求标识在 `X-Request-ID`。读取失败不返回半页结果。成功空集合为 `[]`，未知事实为 `null` 或明确状态。新版本号/日志序号为十进制 JSON 字符串。400/401/403/404/409/413/429/502/503/504 与日志契约一致；一般管理 revision 冲突使用 `revision_conflict`，日志失效使用 `cursor_expired`。

## 配置与升级顺序

尚未进行实际升级。必须先备份并在隔离合成环境完成权限/迁移验证；此处给出操作顺序，不自动执行。

1. 按 P28 指南登记旧目标并停止所有旧事件生产者、Dispatcher 与写事务。保持原平台目标、AUDIT_KEY、项目名和缓冲目录。旧平台 call/attempt 不搬迁。
2. 部署账号执行追加版本：平台至 `logs_008`，上游至 `upstream_logs_007`，控制区至 `control_010`。保持之前版本校验和。logs_005/upstream_logs_003 回填按 `received_at,event_id` 的确定顺序，不声称恢复旧记录的真实提交顺序。迁移后运行 log-cutover，旧缓冲使用当前 append_event 协议按原 ID/内容写回原平台库，确认排空后再启动服务。
3. 迁移后旧 INSERT writer 被权限和非空 sequence 约束拒绝。当前投递组件调用 `logs.append_event`；计数锁保持到事务结束，丢失提交应答可重放。`logs.receipts` 永久保留 event ID/规范化摘要/原序号；本轮清理不会删除它。
4. 2026-10-13 按用户要求采用单管理员授权：同一个 ADMIN_USERNAME/ADMIN_PASSWORD 登录后具有全部已实现管理操作权限，保留会话、CSRF、审计和业务数据保护。日志读取连接自动使用 query/worker 自己的业务服务凭据及对应日志库端点；logs_007/upstream_logs_006 自动将读取角色授予 sicp_query/sicp_worker，将受限清理角色仅授予 sicp_worker。内置 db-log 已有两个服务 LOGIN，无需新填账号密码。外部数据库需保证对应服务 LOGIN/组成员及密码与服务配置一致；不使用 PostgreSQL 管理员或 logger 凭据代替读取权限。
5. Compose 和 .env.example 已移除 LOG_CURSOR_KEYS、LOG_CURSOR_ACTIVE、LOG_INTERNAL_TOKEN 及四个日志 READ/MAINTENANCE URL。默认用已有 SNAPSHOT_KEY 通过带版本/用途标签的 HMAC-SHA256 分别派生游标签名密钥与内部服务令牌，默认启用日志路由，不新增手填密钥。所有副本保持 SNAPSHOT_KEY 一致；轮换它或从旧显式密钥配置切换会使旧日志游标失效，客户端重新查询。管理员密码更换仍需重建 worker 并重新登录，不把密码分发给 query。直接进程的旧显式密钥/连接配置仍兼容，但标准 Compose 不再读取它们。
6. query 只读日志；worker 通过受限函数执行清理，无直接 DELETE/TRUNCATE 权限。维护连接自动复用 worker 服务凭据，同 DSN 只建一个池；用户无需额外授予自己的管理员账号“清理权限”。数据库尚未迁移、暂不可达或清理对象受保护时仍如实返回不可用/拒绝，不表示绕过存储保护。P34 完整引用退休协调尚未完成。
7. 在停用旧 Sender 后启动新 worker。设置首次按有效旧 MAIL_TO 导入；后续启动不覆盖数据库设置。SMTP 不完整只关闭通道，不阻断同步；清空收件人关闭通知。新发送组件替代旧 Sender，不能两者同时运行。旧已投递和发送不明记录不会重新当新错误发送。

回退不能只换旧镜像：必须保留兼容提交序号协议的投递组件，否则进入停写维护；不能重新授予直接写权限绕过协议。控制区新设置/逐人投递与旧 Sender 不兼容，恢复旧 Sender 有重复发送风险，回退时保持发信关闭。已删载荷及已被 SMTP 接受的邮件不能通过代码回退撤销。恢复旧数据库备份需要独立恢复点记录，并先轮换 `logs.commit_state.epoch` 后再开放查询。

## HTTP/SSE 样例

样例中的 cookie、CSRF、游标均由已认证客户端填入，示例没有实际凭据。

```http
GET /admin/v1/logs/platform/events?mode=live&page_size=50
Cookie: sicp_session=<session>
```

读取 `data.items`，历史翻页只传 `next_cursor`，订阅传首次 `stream_cursor`；历史翻页不会改变实时边界。`mode=live` 的 from 固定，to 为 null；发生时间早于 from 的晚到事件不会匹配。history 不提供 stream_cursor。

```http
GET /admin/v1/logs/platform/stream?cursor=<stream_cursor>
Cookie: sicp_session=<session>
Accept: text/event-stream
Last-Event-ID: <last_received_event_or_checkpoint_id>
```

非空 Last-Event-ID 优先，损坏时不退回 URL 游标。按 source/epoch/event_id 去重。event 和 checkpoint 帧更新恢复位置，心跳不更新；control 帧不推进位置，`rebuild_required=true` 时重查历史。签名有效期 24 小时，不因翻页续期；连接最长 5 分钟，4 分 55 秒后尝试发送 connection_expired 控制帧，为最后写入预留 5 秒；会话每 15 秒复核，写超时 5 秒。每实例 16 连接、8 读取、每源 2 扫描；批次 200 条/2 MiB，未进行同规模容量验收。

```json
{"api_id":"dwd_bzkscjzlb","version":"2026-10-13.v1","page":1,"page_size":10,"idempotency_key":"synthetic-test-001"}
```

将上文 JSON POST 到 `/admin/v1/api-tests`。202 仅表示作业与审计已持久受理，按 Location GET 查回。禁止 URL、Header、环境或凭据覆盖；只请求一页，不推进同步进度。总期限 30 秒；结果仅执行实例内存保存最多 10 分钟、总容量 20 MiB、最多 1000 条；跨主体返回 404。到期、淘汰、重启或另一实例无结果时为 `result_unavailable`，不能视为空成功。断开不取消已受理作业。

通知设置 PUT 使用读取到的 revision，例如 `{"revision":"1","enabled":false,"recipients":[],"rules":{"source_schema":true}}`。保存不发信。显式测试只提交 `{"revision":"2","idempotency_key":"synthetic-mail-001"}`，不接受临时邮箱。投递状态区分 accepted/rejected/retry/unknown/cancelled/exhausted；accepted 不是收件箱送达。新投递最多 3 次、24 小时，退避 1 分钟/5 分钟；unknown 不自动重试。

24 小时从投递入队时间开始计算，错误发生时间独立保留。`notifications/decisions` 可分页查询关闭时未生成投递的事实、规则版本、决策和各类投递数量；不会把零投递视为没有错误。

清理先 POST `{"source":"platform"}` 到 preview，再 POST `{"preview_id":"<id>","revision":"1","idempotency_key":"synthetic-cleanup-001"}`。每源单独作业/提交，预览最多 1000 条、10 分钟有效。只按 received_at 删除连续前缀；当前遇到任意非 request 或任务/测试关联记录即停止。返回实际确认数量；提交应答不明为 partial_failed/数量 null，并保留库内清理回执。保留配置改变后必须重新预览。没有任意路径、SQL、槽位切换或文件删除接口。

## 验收入口

`scripts/operations_acceptance.py` 仅允许在 Linux GitHub Actions 执行，要求同 SHA 的后端完成、T27—T35 通过证据和预先冻结容量文件。必须发现并执行全部 32 个 OI；缺用例、skip、失败、超时、非零退出、空运行均拒绝。当前缺少这些真实场景测试，不能执行本轮验收或填写通过报告。原工作流未改动，当前测试暂停继续有效。

前置 JSON 的 `artifacts` 必须包含 T27—T35，每项有相对前置文件目录的 `path` 和 `sha256`；路径必须仍在该证据目录中，文件存在、非空且摘要一致。容量文件的 machine 为非空对象，记录数/大小/速率/并发/保留期/目标为正数。输出保留 capacity.json、discovery.txt、tests.jsonl 和 report.json，其中报告记录 SHA、seed、执行退出码、计数与各工件摘要。

## 本轮复核与静态检查

- 推送前再次复核：修正系统状态仍用旧 LOG_CURSOR_ACTIVE 判断配置、导致默认自动签名器被显示为未配置的问题；同步邮件通道禁用行为的 Compose 检查源码，修正维护连接及升级排空步骤的旧说明。未执行测试或实际升级。
- 人工复核修正了事务回调未继承 5 秒截止时间、通知从旧错误时间计算重试期限、清理超期恢复遗漏已提交回执、SSE 会话撤销后的阻塞写、目录时间示例，以及旧部署测试仍断言旧 Sender 状态等问题。
- 新增真实 PostgreSQL 协议测试源码位于 `tests/integration/operations_protocol_test.go`，覆盖待提交 writer 阻塞后续序号、回滚、受限 writer/reader、受保护前缀、清理回执和不复活重放。尚未运行，不计 OI 覆盖完成。
- 已做 Go 格式、Linux 目标的 internal 测试源码和 integration 测试源码编译、cmd 构建、go vet、Python AST、shell 语法及 git diff --check；没有运行任何测试程序。

## 本轮补充（开发产物，未运行测试）

- SSE 的取消和写截止时间通过 `internal/streamio` 串行处理，阻塞写不会在退出登录后被后续写入延长期限；handler 返回前等待取消回调和会话检查线程退出。
- `sources` 新增 `last_failure={task_id,log_task_id,code,observed_at}` 和 `failure_state`。来源是失败任务的采集进度 checkpoint，只公开固定 `acquisition_failed` 代码。`none` 表示无该类已记录失败，不能作为源健康证明；`unresolved` 表示尚无后续成功检查；`superseded_by_success` 表示已观察到不早于失败的成功检查。失败摘要保留历史，不能推进 `confirmed_until`。
- 受控测试的执行器并发上限由原子标志保护；内存结果同时限制 20 MiB 和 1000 条。逐字段请求契约、超时/重定向/响应超限、队列满载/幂等、结果隔离/淘汰与九业务代理新增源码回归。
- 邮件通道关闭时仍按有效旧 MAIL_TO 导入历史投递；旧最后一次明确 rejected 保留为 rejected，其他应答不明继续 unknown，不自动恢复发送。显式测试创建投递前在受理锁下复核执行者、租约、期限及设置 revision。
- 清理在日志数据库或提交应答不明时保留 running，租约到期由后续 worker 核对持久回执。`cleanup_result` 在与删除相同的计数行锁下读取回执；已提交回执先于当前策略/预览期限处理。`cleanup_try_prefix` 将已知过期/受保护拒绝返回为明确失败，网络/提交不明继续等待核对。数据库长期不可用时任务仍待核对，不能报 deleted=0 或成功。完整引用退休协调与扩展对象清理尚未实现，当前保守范围保持不变。

本轮源码已人工复核；未运行任何 T/OI，不构成部署或验收证据。

P31.04 已收尾：总览增加任务各类时间；active_state 可为 available/empty/unavailable/slot_unavailable，并据依赖状态修正动作提示。相关记录见 verification/P31.md，下一步 P32.01。

P32.01 已完成：目录版本为 2026-10-13.v2，业务响应字段 required 与可空分开；完整字段独立预期见 operations/testdata，下一步 P32.05。

P32.05 已完成：代理保留 X-SICP-Combination；8→9 独立 token、版本与人数整轮校验复用原业务协议，下一步 P33.01。

P33.01 已收尾。通知设置响应保留 channel_state，新增 channel_diagnostics.last_result/observed_at（本实例上次真实通道结果，未发送前 not_observed/null），无主动 SMTP 探测。下一步 P33.02。

P33.02 已完成，发送/租约不超过原 24 小时期限，模糊回执保留 unknown。下一步 P33.04。


### P33.04 开发收尾（未运行测试）

`tests/integration/notification_api_test.go` 覆盖 T33-15—17：保存无投递、重复键/旧 revision、本人作业查回、两类分页/未知详情、游标接口/主体隔离、未登录与 CSRF、临时地址和非法分页参数拒绝。复核补齐 Deliveries 游标校验、详情读取失败清空部分结果、路由依赖缺失拒绝挂载。Linux integration 编译、operations/admin go vet 通过；未运行测试、迁移或 SMTP。无 schema 修改，代码可独立回退。P33.04 已勾选，下一项 P33.05；T/OI 实测框保持未勾选。
