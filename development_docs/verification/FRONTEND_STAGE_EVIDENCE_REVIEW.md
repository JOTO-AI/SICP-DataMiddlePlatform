# 阶段证据与分组摘要修复（2026-10-16）

V06，起点 300f60c。用户截图显示已完成 14/14 表且任务已发布，顶部前六阶段仍“尚未确认”，左侧分组仍“未观测结论”。

## 原因与修复

- 原阶段条只识别当前少数 phase 和 published_at，忽略 acquisition 及终态；操作树根节点由后端固定生成 state=unknown 的目录项，不是单次执行结论。
- 新增统一状态投影：使用逐表采集结果、当前执行阶段和任务结果。14 张已知表全部 collected 可确认采集，但独立封存尚无证据时显示“采集已完成 / 封存状态待记录”，不直接宣称发布。
- 已发布的已知流程可确认必要前置步骤完成；完成时间缺失单独标识，不伪造时间。增量/强制全量发布前必须两副本完成、转移及排空；初始化发布前必须两副本完成，但转移/排空不适用；无变化成功只完成获取封存/校验，后续重建/发布不适用。
- 运行中的已知阶段按流程前置条件展示进度；失败任务保留的旧 phase 不作为成功证据。初始化的 M/S 顺序由后端定义，增量/全量构建不按 M/S 猜先后。
- 操作树根分组显示采集数量/完成、双副本完成、数据已发布和同步进度确认摘要；清理/诊断显示分组入口。选中根节点不再渲染无意义的独立开始、结束、槽位等空字段。原始节点 JSON、叶节点状态、懒加载及游标校验不改。
- 日期字段缺失显示“未记录”，区分时间缺失与阶段结果。不会据采集 100% 把构建或发布标成成功。

## Code review 依据

- internal/operations/views.go: 根目录固定 unknown；采集分支来自 acquisition-progress，独立时间字段为空。
- internal/update/changed.go: build(0) → Shift → Drained 检查 → build(1) → ConfirmCopies → 发布。
- internal/update/initialize.go: M、S 两副本构建校验后发布，没有 route_shift。
- internal/update/unchanged.go: 完整目标与现有内容一致、双副本验证通过后提交进度，不调用发布器。
- internal/storage/control/progress.go: 发布和进度提交分开；published_pending_progress 不显示为整个任务已成功。
- 检查空采集列表、重复表、未知表、未知模式、失败残留阶段和未提交同步进度；不以阶段显示改变后端控制流程或提交条件。

## 验证

全量单元 127/127；状态投影补充复测 5/5。类型/生产构建、56 文件产物审计、150 项来源 hash 通过。

浏览器首轮 40/42，两处根分组旧文案断言更新；第二轮 40/42，一项登录恢复超时、另一项时间缺失旧文案断言更新。最终针对两项失败及详情/初始化布局的双 origin 8/8 复测通过，无未解决失败。已人工检查已发布桌面截图与初始化手机截图，原有五种宽度布局断言通过。原始报告/截图在 artifacts/frontend-stage-evidence，统计与 hash 见 [机器记录](frontend_stage_evidence_verification.json)。

Code review 已检查已发布前置条件、初始化/无变化跳过分支、未知模式、失败残留状态、14 张唯一已知表校验及无伪造时间，未发现未解决阻塞项。重现：frontend 下运行 npm run test:unit、npm run build；浏览器设置 SICP_TEST_BROWSER 后运行 npx playwright test console.spec.ts -g "BT09 |post-acquisition|manual incremental|F07 |V04 figure16|backend diagnostic|historical data|task detail progress" --workers=2 --fully-parallel，最终补测使用 -g "F07 event history keeps stable|F07 event history rejects repeated|V04 figure16|post-acquisition" --workers=1。

新单元用例以既有后端 DTO 和生成目录为基础进行状态/协议边界变体，没有编造业务行或阶段时间。浏览器补充断言沿用原有阶段、初始化和增量场景；原始 fixtures、后端代码未变。服务器联调继续按用户要求延期。
