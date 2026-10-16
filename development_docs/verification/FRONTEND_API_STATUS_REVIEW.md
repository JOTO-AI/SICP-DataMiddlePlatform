# V12 接口实现图标与方法颜色复核（2026-10-16）

- [x] 接口列表和详情的“已实现”前显示绿色圆形勾选 SVG；只有后端 implemented === true 显示，未实现/未知保留原文字。
- [x] POST 使用统一深绿文字、浅绿背景标签，提高字号与字重，使用已有设计色彩。
- [x] 8/8 浏览器回归通过，类型检查、生产构建、57 文件产物审计及 code review 完成。

## 验证与 review

回放原后端目录，核对列表 9 个业务接口图标、列表/详情 POST 计算颜色、390px 无横向溢出及导航。false/null 是实现状态边界，复用后端目录其他字段；未编造查询数据。复核 1280px 列表与 390px 详情截图，证据在 artifacts/frontend-api-status/report.json 和 browser 下。

StatusBadge 新增可选 check 图标，默认不渲染，不影响其他状态页。SVG 设 aria-hidden/focusable=false，状态含义仍由文字传达；实现状态不改变查询服务就绪判断。图标直接内嵌，无外部字体或资源依赖。检查通过，无未解决 review 问题。仅需部署 console。
