# 运维后端验收报告（模板，未验收）

状态：未执行。2026-10-13。P27—P35 有效后端开发及回归源码已完成；T/OI 暂停，不填写成功结论。

| 必填证据 | 当前值 |
| --- | --- |
| 被测 SHA / 镜像 digest | 未生成 |
| Linux runner / CPU / 内存 / 磁盘 | 未执行 |
| Mock 版本 / seed / 冻结容量基线 | 基线已冻结：`verification/operations_capacity_baseline.json`；本次执行 seed/Mock SHA 未生成 |
| T27—T35 通过工件 | 无 |
| OI01—OI07 发现 / 执行 / 通过 / 失败 / 跳过 | 未执行，不计成功 |
| 权限 / 双库故障 / 升级排空 / 重启 / SSE / 清理证据 | 无 |
| SMTP Mock 逐人应答 / unknown / 重试边界 | 无 |
| 命令 / 退出码 / 耗时 / 超时 / 工件摘要 | 未执行 |
| 升级 / 代码回退 / 数据恢复 / epoch 轮换记录 | 未执行 |

运行器只校验执行证据完整性，不能代替场景断言和人工审查。存在失败、跳过、空匹配、缺工件或未冻结容量均不通过。前端页面重构另行验收。

接入包：[HTTP/SSE 样例](OPERATIONS_API_EXAMPLES.md)；操作手册：[升级与回退](OPERATIONS_UPGRADE_RUNBOOK.md)；源码映射：[OI_CASE_MAP](verification/OI_CASE_MAP.md)。

恢复执行后，逐次填写：被测 SHA、同 SHA 的 T 工件及摘要、32 项期望/发现/执行/通过/失败/跳过数、每项 seed/起止时间、真实容量数据与基线比较、升级/回退恢复点及 epoch、未解决故障、报告审阅结论。不得用本模板或编译输出代填通过结果。
