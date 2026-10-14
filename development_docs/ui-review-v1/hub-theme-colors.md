# AI Hub 主题色清单

色值来源于 `frontend/src/styles/global.css` 的 `:root` 色板，按用途分组。Hub 目前只有浅色主题。

- 「用量」是该 token 在 `frontend/src` 中被 `var()` 引用的大致次数，用来区分主力色和局部色。
- 聊天里用户气泡的颜色可以在管理端「品牌设置」中覆盖，表中写的是默认值。

## 1. 品牌主色与交互

| Token | HEX | RGB | 大致颜色 | 主要用途 | 用量 |
| --- | --- | --- | --- | --- | --- |
| `--color-primary` | `#195BFF` | `rgb(25, 91, 255)` | 亮宝蓝 | 品牌主色：主按钮、链接、选中态、进度条、高亮图标 | ~250 |
| `--color-primary-hover` | `#0F48D8` | `rgb(15, 72, 216)` | 深宝蓝 | 主按钮 / 主色元素悬停 | 少量 |
| `--color-primary-soft` | `#195BFF` @14% | `rgba(25, 91, 255, 0.14)` | 浅蓝底 | 主色浅底：选中标签、附件 / 产物卡片、日志高亮 | ~40 |
| `--color-focus` | `#2563EB` | `rgb(37, 99, 235)` | 标准蓝 | 键盘焦点框（focus ring）、输入框聚焦描边 | ~200 |

## 2. 状态色

| Token | HEX | RGB | 大致颜色 | 主要用途 | 用量 |
| --- | --- | --- | --- | --- | --- |
| `--color-success` | `#16823C` | `rgb(22, 130, 60)` | 深绿 | 成功提示、已绑定 / 已连接状态 | 少量 |
| `--color-danger` | `#D92D20` | `rgb(217, 45, 32)` | 警示红 | 删除 / 危险按钮、错误提示、表单校验 | ~90 |
| `--color-danger-hover` | `#B42318` | `rgb(180, 35, 24)` | 暗红 | 危险按钮悬停 | 少量 |
| （浅危险按钮底） | `#D92D20` @10% / 16% | `rgba(217, 45, 32, 0.10)` / `0.16` | 浅红底 | 次级危险按钮的底色 / 悬停底色 | 少量 |

## 3. 背景与表面

| Token | HEX | RGB | 大致颜色 | 主要用途 | 用量 |
| --- | --- | --- | --- | --- | --- |
| `--color-app-bg` | `#F7F8FA` | `rgb(247, 248, 250)` | 极浅冷灰 | 整个应用外层背景 | 全局 |
| `--color-panel` | `#FFFFFF` | `rgb(255, 255, 255)` | 纯白 | 面板、卡片、弹窗、抽屉内容区 | 全局 |
| `--color-surface-muted` | `#F3F5F8` | `rgb(243, 245, 248)` | 浅灰蓝 | 次级区块底色：表单分组、只读信息块、上传预览区 | ~100 |
| `--color-hover-muted` | `#1A1C19` @6% | `rgba(26, 28, 25, 0.06)` | 半透明浅灰 | 列表行、图标按钮、菜单项悬停 | ~55 |
| `--color-skeleton` | `#E8EBF0` | `rgb(232, 235, 240)` | 浅灰 | 加载骨架屏 | ~25 |
| `--color-chat-user-bubble` | `#F4F4F4` | `rgb(244, 244, 244)` | 浅中性灰 | 聊天中用户消息气泡（品牌设置可改） | 聊天 |
| `--color-chat-user-bubble-text` | `#000000` | `rgb(0, 0, 0)` | 黑 | 用户气泡文字（品牌设置可改） | 聊天 |
| `--color-scrollbar-thumb` | `#1A1C19` @20% | `rgba(26, 28, 25, 0.2)` | 半透明灰 | 滚动条滑块 | 全局 |

## 4. 文字

| Token | HEX | RGB | 大致颜色 | 主要用途 | 用量 |
| --- | --- | --- | --- | --- | --- |
| `--color-text-strong` | `#111827` | `rgb(17, 24, 39)` | 近黑（带蓝） | 页面标题、强调文字、首页品牌标题 | ~55 |
| `--color-text` | `#1A1C19` | `rgb(26, 28, 25)` | 近黑（中性） | 正文默认文字 | ~360 |
| `--color-text-muted` | `#667085` | `rgb(102, 112, 133)` | 中灰蓝 | 次要文字：描述、表头、辅助说明、时间戳 | ~520 |
| `--color-text-subtle` | `#9AA3AF` | `rgb(154, 163, 175)` | 浅灰 | 更弱的提示、禁用文字、空状态说明 | ~75 |
| `--color-control-placeholder` | `#9AA3AF` | `rgb(154, 163, 175)` | 浅灰 | 输入框 placeholder | 少量 |

## 5. 边框与分割线

| Token | HEX | RGB | 大致颜色 | 主要用途 | 用量 |
| --- | --- | --- | --- | --- | --- |
| `--color-border` | `#D6DBE4` | `rgb(214, 219, 228)` | 浅灰蓝 | 通用边框 | ~90 |
| `--color-panel-border` | `#D9DEE8` | `rgb(217, 222, 232)` | 浅灰蓝 | 面板、卡片、表格外框 | ~100 |
| `--color-panel-border-subtle` | `#E3E7EF` | `rgb(227, 231, 239)` | 更浅灰蓝 | 面板内部的弱边框 | 少量 |
| `--color-control-border` | `#CFD6E2` | `rgb(207, 214, 226)` | 灰蓝 | 输入框、选择器等表单控件边框 | ~50 |
| `--color-control-border-hover` | `#B8C2D0` | `rgb(184, 194, 208)` | 中灰蓝 | 表单控件悬停边框 | ~15 |
| `--color-divider` | `#E3E7EF` | `rgb(227, 231, 239)` | 更浅灰蓝 | 分割线、列表 / 表格行线 | ~110 |
| `--color-divider-subtle` | `#1A1C19` @6% | `rgba(26, 28, 25, 0.06)` | 半透明浅灰 | 极弱分割线 | 少量 |

