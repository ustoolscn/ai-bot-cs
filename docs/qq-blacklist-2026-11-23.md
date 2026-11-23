# QQ 群黑名单与自动移除：后端交接

任务 `7FAEE764-061E-464F-9BEB-2B644B254DD5`，目录 `/Users/luji/Documents/coding/ai-bot-cs`，基于 main `3ea7391`。本次只提交后端、测试和本文档，不部署，不提交 `.claude/` 或 `docs/production-deployment-2026-11-23.md`。

## 官方能力核实

**现在 QQ 群官方机器人已支持踢人。** 官方 2026-09-03 变更记录新增“群成员批量移除”，当前接口页更新于 2026-11-16。不能再沿用“只有频道可以踢人”的旧判断。本次调用官方群 API：

`POST /v2/groups/{group_openid}/batch_remove_members`

body `{"member_openids":["目标member_openid"]}`，成功必须明确返回 `{"remove_members_result":"success"}`，单次最多20人，30 QPM。本系统每次只移除当前命中的一人。

官方说明两个权限开关：群主需给机器人打开“移除群成员”，机器人 WEB 管理端也需打开对应开关。错误码分别为 40103012、40103011。是否已授权只能在实际调用时确认，本次未连接生产 QQ 实测，遇到无权限、接口受限、网络异常或异常返回均记录失败并提醒人工。

**不能直接按真实 QQ 号匹配。** 官方群消息和入群事件提供成员 openid，不提供真实 QQ 号，也没有在这些事件中提供 QQ号到openid 的转换。管理端应从机器人已见过的成员中选择，用 member_openid 精确匹配；昵称只供展示，不按昵称匹配。不同机器人 ID 隔离。单群黑名单只影响该群；机器人范围黑名单只匹配同机器人收到的完全相同成员 ID，不臆造跨身份映射。

官方来源（本次已读取页面正文）：
- 变更记录：https://bot.q.qq.com/wiki/develop/api-v2/changelog.html （20260903：群成员批量移除、群黑名单）
- 移除接口：https://bot.q.qq.com/wiki/develop/api-v2/autogen/api/v2_groups_group_openid_batch_remove_members.post.html
- 群全量消息：https://bot.q.qq.com/wiki/develop/api-v2/autogen/event/group_message_create.html
- 群成员加入：https://bot.q.qq.com/wiki/develop/api-v2/autogen/event/group_member_add.html

只用了官方 HTTP API，未接第三方 QQ 协议或个人账号登录。也没有把本地黑名单同步成 QQ 平台“禁止再次入群”的黑名单；本次实现是发现后再次踢出。解除本地黑名单不自动邀请成员回群。

## 实际行为

- 新建黑名单默认 `autoKick=true`，立即为已知仍在群内的匹配成员排队移除；之后收到群消息或 GROUP_MEMBER_ADD 事件也会检查。支持 `autoKick=false` 只做本地屏蔽。
- 依赖实际推送：全群发言检测需 QQ 后台开启接收所有消息；入群检测需订阅 GROUP_MEMBER_EVENT (1<<24)。未收到的事件无法检测，不轮询 QQ 成员目录。
- 黑名单入站消息继续留库供审计，但标记 contextExcluded，不创建 AI 回复或客服学习任务。已排队 AI 在处理前、模型返回后及正式发送前再次检查；已提交到外部模型或已进入网络发送的请求无法追溯撤销。
- 添加时排除该范围的既有成员消息及其对应机器人回复，清理相关群摘要和语气画像，归档该范围来源的个人记忆。历史记录不会物理删除；解除黑名单不自动恢复已排除消息/记忆。既有已审核知识条目不自动撤销，管理员可在 LearningView 审查来源。
- 后台评价、记忆保存和客服挖掘均跳过已排除消息；客服挖掘的前文也排除屏蔽内容。语气注入校验样本未被排除。
- 踢出通过后台持久任务执行，记录每群动作状态和平台错误。明确成功才标记 kicked 并把本地成员 active=false；不能把 HTTP 200 但无成功标识误报成功。
- 平台调用失败时本地屏蔽继续生效，生成系统提醒，提醒含成员、群、错误及 requiresManualAction=true。同一动作未确认提醒不重复生成。失败后的自动触发有5分钟冷却；过后下一次发言/入群才重试。管理员重新 POST 同一条黑名单可立即重新排队仍在群内的失败成员。
- 重复 webhook 事件幂等；待执行动作去重。删除黑名单会连带删除动作，未执行任务再检查时跳过。原有旧 blocked_users 记录保持 autoKick=false，避免升级时突然踢出旧屏蔽名单。

