# 公共组件（F02-03）

- `layouts/AppShell.vue`：应用导航、侧栏折叠、主体与会话出口；已随 F01/F02 前置步骤接入。
- `PageHeader`：标题、说明及操作插槽，已用于三个页面。
- `FilterBar`：原生 form/fieldset，busy 时禁止重复提交与编辑；已接入查询页。
- `StatusBadge`：已知状态标签及语义色；未知值保留原枚举并显示“未知状态”，默认中性，不冒充成功。领域可传 labels/label/tone；任务详情已使用。
- `Timestamp`：RFC3339 时间按浏览器本地时区展示，保留原始 datetime/title；缺失为“未提供”，非法为“未知时间”，领域可设置 empty 文案。查询结果已使用；旧 time() 继续保留阶段时间“尚未发生”语义。
- `CopyId`：保留字符串编号，Clipboard 可用时明确反馈；权限拒绝或内网 HTTP 不支持时提供可选择的只读文本，不谎报复制成功。切页/换编号后忽略旧复制结果。查询请求号已使用。
- `CursorTable`：接收后端当前页 rows/nextCursor，发出 next(cursor)/previous/retry，游标不解析、不推算页码/总页数。读取中禁用分页；无结果的加载/错误不显示为零；保留旧结果时说明来源。后续日志/事件页接入。
- `EmptyState`：未操作/空结果/加载状态，加载用静态占位条，不伪造内容。查询页已使用。
- `ErrorState`：文本错误、可选请求号及显式重读事件，无自动提交。查询页已使用。
- `ConnectionStatus`：连接、连接中、断线、暂停、未知，包含文字；供后续流读取页面使用。

`shared/display.ts` 的通用显示规则：0 保留“0”，false 为“否”，null/undefined/空字符串为“未提供”，空数组为“无”，对象提示查看详情。编号与大整数字符串原样显示；不使用 truthiness 把零归为缺失。领域明确规定的成绩“—”、组织有效性等标签仍由领域代码表达，不改变后端含义。

组件验证入口只在 `e2e/components.html`，通过测试注入 Go 导出的学生记录；不写业务示例进 src，入口与测试数据不进入生产 dist。协议异常值与不透明游标仅验证组件边界，不作为业务数据或有效服务端游标。

## 弹窗与抽屉（F02-04）

- `Dialog`：原生 showModal + Teleport 到 body，标题/描述关联，首焦点在标题（或明确 autofocus），Tab 循环，Escape/遮罩/关闭直接退出；嵌套弹窗按栈恢复滚动锁和焦点，触发控件失效则回到主内容。停用和卸载释放资源。
- `Drawer`：共享 Dialog 行为，右侧固定高度、内容内部滚动，小屏撑满视口。
- `AsyncActionDialog`：确认按钮 busy 时禁用，关闭仅发出一次 close(reason)；父页面负责停止等待及保留未知受理状态，组件不会取消服务端任务、发 HTTP 或重试。已接入任务确认。
- 层级由 tokens.css 统一命名；原生 modal 使用浏览器 top layer。抽屉不依赖局部 stacking context。
