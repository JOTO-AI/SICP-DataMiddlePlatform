# SICP 本地数据中台

第一阶段 P01—P15 已实现：四个业务查询接口、十四类源采集、双份构建与发布、增量/强制全量、未决恢复、日志通知、独立管理认证及 Linux 部署。单元测试和 I01—I06 集成测试在 GitHub Actions Linux runner 上串行验收，结果见[第一阶段验收报告](development_docs/PHASE1_ACCEPTANCE.md)。

## Docker Compose 一键部署

要求 Docker Engine 或 Docker Desktop（Linux 容器模式），Compose ≥ 2.30；镜像使用 linux/amd64。无需本机安装 Go、Node.js 或 Bash，Windows PowerShell 也可运行。在仓库根目录执行：

```sh
cp .env.example .env
# 编辑 .env：填写数据库密码、业务/管理密钥、真实上游；SMTP 按需配置
docker compose up -d --build --wait --wait-timeout 300
```

Windows PowerShell 使用 `Copy-Item .env.example .env`。复制只需首次执行，已有 `.env` 时直接修改。

打开控制台 <http://127.0.0.1:18082/console/>。默认 `ENVIRONMENT=production`，Compose 启动 PostgreSQL、Redis、查询服务、同步服务和前端，完成卷权限初始化与数据库迁移后才启动应用。首次构建需要联网拉取基础镜像与依赖。

上游认证填写 `SOURCE_APP_ID` 和 `SOURCE_APP_SECRET`（上游开发者首页获取），无需填写临时 `SOURCE_TOKEN`。worker 自动获取 Token；响应带有效 `expires_in`（秒）时在到期前更新，否则每次分页前获取。HTTP 或业务码 401/403 时重新获取并重试当前页一次。已有 `.env` 请将 `SOURCE_TOKEN` 替换为这两个应用凭据。

worker 会自动初始化全新环境，之后每两小时自动调度增量同步。使用 `.env` 中的 `ADMIN_USERNAME` 和 `ADMIN_PASSWORD` 登录控制台，在“更新任务”查看记录与进度；支持立即增量同步、强制全量、单表强制更新和失败重跑。控制台无需填写业务密钥或管理令牌。容器状态和日志可通过以下命令查看：

```sh
docker compose ps
docker compose logs -f query worker
docker compose down
```

`up` 启动后会自动初始化全新环境；完成前 query `/readyz` 未就绪属于预期，Compose 使用 `/livez` 检查应用是否启动。`down` 保留命名卷，再次执行启动命令可复用数据。默认项目名为 `sicp-production`；开发模式为 `sicp-development`，两者使用不同数据卷。已有部署升级前先停应用并备份，详见[部署说明](deploy/README.md)。

所有服务（包括 console）定义在根目录唯一的 `compose.yaml` 中，统一配置模板为根目录 `.env.example`，实际部署只编辑 `.env`。Compose 按服务传入各自需要的变量。旧的 `deploy/runtime/*.env` 不再读取，升级前请将自定义值迁入 `.env`；数据库端点使用共享的 `DATABASE_*_URL`（不含账号密码），四个账号密码各配置一次，服务自动复用。默认仅监听本机，外部入口可另行配置反向代理和 HTTPS。

## 开发模式

在同一份 `.env` 将 `ENVIRONMENT=production` 改为 `ENVIRONMENT=development`，保留 `COMPOSE_PROFILES=${ENVIRONMENT}` 和 `SICP_PROJECT=sicp-${ENVIRONMENT}`，再执行相同启动命令。数据库密码、业务/管理密钥仍需填写；真实上游和邮件字段可留空。开发模式自动启动 Mock，读取仓库中的合成数据，不访问已填写的真实上游或 SMTP。六个 `MAIL_*` 参数全空时关闭邮件；需要测试邮件时设置 `MAIL_ADDRESS=mock-mail:1025`，由本地接收器接收。

切换前先在原环境执行 `docker compose down`，再修改环境值，避免端口冲突；数据卷保留且按默认项目名隔离。旧版 `sicp-phase1` 项目不会自动改名或搬迁数据，升级前按[部署说明](deploy/README.md)确认目标环境。

## Bash 部署入口

Linux / Bash 脚本用于同一套 Compose 的分步运维，`deploy` 同样包含控制台。服务、CI 与镜像统一使用 Go **1.26.8**。

```bash
./scripts/sicp.sh deploy
./scripts/sicp.sh status
```

