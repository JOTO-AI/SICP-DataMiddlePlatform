# P28 日志存储配置与迁移边界

**当前更新：P28.01—P28.04 后端代码已完成，尚未实际部署或运行验收。下面的 P28.01 说明为历史记录；当前装配及切换流程以文末 P28.03/P28.04 为准。**

日期：2026-10-12。本步提供独立上游日志 Registry、凭据校验、物理身份检查和迁移基础。**双库投递/连接池/Compose 新服务尚未装配**，见 P28.02—P28.04；本步不表示双日志库已部署或可用。

## 显式配置

| 参数 | 规则 |
| --- | --- |
| LOG_STORAGE_MODE | 进程必须显式设置 single_legacy 或 dual，缺失/空值/未知值拒绝 |
| DATABASE_LOG_URL | 平台日志固定目标，沿用原变量及 logs_001/002 历史 |
| DATABASE_LOG_USER/PASSWORD | 平台库分离凭据，成对非空，不能与同一 URL 内嵌凭据混用 |
| DATABASE_UPSTREAM_LOG_URL | dual 必填；独立端点，不得与 M/S/control/平台日志实例重叠；不能回退到平台库 |
| DATABASE_UPSTREAM_LOG_USER/PASSWORD | 上游库独立分离凭据；不继承 DATABASE_* 或 DATABASE_LOG_* 凭据；保留完整 URL 的既有认证写法，但禁止混用 |

Registry.LogMode 返回明确模式；LogConnection 仅接受 platform/upstream。single_legacy 不提供 upstream Connection，存在非空上游配置时拒绝加载。字符串化 Registry/Connection/身份检查结果及错误均不包含 DSN 或密码。

现有 Compose 显式传入 `${LOG_STORAGE_MODE-single_legacy}`，让未增加新变量的旧 .env 继续使用原管线；模板明示 single_legacy。**直接启动二进制或迁移程序必须补充 LOG_STORAGE_MODE=single_legacy**，不读取或改写用户现有 .env。

Registry 可以校验 dual 配置；目前 Runtime.Validate、旧 PostgreSQL adapter 和 db-migrate 入口仍拒绝 dual。P28.03 接通独立资源与就绪检查后才撤销这些临时门槛，不能把“配置解析成功”当作“双库运行成功”。模板中的上游参数目前为注释说明，尚未传入 Compose。

## 实际数据库身份

