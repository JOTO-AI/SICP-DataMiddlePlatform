# Phase 1 简易控制台

Vue 3 + TypeScript + Vite，中文浅色界面。仅连接 Phase 1 已实现的接口，不内置演示数据、不修改 Go 服务或认证规则。

## 启动

要求 Node.js 22.12+ 或 24 LTS。在仓库根目录执行：

```powershell
cd frontend
npm.cmd ci
npm.cmd run dev
```

打开 <http://127.0.0.1:5173/console/>。Linux/macOS 使用 `npm` 替代 `npm.cmd`。后端需另行按 [部署说明](../deploy/README.md) 启动；本地前端默认代理至 `http://127.0.0.1:18080`（query）与 `http://127.0.0.1:18081`（worker）。后端未启动时会显示连接错误，不会展示虚构健康状态。

后端运行在远程 Linux 时，可建立 SSH 端口转发：

```sh
ssh -L 18080:127.0.0.1:18080 -L 18081:127.0.0.1:18081 user@server
```

也可在 `frontend/.env.local` 指定服务器侧代理目标（修改后重启开发服务）：

```dotenv
SICP_QUERY_TARGET=http://127.0.0.1:18080
SICP_WORKER_TARGET=http://127.0.0.1:18081
```

这些变量不使用 `VITE_` 前缀，不进入浏览器构建产物。目标应为你管理的服务。

## 使用

1. 在连接设置中填写业务 `QUERY_API_KEY` 和管理令牌（令牌本身，不带 `Bearer `）。统一在根目录 `.env` 配置，部署模板不提供默认凭据，请使用实际配置值。管理令牌须与 `MANAGEMENT_CREDENTIALS` 中的 token 一致，并同步 `MANAGEMENT_TOKEN` 供命令行使用。不在页面预填任何凭据。
2. 在运行总览检查 query 和 worker 的存活与就绪状态。空数据环境通过更新任务中的“首次初始化”建立数据。
3. 在业务查询中选择学生成绩、批量成绩、学生课表或教师课表，填参查询。课表必填学年与学期；批量成绩不填学号表示查询所选学年内符合条件的成绩。高级 JSON 支持页面列出的其他筛选字段，最终合法性由后端校验。
4. 成绩上下翻页回传 `X-Query-Snapshot`；改筛选条件或点击“查询数据”开启新查询。遇到 409 请重新查询。
5. 更新任务需确认后提交。202 只表示已受理；用返回任务号查看详情，可开启 5 秒轮询。失败任务重跑会创建关联的新任务。
6. 响应丢失或提交失败时页面保留原操作与幂等键，使用“重试原请求”。“结束本次请求”只释放前端表单，不取消后台任务；不要据此推断原任务未被受理。刷新前请自行记录任务号与幂等键，页面不会持久保存它们。

凭据、查询数据与任务详情仅存在页面内存；刷新或关闭即释放。页面明确清除凭据时也会销毁缓存页面。当前仅适合持有全局业务密钥的可信管理员，未实现面向普通学生/教师的账号与数据范围授权。生产入口由部署方配置 HTTPS 和访问范围。

## Docker Compose 部署

console 与后端、数据库等服务都在根目录同一份 `compose.yaml` 中。在仓库根目录首次复制统一配置后启动：

```sh
cp .env.example .env
# 编辑 .env 中的凭据、真实上游及邮件配置（默认 production）
docker compose up -d --build --wait --wait-timeout 300
```

Windows PowerShell 使用 `Copy-Item .env.example .env`；已有 `.env` 不要重复覆盖。打开 <http://127.0.0.1:18082/console/>。镜像内 Nginx 同源代理至 `query:8080` 和 `worker:8081`，业务、管理凭据分别透传。端口由根目录 `.env` 的 `SICP_CONSOLE_PORT` 设置。

仅重建控制台可执行 `docker compose up -d --build --no-deps console`；只停止控制台可执行 `docker compose stop console`。开发测试可将 `.env` 的 `ENVIRONMENT` 改为 `development` 以启用合成数据源。完整操作见[部署说明](../deploy/README.md)。

也可以 `npm run build` 后将 `dist/` 静态资源部署到 `/console/`，参照 [nginx.conf](nginx.conf) 配置同源网关。`npm run preview` 只用于本地预览。

## 验证与范围

```sh
npm run build
npx playwright install chromium
npm test
```

Playwright 测试拦截 HTTP 并使用合成响应，验证前端协议与交互，不连接真实源或替代 Phase 1 后端集成验收。可通过 `SICP_TEST_BROWSER` 指向已有 Chromium。CI 独立运行前端构建、浏览器测试和控制台镜像构建。

已实现：双服务健康、四接口筛选与结果表格/JSON、成绩快照分页、初始化/增量/全量/重跑、按 ID 查任务、可停止轮询、独立凭据和可选部署。

尚未纳入：服务器任务历史列表、日志检索/SSE、源状态目录、邮件配置、完整九接口中心。这些需要额外后端 API；当前接口约定见 [公共查询契约](../development_docs/P06_QUERY_CONTRACT.md)。

实现参考：[Vue TypeScript 与组合式 API](https://vuejs.org/guide/typescript/composition-api)、[Vite 静态构建](https://vite.dev/guide/build)。
