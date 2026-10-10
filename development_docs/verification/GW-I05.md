# GW-I05 协议切换、续期与回退

2026-10-10：通过。验证提交 `e9f51fe465e962a0ccbf05bd6cf4de994159b679`，见 [gateway job 114194192544](https://github.com/JOTO-AI/SICP-DataMiddlePlatform/actions/runs/38045537916/job/114194192544)。

`bash scripts/check-gateway.sh browser` 两个方法均通过，0 fail / 0 skip。保持同一数据目录和浏览器完成 HTTP→HTTPS→HTTP，覆盖退出与保留旧 Secure Cookie 两种客户端；清理旧 Cookie 后重新登录。原子替换证书后只重建网关，可信连接呈现新指纹且会话保留。无效预检保留原容器，变更中注入失败后明确失败并手动恢复。

`bash scripts/check-gateway.sh rollback` 退出码为 0：成套回退至固定基线 `3443eaeafe30e5870dc5c5d84ddfa7e19df517e9` 的镜像及配置，旧查询/管理/页面入口可用，原业务数据保留。最终验收要求 HTTPS 和续期指纹有效且不同，HTTP 阶段存在，缺项不能通过。

五个专项阶段退出码均为 0，最终输出 `Gateway required scenarios: passed`。工件上传仍因配额失败，运行日志可查看；不将工作流整体标记成功。
