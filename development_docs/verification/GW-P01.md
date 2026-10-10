# GW-P01 协议与部署配置规则

更新：2026-10-10。GW-P01.01 已编写实现和测试并完成静态代码复核，等待提交及对应 SHA 的完整 GitHub Actions Linux CI。GW-P01 阶段尚未完成；GW-P01.02 和后续步骤尚未开始。

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