`deploy` 准备配置、构建镜像、创建持久卷、迁移数据库并启动服务；数据初始化和同步由后台自动执行，前端提供运行记录与维护入口。默认 query、worker 分别向宿主机 `127.0.0.1:18080`、`127.0.0.1:18081` 提供 HTTP 服务。PG 和 Redis 仅连接内部网络；正式模式的 worker 使用配置的真实上游，邮件通知按需配置 SMTP；六个 `MAIL_*` 参数全部留空时关闭。Mock 仅在开发模式启用。

完整职责划分、失败处理及验收方案见[自动运行与 ctl 移除方案](development_docs/AUTOMATION_AND_CTL_REMOVAL.md)。

初始化时，一张表采集失败后会继续采集其他表，每张表单独计算采集预算。任务结束后，在任务详情点击失败表的“强制更新此表”，或在人工维护中选择“单表强制更新”。所选表从第一页完整重采，其余表复用已校验完整的持久化数据；尚缺其他表时保留本次成果，继续逐张补齐。只有十四张表完整、构建与校验通过后才统一发布，期间业务查询仍显示未就绪。“重新初始化”会复用完整表并重采全部待补齐表。重启不会丢失持久卷中完整表的采集结果。

历史数据恢复时，概览与任务详情分别显示“正在校验历史数据”和“正在复用历史数据”，页数按当前阶段统计。校验达到 100% 后还需要复制保存，复用完成才计入已完成表；尚未轮到的表显示“等待处理”。

采集完成后，控制台继续显示当前执行阶段，包括整批校验、逐表构建、写入数据库副本、业务校验、发布及同步进度确认，并显示阶段耗时与每 10 秒上报的执行器心跳。超过 45 秒未收到心跳时提示状态待确认；心跳仅表示执行器在线，不作为数据进度或发布成功的依据。

写入阶段显示已提交行数/批数，内容校验显示已核对行数，业务校验显示实际案例数和最后进展时间。总量未知时不显示百分比；心跳正常但 45 秒没有工作量进展时单独提示。100% 行进度仍需等待完整校验与发布，任务终态后计时停止。

本地表数据构建支持 `BUILD_TABLE_CONCURRENCY=2`（默认 2，允许 1—4），与 `SOURCE_PAGE_CONCURRENCY` 独立，修改后重新创建 worker 容器生效。不同表可同时排序、合并；构建组开始与结束各做一次完整输入校验，每页读取仍校验内容，避免每张表都重复扫描全部采集数据。所有表完成并通过复核后才生成完整构建清单，某表失败会取消并等待同组任务结束，数据库副本写入与发布继续遵循既有顺序。控制台会列出正在并行构建的表。每张表的缓冲、排序临时文件独立，资源需求随并发数增加；增量合并仍会读取完整旧数据，本次没有改为只写变化行。

后台完整核对和业务校验使用 `BUILD_CHECK_TIMEOUT`（默认 `30m`，允许 `1s`—`1h`）作为每阶段保护期限，SQL 同步结束后才释放读取资格。完整核对按可信清单读取至结束，单表最多声明 1000 万行；流式批载荷上限 16 MiB，业务计算收集的累计源载荷上限 64 MiB。候选与旧副本检查均使用后台预算；前台仍为 10 万行／64 MiB／20 秒。阶段期限不是端到端 30 分钟达标证明，超过保留缓冲的业务案例会明确失败，不能截断后算成功。

升级本专项版本前，须在受控部署窗口对四个业务槽位应用 `business_003`：增加 C 排序和关联索引，以及 worker 更新统计所需的 MAINTAIN 权限。迁移在事务内执行，可能等待／持有表锁；不要把 CONCURRENTLY 塞入现有迁移器。每次表导入结束后同步执行 ANALYZE，耗时计入构建。二进制回退可保留新增索引；具体校验和回退边界见 [B03 记录](development_docs/verification/B03.md)。

worker 默认 `BUILD_IMPORT_MODE=copy` 使用事务内有界 COPY；需要对照或回退时显式设置 `insert` 并重新创建 worker。两条路径保留相同的类型绑定、主键约束、统计更新和完整核对，COPY 错误不自动回退。后台读取使用 1000 行批次，前台限制不变。

后台业务校验每接口最多 100 万个独立案例，学期身份集合上限 32 MiB，会话关联缓存上限 32 MiB；仍完整执行案例，超限明确失败。缓存只在固定组合、槽位与所有权读取资格内有效，不跨进程复用校验成功结论。

初始化完成后，单表更新沿用当前已发布版本中的其他表，只向上游请求所选表；全部数据仍共同构建、校验与发布。只有所选表推进同步时间，其余表保留原进度。接口为 `POST /admin/v1/tables/{table}/force-full`，使用与强制全量相同的登录会话、防跨站凭据及 `Idempotency-Key`，请求体为 `{}`；已有任务执行或恢复时返回 `busy`，需等待其结束。

