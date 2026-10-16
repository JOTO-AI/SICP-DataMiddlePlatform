# V07 执行时序操作名称复核（2026-10-16）

- [x] 日志摘要补充已有操作上下文，时序与基本信息显示具体阶段、数据表、副本和尝试。
- [x] 使用后端记录器生成的 DTO 验证前端展示，兼容旧接口和缺失字段。
- [x] 完成针对性回归、构建、桌面/手机截图复核和 code review。
- [ ] 服务器部署后的实际联调，沿用用户延期安排。

## 原因与实现

原时序只渲染 kind/phase，列表 EventItem 又不带详情 metadata.build，导致不同操作都显示“更新步骤 · 开始/结束”。现在列表、详情和 SSE 的公共投影可返回可选 context：stage、table、instance、slot、attempt、page、mode、operation。字段取自已经脱敏、校验的事件，不增加数据库列、查询次数或每行详情请求，也不复制响应正文、完整元数据及原始标识。

阶段使用现有操作树中文名；表名、副本和重试次数按实际记录显示。slot=0 是有效槽位，只有记录了 generation 时才投影槽位。无阶段时可显示业务接口或源表采集；旧详情从自身 metadata 回退，旧列表无名称明确显示“更新步骤（名称未记录）”。不借用相同任务中另一事件的上下文，不从开始记录推断任务仍在运行。开始记录显示“已记录开始”，结束结果和 ms 耗时仍使用原事实。未知阶段码保留原文，文本采用 Vue 转义渲染。

部署需同时更新 worker（日志查询接口）与 console（前端）；既有日志 envelope 已包含 build 元数据的记录无需重采或迁移即可补全。新前端兼容旧后端，但旧列表无法补出未返回的阶段信息。

## 测试来源与结果

- 新夹具 frontend/fixtures/log-context.json 来自 internal/logquery/context_test.go 的 TestLogContextBuildBoundaries：真实 events.BeginBuild 生成 M/S 副本边界，经 Sanitizer 和公共 project 转换；没有编造业务查询行。夹具 SHA256：46ba5dac73c376e277ecea98ca0c88e6fa1714a77f3191c8fa83e9b2b3f7c69f。
- 导出：在仓库根目录将 SICP_LOG_CONTEXT_FIXTURE 设为上述文件绝对路径，再运行 go test ./internal/logquery -run TestLogContextBuildBoundaries -count=1。固定时钟和随机源保证可重现。
- 新后端 context 两项测试通过。日志查询整包仅 TestT30_04To06OrderedFramesWriteFailureAndConnectionRelease/a 的 frames buffered 失败；对 HEAD 00fdbdb 的 dto.go/repository.go 使用只读 Go overlay 并移除新增测试后，同一用例连续三次复现。排除该既有用例后的整包通过，go vet ./internal/logquery 通过。未将后端整包记为全绿，也未恢复延期的 T/OI 验收。
- 前端单元首次 129/130，通过修正对旧夹具 operation=null 的错误测试期待，新增三项复测 3/3；合并覆盖 130 项。未知阶段、原型属性名称、错误类型、过长文本和缺失 context 已验证。
- 浏览器最终 12/12（双 origin）通过：V05、V07、F06 日志入口。首次 9/12：一次登录恢复超时；两次新断言错误地要求相同发生时间的边界按配对排序，已改为核验操作名称/副本而不制造先后关系。
- 证据：artifacts/frontend-log-context/report.json 和 final-report.json，截图在 final 下。人工查看 1440px/390px 时序截图：名称、长表名可换行，M/S 可区分；既有 V05 五种宽度检查继续通过。
- 最终类型检查/生产构建通过；既有 150 项夹具来源检查通过，业务 Mock 未修改；生产产物审计通过。

## Code review

复核了摘要白名单、脱敏边界、可选字段兼容、零槽位、分页/世代约束、同时间排序、关联请求数量及手机换行。无新增未解决阻塞项。既有 SSE 测试失败如上记录，不属于本次时序名称改动。

## V08 折叠长时序（2026-10-16）

- [x] 默认展示按既有时间顺序排列的前 3 条；底部按钮显示“展开全部已读取记录（N 条）”，展开后支持收起。3 条以内不出现折叠控件。
- [x] 折叠仅影响渲染，不删数据、不追加请求；后端未读批次仍通过“继续读取时序”获取。按钮声明 aria-expanded 与唯一 aria-controls，支持键盘操作。
- [x] 切换事件/离页及成功重新读取首批恢复折叠；继续分页保持用户展开状态，失败保留已有记录和状态。
- [x] 复用现有后端生成夹具，无新增或更改 Mock；10/10 浏览器回归通过，验证默认 3 条、展开 4 条、收起、键盘、重新读取、请求次数与分页失败保留。报告 artifacts/frontend-timeline-collapse/report.json，截图 browser 子目录；人工复核手机折叠布局。
- [x] 类型检查、生产构建、56 文件产物审计及 git diff --check 通过。Code review 检查状态重置、可访问性与“全部已读取”边界，无未解决问题。本步仅需更新 console，不涉及后端变更。
