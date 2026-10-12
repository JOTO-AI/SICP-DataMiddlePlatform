# 上海工商新数据中台接口设计

本文档定义上海工商新数据中台为组织同步、人员信息查询、成绩查询、课表查询及教学班成员同步提供的业务接口输入与输出。

## 通用约定

- 请求方式统一为 `POST`，请求体和响应体均为 JSON；路径为建议值。
- 返回字段均应出现，类型标注 `/null` 的字段允许返回 null。
- 本文的响应字段和示例定义业务数据。当前已实现的接口 4—7，其鉴权、完整响应外壳、错误码、参数边界与快照分页见 [公共查询传输契约](development_docs/P06_QUERY_CONTRACT.md)；实际响应将本文业务对象放入 `data`，失败时 `data=null`。其余五接口尚未注册，传输协议在实现前确认；生产域名以部署配置为准。
- 示例仅展示业务数据，均为虚构值。
- 2026-10-10 对齐已确认业务规则：文字成绩返回 null、同号多课程报错、课程信息取当前版本、未排课课程不进入课表；源字段空值兼容按文末 2026-10-09 补充说明执行。

### 接口一览

| 接口 | 路径 | 用途 |
| --- | --- | --- |
| [1. 组织查询](#1-组织查询) | `/api/v1/sicp/organizations/query` | 获取学校与各级单位，建立组织树 |
| [2. 学生信息](#2-学生信息) | `/api/v1/sicp/students/query` | 获取学生、学籍、归属和联系方式 |
| [3. 教职工信息](#3-教职工信息) | `/api/v1/sicp/staff/query` | 获取教职工、单位和联系方式 |
| [4. 成绩查询（学生）](#4-成绩查询学生) | `/api/v1/sicp/students/grades/query` | 查询指定学生的成绩 |
| [5. 成绩查询（批量）](#5-成绩查询批量) | `/api/v1/sicp/students/grades/batch-query` | 按学院、行政班或学号列表批量查询成绩 |
| [6. 课表查询（学生）](#6-课表查询学生) | `/api/v1/sicp/students/timetable/query` | 查询指定学生的课表 |
| [7. 课表查询（教师）](#7-课表查询教师) | `/api/v1/sicp/staff/timetable/query` | 查询指定教师的授课安排 |
| [8. 教师教学班查询](#8-教师教学班查询) | `/api/v1/sicp/staff/teaching-classes/query` | 查询教师所授教学班及课程 |
| [9. 教学班学生查询](#9-教学班学生查询) | `/api/v1/sicp/teaching-classes/students/query` | 获取教学班课程的学生名单 |

## 组织与人员基本信息

接口 1 至 3 提供组织与人员基本信息，用于组织同步和人员信息查询。组织接口独立返回完整组织关系，人员接口同时返回所属单位对象。

### 通用规则

#### 查询与分页

- 不传筛选条件时返回全量，包含停用人员和组织。
- 多个条件取交集，数组内的值取并集。
- 可选条件不传或传 null 表示不限；空字符串、空数组应报参数错误。
- 筛选在分页前执行，`total` 为筛选后的总数，按唯一编号稳定排序，分页应保证不漏不重。
- 数据读取失败或结果不完整时应报错，不得返回成功的空列表或部分结果。调用方仅在全部分页成功且完整后应用全量同步。
- 局部查询结果不能用于停用未返回的人员或组织。

#### 人员数据

- 学生按学号关联，教职工按工号关联；姓名仅用于搜索，不作为唯一身份标识。
- 单位号、学号和工号沿用原中台编号，保留前导零，不重新编码；主编号及人员姓名不得为空。
- 学生基本表与学籍必须按学号完整匹配，缺失匹配时应报错，不得静默丢弃学生或伪造空状态。
- 学籍、联系方式等多条记录取最新一条：优先按源业务更新时间，无该字段时按 `ETL_TIME`。同时间内容冲突时由中台明确取值规则。
- 单位、校区、专业、班级或辅导员关联缺失时保留人员及已有编号，关联名称返回 null；不得因所属单位无效而过滤人员。
- 联系方式确认没有值时返回 null，读取失败应报错；返回最新记录的原始邮箱、手机号，不按格式或跨人员重复情况清空，格式校验与唯一性判断由调用方处理。
- 不返回身份证号等非本业务所需字段。

### 1. 组织查询

获取学校及各级单位，建立组织树。学校根单位为 `10001`。

```
POST /api/v1/sicp/organizations/query
```

#### 请求参数

| 名称 | 类型 | 必填 | 说明 |
| --- | --- | --- | --- |
| `page` | Integer | 否 | 页码，默认 1 |
| `page_size` | Integer | 否 | 每页条数，默认 1000 |
| `organization_code` | String | 否 | 按单位号精确查询 |
| `organization_name` | String | 否 | 按单位名称包含匹配 |
| `parent_organization_code` | String | 否 | 查询指定单位的直属下级，不包含更深层级 |
| `category_codes` | String[] | 否 | 按单位类别码筛选，如 `["1","5"]`（教学院系、行政机构） |
| `is_active` | Boolean | 否 | true 查询有效组织，false 查询停用组织，不传则全部 |

#### 响应参数

| 名称 | 类型 | 来源字段 | 说明 |
| --- | --- | --- | --- |
| `total` | Integer | 中台统计 | 筛选后的组织总数 |
| `synced_at` | String | 新中台同步记录 | 新中台最近一次完整同步本接口所需数据的成功时间 |
| `items` | Organization[] | 中台查询结果 | 组织列表 |

#### Organization 对象

| 名称 | 类型 | 来源字段 | 说明 |
| --- | --- | --- | --- |
| `organization_code` | String | 院系所单位表.DWH | 单位号 |
| `organization_name` | String | 院系所单位表.DWMC | 单位名称 |
| `parent_organization_code` | String/null | 院系所单位表.LSDWH | 上级单位号；根单位返回 null |
| `category_code` | String/null | 院系所单位表.DWLBM | 单位类别码 |
| `category_name` | String/null | 院系所单位表.DWLBM | 单位类别名称，按 `DWLBM` 代码集转换 |
| `is_active` | Boolean | 院系所单位表.DWYXBSM、SFSC | `DWYXBSM=1 且 SFSC=0` 为 true，其余为 false |
| `source_updated_at` | String/null | 院系所单位表.ETL_TIME | 原始记录的数据同步时间 |

#### 响应示例

```json
{
  "total": 2,
  "synced_at": "2026-09-18T09:00:00+08:00",
  "items": [
    {
      "organization_code": "10001",
      "organization_name": "示例学校",
      "parent_organization_code": null,
      "is_active": true,
      "source_updated_at": "2026-09-18T08:00:00+08:00",
      "category_code": null,
      "category_name": null
    },
    {
      "organization_code": "DEPT001",
      "organization_name": "示例学院",
      "parent_organization_code": "10001",
      "is_active": true,
      "source_updated_at": "2026-09-18T08:00:00+08:00",
      "category_code": "1",
      "category_name": "教学院系"
    }
  ]
}
```

#### 注意事项

1. 以 `10001` 作为根组织节点，其 `parent_organization_code` 返回 null。
2. **部门是否有效判定**：`is_active` 根据院系所单位表的 `DWYXBSM`（单位有效标识码）和 `SFSC`（是否删除）共同判定：
   - `DWYXBSM = "1"` 且 `SFSC = "0"` 时，返回 true，表示组织有效且未删除。
   - 任一条件不满足时，返回 false，包括字段为空或出现未知码值。
   - 不传 `is_active` 筛选条件时，有效与无效组织均返回。

### 2. 学生信息

查询学生基本信息、学籍、所属单位、班级、辅导员及最新联系方式。

```
POST /api/v1/sicp/students/query
```

#### 请求参数

| 名称 | 类型 | 必填 | 说明 |
| --- | --- | --- | --- |
| `page` | Integer | 否 | 页码，默认 1 |
| `page_size` | Integer | 否 | 每页条数，默认 1000 |
| `student_number` | String | 否 | 按学号精确查询 |
| `name` | String | 否 | 按姓名包含匹配 |
| `department_code` | String | 否 | 按所属院系号精确筛选，不自动包含下级单位 |
| `department_name` | String | 否 | 按所属院系名称包含匹配 |
| `campus_code` | String | 否 | 按校区号精确筛选 |
| `major_code` | String | 否 | 按专业号精确筛选 |
| `grade` | String | 否 | 按所在年级精确筛选，如 `2026` |
| `class_code` | String | 否 | 按行政班号精确筛选 |
| `status_codes` | String[] | 否 | 学生当前状态码，如 `["01","05","08"]`，不传则全部 |
| `enrollment_status_codes` | String[] | 否 | 按学籍状态 `XJZTM` 筛选 |
| `on_campus_flag` | String | 否 | 按是否在校 `SFZX` 精确筛选 |
| `student_category_codes` | String[] | 否 | 按学生类别筛选，如 `["41","432"]`（专科生、中高贯通生） |
| `study_years` | Integer | 否 | 按学制年数精确筛选，如 3、5 |
| `admission_year` | String | 否 | 按入学年份筛选，如 `2026`，取入学年月的年份 |
| `expected_graduation_year` | String | 否 | 按预计毕业年份筛选，如 `2029` |
| `counselor_staff_number` | String | 否 | 按行政班辅导员工号精确筛选 |

#### 响应参数

| 名称 | 类型 | 来源字段 | 说明 |
| --- | --- | --- | --- |
| `total` | Integer | 中台统计 | 筛选并去重后的学生总数 |
| `synced_at` | String | 新中台同步记录 | 新中台最近一次完整同步本接口所需数据的成功时间 |
| `items` | Student[] | 中台查询结果 | 学生列表，学号唯一 |

#### Student 对象

| 名称 | 类型 | 来源字段 | 说明 |
| --- | --- | --- | --- |
| `student_number` | String | 学生基本表.XH | 学号，保留前导零 |
| `name` | String | 学生基本表.XM | 姓名 |
| `status_code` | String/null | 学籍表.XSDQZTM | 学生当前状态，如 `01` 在读、`05` 复学、`08` 结业 |
| `enrollment_status_code` | String/null | 学籍表.XJZTM | 学籍状态 |
| `on_campus_flag` | String/null | 学籍表.SFZX | 原始是否在校标识，保留源代码，不由学生状态推算 |
| `department` | Department/null | 学籍表.YXSH | 所属院系，无单位号时为 null，关联不到单位时保留编号 |
| `campus_code` | String/null | 学籍表.SSXQH | 校区号 |
| `campus_name` | String/null | 校区表.XQMC | 按校区号关联的校区名称 |
| `major_code` | String/null | 学籍表.ZYH | 专业号 |
| `major_name` | String/null | 专业信息表.ZYMC | 按专业号关联的专业名称 |
| `grade` | String/null | 学籍表.SZNJ | 所在年级 |
| `class_code` | String/null | 学籍表.SZBH | 行政班号 |
| `class_name` | String/null | 班级表.BJMC | 按行政班号关联的班级名称 |
| `student_category_code` | String/null | 学籍表.XSLBM | 学生类别码 |
| `student_category_name` | String/null | 学籍表.XSLBM | 按 `XSLBM` 代码集转换，如专科生、中高贯通生、中职学生 |
| `study_years` | Integer/null | 学籍表.XZ | 学制年数 |
| `admission_month` | String/null | 学籍表.RXNY | 入学年月，统一为 `YYYY-MM` |
| `expected_graduation_date` | String/null | 学籍表.YJBYRQ | 预计毕业日期，统一为 `YYYY-MM-DD` |
| `counselor` | Counselor/null | 学籍表.SZBH → 班级表.BH、FDYJGH → 教职工基础表.JGH | 行政班辅导员，班级缺失或未配置时为 null |
| `email` | String/null | 学生通讯表.DZXX | 最新联系方式中的邮箱 |
| `mobile` | String/null | 学生通讯表.YDDH | 最新联系方式中的手机号 |
| `source_updated_at` | String/null | 各关联源表.ETL_TIME | 参与返回结果的源记录中，最新的同步时间 |

#### Department 对象

| 名称 | 类型 | 来源字段 | 说明 |
| --- | --- | --- | --- |
| `organization_code` | String | 学籍表.YXSH / 教职工基础表.DWH | 所属单位号 |
| `organization_name` | String/null | 院系所单位表.DWMC | 单位名称 |
| `parent_organization_code` | String/null | 院系所单位表.LSDWH | 上级单位号，根单位为 null |
| `category_code` | String/null | 院系所单位表.DWLBM | 单位类别码 |
| `category_name` | String/null | 院系所单位表.DWLBM | 按 `DWLBM` 代码集转换 |
| `is_active` | Boolean/null | 院系所单位表.DWYXBSM、SFSC | `DWYXBSM=1 且 SFSC=0` 为 true，关联不到单位时为 null |

#### Counselor 对象

| 名称 | 类型 | 来源字段 | 说明 |
| --- | --- | --- | --- |
| `staff_number` | String | 班级表.FDYJGH | 辅导员工号 |
| `name` | String/null | 教职工基础表.XM | 按工号关联的教职工姓名，未匹配时为 null，保留工号 |

#### 响应示例

```json
{
  "total": 1,
  "synced_at": "2026-09-18T09:00:00+08:00",
  "items": [
    {
      "student_number": "S000001",
      "name": "示例学生",
      "status_code": "01",
      "enrollment_status_code": "0",
      "on_campus_flag": "1",
      "campus_code": "CAMPUS01",
      "campus_name": "示例校区",
      "major_code": "MAJOR001",
      "major_name": "示例专业",
      "grade": "2026",
      "class_code": "CLASS001",
      "class_name": "示例班级",
      "email": "student@example.invalid",
      "mobile": null,
      "source_updated_at": "2026-09-18T08:00:00+08:00",
      "department": {
        "organization_code": "DEPT001",
        "organization_name": "示例学院",
        "parent_organization_code": "10001",
        "category_code": "1",
        "category_name": "教学院系",
        "is_active": true
      },
      "student_category_code": "41",
      "student_category_name": "专科生",
      "study_years": 3,
      "admission_month": "2026-09",
      "expected_graduation_date": "2029-06-30",
      "counselor": {
        "staff_number": "T000001",
        "name": "示例辅导员"
      }
    }
  ]
}
```

#### 注意事项

1. **学生是否有效判定**：根据学籍表的 `XSDQZTM`（接口字段 `status_code`）判定：
   - `01`（在读）、`05`（复学）、`08`（结业）视为有效。
   - 其他值、空值及未知码值均按无效处理。
   - 该规则用于人员同步，不传状态筛选条件时仍返回全部状态。
2. **是否在校说明**：`on_campus_flag` 保留学籍表 `SFZX` 的原始值。学生有效口径包含结业学生，不等同于是否在校；`on_campus_flag` 和 `enrollment_status_code` 不参与有效性判定。

### 3. 教职工信息

查询教职工基本信息、所属单位及最新联系方式。

```
POST /api/v1/sicp/staff/query
```

#### 请求参数

| 名称 | 类型 | 必填 | 说明 |
| --- | --- | --- | --- |
| `page` | Integer | 否 | 页码，默认 1 |
| `page_size` | Integer | 否 | 每页条数，默认 1000 |
| `staff_number` | String | 否 | 按工号精确查询 |
| `name` | String | 否 | 按姓名包含匹配 |
| `department_code` | String | 否 | 按所属单位号精确筛选，不自动包含下级单位 |
| `department_name` | String | 否 | 按所属单位名称包含匹配 |
| `status_codes` | String[] | 否 | 教职工当前状态码，如 `["11","23","04"]`，不传则全部 |

#### 响应参数

| 名称 | 类型 | 来源字段 | 说明 |
| --- | --- | --- | --- |
| `total` | Integer | 中台统计 | 筛选并去重后的教职工总数 |
| `synced_at` | String | 新中台同步记录 | 新中台最近一次完整同步本接口所需数据的成功时间 |
| `items` | Staff[] | 中台查询结果 | 教职工列表，工号唯一 |

#### Staff 对象

| 名称 | 类型 | 来源字段 | 说明 |
| --- | --- | --- | --- |
| `staff_number` | String | 教职工基础表.JGH | 工号，保留前导零 |
| `name` | String | 教职工基础表.XM | 姓名 |
| `status_code` | String/null | 教职工基础表.DQZTM | 当前状态，如 `11` 在职、`23` 实习生、`04` 返聘、`07` 离职、`102` 情况不明、`20` 废止 |
| `department` | Department/null | 教职工基础表.DWH | 所属单位，按教职工 `DWH` 关联单位 |
| `email` | String/null | 教职工通讯表.DZXX | 最新联系方式中的邮箱 |
| `mobile` | String/null | 教职工通讯表.YDDH | 最新联系方式中的手机号 |
| `source_updated_at` | String/null | 各关联源表.ETL_TIME | 参与返回结果的源记录中，最新的同步时间 |

#### Department 对象

字段与[学生信息的 Department 对象](#department-对象)一致。无单位号时返回 null，关联不到组织时保留单位号，其余字段为 null。

> **Note**
> - 人员同步的教职工有效口径为 `status_code` 属于 `11/23/04`，其他值、空值及未知值均按无效处理。
> - `07` 表示离职。“无效”还包括情况不明、废止等状态，不能统一解释为离职。
> - 有效性规则供调用方处理人员同步，接口不传状态筛选时仍返回全部状态。

#### 响应示例

```json
{
  "total": 1,
  "synced_at": "2026-09-18T09:00:00+08:00",
  "items": [
    {
      "staff_number": "T000001",
      "name": "示例教师",
      "status_code": "11",
      "email": "teacher@example.invalid",
      "mobile": null,
      "source_updated_at": "2026-09-18T08:00:00+08:00",
      "department": {
        "organization_code": "DEPT001",
        "organization_name": "示例学院",
        "parent_organization_code": "10001",
        "category_code": "1",
        "category_name": "教学院系",
        "is_active": true
      }
    }
  ]
}
```

## 学生成绩查询

### 4. 成绩查询（学生）

查询指定学生的成绩及嵌套课程信息，支持学年、学期、课程分类、开课单位和成绩区间筛选。

```
POST /api/v1/sicp/students/grades/query
```

#### 请求参数

| 名称 | 类型 | 必填 | 说明 |
| --- | --- | --- | --- |
| `student_number` | String | 是 | 查询学号；学生自查时由我方工具／MCP 层固定为本人学号 |
| `academic_year` | String | 否 | 学年，如 `2025-2026`；`all` 表示全部学年；不传时的范围见注意事项 |
| `semester` | String | 否 | 学期，`1` 或 `2` |
| `course_code` | String | 否 | 按课程号精确查询 |
| `course_name` | String | 否 | 按课程名称包含匹配 |
| `course_attribute_codes` | String[] | 否 | 按课程属性码 `KCSXM` 筛选，如 `["1"]` 表示必修 |
| `course_nature_codes` | String[] | 否 | 按课程性质码 `KCXZM` 筛选，如 `["301","302"]` 表示专业基础课或专业核心课 |
| `course_type_codes` | String[] | 否 | 按课程类型码 `KCLXM` 筛选，如 `["3"]` 表示专业必修课 |
| `course_department_code` | String | 否 | 按开课单位号精确筛选，不自动包含下级单位 |
| `course_department_name` | String | 否 | 按开课单位名称包含匹配 |
| `score_min` | Number | 否 | 最低成绩，包含边界，如 90 表示成绩大于等于 90 |
| `score_max` | Number | 否 | 最高成绩，包含边界 |
| `page` | Integer | 否 | 页码，默认 1 |
| `page_size` | Integer | 否 | 每页条数，默认 100 |

#### 请求示例

查询本人全部学年中，成绩不低于 80 的必修课：

```json
{
  "student_number": "S000001",
  "academic_year": "all",
  "course_attribute_codes": ["1"],
  "score_min": 80,
  "page": 1,
  "page_size": 100
}
```

#### 响应参数

| 名称 | 类型 | 来源字段 | 说明 |
| --- | --- | --- | --- |
| `total` | Integer | 中台统计 | 筛选后的成绩总数 |
| `synced_at` | String | 新中台同步记录 | 新中台最近一次完整同步本接口所需数据的成功时间 |
| `items` | Grade[] | 中台查询结果 | 成绩列表 |

#### Grade 对象

| 名称 | 类型 | 来源字段 | 说明 |
| --- | --- | --- | --- |
| `grade_id` | String | 成绩表.ID | 成绩记录 ID |
| `student` | GradeStudent | 成绩表.XH → 学生基本表.XH、学籍表.XH | 成绩所属学生 |
| `academic_year` | String | 成绩表.XN | 学年 |
| `semester` | String | 成绩表.XQM | 学期 |
| `course` | Course | 成绩表.KCH → 课程表.KCH | 关联课程信息 |
| `score` | Number/null | 成绩表.KCCJ | 数值成绩；文字成绩返回 null，不新增文字成绩字段，不转换为 0 |
| `grade_point` | Number/null | 成绩表.JD | 绩点 |
| `credits` | Number/null | 成绩表.XF | 学分 |
| `source_updated_at` | String/null | 各关联源表.ETL_TIME | 参与返回结果的源记录中，最新的同步时间 |

#### GradeStudent 对象

| 名称 | 类型 | 来源字段 | 说明 |
| --- | --- | --- | --- |
| `student_number` | String | 成绩表.XH | 学号，保留前导零 |
| `name` | String/null | 学生基本表.XM | 学生姓名 |
| `department_code` | String/null | 学籍表.YXSH | 学生所属院系号 |
| `department_name` | String/null | 院系所单位表.DWMC | 按学生院系号关联的名称 |
| `class_code` | String/null | 学籍表.SZBH | 学生行政班号 |
| `class_name` | String/null | 班级表.BJMC | 按行政班号关联的名称 |

#### Course 对象

| 名称 | 类型 | 来源字段 | 说明 |
| --- | --- | --- | --- |
| `course_code` | String | 成绩表.KCH | 课程号，关联不到课程时仍保留 |
| `course_name` | String/null | 课程表.KCMC | 课程名称 |
| `attribute_code` | String/null | 课程表.KCSXM | 课程属性码 |
| `attribute_name` | String/null | 课程表.KCSXM 代码集 | 课程属性，如必修、选修课 |
| `nature_code` | String/null | 课程表.KCXZM | 课程性质码 |
| `nature_name` | String/null | 课程表.KCXZM 代码集 | 课程性质，如公共基础课、专业核心课 |
| `type_code` | String/null | 课程表.KCLXM | 课程类型码 |
| `type_name` | String/null | 课程表.KCLXM 代码集 | 课程类型，如公共必修课、专业必修课、技能课程 |
| `department` | CourseDepartment/null | 课程表.KKXY → 院系所单位表.DWH | 开课单位 |
| `total_hours` | Number/null | 课程表.ZXS | 总学时 |
| `retake_flag` | String/null | 课程表.SFCXKC | 是否重修课程，保留原始码值，码值含义待确认 |
| `retake_course_code` | String/null | 课程表.CXKCH | 原表配置的重修课程号 |
| `retake_course_name` | String/null | 课程表.CXKCMC | 原表配置的重修课程名称 |

#### CourseDepartment 对象

| 名称 | 类型 | 来源字段 | 说明 |
| --- | --- | --- | --- |
| `organization_code` | String | 课程表.KKXY | 开课单位号 |
| `organization_name` | String/null | 院系所单位表.DWMC | 开课单位名称 |

#### 响应示例

```json
{
  "total": 1,
  "synced_at": "2026-09-18T09:00:00+08:00",
  "items": [
    {
      "grade_id": "GRADE001",
      "academic_year": "2025-2026",
      "semester": "2",
      "score": 86,
      "grade_point": 3.6,
      "credits": 3,
      "source_updated_at": "2026-09-18T08:00:00+08:00",
      "course": {
        "course_code": "COURSE001",
        "course_name": "示例课程",
        "attribute_code": "1",
        "attribute_name": "必修",
        "nature_code": "302",
        "nature_name": "专业核心课",
        "type_code": "3",
        "type_name": "专业必修课",
        "department": {
          "organization_code": "DEPT001",
          "organization_name": "示例学院"
        },
        "total_hours": 48,
        "retake_flag": "1",
        "retake_course_code": "COURSE002",
        "retake_course_name": "示例重修课程"
      },
      "student": {
        "student_number": "S000001",
        "name": "示例学生",
        "department_code": "DEPT001",
        "department_name": "示例学院",
        "class_code": "CLASS001",
        "class_name": "示例班级"
      }
    }
  ]
}
```

#### 注意事项

1. **课程关联**：按课程号关联，一条成绩仍返回一条记录。关联不到课程时保留课程号，其余课程字段为 null；同课程号存在多条课程记录时应报错，不任意选择或放大成绩记录。
2. **成绩学分**：`credits` 取成绩表 `XF`，不以课程表学分覆盖历史成绩学分。
3. **重修信息**：重修字段描述课程表配置，不据此判定该学生本次成绩是否来自重修、补考或第几次考试。
4. **关联缺失**：有开课单位号但未匹配到组织时保留编号，名称为 null；无单位号时 `department` 为 null。代码集未匹配时保留原码，名称为 null。
5. **历史课程信息**：课程属性、性质、类型等取当前课程表关联值，不按成绩所属学期推断或还原历史版本。
6. **查询范围**：
   - 单人查询学年、学期均不传时，先选定该学生最近有成绩的学期，再应用其他筛选条件；无匹配结果时返回空列表，不回退到更早学期。
   - 指定学年、不传学期时，查询该学年的全部学期。单人查询仅传学期时，选取该学生有对应学期成绩的最近学年。
   - `academic_year="all"` 时查询全部学年，仍可按 `semester` 筛选。跨学年查询某门课或某类成绩时应明确传 `all`。
7. **筛选规则**：多个条件取交集，数组内取并集。可选条件不传或为 null 表示不限，空字符串、空数组报参数错误。筛选在分页前完成，`total` 为筛选后的成绩记录数，不按课程号合并考试记录。
8. **成绩区间**：上下限均包含边界，`score_min` 不得大于 `score_max`。设置区间时仅匹配数值成绩，空值或文字成绩不按 0 处理；及格、挂科和学生重修筛选待业务规则确认后补充。
9. **调用范围**：我方工具／MCP 层根据可信登录身份校验查询权限；学生自查时固定本人学号，教师查询指定学生时校验其授权范围。
10. **学生归属**：返回最新学籍中的院系、行政班，不代表历史归属。学生信息关联缺失时保留成绩及学号，缺失字段为 null。

### 5. 成绩查询（批量）

按学生所属学院、行政班或学号列表批量查询成绩，支持课程分类、开课单位和成绩区间筛选。

```
POST /api/v1/sicp/students/grades/batch-query
```

#### 请求参数

| 名称 | 类型 | 必填 | 说明 |
| --- | --- | --- | --- |
| `student_numbers` | String[] | 否 | 按学号列表筛选，不传时按其他条件查询 |
| `student_department_code` | String | 否 | 按学生所属院系号精确筛选，不自动包含下级单位 |
| `student_department_name` | String | 否 | 按学生所属院系名称包含匹配 |
| `class_code` | String | 否 | 按学生行政班号精确筛选 |
| `class_name` | String | 否 | 按学生行政班名称包含匹配 |
| `academic_year` | String | 是 | 学年，如 `2025-2026`；`all` 表示全部学年 |
| `semester` | String | 否 | 学期，`1` 或 `2` |
| `course_code` | String | 否 | 按课程号精确查询 |
| `course_name` | String | 否 | 按课程名称包含匹配 |
| `course_attribute_codes` | String[] | 否 | 按课程属性码 `KCSXM` 筛选，如 `["1"]` 表示必修 |
| `course_nature_codes` | String[] | 否 | 按课程性质码 `KCXZM` 筛选，如 `["301","302"]` 表示专业基础课或专业核心课 |
| `course_type_codes` | String[] | 否 | 按课程类型码 `KCLXM` 筛选，如 `["3"]` 表示专业必修课 |
| `course_department_code` | String | 否 | 按开课单位号精确筛选，不自动包含下级单位 |
| `course_department_name` | String | 否 | 按开课单位名称包含匹配 |
| `score_min` | Number | 否 | 最低成绩，包含边界，如 90 表示成绩大于等于 90 |
| `score_max` | Number | 否 | 最高成绩，包含边界 |
| `page` | Integer | 否 | 页码，默认 1 |
| `page_size` | Integer | 否 | 每页条数，默认 100 |

#### 请求示例

查询指定行政班在指定学期的成绩：

```json
{
  "class_code": "CLASS001",
  "academic_year": "2025-2026",
  "semester": "2",
  "page": 1,
  "page_size": 100
}
```

#### 响应参数

| 名称 | 类型 | 来源字段 | 说明 |
| --- | --- | --- | --- |
| `total` | Integer | 中台统计 | 筛选后的成绩总数 |
| `synced_at` | String | 新中台同步记录 | 新中台最近一次完整同步本接口所需数据的成功时间 |
| `items` | Grade[] | 中台查询结果 | 成绩列表 |

#### Grade 对象

`items` 中每条记录与[成绩查询（学生）的 Grade 对象](#grade-对象)一致，包含 `student` 和 `course` 对象。

#### 响应示例

```json
{
  "total": 1,
  "synced_at": "2026-09-18T09:00:00+08:00",
  "items": [
    {
      "grade_id": "GRADE001",
      "academic_year": "2025-2026",
      "semester": "2",
      "score": 86,
      "grade_point": 3.6,
      "credits": 3,
      "source_updated_at": "2026-09-18T08:00:00+08:00",
      "course": {
        "course_code": "COURSE001",
        "course_name": "示例课程",
        "attribute_code": "1",
        "attribute_name": "必修",
        "nature_code": "302",
        "nature_name": "专业核心课",
        "type_code": "3",
        "type_name": "专业必修课",
        "department": {
          "organization_code": "DEPT001",
          "organization_name": "示例学院"
        },
        "total_hours": 48,
        "retake_flag": "1",
        "retake_course_code": "COURSE002",
        "retake_course_name": "示例重修课程"
      },
      "student": {
        "student_number": "S000001",
        "name": "示例学生",
        "department_code": "DEPT001",
        "department_name": "示例学院",
        "class_code": "CLASS001",
        "class_name": "示例班级"
      }
    }
  ]
}
```

#### 注意事项

1. **查询范围**：必须指定学年或传 `all`；不传学期时查询所选学年的全部学期，不按每名学生各自最近学期取数。
2. **筛选规则**：多个条件取交集，数组内取并集。可选条件不传或为 null 表示不限，空字符串、空数组报参数错误。筛选后分页，`total` 为成绩记录总数，不是学生人数，不按课程号合并考试记录。
3. **学生归属**：学院、行政班筛选取最新学籍信息，与课程开课单位筛选独立；当前归属不代表历史师生关系。
4. **调用范围**：我方工具／MCP 层校验并限定可查询的学生、学院或班级范围；不传学号列表不代表获得全校查询权限。学生自查工具使用成绩查询（学生）接口。
5. **共用规则**：课程关联、成绩学分、重修信息、关联缺失、历史课程信息及成绩区间规则与[成绩查询（学生）](#4-成绩查询学生)一致。

## 学生课表查询

### 6. 课表查询（学生）

一次返回学生本人实际排课的课程、上课时间、教师和地点，由中台完成课程关系与排课关联。返回完整列表，不使用分页参数或成绩快照请求头。

```
POST /api/v1/sicp/students/timetable/query
```

关联规则：

- 从学生本人课程关系出发，按教学班号、计划课程号及学年学期匹配排课。
- 不按行政班名、专业或课程名称猜归属。
- 计划课程号按 `JHKCH = KCH` 关联课程；未排课课程不进入课表。已排课但课程名称、日期等可空信息缺失的记录按字段及文末空值规则处理，不能因此视为未排课。

#### 请求参数

| 名称 | 类型 | 必填 | 说明 |
| --- | --- | --- | --- |
| `student_number` | String | 是 | 由我方可信登录信息注入，不由用户或模型填写 |
| `academic_year` | String | 是 | 学年，如 `2026-2027` |
| `semester` | String | 是 | 学期，`1` 或 `2` |
| `course_query` | String | 否 | 按课程名称或编码筛选 |

#### 响应参数

| 名称 | 类型 | 来源字段 | 说明 |
| --- | --- | --- | --- |
| `student_number` | String | 请求参数.student_number | 查询学号 |
| `academic_year` | String | 请求参数.academic_year | 查询学年 |
| `semester` | String | 请求参数.semester | 查询学期 |
| `synced_at` | String | 新中台同步记录 | 新中台最近一次完整同步本接口所需数据的成功时间 |
| `items` | TimetableEntry[] | 中台查询结果 | 排课列表，一条表示同课程、教师、地点及时间安排 |

#### TimetableEntry 对象

| 名称 | 类型 | 来源字段 | 说明 |
| --- | --- | --- | --- |
| `course_code` | String | 排课表.JHKCH → 课程表.KCH | 课程编码，按编号等值关联 |
| `course_name` | String/null | 课程表.KCMC | 课程名称，未关联到时为 null |
| `planned_course_code` | String | 排课表.JHKCH | 计划课程号 |
| `teaching_class_number` | String | 排课表.JXBH | 教学班号 |
| `teaching_class_name` | String/null | 排课表.JXBMC | 教学班名称，可能为合班或分班名称 |
| `enrolled_count` | Integer/null | 排课表.XDRS | 修读人数，缺失时不以 0 代替 |
| `teacher_staff_numbers` | String[] | 排课表.JSJGH | 拆分后的完整教师工号，去重返回 |
| `room_code` | String/null | 排课表.SKJSH | 教室号，保留前导零 |
| `credits` | Number/null | 课程表.XF | 学分 |
| `teacher_names` | String[] | 排课表.JSXM | 任课教师姓名 |
| `location` | String/null | 排课表.JXDD | 上课地点 |
| `weekday` | Integer | 排课表.XQJ | 星期，1–7 对应周一至周日 |
| `sections` | Integer[] | 排课表.SKJC | 节次，如 `[1,2]` |
| `weeks` | Integer[] | 排课表.ZC | 上课周次，如 `[1,3,5]` |
| `dates` | String[] | 排课表.SKRQ | 实际上课日期，统一为 YYYY-MM-DD，用于今天、明天查询 |
| `source_updated_at` | String/null | 各关联源表.ETL_TIME | 参与返回结果的源记录中，最新的同步时间 |

#### 响应示例

```json
{
  "student_number": "S000001",
  "academic_year": "2026-2027",
  "semester": "1",
  "synced_at": "2026-09-18T09:00:00+08:00",
  "items": [
    {
      "course_code": "COURSE001",
      "course_name": "示例课程",
      "planned_course_code": "COURSE001",
      "teaching_class_number": "TEACHING001",
      "credits": 3,
      "teacher_names": [
        "示例教师"
      ],
      "location": "示例教学楼101",
      "weekday": 1,
      "sections": [
        1,
        2
      ],
      "weeks": [
        1,
        2
      ],
      "dates": [
        "2026-09-07",
        "2026-09-14"
      ],
      "source_updated_at": "2026-09-18T08:00:00+08:00",
      "teaching_class_name": "示例教学班",
      "enrolled_count": 37,
      "teacher_staff_numbers": [
        "T000001"
      ],
      "room_code": "012201"
    }
  ]
}
```

#### 注意事项

1. **作息时间**：暂按节次映射固定作息表，学生和教师共用。节次与起止时间对应如下。

| 节次 | 开始时间 | 结束时间 |
| --- | --- | --- |
| 1 | 08:20 | 08:59 |
| 2 | 09:00 | 09:40 |
| 3 | 09:50 | 10:30 |
| 4 | 10:35 | 11:10 |
| 5 | 11:15 | 11:55 |
| 6 | 13:10 | 13:49 |
| 7 | 13:50 | 14:30 |
| 8 | 14:40 | 15:19 |
| 9 | 15:20 | 16:00 |
| 10 | 16:10 | 16:49 |
| 11 | 16:50 | 17:30 |
| 12 | 17:35 | 18:15 |
| 13 | 19:00 | 19:40 |

2. **日期与周次**：上课日期取排课表 `SKRQ`，周次取 `ZC`，不直接把 `QSZ` 至 `ZZZ` 展开为每周都有课。
3. **多教师记录**：排课表 `JSJGH` 包含多个工号时，拆分后按完整工号匹配，并保留关联的全部教师。
4. **节次边界**：按上述表逐节映射，不按固定时长累加；表外节次的作息时间另行确认，不自行推算。

5. **课程关联**：按 `JHKCH = KCH` 等值关联，不截取编号或按课程名猜测。未匹配时保留计划课程号及排课记录，课程名称、学分为 null；出现同号多条课程时应报错，不任意选择或使排课记录被联表放大。

## 教师课表查询

### 7. 课表查询（教师）

查询指定教师在所选学年学期的授课安排，返回课程、教学班、人数、日期、节次及教室，包含关联多个教师的排课记录。

```
POST /api/v1/sicp/staff/timetable/query
```

#### 请求参数

| 名称 | 类型 | 必填 | 说明 |
| --- | --- | --- | --- |
| `staff_number` | String | 是 | 教师工号；教师自查时由我方工具／MCP 层固定为本人工号 |
| `academic_year` | String | 是 | 学年，如 `2026-2027` |
| `semester` | String | 是 | 学期，`1` 或 `2` |
| `course_query` | String | 否 | 课程号精确匹配或课程名称包含匹配 |
| `teaching_class_number` | String | 否 | 按教学班号精确筛选 |

#### 请求示例

```json
{
  "staff_number": "T000001",
  "academic_year": "2026-2027",
  "semester": "1"
}
```

#### 响应参数

| 名称 | 类型 | 来源字段 | 说明 |
| --- | --- | --- | --- |
| `staff_number` | String | 请求参数.staff_number | 查询工号 |
| `academic_year` | String | 请求参数.academic_year | 查询学年 |
| `semester` | String | 请求参数.semester | 查询学期 |
| `synced_at` | String | 新中台同步记录 | 最近一次完整同步本接口所需数据的成功时间 |
| `items` | TimetableEntry[] | 中台查询结果 | 完整授课安排列表，无匹配时返回空数组 |

#### TimetableEntry 对象

与学生课表共用以下字段定义：

| 名称 | 类型 | 来源字段 | 说明 |
| --- | --- | --- | --- |
| `course_code` | String | 排课表.JHKCH → 课程表.KCH | 课程编码，按编号等值关联 |
| `course_name` | String/null | 课程表.KCMC | 课程名称，未关联到时为 null |
| `planned_course_code` | String | 排课表.JHKCH | 计划课程号 |
| `teaching_class_number` | String | 排课表.JXBH | 教学班号 |
| `teaching_class_name` | String/null | 排课表.JXBMC | 教学班名称，可能为合班或分班名称 |
| `enrolled_count` | Integer/null | 排课表.XDRS | 修读人数，缺失时不以 0 代替 |
| `teacher_staff_numbers` | String[] | 排课表.JSJGH | 拆分后的完整教师工号，去重返回 |
| `room_code` | String/null | 排课表.SKJSH | 教室号，保留前导零 |
| `credits` | Number/null | 课程表.XF | 学分 |
| `teacher_names` | String[] | 排课表.JSXM | 任课教师姓名 |
| `location` | String/null | 排课表.JXDD | 上课地点 |
| `weekday` | Integer | 排课表.XQJ | 星期，1–7 对应周一至周日 |
| `sections` | Integer[] | 排课表.SKJC | 节次，如 `[1,2]` |
| `weeks` | Integer[] | 排课表.ZC | 上课周次，如 `[1,3,5]` |
| `dates` | String[] | 排课表.SKRQ | 实际上课日期，统一为 YYYY-MM-DD，用于今天、明天查询 |
| `source_updated_at` | String/null | 各关联源表.ETL_TIME | 参与返回结果的源记录中，最新的同步时间 |

#### 响应示例

```json
{
  "staff_number": "T000001",
  "academic_year": "2026-2027",
  "semester": "1",
  "synced_at": "2026-09-18T09:00:00+08:00",
  "items": [
    {
      "course_code": "COURSE001",
      "course_name": "示例课程",
      "planned_course_code": "COURSE001",
      "teaching_class_number": "TEACHING001",
      "credits": 3,
      "teacher_names": [
        "示例教师"
      ],
      "location": "示例教学楼101",
      "weekday": 1,
      "sections": [
        1,
        2
      ],
      "weeks": [
        1,
        2
      ],
      "dates": [
        "2026-09-07",
        "2026-09-14"
      ],
      "source_updated_at": "2026-09-18T08:00:00+08:00",
      "teaching_class_name": "示例教学班",
      "enrolled_count": 37,
      "teacher_staff_numbers": [
        "T000001"
      ],
      "room_code": "012201"
    }
  ]
}
```

#### 注意事项

1. **教师匹配**：拆分排课表 `JSJGH` 中的多教师工号后，按完整工号匹配。不得用工号子串或姓名匹配。
2. **查询范围**：按排课表 `KKXND`、`KKXQM` 筛选学年学期，再应用课程与教学班条件；多个条件取交集。一次返回符合条件的完整结果，读取失败应报错，不得返回成功的部分课表。
3. **多教师与合班**：匹配任一授课教师即纳入该安排，同一安排不因多个教师匹配重复返回。保留全部教师工号及姓名；不能仅按课程号合并不同教学班、日期、节次或地点。工号数组与姓名数组不承诺按下标对应。
4. **课程与时间**：复用[学生课表](#6-课表查询学生)的课程等值关联、日期与周次处理及暂定作息时间表。
5. **班级与人数**：返回教学班名称及排课记录的修读人数，不以教师所属学院或学生行政班推断授课关系。
6. **调用权限**：我方工具／MCP 层校验教师身份和允许查询的工号范围，本接口不通过学生课表反推教师课表。

## 教学班与学生关系同步

用于阅卷系统同步教师所授教学班及班内学生，先查询教师教学班，再逐班分页获取学生名单。

### 8. 教师教学班查询

返回指定教师在所选学年学期的教学班、课程、授课教师及学生人数。

```
POST /api/v1/sicp/staff/teaching-classes/query
```

#### 请求参数

| 名称 | 类型 | 必填 | 说明 |
| --- | --- | --- | --- |
| `staff_number` | String | 是 | 教师工号 |
| `academic_year` | String | 是 | 学年，如 `2026-2027` |
| `semester` | String | 是 | 学期，`1` 或 `2` |
| `course_code` | String | 否 | 按课程号精确筛选 |
| `page` | Integer | 否 | 页码，默认 1 |
| `page_size` | Integer | 否 | 每页条数，默认 100 |

#### 请求示例

```json
{
  "staff_number": "T000001",
  "academic_year": "2026-2027",
  "semester": "1",
  "page": 1,
  "page_size": 100
}
```

#### 响应参数

| 名称 | 类型 | 来源字段 | 说明 |
| --- | --- | --- | --- |
| `staff_number` | String | 请求参数.staff_number | 查询工号 |
| `academic_year` | String | 请求参数.academic_year | 查询学年 |
| `semester` | String | 请求参数.semester | 查询学期 |
| `total` | Integer | 中台统计 | 筛选并去重后的教学班课程组合数 |
| `synced_at` | String | 新中台同步记录 | 最近一次完整同步本接口所需数据的成功时间 |
| `items` | TeachingClass[] | 中台查询结果 | 教学班课程列表 |

#### TeachingClass 对象

| 名称 | 类型 | 来源字段 | 说明 |
| --- | --- | --- | --- |
| `teaching_class_number` | String | 排课表.JXBH | 教学班号 |
| `teaching_class_name` | String/null | 排课表.JXBMC | 教学班名称，不作为唯一标识 |
| `planned_course_code` | String | 排课表.JHKCH | 计划课程号 |
| `course_code` | String | 排课表.JHKCH → 课程表.KCH | 课程号，未关联到课程时保留计划课程号 |
| `course_name` | String/null | 课程表.KCMC | 课程名称 |
| `teachers` | TeachingClassTeacher[] | 排课表.JSJGH → 教职工基础表.JGH | 所选学期该教学班课程关联的全部教师 |
| `student_count` | Integer | 学生课表关系表.XH | 按教学班号和计划课程号关联后，去重统计的学生人数 |
| `source_updated_at` | String/null | 各关联源表.ETL_TIME | 参与返回结果的源记录中，最新的同步时间 |

#### TeachingClassTeacher 对象

| 名称 | 类型 | 来源字段 | 说明 |
| --- | --- | --- | --- |
| `staff_number` | String | 排课表.JSJGH | 拆分后的教师工号 |
| `name` | String/null | 教职工基础表.XM | 按工号关联的姓名，未匹配时保留工号、姓名为 null |

#### 响应示例

```json
{
  "staff_number": "T000001",
  "academic_year": "2026-2027",
  "semester": "1",
  "total": 1,
  "synced_at": "2026-09-18T09:00:00+08:00",
  "items": [
    {
      "teaching_class_number": "TEACHING001",
      "teaching_class_name": "示例教学班",
      "planned_course_code": "COURSE001",
      "course_code": "COURSE001",
      "course_name": "示例课程",
      "teachers": [
        {"staff_number": "T000001", "name": "示例教师"}
      ],
      "student_count": 1,
      "source_updated_at": "2026-09-18T08:00:00+08:00"
    }
  ]
}
```

#### 注意事项

1. **教师匹配**：按完整工号匹配拆分后的 `JSJGH`，包含多教师关联的排课记录。`teachers` 返回该学期该教学班课程的全部关联教师，不只返回查询教师。
2. **去重与标识**：以学年、学期、教学班号、计划课程号共同标识教学班课程组合。同一组合的多次排课只返回一项，同一组合关联多个教师时不得重复创建班级。
3. **学生人数**：按实际学生关系去重统计，与教学班学生查询的 `total` 口径一致，不使用排课表 `XDRS` 代替。读取名单失败应报错，不得返回人数 0。
4. **分页与权限**：筛选、去重后分页，顺序固定为教学班号、计划课程号升序。我方工具／MCP 层校验教师查询范围；接口结果表示授课关系，不代表考试阅卷任务分配。
5. **数据范围**：以所选学期排课记录确定授课关系，尚无排课记录的教学任务不在本接口范围内。

### 9. 教学班学生查询

返回指定学年学期、教学班及课程的学生名单，用于同步教学班成员关系。

```
POST /api/v1/sicp/teaching-classes/students/query
```

#### 请求参数

| 名称 | 类型 | 必填 | 说明 |
| --- | --- | --- | --- |
| `teaching_class_number` | String | 是 | 教学班号 |
| `planned_course_code` | String | 是 | 计划课程号 |
| `academic_year` | String | 是 | 学年，如 `2026-2027` |
| `semester` | String | 是 | 学期，`1` 或 `2` |
| `page` | Integer | 否 | 页码，默认 1 |
| `page_size` | Integer | 否 | 每页条数，默认 100 |

#### 请求示例

```json
{
  "teaching_class_number": "TEACHING001",
  "planned_course_code": "COURSE001",
  "academic_year": "2026-2027",
  "semester": "1",
  "page": 1,
  "page_size": 100
}
```

#### 响应参数

| 名称 | 类型 | 来源字段 | 说明 |
| --- | --- | --- | --- |
| `teaching_class_number` | String | 请求参数.teaching_class_number | 查询教学班号 |
| `planned_course_code` | String | 请求参数.planned_course_code | 查询计划课程号 |
| `academic_year` | String | 请求参数.academic_year | 查询学年 |
| `semester` | String | 请求参数.semester | 查询学期 |
| `total` | Integer | 学生课表关系表.XH | 符合条件且按学号去重后的学生总数 |
| `synced_at` | String | 新中台同步记录 | 最近一次完整同步本接口所需数据的成功时间 |
| `items` | TeachingClassStudent[] | 中台查询结果 | 学生名单 |

#### TeachingClassStudent 对象

| 名称 | 类型 | 来源字段 | 说明 |
| --- | --- | --- | --- |
| `student_number` | String | 学生课表关系表.XH | 学号，保留前导零 |
| `name` | String/null | 学生基本表.XM | 学生姓名 |
| `department_code` | String/null | 学籍表.YXSH | 当前所属院系号 |
| `department_name` | String/null | 院系所单位表.DWMC | 按院系号关联的名称 |
| `class_code` | String/null | 学籍表.SZBH | 当前行政班号，与查询教学班号不同 |
| `class_name` | String/null | 班级表.BJMC | 按行政班号关联的名称 |
| `source_updated_at` | String/null | 各关联源表.ETL_TIME | 参与返回结果的源记录中，最新的同步时间 |

#### 响应示例

```json
{
  "teaching_class_number": "TEACHING001",
  "planned_course_code": "COURSE001",
  "academic_year": "2026-2027",
  "semester": "1",
  "total": 1,
  "synced_at": "2026-09-18T09:00:00+08:00",
  "items": [
    {
      "student_number": "S000001",
      "name": "示例学生",
      "department_code": "DEPT001",
      "department_name": "示例学院",
      "class_code": "CLASS001",
      "class_name": "示例行政班",
      "source_updated_at": "2026-09-18T08:00:00+08:00"
    }
  ]
}
```

#### 注意事项

1. **成员来源**：按 `JXBH + JHKCH` 查询学生课表关系，按学号去重。不能按行政班补入学生，也不能因当前学籍状态或关联名称缺失而删除名单中的学生。
2. **学期校验**：先确认排课表中存在所选学年学期对应的教学班课程组合，再查询成员；组合不存在时应报错。学生关系表本身没有学期字段，若同一组合跨学期复用且无法区分成员归属，应报错并要求明确历史关系来源，不得返回混合名单。
3. **信息关联**：基本信息或学籍缺失时保留学号，缺失字段返回 null。院系与行政班取最新学籍，不表示该历史学期的行政归属。
4. **完整同步**：按学号升序分页，保证不漏不重；全部分页成功并核对人数后，调用方再更新班级成员。读取失败不得返回成功的空名单或部分名单。
5. **调用权限**：我方工具／MCP 层校验可查询的教学班课程范围。名单用于授课关系同步，具体考试考生及阅卷安排由阅卷系统管理。

## 时间字段与增量同步

| 字段 | 含义 | 用途 |
| --- | --- | --- |
| `source_updated_at` | 原始接口的 `ETL_TIME`；聚合记录取参与返回结果的各源记录最大值 | 追溯该记录在原中台的最近更新时间，不等同于业务实际修改时间 |
| `synced_at` | 新中台完整同步该接口所需各数据集的最近成功时间；各数据集单独同步时取成功时间的最小值 | 判断新中台是否已更新，不能用接口调用时间代替 |

- 时间字段返回带时区的字符串。
- 单条记录的最大 `ETL_TIME` 不能证明其他数据也已同步到该时间。
- 首期人员同步按全量分页查询，不增加 `startTime/endTime`；后续需要增量时，再约定更新范围及删除处理。

### 2026-10-09：源字段空值兼容说明

依据 `source_api_docs` 的可空标记，课表查询与构建冒烟校验统一采用以下规则：

- `KKXND`、`KKXQM` 缺失、null 或空白时，不把记录归入任何学年学期。
- 学生课表关联仍要求完整 `JXBH + JHKCH`；缺失可空的 `JHKCH` 时不推测课程，不影响其他有效关系。
- 教师课表保留缺失 `JHKCH`、`JXBH` 的排课条目，`course_code`、`planned_course_code`、`teaching_class_number` 对应值为 `""`，无法关联的课程名称、学分为 null。
- `SKRQ` 缺失、null 或空白时，`dates` 为 `[]`；不按周次推算日期。教师字段缺失时保持空数组。
- 可空数值字段的空字符串作为 null，不填 0。已有非空值的格式错误仍报错；`PKH`、`XQJ`、`SKJC`、`ZC` 仍必要。