强制全量更新同样逐表容错：某表失败后继续处理其他表，并保存完整结果。任务结束后，可在详情点击“重试未完成的表”，或逐张“强制更新此表”。当最近失败任务为强制全量时，单表操作会关联并复用该任务的完整结果；其他缺失表不会用旧发布版本代替，全部补齐、校验通过后才自动发布。直接点击“强制全量更新”仍会从头采集所有表。若期间发布内容或成功同步进度已经变化，旧采集结果不再复用，以避免覆盖较新的数据。

修复强制全量时，保留修复前的成功同步时间，避免把复用的旧数据误记为修复时刻的数据。发布完成后可点击“立即增量同步”追平这段时间的变化。这里不自动追加追平任务，也不改变增量失败时从上次已确认进度重试的行为。

初始化完成后可点击“立即增量同步”，从所有表各自已确认的同步进度采集增量。有变化时自动构建、校验并发布，无需再确认发布；无变化时显示“检查完成，无变化”并确认同步进度。失败不会发布部分结果或推进成功进度。接口为 `POST /admin/v1/tasks/incremental`，使用登录会话、防跨站凭据、`Idempotency-Key` 和空请求体 `{}`。它与每两小时的定时增量共用执行流程；手动任务开始后，下次计划时间按开始时间顺延两小时。初始化／单表修复后的自动追平暂不启用，需要时在初始化完成后手动触发。

`prepare` 仅在根目录 `.env` 不存在时复制 `.env.example`，保留已有配置。`.env` 被 Git 忽略，由 Compose 解析，不要作为 Shell 脚本执行。

外部业务请求使用 `X-API-Key`；控制台与命令行管理使用管理员账号登录后的会话，后端校验身份及防跨站请求凭据。`/livez` 表示进程存活，`/readyz` 按合法活动组合、实际依赖及恢复状态判断就绪。响应丢失时使用相同幂等键重试；失败重跑生成关联的新任务，历史不改写；重启先恢复未决任务。

详细操作见[部署说明](deploy/README.md)和[运行配置](development_docs/P15_RUNTIME_CONFIGURATION.md)。

## 简易前端控制台

`frontend/` 提供 Phase 1 中文控制台，已包含在根目录 Compose 部署中。前端开发时可在 `frontend` 执行 `npm ci`、`npm run dev`，访问 `http://127.0.0.1:5173/console/` 并填写业务与管理凭据。Windows PowerShell 可使用 `npm.cmd`。详细使用说明见 [前端 README](frontend/README.md)。

## 验证与文档

构建专项的实现范围、待补故障/容量矩阵与复核记录见 [B10 交付记录](development_docs/verification/B10.md)，升级迁移、失败恢复与二进制回退见 [专项升级说明](development_docs/BUILD_TO_READY_ROLLOUT.md)。GitHub 核心容量验收可手动启用 BP01—BP05 各 3 次运行，缺失或超时样本不能通过；该汇总不代表全部 BP/BI 或 30 分钟目标已经完成。

[Linux CI](.github/workflows/ci.yml) 先运行 `scripts/check.sh`（单元测试、race、vet、语法检查、Linux 构建）及生命周期重复回归，再构建镜像、串行执行 I01—I06。普通单测不启用 integration 标签；集成测试仅在 GitHub 的隔离 PG/Redis/Mock 环境中运行。以下为工作流中的检查和构建入口：

```bash
./scripts/check.sh
bash scripts/sicp.sh build
```

检查需要 GCC/libc 开发头文件以运行 Go race detector；正式二进制禁用 CGO。`linux-unit-evidence` 和 `linux-integration-evidence` artifact 保存测试 JSON、覆盖率、容器和进程日志。容器使用非 root 用户、只读根文件系统、持久卷、内嵌时区数据及系统 CA 证书。I06 已覆盖空卷部署、四接口、管理受理、重启恢复、完整更新流程和真实 SIGTERM/SIGINT。

详细契约见[公共查询契约](development_docs/P06_QUERY_CONTRACT.md)、[字段矩阵](development_docs/P01_API_FIELD_MATRIX.md)和[契约启用状态](development_docs/P01_PENDING_RULES.md)；逐步开发与验收证据见[计划](development_docs/PHASE1_DETAILED_DEVELOPMENT_PLAN.md)及 `development_docs/verification/`。当前验收使用合成数据；生产容量和真实源联调属于后续阶段。
