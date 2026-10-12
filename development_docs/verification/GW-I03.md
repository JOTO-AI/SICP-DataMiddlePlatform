# GW-I03 浏览器会话与鉴权隔离

2026-10-10：通过。验证提交 `e9f51fe465e962a0ccbf05bd6cf4de994159b679`，见 [gateway job 114194192544](https://github.com/JOTO-AI/SICP-DataMiddlePlatform/actions/runs/38045537916/job/114194192544)。

`bash scripts/check-gateway.sh browser` 的两个 Chromium 方法全部通过，0 fail / 0 skip，合计 1.9 分钟。真实域名 `sicp-gateway.test` 下完成 HTTP/HTTPS 登录、刷新、查询、Cookie Secure 与 CSRF 检查；HTTPS smoke 还执行真实单表更新。业务 key 不授予管理权限，worker 的内部查询链路可用。

独立 HTTP Vite 测试加载真实配置确认默认代理为 18080，并连接真实网关完成登录/查询。六个容器契约全部通过，逐 location 检查可信转发头、管理分支清除凭据与业务分支保留业务 key；既有 Go session/proxy 回归补足服务端鉴权边界。

五个专项阶段退出码均为 0，最终输出 `Gateway required scenarios: passed`。工件上传仍因配额失败，运行日志可查看；不将工作流整体标记成功。
