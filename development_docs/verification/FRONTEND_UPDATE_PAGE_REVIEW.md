# 更新事件页去重与图稿对齐复核（2026-10-15）

起点：`0a1ee95`。用户要求保留“维护与详情”，其余执行过程统一进入任务详情查看，并修正更新事件列表与 15-updates.png 的结构差异。本步为 V03。

## 变更与图稿取舍

- 列表页删除自动更新说明、当前任务独立区、内嵌执行详情、诊断、阶段时间线、原始响应及其 5 秒轮询。查找任务直接进入 `/updates/:id`；成功受理只显示回执和详情入口，不把受理当作完成，也不自动加载详情。
- 标题统一为“更新事件”，右上保留全量更新入口与禁用原因。采用独立横向筛选栏、事件表格、行内详情/日志操作和底部分批导航；时间可选最近 24 小时、最近 7 天或自定义。
- 维护区保留全量、增量、失败重跑、单表修复及任务查找；幂等重试、待确认请求恢复、操作前校验继续执行。15 秒控制状态读取仅用于维护条件，不刷新历史表格。
- 详情页保留任务事实、进度、失败诊断、操作树及原始响应；加入用户选择的 5 秒刷新，终态或读取失败后停止，离页/隐藏时取消请求。补齐原内嵌视图中的单表目标信息。
- 列表 DTO `internal/operations/views.go:Event` 没有发起来源/执行阶段、总数和新增事件计数，故不伪造设计稿中的列值、总页数、跳页或“有 N 条新事件”。事件号与任务号使用后端现有稳定映射。耗时仅用开始与结束时间计算，缺失/未知状态保持未提供。
- 查看日志沿用既有显式跳转逻辑：用户点击后带原始 task_id 进入日志页并查询；仅打开列表不查询日志。

## Code review

- 检查 Tasks.vue 删除路径：无遗留详情请求、详情计时器或诊断组件；重新进入列表不会偷偷读取当前任务详情。维护状态校验和原请求重试仍可达。
- 检查 UpdateHistory.vue：服务端筛选、游标链校验、重复记录拒绝、过期游标处理、失败保留上次批次、生命周期取消保持原规则。新筛选控件不在修改条件时自动查询。
- 检查 UpdateDetail.vue：默认仅首次读取；用户开启轮询后按读取完成再安排下一次，防止请求重叠；失败停止并保留旧结果提示，终态停止，晚到响应不能覆盖其他任务。
- 修正未知任务状态不能直接显示“尚未结束”的问题；单表字段进行类型验证，不以缺失值补正常状态。
- 维护测试迁移到实际入口：详情断言走独立页，重跑与单表操作回到维护区。原有边界数据不改；新正常事件使用后端捕获投影，未新增业务行。后端与 fixtures 文件无变更。
- Review 无未解决阻塞项。

## 验证

- 最终相关双 origin 浏览器回归 **80/80** 通过，约 2.9 分钟；包含列表、维护、详情进度、错误诊断、幂等重试、筛选分页、操作树、旧链接、注销和页面切换。
- 单元测试 **122/122**；类型检查/生产构建、58 文件产物审计、20 组语义对比度、150 项夹具来源 hash 通过。
- 首轮 68/72；失败为单表选择器和新增日志跳转测试定位问题。随后 4/6，日志测试额外发现预期与既有“点击跳转后查询”行为不符；修正断言，验证 task_id/id_format 绑定，再重跑全部相关 80 项。原报告均保留。
- 1440/1280/1024/768/390px 均验证整页无横向溢出；人工对照 15-updates.png 查看桌面和手机截图。宽事件表在自身容器内滚动；维护区按用户要求保留在列表下方。
- 原始报告和截图：`artifacts/frontend-update-page/{first,recheck,final}`；汇总及 SHA-256 见 [机器可读记录](frontend_update_page_verification.json)。

重现命令（frontend 目录，PowerShell）：

```powershell
$env:SICP_TEST_BROWSER='C:/Program Files/Google/Chrome/Application/chrome.exe'
$env:PLAYWRIGHT_JSON_OUTPUT_NAME='../artifacts/frontend-update-page/final-report.json'
npx.cmd playwright test console.spec.ts -g 'V03 update|F07 |BT09|post-acquisition|historical data|failed full|manual incremental|partial initialization|failed task|backend diagnostic|legacy and malformed|task detail progress|unknown admission|abandoning an|failed initialization|initialization retry|update entry|task page resumes|overview distinguishes|F01 lazy|F03 logout|F04 empty|F05 management' --workers=2 --fully-parallel --reporter=line,json --output=../artifacts/frontend-update-page/final
npm.cmd run test:unit
npm.cmd run build
npm.cmd run check:production
npm.cmd run check:fixtures
npm.cmd run check:theme
```

本次不修改服务器 F12-02/-03 联调延期、既有后端 T/OI 暂停和整套 28 图综合视觉验收状态。
