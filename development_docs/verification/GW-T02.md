# GW-T02 真实网关模板与启动验证

提交 `527c874941b087c2102e4fef946e95583f46b5ba`，[CI 38043598253](https://github.com/JOTO-AI/SICP-DataMiddlePlatform/actions/runs/38043598253)，gateway job `114188565997`。`python3 scripts/gateway_container_checks.py` 实际运行 6 个方法，6 pass / 0 fail / 0 skip，51.332 秒，退出码 0。

| 实际方法 | 主要契约 |
| --- | --- |
| `test_gw_t02_01_02_and_i01_route_allowlist` | 双协议真实 Nginx、路径/方法/查询串/正文保留、相对跳转、裸前缀 404、内部白名单、UID/只读边界 |
| `test_gw_t02_03_and_i03_headers_each_location` | 各 location 的 Host/协议头重建、伪造转发头清除、管理凭据隔离、Cookie/CSRF 保留 |
| `test_gw_t02_04_05_invalid_files_no_listener` | 缺失、目录、设备、不可读、无效 PEM、错配、加密私钥拒绝 |
| `test_gw_t02_05_06_check_only_no_dependencies_and_replica_limits` | HTTP 占位、不依赖上游、不启动监听、副本 1/32 与错误值、半配置拒绝 |
| `test_gw_i02_client_identity_and_expiry` | 受信域名成功；错误 CA、IP 域名不匹配、过期证书拒绝 |
| `test_gw_i04_limits_errors_no_post_replay_and_bounded_storage` | JSON 错误状态保留、请求体限制、双上游 POST 断开只接收一次、超时、16 MiB 慢读及无响应临时文件、日志脱敏 |

该次 gateway 作业随后在真实 Compose setup 失败，不能把 T02 的成功作为 I01—I05 全部通过。已修正鉴权检查使用的请求方法，并在后续提交继续验证；工件上传失败亦单独保留。
