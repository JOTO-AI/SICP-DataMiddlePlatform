# P16 五接口字段与规则矩阵

2026-10-12，P16.01。本文件冻结接口 1、2、3、8、9 的传输预期；DTO 不表示路由已开放。生产 `Definitions()` 在 P24 兼容检查前仍仅包含接口 4—7。解析、业务查询、缓存与快照分别由后续小步实现。

依据：[接口设计](../sicp-api-design.md)、[第二步计划](PHASE2_DETAILED_DEVELOPMENT_PLAN.md)、[公共传输契约](P06_QUERY_CONTRACT.md)、[单位源文档](../latest_api_docs/院系所单位基本数据子类表.md)、[学籍源文档](../latest_api_docs/本专科生学籍基本数据子类表.md)。以下补充边界是本项目实现决定，不表示源端已确认。

## 公共请求与响应

所有接口为 POST JSON。请求的可选字段缺省/null 等价，非空字符串保留原值、前导零；不 trim。数组为集合语义，数组内并集、条件间交集，禁止空数组和非法元素。名称匹配为区分大小写的字面子串。具体严格解析复用 P06 并在 P16.02 扩展。

| 字段 | 类型与默认值 | 边界 |
| --- | --- | --- |
| page | 可选整数，默认 1 | 1—1000000，拒绝小数、指数、字符串 |
| page_size | 可选整数，1—3 默认 1000；8—9 默认 100 | 1—1000 |
| total | 必出整数 | 过滤/去重后、分页前的完整数量，0 不省略 |
| synced_at | 必出字符串 | 固定依赖表已确认时间最小值；空结果同口径 |
| items | 必出数组 | 无结果为 []，不能为 null |
| source_updated_at | 每个结果必出 string/null | 实际参与源记录 ETL_TIME 最大值 |

成功 data 放入既有四字段外壳。可空响应字段必须显式 null，不使用 omitempty。失败整次 data=null，不缓存；接口 8/9 额外回显必填查询身份。五接口分页均使用 operation 绑定快照；8 的 token 不用于 9，调用方逐页核对 X-SICP-Combination 和人数。

## 请求字段

下表之外仅接受公共 page/page_size。未标“必填”的均可选。

| 接口 | JSON 字段 | 类型 | 筛选来源及规则 |
| --- | --- | --- | --- |
| 1 | organization_code | string | 单位 DWH 精确 |
| 1 | organization_name | string | 单位 DWMC 字面包含 |
| 1 | parent_organization_code | string | 单位 LSDWH，仅直属子级；根的有效父编号为 null |
| 1 | category_codes | string[] | 单位 DWLBM |
| 1 | is_active | boolean | DWYXBSM="1" 且 SFSC="0"；false 是有效条件；未传不限 |
| 2 | student_number、name | string | 学生基本 XH 精确、XM 字面包含 |
| 2 | department_code、department_name | string | 学籍 YXSH 精确、单位 DWMC 字面包含 |
| 2 | campus_code、major_code、grade、class_code | string | 学籍 SSXQH、ZYH、SZNJ、SZBH 精确 |
| 2 | status_codes、enrollment_status_codes | string[] | 学籍 XSDQZTM、XJZTM，独立筛选 |
| 2 | on_campus_flag | string | 学籍 SFZX 原码精确，不由状态推算 |
| 2 | student_category_codes | string[] | 学籍 XSLBM |
| 2 | study_years | integer | 学籍 XZ 转换后精确；请求范围 1—2147483647 |
| 2 | admission_year、expected_graduation_year | string | 四位年份 0001—9999，分别取有效 RXNY、YJBYRQ 年份 |
| 2 | counselor_staff_number | string | SZBH→班级 BH→FDYJGH 精确 |
| 3 | staff_number、name | string | 教职工基础 JGH 精确、XM 字面包含 |
| 3 | department_code、department_name | string | 基础 DWH 精确、单位 DWMC 字面包含 |
| 3 | status_codes | string[] | 基础 DQZTM；未传包含全部状态 |
| 8 | staff_number | 必填 string | JSJGH 拆分后完整工号匹配 |
| 8 | academic_year、semester | 必填 string | KKXND、KKXQM；相邻四位年份（首年≥1000），学期 1/2，不接受 all |
| 8 | course_code | string | 排课 JHKCH 精确 |
| 9 | teaching_class_number、planned_course_code | 必填 string | JXBH + JHKCH 精确 |
| 9 | academic_year、semester | 必填 string | 同接口 8，先确认组合存在 |

