# QQ 群回复与客服学习后端交接

任务 7B9A3AD2-8C91-410A-A3E9-F595B18D67C4，工作目录 `/Users/luji/Documents/coding/ai-bot-cs`。未部署、未 commit/push；保留原有未提交改动，未修改前端。

## QQ 官方能力核实（2026-11-23）

- 群历史：当前 QQ 官方 v2 群接口没有列出拉取群历史消息 API，不能补拉机器人入群前或服务离线期间未收到的消息。腾讯云 IM 历史接口不是这个产品，不能混用。需在 QQ 后台开启“接收所有消息”，从 GROUP_MESSAGE_CREATE 实时保存；未开启时只能拿到机器人实际收到的 @ 消息。系统已有存储能力，本次直接复用。
- 原生引用：新版官方文档已经支持 message_reference，旧 GitHub 文档的“暂未支持”已过时。message_reference.message_id 使用入站 message_scene.ext 中 msg_idx= 后的索引；msg_id 仍用 d.id，两者不同。缺少引用索引的旧消息回退为摘录问题前 120 字。
- @：群聊 Markdown 支持 `<qqbot-at-user id="member_openid" />`；群普通文本不支持 @单个用户。正常 AI 回复走 Markdown；平台拒绝 Markdown 时沿用原有纯文本降级，展示 @昵称文字，无法保证真正的 @通知。没有在线 QQ 账号实测，本次是官方文档核实和 HTTP mock 测试。

官方来源：
- https://bot.q.qq.com/wiki/develop/api-v2/server-inter/message/overview.html
- https://bot.q.qq.com/wiki/develop/api-v2/autogen/event/group_message_create.html
- https://bot.q.qq.com/wiki/develop/api-v2/autogen/api/v2_groups_group_openid_messages.post.html
- https://bot.q.qq.com/wiki/develop/api-v2/server-inter/message/trans/text-chain.html

## 行为

1. 群上下文不再按提问者过滤，即使旧 groupThreadMode=true，也使用所有群成员的近期消息和共享摘要。仍受 conversation.contextLimit 和 token 预算限制。个人记忆继续按 botId + senderID 读取；没有变成全群共享记忆。消息保留昵称，缺昵称时使用发送者 ID。
2. persistInbound 不再创建 PROCESSING_ACK/ark_ack。迁移将旧待发占位任务过期，worker 也拦截旧占位。AI 正式回复 msg_seq 从 1 开始。旧已发卡片的兼容撤回代码保留用于清理旧数据，新消息没有占位和撤回流程。
3. AI 群回复在发送层加 @，不依赖模型自行生成；从原始事件解析引用索引，保留被动回复 token。未拿到索引时加文本摘录。私聊不增加群引用或 @。
4. 复用 conversation_members.is_staff，可指定多位客服。设置 isStaff=true 时自动排队历史学习；也支持手动重新学习。从当前指定客服已存消息中取最近 200 条非空、未排除的消息提炼群语气，记录样本 ID。语气自动用于该群之后的 AI 回复。更改客服配置会清理旧语气，且注入时再次比对客服集合，避免过期结果影响回复。知识按消息异步挖掘，复用 source=staff 的候选、审核、发布、向量索引流程。历史学习是管理员显式操作，知识挖掘不受 StaffMiningEnabled 开关限制；后续实时客服消息仍遵循原有 StaffMiningEnabled。知识是否自动发布沿用 AutoPublishStaff 等现有设置，否则待管理员审核。发布后的知识库必须绑定到该群，才会被该群检索。

知识候选以原消息 ID 去重，实时学习和历史学习并发时也不会重复写入同一来源候选。辅助模型不可用、无历史样本等错误可从任务状态查看，后台最多尝试 3 次。历史学习主任务完成表示语气已提炼、知识任务已排队；知识候选可能稍后出现。

## 前端接口（均带现有登录 cookie，响应使用现有 data 包装）

### 指定客服（已有接口）

- GET `/api/conversations/:id/members`：现有成员列表，字段 userId/displayName/isStaff/messageCount 等。列表只包含实际见过的成员，不是 QQ 全群成员目录。
- PUT `/api/conversations/:id/members/:userId`，body `{"isStaff":true}` 或 false；响应 data `{"updated":true}`。多位客服分别调用；true 自动排队学习，false 清理现有语气。批量设置完可再调用下面 POST 对最终集合重新学习。

### 新增：启动历史学习

POST `/api/conversations/:id/staff-learning`，无请求字段（空 body 或 `{}`）。

成功 data：`{"jobId":"uuid","source":"stored_group_messages","maxMessages":200}`。
相同群未完成任务复用 jobId；完成后允许重学。404 群不存在；400 no_staff 未指定客服；500 创建任务失败。无历史样本会在后台任务中显示错误。

### 新增：查看语气、样本和状态

GET `/api/conversations/:id/staff-learning`，无请求字段。

data 字段：

```json
{
  "staff": [{"userId":"member_openid","displayName":"客服昵称","availableMessages":125}],
  "style":"语气自然亲切，先说结论，再补充一步操作。",
  "sampleCount":125,
  "sourceMessageIds":["内部消息uuid"],
  "learnedAt":"RFC3339时间或null",
  "source":"stored_group_messages",
  "job":{"id":"uuid","status":"pending|processing|completed|failed","attempts":1,"error":"","updatedAt":"RFC3339时间"}
}
```

没有任务时 job=null；没有学习结果时 style=""、sampleCount=0、sourceMessageIds=[]、learnedAt=null。建议轮询直到 completed/failed，并清楚显示“仅学习系统已保存的最近 200 条客服消息，无法补拉 QQ 历史”。样本 ID 可链接现有 `/api/messages/:id` 查看证据。

### 查看/审核知识（复用，补充群过滤）

GET `/api/knowledge/candidates?status=pending&conversationId=<群内部uuid>`，status 可用 all。新增可选 conversationId 过滤，返回格式保持原有列表；本次客服来源 source=staff，evidence 包含 messageId/conversationId/content/context。候选会包含审核状态、question/answer/aliases/confidence/entryId。现有 approve/reject 接口和 LearningView 可直接复用：

- POST `/api/knowledge/candidates/:id/approve` body `{knowledgeBaseId,question,aliases,answer,note}`，成功 data `{entryId}`。
- POST `/api/knowledge/candidates/:id/reject` body `{note}`。

## 改动文件

- 群上下文/语气注入：backend/internal/app/agent_pipeline.go。
- 停止占位、路由：backend/internal/app/app.go；旧待发拦截/回复元数据：workers.go。
- 客服管理/自动学习：ops.go；新增 staff_learning.go；任务调度/历史知识挖掘：jobs.go；候选群过滤：learning.go。
- QQ 引用与 @：backend/internal/domain/domain.go、backend/internal/qq/client.go、新增 reference.go。
- 数据迁移：backend/internal/db/migrations/010_staff_style.sql（幂等；启动自动执行）。
- 测试：app/agent_integration_test.go、app/app_integration_test.go、qq/client_test.go、新增 qq/reference_test.go。

## 验证

使用本机 `/tmp/ai-bot-toolchain/go/bin/go`，本地 PostgreSQL + pgvector，非生产服务器。
`GIN_MODE=release TEST_DATABASE_URL=... go test ./...` 全部通过（含数据库集成）。覆盖群内其他用户上下文、个人记忆、无占位发送/去重、客服历史语气与接口、知识候选/审核/索引、引用索引解析、Markdown @与原生引用 payload、人工接管和回复等现有回归测试。测试日志 `/tmp/qq-test.log`。QQ 实际展示仍需上线获授权后或沙箱账号联调。
