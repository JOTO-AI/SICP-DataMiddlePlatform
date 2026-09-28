# 运行配置与依赖装配

Docker Compose 的统一配置模板位于根目录 `.env.example`，复制为 `.env` 后使用根目录 `compose.yaml` 启动全部服务。Compose 将 `QUERY_DATABASE_*`、`WORKER_DATABASE_*`、`MIGRATE_DATABASE_*` 分别映射为对应容器的 `DATABASE_*`；下表描述的是进程内变量名。公共配置在 `.env` 只填写一次，凭据按服务分别传入。旧 `deploy/runtime/*.env` 不再读取。

双服务入口使用 `config.LoadRuntime`，先完整校验，再按 PostgreSQL → Redis → 持久文件 → worker HTTP 源 → worker 邮件创建。缺配置、缺工厂或初始化失败直接失败；释放部分结果及此前资源，顺序相反且有总预算，不回退内存存储。P15.05 接通真实查询、管理、日志、通知与串行 worker：每轮先读取恢复事实并证明旧进程/SQL 已停止，再处理任务或调度。

| 环境变量 | 约束 |
|---|---|
| HTTP_ADDRESS、SOURCE_TIMEZONE、STARTUP_TIMEOUT、SHUTDOWN_TIMEOUT、HTTP_READ_TIMEOUT、HTTP_WRITE_TIMEOUT、HTTP_IDLE_TIMEOUT | 延用基础服务配置；默认时区 Asia/Shanghai，时间预算须正值 |
| QUERY_API_KEY | 两角色均要求；用于查询鉴权及与管理秘密隔离校验 |
| DATABASE_M0_URL、DATABASE_M1_URL、DATABASE_S0_URL、DATABASE_S1_URL | 显式 postgres URL，端口/数据库/sslmode 必填；同实例槽位同端点、不同数据库，M/S 独立端点 |
| DATABASE_CONTROL_URL、DATABASE_LOG_URL | 控制库固定 M 端点且独立数据库；日志库独立端点 |
| REDIS_URL | redis/rediss URL，显式端口，固定 `/0`，禁止查询参数覆盖客户端行为 |
| DEPENDENCY_TIMEOUT | 默认 10s，可设 1ms—1min；连接及邮件提交使用有界预算 |
| SNAPSHOT_KEY、AUDIT_KEY | 不同的至少 32 字节秘密，与业务 key 分离；AUDIT_KEY 经 SHA-256 派生固定 32 字节日志 HMAC 密钥，重启/双服务需保持相同 |
| CODE_VERSION、ENVIRONMENT | 显式代码版本；环境为 test/development/staging/production |
| LOG_DIRECTORY、LOG_MAX_BYTES、LOG_MAX_FILES | 必填；独立预建持久目录，字节数 1024—1TiB，文件数 2—1000000；保留旧事件，容量不足拒绝新写入 |
| STAGING_DIRECTORY、TARGET_DIRECTORY、SPOOL_DIRECTORY | worker 必填；预建的三个独立持久目录，与日志路径不得重叠，实际符号链接解析后也不得重叠 |
| STAGING_MAX_BYTES、TARGET_MAX_BYTES、SPOOL_MAX_BYTES | worker 必填；各 1MiB—1TiB，目标/获取文件使用共享配额，spool 预算供后续任务编排使用 |
| SOURCE_ORIGIN、SOURCE_TOKEN | worker 必填；明确 http/https origin，无用户信息、查询和额外路径；无默认真实上游，禁跳转及环境代理 |
| SOURCE_SCOPE、SOURCE_QUALITY | worker 必填；scope 为稳定源范围标识，最多 256 字节；quality 是十四个源表名对应的 JSON 对象，每项含 MinimumRows、MaximumDeletedRows、AllowEmpty，计数不可负，不提供隐式宽松策略 |
| MANAGEMENT_CREDENTIALS | worker 必填；JSON 数组，结构和动作见管理 API 文档，未知字段/尾随 JSON 拒绝 |
| MAIL_ADDRESS、MAIL_TLS、MAIL_FROM、MAIL_TO | worker 必填；SMTP host:port、tls/starttls/none 和明确发件/收件地址；无默认外部目标 |
| MAIL_USERNAME、MAIL_PASSWORD | 必须成对配置；none 不允许认证，production 不允许 none |

真实 PG 使用 pgx v5.11.0 池与上下文事务，保留 nullable/JSON 原文/naive timestamp 的仓储绑定；六个池逐一检查连接，失败逆序关闭。Redis 使用 go-redis v9.22.0，关闭自动重试并遵守上下文，缓存 SET 不添加 TTL，活动路由继续使用完整值原子比较替换。参考 [pgxpool 文档](https://pkg.go.dev/github.com/jackc/pgx/v5/pgxpool)、[Redis 官方客户端文档](https://redis.io/docs/latest/develop/clients/go/connect/)。版本与校验和已锁定在 go.mod/go.sum。

邮件采用 SMTP，强制检查配置收件目标、TLS 证书验证与上下文取消。DATA 确认仅代表 accepted；不产生 delivered_at，不声称 SMTP 可保证去重。原始驱动错误、响应正文及凭据不进入外部错误。

T15-09/12 仅使用工厂替身、临时私有目录、httptest 与本机合成 SMTP 监听器，不连接实际 PG/Redis、真实上游或外部邮箱。真实驱动/迁移/权限及完整部署流程在全部 P/T 通过后由 I01—I06 验证。

worker 仅在 Linux 运行，持有 spool 卷内 `.worker.lock` 的进程锁；所有 worker 必须挂载同一个持久卷，禁止各自挂载独立卷同时运行。锁文件不能删除或替换。数据库连接带随机进程标识，接管前逐实例确认旧会话消失；同进程下一轮恢复须确认没有未结束事务。HTTP 超时、心跳过期和冷却到期均不能替代 SQL 结束证明。

SIGTERM/SIGINT 停止 HTTP 准入及后台新任务，HTTP → application → runtime 按逆序使用同一份 SHUTDOWN_TIMEOUT 总预算。超时返回非零退出及 incomplete 资源状态；保留控制事实与未确认日志文件，不自动删 reader/owner、不伪造结束时间。容器 stop_grace_period 必须大于该预算，入口直接 exec Go 服务。不得依据容器退出耗时推断实际 SQL 已结束，重启仍执行数据库侧证明。

`/readyz` 分别返回 `query_ready`、`sync_ready`、`dependencies_ready`。查询就绪要求 Redis 与已确认控制元数据完整一致、当前代次可读、查询角色能连接活动业务库并读取十四表；worker 同步就绪要求启动恢复完成、依赖可用且没有正在恢复/执行的任务。空环境可同步初始化但不能查询。候选校验使用 `SET LOCAL ROLE sicp_query`，需应用 business_002/control_006 迁移；不能使用 worker 写权限代替查询权限。
