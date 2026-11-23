# 可用性与外部标准

这些条款来自公开的设计与无障碍规范，是本规范中可读性、无障碍、错误提示等条目的依据。项目自己的硬性要求（如 Z-API 的 `AGENTS.md`）与之冲突时以项目为准。整理于 2026-11-02。

## 无障碍（WCAG 2.2 AA）

- **对比度**：普通文字 ≥ 4.5:1；大字（≥ 18.66px 粗体或 ≥ 24px）≥ 3:1；控件边界、图标、焦点指示 ≥ 3:1。
- **点击区（2.5.8）**：可点击目标至少 24×24 CSS 像素，或周围留出足够间距。表格行内的小图标按钮用 `size-7`（28px）。
- **焦点不被遮挡（2.4.11）**：获得焦点的元素不能被吸顶表头、固定页脚或浮层完全挡住。局部滚动区要让焦点元素滚入可见范围。
- **焦点可见**：不要去掉 `outline` 而不给替代样式；输入框、按钮、链接都需要清晰的 `focus-visible` 样式。
- **弹窗焦点**：打开时焦点进入弹窗，Tab 键在弹窗内循环，关闭后焦点回到触发按钮。
- **不只靠颜色**：状态同时用文字或图标表达。

## 弹窗使用（NN/g）

- 弹窗适合“用户主动发起的一件完整任务”和“防止严重错误的确认”。不要用系统主动弹出的弹窗打断用户。
- 内容量非常大、需要长时间停留的任务，考虑分区（左侧分区栏）而不是在一个弹窗里无限加字段。
- 取舍：为了“同一件事在一处完成”，编辑、导入、配置一律用弹窗；但不在用户没有操作时自动弹窗（公告除外，且只弹一次）。

## 错误提示（NN/g 错误信息准则）

- **放在出错的地方附近**：字段错误放在字段下方，并给字段加红框；整体失败用页内提示条。
- **人话 + 具体**：说清发生了什么，不只给错误码；技术细节可以折叠在“详情”里。
- **给出解决办法**：例如“密钥已过期，请在账号编辑里重新授权”，并附上能直接操作的按钮。
- **不责怪用户**，不用“非法”“错误操作”这类词。
- 容易出错的输入（JSON、批量导入）实时校验并提示。

## 空状态

- 说明为什么是空的（还没有数据 / 筛选没有结果 / 没有权限）。
- 给出下一步：“添加账号”“清除筛选”。
- 尺寸与有数据时相同（避免加载完成时布局跳动）。

## 数据表格

- 表格适合“行”形态的数据，比卡片信息密度高、更好扫读；后台列表默认用表格，卡片只用于少量、视觉化的条目。
- 密度：内部运营工具可以偏紧凑（行高 44–56）。如果以后用户反映太挤，可以加“紧凑 / 标准”切换并记住选择，而不是改默认值。
- 首列放最能识别该行的信息（名称 + ID），操作列固定在最右。

## 中文排版

- 中文正文行高 1.5–1.75 较舒适（Adobe Spectrum 对中日韩文字用 1.5 倍；百度移动端测试接受范围 1.5–1.7）。界面短文本可以 1.4–1.5，连续阅读段落用 1.6–1.75。
- 中文不加字距；避免标点出现在行首（浏览器默认的 `line-break: auto` 基本够用）。
- 字号按固定刻度，不随意新增中间值。

## 动效

- 时长：小元素 100–200ms，弹层 200–300ms；常用标准缓动 `cubic-bezier(0.2, 0, 0, 1)`（Material 3 标准曲线）。
- `prefers-reduced-motion: reduce` 时去掉位移和缩放，必要时只保留短暂淡入淡出。

## 出处

- [What's New in WCAG 2.2 — W3C](https://www.w3.org/WAI/standards-guidelines/wcag/new-in-22/)
- [WCAG 2.2 in language I can understand](https://tempertemper.net/blog/wcag-2-2-in-language-i-can-understand)
- [Modal & Nonmodal Dialogs — NN/g](https://nngroup.com/articles/modal-nonmodal-dialog/)
- [Modal dialogs — NZ Government Web Accessibility Guidance](https://govtnz.github.io/web-a11y-guidance/wct/modal-dialogs)
- [Error-Message Guidelines — NN/g](https://www.nngroup.com/articles/error-message-guidelines/)
- [Error Messages Scoring Rubric — NN/g](https://www.nngroup.com/articles/error-messages-scoring-rubric/)
- [Data table UI design reference guide — Setproduct](https://www.setproduct.com/blog/data-table-ui-design)
- [Admin Dashboard Design — AdminLTE](https://adminlte.io/blog/admin-dashboard-design/)
- [Typography — Adobe Spectrum](https://spectrum.adobe.com/page/typography)
- [typo.css（中文排版）](https://docsearch.algolia.com/mcp/docs/repo/sofish/typo.css)
- [Material Design 3 motion tokens（二手整理）](https://matraic.github.io/m3e/styles/motion.html)
