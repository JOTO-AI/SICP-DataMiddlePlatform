# 将控制数据库迁移到 db-log

将整个 `db-m/control` 迁到同一部署的 `db-log/control`，保留任务、调度、检查点、发布状态、读者注册、管理配置、通知、审计及迁移记录。校验通过并切换应用后，删除原 `db-m/control`。此操作移动数据库位置，不把控制表改造成可随日志保留期删除的事件表。

## 迁移后的结构

```text
db-m / pg-m                 db-s / pg-s
├── slot0                   ├── slot0
└── slot1                   └── slot1

db-log / pg-log
├── control        任务、调度、发布、管理配置和通知
├── logs           平台事件日志
└── upstream_logs  上游调用日志
```

三个逻辑库共用 PostgreSQL 实例、端口和 `pg-log` 数据目录，各自使用独立连接池。`DATABASE_CONTROL_URL` 改为 `postgres://db-log:5432/control?sslmode=disable`；query/worker 继续使用 `sicp_query` / `sicp_worker`，前端不获得数据库凭据。

迁移后 `db-log` 成为业务关键依赖。整个实例故障会影响任务运行、管理接口和需要控制状态的业务查询；单独某个日志库的错误仍按原逻辑降级。**不得再删除 `pg-log` 来清空日志，它已包含控制数据。** 清理日志应使用限定日志库的管理能力。`pg-m`、`pg-s` 和 Redis、staging、targets、spool 都保留。

## 前端和接口范围

前端路径、HTTP 方法和请求/响应结构保持现有契约，通过后端固定的 `Registry.Control()` 自动切换位置。

| 功能 | 迁移后的数据来源 |
| --- | --- |
| 更新任务列表、详情、进度、失败重跑 | db-log/control |
| 更新事件、操作树中的控制事实 | db-log/control；诊断事件仍来自日志库 |
| 调度、源进度、发布、快照和读者登记 | db-log/control |
| 通知设置、投递记录、管理任务、保留设置 | db-log/control |
| 平台日志、上游日志 | db-log/logs、db-log/upstream_logs |
| 组织、学生、教师、成绩、课表等业务查询结果 | db-m 或 db-s 的业务槽位库 |
| 登录会话、健康和部署状态 | 保留各自现有来源，不伪装成数据库记录 |

“接口走 db-log”指管理及控制数据连接迁入该实例，不是让浏览器直连数据库，也不是把业务源表一起迁移。

## 执行条件

- 使用当前仓库的内置 Compose、标准库名和地址，运行位置为部署所在的 Linux Docker 主机。本脚本不处理外部数据库、远程 Docker daemon、命名日志卷或自定义 locale/数据库级设置。
- 主机有 Bash、Python 3.10+、Docker Compose；使用 root/sudo 读取并锁定 UID 65532 的日志目录。使用原项目名和根目录 `.env`，不要通过 shell 覆盖 `DATABASE_CONTROL_URL`。
- PostgreSQL 两端版本一致。预留完整备份、临时恢复库及排序校验所需磁盘空间；表内容校验会读取整个控制库。
- 当前 `.env` 的控制地址必须仍为 `db-m/control`。脚本在校验后自动修改这一项，其他配置原样保留。不要提前手改地址，否则可能把空库当作已迁移系统。
- 先完成现有单日志到双日志升级，保持 `AUDIT_KEY`、日志地址和目录不变。已有未标记的非空旧缓冲会阻止本次迁移。部署到新版并完成 `log-register → deploy` 后再迁控制库。
- 在维护窗口执行。脚本会停止 console、query-gateway、全部 query 副本和 worker；停机期间不要启动旧服务，也不要用管理员或其他外部程序修改原控制库。
- 新部署默认 `POSTGRES_LOG_MAX_CONNECTIONS=160`。已有 `.env` 的值不自动修改；迁移前合计 control、两类日志及运维连接额度，按 query 副本数调整。提高连接上限需同时核算内存。
- 迁移前备份现有部署。脚本另行生成本次 control 的逻辑备份和原 `.env`，但不代替业务库、日志库和持久文件的整体备份。

## 命令

在已更新代码的项目根目录执行。备份目录应位于所有在线数据挂载之外，不使用 `pg-m`、`pg-log` 或其他业务目录的子目录。

```bash
# 迁移、校验、切换并启动；原 control 暂时封存，不能再连接
sudo bash scripts/migrate-control.sh migrate \
  --backup-dir /srv/sicp-backups/control-move-20261013

# 检查前端更新任务、进度、通知设置和业务查询后，删除旧 control
sudo bash scripts/migrate-control.sh finalize \
  --backup-dir /srv/sicp-backups/control-move-20261013
```

需要自动完成删除时，第一条可加 `--drop-old`：

```bash
sudo bash scripts/migrate-control.sh migrate \
  --backup-dir /srv/sicp-backups/control-move-20261013 --drop-old
```

`--drop-old` 只在恢复校验、应用启动、实际连接目标及服务账号访问检查成功后删除 `db-m/control`。没有 `CASCADE`，不删除卷、业务库、日志库、备份文件或数据库角色。应用存活与连接验证不代表一轮完整同步已经完成，正式环境建议先用分步方式观察。

脚本构建镜像后才停应用，不需要提前 `compose down`。如果已经 down，先按原 `.env` 启动数据库和 Redis；日志目录需保留在原路径。

