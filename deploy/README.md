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
docker compose up -d --build --wait --wait-timeout 300
```

Windows PowerShell 使用 `Copy-Item .env.example .env`。复制只需首次执行，不覆盖已有配置。

根目录唯一的 `compose.yaml` 直接定义所有服务（含 console），正式环境默认项目名 `sicp-production`。启动顺序由 Compose 依赖关系保证：数据库完成空卷初始化并可通过 TCP 访问后执行迁移；卷目录权限初始化成功、迁移成功且 Redis 健康后启动 query/worker（开发模式的 worker 同时等待 Mock 健康）；两服务存活后启动控制台。初始化或迁移失败会阻止依赖服务启动，可用 `docker compose logs volumes migrate db-m db-s db-log` 排查。

| 服务 | 本机入口 |
| --- | --- |
| 控制台 | http://127.0.0.1:18082/console/ |
| 查询 API | http://127.0.0.1:18080 |
| 管理 API | http://127.0.0.1:18081 |

控制台使用 `.env` 中的 `ADMIN_USERNAME` 和 `ADMIN_PASSWORD` 登录，不提供默认密码。业务密钥保留在服务端；前端无需填写 API Key 或管理 Token。原有部署需删除 `MANAGEMENT_CREDENTIALS`、`MANAGEMENT_TOKEN` 并填写新的账号密码。会话有效 8 小时，worker 重启后需重新登录。HTTPS 入口设 `CONSOLE_COOKIE_SECURE=true`。全新环境由 worker 自动初始化，之后每两小时自动调度增量同步。在“更新任务”查看记录，仅强制全量和失败重跑需要人工操作。

```sh
docker compose logs -f query worker
docker compose down
```

首次数据初始化自动执行，`up --wait` 只等待应用存活，不代表已经发布可查询的数据。`volumes` 和 `migrate` 是一次性任务，成功后显示 `Exited (0)` 属于正常状态。`down` 保留宿主机数据目录；重新执行启动命令可复用数据。`--volumes` 不会删除绑定挂载的宿主机目录。

初始化中途重启后，worker 先恢复已有任务。采集结果未封存或校验失败时，自动创建关联的新任务重新采集；已有 `acquisition_failed` 初始化也可在启动后自动重试。每次启动最多自动重新采集一次，与前次失败至少间隔 5 分钟（结束时间未知时按开始时间计算），并遵守上游冷却，避免持续故障触发重复全量采集。原记录保留，完整数据包可用时仍走本地恢复。容量、认证或源数据错误需先修正，自动恢复不修改这些配置。前端“更新任务”页首提供“强制全量更新”，首次初始化完成后可用；初始化失败由启动恢复或维护区的“失败重跑”处理。

容器通过服务自身的 `--healthcheck` 检查 `/livez`，探测地址跟随服务进程的 `HTTP_ADDRESS`；通配监听地址会转换为对应的本机回环地址。初始化、更新或恢复期间 `/readyz` 暂时不就绪属于正常状态，不应将它用作容器存活检查。宿主机发布端口仍由 `SICP_QUERY_PORT`、`SICP_WORKER_PORT` 设置。

统一配置模板为根目录 `.env.example`，实际部署只维护根目录 `.env`。六个 `DATABASE_*_URL` 只配置共享端点和库名，不含账号密码；`POSTGRES_PASSWORD`、`QUERY_PASSWORD`、`WORKER_PASSWORD`、`LOGGER_PASSWORD` 各填写一次。Compose 按服务分配账号与密码，进程自动编码特殊字符并构造完整连接串；query 不会获得 worker 或迁移账号密码。已有 PG 卷不会因修改环境变量而自动修改角色密码，轮换时仍需先在数据库内修改，再更新 `.env`。

业务持久化数据只需在 `.env` 设置一个宿主机绝对路径：`SICP_DATA_ROOT=/home/ubuntu/sicp`。Compose 启动时自动在 `${SICP_DATA_ROOT}/<Compose 实际项目名>/` 下创建 `pg-m`、`pg-s`、`pg-log`、`redis`、`query-logs`、`worker-logs`、`staging`、`targets`、`spool` 子目录并分别挂载；默认项目名来自 `SICP_PROJECT`；使用 `docker compose -p` 或 `COMPOSE_PROJECT_NAME` 覆盖时，数据子目录也跟随实际项目名，保持隔离。容器内路径由 Compose 固定，无需在 `.env` 配置。开发环境的模拟邮件使用 Docker 管理的 `mock-mail` 命名卷，无需配置宿主机路径；`down --volumes` 会删除该测试邮件卷。

从旧命名卷切换前，先停止写入并备份数据，将各卷内容复制到对应宿主机目录，保留文件所有权和权限；仅修改配置不会自动迁移。已有宿主机目录更换路径时也需迁移数据，再执行 `docker compose up -d --force-recreate` 使挂载生效，单纯 `restart` 不会更新挂载。全新部署会创建缺失目录，应用目录权限仍由初始化服务设置。

项目名和端口也统一在 `.env` 设置：`SICP_PROJECT`、`SICP_QUERY_PORT`、`SICP_WORKER_PORT`、`SICP_CONSOLE_PORT`。终端同名环境变量优先。只需后端时可指定服务：`docker compose up -d --build --wait --wait-timeout 300 query worker`。

旧版 `deploy/compose*.yaml`、`deploy/examples/*.env.example` 和 `configs/*.env.example` 已移除。原有 `deploy/runtime/*.env` 不再加载，请先将自定义值迁入根目录 `.env`；旧文件不会自动改写。上一版的 `QUERY_DATABASE_*`、`WORKER_DATABASE_*`、`MIGRATE_DATABASE_*` 也不再读取：请将相同的端点与库名合并为六个不含凭据的 `DATABASE_*_URL`，账号密码填写到上方对应变量。旧项目 `sicp-phase1` 的卷不会自动迁移。仅在确认旧卷数据属于目标环境后，才将其迁入对应宿主机目录；不要将含合成数据的卷直接用于正式环境。

升级已有部署时先 `docker compose stop console query worker` 并备份全部卷；如果涉及 PostgreSQL 大版本变化，先完成下节的数据迁移，再运行上述启动命令。如需要明确重跑应用表结构迁移，先执行 `docker compose build`、`docker compose run --rm migrate`。重复迁移校验已应用版本与 checksum。不要在旧服务仍运行时直接进行结构升级。

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

脚本使用根目录统一配置，默认按正式环境部署。要求 Linux amd64、Bash、Docker Engine 与 Compose ≥ 2.30；各服务进程使用 UID/GID 65532，配置/日志时区按 `Asia/Shanghai` 解析源时间，数据库会话使用 UTC。M、S、日志分别为独立 PostgreSQL 容器，控制库固定在 M；服务不能通过活动路由选择控制或日志库。

```bash
./scripts/sicp.sh deploy
./scripts/sicp.sh status
```

`deploy` 依次 prepare → build → validate → infra → migrate → start。也可逐项执行；迁移在服务首次启动前运行，重复迁移校验原版本与 checksum，不修改已应用脚本。数据库角色与空数据库由 `init-postgres.sh` 在空 PG 卷第一次启动时创建，后续结构更新由 一次性 `db-migrate` 部署任务 完成。升级先停应用，备份全部卷，构建、校验、迁移，再启动应用；不删除旧迁移记录。

`prepare` 只在根目录 `.env` 缺失时复制 `.env.example`，生成文件权限受 umask 077 约束；已有 `.env` 保持不变。`.env` 被 Git 和镜像构建上下文排除。文件使用 Compose dotenv 语法，不作为 Shell 脚本执行；包含字面量 `$` 或 `#` 的值使用单引号，JSON 保持单行。参数列表详见 [运行配置](../development_docs/P15_RUNTIME_CONFIGURATION.md)。

部署凭据由操作者填写。query 使用 sicp_query，worker 使用 sicp_worker，日志使用 sicp_logger；配置文件统一管理凭据，但迁移账号只传给 migrate，管理员账号密码只传给 worker。查询与管理服务同时连接内部依赖网络和入口网络，仅向宿主机的 127.0.0.1 发布 18080、18081。数据库、Redis 和开发模式的 Mock 仅连接 internal 网络，不发布宿主端口，可通过 `docker compose exec` 在对应容器内排查。仅开发模式的 Mock 从只读挂载的 `mock_data` 加载合成快照，从不加载 source_samples，也不发送外部邮件。

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

自定义本机项目/端口可显式设置 `SICP_PROJECT`、`SICP_QUERY_PORT`、`SICP_WORKER_PORT`。脚本不提供自动删卷命令；销毁仅限操作者已核对的独立测试项目。

GitHub Actions 先执行单元测试、race、配置解析/装配、Bash/Python 语法和镜像构建，再串行执行 I01—I06。I06 从同一 job 的干净 checkout 构建镜像，每个编号独立创建项目与空卷，通过 Bash 脚本迁移、启动并等待自动初始化；定时同步测试在隔离数据库推进计划时间。历史验收记录见 verification/I06.md，本次行为变更需以新一轮 CI 结果为准。集成 JSON 结果及容器日志保存在 linux-integration-evidence artifact。

构建专项新增 business_003 事务索引迁移、worker MAINTAIN 权限、BUILD_CHECK_TIMEOUT 与 BUILD_IMPORT_MODE；请按[专项升级与回退说明](../development_docs/BUILD_TO_READY_ROLLOUT.md)安排窗口。COPY 默认启用，额外数据库并发未启用，完整容量验收尚未完成。


### 构建后的逐表检查

生产构建不执行学生成绩、批量成绩、学生课表或教师课表查询。候选副本、切换前旧副本、无变化更新轮次均只检查源数据存储及索引/读取权限；全部 14 张表仍进行内容、行数和主键顺序核对。必要存储字段/类型、质量门禁、文件完整性与发布身份约束仍生效，文档允许的可空字段不会额外接受业务查询门禁。

新就绪记录使用 `source-storage-v1`，绑定全部 14 张表的范围、行数和摘要，以及副本槽位、代次和组合。旧版本的业务校验证据继续可读，重新构建生成新存储校验证据；不需要数据表迁移。升级需重新构建并部署 worker，新代码不会替换运行中的旧进程。

业务规则回归使用代码库中固定、脱敏的输入与明确预期结果，在 GitHub 测试执行；生产构建不会读取测试夹具。此前的 `BUILD_SMOKE_CASES` 已退出部署配置，旧 `.env` 即使保留它也会被忽略；不需要同步真实学号、教师工号配置。

课表可空字段兼容保持不变：缺少学年/学期不推测归属，缺少学生课程关联键不推测课程；教师课表可空编号返回空字符串，日期缺失返回空数组，可空数值的空字符串按 null 处理。业务请求中必要字段或已有非空值的格式错误仍报错，但不参与同步发布门禁。
