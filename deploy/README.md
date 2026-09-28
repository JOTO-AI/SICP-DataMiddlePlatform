# Docker Compose 部署与运维

## 一键启动（推荐）

安装 Docker Engine 或 Docker Desktop（Linux 容器模式）和 Compose ≥ 2.30，在仓库根目录执行，Windows PowerShell 与 Linux/macOS 终端通用：

```sh
docker compose up -d --build --wait --wait-timeout 300
```

根目录 `compose.yaml` 合并后端与控制台配置，默认项目名 `sicp-phase1`。启动顺序由 Compose 依赖关系保证：数据库完成空卷初始化并可通过 TCP 访问后执行迁移；卷目录权限初始化成功、迁移成功且 Redis 健康后启动 query/worker（worker 同时等待 Mock 健康）；两服务存活后启动控制台。初始化或迁移失败会阻止依赖服务启动，可用 `docker compose logs volumes migrate db-m db-s db-log` 排查。

| 服务 | 本机入口 |
| --- | --- |
| 控制台 | http://127.0.0.1:18082/console/ |
| 查询 API | http://127.0.0.1:18080 |
| 管理 API | http://127.0.0.1:18081 |

控制台使用业务密钥 `synthetic-query-secret`、管理令牌 `synthetic-management-token`（仅合成示例）。通过“更新任务 → 首次初始化”建立首份数据，或执行：

```sh
docker compose run --rm --no-deps ctl initialize initialization-001
docker compose run --rm --no-deps ctl status <返回的任务ID>
docker compose logs -f query worker
docker compose down
```

首次数据初始化仍需明确提交，`up --wait` 只等待应用存活，不代表已经发布可查询的数据。`volumes` 和 `migrate` 是一次性任务，成功后显示 `Exited (0)` 属于正常状态。`down` 保留全部持久卷；重新执行启动命令可复用数据，不要加 `--volumes`，除非确实要销毁数据。

默认读取 `deploy/examples/*.env.example`，然后以可选的 `deploy/runtime/*.env` 覆盖同名变量。无需预先运行 Bash 或创建 runtime 目录。自定义时复制对应示例为 `query.env`、`worker.env`、`stack.env`、`migrate.env`、`admin.env`；修改数据库密码时需同步相关连接串，已有 PG 卷不会因修改环境变量而自动修改密码。完整配置和 raw 格式要求见下文。

可在根目录 `.env` 或终端环境变量中设置 `SICP_PROJECT`、`SICP_QUERY_PORT`、`SICP_WORKER_PORT`、`SICP_CONSOLE_PORT`。`.env` 只用于 Compose 插值，不替代 `deploy/runtime/*.env`。仅部署后端可使用 `docker compose -p sicp-phase1 -f deploy/compose.yaml up -d --build --wait --wait-timeout 300`。

升级已有部署时先 `docker compose stop console query worker` 并备份全部卷，再运行上述启动命令；如需要明确重跑迁移，先执行 `docker compose build`、`docker compose run --rm migrate`。重复迁移校验已应用版本与 checksum。不要在旧服务仍运行时直接进行结构升级。

## Bash 分步运维

此模板用于本机隔离的合成验收环境。要求 Linux amd64、Bash、Docker Engine 与 Compose ≥ 2.30；各服务进程使用 UID/GID 65532，配置/日志时区按 `Asia/Shanghai` 解析源时间，数据库会话使用 UTC。M、S、日志分别为独立 PostgreSQL 容器，控制库固定在 M；服务不能通过活动路由选择控制或日志库。

```bash
./scripts/sicp.sh deploy
# deploy 不获取源数据。无完整组合时 query ready=false 是预期行为。
./scripts/sicp.sh initialize initialization-001
./scripts/sicp.sh task <返回的任务ID>
./scripts/sicp.sh status
```

`deploy` 依次 prepare → build → validate → infra → migrate → start。也可逐项执行；迁移在服务首次启动前运行，重复迁移校验原版本与 checksum，不修改已应用脚本。数据库角色与空数据库由 `init-postgres.sh` 在空 PG 卷第一次启动时创建，后续结构更新由 `sicpctl migrate` 完成。升级先停应用，备份全部卷，构建、校验、迁移，再启动应用；不删除旧迁移记录。

