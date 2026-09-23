# P01 待定契约与启用条件

- P06 已完成：响应外壳、错误码/HTTP 映射、参数上限、固定开放范围和可信身份责任见 [公共查询契约](P06_QUERY_CONTRACT.md)。
- P07：非数值成绩表达、课程同号唯一规则、历史课程信息版本、代码字典及重修码含义。原码保留，不猜名称；受影响语义确认后才实现。
- P09：未排课课程表达、多教师分隔符、源日期/周次/节次格式、表外作息；工号和姓名数组不假定下标配对。
- P11：合法组合读取协调、缓存与跨请求分页凭据。

`contract.Definitions` 记录阻止启用的规则，`Registry.Enable` 遇到待定规则返回 not_ready；清空调用方的规则副本不能绕过目录检查。`Register` 提供依赖注入骨架和隔离 Mock 测试。

两个真实入口均未装配业务处理器、合法组合或同步执行器，`/readyz` 持续 503，`/livez` 为 200；业务入口已校验 X-API-Key，无凭据返回 401，合法凭据下占位路由返回 503。严格解析器已在 P06 实现，随 P07—P10 业务处理器接入；当前服务默认监听回环地址。

用户已确认：文字成绩 score=null、不新增字段；同课程号多条报错；课程信息用当前版本；未排课课程不进入课表。相关规则在 P07/P09 实施及验证后才解除启用门槛，不能仅因语义确认就标记服务就绪。

内部错误类别：invalid_argument、unauthenticated、forbidden、not_ready、conflict、dependency_failure、deadline_exceeded、internal。启动/退出事件只记录资源名称与 closed/failed/incomplete 状态，不输出底层错误或凭据。

来源：[接口设计](../sicp-api-design.md) 第 4–7 节、[字段矩阵](P01_API_FIELD_MATRIX.md)、[阶段计划](PHASE1_DETAILED_DEVELOPMENT_PLAN.md)。
