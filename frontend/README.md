# SICP 管理控制台

Vue 3 + TypeScript + Vite，中文浅色管理台。浏览器通过同源会话连接后端；业务结果、运行状态、日志和配置均来自后端，不内置演示数据。

F01–F11 开发项已交付；F12 的本机回归、容量检查、交付文档与复核结果见 [交付复核](../development_docs/FRONTEND_REVIEW.md)。**2026-10-15 按用户要求，真实后端联调和实际网关验收延后至服务器部署完成后执行。** 浏览器拦截测试通过不等于真实服务已验收。后端 T/OI 验收保持原暂停状态。

## 启动与连接

要求 Node.js 22.12+ 或 24 LTS，使用仓库 lockfile 安装。在仓库根目录执行：

```powershell
cd frontend
npm.cmd ci
npm.cmd run dev
```

访问 <http://127.0.0.1:5173/console/>。Linux 使用 `npm` 替代 `npm.cmd`。后端按 [部署说明](../deploy/README.md) 另行启动；默认开发代理目标为 `http://127.0.0.1:18080` 统一网关。远程服务可先建立 SSH 转发：

```sh
ssh -L 18080:127.0.0.1:18080 user@server
```

也可在 `frontend/.env.local` 设置 `SICP_WORKER_TARGET=http://127.0.0.1:18080`，重启开发服务生效。这是 Vite 服务端代理配置，不使用 `VITE_` 前缀，不包含在浏览器构建中。HTTP 开发服务连接 HTTP 网关；HTTPS 验收直接使用受信证书的网关入口。后端不可达会显示错误，不能以此推断业务无数据。

## 导航与使用

所有地址位于 `/console/`；当前为单管理员、单环境，不提供学校切换或普通师生数据授权。

| 导航 | 路径 | 功能与边界 |
| --- | --- | --- |
| 运行总览 | `overview` | 分开显示查询可用性、更新事实、日志状态、组合及十四源；发布、检查、计划、确认时间不混用 |
| 接口中心 | `apis`、`apis/:apiId` | 九业务、十四源的后端目录与文档；管理操作为版本化说明；行政班独立查询仍待定 |
| 更新事件 | `updates`、`updates/:id` | 后端历史筛选/游标、任务事实、操作树、诊断进度及受控维护；事件标识沿用真实任务号 |
| 数据源 | `sources` | 十四源确认进度、窗口、失败与文档/任务/日志关联；没有失败记录不等于已确认健康 |
| 日志中心 | `logs`、`logs/platform`、`logs/upstream` | 本地接口、更新操作、上游调用、管理审计四视图；历史、实时及关联详情 |
| 通知中心 | `notifications` | 决策、逐人投递、逐次尝试、邮件设置及独立测试邮件 |
| 系统设置 | `settings`、`settings/:section` | 依赖/双源/容量、运行与部署配置摘要、保留期、单源清理 |

日志深链接为 `logs/:source/events/:eventId`，eventId 是后端 64 位十六进制 ID，不能用 request_id/task_id 代替。旧 `#overview/#queries/#tasks`、`queries`、`tasks` 和 `sync/tasks/:id` 映射正式路由；跳转不执行查询或更新。未识别路径显示 404。

### 接口测试和查询

从接口详情打开“在线测试”，打开、载入案例和教学班跳转只填参。业务请求同步走查询代理；源接口文档展示原上游 GET/Query，实际测试只提交固定接口/版本与单页参数的管理作业，不能覆盖 URL、Header 或凭据。JSON、表格、课表、组织树、响应头对应同一次响应，切换不增加请求。

七个业务分页接口各自保存 `X-Query-Snapshot`，固定条件和数据组合翻页；改条件或页大小重置，快照失效需明确重查。两个课表不分页。组织完整集合和教学班名单有显式读取上限；组合变化、人数不符、歧义不拼成完整结果。缺档、null、false、0、大整数和原始代码保持后端语义。

“业务数据核验”保留在接口中心内部，旧独立 Queries 页面已移除。任务维护区和独立更新详情共用进度与失败诊断组件。折叠原文展开后才渲染，默认显示 4096、展开最多 65536 字符；复制仅包含当前显示文本。服务端截断不能由前端恢复。

### 更新、日志和刷新

首次初始化由后端自动进行。立即增量、强制全量、单表更新、失败重跑依后端允许条件提交，服务端仍可拒绝。采集 100% 不表示构建、校验和发布完成；执行器恢复沿用事件，主动重跑创建新事件并链接前序。缺少日志不会改写任务控制事实，没有任意 SQL、裸槽位切换或无契约回滚按钮。

