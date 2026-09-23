# 公共查询传输契约

用户 2026-09-23 授权 P06—P15 串行实施；本文件补充 sicp-api-design.md 留出的传输层约定，不替换业务 data。

## 鉴权与响应（P06.01）

- 业务入口只接受一个 `X-API-Key` 头，值精确匹配 `QUERY_API_KEY`。不接受 URL、JSON 或 Bearer 替代；空值、多头、逗号合并值、空白和非 ASCII 值拒绝。配置为 1—4096 字节可见 ASCII，排除逗号；生产使用随机高熵密钥。
- 凭据校验在缓存、读取资格和 SQL 前。固定 key 仅代表可信服务调用；可信工具/MCP 必须在调用前限定终端用户可查询的学号、工号和批量范围。业务 key 不授予管理权限；管理认证在 P15 独立实现。
- 当前开放范围为接口 4—7。其余业务路径不注册；健康检查不要求业务 key。P06 不令未实现业务处理器就绪。
- 所有业务结果外壳固定为 `{"code":"ok","message":"OK","request_id":"服务端标识","data":{}}`；data 是接口文档的原业务响应。失败时 data 为 null。JSON 四字段均出现，Content-Type 为 application/json，Cache-Control 为 no-store。
- `request_id` 使用服务端 16 随机字节的小写十六进制，与 X-Request-ID 响应头一致，不接受客户端指定。随机源失效时返回 internal，标识为空，不伪造标识。
- 非法路径及方法仍使用 HTTP 路由层 404/405；下表适用于已进入业务处理的错误。错误正文不包含内部 cause、SQL、凭据或请求参数。

| code | HTTP | message |
| --- | --- | --- |
| invalid_argument | 400 | Invalid request |
| unauthenticated | 401 | Authentication required |
| forbidden | 403 | Access denied |
| not_ready | 503 | Service not ready |
| conflict | 409 | Snapshot conflict |
| dependency_failure | 503 | Dependency unavailable |
| deadline_exceeded | 504 | Query timed out |
| internal | 500 | Internal error |

未知错误映射 internal；成功数据编码失败也返回完整 internal 错误，不先写 200。

## 后续业务决定

用户本次明确确认：文字成绩的 score 返回 null，不新增业务字段；同课程号多条记录报错；课程信息取当前版本，不推断历史版本；未排课课程不进入课表，课表只返回实际排课记录。相关实现和独立测试分别在 P07/P09 完成，本记录不表示这些阶段已经验收。

排序沿用字段矩阵：成绩按 student_number、grade_id 升序；课表按源 PKH 升序，PKH 不出现在响应。参数及资源限额随 P06 对应小步补充。

## 请求解析（P06.02）

- 单个 UTF-8 JSON 对象，字段名严格大小写，拒绝未知字段、重复键（含转义同名）、尾随值/垃圾、错误类型、孤立代理字符或替换字符。必填字段不可缺失/null；所有字符串不得空白、带控制字符或超过 512 UTF-8 字节。编号不 trim，不转换数字。
- 可选 null/缺省均为未设置；数组不得为空、含 null/空白/非字符串元素。每个数组最多 1000 项；按原顺序保留，集合语义在业务筛选层处理。
- 请求最多 65536 字节，包含空白；按上限加一读取，超限返回 invalid_argument。流读取失败也不能产生部分参数。
- page 默认 1、上限 1000000；page_size 默认 100、上限 1000；正 JSON 整数，拒绝小数、科学计数及字符串。null 与缺省取同一默认值。
- 学年必须为四位相邻年 YYYY-YYYY，首年不少于 1000；仅成绩接口允许 all。学期必须为字符串 1 或 2。课表学年/学期必填。
- score_min/max 接受有限 JSON 数值，包含边界；下限不得高于上限，不擅自增加 0—100 的业务范围限制。null 表示无该方向限制。
