# SICP 管理控制台

Vue 3 + TypeScript + Vite，中文浅色界面。使用管理员账号登录，后端通过 HttpOnly 会话校验身份；仅连接已实现的接口，不内置演示数据。

## 启动

要求 Node.js 22.12+ 或 24 LTS。在仓库根目录执行：

```powershell
cd frontend
npm.cmd ci
npm.cmd run dev
```

打开 <http://127.0.0.1:5173/console/>。Linux/macOS 使用 `npm` 替代 `npm.cmd`。后端需另行按 [部署说明](../deploy/README.md) 启动；本地前端默认代理至 `http://127.0.0.1:18080`（统一网关），查询由 worker 转发至 `CONSOLE_QUERY_URL`。后端未启动时会显示连接错误，不会展示虚构健康状态。

后端运行在远程 Linux 时，可建立 SSH 端口转发：

```sh
ssh -L 18080:127.0.0.1:18080 user@server
```

也可在 `frontend/.env.local` 指定服务器侧代理目标（修改后重启开发服务）：

```dotenv
SICP_WORKER_TARGET=http://127.0.0.1:18080
```

这些变量不使用 `VITE_` 前缀，不进入浏览器构建产物。目标应为你管理的服务。

## 使用

1. 使用根目录 `.env` 中的 `ADMIN_USERNAME`、`ADMIN_PASSWORD` 登录。密码至少 12 字节，且不得与 `QUERY_API_KEY` 相同。浏览器不接收业务密钥或管理 Token。
2. 在运行总览检查 query 和 worker 的存活与就绪状态。全新环境自动初始化，之后自动同步；无需提交日常更新。
3. 在接口中心的业务查询中选择九个入口：学生成绩、批量成绩、学生课表、教师课表、组织查询、学生信息、教职工信息、教师教学班、教学班学生。组织/人员可不传身份查询全量；课表和教学班必填学年、学期，班级成员还必填教学班编号和计划课程编号。高级 JSON 支持页面列出的筛选字段，最终合法性由后端校验。
4. 成绩与五新接口上下翻页回传各自的 `X-Query-Snapshot`；控制台明确发送每页 20 条，可选 50/100/1000，不改变后端省略 page_size 时的默认值。切换接口清空条件、结果和快照；改条件或点击“查询数据”开启新查询。失败续页清空旧结果，遇到 409 请重新查询。缺档显示“未提供”，仍保留编号；完整字段可展开 JSON 查看。
5. 更新任务展示最近 50 条记录和下次计划时间，每 10 秒刷新。点击任务查看详情；仅强制全量和失败重跑需确认后提交。202 只表示已受理；用返回任务号查看详情，可开启 5 秒轮询。失败任务重跑会创建关联的新任务。
6. 响应丢失或提交失败时页面保留原操作与幂等键，使用“重试原请求”。“结束本次请求”只释放前端表单，不取消后台任务；不要据此推断原任务未被受理。当前会话可在 sessionStorage 中短时保留白名单操作恢复信息，成功回执、主动结束和退出会清除；不会保存查询结果。

会话 Cookie 为 HttpOnly、SameSite=Strict，有效期 8 小时；刷新页面会恢复登录，退出会撤销会话并销毁缓存页面，会话过期自动返回登录页。密码不写入浏览器存储。查询数据与任务详情仅在页面内存中，刷新即释放。当前为单管理员模式，未实现面向普通学生/教师的账号与数据范围授权。统一部署根据证书配置自动派生 Cookie Secure。HTTP Vite 开发请连接 HTTP 网关；真实 HTTPS 验收直接访问网关并使用受信 CA。

## Docker Compose 部署

console 与后端、数据库等服务都在根目录同一份 `compose.yaml` 中。在仓库根目录首次复制统一配置后启动：

```sh
cp .env.example .env
# 编辑 .env 中的凭据、真实上游及邮件配置（默认 production）
docker compose up -d --build --wait --wait-timeout 300
```

Windows PowerShell 使用 `Copy-Item .env.example .env`；已有 `.env` 不要重复覆盖。打开 <http://127.0.0.1:18080/console/>。镜像内 Nginx 仅提供静态资源与 SPA，统一网关将管理请求转给 worker。业务查询由 worker 校验会话后经内部网关转发至 query。唯一端口由根目录 `.env` 的 `SICP_GATEWAY_PORT` 设置。