URL 检查可以发现同主机/端口和相同数据库，但不能识别 DNS 别名。新增 VerifyLogIsolation 用只读事务读取集群 system_identifier、当前库 OID 和库名；集群标识来自 [PostgreSQL 控制信息函数](https://www.postgresql.org/docs/18/functions-info.html#FUNCTIONS-CONTROLDATA)，库 OID/名称来自 [pg_database](https://www.postgresql.org/docs/18/catalog-pg-database.html)。

任一日志库与业务/控制/另一日志库具有相同集群标识，即使库名不同也拒绝。副本或克隆保留相同 system_identifier 时保守拒绝，不声称不同 URL 或不同库名代表独立实例。库名与配置不一致、身份缺失/异常、超时、权限不足或清理事务失败均视为未验证。

这是部署预检：总读取预算 5 秒，事务逐个释放，每次回滚有独立 1 秒预算；结果只在当前部署操作使用。迁移在执行目标事务内再核对完整身份，防止预检后路由到不同后端。预检使用获准读取控制信息的部署账号，不提升服务写入账号权限。

部署必须使用固定后端连接，不能通过会将同一端点随机路由到不同集群的代理。此处一次预检不是长期连接授权，也不是运行期健康检查。P28.03 还需实现每个日志源的独立连接、重新验证和降级；不能将“预检所有目标”用作运行期全局健康门槛，否则会让一个日志库故障阻断另一库。

## 追加迁移

新增 Domain=upstream_logs、版本 upstream_logs_001；目录/目标与平台 logs 独立，日志事件仍位于目标数据库的 logs.events。旧 SQL 文件未修改。新表只接受 call/attempt 和 started/finished，支持原有按实体、类型、失败任务尝试的索引。

roles.sql 追加 NOLOGIN 组角色 sicp_upstream_logger 和 sicp_upstream_reader；前者 SELECT/INSERT，后者仅 SELECT，两者均无 UPDATE/DELETE/TRUNCATE。部署账号负责 DDL 和 ledger，运行账号不可访问迁移元数据。实际 LOGIN、密码、实例和目录配置在 P28.03 落地。

dual 模式的日志迁移必须通过物理隔离预检，在写事务内重新核验身份后才建 ledger/执行 DDL；持迁移锁检查 ledger 不含其他 Domain。匹配 checksum 重入跳过，冲突失败回滚；迁移错误只返回阶段，不透传驱动错误、SQL 或凭据。提交顺序计数、epoch 和受限 writer 协议留给 P29，不以本步 schema 声称可靠 SSE 就绪。

## 升级、回退与当前限制

当前继续用 single_legacy 部署，不启动新库或运行新增迁移。不切换旧缓冲目标，不修改旧日志；P28.04 才实现旧缓冲排空和切换检查。完成 P28.03 前，不将 dual 配置用于现有服务入口。

本次只提交代码、迁移文件和配置说明，没有执行 DDL、创建数据库或操作真实服务。回退本步代码不涉及业务数据；以后应用新迁移后，代码回退不自动删除新库、撤销角色或还原数据，应按届时升级记录处置。

测试源码、review 和静态检查见 [P28](verification/P28.md)、[T28](verification/T28.md)。用户暂停测试运行，T28-01—04 仅有测试代码与编译检查，不标记实测通过。

## P28.03 当前装配

双库进程新增 UPSTREAM_LOG_DIRECTORY、UPSTREAM_LOG_MAX_BYTES、UPSTREAM_LOG_MAX_FILES，规则与平台目录/配额相同且不能重叠。两套 Journal、Quota、Buffer、Dispatcher 和 PostgreSQL pool 独立；日志 pool 不参与业务启动 Ping，所有日志连接按需建立。满配额只拒绝对应源新增事件，已存在文件保留，不阻止业务启动。

追加 logs_004/upstream_logs_002，建立仅部署账号可写的 deployment_identity。双库迁移在 DDL 后再次核验目标身份并记录源、集群、库 OID 和全目标拓扑摘要。运行账号只可执行固定 search_path、无动态 SQL 的 verify_deployment 函数，每笔日志事务核验当前实际后端与部署记录；不授予 writer pg_control_system 或身份表写权限。拓扑摘要不含凭据，轮换密码不要求重新迁移；更换数据库目标需要重新隔离预检。缺失 schema、身份不符和未核验都表现为对应能力未就绪，恢复后逐源重试。

Linux 双库配置位于 compose.dual-logs.yaml，显式选择 `SICP_DUAL_LOGS=1 scripts/sicp.sh ...`；overlay 设置 dual，并启用 db-upstream-log、独立 pg-upstream-log 数据目录和 query/worker 上游缓冲。需要在部署配置填 DATABASE_UPSTREAM_LOG_URL、独立 UPSTREAM_LOGGER_PASSWORD；进程收到独立 DATABASE_UPSTREAM_LOG_USER/PASSWORD。查询副本沿用固定 slot 锁，两个日志源各有该副本的目录。初始化脚本在上游实例只创建 upstream logger/reader 角色，不复用平台 logger 登录。

迁移是显式部署步骤：按 `infra → migrate → start` 执行（scripts/sicp.sh deploy 已按此顺序）。query/worker 不再依赖 migrate 或日志库健康，已迁移系统重启时单日志库离线不会间接卡住业务启动。首次未部署 schema 不表示就绪，日志保留在缓冲；禁止以 start 替代首次迁移。日志就绪状态与业务 query_ready/sync_ready 分离，single_legacy 明示兼容模式。

关闭先停业务生产，再并行关闭两套 Dispatcher，单侧阻塞不会占用另一侧排空机会；失败/未知确认保留文件并报告未完成。该代码已做静态审查和源码编译，但尚未执行测试、DDL、容器启动、角色权限或故障演练。

## P28.04 切换与回退边界

1. 保持原平台目标、原 AUDIT_KEY 和 single_legacy 配置，构建当前代码。运行 `scripts/sicp.sh log-register`：停止 query/worker/gateway/console，逐个锁定并登记旧查询 slot 1—32 中存在的目录及 worker 目录。只启动准备资源的一次性入口，不启动 HTTP、采集任务或通知发送。目录内 .legacy-target 保存 HMAC 目标摘要，不保存 URL/凭据。没有原目标登记的非空目录不能直接切为双库。
2. 配置独立上游库后，以 `SICP_DUAL_LOGS=1` 执行 infra、migrate、log-cutover、start，或 deploy。dual deploy 在迁移前停止旧生产者，并在启动新服务前运行 log-cutover。运行 log-cutover 同样先停止旧服务，逐个处理全部已存在查询 slot 和 worker；任何一步失败整体退出非零，不报告完成。
3. 每个目录由 Linux flock 独占；新进程在安装 Recorder 前，将旧缓冲全部通过**原平台目标**提交，旧 call/attempt 不搬到上游。只有提交确认、文件回收、旧 Dispatcher 关闭和目录确认为空后才写 .storage-mode。丢失提交应答时保留文件，重试依靠原 ID/原 payload 去重，绝不重新哈希。
4. 标记绑定完整拓扑和关联密钥；双库重启核对标记后各源独立降级。无旧数据的新目录可直接登记；旧非空缓冲遇原库不可达时必须拒绝完成升级，不能为了启动而丢弃文件。未知临时文件、损坏/半写标记也拒绝，保留现场供恢复核验。
5. 一旦某目录标记 dual，当前二进制拒绝用 single_legacy 打开该目录。部分目录完成、另一目录失败时继续修复并重试，不混用旧 writer。不要手工删除标记、锁文件或缓冲，也不能用不识别标记的旧二进制直接回退；P29 的 writer 协议升级还有单独回退边界。

平台历史日志原位保留。失败诊断并行读取平台历史和上游新记录，总返回最多 20 条，以 log_source + event_id 区分同 ID，按时间稳定合并；source 仍表示业务源表。响应新增 diagnostic_sources，列出各日志源 complete/reason。一个库失败时保留另一个库的证据并设置 diagnostics_unavailable；兼容旧调用的 FailedAttempts 在不完整时返回错误，不能伪装为空列表。

以上是可执行流程和代码边界，不是实际升级成功记录。本轮没有启动上述部署命令、数据库或容器，查询 mock 数据保持原样。