## 迁移和保护步骤

1. 验证固定端点、项目名、账号、PostgreSQL 版本及数据库身份；目标 `control` 若已有表或其他用户对象，拒绝覆盖。构建当前镜像并停止应用。
2. 独占锁定 query slot 1—32、worker 及对应上游日志目录。核验所有既有拓扑标记；未知目标或密钥不匹配时停止。
3. 将旧库连接上限设为 0 并结束旧连接，阻止普通服务账号重连。管理员保留导出能力；禁止其他管理员写入。
4. 导出完整 custom-format `control.dump`，记录 SHA-256；原 `.env` 保存为 `env.before`。备份目录权限 0700，文件默认 0600，输出不显示凭据。
5. 在 db-log 创建专用临时库 `control_relocation`。以单事务恢复完整 schema、数据、约束、索引、函数、表权限和迁移 ledger。逐表按确定顺序计算完整内容摘要，核对序列状态和 schema/权限摘要，再检查源库没有变化。
6. 禁止原库新连接。仅在目标 control 不存在或为空的情况下，将已验证临时库改名为 control，设置数据库级 CONNECT 权限。
7. 对新控制地址执行现有 migrate，已应用 SQL 按 checksum 跳过，日志库部署身份重新绑定到新拓扑。
8. 在锁内核验旧/新 HMAC 标记，原子更新 `.storage-mode`。只改变 control 的拓扑绑定，不重写、删除或重新分流任何缓冲事件，不删除锁文件。
9. 原子修改 `.env` 中唯一的 `DATABASE_CONTROL_URL`，释放目录锁，再按 Compose 依赖顺序启动应用。检查每个运行中 query/worker 的实际配置及健康状态，并用服务账号通过 TCP 访问新控制库。
10. finalize 再检查备份 checksum、项目及数据库身份、应用目标和旧库封存状态，然后 DROP 原 control。备份和状态文件继续保留。

初始化脚本已调整：新环境仅在 db-log 创建 control，db-m/db-s 仅创建业务槽位库。旧环境地址仍暂时允许在 db-m，以供迁移和恢复使用；日志与业务实例的物理隔离检查继续保留，control 与任意日志目标也不得指向同一个物理库。

数据库级 ACL 不由“不带 --create 的 pg_restore”自动恢复，脚本针对内置部署显式补齐。恢复选项和连接封存语义见 [PostgreSQL pg_restore](https://www.postgresql.org/docs/18/app-pgrestore.html) 和 [ALTER DATABASE](https://www.postgresql.org/docs/18/sql-alterdatabase.html)。

## 中断和恢复

任何失败都不自动删除原 control。错误后保留整个备份目录，在修复磁盘、权限、容器或数据库故障后，使用同一路径继续：

```bash
sudo bash scripts/migrate-control.sh resume \
  --backup-dir /srv/sicp-backups/control-move-20261013
```

`state.json` 记录阶段、项目、物理数据库身份、备份/配置 checksum 和日志标记。不要手改状态文件、密钥或数据路径。临时恢复库可在 `restoring` 阶段按已登记的身份重建；恢复失败不得把半成品改名为 control。如果创建临时库后、保存其身份前主机中断，脚本会拒绝自动删除这个身份未登记的库，需要操作者核验后处理。`finalize` 的删除应答丢失可以重试：确认其他条件后，旧库已不存在时仅补记完成状态。

| 阶段 | 源库及应用状态 | 处理方式 |
| --- | --- | --- |
| 未生成 state.json | 未切换，应用可能已停止 | 排除前置问题；仍使用旧地址；重新执行 migrate |
| prepared / backed_up / restoring | 源库保留，可能阻止普通连接，应用停止 | 修复后 resume，不改地址，不删除备份 |
| verified / cutover | 源库可能已封存，目标库已校验或改名 | resume 完成拓扑和配置切换 |
| configured | 配置已切换，可能已有部分新服务运行 | resume；不能直接恢复旧 `.env`，新库可能已有写入 |
| started | 新库已启用，旧库封存 | 验收后 finalize；回退需保留并迁回新写入 |
| finalized | 原 control 已删除，备份仍在 | 恢复需维护窗口和逻辑恢复，不是代码回滚 |

正式切换前若决定放弃，先停全部应用，确认新库从未被应用写入，再由操作者恢复源库连接权限、原 `.env`、旧日志拓扑身份和标记。不能仅把 `.env` 改回去。任何新服务开始写入后，旧备份都不再代表最新控制状态，必须另行执行反向迁移；脚本不提供会丢掉新写入的自动回滚。

老版本二进制仍可能强制 control 在 M 上，不能在新布局上直接进行旧版本回滚。历史网关回滚夹具仍只适用于迁移前布局。

## 验收

- 迁移前后任务 ID、模式、状态、重跑关联和迁移 ledger 一致；活动任务按现有恢复机制继续。
- 前端能读取任务列表/详情、数据源进度、通知和系统设置；管理操作的新记录只出现在 db-log/control。
- 业务查询仍从 M/S 槽位读取，发布版本、快照和读者登记一致。
- 两类日志保留既有事件，旧缓冲可正常投递；不存在拓扑标记不匹配错误。
- db-log 有 control/logs/upstream_logs 三库；finalize 后 db-m 不再有 control。
- 重启部署不重新创建 db-m/control；备份保留到运维确认的恢复期限。
