# GW-P01 协议与部署配置规则

更新：2026-10-10。GW-P01.01 已实现、复核并推送 `2ea9176`；对应 Linux CI 的测试和构建步骤成功，但工件上传因存储配额耗尽而失败，完整 CI 未通过。GW-P01 阶段尚未完成；GW-P01.02 和后续步骤尚未开始。

## GW-P01.01 协议选择与错误分类

- `deploy/gateway-config.sh` 提供供网关及 worker 后续共同引用的 POSIX sh 函数 `sicp_gateway_protocol`。两项证书路径未设置或为空时输出 `http`，成对非空时输出 `https`；只填一项返回 `missing_pair`，纯空白返回 `whitespace_only`。错误只包含配置项名称和类别，失败时标准输出为空。
- 函数只读取原始环境变量，不读取 `.env`、证书或私钥，不执行配置值、不修改环境或调用者 Shell 选项；不依赖 `ENVIRONMENT`、端口、外部命令或额外运行时。协议选择成功不代表路径或证书有效。
- `scripts/test_gateway_config.py` 为 GW-T01-01—03 提供四个测试方法及独立预期，使用合成路径、隔离环境和实际 `/bin/sh -eu` 调用。测试覆盖未设置/空值、环境及端口组合、双向半配置、六种 ASCII 空白及混合空白、固定错误输出。
- `scripts/check.sh` 添加共享组件的 POSIX Shell 语法检查及 Linux 单测入口；现有 `.github/workflows/ci.yml` 的 unit 作业直接调用此脚本，继续执行既有完整 CI。测试输出和退出码保存为 `artifacts/gateway-config-tests.txt`、`artifacts/gateway-config-exit-code.txt`，由现有工件步骤上传；单测失败或证据写入失败均使检查失败。
- 本小步尚不修改镜像 COPY、默认入口、Compose、Cookie 或端口发布；共享组件接入及文件验证按 GW-P02/GW-P03 执行。路径和端口边界在 GW-P01.02 实现。

## 代码复核与回退边界

- 2026-10-10 按用户要求再次逐项 code review：核对协议分支、失败退出、`set -eu` 兼容、错误脱敏、独立测试预期、CI 管道状态及现有工作流调用关系，未发现本小步的阻断问题。用户已授权复核通过后提交并推送；仅提交本小步的五个文件。
- 检查所有变量展开均有引号，并使用未设置安全的展开形式，兼容入口现有 `set -eu`。
- 先检查纯空白，再判断成对关系，避免把空白作为关闭 HTTPS 的方式；两个值均为空时才选择 HTTP，不按环境名或端口猜测协议。
- 错误分支明确返回非零，且没有路径或私钥正文输出；测试按完整标准输出、标准错误和退出码断言。
- 复核修正 CI 证据管道处理：同时检查 Python 与 `tee` 的退出码，避免证据写入失败被误计为成功。
- 回退仅涉及新增公共脚本、测试、CI 调用和记录，不改变运行中的入口，不涉及数据库或数据卷。

本地已完成源码静态复核、改动文件尾随空白检查及 `git diff --check -- scripts/check.sh`（退出码 0）。尝试 Python AST 语法检查时，本机 `python` 不在 PATH，`py -3` 也报告未安装 Python 3；因此 AST 检查未执行成功，不计为通过。未执行单元测试、容器测试或部署。待本小步提交及完整 Linux CI 成功后，才能进入 GW-P01.02；CI 失败先修复当前小步。

## 首次提交与 CI 结果

- 分支 `feat/query-gateway-https`，提交 `2ea9176841a042492b11f508079391016db14da8`，已通过 Git 推送并核对远端分支指向。
- [Linux CI 38040043167](https://github.com/JOTO-AI/SICP-DataMiddlePlatform/actions/runs/38040043167) 最终结果为 failure。unit 的单测/race/vet/Linux 构建、重复生命周期回归、服务镜像与部署配置检查，frontend 的构建/测试/镜像，以及 Compose 烟测步骤均 success；三者均在 upload-artifact 失败，integration 因依赖 unit 失败被 skipped。
- GitHub 错误注释明确为 `Artifact storage quota has been hit`。不能忽略工件失败或把跳过的集成作业计为通过；需恢复工件存储可用性并重跑当前提交的 CI，完整成功后才能继续。
- 按用户要求复用历史 `artifacts/ci_status.py`、`artifacts/github_ci.py`、`artifacts/ci_counts.py` 查询状态、错误和测试统计；已移除本次重复创建的查询脚本。授权凭据仅用于 API 认证，不写入文件或输出。尚未删除远端工件或修改存储额度。
- 后续定位到 `C:/Users/41058/anaconda3/python.exe`，仅执行本地测试文件 AST 解析，结果成功；单测仍仅在 Linux CI 执行。测试计数见 GW-T01。

## 工件容量修正与计划同步

- 计划文档已勾选 GW-P01.01 和实际通过的 GW-T01-01—03 子项；GW-P01/GW-T01 整组及其后步骤保持未完成，完整 CI 仍未通过。
- 复核发现 unit 工件上传整个 `artifacts/`，同时保存了三个每次可重建的 Linux 二进制。调整 CI 上传范围，仅排除 `query-service`、`sync-worker`、`db-migrate` 三个二进制；保留单测 JSON、覆盖率、网关测试输出、退出码及其他测试证据，仍执行全部 Linux 构建。
- Review：三个二进制没有跨作业下载使用点；服务镜像在对应步骤单独构建。保持 upload-artifact 的失败门禁及 `if-no-files-found: error`，不使用 continue-on-error、不跳过测试或强行标记成功。此修正减少新增占用，但不会自动释放已耗尽的远端存储。

容量修正和计划勾选已推送为 `3443eaeafe30e5870dc5c5d84ddfa7e19df517e9`。其 [CI 38040965087](https://github.com/JOTO-AI/SICP-DataMiddlePlatform/actions/runs/38040965087) 首次运行仍因工件配额失败。经用户授权，已清理精确清单中的 159 份旧 `linux-unit-evidence` 工件，释放 2232.9 MiB；远端复查清单剩余 0 份，保留单测工件 10 份、前端 26 份、Compose 25 份、集成 49 份。未修改计费设置。2026-10-10 已成功请求同一 SHA 的失败作业重跑（GitHub API 201），等待重跑结果，不提前标记完整 CI 通过。

查询、备份及清理复用本地 `artifacts/` 历史工具，并保留精确清单及删除结果。用户随后要求后续无需备份；该指令到达时已有备份进程已完成，不再新增备份步骤。

重跑第 2 次的 frontend job `114182950293` 已再次在 upload-artifact 失败，错误注释仍为 `Artifact storage quota has been hit`，并明确说明用量每 6–12 小时重新计算。远端复查剩余工件约 256.7 MiB，159 份候选全部不存在；清理已完成，但尚无配额计量恢复证据。此时其他重跑作业尚在执行，不将它们提前记为成功或失败。下一小步仍等待平台配额刷新后同一提交的完整 CI 通过；不继续重复删除清单外工件、不更改计费、不降低 CI 门槛。

## 后续授权与 GW-P01.02

用户要求不再等待配额，先完成全部剩余开发；此前逐步等待 CI 的安排由此覆盖。已实现 Linux 字面路径、控制字符、成对路径、端口范围及占位挂载校验，测试位于 `scripts/test_gateway_runtime.py`。本地仅静态复核；新增测试未运行，不变更既有 T01 通过统计。
