# AI 客服机器人：产品重新规划

> 目标：从“读最近几条消息的自动回复”升级为“像真人客服一样说话、记得住人、会用工具、知道网站实时情况、知识库会自己成长”的客服 Agent。
> 现有底座（Webhook 幂等、inbox/outbox 任务、pgvector、撤回、审计）保留，只在其上新增“大脑”。

## 1. 现状诊断

| 领域 | 现状（代码位置） | 问题 |
|---|---|---|
| 上下文 | `workers.go processInbox`：取会话最近 N 条（按条数） | 群聊中多人消息混杂；无摘要、无长期记忆；长消息可能超 token |
| 检索 | 只用当前这句话做向量检索 | “那这个呢？”“还是不行”检索不到 |
| 能力 | 单次模型调用；`tool_definitions` / `tool_calls` 表闲置 | 不能查价格、查状态、转人工 |
| 风格 | 系统提示词一句“准确、简洁” | 回复长、像机器人，照搬资料 |
| 实时信息 | 无 | 公告、分组状态、模型价格答不上或答错 |
| 知识 | 手动上传 TXT/MD，固定切块 | 无反馈、无缺口统计、真人客服答案没被利用 |

## 2. 目标架构

```text
QQ Webhook → 统一入口（不变）
   ↓
① 理解：识别发言人 / 线程 → 问题改写（结合上下文生成独立问题）
   ↓
② 上下文装配（按 token 预算）
   L0 工作记忆：本线程近期往来（群聊按发言人过滤 + 官方客服消息）
   L1 会话摘要：超出预算的历史滚动摘要
   L2 用户记忆：长期事实/偏好/历史问题（常驻 + 向量召回）
   L3 知识：FAQ 条目 + 文档块检索（用改写后的问题）
   L4 实时快照：最新公告 + 异常分组（来自 new-api）
   ↓
③ 行动：工具调用循环（最多 N 步）
   内置工具：search_knowledge / get_model_price / get_group_status / get_announcements
             remember_user_fact / handoff_to_human
   自定义工具：管理端配置的 HTTP 接口
   技能（Skill）：何时使用 + 专属指令 + 绑定工具
   ↓
④ 风格后处理：去 Markdown 装饰、超长按段拆成多条（像真人分段发）
   ↓
统一出口（不变）
   ↓
⑤ 学习（异步）：对话评估 → 用户记忆抽取 → 隐式反馈 → 知识缺口 → 候选知识 → 审核 → FAQ 入库
```

## 3. 回复风格（解决“太长、太像机器人”）

1. **人设与风格提示词**（全局设置，可按会话追加业务提示词）。默认模板：先给结论；一般 1～3 句；不用标题/列表/加粗；口语化；不说“作为 AI / 希望对您有帮助”；一次只答被问到的点；信息不足先反问一句；需要细节时最后问“要详细步骤吗？”。
2. **示例对话（few-shot）**：从官方客服真实回答中挑选，放进提示词，最能让语气像自家客服。
3. **硬性兜底**：`max_tokens` 上限；出口前清理标题、加粗等 Markdown 装饰（保留代码块，API 站点用户常需要配置示例）；超过设定字数按段落拆成最多 3 条依次发送（QQ msg_seq 递增）。
4. **闭环**：管理员可给回复打“太长 / 生硬 / 错误 / 好”标签，进入反馈数据。

## 4. 网站实时数据（new-api）

原则：**实时数据不进向量库**，否则价格一改 AI 就报错价。

| 数据 | new-api 接口 | 刷新 | 用法 |
|---|---|---|---|
| 公告 | `GET /api/notice`（Markdown）、`GET /api/status` 中的 `announcements` | 5 分钟 | 内容变化即同步为知识库 FAQ 条目（来源=公告）；最新 1～2 条摘要常驻提示词 |
| 分组 / 状态 | `GET /api/uptime/status`（Uptime Kuma 监控分组）、`/api/pricing` 中的 `group_ratio` / `usable_group` | 1 分钟 | 仅异常项常驻提示词；`get_group_status` 工具查询 |
| 模型价格 | `GET /api/pricing` | 5 分钟 | **只能通过 `get_model_price` 工具查询**；模糊匹配模型名；返回输入/输出单价与分组倍率及数据时间 |

价格换算（new-api 约定）：按量计费模型 `输入 $/1M tokens = model_ratio × 2`，`输出 = 输入 × completion_ratio`，再乘分组倍率；按次计费模型 `model_price × 分组倍率`（美元/次）。

- 数据源配置：站点地址、可选系统访问令牌（`Authorization` + `New-Api-User`）、刷新间隔。
- 拉取失败继续使用上一次成功数据，并在提示词中标注“可能不是最新”。
- 分组状态由正常变为异常时，在管理端概览中提醒。

## 5. 记忆体系

- **工作记忆**：私聊取整个会话；群聊取“该用户的消息 + 机器人回复该用户的消息 + 官方客服消息”，按 token 预算从新到旧装入；图片消息以“[图片]”占位。
- **会话摘要**：线程中超出窗口的历史由辅助模型滚动摘要（`conversation_summaries`，群聊按发言人分线程）。
- **用户记忆**：每轮对话后异步抽取（设备、使用的分组、令牌问题、偏好等），存 `user_memories`，带置信度、来源消息、过期时间；回复时取常驻记忆 + 向量召回。
- **身份说明**：QQ 官方机器人在群聊（member_openid）与私聊（user_openid）中的 ID 不同，记忆按“机器人 + 用户 ID”隔离，不做自动跨场景合并。
- 管理端可查看、编辑、删除某个用户的记忆。

