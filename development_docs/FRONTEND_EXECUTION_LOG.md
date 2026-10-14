# 前端逐步开发记录

## 执行约定（2026-10-14）

以 [搭建计划](FRONTEND_IMPLEMENTATION_PLAN.md) 为唯一任务清单，按 F00 → F01/F02 → F03 → F04 → F05/F06 → F07 → F08/F09 → F10 → F11 → F12 推进。每个编号步骤完成实现、适用验证和代码复核后，勾选对应项，独立提交并 push；push 成功后才开始下一步。若发布受阻，保留成果和阻塞记录，不跳过门槛。

前端查询与运行状态始终来自后端。正常路径测试读取现有 `mock_data/snapshots/initial`、`responses` 及后端固定预期，不重新编造人员、组织、成绩、课表和班级关系。DTO 保真夹具只用于协议测试，不冒充正常查询结果；边界/失败使用仓库已有对应场景。Mock 测试、真实后端联调、真实网关验收分开登记，未执行不得勾选。后端 T/OI 暂停约束保持不变。

## F00-01 图稿追踪基线

- 基于提交：`b83ac1d`，分支 `feat/frontend-development`。
- 输入：V5 图库 28 张 PNG、图库 manifest、搭建计划、设计方案、Hub 主题，以及当前后端契约代码。
- 交付：[图稿追踪矩阵](FRONTEND_VISUAL_BASELINE.md)；`frontend/scripts/check-visual-baseline.mjs` 校验图像完整性、需求/实施/测试编号及覆盖数量。
- 复核范围：逐图固定布局、重要状态和契约修正；不表示页面已实现，也不表示图稿已获得人工验收。
- 数据：没有新增或修改业务 Mock；图库中的示例值不进入运行时代码。
- 验证：Node v24.15.0，`node frontend/scripts/check-visual-baseline.mjs` 通过，28 图的 SHA-256/尺寸/字节数一致，R01 至 R11 全覆盖，所有实施/验收编号存在。首次暂存检查发现原计划的 Markdown 双空格换行触发行尾空白告警，已改为等效反斜杠换行，提交前复查。
- 代码复核：已修正课表/组织/人员/教学班的验收编号映射；校验器从脚本位置解析路径，拒绝缺图、重复图、哈希或版本变化、缺列和未知编号。当前复核无遗留问题；此为本次代码自审，最终人工验收项不勾选。
- 提交与 push：本步骤提交后推送 `origin/feat/frontend-development`；成功后的 SHA 与结果在下一步记录中补登，失败则保留阻塞记录。

## F00-02 逐端点接入核对

- 前一步：`283dd68` 已成功 push 至 `origin/feat/frontend-development`；Git 返回 `[new branch] HEAD -> feat/frontend-development`。并行只读 ls-remote 曾超时，不影响 push 已返回的成功结果。
- 基线：[端点接入表](FRONTEND_ENDPOINT_BASELINE.md)，覆盖认证/健康/旧任务、九业务、目录/运维/源测试、通知/维护及双源日志。
- 验证：Node v24.15.0，`node frontend/scripts/check-endpoint-baseline.mjs` 通过：47 个方法/路径组合、九业务、十四源与既有管理 HTTP 样例均有登记。没有运行后端 T/OI，没有发送副作用请求。
- 代码自审：逐项对照路由、DTO、解析器与接入包；修正本地业务 code 的字符串语义，区分业务 422/管理 429，明确 Organization 与 Department 的 is_active 可空性差异、根操作节点省略 parent 参数。路由校验包含集合双向比较，拒绝漏记或虚构端点。
- 本步骤完成后勾选 F00-02，提交并 push；下一步 F00-03 处理差异关闭范围。真实接口可用性与页面验收均保持未勾选。

## F00-03 契约差异归属与关闭条件

- 前一步：`45b685c` 已成功 push，输出 `283dd68..45b685c HEAD -> feat/frontend-development`。
- 交付：[差异登记](FRONTEND_CONTRACT_GAPS.md)，覆盖原计划九类差异及 is_active、源 Query/测试载荷两项补充；修正 `OPERATIONS_API_EXAMPLES.md` 的本地业务 code 类型错误。
- 文档自审：每项区分前端工作、后端缺口、关闭条件；“登记完成”不等同于依赖关闭。所有页面及实际验收项保持待办，未改正常 mock 或后端代码。
- 验证：复跑端点基线，提交前 `git diff --check`；本步为文档契约修正，无运行时代码变化，不新增镜像实现的单测。

