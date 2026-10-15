# 图 16 更新详情复核（2026-10-15）

起点：039207d。本步 V04 按 16-update-detail.png 实现独立详情页。

## 实现与边界

- 顶部事件状态、标识、受理/开始/发布时间，七阶段横向进度，四个详情标签，左操作树右节点详情及构建目标表。小屏折行和上下排列。
- 进度和失败诊断分别进入操作树与异常标签；转移/发布和关联日志入口保留。5 秒轮询、终态停止、错误保留已确认值及旧请求取消不变。
- 后端未提供两次构建的先后顺序和每个阶段边界；阶段只用实际 phase 或 published_at 确认证据，不按 M/S 推断先后、不把未观测阶段画成成功。排队捕获没有构建节点，截图保持空态。

## Code review

- 核查任务身份验证、未知响应拒绝、树游标/节点边界、懒加载、选中节点切换、手动读取构建和标签键盘操作；没有新增写请求。
- header 仅在详情验证后暴露事实区域，修复首次回归发现的提前暴露与操作者定位问题；布局不改变诊断证据与原始 JSON。
- 新视觉用例复用后端生成的事件/操作节点，fixtures 和后端文件未改。review 无未解决阻塞项。

## 验证

- 最终浏览器 58/58（两个 origin），单元 122/122，类型/生产构建、58 文件产物检查、150 项来源 hash、20 组对比度通过。
- 首轮 51/58：事实区域和操作者定位问题修正后，全部相关 58 项重跑通过。
- 1440/1280/1024/768/390px 无整页横向溢出；人工查看桌面和手机截图，对照图 16 的信息层级与左右布局。
- 报告：artifacts/frontend-figure16/{first,final}-report.json；截图：artifacts/frontend-figure16/final/。
- 重现：frontend 下设置 SICP_TEST_BROWSER 为 Chrome 路径，运行 npx playwright test console.spec.ts -g "V04 figure16|F07 |BT09|post-acquisition|historical data|failed full|manual incremental|partial initialization|failed task|backend diagnostic|legacy and malformed|task detail progress|failed initialization|task page resumes|V03 update" --workers=2 --fully-parallel。

服务器 F12-02/-03 仍按用户要求延期，后端 T/OI 暂停、全部 28 图验收待办不变。