仅重建控制台可执行 `docker compose up -d --build --no-deps console`；只停止控制台可执行 `docker compose stop console`。开发测试可将 `.env` 的 `ENVIRONMENT` 改为 `development` 以启用合成数据源。完整操作见[部署说明](../deploy/README.md)。

也可以 `npm run build` 后将 `dist/` 静态资源部署到 `/console/`，参照 [nginx.conf](nginx.conf) 配置同源网关。`npm run preview` 只用于本地预览。

## 验证与范围

```sh
npm run build
npx playwright install chromium
npm test
```

Playwright 测试拦截 HTTP 并使用合成响应，验证前端协议与交互，不连接真实源或替代 Phase 1 后端集成验收。可通过 `SICP_TEST_BROWSER` 指向已有 Chromium。CI 独立运行前端构建、浏览器测试和控制台镜像构建。

已实现：双服务健康、九接口筛选与结果表格/JSON、七个分页接口的快照翻页、自动任务记录与调度时间、强制全量/失败重跑、按 ID 查任务、可停止轮询、独立凭据和可选部署。

尚未纳入前端页面：日志检索/SSE、源状态目录、邮件配置。对应运维后端已有实现，前端按 [搭建计划](../development_docs/FRONTEND_IMPLEMENTATION_PLAN.md) 逐步接入；实际契约见 [端点基线](../development_docs/FRONTEND_ENDPOINT_BASELINE.md)。后端实现完成不代表联调验收已通过。

