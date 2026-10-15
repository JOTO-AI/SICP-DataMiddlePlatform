# F12 前端代码与交付复核

2026-10-15。范围为既有 Vue 工程 F01–F12 前端交付；沿用“实现 → code review → 测试 → 勾选文档 → commit/push”的逐项流程。本次为逐文件人工代码复核与本机协议/浏览器测试，没有执行真实后端、邮件、更新或清理操作。

**验收边界：用户明确要求 F12-02/-03 等服务器部署好后再测。** 后端 T/OI 继续暂停，F12 完整上线验收未通过；未关闭的契约扩展与完整视觉验收继续保留待办。

## Review 结果与修复

| 问题 | 处理与证据 |
| --- | --- |
| 旧独立查询页面与新接口中心重复；独立更新详情缺少已有进度诊断 | F12-01 迁为 BusinessVerification 内部组件，共用 TaskDiagnostics；旧路由只重定向，保留九接口、分页和维护操作回归 |
| 原总览测试仍假设 10 秒刷新；SSE 精确时钟断言混入真实执行时间 | 与已交付 15 秒刷新同步；SSE 专项固定/暂停时钟。没有调整应用频率来迁就测试 |
| 网关用例仍点击旧导航名称 | 链接改为“更新事件”，页面标题继续为“更新任务”；真实网关测试延后 |
| 隐藏原文仍序列化/挂载，旧任务原文没有长度上限 | LazyResponse 展开后渲染、收起卸载、停用收起，使用公共 4096/65536 字符限制；F12 原文 DOM 检查及容量测量 |
| 失败诊断只检查顶层，异常 response.body 对象会流入文本组件 | 验证嵌套 response/authentication 字段与构建表字符串；畸形诊断保守过滤并提示，不清掉可确认任务事实；F12 边界变体回归 |
| README、图稿追踪与差异表仍包含已接入模块“待开发”描述 | F12-05 和本次文档复核同步；历史启动基线与当前状态明确区分 |

复核没有发现需要阻止本次前端代码提交的未修复问题。此结论不替代未执行的服务器验证或完整视觉验收。

## R01–R11 与测试追踪

以下路径均在 `frontend/`。测试标题保留 F/旧 T/BT 标识只是历史命名，本轮执行均按前端拦截测试记账，不能计入后端 T/OI。

| 需求 / 图号 | 代码复核入口 | 当前证据 / 尚待确认 |
| --- | --- | --- |
| R01 / 01、26、27 | session、app/router、AppShell、requestScope、pending | console 的登录、七导航、旧链接、403/404、退出和迟到请求；auth/http 单元测试。真实 Cookie/CSRF 拒绝由服务器验收补充 |
| R02 / 02 | Overview、overview/sources 状态适配 | F04 旧快照、时间缺失、查询/更新独立状态、自动初始化；不增加手动首次初始化入口 |
| R03 / 03、04、28 | ApiCenter、ApiDetail、catalog、requestExamples | F05 23 项目录/Field/原 Query 与作业 Body/示例占位符；D01 管理说明与行政班待定不冒充后端目录 |
| R04 / 05、28 | ApiTestDialog、businessTest/sourceTest、ApiResponseViews | F06 一次执行、实例/版本绑定、关闭取消等待、原键恢复、结果不可用、视图不增加请求 |
| R05 / 06–14 | queryOperations、responseViews、pagination、timetable、organizationCollection、roster | 九业务逐项捕获；七分页逐项独立快照；课表不分页、教师不强配、组织部分树、人员缺档、名单组合/人数核对 |
| R06 / 15、16、25 | Tasks、UpdateDetail、TaskDiagnostics、OperationTree、updateActions | F07 历史后端筛选、树分支/游标、控制事实和诊断隔离；F01/F07 同键重试、失败重跑、逐表补齐；F12 嵌套诊断异常 |
| R07 / 17 | Sources、SourceDetail、source 状态适配 | F04 已获取源筛选、抽屉键盘返回、旧快照、隐藏/离页停止；无失败不推断绿色健康 |
| R08 / 18–21 | Logs、LogDetail、logHistory/logSSE/logBuffer、logStream | f06-logs 中 F06/F08/F09 全流程；原始/关联 ID、仅完整帧续接、Retry-After、缺口/裁剪、1000 总预算、文本脱敏展示 |
| R09 / 22、23 | Notifications、MailSettings/MailTest、DeliveryDetail、managementJobs | f10-notifications 的 revision 冲突、保存不发信、显式测试、原键恢复、八种投递语义；多收件人真实部分投递待联调 |
| R10 / 24、25 | Settings、SystemStatus/SystemConfiguration、RetentionSettings、LogCleanup | f11-settings 与 systemStatus/retention/cleanup 单元测试；未知容量、只读部署摘要、1..3650、预览失效、来源与真实回执匹配 |
| R11 / 25–27 及所有页面 | Dialog/Drawer/JsonText、CSS 令牌、LazyResponse、请求与可见性范围 | dialog/components/states、F02 七个断点边界、390px 表格/弹窗、主题 20 组、F12 容量。全页逐控件 200% 缩放/屏幕阅读器/28 图逐断点尚未完整人工验收 |

