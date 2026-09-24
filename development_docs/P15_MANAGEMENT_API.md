# 管理 API

管理服务独立使用 `Authorization: Bearer <管理 token>`，不接受业务 `X-API-Key`，也不能把业务 key 配成管理 token。每份凭据绑定唯一 operator 与明确 actions；token 必须为 16—4096 个无空白/逗号的可见 ASCII 字符，生产部署使用随机生成的秘密。运行中只保留 token 的 SHA-256 摘要。

| 方法与路径 | 权限 |
|---|---|
| POST /admin/v1/tasks/initialize | initialize |
| POST /admin/v1/tasks/sync | sync |
| POST /admin/v1/tasks/force-full | force_full |
| POST /admin/v1/tasks/{task_id}/rerun | rerun |
| GET /admin/v1/tasks/{task_id} | status |

缺失/错误/重复管理认证返回 401；有效凭据缺少对应动作权限返回 403。认证通过后操作者取配置中的 operator，请求正文、查询参数和 X-Operator 均不能指定审计身份。响应禁止缓存，错误正文只包含固定 code/message，不回显秘密。

初始化、手动同步及强制全量提交必须带唯一 `Idempotency-Key`（1—128 个无空白/逗号的可见 ASCII 字符）；正文为空或 `{}`，其他字段拒绝。相同操作者/模式/幂等键重试返回原任务；不能用相同键执行不同动作或查询另一操作者的任务。

事务提交后返回 HTTP 202，data 含 `task_id`、持久 `status`、`accepted_at`、`created`，Location 指向状态接口。`accepted` 仅表示已持久受理，尚未设置 `started_at`；工作进程使用自己的生命周期开始执行，客户端断连不会取消已提交任务。重复请求可能返回原任务当前 running/终态。

已有执行/待恢复任务/未决发布返回 409 `busy`；首次没有完整数据时同步或强制全量返回 409 `initialize_required`，提示初始化；已有完整数据时初始化返回 `already_initialized`。幂等参数冲突为 `idempotency_conflict`，提交未确认返回 503 `unavailable`，可携相同键重试。

状态返回 task_id、mode、status、operator、predecessor_id，以及独立的 accepted_at、started_at、ready_at、published_at、checked_at、finished_at；未知时间显式为 null。progress_confirmed 仅在任务完成成功进度事务后为 true，confirmed_until 来自该任务自身的不可变十四类进度检查点，不读取后续任务已推进的全局进度。counts 只输出经过校验的 SourceCounts/CopyCounts；failure 只输出固定失败码。不返回原始摘要、幂等键、执行者或源数据。

失败任务重跑使用相同的空正文和 Idempotency-Key 规则；模式由服务器从旧任务读取，新建任务并保留 predecessor_id，旧历史包括未知结束时间均不改写。不存在返回 404 not_found，非失败任务返回 409 not_failed；旧执行仍未释放或存在其他未决工作时返回 busy。同一重跑键返回原先受理的新任务。

P15.01—P15.03 已提供管理边界；服务启动配置与部署在后续小步完成，当前不宣称端到端管理服务已就绪。
