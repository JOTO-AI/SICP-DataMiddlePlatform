# GW-I02 HTTPS 与错误证书边界

2026-10-10：通过。验证提交 `e9f51fe465e962a0ccbf05bd6cf4de994159b679`，见 [gateway job 114194192544](https://github.com/JOTO-AI/SICP-DataMiddlePlatform/actions/runs/38045537916/job/114194192544)。

`bash scripts/check-gateway.sh contracts` 的六个真实 Nginx 测试全部通过，0 fail / 0 skip，49.225 秒。覆盖受信 CA/域名成功，错误 CA、错误域名、过期证书拒绝；半配置、缺失文件、目录、设备、不可读文件、无效 PEM、加密或错配私钥拒绝且不开放监听。

无上游的 check-config 能成功且不监听；UID/GID 65532、只读根目录及 16 MiB tmpfs 下正常启动。真实 HTTPS 部署和浏览器使用受信测试 CA，未关闭证书校验。可信连接取得的叶证书 SHA256 与独立文件预期比较，并生成供最终验收检查的证书记录。

五个专项阶段退出码均为 0，最终输出 `Gateway required scenarios: passed`。工件上传仍因配额失败，运行日志可查看；不将工作流整体标记成功。