`prepare` 只复制缺失的示例，保留已有 `deploy/runtime/*.env`，生成文件权限受 umask 077 约束。该目录被 Git 和镜像构建上下文排除。环境文件使用 raw `KEY=value`：不加外围引号，不写 export，不做变量替换，不执行 Shell。JSON 保持单行原文；路径、源地址、邮件收发目标、十四源质量策略均明确配置。参数列表详见 [运行配置](../development_docs/P15_RUNTIME_CONFIGURATION.md)。Compose 的 raw 文件格式见 [Docker 官方文档](https://docs.docker.com/compose/how-tos/environment-variables/set-environment-variables/)；PG 空卷初始化行为见 [官方镜像说明](https://hub.docker.com/_/postgres)。

示例凭据都是公开合成值，仅供隔离验收。query 使用 sicp_query，worker 使用 sicp_worker，日志使用 sicp_logger；迁移账号仅出现在独立 migrate 工具配置，管理 token 只在 admin 工具配置和 worker 中存在。查询与管理服务同时连接内部依赖网络和入口网络，仅向宿主机的 127.0.0.1 发布 18080、18081。数据库、Redis 和 Mock 仅连接 internal 网络，不发布宿主端口，可通过 `docker compose exec` 在对应容器内排查。Mock 从只读挂载的 `mock_data` 加载合成快照，从不加载 source_samples，也不发送外部邮件。

```bash
./scripts/sicp.sh sync round-002
./scripts/sicp.sh force-full full-003
./scripts/sicp.sh rerun retry-004 <失败任务ID>
./scripts/sicp.sh task <任务ID>
./scripts/sicp.sh stop
./scripts/sicp.sh start
./scripts/sicp.sh restart
./scripts/sicp.sh down     # 保留所有命名卷
```

幂等键由操作者为一次动作确定，丢失响应时使用同一个键重试。受理成功仅表示任务持久存在，不表示构建/发布成功。首次强制全量明确要求先初始化；失败重跑新建关联任务，旧历史不变。定时任务按实际 started_at 修订为下一次两小时后，持久冷却未结束时不会提前调用源。

卷初始化工具只设置各挂载根目录的 UID/GID 与 0700，不递归改写已有文件。query/worker 日志各自隔离；worker 的 staging、完整 targets 与 spool 分卷，全部需要可写、fsync 和配置容量。不要共享两服务日志目录，不要删除 `.worker.lock` 或启动挂载不同 spool 卷的第二个 worker。实际数据库 SQL 未确认结束时，不得人工清空 owner/readers 来强行接管。

服务收到 SIGTERM/SIGINT 后按 SHUTDOWN_TIMEOUT=20s 总预算停止准入并排空；Compose stop_grace_period=35s。启动恢复先处理 pending 发布及旧任务，未决时 sync_ready=false。query_ready 独立检查可确认的活动组合与实际查询权限。日志 PG 不可用时保留未确认日志，邮件发送失败保留控制库 outbox；不将超时当作成功或伪造任务结束时间。

生产部署须替换示例凭据、TLS、上游/邮箱、网络规则、持久存储和备份方案，并将 M/S/日志布置到满足故障隔离要求的实例。当前容量是合成样例预算，尚不代表生产容量/吞吐验收；部署前需测量源规模、十四表全量封存和双候选峰值、spool 外排空间、日志断网积压、查询并发及快照保留窗口。禁止通过降低质量门禁或删除恢复引用绕过空间不足。

自定义本机项目/端口可显式设置 `SICP_PROJECT`、`SICP_QUERY_PORT`、`SICP_WORKER_PORT`。脚本不提供自动删卷命令；销毁仅限操作者已核对的独立测试项目。

GitHub Actions 先执行单元测试、race、配置解析/装配、Bash/Python 语法和镜像构建，再串行执行 I01—I06。I06 从同一 job 的干净 checkout 构建镜像，每个编号独立创建项目与空卷，通过 Bash 脚本迁移、启动及受控初始化；实际通过项见开发计划和 verification/I06.md。集成 JSON 结果及容器日志保存在 linux-integration-evidence artifact。
