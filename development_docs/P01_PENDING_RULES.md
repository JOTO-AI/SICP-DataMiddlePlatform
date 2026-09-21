# P01 待定契约与启用条件

- P06：响应外壳、对外错误码/HTTP 映射、参数上限、固定开放范围和可信身份注入责任。P01 的 category 仅为占位状态，不是正式错误协议。
- P07：非数值成绩表达、课程同号唯一规则、历史课程信息版本、代码字典及重修码含义。原码保留，不猜名称；受影响语义确认后才实现。
- P09：未排课课程表达、多教师分隔符、源日期/周次/节次格式、表外作息；工号和姓名数组不假定下标配对。
- P11：合法组合读取协调、缓存与跨请求分页凭据。

`contract.Definitions` 记录阻止启用的规则，`Registry.Enable` 遇到待定规则返回 not_ready；清空调用方的规则副本不能绕过目录检查。`Register` 提供依赖注入骨架和隔离 Mock 测试。

两个真实入口均未装配业务处理器、合法组合或同步执行器，`/readyz` 持续 503，`/livez` 为 200；查询占位路由返回 503，不会伪造空业务成功。完整鉴权与参数校验在 P06 接入；当前占位服务仅供本地基础开发，默认监听回环地址。

内部错误类别：invalid_argument、unauthenticated、forbidden、not_ready、conflict、dependency_failure、deadline_exceeded、internal。启动/退出事件只记录资源名称与 closed/failed/incomplete 状态，不输出底层错误或凭据。

来源：[接口设计](../sicp-api-design.md) 第 4–7 节、[字段矩阵](P01_API_FIELD_MATRIX.md)、[阶段计划](PHASE1_DETAILED_DEVELOPMENT_PLAN.md)。
