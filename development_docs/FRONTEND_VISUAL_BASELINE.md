# F00-01 · V5 图稿追踪基线

本表固定实现范围，不代表页面交付或视觉验收通过。图像以 `ui-review-v1/manifest.json` 的 V5 文件名、尺寸和 SHA-256 为基线；后端代码优先于图中文字和示例值。

## 公共布局

使用现有 `frontend/` 工程；中文浅色管理台，Hub 主色 `#195BFF`。顶栏约 56px、侧栏 216px/折叠 64px、内容边距 24px，标题/区块/正文 24/18/14px。主任务内容在左，辅助诊断或详情在右；小屏按 1280/768px 断点折叠，宽表仅内部滚动。七项导航共用同一框架，测试只从接口详情打开。

06 至 14 是 05 的结果呈现方式，不能创建九个独立测试页面；25、26、27 是跨页面能力。下列状态均从真实响应或明确的本地交互阶段派生，不采纳截图中的时间、数量、身份和健康结论。

## 逐图交付矩阵

| 图像 | 需求 | 实施 | 验收 | 固定布局与关键状态 | 按当前契约修正 |
| --- | --- | --- | --- | --- | --- |
| 01-login.png | R01 R11 | F03 F02 | FT01-01 FT01-02 FT09-03 | 左侧登录表单、右侧产品说明；密码错误、提交中、会话失效 | 使用真实管理员账号密码；单环境提示，不创建环境选择器或账号平台 |
| 02-overview.png | R02 R07 | F04 | FT02-01 FT02-02 FT02-03 | 顶部摘要、最近更新、十四源表、右侧关注事项；初始化、旧快照、局部失败 | 查询/更新/日志独立；发布、检查、计划时间分开，未知不填零 |
| 03-api-catalog.png | R03 | F05 | FT03-01 | 搜索与分类在上，接口列表在下；未知就绪、不可执行条目 | 九业务十四源均已实现；管理文档为版本化补充，行政班待定，不伪造目录 ID |
| 04-api-detail.png | R03 R04 | F05 F06 | FT03-01 FT03-03 | 左目录右详情、常驻 Endpoint、文档标签、唯一测试按钮 | 自定义 Field 需适配；readiness unknown 不等于可用 |
| 05-api-test.png | R04 R11 | F06 F02 | FT03-03 FT03-05 FT03-06 FT03-07 | 参数在上结果在下，JSON/表格/响应头共享响应；未操作、加载、错误 | 打开与填参不执行；本地同步代理和上游异步作业分开，不虚构 test_run_id |
| 06-student-grades.png | R05 | F06 | FT04-01 FT04-03 FT04-10 | 成绩表、行详情、分页；嵌套学生课程、缺档 | 七分页接口之一；使用实际快照与分页元数据，保留 null |
| 07-batch-grades.png | R05 | F06 | FT04-02 FT04-10 FT04-11 | 多条件表单、成绩表；旧结果、快照失效 | 数量按成绩记录，不按学生数；改条件开启新查询 |
| 08-student-timetable.png | R05 | F06 | FT04-04 | 参数、列表/周课表、排课详情；日期缺失 | 无分页；不按截图推算缺失日期和节次时间 |
| 09-staff-timetable.png | R05 | F06 | FT04-04 FT04-05 | 列表与排课侧详情；多个教师、缺档 | 教师 ID/名称独立数组，不按索引强行配对，不丢缺档记录 |
| 10-organizations.png | R05 | F06 | FT04-06 | 表格/组织树切换、组织详情；部分树、未知有效性 | 移除过时未实现标签；有效性 true/false/null 三态，部分分页不宣称完整树 |
| 11-students.png | R05 | F06 | FT04-07 | 学生结果、学籍归属和详情；缺档、前导零 | 移除过时未实现标签；只展示后端授权字段，不自行填联系方式 |
| 12-staff.png | R05 | F06 | FT04-07 | 教职工表、单位/状态和行展开；null/false/0 | 移除过时未实现标签；保留编号、原始业务代码和空值语义 |
| 13-teaching-classes.png | R05 | F06 | FT04-08 FT04-09 | 教学班表、人数、查看学生入口；歧义提示 | 移除过时未实现标签；学年/学期/班号/计划课程号组成身份，跳转只填参 |
| 14-class-members.png | R05 | F06 | FT04-08 FT04-09 | 班级摘要与名单；组合/人数不一致提示 | 移除过时未实现标签；使用相同数据组合对照人数，不合并同班号跨学期数据 |
| 15-updates.png | R06 | F07 | FT05-01 FT05-04 | 一次更新一行、状态模式来源、受控动作；提交不明 | 当前仅 cursor/page_size；使用加载更多，不伪造全历史筛选与总页数 |
| 16-update-detail.png | R06 | F07 F04 | FT05-02 FT05-03 FT05-06 | 左操作树右详情、阶段进度和失败证据；延迟、失败、待确认 | 采集 100% 不等于发布；区分两份构建、发布与进度确认；未知分母不算百分比 |
| 17-sources.png | R07 | F04 | FT02-04 FT02-06 | 十四源状态表与详情抽屉；确认进度、失败窗口 | 无失败记录不推导绿色健康；失败不推进 confirmed_until，本地筛选只作用已获取数据 |
| 18-platform-logs.png | R08 | F08 F09 | FT06-01 FT06-03 FT06-07 | 筛选、实时状态、新日志提示、右详情；读取失败、过期 | 本地逻辑源与上游源分开；历史 next_cursor 不作为实时 stream_cursor |
| 19-upstream-logs.png | R08 | F08 F09 | FT06-02 FT06-05 FT06-08 | 逻辑调用与逐次尝试、超时退避、断线提示 | 同一 db-log 内独立逻辑库，不声称物理隔离；按 source/epoch/event_id 区分 |
| 20-log-detail.png | R08 | F08 | FT06-02 FT06-08 FT06-09 | 时间线、脱敏参数/响应、跨源关联；日志待入库 | 路由含 source/events/eventId；缺日志不改变任务结果，JSON 只按文本显示 |
| 21-audit.png | R08 | F08 | FT06-01 FT06-02 | 操作者、动作、对象、后续执行链接；受理/拒绝 | 管理审计是本地日志视图；202 表示受理，不是执行成功 |
| 22-notifications.png | R09 | F10 | FT07-01 FT07-04 | 通知决策、收件人投递及尝试；部分失败、unknown | 零投递不等于无异常；accepted 为通道接受，不是邮箱送达；游标加载 |
| 23-email-settings.png | R09 | F10 | FT07-02 FT07-03 FT07-05 | 开关、地址列表、规则、右侧通道摘要；版本冲突、未配置 | 只开放 source_schema/task_failed/source_test_failed；保存与显式测试分开，revision 为字符串 |
| 24-system.png | R10 | F11 | FT08-01 FT08-02 FT08-03 FT08-04 | 依赖/双源、容量缓冲、配置探测；unknown/unavailable | runtime_fields 与 deployment_fields 分开；无契约配置不可编辑，保留期与清理独立 |
| 25-operation-confirm.png | R06 R10 R11 | F02 F07 F11 | FT05-04 FT05-05 FT08-03 | 明确范围影响的确认对话框；提交中、受理不明 | header Idempotency-Key 与 JSON idempotency_key 分开；重试复用原载荷，关闭不取消后台 |
| 26-ui-states.png | R01 R11 | F02 F03 F06 | FT01-02 FT03-07 FT09-05 | 未初始化、未操作、空结果、不可测试、错误、401/403 | 新增未知/过期/旧快照明确文案；不提供无契约手动初始化、恢复或回滚按钮 |
| 27-responsive.png | R11 | F02 F03 | FT09-01 FT09-02 FT09-03 FT09-04 | 抽屉导航、单列表单、全屏测试、单列任务详情 | 1440/1280/1024/768/390px 实测；焦点返回、44px 触控目标、200% 缩放 |
| 28-upstream-api.png | R03 R04 | F05 F06 | FT03-01 FT03-02 FT03-08 | 左目录右详情、Query 参数和独立字段表、单页测试 | 固定目标，不接收自定义 URL/Header；上游数值 code 与本地字符串 code 分开 |

## 后端核对入口

- 九接口与快照能力：`internal/query/contract/operations.go`；响应 DTO：同目录 `dto.go`、`phase2_dto.go`。
- 目录及源测试：`internal/operations/catalog.go`、`internal/admin/api_catalog.go`。目录独立字段预期：`internal/operations/testdata/reviewed_catalog.json`。
- 运维列表及错误：`internal/admin/operations.go` 的 `pageInput`、`operationError`；通知/维护：同目录 `notifications.go`、`maintenance.go`。
- 任务动作：`internal/admin/routes.go`；日志：`internal/admin/logs.go`、`internal/logquery/`。

逐端点请求/响应登记属于 F00-02；差异关闭条件属于 F00-03；现有 mock 与固定预期的版本化引用属于 F00-04。此处只冻结视觉到交付物的追踪，不提前勾选这些步骤。
