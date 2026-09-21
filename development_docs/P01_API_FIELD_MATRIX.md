# P01 四接口字段矩阵

本矩阵逐项引用业务设计原文，DTO 位于 `internal/query/contract/dto.go`。业务设计是权威来源；通用架构中旧版五个 GET 接口不适用于本阶段。

| 接口 | POST 路径 | 唯一操作标识 |
| --- | --- | --- |
| 4 | /api/v1/sicp/students/grades/query | students.grades.query |
| 5 | /api/v1/sicp/students/grades/batch-query | students.grades.batch-query |
| 6 | /api/v1/sicp/students/timetable/query | students.timetable.query |
| 7 | /api/v1/sicp/staff/timetable/query | staff.timetable.query |

响应字段没有 omitempty；可空标量使用指针，零值响应数组输出 []。请求数组保留 null 和 [] 的区别；请求标量指针保留 null 与 0。必填、null、默认值和上限的正式校验在 P06 接入；P01 DTO 本身不声称验证参数。请求夹具包括空数组等待拒绝参数，专用于检验传输类型保真，不代表合法业务请求。

成绩稳定排序约定：student_number、grade_id 升序。课表按源 PKH 升序，PKH 不进入响应；P06 实现。成绩 total 为过滤后的记录数；课表不分页。

## 4 / StudentGradesRequest

Source: [sicp-api-design.md](../sicp-api-design.md), sections 4-7.

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


## 4, 5 / GradesResponse, Grade, GradeStudent, Course, CourseDepartment

Source: [sicp-api-design.md](../sicp-api-design.md), sections 4-7.

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
| `score` | Number/null | 成绩表.KCCJ | 数值成绩；非数值成绩如何表达待讨论 |
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


## 5 / BatchGradesRequest

Source: [sicp-api-design.md](../sicp-api-design.md), sections 4-7.

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


## 6 / StudentTimetableRequest, StudentTimetableResponse, TimetableEntry

Source: [sicp-api-design.md](../sicp-api-design.md), sections 4-7.

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


## 7 / StaffTimetableRequest

Source: [sicp-api-design.md](../sicp-api-design.md), sections 4-7.

#### 请求参数

| 名称 | 类型 | 必填 | 说明 |
| --- | --- | --- | --- |
| `staff_number` | String | 是 | 教师工号；教师自查时由我方工具／MCP 层固定为本人工号 |
| `academic_year` | String | 是 | 学年，如 `2026-2027` |
| `semester` | String | 是 | 学期，`1` 或 `2` |
| `course_query` | String | 否 | 课程号精确匹配或课程名称包含匹配 |
| `teaching_class_number` | String | 否 | 按教学班号精确筛选 |


## 7 / StaffTimetableResponse, TimetableEntry

Source: [sicp-api-design.md](../sicp-api-design.md), sections 4-7.

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