实现参考：[Vue TypeScript 与组合式 API](https://vuejs.org/guide/typescript/composition-api)、[Vite 静态构建](https://vite.dev/guide/build)。

## 逐表采集进度

初始化失败且没有进行中任务时，顶部更新入口显示“重新初始化”，直接关联最近失败的初始化任务重新完整采集，无需复制任务号或重启服务。首次发布成功后该入口恢复为“强制全量更新”；状态读取失败或任务进行中时禁用并说明原因。

运行总览显示当前采集表的已完成页数、总页数和百分比；更新任务自动打开正在执行的任务，详情每 5 秒刷新各表进度。总页数来自上游总记录数与本次请求的 `SOURCE_PAGE_SIZE`，不足一页向上取整，空表计为已确认的 1 页。尚未取得首个结果时不显示虚构的百分比。

只有校验并保存成功的页面才计入完成页数，重试不会重复计数。100% 表示采集完成，构建、校验和发布仍以任务状态为准。失败任务保留最后一次进度；查询失败停止自动刷新并保留上次结果。旧任务没有这项记录时显示暂无逐页记录，需要部署新版 worker 才会为新采集生成进度。

## 九接口前端补齐（2026-10-12）

查询目录、表单与结果列集中在 src/queryOperations.ts；新增 Playwright 用例覆盖五接口请求、null/false/0、前后翻页、409 后重查、跨接口清空和 390px 布局。目录用例对照后端交付清单，避免再次出现前后端接口数量不一致。本次 vue-tsc 静态检查通过，浏览器测试与生产构建尚未执行；CI 按会话要求暂不等待。

已运行的旧 console 镜像不会随 Git push 自动更新。后端已升级到九接口版本时，拉取新代码后在现有部署根目录执行 `docker compose up -d --build --no-deps console`，完成后刷新页面。本次没有操作实际部署。

## 工程迁移进度（F01-01）

`src/app/` 负责启动和 Vue Router，`src/layouts/AppShell.vue` 负责控制台框架，`src/pages/` 放置登录与三个既有页面。总览、查询、任务按路由懒加载，KeepAlive 最多保留三个页面；停用后停止轮询和进度计时器，旧查询响应不写回重新打开的页面。退出/会话失效销毁缓存。

F01-01 完成基础路由后，F03 已将正式地址统一为 `/console/overview`、`/console/apis`、`/console/updates` 等七模块路径。旧 `#overview/#queries/#tasks` 和 queries/tasks 路径自动映射，导航不会自动执行业务查询或任务动作；详情业务内容仍按 F05/F07/F08 接入。

正常业务测试复用 [后端来源夹具](fixtures/README.md)。`npm.cmd run test:unit` 执行不依赖浏览器服务的协议/状态检查，`npm.cmd run check:fixtures` 核对来源与固定预期；`npm.cmd test` 继续执行两个 origin 的浏览器交互回归。可设 `SICP_TEST_BROWSER` 使用本地 Chromium/Chrome。完整逐步提交与复核见 [开发记录](../development_docs/FRONTEND_EXECUTION_LOG.md)。

## 待确认操作恢复（F01-06）

真正提交前，必要的操作元数据保存在当前标签页 sessionStorage：环境、登录主体、动作路径、原幂等键、白名单参数及可选回执位置。记录最多 8 条、有效期 15 分钟；不延长既有记录的到期时间，不挤掉未确认记录。密码、CSRF、人员查询结果和源响应不写入浏览器存储，也不使用 localStorage。

刷新并成功恢复同一主体会话后可恢复待确认任务，由管理员再次确认同键重试；页面加载不会自动重新提交。得到受理回执、主动结束请求、退出或会话失效时清理对应记录。存储被禁用时仍可在当前页面重试，并提示刷新将丢失恢复信息。新作业页面可复用相同存储接口，尚未接入的页面不计为完成。

## 只读轮询（F01-07）

总览和任务列表正常每 10 秒读取一次。连接失败后间隔增至 20、40、60 秒，成功恢复正常频率；有效 Retry-After 可延长至最多 5 分钟。任务详情正常每 5 秒读取，结束或失败即停止，保留上次读取结果。

标签页隐藏时暂停轮询并中止相关等待，重新可见时只恢复当前页面原先启用的读取；切页或退出仍按请求代次隔离。GET 全局最多 4 个在途、16 个排队，排队等待计入请求超时且可以取消。POST/PUT 不进入轮询或通用自动重试。

## 登录状态（F03-01）

登录继续使用同源 login/session/logout：错误凭据、429 限流、网络/服务不可用与异常响应分别说明，后端提供 Retry-After 时展示等待秒数，不自动重试。密码在每次提交结束后清空，不保存到 URL 或浏览器存储。未登录时保留合法站内原页面，登录后返回该页面，不执行查询；任意 redirect 外部地址不会作为跳转目标。

## 页面路由（F03-03）

正式路径统一位于 `/console/`：login、overview、apis、apis/:apiId、updates、updates/:id、sources、logs/:source、notifications、settings/:section，以及 403/404。旧 queries/tasks 路径重定向到 apis/updates，参数语义保留。未知地址显示 404 页面，不静默回总览。

认证恢复等待首次路由就绪；未登录时去 login，原合法站内路径暂存在当前页面内存，登录跳转成功后清理。刷新 login 页时没有保存原目标则回总览；显式 returnTo 仅接受可识别的站内路径，外部地址和 login 自环被拒绝。认证从不自动查询业务或提交动作。当前详情路由只建立页面结构，未完成模块仍明确显示“暂未开放”，后续步骤再接入接口。

日志详情固定为 `/console/logs/:source/events/:eventId`，source 为 platform/upstream，eventId 使用服务端原始小写 64 位十六进制 ID。`app/links.ts` 提供路径构造，不把原始 request/task ID 当 eventId，也不再次哈希。实际详情读取在 F08 实现。接口测试属于 `/console/apis/:apiId` 内的弹窗状态，F06 接入时绑定接口/版本/弹窗实例；不创建独立测试路由，打开页面不执行测试。

## 旧链接与退出（F03-05）

`#overview/#queries/#tasks` 分别映射 overview/apis/updates。设计基线中的 `/console/sync/tasks/:taskId` 及 `/console/tasks/:taskId` 直接转到 `/console/updates/:id`，保留 ID 与参数语义；不提交更新、测试或业务查询。总览内部链接已使用正式 RouterLink。

退出立即中止当前保护请求、销毁整个 AppShell/KeepAlive 缓存，并停止已有页面的轮询/计时器；session epoch 拒绝迟到响应。查询结果只存在内存，重新登录不会恢复旧表格，操作恢复记录也已清除。未来日志流仍必须接入同一会话取消范围，不能把本步验证冒充尚未实现的 SSE 验收。