# SICP 本地数据中台

P01 提供 Go 查询服务和更新服务基础、四接口 DTO 与路由骨架。业务查询、数据库、同步、鉴权和日志持久化按后续 P02–P15 接入。

## 本地运行（Linux / Bash）

使用 Go **1.26.8**，与 `go.mod`、CI、容器一致。

```bash
cp configs/query.env.example .env
# 编辑 .env，为 QUERY_API_KEY 填写本地测试值
set -a
source .env
set +a
go run ./cmd/query-service
```

更新服务使用 `configs/worker.env.example`，运行 `go run ./cmd/sync-worker`，默认端口 8081。环境变量是配置来源；配置文件不会自动加载。查询服务必须提供非空 API key，错误仅包含配置键名。P01 不读取真实上游、人员样本或邮件配置。

`GET /livez` 返回 200，`GET /readyz` 返回 503。四个 POST 业务路由返回 503/not_ready；业务规则和处理器尚未完成，不提供业务查询结果。`X-Request-ID` 由服务生成，不信任客户端传入值。请求上下文包含注入时钟的开始时间。SIGINT/SIGTERM 停止接收工作、关闭 HTTP、逆序释放依赖；超过总预算的资源记录 incomplete。

## 检查与镜像

```bash
./scripts/check.sh
docker build --platform linux/amd64 -f deploy/Dockerfile --build-arg SERVICE=query-service -t sicp-query:p01 .
docker build --platform linux/amd64 -f deploy/Dockerfile --build-arg SERVICE=sync-worker -t sicp-worker:p01 .
```

检查需要 GCC/libc 开发头文件以运行 Go race detector；正式二进制禁用 CGO。输出位于被忽略的 `artifacts/`，含测试 JSON、覆盖率与两服务二进制。容器使用非 root 用户、内嵌时区数据，保留系统 CA 证书。

详细契约见[字段矩阵](development_docs/P01_API_FIELD_MATRIX.md)和[待定规则](development_docs/P01_PENDING_RULES.md)；开发与验收证据见[计划](development_docs/PHASE1_DETAILED_DEVELOPMENT_PLAN.md)及 `development_docs/verification/`。I 系列集成测试在全部 P/T 完成后进行。
