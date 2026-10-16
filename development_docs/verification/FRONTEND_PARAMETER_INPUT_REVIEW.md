# V09 在线测试参数输入复核（2026-10-16）

- [x] 参数模式的字符串直接输入，不要求 JSON 引号；文本数组每行一项，布尔字段使用选择框。
- [x] 依据后端字段类型编码请求，保留 JSON 模式和原始草稿，完成针对性测试与 code review。

## 行为与边界

RequestEditor 通过 ParameterDraft 保存字段原值和是否修改。文本不裁剪、不解析成数字/布尔/null，学号前导零及引号、反斜杠原样保留，由 JSON.stringify 编码。字符串数组由后端 items.type 判定，不按逗号拆分，也不悄悄丢弃空行。布尔选择正确区分 false、null 和省略。清空编辑过的字段表示省略，必填字段仍报错。

未编辑的 null、空字符串、空数组和错误类型保留原值；旧 JSON 中类型不匹配的值保留 JSON 编辑方式，不因修改其它字段变成有效请求。字符串/列表的原始 null 用占位提示，输入后变为相应类型；显式设置 null 可使用 JSON 模式。数字保留原 token 检查，整数的小数/指数形式不会因转换被偷偷规范化。无修改切回 JSON 保留原文，错误留在当前编辑稿，未知字段仍交给现有契约校验。业务请求和后端接口不变。

## 来源与验证

- 9/9 针对性单元通过（json、testRequest、parameterDraft）：九个业务接口真实捕获请求往返；字符串前导零、引号、反斜杠、false/null 文本；清空、未修改 null/空字符串/错误类型；列表和布尔值。
- 普通请求来自 frontend/fixtures/v1/business.json。列表和布尔边界直接读取既有 internal/query/contract/testdata/batch_grades_request.json 与 phase2.json；没有新增正常业务 Mock。协议边界测试与正常请求分开。
- 主浏览器回归 24/24：双 origin 的九接口执行、原 JSON 编辑器、在线参数校验和纯文本填写后实际发送内容比对。
- 补充数组/布尔控件最终 4/4。首次 2/4：误把含 course_attribute_codes=[] 的编解码样本当作有效业务请求，编辑器正确拒绝。有效数组场景改用样本中原始 student_numbers、academic_year 两字段，没有改变样本值；组织场景保留完整原请求。未放宽生产校验。
- 类型检查、生产构建、56 文件产物审计及 150 项来源 hash 检查通过；人工复核 1440px/390px 参数模式截图。
- 证据：artifacts/frontend-plain-parameters/report.json（24 项）、controls-report.json（首次补充）、controls-final-report.json（最终 4 项）及各目录截图。

## Code review

检查类型识别、未修改字段保留、未知字段、空值/省略、字符串转义、无自动请求、必填/高级参数及失败草稿保留。未发现未解决问题。仅需更新 console；服务器真实联调仍按既有安排延期。
