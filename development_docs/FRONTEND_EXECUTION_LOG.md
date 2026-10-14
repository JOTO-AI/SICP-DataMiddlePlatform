# 前端逐步开发记录

## 执行约定（2026-10-14）

以 [搭建计划](FRONTEND_IMPLEMENTATION_PLAN.md) 为唯一任务清单，按 F00 → F01/F02 → F03 → F04 → F05/F06 → F07 → F08/F09 → F10 → F11 → F12 推进。每个编号步骤完成实现、适用验证和代码复核后，勾选对应项，独立提交并 push；push 成功后才开始下一步。若发布受阻，保留成果和阻塞记录，不跳过门槛。

前端查询与运行状态始终来自后端。正常路径测试读取现有 `mock_data/snapshots/initial`、`responses` 及后端固定预期，不重新编造人员、组织、成绩、课表和班级关系。DTO 保真夹具只用于协议测试，不冒充正常查询结果；边界/失败使用仓库已有对应场景。Mock 测试、真实后端联调、真实网关验收分开登记，未执行不得勾选。后端 T/OI 暂停约束保持不变。

## F00-01 图稿追踪基线

- 基于提交：`b83ac1d`，分支 `feat/frontend-development`。
- 输入：V5 图库 28 张 PNG、图库 manifest、搭建计划、设计方案、Hub 主题，以及当前后端契约代码。
- 交付：[图稿追踪矩阵](FRONTEND_VISUAL_BASELINE.md)；`frontend/scripts/check-visual-baseline.mjs` 校验图像完整性、需求/实施/测试编号及覆盖数量。
- 复核范围：逐图固定布局、重要状态和契约修正；不表示页面已实现，也不表示图稿已获得人工验收。
- 数据：没有新增或修改业务 Mock；图库中的示例值不进入运行时代码。
- 验证：Node v24.15.0，`node frontend/scripts/check-visual-baseline.mjs` 通过，28 图的 SHA-256/尺寸/字节数一致，R01 至 R11 全覆盖，所有实施/验收编号存在。首次暂存检查发现原计划的 Markdown 双空格换行触发行尾空白告警，已改为等效反斜杠换行，提交前复查。
- 代码复核：已修正课表/组织/人员/教学班的验收编号映射；校验器从脚本位置解析路径，拒绝缺图、重复图、哈希或版本变化、缺列和未知编号。当前复核无遗留问题；此为本次代码自审，最终人工验收项不勾选。
- 提交与 push：本步骤提交后推送 `origin/feat/frontend-development`；成功后的 SHA 与结果在下一步记录中补登，失败则保留阻塞记录。

## F00-02 逐端点接入核对

- 前一步：`283dd68` 已成功 push 至 `origin/feat/frontend-development`；Git 返回 `[new branch] HEAD -> feat/frontend-development`。并行只读 ls-remote 曾超时，不影响 push 已返回的成功结果。
- 基线：[端点接入表](FRONTEND_ENDPOINT_BASELINE.md)，覆盖认证/健康/旧任务、九业务、目录/运维/源测试、通知/维护及双源日志。
- 验证：Node v24.15.0，`node frontend/scripts/check-endpoint-baseline.mjs` 通过：47 个方法/路径组合、九业务、十四源与既有管理 HTTP 样例均有登记。没有运行后端 T/OI，没有发送副作用请求。
- 代码自审：逐项对照路由、DTO、解析器与接入包；修正本地业务 code 的字符串语义，区分业务 422/管理 429，明确 Organization 与 Department 的 is_active 可空性差异、根操作节点省略 parent 参数。路由校验包含集合双向比较，拒绝漏记或虚构端点。
- 本步骤完成后勾选 F00-02，提交并 push；下一步 F00-03 处理差异关闭范围。真实接口可用性与页面验收均保持未勾选。
