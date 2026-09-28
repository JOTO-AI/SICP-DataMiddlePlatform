# SICP 本地数据中台

第一阶段 P01—P15 已实现：四个业务查询接口、十四类源采集、双份构建与发布、增量/强制全量、未决恢复、日志通知、独立管理认证及 Linux 部署。单元测试和 I01—I06 集成测试在 GitHub Actions Linux runner 上串行验收，结果见[第一阶段验收报告](development_docs/PHASE1_ACCEPTANCE.md)。

## 启动合成环境（Linux / Bash）

要求 Linux amd64、Docker Engine、Compose ≥ 2.30 和 Bash。服务、CI 与镜像统一使用 Go **1.26.8**。

```bash
./scripts/sicp.sh deploy
./scripts/sicp.sh initialize initialization-001
./scripts/sicp.sh task <返回的任务ID>
./scripts/sicp.sh status
```

`deploy` 准备配置、构建镜像、创建持久卷、迁移数据库并启动服务；数据初始化通过管理入口显式受理，使用 task 命令确认最终完成。默认 query、worker 分别向宿主机 `127.0.0.1:18080`、`127.0.0.1:18081` 提供 HTTP 服务。PG、Redis 和 Mock 仅连接内部网络。源数据来自只读 `mock_data`，邮件由本地 Mock SMTP 接收。

完整运行配置使用 `deploy/examples/*.env.example`，首次准备后保存在忽略提交的 `deploy/runtime/`；`prepare` 保留已有配置。这些 raw 环境文件不能直接作为 Shell 脚本执行。`configs/` 下早期最小示例不足以启动最终装配服务。

业务请求使用 `X-API-Key`，管理入口使用独立 Bearer 凭据及动作权限。`/livez` 表示进程存活，`/readyz` 按合法活动组合、实际依赖及恢复状态判断就绪。响应丢失时使用相同幂等键重试；失败重跑生成关联的新任务，历史不改写；重启先恢复未决任务。

详细操作见[部署说明](deploy/README.md)和[运行配置](development_docs/P15_RUNTIME_CONFIGURATION.md)。

## 验证与文档

[Linux CI](.github/workflows/ci.yml) 先运行 `scripts/check.sh`（单元测试、race、vet、语法检查、Linux 构建）及生命周期重复回归，再构建镜像、串行执行 I01—I06。普通单测不启用 integration 标签；集成测试仅在 GitHub 的隔离 PG/Redis/Mock 环境中运行。以下为工作流中的检查和构建入口：

```bash
./scripts/check.sh
bash scripts/sicp.sh build
```

检查需要 GCC/libc 开发头文件以运行 Go race detector；正式二进制禁用 CGO。`linux-unit-evidence` 和 `linux-integration-evidence` artifact 保存测试 JSON、覆盖率、容器和进程日志。容器使用非 root 用户、只读根文件系统、持久卷、内嵌时区数据及系统 CA 证书。I06 已覆盖空卷部署、四接口、管理受理、重启恢复、完整更新流程和真实 SIGTERM/SIGINT。

详细契约见[公共查询契约](development_docs/P06_QUERY_CONTRACT.md)、[字段矩阵](development_docs/P01_API_FIELD_MATRIX.md)和[契约启用状态](development_docs/P01_PENDING_RULES.md)；逐步开发与验收证据见[计划](development_docs/PHASE1_DETAILED_DEVELOPMENT_PLAN.md)及 `development_docs/verification/`。当前验收使用合成数据；生产容量和真实源联调属于后续阶段。
