# Docker Compose 部署与运维

## 一键启动（推荐）

上游采集每页条数通过 `.env` 的 `SOURCE_PAGE_SIZE` 设置，默认 `1000`，本地允许 `1`—`10000`。例如 `SOURCE_PAGE_SIZE=5000` 可将约 144 万条数据的请求从约 1440 页降到 288 页，前提是上游允许该条数且每页能在请求超时内返回；这不是对总耗时的保证。修改后重建 worker 才生效，当前任务不会动态改变分页；重启会中断尚未封存的采集。已有配置未设置该变量时继续使用 1000。

同一张表的分页并发数通过 `SOURCE_PAGE_CONCURRENCY` 设置，默认 `4`，允许 `1`—`8`，`1` 为串行。先确认第一页和总页数，再分批并行获取后续页面，按页码顺序校验、保存和报告进度；不同表仍依次采集。初始化、增量和强制全量均适用，重建 worker 后生效。每批最多保留配置数量的响应页，内存占用会随并发数和每页条数增加；建议先使用 `SOURCE_PAGE_SIZE=1000`、`SOURCE_PAGE_CONCURRENCY=4`，根据上游响应与限流情况调整，并发不保证等比例提速。批次中有页面最终失败时取消其余请求，不获取后续批次；重试、Retry-After 冷却、总条数与重复记录校验继续生效。

`STAGING_MAX_BYTES`、`TARGET_MAX_BYTES`、`SPOOL_MAX_BYTES` 默认均为 `0`，表示不设置应用层磁盘字节配额，适用于初始化、增量和强制全量。旧 `.env` 里的 256MiB/128MiB 正值不会被自动覆盖，升级时将这三项改为 `0`，重建 worker 后生效；如有部署需要，仍可显式设置正数配额。

关闭磁盘字节配额不会把全量数据一次性读进内存：单页 64MiB、排序缓冲、输出批次和日志容量仍有独立限制。采集总预算仍为 1 小时，实际磁盘写入失败会使任务报错；失败采集文件继续保留，不能直接删除仍被任务引用的文件。

安装 Docker Engine 或 Docker Desktop（Linux 容器模式）和 Compose ≥ 2.30，在仓库根目录执行，Windows PowerShell 与 Linux/macOS 终端通用：

```sh
cp .env.example .env
# 编辑 .env，填写所有必填凭据和真实上游；SMTP 按需配置
bash scripts/sicp.sh deploy --url http://127.0.0.1:18080
```

Windows PowerShell 使用 `Copy-Item .env.example .env`，部署脚本通过 WSL / Git Bash 执行。复制只需首次执行，不覆盖已有配置。

根目录唯一的 `compose.yaml` 直接定义所有服务（含 console），正式环境默认项目名 `sicp-production`。部署脚本依次初始化目录与数据库、迁移、排空旧日志缓冲，再启动 query/worker 和控制台；任何步骤失败停止部署。Compose 同样保证启动顺序：db-m/db-s/db-log 健康后运行 migrate，query/worker 等待 migrate 成功及目录权限初始化完成后启动，网关和控制台再等待应用健康。迁移失败时应用不会启动。该依赖只控制 Compose 启动顺序，运行期间日志库故障不会自动停止应用；通过 Compose 重新启动依赖链时仍需数据库健康及迁移成功。

首次部署，或停服并清空日志数据库及全部本地日志缓冲后，补齐 `.env` 即可执行 `docker compose up -d --build`，自动完成建库、建表和应用启动。已有单库数据及缓冲需要保留的升级仍按下文登记和部署流程执行，直接 up 不会补建已有 PG 数据目录中缺失的上游库，也不替代旧缓冲切换。可用 `docker compose logs volumes db-m db-s db-log migrate` 排查初始化失败。

一个独立于 M/S 的 `db-log` PostgreSQL 服务承载 `logs` / `upstream_logs` 两个逻辑库，共用 `pg-log` 卷；两库分别管理写入、读取和清理权限，实例故障与连接总额度共享。只使用 compose.yaml，无需额外 overlay。上游默认地址为 `postgres://db-log:5432/upstream_logs?sslmode=disable`，另填 `UPSTREAM_LOGGER_PASSWORD`。

控制台只有一个管理员账号：使用 ADMIN_USERNAME/ADMIN_PASSWORD 登录即可访问全部已实现管理操作，日志历史/实时读取和清理无需另配账号或启用开关。query/worker 自动用自己的数据库凭据连接两个日志库，新增 logs_007/upstream_logs_006 迁移负责读取及受限清理授权；现有实例先迁移，再重建应用。数据库服务凭据和外部 SMTP 等连接配置仍需按实际环境提供，它们不是额外的控制台账号。

