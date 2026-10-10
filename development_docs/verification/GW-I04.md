# GW-I04 多实例、DNS 与故障转发

2026-10-10：通过。验证提交 `e9f51fe465e962a0ccbf05bd6cf4de994159b679`，见 [gateway job 114194192544](https://github.com/JOTO-AI/SICP-DataMiddlePlatform/actions/runs/38045537916/job/114194192544)。

`bash scripts/check-gateway.sh replicas` 退出码为 0。query 按 2→4→重建 4→1→2 验证实际后端、共享快照与日志，扩缩容不重建 worker/网关。真实 deployment 阶段验证重建 worker、console 后 DNS 恢复。

六个真实容器契约全部通过，覆盖已接收 POST 断开后不重放、35 秒超时预算、请求体限制、16 MiB 慢读响应及有限缓冲、不写响应临时文件、日志脱敏、错误状态/JSON 传递和管理 no-store。该规模仅证明功能边界，不代表生产吞吐验收。

五个专项阶段退出码均为 0，最终输出 `Gateway required scenarios: passed`。工件上传仍因配额失败，运行日志可查看；不将工作流整体标记成功。
