# 运维后端升级与回退手册

状态：可操作材料，未执行升级或回退演练。本文不作为通过证据。使用根 `compose.yaml`；两类日志是同一个 db-log 服务内的 logs / upstream_logs，独立于 M/S。它们有独立逻辑库、角色、迁移、缓冲和提交顺序，共享实例故障及连接额度。

## 升级前记录

控制库迁移是独立的停写操作：新部署 control 位于 db-log，已有 db-m/control 按 [控制库迁移方案](../deploy/CONTROL_DATABASE_MIGRATION.md)执行，不能只修改 `.env`。迁移后 pg-log 同时包含控制数据，禁止删除该目录来清空日志。旧二进制若强制 control 位于 M，不支持在新布局上直接回退。

登记当前 SHA、镜像 digest、项目名、SICP_DATA_ROOT、卷/缓冲目录清单和原日志目标；秘密只保存在原配置/备份系统中，不写入报告。保持原 AUDIT_KEY、SNAPSHOT_KEY、平台 URL、项目名和目录。先停旧生产者并备份控制库、两日志库、M/S、持久缓冲及其标记、构建/恢复所需文件，记录一致恢复点、时区、备份摘要和可恢复性。不要只备份 events：receipts、commit_state、cleanup_receipts、控制引用退休/身份映射和通知事实同样需要保留。

已有卷不会因为 `.env` 改密码自动改角色密码。标准 `.env.example` 的 QUERY_PASSWORD/WORKER_PASSWORD 同时供服务业务连接和日志读取/维护；LOGGER_PASSWORD/UPSTREAM_LOGGER_PASSWORD 只供各自 writer。应用管理员 ADMIN_USERNAME/ADMIN_PASSWORD 具有全部管理能力，无需新增 LOG_CURSOR_KEYS/LOG_CURSOR_ACTIVE/LOG_INTERNAL_TOKEN 或四个 READ/MAINTENANCE URL；标准部署从现有 SNAPSHOT_KEY 分域派生日志签名/内部令牌。不要把数据库管理员或 writer 账号用作日志 reader。

以下命令在仓库根目录、Linux 部署机执行；本轮没有执行。先填写既有 `.env` 中新增的上游端点/独立 writer 密码，保留上述标识。正式部署不得用合成测试秘密。构建新镜像不自动启动业务：

```bash
scripts/sicp.sh build
scripts/sicp.sh validate
```

## 旧单日志部署

```bash
scripts/sicp.sh log-register
scripts/sicp.sh infra
scripts/sicp.sh migrate
scripts/sicp.sh log-cutover
scripts/sicp.sh start
scripts/sicp.sh status
```

1. `log-register` 自行停止 query/query-gateway/worker/console；仍须停止本项目之外连接相同库/缓冲的旧 writer，不能留下另一套旧 Sender。一次性进程使用 single_legacy 登记原目标，锁定所有已有 query slot 1—32 和 worker 缓冲；不启动业务或发信。未登记的旧非空目录不得跳过此步。
2. `infra` 对已有 pg-log 卷只补建缺失的 upstream_logs/LOGIN，不重建 logs、不修改已有密码。外部数据库需按部署角色方案先建独立库和 LOGIN；两日志库可以同 cluster，但不能同 OID/同库，不能与 M/S/control 共实例。
3. `migrate` 显式应用不可变迁移，当前末版本为 `business_004`、`control_012`、`logs_009`、`upstream_logs_008`。ledger/checksum 不符应停止，禁止手改历史 SQL/ledger 伪装成功。日志暂不可达或缺库/权限时不绕过迁移。
4. 新 writer 只调用 append_event。迁移锁等待旧写事务退出；旧直接 INSERT writer 之后被权限/非空 sequence 阻止。旧载荷按 received_at,event_id 固定回填序号，不声称恢复历史真实提交顺序。
5. `log-cutover` 再次停旧服务，逐目录 flock，将旧 ID/原 payload 全部投回原平台库，确认后回收缓冲并写 dual 标记；旧 call/attempt 原位保留。提交应答丢失保留文件，重试用永久 receipts 去重；不重新哈希、不搬历史到 upstream_logs。
6. 只有所有目录排空/标记成功才 `start`。任何目录未恢复/未知文件/损坏标记/目标不符都停止升级；部分已完成时修复后重跑 cutover，不删除标记、锁文件或缓冲。`deploy` 内置 infra→migrate→log-cutover→start，但不能代替旧非空目录首次 log-register。

全新空目录不需要 log-register，按 build/validate/infra/migrate/log-cutover/start 执行即可。已有 dual 部署保留拓扑/目录/密钥，停止生产后应用追加迁移并 cutover；标记 dual 后不能用 single_legacy 重新打开。更换目标需要独立迁移与隔离核验，不能只修改 URL 指向空库。

## 启动后核对

按 [接入样例](OPERATIONS_API_EXAMPLES.md) 登录、查询 system/status，分别读取两源历史及 live→SSE，记录 epoch、high_water/floor、就绪状态与缓冲补投。日志身份未核验/权限或 schema 不兼容会显示相应能力未就绪；不能将查询空集代替部署验证。已迁移系统单逻辑库离线可逐源降级；整个 db-log 实例离线会同时影响两源。日志可用性不等同业务 query_ready/sync_ready。

