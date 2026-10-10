# GW-I01 默认 HTTP 与统一路径分流

2026-10-10：通过。验证提交 `e9f51fe465e962a0ccbf05bd6cf4de994159b679`，见 [gateway job 114194192544](https://github.com/JOTO-AI/SICP-DataMiddlePlatform/actions/runs/38045537916/job/114194192544)。

`bash scripts/check-gateway.sh deployment` 的真实 Compose setup/smoke 退出码为 0。两项 TLS 配置分别未设置及为空时均完成部署；验证 `/dev/null` 占位、SPA 深链、登录、业务查询、管理鉴权、未知和相似路径拒绝、内部/外部探针边界。仅网关 8443 发布至宿主机，旧 worker/console 端口不发布。

`contracts` 的六个真实 Nginx 测试全部通过（0 fail / 0 skip，49.225 秒），覆盖方法、路径、查询参数、请求体、相对跳转及错误方法拒绝。网关使用 UID/GID 65532、只读根目录和受限 tmpfs。

五个专项阶段退出码均为 0，最终输出 `Gateway required scenarios: passed`。工件上传仍因配额失败，运行日志可查看；不将工作流整体标记成功。
