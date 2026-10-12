# 第二阶段升级、回退与验收

2026-10-12：九接口开发及验收入口已交付；本次未运行 Linux 测试、未部署或操作实际数据库。用户临时豁免 CI 等待，P 勾选只代表开发与静态复核完成，T/I 不因此勾选。

## 部署前检查

确认目标 Git 提交、query/worker/db-migrate 镜像摘要及可回退的上一镜像。备份业务四槽位、控制库、迁移 ledger 和持久化目录；确认没有运行中的构建、发布或维护任务。保留现有 `.env`，不要用示例覆盖凭据。部署服务名、命令入口与卷清理规则沿用 [部署说明](../deploy/README.md)。

新增迁移仅 `business_004`：排课 `(JXBH COLLATE C, PKH COLLATE C)` 与关系 `(JXBH COLLATE C, ID COLLATE C)` 两个班级游标索引。旧 business_001—003 文件和 checksum 不变，不新增业务表、不改变源列。DDL 采用现有事务迁移，会占用数据库资源，需要安排维护窗口与磁盘空间；不能跳过迁移直接启用新代码。

1. 暂停入口和应用，保留数据库、Redis 和卷；确认 SQL 读者已经排空，不能用 HTTP 请求结束代替实际 SQL 结束证明。
2. 使用本提交的 db-migrate 按既有部署入口迁移 M0/M1/S0/S1 全部业务库，确认四库 ledger 均包含 business_004、两个索引 valid/ready/live 且列与排序正确。失败时当前库事务回滚，修复后重跑；四库之间并非一个全局事务，全部完成前保持维护状态。
3. 启用同一版本的 query 与 worker，再恢复入口。九条 Definitions 全部装配；`/readyz` 还要求合法已发布组合与依赖可用，不能仅以 `/livez` 或 HTTP 进程启动作为业务就绪。
4. 使用占位示例替换后的合成/授权身份冒烟检查九接口，检查固定版本分页、完整字段、缺档 null、错误 data=null；8→9 同步使用 [联调指南](PHASE2_CALLING_GUIDE.md)。禁止把固定生产身份全量查询放入每次候选校验。

容量配置不新增必填项，默认 `QUERY_MAX_EXECUTIONS=8`、`QUERY_MAX_QUEUE=64`、`QUERY_EXECUTION_TIMEOUT=30s`，三个 `QUERY_DB_*_MAX_CONNS=16`；未配置时使用相同默认。按副本数核算总连接、日志磁盘和内存。业务请求仍受 20 秒、100000 行、1000 批、64 MiB 计费及 16 MiB 响应限制，见 [容量约束](PHASE2_CAPACITY.md)。可机读的九路由、固定依赖、默认分页和升级配置见 [phase2_delivery.json](examples/phase2_delivery.json)，测试与实际目录/解析器/配置加载器对照。

## 兼容与回退

历史证据中的业务探测仍只接受原四操作，冻结 `LegacyBusinessDefinitions()`，不会要求旧 JSON 补九接口。新候选使用 source-storage-v1，不运行固定身份的业务扫描；原 bp07-four-operations-v1 构建负载基线仍为四接口，不能改名算九接口容量达标。

回退 query/worker 到事先固定的上一镜像时，保留 business_004 索引和 ledger，不执行 DROP INDEX、删除迁移记录、清空卷或重写旧迁移。新增索引不改变源值，旧迁移目录可以复开新库；这项数据库兼容有 I11 自动化入口，**不等同于已验证任意历史镜像**。实际上一镜像仍需在隔离副本验证配置读取、历史证据、原四接口和重启，再执行回退。回退后旧镜像不提供五新接口，调用方须暂停这些同步任务，不能把 404 当空数据应用。

应用版本回退与数据组合回滚是两件事。不要手工改 Redis active 或控制库状态；旧组合是否完整、可读、未被覆盖应由既有受控流程确认。快照不保证无限保留，409 必须整轮重采。镜像重启/回退演练记录 commit、镜像摘要、迁移前后 checksum、四槽位索引与权限、原四及新五接口结果、恢复步骤和残留问题。

## 自动化验收入口与证据

仅在隔离的 GitHub Actions Linux runner 运行；本地工作流只编辑、格式化与静态复核。沿用 ci.yml 隔离 PostgreSQL/Redis 依赖，`SICP_INTEGRATION=1`、`SICP_TEST_*_HOST`，Go 版本跟随 go.mod。不要把这些用例接到生产数据库。

```bash
./scripts/check.sh
for group in I07 I08 I09 I10 I11; do
  python3 scripts/phase2_acceptance.py "$group"
done
```

| 组 | 新入口及复用范围 | 自动化证据范围 |
| --- | --- | --- |
| I07 | Phase2RealHTTPAssociations + 原 I03 字段回归 | 实际采集、构建、发布后九接口；学生完整字段、组织停用、教职工原联系方式、共同授课和缺档名单 |
| I08 | Phase2PaginationAndAtomicClient | 五接口三页不漏不重；真实 HTTP 执行 Python 8→9 脚本；不存在与存在零人区别；错误零应用在 T25 模拟末页覆盖 |
| I09 | Phase2CachePublicationAndOldSnapshots + 原 I03/I04 快照及发布用例 | 五接口冷热、无变化确认时间刷新、有变化发布、清缓存后的旧组合读取；复用同组合转移、过期和回收协议回归 |
| I10 | Phase2AuthenticationAndInvalidSources + 原 I03 SQL/取消/迟到缓存 | 五接口非法 key、跨 operation/过期 token、缺学籍、非法学制、跨学期歧义；共享协调器真实 SQL 未排空禁止覆盖 |
| I11 | Phase2FullScaleSourceQueries + Phase2IndexUpgradeRollbackAndReadPrivileges | 同量级合成源、全部人员页与热点班级；四槽位 003→004、失败事务回滚、重放、旧目录复开、源值和只读权限 |

精确测试名单在 [phase2_acceptance.json](examples/phase2_acceptance.json)。runner 保存每组 JSONL、stderr、exit_code 与 `*-execution.json`，绑定提交；必须同时有每个测试 run/pass 和 package pass，拒绝 skip、fail、空匹配、缺记录、超时或非零退出。`automated_pass` 仅表示清单中的自动化用例，不表示整组人工验收完成。CI 上传失败仍是工作流失败，不篡改测试结论。

I11 的容量报告另外记录 seed、数据分布、冻结预算、逐查询 SQL/行数/耗时/累计分配和 EXPLAIN；累计分配不是 RSS 峰值。总验收仍缺实际执行以及清单列出的人工证据：历史四接口证据启动新镜像、代表性并发下 HTTP 冷/热与峰值 RSS、同镜像重启、固定旧镜像回退。不能以小样本、单元测试或静态检查代替这些证据。恢复 CI 后按实际提交逐项修复、复核、复测，再勾选 T16—T25/I07—I11。