## 响应对象逐字段

| 对象 | 字段 | 类型/null | 源字段及规则 |
| --- | --- | --- | --- |
| Organization | organization_code、organization_name | string，非空 | 单位 DWH、DWMC |
| Organization | parent_organization_code | string/null | LSDWH；10001 固定 null |
| Organization | category_code、category_name | string/null | DWLBM 原码及已知映射 |
| Organization | is_active | boolean | 仅 DWYXBSM=1 且 SFSC=0 为 true；空/未知为 false |
| Organization | source_updated_at | string/null | 当前单位 ETL_TIME |
| Department | organization_code | string | 学籍 YXSH 或教职工 DWH；无号时整个对象 null |
| Department | organization_name、parent_organization_code、category_code、category_name | string/null | 单位 DWMC、LSDWH、DWLBM 及映射；缺档时全部 null，根父 null |
| Department | is_active | boolean/null | 单位存在按同上状态规则；单位缺档为 null |
| Student | student_number、name | string，非空 | 基本 XH、XM |
| Student | status_code、enrollment_status_code、on_campus_flag | string/null | 学籍 XSDQZTM、XJZTM、SFZX |
| Student | department | Department/null | 学籍 YXSH→单位 DWH |
| Student | campus_code、campus_name | string/null | 学籍 SSXQH→校区 XQH、XQMC |
| Student | major_code、major_name | string/null | 学籍 ZYH→专业 ZYH、ZYMC |
| Student | grade | string/null | 学籍 SZNJ |
| Student | class_code、class_name | string/null | 学籍 SZBH→班级 BH、BJMC |
| Student | student_category_code、student_category_name | string/null | 学籍 XSLBM 及已知映射 |
| Student | study_years | integer/null | 学籍 XZ；精确整数，不截小数，不使用专业学制替代 |
| Student | admission_month、expected_graduation_date | string/null | 学籍 RXNY、YJBYRQ，见转换规则 |
| Student | counselor | Counselor/null | SZBH→班级 BH→FDYJGH→教职工 JGH；缺班/无工号为 null |
| Counselor | staff_number、name | string、string/null | FDYJGH、教职工 XM；缺教师档案仍保留工号 |
| Student | email、mobile | string/null | 学生通讯 DZXX、YDDH 最新整行原值 |
| Student | source_updated_at | string/null | 本人及实际关联记录最大 ETL_TIME，含辅导员 |
| Staff | staff_number、name | string，非空 | 基础 JGH、XM |
| Staff | status_code | string/null | DQZTM |
| Staff | department | Department/null | DWH→单位 DWH |
| Staff | email、mobile | string/null | 教职工通讯 DZXX、YDDH 最新整行原值 |
| Staff | source_updated_at | string/null | 基础、当前通讯及单位最大 ETL_TIME |
| TeachingClass | teaching_class_number、planned_course_code | string | 排课 JXBH、JHKCH |
| TeachingClass | teaching_class_name | string/null | JXBMC；空与唯一非空合并，不同非空冲突报错 |
| TeachingClass | course_code、course_name | string、string/null | JHKCH→课程 KCH、KCMC；课程缺档保留 JHKCH |
| TeachingClass | teachers | TeachingClassTeacher[] | 该学期该组合全部排课教师并集，按工号排序 |
| TeachingClassTeacher | staff_number、name | string、string/null | 拆分 JSJGH→教职工 JGH、XM，缺档保留号，不按 JSXM 下标配对 |
| TeachingClass | student_count | integer | 关系 JXBH+JHKCH→XH 去重；不取 XDRS |
| TeachingClass | source_updated_at | string/null | 全部参与排课、课程、教师档案、计数关系最大 ETL_TIME |
| TeachingClassStudent | student_number | string | 实际关系 XH，非空 |
| TeachingClassStudent | name | string/null | 基本 XM，缺档允许 |
| TeachingClassStudent | department_code、department_name | string/null | 学籍 YXSH、单位 DWMC，当前归属 |
| TeachingClassStudent | class_code、class_name | string/null | 学籍 SZBH、班级 BJMC，当前行政班 |
| TeachingClassStudent | source_updated_at | string/null | 组合判定排课及该成员全部关系、基本、学籍、单位、班级最大 ETL_TIME |