模板不再包含 LOG_CURSOR_KEYS、LOG_CURSOR_ACTIVE、LOG_INTERNAL_TOKEN 和四个日志 READ/MAINTENANCE URL；现有 .env 中这些旧项可删除，当前 Compose 不再读取。内部令牌及游标签名密钥由已有 SNAPSHOT_KEY 按不同用途派生，所有副本必须一致；变更 SNAPSHOT_KEY 或切换旧显式游标密钥后，客户端需要重新查询取得新游标。真实 .env 不会被自动覆盖。

已有单库部署先补齐上游 URL/密码，保持原 AUDIT_KEY、平台地址及目录，构建后执行 `bash scripts/sicp.sh log-register`，再执行 deploy；infra 会为已有 pg-log 卷补建缺失的上游逻辑库及账号。详细步骤见 [旧缓冲切换说明](../development_docs/P28_LOG_STORAGE_CONFIGURATION.md)。此流程不自动搬迁已启用双实例部署的数据；这类部署需保留原目标，另行安排备份/恢复及拓扑标记切换。

| 服务 | 本机入口 |
| --- | --- |
| 控制台 | http://127.0.0.1:18080/console/ |
| 查询 API | http://127.0.0.1:18080 |
| 管理 API | http://127.0.0.1:18080（会话及 CSRF 校验） |

控制台使用 `.env` 中的 `ADMIN_USERNAME` 和 `ADMIN_PASSWORD` 登录，不提供默认密码。业务密钥保留在服务端；前端无需填写 API Key 或管理 Token。原有部署需删除 `MANAGEMENT_CREDENTIALS`、`MANAGEMENT_TOKEN` 并填写新的账号密码。会话有效 8 小时，worker 重启后需重新登录。统一部署按证书配置自动派生 Cookie Secure，无需手动设置。全新环境由 worker 自动初始化，之后每两小时自动调度增量同步。在“更新任务”查看记录，仅强制全量和失败重跑需要人工操作。

```sh
docker compose logs -f query worker
docker compose down
```

首次数据初始化自动执行，`up --wait` 只等待应用存活，不代表已经发布可查询的数据。`volumes` 和 `migrate` 是一次性任务，成功后显示 `Exited (0)` 属于正常状态。`down` 保留宿主机数据目录；重新执行启动命令可复用数据。`--volumes` 不会删除绑定挂载的宿主机目录。

初始化中途重启后，worker 先恢复已有任务。采集结果未封存或校验失败时，自动创建关联的新任务重新采集；已有 `acquisition_failed` 初始化也可在启动后自动重试。每次启动最多自动重新采集一次，与前次失败至少间隔 5 分钟（结束时间未知时按开始时间计算），并遵守上游冷却，避免持续故障触发重复全量采集。原记录保留，完整数据包可用时仍走本地恢复。容量、认证或源数据错误需先修正，自动恢复不修改这些配置。前端“更新任务”页首提供“强制全量更新”，首次初始化完成后可用；初始化失败由启动恢复或维护区的“失败重跑”处理。

容器通过服务自身的 `--healthcheck` 检查 `/livez`，探测地址跟随服务进程的 `HTTP_ADDRESS`；通配监听地址会转换为对应的本机回环地址。初始化、更新或恢复期间 `/readyz` 暂时不就绪属于正常状态，不应将它用作容器存活检查。仅网关发布宿主机端口，由 `SICP_GATEWAY_PORT` 设置；外部 `/livez`、`/readyz` 返回 404。

统一配置模板为根目录 `.env.example`，实际部署只维护根目录 `.env`。七个写入目标 `DATABASE_*_URL` 只配置共享端点和库名，不含账号密码；`POSTGRES_PASSWORD`、`QUERY_PASSWORD`、`WORKER_PASSWORD`、`LOGGER_PASSWORD`、`UPSTREAM_LOGGER_PASSWORD` 各填写一次。Compose 按服务分配账号与密码，进程自动编码特殊字符并构造完整连接串；query 不会获得 worker 或迁移账号密码。读取和清理地址自动组装，无需额外 URL。已有 PG 卷不会因修改环境变量而自动修改角色密码，轮换时仍需先在数据库内修改，再更新 `.env`。