## F00-04 后端来源测试数据 v1

- 前一步：`437e2d3` 已成功 push，输出 `45b685c..437e2d3 HEAD -> feat/frontend-development`。
- 交付：`frontend/fixtures/` 的后端导出响应、版本清单、只读加载器与用法；显式 Go 导出入口 `TestFrontendFixtureExport`，默认跳过。原始 mock 未修改；十四源响应直接引用现有文件，九业务经真实 HTTP Pipeline 导出。
- 验证：Go 1.26.8、Node v24.15.0。专用导出命令连续两次通过，第二次产物哈希不变；`node frontend/scripts/check-fixtures.mjs` 通过（34 响应、9 业务、14 源成功/空响应、148 来源哈希及既有五类独立预期）。后端 T/OI 没有运行。
- 前端验证：`npm.cmd run test:unit --prefix frontend` 2/2 通过，`npm.cmd run build --prefix frontend` 类型检查与生产构建通过；dist 未检出 mock 路径/示例学号/示例姓名。首次通过原 E2E 配置执行的 4 用例也最终退出成功，但 Windows 开发服务清理较慢，已清理该服务并为纯状态/协议测试采用不启动服务的独立配置。
- 代码自审：实际响应保留 null/前导零/响应头；源返回原始文本避免大整数转换；每次返回独立副本；请求未知场景报错；显式更新 manifest 在校验通过后写入。未复制业务查询算法作为预期。全量业务语义和真实网关验收仍待后端/集成测试。

## F00-05 技术与依赖锁定

- 前一步：`ba655d6` 已成功 push，输出 `437e2d3..ba655d6 HEAD -> feat/frontend-development`。
- 交付：[技术基线](FRONTEND_TECH_BASELINE.md)，精确新增 vue-router 4.6.4 及其必要传递依赖，复用已有 Playwright 的独立单元配置；既有依赖无批量升级。
- 兼容核对：npm 官方 registry 元数据和安装包 peerDependencies 均为 Vue ^3.5.0，当前锁定 Vue 3.5.43；npm ls 无冲突。
- 验证：单元测试 2/2、vue-tsc 与 Vite 生产构建通过。代码自审核对 lockfile 仅新增 vue-router 和 @vue/devtools-api 两项，未改代理/部署/运行时代码。
- F00 五项基线完成；后续 F01-01 开始应用结构和懒加载迁移，页面及真实网关验收仍未完成。

## F01-01 应用分层与懒加载

- 前一步：`fe3c2ff` 已成功 push，输出 `ba655d6..fe3c2ff HEAD -> feat/frontend-development`。
- 实现：拆分 app/router、app/startup、AppShell、LoginPage，将原三个页面移动到 pages，不复制第二套业务实现。Vue Router history 使用 /console/，旧 hash 兼容；三页分别懒加载，缓存绑定会话并有上限。查询停用时隔离迟到响应，进度计时器随缓存激活/停用启停。
- 代码自审：发现 WorkProgress 原计时器只处理卸载而未处理缓存停用，已修正；3 个旧测试的学期下拉定位改为可访问 combobox 角色，保留原业务断言。检查路由切换不执行查询或任务，原任务幂等和各分页行为保留。外部请求取消/会话请求统一代次由 F01-02/F01-04 继续实现。
- 验证：Node v24.15.0，本机 Chrome；受影响 22 项复测通过后，最终 `node node_modules/@playwright/test/cli.js test --reporter=line` 在 frontend 下执行，84/84 通过（2.3 分钟，loopback 与 intranet-http 各 42）。`npm.cmd run test:unit --prefix frontend` 2/2 通过，`npm.cmd run build --prefix frontend` 类型与构建通过；图稿/端点/夹具三个基线校验通过。
- 新增业务交互场景直接使用 F00-04 后端学生样例，验证 12 条原始 mock 学生与前导零 ID；没有新增手写正常查询数据。
- 截图证据：`frontend/test-results/console-session-survives-r-63901-d-logout-clears-the-console-loopback/overview-desktop.png` 及各进度/九接口 mobile 截图。已查看桌面截图，结构迁移未改变既有视觉；Hub 主题尚待 F02。真实静态网关没有运行，不勾选 FT09-08。
