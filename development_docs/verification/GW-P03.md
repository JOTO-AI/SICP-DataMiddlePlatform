# GW-P03 统一部署接入复核

2026-10-10：GW-P03.01—03 实现、测试代码及静态复核完成；新增测试待 Linux CI。

Compose 仅发布 `SICP_GATEWAY_PORT → 8443`，证书使用只读长格式绑定挂载、`create_host_path: false`，仅空值使用 `/dev/null`。worker 固定标记受网关管理，消费相同 TLS 路径并派生 Cookie Secure，不挂载私钥。未受管理时保留旧 Cookie 配置；未知标记拒绝。

console 只服务静态资源与 SPA；Vite 默认代理新网关 18080。Compose 烟测、学生成绩查询、多副本检查、表更新检查及旧 I06 部署测试已迁移；内部 worker 探针通过隔离私有网络检查，不恢复独立宿主端口。旧 Go 测试的多网络地址检查按项目 sicp 网络解析，并在服务启动/重启后更新地址。

部署读取复用 `gateway_runtime.Compose`，只由 Docker Compose 解释 dotenv；渲染 JSON 的美元转义还原一次，不再次执行或插值。配置/子进程失败只报告固定类别。显式 URL 优先、HTTP 可从配置推导、HTTPS 要求实际域名；CA 保持验证。查询工具在显式 URL 和环境 API key 均存在时无需 Docker。

测试映射：GW-T03-01—03、07—09 在 `test_gateway_runtime.py`；04—06 复用 `internal/admin/session_test.go` 的 `TestSessionLoginAuthorizationLogoutAndExpiry`、`TestConsoleProxyKeepsSecretsServerSideAndRejectsRedirects` 及 `task_list_test.go` 的 `TestTaskHistoryRequiresSessionAndDoesNotExposeStoreErrors`，新网关下的真实浏览器另验证无会话、错误 CSRF、正确请求、worker 查询代理。Compose 配置检查新增 development/production × HTTP/HTTPS、字面美元路径、挂载与单一端口约束。

说明文档、模板及实际入口均已同步。旧三个端口变量仅在迁移说明和固定旧版本回退夹具中保留，不再消费于默认部署。