业务持久化数据只需在 `.env` 设置一个宿主机绝对路径：`SICP_DATA_ROOT=/home/ubuntu/sicp`。Compose 启动时自动在 `${SICP_DATA_ROOT}/<Compose 实际项目名>/` 下创建 `pg-m`、`pg-s`、`pg-log`（含两类日志库）、`redis`、`query-logs`、`query-replicas`、`worker-logs`、`query-upstream-logs`、`worker-upstream-logs`、`staging`、`targets`、`spool` 子目录并分别挂载；默认项目名来自 `SICP_PROJECT`；使用 `docker compose -p` 或 `COMPOSE_PROJECT_NAME` 覆盖时，数据子目录也跟随实际项目名，保持隔离。容器内路径由 Compose 固定，无需在 `.env` 配置。开发环境的模拟邮件使用 Docker 管理的 `mock-mail` 命名卷，无需配置宿主机路径；`down --volumes` 会删除该测试邮件卷。

从旧命名卷切换前，先停止写入并备份数据，将各卷内容复制到对应宿主机目录，保留文件所有权和权限；仅修改配置不会自动迁移。已有宿主机目录更换路径时也需迁移数据，再执行 `docker compose up -d --force-recreate` 使挂载生效，单纯 `restart` 不会更新挂载。全新部署会创建缺失目录，应用目录权限仍由初始化服务设置。

项目名和端口也统一在 `.env` 设置：`SICP_PROJECT`、`SICP_GATEWAY_PORT`。终端同名环境变量优先。统一入口同时启动 query-gateway、worker 和 console，query 由依赖自动启动。

旧版 `deploy/compose*.yaml`、`deploy/examples/*.env.example` 和 `configs/*.env.example` 已移除。原有 `deploy/runtime/*.env` 不再加载，请先将自定义值迁入根目录 `.env`；旧文件不会自动改写。上一版的 `QUERY_DATABASE_*`、`WORKER_DATABASE_*`、`MIGRATE_DATABASE_*` 也不再读取：请将相同的端点与库名合并为七个不含凭据的 `DATABASE_*_URL`，账号密码填写到上方对应变量。旧项目 `sicp-phase1` 的卷不会自动迁移。仅在确认旧卷数据属于目标环境后，才将其迁入对应宿主机目录；不要将含合成数据的卷直接用于正式环境。

升级已有部署时先 `docker compose stop console query-gateway query worker` 并备份全部卷；如果涉及 PostgreSQL 大版本变化，先完成下节的数据迁移，再运行上述启动命令。如需要明确重跑应用表结构迁移，先执行 `docker compose build`、`docker compose run --rm migrate`。重复迁移校验已应用版本与 checksum。不要在旧服务仍运行时直接进行结构升级。

## PostgreSQL 18 与旧数据升级

三个数据库统一使用 `postgres:18.6-trixie`（PostgreSQL 18.6 / Debian 13），与 P01 的数据库版本决策一致。现有实现依赖 PostgreSQL 事务、advisory lock、pgx 批量 COPY 和 JSON 存储，继续使用 PostgreSQL 可直接沿用这些能力。镜像标签明确固定数据库补丁版本和基础系统；后续补丁升级需同步修改配置检查并重新执行部署与集成验证。