更新/日志历史默认最近 24 小时，自定义窗口最多 7 天，每批 50 条，按游标读取、返回已读批次；最多缓存 20 批。没有可信总页数，不能直接跳第 10 页。仅发送后端支持的筛选字段；原始 ID 与关联 ID 的 `id_format` 分开。敏感筛选只在内存传递，不进入 URL 或长期存储。

实时模式先读取 live 历史起点，再用 `stream_cursor` 建立可取消的流式 fetch；`next_cursor` 只用于历史。仅完整 event/checkpoint 推进续传位置，半帧、心跳、control 不推进。断线退避，尊重 Retry-After；游标/epoch/floor 失效明确提示缺口，需重新读取。历史、可见实时记录与等待缓冲合计最多 1000 条；新记录不抢滚动位置，裁剪可转历史补查。

总览、源和任务列表正常 15 秒刷新；活动任务正常 5 秒，终态停止密集轮询。邮件测试/清理作业按 3 秒、最多 15 次读取，之后可手动查询。隐藏页面暂停普通轮询和实时连接；离页取消旧请求，迟到响应不能写回新页面。GET 全局最多 4 个在途、16 个排队；写操作不自动重试。

### 邮件与维护

邮件配置使用字符串 revision，最多 20 个收件人，当前表单接收不带显示名称的普通邮箱地址；三条规则为 `source_schema`、`task_failed`、`source_test_failed`。空地址必须关闭通知，未配置 SMTP 仍可关闭。冲突保留本地编辑，重新读取后明确比较、采用或重基版本。

保存配置不发信；“发送测试邮件”只使用已保存版本与收件人。202 只表示作业受理，accepted 仅表示通道接受，unknown 不自动重发，not_observed 不等于健康。通知决策和逐人投递分别查；测试事实关联仅在实际已读批次内精确匹配，不能冒充完整过滤结果。额外规则/阈值与全历史通知筛选仍待后端增补。

系统页只展示已知安全字段，null/unknown/unavailable 不替换成零或健康。运行配置可编辑；凭据、SMTP、连接、快照密钥等部署配置只显示摘要。保留期每源 1–3650 天，保存不会自动清理。

清理每次固定一个日志源：先预览数量、范围、保护原因、版本与期限，再确认提交该 preview_id。零数量无需提交，过期或策略变更重新预览。实际删除数量必须有匹配来源、作业和回执的已确认结果；running、未知受理不显示 deleted=0。两个源分别追踪，不承诺原子删除。

### 会话、恢复与隐私

使用部署配置中的管理员账号密码登录。密码不写 URL、日志或浏览器存储；Cookie 为 HttpOnly、SameSite=Strict，Secure 由统一网关的 HTTPS 配置派生。刷新恢复会话；退出/401 清除业务缓存、关闭请求/流和恢复记录，旧响应无法恢复敏感数据。

任务受理不明时保留原键和原载荷，使用“重试原请求”。新作业的幂等键在 JSON，旧任务在请求头；关闭等待或结束表单不表示后台取消。当前标签页 sessionStorage 仅保存最多 8 条、15 分钟有效的白名单恢复元数据，不含邮箱、查询结果或响应正文；恢复页面不会自动 POST。存储禁用时仅能在当前页面恢复。查询数据与最近 50 条调用摘要只在内存，刷新/退出清空；持久追溯使用后端双日志库。

## 本机验证

在 `frontend/` 目录执行：

```powershell
npm.cmd run check:fixtures
npm.cmd run check:visual-baseline
npm.cmd run check:theme
npm.cmd run test:unit
npm.cmd run build
npm.cmd run check:production
$env:SICP_TEST_BROWSER='C:/Program Files/Google/Chrome/Application/chrome.exe'
npm.cmd test -- --fully-parallel --workers=2 --reporter=line --output=../artifacts/frontend-regression
```

CI 同样将功能回归与容量测试顺序分开，并保留 JSON 报告和构建清单。

Linux 可使用 `npx playwright install --with-deps chromium` 安装浏览器，无需 `SICP_TEST_BROWSER`。容量用例独立放在 performance/，不与功能用例并行，按预登记单 worker 执行：

```sh
npm run test:capacity
```

用例覆盖 loopback 和内网 HTTP 域名；使用[后端来源夹具](fixtures/README.md)，正常业务不新增编造数据。畸形、限流、响应丢失、日志容量等为明确标记的协议边界变体。独立报告保存在 `artifacts/`，它不受 Git 跟踪；需要长期留存时一并归档。

