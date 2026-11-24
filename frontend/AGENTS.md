# Prototype Instructions

Run the local server yourself and open the preview in the in-app browser. Do not give the user server-start instructions when you can run it.

Before making substantial visual changes, use the Product Design plugin's `get-context` skill when the visual source is unclear or no longer matches the current goal. When the user gives durable prototype-specific design feedback, preferences, or decisions, record them in `AGENTS.md`.

When implementing from a selected generated mock, treat that image as the source of truth for layout, component anatomy, density, spacing, color, typography, visible content, and hierarchy.

## Confirmed visual direction（2026-11-23 第二版，用户要求“温暖、可爱、有设计感，用 SVG/JS 动画展示进度”）

- 信息架构见 `docs/ui-redesign-proposal.md`：左侧分组导航（工作台 / 运行记录 / 客服配置 / 接入设置），导航配置在 `src/router/nav.ts`。
- 视觉：奶油色背景 + 珊瑚渐变主色（#f07a5a → #ec5f78），辅助色薄荷、奶黄、薰衣草、天蓝；大圆角（卡片 16–24px、按钮胶囊形）；标题和数字用圆体（`--font-round`）。
- 外壳：桌面端侧栏为悬浮圆角面板，底部有吉祥物提示卡。
- 吉祥物“小Q”（`src/components/fx/Mascot.vue`，纯 SVG + CSS 动画，有 happy / sleepy / thinking / wave 四种表情），用于品牌标识、空状态、概览问候、登录页。
- 动效组件在 `src/components/fx/`：`CountUp` 数字滚动、`ProgressRing` 渐变圆环、`WaveProgress` 水波进度、`PipelineFlow` 消息流水线；数值过渡用 `src/composables/useTween.ts`（requestAnimationFrame）。所有动画都要尊重 `prefers-reduced-motion`。
- 颜色和形状统一用 `src/styles/index.css` 顶部的 CSS 变量；Element Plus 主题变量也在那里覆盖。正文 14px、次要 13px。
- 空状态写清下一步；后端断开时只在顶部显示一条提示（`src/utils/connection.ts`）。
- 智能体设置：一页滚动 + 左侧目录 + 底部保存条；有未保存修改时禁止“立即同步”网站数据。
- 客服配置以 QQ 机器人为入口。机器人下可添加多个 AI 客服，定时任务在独立页面配置北京时间、内容指令、格式以及全部或指定群，可立即测试发送。每个客服独立选择对话模型并指定智能体，智能体引用在性格模块维护的可复用预设。

- 视觉（2026-11-23 第三版，用户指定参照 QQ 机器人管理后台 q.qq.com/qqbot/dashboard）：系统字体、白色侧栏 + #f5f5f7 内容区、浅蓝胶囊选中态、主色 #0077e6、16px 圆角面板，变量见 `src/styles/index.css`，取代上面的奶油珊瑚方案。
- 知识库按库分组：先选知识库，再看它的文档、问答、待审核；没答上的问题和反馈为所有库共用。群聊和机器人都可以删除。
- 文档、知识问答、待审核、知识缺口和反馈统一在知识库管理；记忆使用独立导航。用户记忆先选择机器人对应的群聊或私聊，再选择用户查看及管理记忆。
- 消息流程按实际执行顺序展示本步和累计用时，累计从后台接收开始。分段回复以全部投递结束为总用时。
- 环节用时统一用带图标的条形图（`StageBars`）：概览平均按流程先后排；单条消息用瀑布图（起点=开始时刻），悬停看说明。Agent 步骤为可展开的单行条形，不堆文字。
- 消息状态：判断无需回复的显示“无需回复”（skipped），不显示“待处理”。上下文按发言人昵称标注。反馈有已读状态，角标只计未读差评。
- 全站重做（2026-11-24，用户：“每一个地方都重新设计”“动效多加一点”“弹窗高度超过屏幕”）：统一刻度和页头结构（标题与操作同一行居中）；全局细滚动条；所有弹窗垂直居中、最高为视口减 48px，只滚内容区；路由切换淡入、页面分区依次进入、表格行滑入、按钮按下收缩。规则同步写入 `.claude/skills/ui-taste`。
