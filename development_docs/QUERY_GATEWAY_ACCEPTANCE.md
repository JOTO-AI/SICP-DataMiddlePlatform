# query-gateway 专项验收记录

2026-10-10。状态：**开发与静态复核完成，新增 Linux CI 验证待执行；专项尚未验收通过。** 按用户“先开发完，不管配额”要求合并完成剩余开发，不继续等待或处理存储额度。

GW-P01.01 原提交 `2ea9176` 的四个协议测试曾通过；该结果不代表本次完整变更通过。本次 SHA、Actions 链接、运行数量和退出码应在推送后的实际运行结束后记录，未执行、缺失和跳过均不勾选。

| 项目 | 已实现的验证入口 | 当前运行状态 |
| --- | --- | --- |
| GW-T01 | `test_gateway_config.py`、`test_gateway_runtime.py` 的路径/端口/占位用例 | 新增部分待 CI |
| GW-T02 | `gateway_container_checks.py` 真实 Nginx 容器契约 | 待 CI |
| GW-T03 | Python worker/配置契约、既有 Go session/proxy 测试、Compose 配置检查 | 待本次 CI |
| GW-T04 | `test_gateway_runtime.py` 的 Operations 与验收缺项检查 | 待 CI |
| GW-I01 | 真实部署 setup/smoke，两类空配置、SPA、相对跳转、管理/业务鉴权、单端口；容器内外白名单 | 待 CI |
| GW-I02 | 真实 Nginx 普通/异常证书、权限/设备/目录/缺失、错误 CA/域名/过期、无依赖预检 | 待 CI |
| GW-I03 | 真实 HTTP/HTTPS Chromium 会话/CSRF/查询、Vite HTTP；合成上游逐 location 头检查 | 待 CI |
| GW-I04 | 多副本 2→4→重建→1→2、共享快照/日志、worker/console DNS 恢复、POST 不重放、请求/缓冲/超时 | 待 CI |
| GW-I05 | 同一浏览器双向切换、Cookie 清理、先退出分支、续期保留会话、无效预检保留旧入口、失败中断恢复、整套旧版回退 | 待 CI |

自动入口：`bash scripts/check.sh`、既有 Compose/integration/frontend 作业，以及新 `gateway` 作业的 `bash scripts/check-gateway.sh`。gateway 报告生成器要求所有必需组退出码为 0，真实容器及浏览器测试无跳过。工件仅包含测试结果与脱敏统计；测试证书私钥、Cookie、完整环境不上传。

夹具固定：域名 `sicp-gateway.test`；真实部署端口 28080，合成网关 28443；项目 `sicp-gateway-http`，合成网络 `sicp-gateway-contracts`；CA 有效 3 天、站点证书 2 天、过期证书 -1 天；慢读响应 16 MiB、每 64 KiB 等待 10 ms；POST 超时预算 35 秒、断言上限 42 秒；回退基线 `3443eaeafe30e5870dc5c5d84ddfa7e19df517e9`。这些规模用于功能边界，不能推导生产容量。

生产变更不涉及数据库结构或数据布局。迁移、HTTPS URL/CA、文件权限、续期、失败恢复和整套回退见 [部署说明](../deploy/README.md)。本报告只覆盖网关专项，不勾选第二阶段业务接口。

本次本地静态检查：Python AST、Compose/Actions YAML 解析、逐文件 Bash 语法、`vue-tsc --noEmit --project frontend/tsconfig.json` 和 `git diff --check` 通过。曾尝试通用 `tsc`，其无法解析仓库 Vue SFC；改用项目已有 `vue-tsc` 后通过。未在本地执行单元、容器或浏览器测试。
