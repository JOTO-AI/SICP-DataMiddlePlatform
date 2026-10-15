# 上游统一错误诊断验证（2026-10-15）

基线：`bc41a5c31e94650e3a03cd6532dda8f57cebe599`。本次仅修改上游诊断采集、日志读写投影、任务详情展示及相应测试和文档。

## Code review

- 认证与数据请求共享 `diagnostic.Request` 和采集逻辑；401 后刷新认证失败保留两次请求，不改变认证刷新和业务重试策略。
- 未知分类归为 `unknown`，保留具体原因；网络异常先移除 URL 和配置凭据，再截断。正文递归过滤业务数据和敏感字段，HTML 作为文本展示。
- 修复审查发现的遗漏：请求成功后在冷却检查失败仍保留真实状态；日志详情接口返回新字段；旧认证记录继续展示失败原因。
- 事件结束重试复制诊断值并拒绝更改历史事实。较长组合摘要按事件容量收缩并标记截断，避免丢弃整条记录。
- 成功认证数据不写入摘要；HTTP 200 的非成功错误页面仍保留安全摘要。旧记录兼容，不补造正文或 HTTP 状态。
- 修改完成后复查，无待解决的本次变更问题。未修改重试次数、发布流程、数据库结构或生产部署。

## 验证

- Linux/WSL：`internal/diagnostic`、`internal/ingest`、`internal/events`、`internal/observability` 完整测试通过；最后的 HTTP 200 认证错误页调整另跑相关 Token 摘要测试通过。
- `internal/logquery` 除下述基线失败外通过，新增日志详情诊断投影测试通过。
- `go vet`：diagnostic、ingest、events、observability、logquery、admin 通过。
- 前端 `npm run build` 通过；Playwright 三项诊断场景通过，覆盖未知异常、没有响应、业务错误、401→502、旧记录、空正文、文本转义和移动端宽度。已检查移动端截图。
- `git diff --check` 通过。

扩大测试发现两项既有失败，并在未修改的上述基线快照复现：`TestT30_04To06OrderedFramesWriteFailureAndConnectionRelease/a` 的 `frames buffered`；`TestT31_10OverviewSeparatesDependencyReadinessAndTaskTimes/ready` 的登录 401。它们不属于此次修复，不能将扩大测试表述为全绿。

部署需同步升级 worker、全部 query 读端和 console；新事件写入后不能单独回滚到不识别 `diagnostics` 的旧读取器。服务器运行验证尚未执行。