日志读取角色由迁移授给 sicp_query/sicp_worker；受限 maintenance 仅授给 worker。外部库须存在对应服务 LOGIN/密码及组成员关系。query 不执行清理；worker 也无直接 DELETE/TRUNCATE 权限。没有必要给应用管理员另配数据库 URL。

通知设置只在首次从有效旧 MAIL_TO 导入，后续数据库设置优先；SMTP 不完整仅关闭通道，不阻止同步。旧已投递保持 accepted，明确拒绝保持 rejected，发送不明保持 unknown，不自动重发。停旧 Sender 后才启动新 Notifier。保存配置不发信，显式测试只使用已保存地址；演练只对 Mock 通道。实际邮件被 SMTP 接受之后不可撤回。

清理的控制退休栅栏先持久化，再确认目标日志引用，最后同事务删除前缀/推进 floor/写回执。可能出现“已退休但尚未删除”；不能撤销栅栏复活旧依赖。控制 task ID 与日志 HMAC ID 通过 control_012 的有界索引映射；未映射/密钥不匹配保守保护。恢复时先查日志删除回执，再决定策略，不能按当前预览已过期判断之前没删除。

## 失败、代码回退和数据恢复

| 场景 | 操作边界 |
|---|---|
| 缺上游库/密码、缺 SNAPSHOT_KEY 或 AUDIT_KEY | 修配置/预建库并重做校验；不回退单库假装 dual 就绪 |
| 任意目标不可达/身份未核验/迁移 checksum 不兼容 | 保留现场，修复后重试相同步骤；不强行 start 宣告升级完成 |
| 旧平台目标未恢复、旧 writer 未停止、缓冲未确认 | 升级未完成；原目标补投排空后再切换，不把旧 call 改投新库 |
| 新功能代码回退 | 保留兼容 append_event 的投递组件、dual 标记和新增 DB 保护；否则停写维护 |
| 直接换旧 INSERT writer / 恢复旧 Sender | 拒绝此回退；不得重授直接写权限，保持发信关闭以防重复投递 |
| 已删除载荷 / 已接受邮件 | 代码回退不能撤销；恢复载荷需协调备份及明确恢复点，邮件无法撤回 |
| 仅一库恢复旧备份 | 禁止直接开放；先协调控制区退休标记/作业/通知事实与日志回执的一致恢复点 |

数据恢复前停止所有生产者、读取及 Sender，记录备份恢复点与可能丢失的时间窗口。由数据库运维按已验证备份方式恢复关联数据，保留永久去重与退休信息；不能手工把 floor/high_water 归零或删 receipts。保留恢复点之后的外部 SMTP 已接受事实，不把恢复后的待发送记录自动重新发送。核对迁移 ledger 后，在每个恢复过的日志逻辑库用数据库运维身份执行以下事务，**保持服务停止直到全部核对完成**：

```sql
BEGIN;
SELECT epoch, high_water, floor FROM logs.commit_state WHERE singleton FOR UPDATE;
UPDATE logs.commit_state SET epoch = gen_random_uuid()::text WHERE singleton;
SELECT epoch, high_water, floor FROM logs.commit_state WHERE singleton;
COMMIT;
```

这不是应用管理 API，也不是一键恢复脚本。SQL 在目标库的已认证管理连接中执行，日志 reader/writer 不应有此权限；保存前后 epoch 和恢复点，不记录连接密码。两库分别轮换恢复后的 epoch，旧游标必须 409 后重新查询。待核对的清理任务遇 epoch 变化须保守停止并人工核对原回执，不能自动重复删除。数据/外部副作用没有天然跨库原子回退，未解决的事实不得标记恢复成功。

## 测试恢复后的验收入口

本轮仅编译/静态检查；测试执行仍暂停。恢复测试后先在隔离 Linux Actions、合成数据和 Mock 邮件/上游环境产出同 SHA 的 T27—T35 工件。前置 JSON 必须含 sha、backend_P27_P35=complete、T27_T35=passed，以及 T27—T35 各项非空工件 path/sha256；不可拿本开发记录代替。

```bash
python3 scripts/operations_acceptance.py --prerequisites artifacts/operations-units/prerequisites.json --capacity development_docs/verification/operations_capacity_baseline.json --output artifacts/operations-acceptance --seed 20261013
```

此命令要求 GITHUB_ACTIONS=true、SICP_INTEGRATION=1 及既有隔离集成环境；不建议本机绕过。输出目录须新建或空。运行器严格检查全部 32 项发现/执行/通过、子项 skip/fail、退出码、逐项 seed/时间和容量测量；超时/中断终止整个进程组，保留失败报告。冻结容量变更须先修改并复核基线，不能现场降低门槛；环境销毁沿用隔离入口的清理流程。实际升级/回退、权限和容量证据填写 [报告模板](PHASE2_OPERATIONS_ACCEPTANCE.md)，未执行项不得打通过勾。


源码证据对应：缺库/密钥与模板由 `TestT35_05UpgradeCommandsVersionsAndConfigurationGates`；不兼容 checksum 由 `TestT02_06MigrationReplayConflictAndRollback`；旧 writer 禁止绕过由 `TestT29_04BackfillAndNoLegacyWriterBypass`；原目标未确认和禁止回退由 `TestT28_13CutoverPreservesOriginalTargetUntilAcknowledged`、`TestT28_13UnmarkedOrOrphanedJournalCannotCompleteSwitch` 覆盖。当前这些测试均未执行，本段仅登记入口。