FT 勾选只覆盖条件已完整证实的条目；复合用例有部分证据时仍留空。尤其 FT01-03 的实际 CSRF 拒绝、FT07-01/-04 的真实隔离业务/投递链、FT09-01/-03/-04/-05 的完整人工体验及 FT09-08/-09/-10 不因 Mock 回归成功而批量勾选。

## 28 图与合理差异

[图稿矩阵](FRONTEND_VISUAL_BASELINE.md)逐图包含 01–28 的来源文件、需求、实现和验收编号；校验脚本已核验 28 张 V5 图片 SHA 与引用。上表完整覆盖图号，当前截图来自测试输入，不能用图中人数、时间、身份代替后端事实。

- 01–05、28：沿用浅色 Hub 蓝、七导航和详情内测试弹窗；单管理员、单环境，管理文档/行政班不虚构可执行目录。
- 06–14：共用同一响应视图，不新建九个测试页面。空/缺档/未知字段按 DTO；课表日期缺失不补造，组织单页不冒充全树。
- 15–21：运维列表采用真实游标，数字跳页延后。更新后端筛选已接入，日志仅开放已有参数；操作树/详情按需展开，实时时序按服务端游标。
- 22–24：接受邮件不等于送达，配置不等于观测健康；只开放三条规则；部署凭据只读摘要；保留期与单源清理独立。
- 25–27：危险动作显示实际对象/范围并确认；关闭等待不宣称撤销；小屏保留全部事实和内部滚动，未提供项明确未知。

本轮人工查看了最终回归的 1440px/768px/390px 总览、390px 业务测试弹窗、通知详情和系统状态截图（无整页横向溢出，表格内部滚动），并结合既有任务截图复核长内容。完整逐图、逐断点的视觉接受状态仍留 FT09-01 未勾选，不把追踪校验当视觉签收。

## 证据与重现

- 本轮起点：`352cad7`；此前 F12-01 `13d95bc`、延期/网关导航 `86580bd`、容量 `3dfee7b`、README `352cad7` 已分别 push。
- 正常输入：34 基础业务捕获 + 82 分页捕获、14 对原始源响应，150 项来源 hash；边界变体在用例内注释。没有重写已有正常样例。
- 单元 112 项、类型检查与生产构建、夹具来源、28 图追踪、20 组对比度已通过。容量单 worker 两 origin 结果见 [容量记录](verification/frontend_f12_performance.json)；四 worker 混跑容量计时出现超限，因此将容量用例移到独立 performance 目录与单 worker 配置，保留原阈值、保留失败记录，按预登记条件重新执行；容量测试不再混入并行功能回归。
- 最终浏览器回归：完整运行 363/366（11.7 分钟），三项失败为两个 origin 的并行容量计时和一个浏览器上下文关闭超时。按原阈值隔离运行容量 4/4、更新/诊断单 worker 复测 4/4 通过；合并证据覆盖 362 个功能用例及 4 个容量用例，未解决项为 0，原始完整运行不改写成全绿。命令在 frontend 中：`SICP_TEST_BROWSER` 指向 Chrome，`PLAYWRIGHT_JSON_OUTPUT_NAME=../artifacts/frontend-f12-06/final-report.json`，`npx playwright test --fully-parallel --workers=4 --reporter=line,json --output=../artifacts/frontend-f12-06/final`。
- 原始报告/截图保存在 `artifacts/frontend-f12-06/`（Git 忽略）；[回归摘要](verification/frontend_f12_regression.json)、[168 张截图 SHA 清单](verification/frontend_f12_screenshots.json)与[最终构建清单](verification/frontend_f12_final_build.json)随本次提交保存。测试前 HEAD 和工作区变化配合源码树 hash 标明精确构建输入，最终复核提交将包含这些修改。
- CI 仅更新 frontend job：补齐来源/图稿/主题/单元/构建检查，功能回归使用两个 worker，容量随后独立单 worker，上传两类报告与构建清单。YAML 已解析校验；未修改 backend/gateway job、未主动重跑后端验收，也没有远端 CI 通过结论。
- 回退保留旧 console 镜像/完整 dist，先核对与后端契约兼容，不能只恢复 index.html。操作说明见 [README](../frontend/README.md)；本轮未执行部署或回退。