## 6. 技能与工具

- **工具调用循环**：Chat Completions 与 Responses API 都支持 function calling；最多 4 步，超过回复截止时间则直接用已有信息回答。
- **技能**（`skills` 表）：名称、何时使用、专属指令、绑定工具、启用状态。启用的技能指令拼入系统提示词，绑定的工具开放给模型。内置默认技能：价格咨询、故障排查、转人工。
- **自定义 HTTP 工具**（复用 `tool_definitions`）：方法、URL、请求头（加密存储）、参数 JSON Schema、超时；响应截断到 4KB。
- **转人工**：`handoff_to_human` 创建工单（`handoffs`），该用户在设定时间内机器人静默；管理端“人工工作台”可直接回复（在 QQ 被动回复窗口内）并结单。所有工具调用写入 `tool_calls`。

## 7. 知识自动成长（飞轮）

**信号来源**
1. **官方客服回答**（价值最高）：在会话成员中标记“官方客服”。其群内发言进入任务，由辅助模型判断回答了哪条用户问题，生成候选 FAQ。
2. **管理员人工回复 / 纠错**：在人工工作台回复或在消息链路中写出正确答案 → 候选 FAQ（可选择直接发布）。
3. **隐式反馈**：每轮评估时判断用户追问是否表明上一条回答没帮上（重复问、说“不对/没用”、要求人工）→ 记负反馈。
4. **知识缺口**：评估认为“未能回答”的问题 → 向量聚类到 `knowledge_gaps`，统计次数，供管理员补答。

**加工与审核**
- 候选（`knowledge_candidates`）进入审核队列：通过 / 编辑后通过 / 驳回 / 合并到已有条目。
- 通过后成为结构化 FAQ 条目（`knowledge_entries`：标准问、相似问、答案、来源、版本），自动建立向量索引，与文档块一起检索。
- 条目跟踪命中次数、好评、差评；差评率高时标记“需复核”。
- **安全**：默认不自动发布任何来自普通用户的内容；仅“官方客服来源且置信度 ≥ 阈值”可开启自动发布。

## 8. 评测（防止越改越差）

- 评测用例（`eval_cases`）：问题 + 期望要点；可从已发布 FAQ 一键生成。
- 评测运行（`eval_runs`）：选择模型配置，以“试运行”模式跑完整 Agent 流程（不发送 QQ），由辅助模型按要点打分，记录长度与是否使用工具。用于比较提示词 / 模型 / 风格调整前后的效果。

## 9. 数据结构（迁移 009）

- `bots`：不变；全局 Agent 设置存 `system_settings.agent_settings`
- `conversation_members.is_staff`
- `conversation_summaries(conversation_id, scope_key, summary, covered_until, …)`
- `user_memories(bot_id, platform_user_id, kind, content, embedding, confidence, pinned, source_message_id, expires_at, …)`
- `agent_jobs(kind, payload, dedupe_key, status, attempts, next_attempt_at, …)`：记忆抽取 / 摘要 / 评估 / 客服答案挖掘 / FAQ 索引
- `skills`；`tool_definitions` 增加 `kind/parameters/…`
- `handoffs(conversation_id, platform_user_id, message_id, reason, status, …)`
- `message_feedback(message_id, source, rating, comment, corrected_answer, …)`
- `knowledge_entries`、`knowledge_candidates`、`knowledge_gaps`；`knowledge_chunks.entry_id`
- `live_data_snapshots(source, data, fetched_at, error)`
- `eval_cases`、`eval_runs`、`eval_results`
- `agent_runs` 增加 `rewritten_query / steps / evaluation`

## 10. 管理端

| 页面 | 内容 |
|---|---|
| 智能体 | 人设与风格、示例对话、上下文预算、记忆与学习开关、辅助模型、new-api 数据源与实时快照预览 |
| 技能 | 技能、内置工具、自定义 HTTP 工具 |
| 知识运营 | 待审核候选、FAQ 条目、知识缺口、反馈记录 |
| 人工台 | 转人工工单、人工回复、结单 |
| 评测 | 用例、运行、结果对比 |
| 会话（增强） | 成员列表标记官方客服、查看/编辑用户记忆 |
| 消息（增强） | Agent 步骤（改写、工具调用）、打标签/纠错、人工回复 |

## 11. 交付分期

| 阶段 | 内容 |
|---|---|
| P0 | 流水线重构；token 预算上下文；群聊线程过滤；问题改写；滚动摘要；人设风格与拆条；实时快照常驻 |
| P1 | 工具调用循环；内置工具（价格/状态/公告/知识/记忆/转人工）；用户记忆；技能；人工工作台 |
| P2 | 官方客服答案挖掘；反馈；候选审核；FAQ 条目；知识缺口 |
| P3 | 自定义 HTTP 工具；评测用例与运行 |

本次按 P0～P3 一并实现，数据库迁移向后兼容（只新增表 / 列），可直接在现有生产库上升级。
