# 管理 API

管理服务使用管理员账号登录后的会话，不接受业务 `X-API-Key` 或旧管理 Bearer Token。部署配置为 `ADMIN_USERNAME`、`ADMIN_PASSWORD`；当前为单管理员模式，登录管理员可以执行下表全部操作，操作人由后端从账号取得。

`POST /console-api/login` 接收 JSON `{ "username": "...", "password": "..." }`，必须带 `Content-Type: application/json` 和 `X-SICP-Request: console`。成功返回 `data.operator`、`data.csrf`，同时设置 HttpOnly、SameSite=Strict 的 `sicp_session` Cookie；启用 `CONSOLE_COOKIE_SECURE` 后 Cookie 仅通过 HTTPS 发送。登录每分钟最多尝试 10 次，会话固定有效 8 小时，最多保留 64 个；worker 重启或修改凭据后重启会使会话失效。服务端认证器保留 PBKDF2-HMAC-SHA256 密码摘要与随机盐，不在响应中返回密码或会话标识。

`GET /console-api/session` 恢复当前会话信息。`POST /console-api/logout` 撤销当前会话并清除 Cookie。所有修改请求（退出、管理操作、控制台 POST 查询）须同时携带 Cookie、`X-SICP-Request: console` 和会话返回的 `X-SICP-CSRF`，跨站请求被拒绝，不开放 CORS。所有会话响应禁止缓存。

控制台查询通过 `/console-api/query` 转发，仅放行四个已有查询路径和健康检查，后端注入业务密钥；浏览器 Cookie、管理凭据、CSRF 值不会转发至 query。外部业务系统继续直接使用 query 的 API Key 接口。

| 方法与路径 | 权限 |
|---|---|
| POST /admin/v1/tasks/initialize | initialize |
| POST /admin/v1/tasks/sync | sync |
| POST /admin/v1/tasks/force-full | force_full |
| POST /admin/v1/tasks/{task_id}/rerun | rerun |
| GET /admin/v1/tasks/{task_id} | status |

缺失、错误、过期或重复会话 Cookie 返回 401；缺失或错误的防跨站请求凭据返回 403。认证通过后操作者取配置中的管理员账号，请求正文、查询参数和 X-Operator 均不能指定审计身份。响应禁止缓存，错误正文只包含固定 code/message，不回显秘密。

初始化、手动同步及强制全量提交必须带唯一 `Idempotency-Key`（1—128 个无空白/逗号的可见 ASCII 字符）；正文为空或 `{}`，其他字段拒绝。相同操作者/模式/幂等键重试返回原任务；不能用相同键执行不同动作或查询另一操作者的任务。

事务提交后返回 HTTP 202，data 含 `task_id`、持久 `status`、`accepted_at`、`created`，Location 指向状态接口。`accepted` 仅表示已持久受理，尚未设置 `started_at`；工作进程使用自己的生命周期开始执行，客户端断连不会取消已提交任务。重复请求可能返回原任务当前 running/终态。

已有执行/待恢复任务/未决发布返回 409 `busy`；首次没有完整数据时同步或强制全量返回 409 `initialize_required`，提示初始化；已有完整数据时初始化返回 `already_initialized`。幂等参数冲突为 `idempotency_conflict`，提交未确认返回 503 `unavailable`，可携相同键重试。

状态返回 task_id、mode、status、operator、predecessor_id，以及独立的 accepted_at、started_at、ready_at、published_at、checked_at、finished_at；未知时间显式为 null。progress_confirmed 仅在任务完成成功进度事务后为 true，confirmed_until 来自该任务自身的不可变十四类进度检查点，不读取后续任务已推进的全局进度。counts 只输出经过校验的 SourceCounts/CopyCounts；failure 只输出固定失败码。不返回原始摘要、幂等键、执行者或源数据。

失败任务重跑使用相同的空正文和 Idempotency-Key 规则；模式由服务器从旧任务读取，新建任务并保留 predecessor_id，旧历史包括未知结束时间均不改写。不存在返回 404 not_found，非失败任务返回 409 not_failed；旧执行仍未释放或存在其他未决工作时返回 busy。同一重跑键返回原先受理的新任务。

`sicpctl admin` 使用相同账号登录、携带会话提交请求，结束时退出登录；不再使用 MANAGEMENT_TOKEN。
