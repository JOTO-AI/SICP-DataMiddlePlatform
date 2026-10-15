# 后端失败诊断捕获

这些 JSON 来自现有后端失败场景测试，不是正常业务查询 Mock，也不是前端手写的上游响应。

- `TestUnifiedEvidenceSurvivesEveryFailureStage`：网络错误、未知 HTTP/业务错误、无效正文/数据、空正文、读取中断。
- `TestAuthenticationFailureSurvivesPersistenceAndTaskLookup`：首次认证 502，以及数据页 401 后认证 502。
- `attempt` 由真实 ingest → events → sanitizer → 测试日志存储 → TaskDiagnostics.FailedAttempts 生成。
- `envelope` 是同一持久事件的脱敏内容；`log_detail` 由 logquery 恢复该事件并通过真实 project 函数生成。日志读取记录的 sequence/received_at/epoch 使用现有 readFixture，不代表真实服务器入库。
- 每份文件的 `provenance` 标明场景；前端仅使用已有任务外壳装载这些 DTO。旧格式、损坏字段、超大业务码和未发送状态属于明确标注的协议边界变体，不冒充正常查询结果。
- 单日志库场景中的 `log_source=platform` 必须保留，事件/调用/任务关联 ID 不得再次哈希或作为原始控制任务 ID。

在仓库根目录、已配置 Go 1.26.8 的环境生成（PowerShell）：

```powershell
$env:SICP_DIAGNOSTIC_FIXTURE_DIR = "$PWD/frontend/fixtures/diagnostics"
go test ./internal/observability -run 'TestUnifiedEvidenceSurvivesEveryFailureStage|TestAuthenticationFailureSurvivesPersistenceAndTaskLookup' -count=1
go test ./internal/logquery -run TestExportFrontendDiagnosticDetails -count=1
Remove-Item Env:SICP_DIAGNOSTIC_FIXTURE_DIR
```

生成时只使用测试 transport 和测试数据库，不访问真实上游、数据库或用户凭据。时间/耗时来自本次后端测试运行；重新生成会改变这些值。正常业务和分页捕获仍由 `npm run check:fixtures` 的既有 150 项来源哈希校验。
