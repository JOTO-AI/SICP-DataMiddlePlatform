# F00-05 · 前端技术基线

2026-10-14，继续在 `frontend/` 增量迁移。

| 能力 | 已锁定方案 | 核对 |
| --- | --- | --- |
| 组件与类型 | Vue 3.5.43 / TypeScript 5.9.3 / vue-tsc 3.3.11 | 沿用 lockfile，不升级 |
| 构建 | Vite 7.3.6 / plugin-vue 6.0.9 | `/console/` base、同源代理、Docker 静态部署继续使用 |
| 路由 | 新增精确版本 vue-router 4.6.4 | npm 官方元数据与安装包声明 peer Vue ^3.5.0，与现有 Vue 匹配；唯一传递新增为 @vue/devtools-api 6.6.4 |
| 网络 | 浏览器原生 fetch / AbortController / ReadableStream | 后端数据为唯一查询来源；无浏览器直连上游或新代理框架 |
| 样式 | 自有 CSS，F02 统一 Hub tokens | 沿用已有组件与系统字体，不整体引入 Element Plus |
| 单元与协议 | 现有 Playwright 1.63.0，独立 playwright.unit.config.ts | `npm.cmd run test:unit`；不启动浏览器或开发服务，适用于状态/协议测试 |
| 浏览器 | 既有 playwright.config.ts 两个 origin 项目 | `npm.cmd test`；保留原有效场景，随页面迁移 |
| 数据 | `fixtures/v1` 后端导出 + 原始十四源响应 | `npm.cmd run check:fixtures`；夹具不进入 src 或 dist |
| 视觉/端点基线 | 图稿与实际后端注册对照 | `npm.cmd run check:visual-baseline`、`node scripts/check-endpoint-baseline.mjs` |

本机 Node v24.15.0，项目启动要求仍为 Node 22.12+ 或 24 LTS。仅新增两个依赖包；既有锁定版本、integrity、Vue 编译器和构建依赖均未变。安装使用 `--ignore-scripts --no-audit --no-fund`，未执行依赖生命周期脚本。

验证：`npm.cmd ls --depth=0` 无 peer 冲突；单元测试 2/2 通过；`npm.cmd run build` 类型检查与生产构建通过。F00 阶段没有接入新路由运行行为，其浏览器深链接/会话/懒加载回归属于后续 F01/F03。
