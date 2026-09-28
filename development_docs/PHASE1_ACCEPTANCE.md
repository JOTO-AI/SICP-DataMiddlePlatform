# 第一阶段验收记录

2026-09-28，验收基准提交 `c5a9e9ae168cde2e15f1b67fe62c0b9880dd9dba`，[GitHub Linux CI 36375442971](https://github.com/JOTO-AI/SICP-DataMiddlePlatform/actions/runs/36375442971) 完整成功。P01—P15/T01—T15 开发与单测完成后，串行实施 I01—I06；每个小步复核、提交、push，并等待对应 SHA 完整 CI，通过前不开始下一步。失败与修正记录保留在各阶段文档。

## 实际执行结果

- 单元测试 JSON：1186 条 pass，0 fail、0 skip；另通过 vet、Go race、30 次生命周期重复回归、Bash/Python 语法、Linux amd64 构建与镜像构建。
- 集成测试 JSON：58 个顶层编号，另 32 个子场景通过记录，共 90 条 pass，0 fail、0 skip。所有测试仅在 GitHub Linux runner 执行，本地仅编辑、格式、静态检查与编译。
- 17 个集成 Go 文件全部有 integration 构建约束，普通单元测试不发现 I 系列。各组顺序运行；独立编号隔离数据库、Redis 前缀、文件或完整 Compose 项目。

| 组 | 顶层编号数 | 主要验证与证据 |
| --- | ---: | --- |
| I01 | 7 | [真实数据库、迁移、权限、互斥](verification/I01.md) |
| I02 | 8 | [采集、封存、日志和通知](verification/I02.md) |
| I03 | 12 | [查询、缓存、分页与读者](verification/I03.md) |
| I04 | 13 | [初始化、双份切换、增量与全量](verification/I04.md) |
| I05 | 9 | [恢复、引用保护与实际文件回收](verification/I05.md) |
| I06 | 9 | [Linux 部署、完整流程和真实信号](verification/I06.md) |

I06-05 重启 query/worker、三组 PostgreSQL、Redis 和 Mock，验证原未决任务补确认，最终回归用时 35.99 秒。I06-09 的 direct/container × query/worker × SIGTERM/SIGINT 八个组合全部通过；193.22 秒为整组环境启动、验证和清理时间，每次被测信号退出另有 25 秒上限，服务配置退出预算为 20 秒。

`linux-unit-evidence` 保存单测 JSON、覆盖率和构建产物；`linux-integration-evidence` 保存逐组 JSON、退出码、Compose 日志和直接进程日志。各编号的数据来源和断言见 I 文档及 `tests/integration`；T 来源、子步骤和历史 CI 见 `verification/T01.md`—`T15.md`，逐步 review 与回退记录见对应 P 文档。

## 集成中补齐的实现

- Redis 活动元数据丢失时，仅在执行终止、控制确认和槽位可读证据匹配后，以原子缺失条件恢复已确认元数据；保留未决候选，下一周期重新核对。
- 成功任务接通退役版本文件回收：分页、实际 SQL、恢复与文件引用阻止删除；维护失败独立记录，不能撤销成功发布或重写业务时间。当前自动回收范围是已注册的历史发布版本，未发布/孤立文件不计为已完成自动回收。
- Linux 服务加入仅用于宿主机回环 HTTP 入站的 bridge；PG、Redis、Mock 保持 internal 网络。重启测试重新解析 Mock bridge 地址并等待 HTTP 就绪。

## 部署、恢复与回退

交付平台为 Linux amd64、Go 1.26.8、Bash/Compose；完整步骤见[部署说明](../deploy/README.md)和[运行配置](P15_RUNTIME_CONFIGURATION.md)。从空卷经 prepare/build/validate/infra/migrate/start，再显式 initialize 并查询任务最终状态；四接口、卷 UID/GID/权限、只读根文件系统和真实信号均已验证。

代码回退使用对应小步提交或阶段合并边界，不覆盖用户文件。持久数据恢复单独处理：先停止服务并保存数据库/Redis/文件卷，核对控制库确认、实际活动元数据和任务所有权，再按恢复规则启动；不能仅靠回退二进制撤销已经发布的组合、迁移或源进度。迁移 checksum 和历史记录保留，不能删除迁移记录或清空 owner/readers 来强行接管。

查询排序、文字成绩、课程同号、当前课程版本和课表缺关联等第一阶段规则已落实，见[公共契约](P06_QUERY_CONTRACT.md)与[启用状态](P01_PENDING_RULES.md)。测试只使用 mock_data、独立合成夹具、本地 Mock HTTP/SMTP、PG 和 Redis；不读取 source_samples，不调用生产源，不投递外部邮箱。配置/排序小样例位于 `tests/fixtures/phase1`，控制状态与故障夹具也以各测试内独立构造方式表达。

生产容量、吞吐、真实源时间/分页契约及生产网络与邮件联调仍属于后续阶段，本验收没有将这些内容计为通过。