## Claude 前端接口

全部是现有登录态的 `/api` 接口，沿用 `{data: ...}` 包装。

### 查询

`GET /api/blocked-users?botId=<可选>&conversationId=<可选>`

返回 data 数组，最多500条。conversationId 过滤时包括该群黑名单及机器人范围名单（若也提供 botId 则限该机器人）。每条：

```json
{
 "id":"黑名单uuid",
 "botId":"机器人内部uuid",
 "botName":"机器人名称",
 "conversationId":"群内部uuid或null（机器人范围）",
 "userId":"member_openid",
 "displayName":"最近见过的昵称或空串",
 "reason":"原因",
 "autoKick":true,
 "createdAt":"时间",
 "actions":[{
   "id":"动作uuid",
   "conversationId":"群内部uuid",
   "status":"pending|kicked|failed|disabled",
   "attempts":1,
   "error":"平台错误或空串",
   "lastAttemptAt":"时间或null",
   "updatedAt":"时间"
 }]
}
```

status=pending 表示排队/正在调用，不表示已经移除。kicked 才是平台明确成功。

### 新增/修改同一身份记录

`POST /api/blocked-users`

```json
{
 "botId":"必填，机器人内部uuid",
 "conversationId":"可选，群内部uuid；省略或空串为该机器人范围",
 "userId":"必填，从成员列表选择的member_openid",
 "reason":"可选，最多1000字",
 "autoKick":true
}
```

同一 botId + conversationId + userId 重复提交会更新原因和 autoKick，返回同一 id。autoKick 省略默认为 true。设置 false 会停用已有动作。

成功 HTTP 200，data `{"id":"uuid","autoKick":true,"identityType":"member_openid"}`。成功只代表本地规则保存/任务排队，不能显示“已踢出”。未知成员（包括直接填 QQ 号）、群不属于机器人等返回400 unknown_member；参数无效返回400 invalid_request；数据库失败500。

### 删除

`DELETE /api/blocked-users/:id`

成功 data `{"deleted":true}`。不存在404；ID格式错误400。机器人范围和群范围记录可并存，解除一条后若还有匹配记录，仍然屏蔽；界面需提示范围。

### 成员列表扩展

现有 `GET /api/conversations/:id/members` 每项新增：

- `isBlocked: boolean`：机器人范围或本群范围任一匹配即为true。
- `blockedUserIds: string[]`：命中的黑名单记录ID，解除时可用于删除。

原有 userId/displayName 等字段不变。UI 应显示“选择群成员（openid）”，不要显示“填写 QQ 号”。

### 失败提醒

复用 `GET /api/system/alerts` 和原有确认提醒接口。新增 kind=`blacklist_kick_failed`，detail 含 actionId/botId/conversationId/userId/error/requiresManualAction。可引导管理员检查群主、WEB端的“移除群成员”开关或手动处理。

## 文件与验证

新增：
- `backend/internal/app/blacklist.go`：增删查、屏蔽历史、任务与动作状态、失败提醒。
- `backend/internal/db/migrations/011_blacklist_kicks.sql`：幂等扩展 auto_kick 和动作表。
- `backend/internal/qq/moderation.go`：官方群移除客户端。
- `backend/internal/app/blacklist_integration_test.go`、`backend/internal/qq/moderation_test.go`。

修改：app.go（路由、消息与入群检测）；ops.go（成员黑名单状态）；agent_pipeline.go、workers.go（异步回复检查）；jobs.go（移除任务、学习检查及上下文过滤）；memory.go（阻止记忆写入）；staff_learning.go（语气样本有效性检查）。

本机 Go 工具链 `/tmp/ai-bot-toolchain/go/bin/go`，本地 PostgreSQL+pgvector，运行 `GIN_MODE=release TEST_DATABASE_URL=... go test ./...`。测试覆盖官方成功/权限拒绝/200业务失败/无成功字段、API验证与幂等、单群隔离、旧待处理回复拦截、不进上下文/记忆/学习、入群重触发、重复事件、失败提醒与冷却、删除后不再踢。结果日志 `/tmp/blacklist-test.log`。不涉及生产环境。