## 继续保持待办

F12-02/-03 及 FT09-08/-09：等用户在服务器部署完成后，实际验证登录/查询/源测试/更新/双源日志/通知/清理，以及同源 API 错误、深链接、SSE、HTTP/HTTPS Cookie/CSP。保持隔离合成上游和测试邮箱边界，不恢复其他暂停验收。

D02/D07/D09/D12：额外筛选、额外规则、无契约恢复/回滚及单源跨作业完整历史继续开放；行政班独立查询未纳入。通知表单目前限定不含显示名称的普通邮箱格式，完整 RFC 引号地址不是本轮验收范围。

FT09-01/-10 与其余未勾选复合条件：完整视觉/真实环境验收和上线签收仍待执行。F12-06 勾选只表示完成本轮代码 review、追踪核对和问题修复，不能写作“整套前端已上线验收通过”。
## main 同步补充（2026-10-15）

本轮将 main `5433ec8` 的统一上游诊断接入当前前端，沿用任务与日志详情，并修复单日志库来源兼容。具体冲突取舍、后端捕获来源、review 发现与测试结果见 [合并复核](verification/FRONTEND_MAIN_MERGE_REVIEW.md)。本节不改变以上真实服务和完整视觉验收的待办状态。

## 更新任务页面去重复核（2026-10-15）

- 起点：`2af55cd`。删除重复历史列表与仅该列表使用的 CSS；自动更新摘要和当前未结束任务入口保留，任务结束后该入口消失。状态读取失败时不展示当前任务入口，并继续禁止无法核对条件的新操作。
- Review：事件列表仍负责历史分页、筛选与详情；原任务号查询、维护、诊断与重试保持可达。状态请求、轮询、生命周期取消、操作条件及幂等逻辑无改动。当前事件入口和忙碌提示链接采用不同文案，避免同页同名入口混淆。未发现未解决阻塞项。
- 测试：70/70 浏览器用例通过（loopback / intranet-http，各 35 项），覆盖任务进度、失败重跑、单表维护、诊断、受理重试、状态失效、页面切换、历史分页/筛选/游标与操作树。复用已有后端捕获和边界场景，仅调整被删除列表的导航方式并补充去重/当前任务断言；夹具文件未改。类型检查、生产构建、57 文件审计及 150 项来源 hash 通过。
- 人工查看 `artifacts/frontend-task-simplify/results/console-update-entry-expla-b6a8a-blocks-use-of-stale-history-loopback/` 下 `updates-current-task.png` 与 `updates-current-task-mobile.png`：重复列表消失，当前任务与维护区正常，小屏无整页横向溢出。
- 重现：在 frontend 设置 `SICP_TEST_BROWSER` 为 Chrome 路径，运行 `npx playwright test console.spec.ts -g "BT09|post-acquisition|historical data|failed full|manual incremental|partial initialization|failed task|backend diagnostic|legacy and malformed|table page progress|task page|unknown admission|abandoning an unconfirmed|failed initialization|initialization retry|update entry|F01 leaving|F01 pending|F01 read polling|F07" --workers=2 --fully-parallel --reporter=line,json --output=../artifacts/frontend-task-simplify/results`。JSON 报告由 `PLAYWRIGHT_JSON_OUTPUT_NAME=../artifacts/frontend-task-simplify/report.json` 指定。
- 报告 SHA-256：`d8cfd3f4df0855972cfad76037d40595f0430071d58c17a9547a8f8e6b590e0f`。本次为针对性前端回归，不代替服务器联调、完整视觉验收或既有后端验收。

## 接口中心及共享细节补充（2026-10-15）

按用户反馈修正英文元数据、根对象占位、必填排序、耗时单位，以及目录/详情布局、源中文名、导航图标、表格与日志分类。逐项 review、图稿取舍和最终验证见 [界面细节复核](verification/FRONTEND_VISUAL_DETAIL_REVIEW.md)。完整回归原报告 366/368；修正旧图标断言并完成相关 42/42 复测，未解决项为 0。未变更服务器联调或全部 28 图综合视觉验收状态。

## 更新事件页补充（2026-10-15）

V03 按用户反馈进一步删除 M05 当时保留的自动更新摘要和当前任务区，同时删除内嵌执行详情。维护区保留，历史与详情各自独立；列表按 15 图重排。80/80 相关双 origin 回归、122 单元及构建/产物/来源/对比度通过，五种宽度和桌面/手机截图已核对，code review 无未解决问题。详细取舍及原始失败记录见 [更新事件页复核](verification/FRONTEND_UPDATE_PAGE_REVIEW.md)。
