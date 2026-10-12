# query-gateway 专项验收记录

2026-10-10。**GW-P01—GW-P04 开发和复核完成，GW-T01—GW-T04 与 GW-I01—GW-I05 全部通过；适用的既有回归也已通过。** 最终运行代码提交为 `e9f51fe465e962a0ccbf05bd6cf4de994159b679`，[CI 38045537916](https://github.com/JOTO-AI/SICP-DataMiddlePlatform/actions/runs/38045537916)。仅工件归档尚未完成，原因与各项证据如下。

| 验证范围 | 实际结果 | 证据 |
| --- | --- | --- |
| GW-T01 / T03 / T04 | 20 个 Python 方法通过，0 fail / 0 skip，0.375 秒；包括全部现有服务容器的配置漂移拒绝 | [unit job](https://github.com/JOTO-AI/SICP-DataMiddlePlatform/actions/runs/38045537916/job/114194192603) |
| 既有 Go 单元回归 | 1,482 项通过（含子测试），0 fail / 0 skip；race、vet、Linux 构建及 30 次生命周期回归通过 | 同上 |
| GW-T02、GW-I01—I02 | 6 个真实 Nginx 容器方法通过，0 fail / 0 skip，49.225 秒；真实 Compose setup/smoke 退出码 0 | [gateway job](https://github.com/JOTO-AI/SICP-DataMiddlePlatform/actions/runs/38045537916/job/114194192544) |
| GW-I03 / I05 | 2 个真实 Chromium 方法通过，0 fail / 0 skip，1.9 分钟；双向切换、可信 TLS、续期会话、失败恢复及 Vite HTTP 开发链路可用 | 同上 |
| GW-I04 | query 2→4→重建 4→1→2、副本/快照/日志及 worker/console DNS 恢复通过 | 同上 |
| 整套旧版本回退 | 固定基线 `3443eaeafe30e5870dc5c5d84ddfa7e19df517e9` 的旧镜像与配置恢复，登录/查询/页面正常，业务数据保留 | 同上 |
| 前端既有回归 | 构建、Playwright 测试、console 镜像构建通过 | [frontend job](https://github.com/JOTO-AI/SICP-DataMiddlePlatform/actions/runs/38045537916/job/114194192463) |
| Compose 既有回归 | 干净检出部署、初始化及保留数据重建通过 | [compose job](https://github.com/JOTO-AI/SICP-DataMiddlePlatform/actions/runs/38045537916/job/114194192607) |
| 既有 I01—I05 集成回归 | 103 项通过（含子测试），0 fail / 0 skip，所有分组退出码 0；提交 `570827a` | [integration job](https://github.com/JOTO-AI/SICP-DataMiddlePlatform/actions/runs/38044768384/job/114192879141) |
| 既有 I06 部署回归 | 20 项通过（含子测试），0 fail / 0 skip，842.014 秒，退出码 0；提交 `570827a` | [deployment job](https://github.com/JOTO-AI/SICP-DataMiddlePlatform/actions/runs/38044768384/job/114192879115) |

既有 I01—I06 的上述证据来自 `570827a6c753539612f71becdc73b6422e75dae7`；其后 `e9f51fe` 的运行代码变更仅为续期配置漂移检查，已由该提交的 Python 契约及真实网关续期场景验证。两组旧回归自身代码没有再变动；记录时 `e9f51fe` 的同组重复回归仍在运行，未提前将这些重复运行标为成功。

网关的 contracts/deployment/replicas/browser/rollback 五组退出码均为 0，最终门禁输出 `Gateway required scenarios: passed`。门禁检查六项容器方法、两个浏览器方法和证书记录，缺失、失败、跳过不能通过；HTTPS 与续期叶证书 SHA256 来自可信连接并与独立文件预期匹配，记录检查要求两次指纹不同。不在报告中编造未取得的具体指纹值。

**CI 整体尚非成功：工件上传因 GitHub 存储配额失败。** 功能测试和工件上传分别记录；既有 integration/deployment 只依赖 unit 的实际验证输出，上传失败不会跳过测试，测试失败仍阻止后续依赖。按用户要求本轮不处理配额、不继续清理或备份；不能宣称脱敏工件已归档。上述 Actions 步骤和日志可查。

执行入口：`bash scripts/check.sh`、`bash scripts/check-gateway.sh`、既有 Compose/frontend 作业，以及独立的 I01—I05 integration、I06 deployment 作业。各场景映射见 [计划](QUERY_GATEWAY_HTTPS_DEPLOYMENT_PLAN.md) 和 `verification/GW-T*.md`、`verification/GW-I*.md`。

夹具固定：域名 `sicp-gateway.test`；真实部署端口 28080，合成网关 28443；项目 `sicp-gateway-http`，合成网络 `sicp-gateway-contracts`；CA 有效 3 天、站点证书 2 天、过期证书 -1 天；慢读响应 16 MiB、每 64 KiB 等待 10 ms；POST 超时预算 35 秒、断言上限 42 秒。这些规模验证功能边界，不代表生产容量。临时 CA/私钥在工件目录之外生成，不上传 Cookie、完整环境、私钥或浏览器 storage state。

本地仅完成静态检查：Python AST、Compose/Actions YAML 解析、逐文件 Bash 语法、`vue-tsc --noEmit --project frontend/tsconfig.json` 及 `git diff --check`；未在本地执行单元、容器或浏览器测试。代码复核及 CI 已修正构建上下文遗漏、无尾斜杠路径意外重定向、测试夹具启动等待/管理请求方法、旧独立进程测试绑定地址和续期配置漂移覆盖。

生产变更不涉及数据库结构或数据布局。迁移、URL/CA、文件权限、续期、失败恢复及整套回退见 [部署说明](../deploy/README.md)。单网关切换/续期有停机窗口；HTTPS→HTTP 须处理旧 Secure Cookie/HSTS。本报告仅覆盖网关专项，不勾选第二阶段业务接口，也不代表已部署到生产。