按 [Docker 官方镜像约定](https://hub.docker.com/_/postgres)，宿主机的三个数据库目录分别挂载到 `/var/lib/postgresql`，实际数据目录为 `/var/lib/postgresql/18/docker`。全新空卷可直接使用一键启动命令；`.env` 中的数据库地址和凭据格式不变。`scripts/check-compose-config.py` 校验三个实例的镜像版本和各自持久卷挂载，防止部署配置再次偏离版本决策。

已有 PostgreSQL 16 数据不能通过换镜像或移动目录直接升级。旧卷名称仍为 `pg-m`、`pg-s`、`pg-log`；官方入口发现旧格式数据时会拒绝初始化，需要先进行 [PostgreSQL 大版本迁移](https://www.postgresql.org/docs/18/upgrading.html)。应用的 `db-migrate` 只负责表结构，不升级 PG 数据文件。

保留旧数据时，在仍使用 16 的部署配置下停止 console/query/worker 并等待写入结束，备份所有持久卷，分别导出 M、S、日志实例的数据库和角色。M 包含 slot0、slot1、control，S 包含 slot0、slot1，日志实例包含 logs。随后在独立新卷中初始化 18，恢复角色及各库，核对数据、权限、应用迁移记录后再切换应用。恢复方案需要处理初始化脚本已创建的同名角色和空库；不要直接向它们重复执行全实例建库脚本。也可在同时具备 16/18 二进制的专用迁移环境中使用 `pg_upgrade`。迁移验收前保留旧卷和备份，应用恢复写入后回退还需处理新增数据。

若旧环境仅含可丢弃的测试数据，可在停掉旧项目后选择新的 `SICP_PROJECT` 启动全新环境，旧项目的卷仍保留。不要为升级删除旧数据。旧命名卷部署中的 `docker compose down --volumes` 会删除其命名卷；当前绑定挂载的宿主机目录不会由该命令删除。

## 正式与开发环境切换

根目录 `.env.example` 默认为 `ENVIRONMENT=production`。先填写四个数据库密码、`QUERY_API_KEY`、`SNAPSHOT_KEY`、`AUDIT_KEY`、`ADMIN_USERNAME` 和 `ADMIN_PASSWORD`，再配置真实 `SOURCE_ORIGIN`、`SOURCE_APP_ID`、`SOURCE_APP_SECRET`、`SOURCE_SCOPE`、`SOURCE_QUALITY`；SMTP 按需配置。正式环境不会启动或挂载 Mock 服务；缺少上游配置、误填开发源地址或邮件配置不完整会报错，不会回退模拟数据。

`SOURCE_APP_ID` 与 `SOURCE_APP_SECRET` 来自上游开发者首页。worker 在首次采集时向同一 origin 的 `GET /apizt/opentoken/oauth/token` 传入 `appId`、`secret`，读取 `data.access_token`。Token 仅保存在内存；有效 `data.expires_in`（秒，整数或整数字符串）存在时提前 10% 有效期（最多 30 秒）重新获取；没有可用有效期时复用到上游明确拒绝，不假定固定时长。并发页面共享 Token，重新获取前等待使用旧 Token 的请求结束，避免新 Token 使并发中的旧 Token 失效。HTTP 或业务码 401/403 会触发一次重新获取及当前页重试，其他业务码按原失败策略处理；上游专有的失效码尚待完整合同确认。认证请求受 `DEPENDENCY_TIMEOUT` 限制，失败不会回退静态令牌或模拟数据，也不会记录凭据、Token 或响应正文。已有 `.env` 应删除 `SOURCE_TOKEN` 并填写上述两个应用凭据；直接运行进程时，静态 `SOURCE_TOKEN` 仅允许在 development/test 环境使用。

邮件通知默认关闭：`MAIL_ADDRESS`、`MAIL_TLS`、`MAIL_FROM`、`MAIL_TO`、`MAIL_USERNAME`、`MAIL_PASSWORD` 六项全部留空或未设置时，worker 正常启动，不创建 SMTP 客户端、不启动邮件发送任务，也不新增待发邮件。正式环境启用时必须完整填写前四项，`MAIL_TLS` 为 `tls` 或 `starttls`；需要登录时成对填写用户名和密码。部分配置缺失仍会报错。已有 `.env` 若保留 `MAIL_TLS=starttls`，关闭邮件时也需清空该项。

开发时只改环境值：

```dotenv
ENVIRONMENT=development
COMPOSE_PROFILES=${ENVIRONMENT}
SICP_PROJECT=sicp-${ENVIRONMENT}
```

第二、三行保持模板写法即可。开发环境仍需填写数据库和 API 凭据，但真实上游/邮件可以留空；worker 自动使用合成数据源和测试质量门禁。六个邮件参数全空时同样关闭邮件；需要测试邮件时设置 `MAIL_ADDRESS=mock-mail:1025`，入口自动填充测试邮件配置并清空 SMTP 登录凭据，不访问真实 SMTP。Mock 及其卷初始化服务只属于 development profile；其测试控制令牌为固定合成值，不用于正式部署。开发依赖使用 [Compose profiles](https://docs.docker.com/compose/how-tos/profiles/) 按需启用。

默认项目名随环境变为 `sicp-production` / `sicp-development`，宿主机数据子目录随之隔离。切换前先运行旧环境的 `docker compose down` 再改 `.env`；若同时运行两套环境，还需使用不同的宿主机端口。已有固定项目名不会自动隔离，请勿让两种环境共享项目名。

不要单独覆盖 `COMPOSE_PROFILES` 或给启动命令额外传 `--profile` 来替代环境开关；显式启用 development profile 只控制容器选择，worker 数据源仍由 `ENVIRONMENT` 决定。原先的 `ENVIRONMENT=test` 也不会自动启用 Mock，旧演示部署升级时须明确切换为 development。

## Bash 分步运维

脚本使用根目录统一配置，默认按正式环境部署。要求 Linux amd64、Bash、Python 3.10+、Docker Engine 与 Compose ≥ 2.30；各服务进程使用 UID/GID 65532，配置/日志时区按 `Asia/Shanghai` 解析源时间，数据库会话使用 UTC。M、S、日志分别为独立 PostgreSQL 容器，控制库固定在 M；db-log 内含 logs / upstream_logs；服务不能通过活动路由选择控制或日志库。

```bash
./scripts/sicp.sh deploy
./scripts/sicp.sh status
```

`deploy` 依次 prepare → build → validate → 停止应用 → infra → migrate → log-cutover → start → 外部入口校验。也可逐项执行；迁移在服务首次启动前运行，重复迁移校验原版本与 checksum，不修改已应用脚本。数据库角色与空数据库由 `init-postgres.sh` 在空 PG 卷第一次启动时创建，后续结构更新由 一次性 `db-migrate` 部署任务 完成。升级先停应用，备份全部卷，构建、校验、迁移，再启动应用；不删除旧迁移记录。

`prepare` 只在根目录 `.env` 缺失时复制 `.env.example`，生成文件权限受 umask 077 约束；已有 `.env` 保持不变。`.env` 被 Git 和镜像构建上下文排除。文件使用 Compose dotenv 语法，不作为 Shell 脚本执行；包含字面量 `$` 或 `#` 的值使用单引号，JSON 保持单行。参数列表详见 [运行配置](../development_docs/P15_RUNTIME_CONFIGURATION.md)。

部署凭据由操作者填写。query 使用 sicp_query，worker 使用 sicp_worker，平台日志使用 sicp_logger，上游日志使用 sicp_upstream_logger；配置文件统一管理凭据，但迁移账号只传给 migrate，管理员账号密码只传给 worker。query 通过 query-gateway 向宿主机的 0.0.0.0 发布 18080，worker 和 console 不再独立发布端口，管理路由与页面共用网关入口。数据库、Redis 和开发模式的 Mock 仅连接 internal 网络，不发布宿主端口，可通过 `docker compose exec` 在对应容器内排查。仅开发模式的 Mock 从只读挂载的 `mock_data` 加载合成快照，从不加载 source_samples，也不发送外部邮件。

```bash
./scripts/sicp.sh stop
./scripts/sicp.sh start
./scripts/sicp.sh restart
./scripts/sicp.sh down     # 保留所有宿主机数据目录
```

幂等键由操作者为一次动作确定，丢失响应时使用同一个键重试。受理成功仅表示任务持久存在，不表示构建/发布成功。强制全量要求自动初始化已完成；失败重跑新建关联任务，旧历史不变。定时任务按实际 started_at 修订为下一次两小时后，持久冷却未结束时不会提前调用源。

卷初始化工具只设置各挂载根目录的 UID/GID 与 0700，不递归改写已有文件。query/worker 日志各自隔离；worker 的 staging、完整 targets 与 spool 分卷，全部需要可写、fsync 和配置容量。不要共享两服务日志目录，不要删除 `.worker.lock` 或启动挂载不同 spool 卷的第二个 worker。实际数据库 SQL 未确认结束时，不得人工清空 owner/readers 来强行接管。

服务收到 SIGTERM/SIGINT 后按 SHUTDOWN_TIMEOUT=20s 总预算停止准入并排空；Compose stop_grace_period=35s。启动恢复先处理 pending 发布及旧任务，未决时 sync_ready=false。query_ready 独立检查可确认的活动组合与实际查询权限。日志 PG 不可用时保留未确认日志，邮件发送失败保留控制库 outbox；不将超时当作成功或伪造任务结束时间。

生产部署须完成凭据、TLS、上游/邮箱、网络规则、持久存储和备份配置，并将 M/S/日志布置到满足故障隔离要求的实例。模板容量仅为起始预算，尚不代表生产容量/吞吐验收；部署前需测量源规模、十四表全量封存和双候选峰值、spool 外排空间、日志断网积压、查询并发及快照保留窗口。禁止通过降低质量门禁或删除恢复引用绕过空间不足。

自定义本机项目/端口可显式设置 `SICP_PROJECT`、`SICP_GATEWAY_PORT`。脚本不提供自动删卷命令；销毁仅限操作者已核对的独立测试项目。

GitHub Actions 先执行单元测试、race、配置解析/装配、Bash/Python 语法和镜像构建，再串行执行 I01—I06。I06 从同一 job 的干净 checkout 构建镜像，每个编号独立创建项目与空卷，通过 Bash 脚本迁移、启动并等待自动初始化；定时同步测试在隔离数据库推进计划时间。历史验收记录见 verification/I06.md，本次行为变更需以新一轮 CI 结果为准。集成 JSON 结果及容器日志保存在 linux-integration-evidence artifact。

构建专项新增 business_003 事务索引迁移、worker MAINTAIN 权限、BUILD_CHECK_TIMEOUT 与 BUILD_IMPORT_MODE；请按[专项升级与回退说明](../development_docs/BUILD_TO_READY_ROLLOUT.md)安排窗口。COPY 默认启用，额外数据库并发未启用，完整容量验收尚未完成。

第二阶段九接口另追加 business_004，必须在 M0/M1/S0/S1 全部迁移完成后启用新版 query/worker；沿用源表、只读权限和旧证据目录。回退保留新增索引与 ledger，旧镜像不提供五新接口。完整步骤、冻结容量、测试入口及尚缺的实际证据见[第二阶段升级与回退](../development_docs/PHASE2_ROLLOUT.md)。


### 构建后的逐表检查

生产构建不执行学生成绩、批量成绩、学生课表或教师课表查询。候选副本、切换前旧副本、无变化更新轮次均只检查源数据存储及索引/读取权限；全部 14 张表仍进行内容、行数和主键顺序核对。必要存储字段/类型、质量门禁、文件完整性与发布身份约束仍生效，文档允许的可空字段不会额外接受业务查询门禁。

新就绪记录使用 `source-storage-v1`，绑定全部 14 张表的范围、行数和摘要，以及副本槽位、代次和组合。旧版本的业务校验证据继续可读，重新构建生成新存储校验证据；不需要数据表迁移。升级需重新构建并部署 worker，新代码不会替换运行中的旧进程。

业务规则回归使用代码库中固定、脱敏的输入与明确预期结果，在 GitHub 测试执行；生产构建不会读取测试夹具。此前的 `BUILD_SMOKE_CASES` 已退出部署配置，旧 `.env` 即使保留它也会被忽略；不需要同步真实学号、教师工号配置。

课表可空字段兼容保持不变：缺少学年/学期不推测归属，缺少学生课程关联键不推测课程；教师课表可空编号返回空字符串，日期缺失返回空数组，可空数值的空字符串按 null 处理。业务请求中必要字段或已有非空值的格式错误仍报错，但不参与同步发布门禁。

增量源请求的 startTime / endTime 使用 SOURCE_TIMEZONE 的 YYYY-MM-DD HH:mm:ss，不携带小数秒或时区后缀。起点向下、终点向上取整到秒，保证覆盖原窗口；任务时间和成功进度仍保留原精度，已有重叠窗口和按主键合并规则继续生效。升级后可恢复旧格式的完整封存输入；已失败的采集需要发起失败重跑，不能复用不完整输入。


### 查询并发和连接池配置

根目录 .env 支持以下参数，省略时沿用原有默认值；显式空值或越界值会阻止对应服务启动。

| 参数 | 默认值 | 范围与含义 |
| --- | --- | --- |
| QUERY_MAX_EXECUTIONS | 8 | 1—10000，每个 query 进程同时运行的缓存未命中合并任务数 |
| QUERY_MAX_QUEUE | 64 | 0—100000，额外等待容量；总调用容量为执行数加等待容量，相同查询的等待者也计数 |
| QUERY_EXECUTION_TIMEOUT | 30s | 大于 0、不超过 1h，合并任务从创建起计时，包含排队；不会扩大前台单次查询的 20s 预算 |
| QUERY_DB_BUSINESS_MAX_CONNS | 16 | 1—1000，query 的每个业务槽位连接池上限，共四个池 |
| QUERY_DB_CONTROL_MAX_CONNS | 16 | 1—1000，query 的控制库连接池上限 |
| QUERY_DB_LOG_MAX_CONNS | 16 | 1—1000，query 的日志库连接池上限 |

上述参数仅对 query 生效，也可直接作为 query 进程的环境变量。worker 和数据库迁移工具保留原有默认池上限。BUSINESS 设置分别作用于 M0、M1、S0、S1，不表示四池共享该数量；池按需建连，最小连接数仍为 0。内置 db-m 同时承载两个业务槽位和 control，需将 query、worker 各池、迁移及其他客户端的连接需求合计，与 PostgreSQL max_connections 和保留连接额度核对。此配置不自动修改数据库服务端连接上限。

首次升级需要重新构建镜像；之后修改 .env 需通过 docker compose up -d 重新创建 query 服务，单纯 restart 不会重新注入环境变量。配置变大仅改变资源门限，不代表已经达到 300 并发吞吐目标；缓存命中仍有快照、控制库和日志开销。共享查询被所有调用者取消时仍会取消实际执行，发布、快照与副本切换的一致性规则不变。


### 多 query 实例与负载均衡

`.env` 中设置 `QUERY_REPLICAS=4` 即配置四个 query 容器，默认 1，支持 1—32。外部访问 `SICP_GATEWAY_PORT`（默认 18080）；query-gateway 使用最少连接策略分配请求，通过 Docker DNS 每 2 秒更新后端地址。控制台的 worker 代理也经过该入口。Redis、业务库和日志库共享；worker 仍为一个实例，原有容量设置不变。

首次从直接发布 query 端口的版本升级，需要短暂查询维护窗口：先 `docker compose build query query-gateway`，然后 `docker compose stop query` 并执行 `docker compose run --rm --no-deps volumes` 初始化新增日志目录权限，最后 `docker compose up -d --no-deps query query-gateway worker`。worker 需重建容器以更新控制台代理地址；请在同步任务空闲时执行。后续只调整副本数量或 query 参数时，执行 `docker compose up -d --no-deps query query-gateway`，无需重启 worker。`restart` 不会应用副本数量或新环境变量。控制台首次升级也应在随后完整 `docker compose up -d` 时同步依赖配置。

六个查询容量参数均为每实例预算。例如四个实例各设置 8 个执行任务，合计最多 32 个缓存未命中任务。db-m 的 query 连接上限预算为 `QUERY_REPLICAS × (2 × QUERY_DB_BUSINESS_MAX_CONNS + QUERY_DB_CONTROL_MAX_CONNS)`，还需加 worker、迁移、保留连接与其他客户端。此功能不自动调整 PostgreSQL 上限，也不代表 300 并发验收通过。

平台日志写入 db-log 的 logs 库，上游 call/attempt 写入同一实例的 upstream_logs 库。本地文件是入库前的持久缓冲：第一个取得目录锁的实例复用原 `query-logs`，其余实例使用 `query-replicas/2` 至 `query-replicas/32`。编号是目录租约，不对应固定容器编号。锁保持到进程退出，异常退出后由内核释放；新实例会复用空闲目录并处理其中的日志。两个宿主机目录必须放在支持 Linux flock 的本地文件系统上，不适用于跨主机共享卷。不得删除 `.lock` 文件。

缩容不会删除日志；停用目录中尚未入库的缓冲仍会保留，后续再次分配该目录时继续处理。若日志库故障期间需要缩容，恢复日志库、确认缓冲入库后再缩容，避免历史日志延迟可见。LOG_MAX_BYTES / LOG_MAX_FILES 和 UPSTREAM_LOG_MAX_BYTES / UPSTREAM_LOG_MAX_FILES 分别为每实例的平台/上游缓冲限额，需按副本数计算总磁盘预算。

入口保留 API 密钥、快照头和响应内容，不自动重放已发送到后端的 POST。扩缩容期间地址刷新和在途请求仍可能出现短暂失败；这不是无损滚动升级承诺。GitHub Compose 验证覆盖多实例分流、跨实例快照查询、共享日志入库、实例重建、扩缩容及独立目录租约。


### PostgreSQL 服务端连接上限

内置 Compose 从 `.env` 读取 `POSTGRES_M_MAX_CONNECTIONS=256`、`POSTGRES_S_MAX_CONNECTIONS=128`、`POSTGRES_LOG_MAX_CONNECTIONS=100`，分别作为 db-m、db-s、db-log 的 `postgres -c max_connections=...` 启动参数。省略变量时也使用这些项目默认值；它们不同于 PostgreSQL 通常为 100 的原生默认值。值须为 PostgreSQL 接受的正整数，显式空值或非法值会导致数据库启动失败。外部数据库不受这些 Compose 参数控制。

上限由整个数据库实例共享，包含 query、worker、迁移、运维和保留连接；db-log 的两库共用 POSTGRES_LOG_MAX_CONNECTIONS，须合计各源写入、读取及维护连接池，同凭据的 worker 读取/维护共用池；db-m 的 slot0、slot1 与 control 共用 M 的额度。连接池按需建连，这些配置不代表预先建立同等数量的连接，也不保证 300 并发达标；调大上限会增加 PostgreSQL 的资源需求。应用池参数和 worker 设置不会因此自动改变。

已有数据卷同样适用，无需重新初始化数据库。更新 Compose 后，旧 `.env` 即使未添加这些变量，也会在下次重建数据库容器时使用新的项目默认值。安排同步任务空闲的维护窗口，暂停应用后重建数据库，再恢复应用：

```bash
docker compose stop query worker
docker compose up -d --no-deps --wait db-m db-s db-log
docker compose up -d --no-deps --wait query query-gateway worker
```

修改 `.env` 后仅运行 `restart` 不会更新容器启动参数。用 `docker compose exec db-m psql -U postgres -d postgres -c "SHOW max_connections;"` 核实实际值，db-s、db-log 同理；自定义 POSTGRES_USER 时替换命令中的用户名。GitHub Compose 检查会验证首次启动及保留数据重建后的三个实际值。

## 统一 HTTP / HTTPS 入口

默认 `.env` 的 `SICP_TLS_CERT_FILE`、`SICP_TLS_KEY_FILE` 均为空，访问 `http://<服务器>:18080/console/`。`SICP_GATEWAY_PORT` 缺省或空值为 18080，范围 1—65535。旧 `SICP_QUERY_PORT`、`SICP_WORKER_PORT`、`SICP_CONSOLE_PORT` 不再读取；迁移时将唯一对外端口写入 `SICP_GATEWAY_PORT`，同步修改所有客户端地址。容器外部监听固定 8443，内部 HTTP 8080 仅用于 query 与探针，不发布给宿主机。

HTTPS 配置示例（Linux 宿主机路径）：

```dotenv
SICP_GATEWAY_PORT=18443
SICP_TLS_CERT_FILE='/etc/sicp/tls/fullchain.pem'
SICP_TLS_KEY_FILE='/etc/sicp/tls/privkey.pem'
```

两项必须成对。路径可含空格，使用 Compose dotenv 引号保留字面字符；拒绝相对路径、控制字符和纯空白。文件直接只读挂载，必须为 UID/GID 65532 可读的普通文件；证书链和私钥需匹配，不接受需交互口令的加密私钥。无需把私钥复制进仓库或镜像。预检使用正式网关相同镜像、UID、挂载和 tmpfs；无效文件失败且不切换为 HTTP。未配置时 `/dev/null` 仅作为只读占位，HTTP 不读取其内容。

首次部署：

```sh
bash scripts/sicp.sh deploy --url https://data.example.org:18443
# 私有 CA 可追加 --ca-file /etc/sicp/ca.pem
```

`--url` 必须是与目标协议、端口一致的 origin，不含账号、路径、查询串或 fragment。HTTPS 必须显式给出访问域名；脚本验证 CA 信任、域名及服务端叶证书指纹，不从证书 SAN 猜域名。单独启动后可运行 `python3 scripts/gateway.py verify --url <入口> [--ca-file <CA>]`。查询工具使用 `--base-url`/`SICP_BASE_URL` 优先，HTTPS 同样要求显式域名；`--ca-file`/`SICP_CA_FILE` 指定测试或私有 CA。

已有统一入口切换协议：修改 `.env` 后运行以下命令。完整预检成功前不触碰现有容器；随后先停止网关，再重建 worker 并等待健康，最后重建网关、等待健康并验证外部入口。健康等待每服务 90 秒，失败立即退出，可能留下入口中断，需要操作者恢复。

```sh
bash scripts/sicp.sh gateway-apply --url https://data.example.org:18443
# 切回 HTTP：清空两项证书路径，并运行
bash scripts/sicp.sh gateway-apply --url http://data.example.org:18443
```

worker 重建会使原会话失效。HTTPS → HTTP 前先退出；未退出者清理该站点 Cookie 后再在 HTTP 登录，改端口不会清除 Secure Cookie。已有 HSTS 策略可能阻止 HTTP，须处理原策略或使用独立 HTTP 域名。不要只重启网关切协议；这会使入口与 worker 的 Cookie 设置不一致。

证书续期时原子替换宿主文件或更换软链接目标，再执行：

```sh
bash scripts/sicp.sh gateway-renew --url https://data.example.org:18443
```

仅续期要求当前网关和 worker 均为受管理 HTTPS，配置与运行容器一致；有其他配置变更应使用发布流程。预检新文件后只重建网关，重新绑定新 inode 并核验新指纹；worker 会话保留。仅 `reload` 或 `restart` 无法保证重新挂载原子替换文件。

失败恢复：预检失败保留原入口；切换开始后失败不会自动开放 HTTP。修正配置后，先以 `validate` 确认有效，再按 worker → 网关顺序分别运行 `docker compose up -d --force-recreate --no-deps --wait --wait-timeout 90 <服务>`，最后运行 `gateway.py verify`。若恢复 HTTP，必须明确清空两项证书路径并处理浏览器 Cookie。

旧版本回退须同时恢复上一版镜像、Compose、网关入口/模板、worker 入口、console 配置和对应 `.env`；先停当前应用，再按旧配置构建/启动、检查旧端口登录和查询。保持项目名与数据根目录不变，不删除数据卷。仅恢复一个 Nginx 文件不构成完整回退。CI 的整套回退夹具固定基线提交 `3443eaeafe30e5870dc5c5d84ddfa7e19df517e9`。
