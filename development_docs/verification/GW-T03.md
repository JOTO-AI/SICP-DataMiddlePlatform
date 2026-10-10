# GW-T03 worker、脚本与鉴权回归

通过证据：提交 `4644ff2d0f8776dd38ef2773e752b74616ac280c`，[CI 38044353260](https://github.com/JOTO-AI/SICP-DataMiddlePlatform/actions/runs/38044353260)。unit job `114190756739` 的测试、构建、配置检查步骤通过；gateway job `114190756694` 的真实浏览器阶段通过。工件上传失败另记，不伪报工作流整体成功。

Python `WorkerCookie` 三个方法对应 GW-T03-01—03；`Configuration` 的 origins/Compose 字面美元/配置与 CA 拒绝三个方法对应 07—09。独立 Vite 浏览器方法通过实际加载 Vite 配置确认默认代理 18080，并通过 HTTP 开发服务器连接真实网关完成登录和查询。

GW-T03-04—06 复用 `TestSessionLoginAuthorizationLogoutAndExpiry`、`TestConsoleProxyKeepsSecretsServerSideAndRejectsRedirects`、`TestTaskHistoryRequiresSessionAndDoesNotExposeStoreErrors`，覆盖无会话/业务 key 拒绝、错误 CSRF、正确请求、服务端注入 key 与剥离 Cookie/Authorization 等。真实 HTTP/HTTPS 浏览器及 HTTPS 单表更新补足网关接入后的链路证据。

Compose 配置检查实际通过 production/development、双协议、非默认端口、字面美元/空格路径、只读不创建宿主路径和单一端口约束。详细 I 场景证据见 GW-I01—GW-I05。
