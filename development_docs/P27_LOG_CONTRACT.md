# P27 管理日志公共契约

日期：2026-10-12。范围：`internal/logquery` 和独立 `admin.LogsRead` 权限。本步定义后续接入边界，尚未注册日志 HTTP 路由、建立双库或提供 SSE 服务。

## 筛选和标识

源仅为 `platform`、`upstream`。参数必须单值；未知、重复和空参数均拒绝。

| 字段 | 规则 |
| --- | --- |
| mode | 默认 history；允许 history/live |
| from/to | 带时区 RFC3339，2000—9999 年，最多六位小数，规范化 UTC；超精度的零尾数同样拒绝 |
| history 时间 | `[from,to)`；to 默认首次服务端时间，from 默认 to 前 24h；跨度最多 7 天；stream_cursor=null |
| live 时间 | to 必须省略且输出 null；首次 from 距当前为 0—7 天，默认前 24h；之后 from 固定，不设发生时间上界 |
| page_size | 默认 50，最大 200，只接受规范十进制正整数 |
| kind | request/task/step/call/attempt/audit；平台源仍允许读取历史 call/attempt |
| phase/outcome | started/finished/transport；succeeded/failed/interrupted |
| operation | 已有九接口的稳定 operation 枚举 |
| task_id/request_id/test_run_id | 服务生成的不透明标识，不能是人员标识或自由文本 |
| id_format | 默认 raw；raw 为最多 128 字节的 ASCII 字母、数字、`-`、`_`，由配置的 Sanitizer.Correlation 摘要一次；correlation 为 64 位小写十六进制，原样保留 |
| cursor | 最长 4096 字节，先鉴权和解签，再用其中 Filter 作为续页 base |

id_format 不存入游标；响应和游标只含标识摘要。将响应标识用于筛选时需显式指定 correlation。续页省略的参数继承原值，再次提供的参数经规范化后须一致；不允许更改模式、时间、页大小或筛选。Filter 校验不代替游标有效期检查。

## 签名和续传

HMAC-SHA256 使用独立 32 字节非全零密钥，与业务 key 和日志关联摘要密钥分开配置。NewKeyring 接收 active key_id 和至多 8 个 key，复制后只读。key_id 是 1—32 位服务标识且参与签名。多实例必须共享配置；轮换后保留旧 key 至旧游标过期，紧急撤销则要求重查。配置装配归 P29.04。

游标绑定版本、source、epoch、direction、规范化 Filter、权限范围摘要、首次已提交高水位 H、位置、签发时 floor 和固定期限。默认有效 24h；续页用 Encode 保留原 issued_at/expires_at，不能重新 Mint 延期。序号和水位使用十进制字符串。

Decode 先验签再解释载荷；篡改/源或方向不符为 400，权限范围不同为 403，合法过期为 409。范围由服务端稳定主体及有效权限构造；游标不是凭据，每个请求仍需管理鉴权。

ValidateContinuation 必须在与记录读取相同的短数据库快照内调用。epoch 改变返回 409，即使恢复后的 floor 更小；同 epoch 的 floor 倒退属于非法状态。历史 floor 高于签发值即失效；实时仅 position 小于 floor 时失效，相等可继续。

StreamStart 从有效 live 历史游标生成订阅边界，始终将位置设为首次 H，保留 Filter、epoch 和期限；翻旧页不会改变实时起点。实际扫描、checkpoint、Last-Event-ID 和慢客户端处理归 P30。

## DTO 和脱敏边界

HTTP 外壳沿用 code/message/data，失败 data=null；成功空页 items=[]。Page 包含规范化 filter、source、epoch、high_water、retained_through、next_cursor、stream_cursor、expires_at。没有下一页/实时订阅时 cursor 为 null。

EventItem 包含源/代次/事件身份、序号、kind/phase、发生/接收时间、outcome、operation、各层关联 ID、attempt_no、HTTP/业务码、duration_ns、test、audit。缺失事实为 null，已知 0 保留。序号及 duration_ns 输出十进制字符串。Detail 增加 entity_id、现有类型化 Metadata、已脱敏 Response 和 Redaction（redacted/truncated/original_bytes/field_count）；original_bytes 也为字符串。

DTO 不是脱敏器。P29 必须先通过安全事件恢复/校验再投影字段，不能直接复制任意数据库 JSON 或未经校验的 Metadata/Response。单事件 256 KiB、整响应 2 MiB 已声明，实际恢复与发送限制归 P29。

P27 只声明新增事件契约，P28 才同步 events、Sanitizer、迁移及投递：

- audit 归平台库。AuditIdentity 只含可信 actor_id 摘要、服务端 action、可选 object_id 摘要；action 白名单为 source_test、notification_settings、notification_test、retention_settings、cleanup。不记录凭据、收件地址、原始请求/响应或自由文本。
- call/attempt 使用 TestIdentity：purpose=test 必须有摘要 test_run_id；purpose=sync 不得携带该 ID；旧事件无对象时输出 null，不伪造同步 task_id。
- 标识在入缓冲前摘要一次，恢复及投影不重复摘要；声明 DTO 不表示旧 writer 已支持新事件。

## 错误和请求预算

| HTTP | code | 场景 |
| --- | --- | --- |
| 400 | invalid_argument | 参数/签名无效或更改条件 |
| 401 | 既有管理认证错误码 | 未认证，由 admin.Auth 处理 |
| 403 | forbidden | 无权限或游标范围不匹配 |
| 404 | not_found | 事件不存在 |
| 409 | cursor_expired | 期限、epoch、保留水位失效 |
| 413 | request_too_large | 新管理 JSON 超过 64 KiB |
| 429 | resource_exhausted | 预算耗尽 |
| 502 | proxy_unavailable | 内部代理失败 |
| 503 | unavailable | 依赖故障或未分类内部错误 |
| 504 | deadline_exceeded | 执行超时 |

HTTPError 不回显内部错误文本。DecodeRequest 最多读 64 KiB+1 字节，解析前拒绝超限，拒绝非对象、未知字段和尾随 JSON；调用方负责鉴权及读取超时。

测试源码与编号映射见 [T27](verification/T27.md)，review 及静态检查见 [P27](verification/P27.md)。测试按用户要求暂停执行，编译不算单测通过。