## 6. 侧栏

用户端侧栏的选中态用中性灰，管理端侧栏的选中态用浅蓝加蓝字，两端视觉上有区分。

| Token | HEX | RGB | 大致颜色 | 主要用途 |
| --- | --- | --- | --- | --- |
| `--color-sidebar-bg` | `#F6F7F9` | `rgb(246, 247, 249)` | 极浅冷灰 | 侧栏背景 |
| `--color-sidebar-hover` | `#F1F3F5` | `rgb(241, 243, 245)` | 浅灰 | 侧栏菜单项悬停 |
| `--color-sidebar-button-hover` | `#F9FAFB` | `rgb(249, 250, 251)` | 近白 | 侧栏操作按钮悬停 |
| `--color-sidebar-text` | `#344054` | `rgb(52, 64, 84)` | 深灰蓝 | 侧栏菜单文字 |
| `--color-sidebar-text-muted` | `#667085` | `rgb(102, 112, 133)` | 中灰蓝 | 侧栏分组标题、次要文字 |
| `--color-sidebar-active-text` | `#101828` | `rgb(16, 24, 40)` | 近黑 | 侧栏选中项文字 |
| `--color-sidebar-active-primary` | `#E2E5E9` | `rgb(226, 229, 233)` | 浅中灰 | 用户端：一级菜单选中底色 |
| `--color-sidebar-active-secondary` | `#ECEFF2` | `rgb(236, 239, 242)` | 浅灰 | 用户端：二级菜单 / 会话列表选中、会话悬停底色 |
| `--color-sidebar-active-bg` | `#FFFFFF` | `rgb(255, 255, 255)` | 纯白 | 管理端：一级菜单选中卡片底色 |
| `--color-sidebar-accent` | `#2563EB` | `rgb(37, 99, 235)` | 标准蓝 | 管理端：选中卡片左侧的蓝色指示条 |
| `--color-sidebar-active-strong` | `#1D4ED8` | `rgb(29, 78, 216)` | 深蓝 | 管理端：折叠侧栏选中图标颜色 |
| `--color-sidebar-active-structural` | `#E2ECFF` | `rgb(226, 236, 255)` | 浅天蓝 | 管理端：二级菜单选中底色 |
| `--color-sidebar-active-structural-hover` | `#D8E6FF` | `rgb(216, 230, 255)` | 浅天蓝（略深） | 管理端：二级菜单选中项悬停 |
| `--color-sidebar-active-structural-text` | `#1D4ED8` | `rgb(29, 78, 216)` | 深蓝 | 管理端：二级菜单选中文字 |
| `--color-sidebar-active-soft` | `#E2ECFF` | `rgb(226, 236, 255)` | 浅天蓝 | 头像菜单、语言切换、搜索菜单的选中项 |
| `--color-sidebar-active-soft-hover` | `#D8E6FF` | `rgb(216, 230, 255)` | 浅天蓝（略深） | 上述选中项悬停 |
| `--color-sidebar-border` | `#E5E7EB` | `rgb(229, 231, 235)` | 浅灰 | 侧栏边框 |
| `--color-sidebar-border-hover` | `#D0D5DD` | `rgb(208, 213, 221)` | 中浅灰 | 侧栏边框悬停 |
| `--color-sidebar-border-subtle` | `#EAECF0` | `rgb(234, 236, 240)` | 更浅灰 | 侧栏内部分隔线 |

## 7. 阴影

| Token | 值 | 主要用途 |
| --- | --- | --- |
| `--shadow-panel` | `0 16px 48px rgba(17, 24, 39, 0.08)` | 弹窗、浮层面板 |
| `--shadow-panel-subtle` | `0 1px 2px rgba(16, 24, 40, 0.05)` | 卡片轻阴影 |
| `--shadow-control` | `0 1px 2px rgba(16, 24, 40, 0.04)` | 输入框、按钮等控件 |
| `--shadow-sidebar-active` | `0 1px 2px rgba(16, 24, 40, 0.06)` | 管理端侧栏选中卡片 |

## 附：第三方组件映射色

Markdown 渲染组件（streamdown 的 mermaid 工具栏、下载菜单、全屏查看器等）使用 shadcn 语义色，`global.css` 中的 `@theme inline` 把它们映射到 Hub 色板。业务代码不使用这组命名，设计稿中可以忽略。

| Token | HEX | RGB | 对应 Hub 色 |
| --- | --- | --- | --- |
| `--color-background` | `#FFFFFF` | `rgb(255, 255, 255)` | 面板白 |
| `--color-foreground` | `#101828` | `rgb(16, 24, 40)` | 近黑文字 |
| `--color-border` | `#D6DBE4` | `rgb(214, 219, 228)` | 通用边框 |
| `--color-muted` | `#F2F4F7` | `rgb(242, 244, 247)` | 浅灰底 |
| `--color-muted-foreground` | `#667085` | `rgb(102, 112, 133)` | 次要文字 |
| `--color-primary` | `#195BFF` | `rgb(25, 91, 255)` | 品牌主色 |
| `--color-primary-foreground` | `#FFFFFF` | `rgb(255, 255, 255)` | 主色上的文字 |
| `--color-sidebar` | `#F7F7F8` | `rgb(247, 247, 248)` | 侧栏浅灰 |
