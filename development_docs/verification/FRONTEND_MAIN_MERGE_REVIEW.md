# main 统一诊断接入复核

前端基线：`6c0b1d1`；合入 main：`5433ec8`（包含 `dd59480`、`bc41a5c`）。本次使用 merge 保留两侧历史，不重写已发布提交。

## 合并范围

- 后端诊断运行代码采用 main 的统一实现；保留前端分支已有的更新事件筛选、游标绑定和业务 fixture 导出。后端额外改动仅为测试数据导出。
- 沿用当前导航、任务详情和日志页。共享 RequestDiagnostics 优先消费 diagnostics，缺失该字段时兼容旧 response/authentication；不创建新的日志库、调用历史接口或错误页面。
- 任务保留来源、页码、尝试、原有日志跳转和部分读取失败时的已确认记录；日志详情保留精确原文。
- 请求级 HTTP、字符串业务码、时间、阶段、错误类型、脱敏摘要、空正文及截断/读取中断独立展示；不把认证状态覆盖到数据请求。
- 9 份新增错误场景捕获来自实际后端失败测试的持久化与 DTO 投影，来源及再生成命令见 frontend/fixtures/diagnostics/README.md；正常业务 Mock 不变。

## Review 与修复

- 检查了 backend → persisted event → FailedAttempt/logquery.Detail → frontend 校验 → 页面 → 日志跳转链路。
- 发现并修复单日志库契约遗漏：platform 可以实际包含 call/attempt；历史、详情和调用尝试查询保留后端来源，不强行改为 upstream。跨来源/世代/调用号混合仍拒绝。
- 诊断字段损坏时保留有效的其他请求及任务/日志事实，显示不完整提示；不会静默用旧字段重复展示或推断成功。
- 新旧字段不双重展示；未知分类不靠前端枚举拒绝；大业务码保持字符串。错误正文经 JsonText 有界、纯文本展示，没有 HTML 注入。
- 关联号仅用于日志查询，保持 correlation 格式，不映射成原始任务控制 ID。更新筛选与游标实现未被覆盖。
- 核对升级顺序与回退限制：先更新全部 query 读端和 console，再恢复新 worker 写入；已有新诊断事件时不能单独回滚旧读端。

## 验证

- 117 项前端单元测试通过；诊断专项浏览器回归 6/6 通过（loopback、intranet-http），含任务到日志跳转和 390px 截图。
- 诊断、ingest、events 完整 Go 测试通过；observability 相关认证、统一诊断、响应及双日志测试通过；logquery 诊断投影/导出测试通过；operations/admin 更新筛选测试通过。
- Windows observability 全包有 4 项文件日志缓冲测试失败，均在未修改的 main 5433ec8 隔离快照上复现：TestDirectoryDurableReplayAndConflict、TestDirectoryBatchBoundDoesNotPreventDrainingBacklog、TestT28_13RealJournalOwnershipAndModeSurviveReopen、TestQuotaCountsOrphansAndAllowsIdempotentReplayWhenFull。不能将后端全包表述为全绿；相关日志文件运行代码与 main 一致。
- main 原有复核另记录日志 SSE frames buffered 和概览登录 401 的两项基线失败，本轮没有恢复全套后端 T/OI 验收。
- 最终完整前端功能回归 366/366 通过（两个 origin、两个 worker、13.0 分钟）。早期诊断专项暴露单日志库拒绝问题并已修复；中间完整运行因测试标题选择器误改而中止，恢复原断言后重新完整运行通过。最终 5 项诊断单元复测通过。
- 最终 vue-tsc/生产构建、57 文件产物检查、150 项既有 fixture 来源 hash、28 图追踪及 20 组主题对比度通过；生产 JS 500302 字节。7 个相关 Go 包的 go vet 通过。
- Code review 结论：本次合并与适配通过，无新增未解决阻塞项。任务/日志的 390px 截图已检查，无横向溢出；完整视觉与真实服务验收不在本结论内。
- 汇总证据及报告/捕获 SHA-256 见 [机器可读验证记录](frontend_main_merge_verification.json)。完整报告与截图位于 artifacts/frontend-main-merge/final-regression*；产物未提交。
- 服务器联调和真实网关验收继续按用户要求延后；F12-02/-03 保持未勾选。
