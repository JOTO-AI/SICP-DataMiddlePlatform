# 查询契约与启用状态

- P06 已完成：响应外壳、错误码/HTTP 映射、参数上限、固定开放范围和可信身份责任见 [公共查询契约](P06_QUERY_CONTRACT.md)。
- P07/P08 已完成单人及批量成绩处理器：文字成绩 null、课程同号重复报错、当前课程版本、原码/重修配置保留，缺完整字典名称 null；各自保持独立范围、操作标识及共同 Grade 语义。
- P09/P10 已完成学生和教师课表处理器：未排课排除，缺计划号失败，多教师 ASCII 逗号分隔且独立数组，日期/周次/节次格式及表外作息边界见 [P09 开发记录](verification/P09.md)；教师完整工号/学期/课程与教学班筛选见 [P10 开发记录](verification/P10.md)。四接口目录规则门槛已解除。
- P11—P15 已接通合法组合固定读取、持久读者/SQL 排空、缓存与分页、双份发布、恢复及真实服务装配。I03—I06 已验证四接口、未决隔离和 Linux 部署就绪，证据见[最终验收](PHASE1_ACCEPTANCE.md)。

`contract.Definitions` 记录阻止启用的规则，`Registry.Enable` 遇到待定规则返回 not_ready；清空调用方的规则副本不能绕过目录检查。`Register` 提供依赖注入骨架和隔离 Mock 测试。

两个真实入口已装配业务处理器与依赖。`/livez` 表示存活，`/readyz` 动态检查查询、同步及依赖就绪状态；空卷尚未初始化时查询未就绪，合法组合发布后四接口可用。业务入口校验 X-API-Key，无凭据返回 401；管理入口使用独立凭据。Linux Compose 仅向宿主机回环地址发布 HTTP 端口。

用户确认的文字成绩 score=null、不新增字段、同课程号多条报错、课程信息用当前版本、未排课课程不进入课表，均已在 P07/P09 实施并验证。服务就绪仍以实际依赖和合法发布组合为准。

内部错误类别：invalid_argument、unauthenticated、forbidden、not_ready、conflict、dependency_failure、deadline_exceeded、internal。启动/退出事件只记录资源名称与 closed/failed/incomplete 状态，不输出底层错误或凭据。

来源：[接口设计](../sicp-api-design.md) 第 4–7 节、[字段矩阵](P01_API_FIELD_MATRIX.md)、[阶段计划](PHASE1_DETAILED_DEVELOPMENT_PLAN.md)。