[性能阈值与范围](../development_docs/FRONTEND_PERFORMANCE.md)、[构建清单](../development_docs/verification/frontend_f12_build.json)和[容量结果](../development_docs/verification/frontend_f12_performance.json)随 F12-04 提交 `3dfee7b` 保存。构建清单记录测试前 HEAD、工作区变化、源码树 hash 和每个产物 SHA-256，因此测试时未提交的源码可与该提交准确对应；本机绝对路径不是服务器路径。截图路径与对应开发提交见[逐步执行记录](../development_docs/FRONTEND_EXECUTION_LOG.md)和[28 图基线](../development_docs/FRONTEND_VISUAL_BASELINE.md)。图片检查通过仅证明追踪和来源完整，不代表所有视觉断点人工验收通过。

## 构建、部署与回退

本次 main 统一诊断合并涉及后端日志格式：先更新全部 query 读取副本与 console，再恢复新 worker 写入。日志出现 `diagnostics[]` 后，不能单独回滚到不识别它的旧日志读取器；以下“只替换 console”命令适用于后端已经兼容的部署。任务和日志共用错误展示，历史 `response/authentication` 继续兼容，见 [合并复核](../development_docs/verification/FRONTEND_MAIN_MERGE_REVIEW.md)。

```sh
# 现有已配置部署：只构建并替换 console，不重启后端
docker compose up -d --build --no-deps console
```

在仓库根目录执行，浏览器默认访问 <http://127.0.0.1:18080/console/>。首次部署按 [统一部署说明](../deploy/README.md) 准备 `.env`；已有文件不要覆盖。根 Compose 已包含 console，镜像多阶段构建只复制 dist 到 Nginx，不将测试夹具/源代码放入运行层。`npm run build` 也可生成静态资源，但必须沿用 `/console/` 路径、[静态 Nginx 配置](nginx.conf)及统一同源网关；`npm run preview` 只用于本地预览。

升级前记录 Git SHA、后端版本、部署配置版本与当前 `sicp-console:phase1` 镜像 ID，将旧镜像另打唯一版本标签并保存离线镜像；静态目录部署则保留上一版整个 dist，不能只备份 index.html。升级后核对登录、深链接、查询、API 错误、SSE、Cookie/CSP 和 HTTP/HTTPS。

只回退前端时，先核对旧前端与当前后端路由/DTO/认证契约兼容，再将保留的旧镜像重新标记为 `sicp-console:phase1`，执行 `docker compose up -d --no-build --no-deps console`；静态部署整体恢复旧 dist。此时不要加 `--build`，否则会重新编译当前源码。网关或后端同时变更时，按部署说明整套回退，保留原项目名、数据目录和卷；前端回退不等于回滚业务数据。本轮未实际执行服务器部署或回退。

真实网关夹具 `scripts/check-gateway.sh` 限专用隔离 Linux CI；其环境准备不能在已有 `.env` 的工作目录运行，也不要伪造 CI 标志绕过约束。`gateway-e2e` 保留深链接、HTTP/HTTPS、Cookie/CSP、SSE 与实际代理测试，待服务器环境就绪后另行记录 F12-02/-03、FT09-08/-09。

## 常见问题与未交付边界

| 现象 | 处理 |
| --- | --- |
| 登录 429 / 连接失败 / HTML 响应 | 按 Retry-After 等待；核对代理地址及网关，不反复提交密码 |
| 页面有旧快照警告 | 当前读取失败，保留的是上次事实；恢复后手动刷新，不当作最新状态 |
| 源测试 result_unavailable | 缓存可能到期、重启、淘汰或位于其他实例；按原作业查回，不能据此自动重发 |
| 409 配置或查询冲突 | 配置重新读取比较；查询重新建立快照；不要静默覆盖或拼页 |
| 日志断线/缺口/无关联 | 核对当前源，按提示重取起点或历史补查；日志晚到/不可用不等于任务未执行 |
| 修改源码后服务器没有变化 | Git push 不会自动替换已运行 console 镜像；需按上述步骤部署 |

[D02/D07/D09/D12 等契约差异](../development_docs/FRONTEND_CONTRACT_GAPS.md)保持明确状态：扩展筛选、额外通知规则、无契约恢复/回滚、单源跨作业完整历史不伪造实现。数字跳页按用户要求延后。全部开发勾选与验收勾选见[搭建计划](../development_docs/FRONTEND_IMPLEMENTATION_PLAN.md)，未实测的服务器和完整视觉验收项继续留空。