接口 8 外层额外必出 staff_number、academic_year、semester（string）；接口 9 额外必出 teaching_class_number、planned_course_code、academic_year、semester（string），均来自有效请求。接口 8 total 计组合，接口 9 total 计唯一学号。

## 固定依赖、完整性与转换决定

| 接口 | synced_at 固定依赖 | 稳定排序 |
| --- | --- | --- |
| 1 | 院系所单位 | DWH C 升序 |
| 2 | 学生基本、学籍、学生通讯、院系所单位、校区、专业、行政班、教职工基础 | XH C 升序 |
| 3 | 教职工基础、教职工通讯、院系所单位 | JGH C 升序 |
| 8 | 排课、课程、教职工基础、学生课表关系 | JXBH、JHKCH C 升序 |
| 9 | 排课、学生课表关系、学生基本、学籍、院系所单位、行政班 | XH C 升序 |

- 组织图：接口 1 每次在同一组合读取完整单位目录，先检查唯一编号、必需名称、根 10001 存在、所有非根父存在且无环，再筛选分页；即使筛选可排除异常节点也不能掩盖图错误。空目录缺根报错。不合成学校，不重写编号；10001 的源父值不参与图边。人员 Department 仅检查关联单位，不因无关组织图缺损扩大失败范围。
- 字典：源文档仅列 DWLBM/XSLBM 字段，没有完整映射。当前依据接口设计第 1 节明确 DWLBM `1=教学院系`、`5=行政机构`；第 2 节明确 XSLBM `41=专科生`、`432=中高贯通生`。没有明确 code 的“中职学生”不猜码。未知非空码原样保留、name=null；缺码两字段 null。
- RXNY 只接受 ASCII `YYYYMM` 或 `YYYY-MM`；YJBYRQ 只接受 `YYYYMMDD` 或 `YYYY-MM-DD`。年份 0001—9999，实际日历校验（包括闰日），输出 YYYY-MM / YYYY-MM-DD。缺失/null/全空白为 null；其他非空非法值（包括带首尾空白、时间后缀、错误分隔符）报数据错误。源文档未承诺具体输入格式，这些是本接口接受范围，不是源端确认结果。
- XZ：源 JSON number 或数值字符串按 JSON 数字语法精确解析（允许数学上为整数的 3.0、3e0），整数范围 -2147483648—2147483647；不先经 float64，不截断。缺失/null/空白字符串为 null；小数、非数、越界报错。输出不擅自新增“必须为正”的源数据含义；请求 study_years 则必须是 1—2147483647 的正 JSON 整数，拒绝指数与小数表示。边界在 P17 转换测试验证。
- 学生基本/学籍严格双向匹配：精确 student_number 请求仅核对该身份（双方均无可返回空）；其他请求先核对全量，再应用其他筛选。缺任一边、重复主身份或姓名空使请求失败。接口 9 独立使用宽松关联模式，缺基本/学籍仍保留关系学号。
- 学籍及通讯使用已有 ETL_TIME 最新整行入库规则；同时间冲突、不可比较时间报错，不取第一条、不回填旧非空值。通讯原值不因格式异常或跨人员重复而清空；孤立通讯不创建人员。
- 教学班只使用身份、学期、教师字段聚合，不依赖节次/日期展示字段。JXBH/JHKCH/学期缺失、null 或全空白的排课跳过；非空非法身份/学期报错。组合多课次聚合全部教师，不仅命中请求教师的课次。关系跨学期复用且无法区分归属时报 conflict，即使成员为空也不得返回“确认空名单”。
- 错误：组合不存在 invalid_argument/400；跨学期关系歧义 conflict/409；源值非法、必要匹配缺失、重复/冲突记录 internal/500；读取/取消/预算沿用现有分类。内部日志仅记录脱敏分类，不把原值、SQL 或联系方式回显。

## 独立测试预期

`internal/query/contract/testdata/phase2.json` 手工固定十组完整请求/响应预期，覆盖五接口、所有字段、嵌套对象、前导零、null、false、0；该文件不是业务服务生成结果。T16-01 精确比较字段和值；T16-02 覆盖零值输出 []、null、false、0 和布尔筛选三态；T16-03 锁定唯一 operation 值及仅四个已实现生产路由。测试未运行时不得标记 T 通过。
