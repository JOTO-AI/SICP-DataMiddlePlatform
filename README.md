# SICP 本地数据中台

第一阶段 P01—P15 已实现：四个业务查询接口、十四类源采集、双份构建与发布、增量/强制全量、未决恢复、日志通知、独立管理认证及 Linux 部署。单元测试和 I01—I06 集成测试在 GitHub Actions Linux runner 上串行验收，结果见[第一阶段验收报告](development_docs/PHASE1_ACCEPTANCE.md)。

## Docker Compose 一键部署

要求 Docker Engine 或 Docker Desktop（Linux 容器模式），Compose ≥ 2.30；镜像使用 linux/amd64。无需本机安装 Go、Node.js 或 Bash，Windows PowerShell 也可运行。在仓库根目录执行：

```sh
cp .env.example .env
# 编辑 .env：填写数据库密码、业务/管理密钥、真实上游与 SMTP 配置
docker compose up -d --build --wait --wait-timeout 300
```

Windows PowerShell 使用 `Copy-Item .env.example .env`。复制只需首次执行，已有 `.env` 时直接修改。

打开控制台 <http://127.0.0.1:18082/console/>。默认 `ENVIRONMENT=production`，Compose 启动 PostgreSQL、Redis、查询服务、同步服务和前端，完成卷权限初始化与数据库迁移后才启动应用。首次构建需要联网拉取基础镜像与依赖。

上游认证填写 `SOURCE_APP_ID` 和 `SOURCE_APP_SECRET`（上游开发者首页获取），无需填写临时 `SOURCE_TOKEN`。worker 自动获取 Token；响应带有效 `expires_in`（秒）时在到期前更新，否则每次分页前获取。HTTP 或业务码 401/403 时重新获取并重试当前页一次。已有 `.env` 请将 `SOURCE_TOKEN` 替换为这两个应用凭据。

在控制台填写 `.env` 中配置的业务密钥和管理令牌，通过“更新任务 → 首次初始化”从真实上游导入数据，并查看任务完成状态。也可使用命令：

```sh
docker compose run --rm --no-deps ctl initialize initialization-001
docker compose run --rm --no-deps ctl status <返回的任务ID>
docker compose ps
docker compose logs -f query worker
docker compose down
```

`up` 只部署，不自动提交数据初始化任务；首次初始化完成前 query `/readyz` 未就绪属于预期，Compose 使用 `/livez` 检查应用是否启动。`down` 保留命名卷，再次执行启动命令可复用数据。默认项目名为 `sicp-production`；开发模式为 `sicp-development`，两者使用不同数据卷。已有部署升级前先停应用并备份，详见[部署说明](deploy/README.md)。

所有服务（包括 console）定义在根目录唯一的 `compose.yaml` 中，统一配置模板为根目录 `.env.example`，实际部署只编辑 `.env`。Compose 按服务传入各自需要的变量。旧的 `deploy/runtime/*.env` 不再读取，升级前请将自定义值迁入 `.env`；数据库端点使用共享的 `DATABASE_*_URL`（不含账号密码），四个账号密码各配置一次，服务自动复用。默认仅监听本机，外部入口可另行配置反向代理和 HTTPS。

## 开发模式

在同一份 `.env` 将 `ENVIRONMENT=production` 改为 `ENVIRONMENT=development`，保留 `COMPOSE_PROFILES=${ENVIRONMENT}` 和 `SICP_PROJECT=sicp-${ENVIRONMENT}`，再执行相同启动命令。数据库密码、业务/管理密钥仍需填写；真实上游和邮件字段可留空。开发模式自动启动 Mock，读取仓库中的合成数据并在本地接收邮件，不访问已填写的真实上游或 SMTP。

切换前先在原环境执行 `docker compose down`，再修改环境值，避免端口冲突；数据卷保留且按默认项目名隔离。旧版 `sicp-phase1` 项目不会自动改名或搬迁数据，升级前按[部署说明](deploy/README.md)确认目标环境。

## Bash 部署入口

Linux / Bash 脚本用于同一套 Compose 的分步运维，`deploy` 同样包含控制台。服务、CI 与镜像统一使用 Go **1.26.8**。

```bash
./scripts/sicp.sh deploy
./scripts/sicp.sh initialize initialization-001
./scripts/sicp.sh task <返回的任务ID>
./scripts/sicp.sh status
```

`deploy` 准备配置、构建镜像、创建持久卷、迁移数据库并启动服务；数据初始化通过管理入口显式受理，使用 task 命令确认最终完成。默认 query、worker 分别向宿主机 `127.0.0.1:18080`、`127.0.0.1:18081` 提供 HTTP 服务。PG 和 Redis 仅连接内部网络；正式模式的 worker 使用配置的真实上游与 SMTP。Mock 仅在开发模式启用。

`prepare` 仅在根目录 `.env` 不存在时复制 `.env.example`，保留已有配置。`.env` 被 Git 忽略，由 Compose 解析，不要作为 Shell 脚本执行。

业务请求使用 `X-API-Key`，管理入口使用独立 Bearer 凭据及动作权限。`/livez` 表示进程存活，`/readyz` 按合法活动组合、实际依赖及恢复状态判断就绪。响应丢失时使用相同幂等键重试；失败重跑生成关联的新任务，历史不改写；重启先恢复未决任务。

详细操作见[部署说明](deploy/README.md)和[运行配置](development_docs/P15_RUNTIME_CONFIGURATION.md)。

## 简易前端控制台

`frontend/` 提供 Phase 1 中文控制台，已包含在根目录 Compose 部署中。前端开发时可在 `frontend` 执行 `npm ci`、`npm run dev`，访问 `http://127.0.0.1:5173/console/` 并填写业务与管理凭据。Windows PowerShell 可使用 `npm.cmd`。详细使用说明见 [前端 README](frontend/README.md)。

## 验证与文档

[Linux CI](.github/workflows/ci.yml) 先运行 `scripts/check.sh`（单元测试、race、vet、语法检查、Linux 构建）及生命周期重复回归，再构建镜像、串行执行 I01—I06。普通单测不启用 integration 标签；集成测试仅在 GitHub 的隔离 PG/Redis/Mock 环境中运行。以下为工作流中的检查和构建入口：

```bash
./scripts/check.sh
bash scripts/sicp.sh build
```

检查需要 GCC/libc 开发头文件以运行 Go race detector；正式二进制禁用 CGO。`linux-unit-evidence` 和 `linux-integration-evidence` artifact 保存测试 JSON、覆盖率、容器和进程日志。容器使用非 root 用户、只读根文件系统、持久卷、内嵌时区数据及系统 CA 证书。I06 已覆盖空卷部署、四接口、管理受理、重启恢复、完整更新流程和真实 SIGTERM/SIGINT。

详细契约见[公共查询契约](development_docs/P06_QUERY_CONTRACT.md)、[字段矩阵](development_docs/P01_API_FIELD_MATRIX.md)和[契约启用状态](development_docs/P01_PENDING_RULES.md)；逐步开发与验收证据见[计划](development_docs/PHASE1_DETAILED_DEVELOPMENT_PLAN.md)及 `development_docs/verification/`。当前验收使用合成数据；生产容量和真实源联调属于后续阶段。
