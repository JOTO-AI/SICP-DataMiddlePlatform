# 目录独立预期

`reviewed_catalog.json` 的 responses/objects 逐项摘录自 `sicp-api-design.md` 响应字段表，sources 摘录自 `数据表结构与主键规划.md` 第 3 节的已确认主键及物理字段类型（14 表、274 字段）。未从 Catalog、Go DTO 或真实源记录生成；只保留字段名称和类型，不含人员数据。修改目录时须先核对事实文档，不能直接用实现输出刷新此预期。

业务响应字段必须出现，`/null` 表示可空；数组不可为 null。源字段描述本地已确认读取契约：主键必须出现且非空，其他字段允许缺失/null，JSON 类型保留原始 token，timestamp 无时区。
