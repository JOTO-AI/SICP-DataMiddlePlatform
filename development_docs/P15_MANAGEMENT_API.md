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

P15.01 提供认证与路由边界，管理受理/状态处理器分别在 P15.02/P15.03 接入；服务启动配置与部署在后续小步完成，当前不宣称端到端管理服务已就绪。
