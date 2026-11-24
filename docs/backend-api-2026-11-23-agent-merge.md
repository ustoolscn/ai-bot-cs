# AI 客服后台接口（2026-11-23）

下列路径均相对 `/api`，沿用现有管理员鉴权和 `{data: ...}` 返回格式。

## AI 客服及实时数据

| 方法 | 路径 | 请求与结果 |
|---|---|---|
| GET | `/bots/:id/services` | 客服数组。每项含 `id,name,instructions,chatProfileId,simpleProfileId,decisionProfileId,settings` 及兼容字段 `agentId`。模型未单独指定时返回空串。 |
| POST、PUT | `/bots/:id/services`、`/bots/:id/services/:item` | 必填 `name`，其余上述字段可选。PUT 只覆盖提交的设置，模型 ID 空串清除选择。返回 `{id}`。新客服无需 agentId。 |
| GET、POST | `/bots/:id/services/:item/live-data`、`/bots/:id/services/:item/live-data/refresh` | 本客服网站的最近快照、手工刷新，结构沿用 `/agent/live-data`。 |
| GET、PUT | `/agent/settings` | 系统默认值，适用于没有单独配置的客服和未绑定客服的群。 |

`settings` 接受以下字段，列表接口返回合并默认值后的可编辑配置。

- 人设与示例使用 `stylePrompt`、`examples: [{question,answer}]`。客服名称兼作人设名称，职责在顶层 `instructions`。
- 技能和工具使用 `toolsEnabled,maxToolSteps,skillIds,toolNames`。技能按 ID、工具按名称选择，供给范围仍受系统技能和工具启用状态限制。`null` 沿用可用集合，`[]` 表示不选。知识库使用 `knowledgeBaseIds`，`null` 沿用群绑定，`[]` 关闭检索。
- 连续消息合并使用整数秒。`replyQuietSeconds` 是最后一条后的静默时间，默认 6 秒。`replyMaxWaitSeconds` 从第一条起算，默认 20 秒。两项范围均为 1–120，最长等待须大于或等于静默时间。
- 学习及人工使用 `evaluationEnabled,staffMiningEnabled,autoPublishStaff,autoPublishMinConfidence,learningKnowledgeBaseId,handoffPauseMinutes`。
- `liveData` 支持 `enabled,baseUrl,userId,injectSnapshot,priceNote,statusPageUrl,statusImageStyle`。`hasAccessToken` 只读。设置令牌使用**请求顶层**的 `liveDataAccessToken`；省略保留同站令牌，空串清除，更换网站请重新输入令牌。
- 原 `/personalities` 与 `/support-agents` 仍可读写旧模板，原客服与会话 ID 保留。新页面直接编辑客服。

## 公网地址与网页

- `GET/PUT /system/settings` 增加 `publicBaseUrl`，可单独提交该字段。空串清除公网地址。非空值须为 HTTP(S) 地址且没有查询串或片段。`PUBLIC_BASE_URL` 仅初始化已有配置中缺少的字段。
- `GET /bots` 每项新增 `callbackUrl`。有公网地址时返回绝对 URL，未配置时返回相对回调路径。
- 工具目录新增 `publish_html_page({html})`，返回 `{url,expiresInHours:168}`。链接使用现有公网 `/p/:id` 路由，页面经过静态过滤并在七天后失效。图片工具仍为 `render_html_image`。
- 报价工具返回以美元计的核对数据和提问语言对应的 `displayCurrency,displayQuotes`。未知倍率时不上报猜测价格。若配置状态页，两个状态查询工具会将页面截图作为同一条 QQ 回复的图片附件。

## 用户档案、事件与概览

| 方法 | 路径 | 请求与结果 |
|---|---|---|
| GET、PUT、DELETE | `/conversations/:id/user-profiles/:user` | 用户 ID 为平台用户 ID，路径参数需编码。PUT 提交 `{content}`。均返回 `{conversationId,userId,content,updatedAt}`。尚无档案或已清除时正文为空。 |
| GET | `/memories/directory` | 沿用现有结构，也列出仅有档案的用户，count 仅计事件。 |
| GET | `/memories?conversationId=…&userId=…` | 事件新增 `eventAt`。按既有 `POST /memories`、`PUT/DELETE /memories/:id` 管理，创建及修改可传 RFC 3339 格式的 `eventAt`。 |
| GET | `/system/overview` | 新增字段见下。 |

- `activeTasks[]` 包含 `id`（入站消息 ID）、`taskId,botId,bot,conversationId,conversation,userId,sender,question,stage,elapsedMs`。仅列出正在处理的入站回复；空闲时返回空数组。
- `queuedTasks,processingTasks` 分别是待处理和处理中收发队列数，不含处理回执，旧计数字段兼容保留。
- `knowledge` 含 `documentCount,entryCount,indexedCount,indexedRatio`。问答只统计已发布项目，比例为已就绪文档和问答占两者总数的比例（0–1，空库为 0）。
- `stageAverages[]` 包含 `label,averageMs,samples`，聚合最近 24 小时已完成计时的样本。包括排队、模型、工具及投递。

用户档案和事件沿用「机器人内的会话＋平台用户」边界。档案上限采用现有 token 估算器。提取由回复事务创建的后台任务执行；模型可新增、更新、删除事件或保持原样，管理员置顶的事件不会被自动改删。旧事件以创建时间作为缺少事件时间时的回退值。

## 客服生图

生图供应商配置在 `/api/model-profiles`，使用通用模型的 GET、POST、PUT、DELETE 接口。类型填 `kind="image"`，请求包含 `name,baseUrl,model,apiKey`，可选 `proxyId,enabled` 等原有字段。基础地址包含版本前缀，例如 `https://provider.example/v1`。后端追加 `/images/generations`，每次请求一张，兼容图片的 `b64_json` 和公网 HTTPS URL。

客服创建、修改及列表接口中，`imageProfileId` 与 `chatProfileId` 同级。选择启用的 image 模型后，按现有 `toolsEnabled`、`toolNames` 开启 `generate_image`。更新客服时省略 ID 保留选择，传空串清除。未选择或所选模型已停用时工具不可用。迁移将旧全局供应商及加密密钥保存为“原全局生图模型”，由管理员选择需要使用它的客服。

`POST /api/model-profiles/:id/test` 接收 `{"input":"画面描述"}`，使用所选代理真实请求一张图，可能产生费用。成功返回 `data.{kind,model,content,imageUrl,contentType,sizeBytes,latencyMs}`，其中 `kind="image"`，`content` 是测试成功说明。`imageUrl` 为可直接预览的图片 Data URL；测试图片不入消息记录。供应商错误返回 HTTP 502。

## 状态图与网站手动同步

- 每个客服的 `settings.liveData.statusImageStyle` 默认 `brief`。可选 `brief`（简报）、`table`（明细表）、`heatmap`（热力格）、`night`（夜间卡片）、`page`（截取 `statusPageUrl`）及 `off`（仅文字）。四种卡片读取已保存的小时统计与监测数据，固定模板离线渲染。
- `GET /api/bots/:id/services/:item/live-data` 读取快照，`POST /api/bots/:id/services/:item/live-data/refresh` 发起手动同步。全局 `/api/agent/live-data` 与 `/refresh` 同理。保存设置不联网。首次同步前没有快照，快照超过 24 小时或最近同步报错时，`possiblyStale=true`。
- 移除 `refreshSeconds`、`syncAnnouncements` 和 `announcementKnowledgeBaseId`。升级时清理来源为 `announcement` 的知识条目及其索引，普通人工条目保留。模型仍可通过快照和 `get_announcements` 阅读公告。

## 人工工单

| 管理端接口 | 请求与返回 |
|---|---|
| `GET /api/handoffs` | `status=active` 为默认值，含待接单和处理中。可选 `open`、`processing`、`resolved` 或 `all`。返回 `data` 数组。 |
| `POST /api/handoffs/:id/claim` | 使用工单 UUID，无请求体。返回 `data.status="processing"`。同一接单人重复点击不会重复发通知。 |
| `POST /api/messages/:id/reply` | 使用工单内用户消息的 UUID。请求 `{"text":"回复","learn":false}`，返回 `data.id`（待送达消息 UUID）。可连续调用。 |
| `POST /api/handoffs/:id/resolve` | 使用工单 UUID，无请求体。返回 `data.status="resolved"` 和 `data.resolved=true`。 |

- 工单 `status` 的三个值为 `open`（待接单）、`processing`（处理中）、`resolved`（已结单）。消息列表及详情的 `handoffStatus` 分别为 `waiting_human`、`human_processing`、`human_closed`，无工单为 null。
- 工单列表提供 `claimedByName`、`claimedById`、`claimedVia`、`claimedAt`。网页接单的 `claimedVia="web"`，ID 是当前管理员 UUID；QQ 接单的 `claimedVia="qq"`，ID 是接单人的私聊会话 UUID。两类账号独立鉴权。同名账号也不能代替接单人。未接单时这些字段为 null。非接单人操作或工单状态冲突时返回 HTTP 409，界面可直接展示 `error.message`。
- `resolvedAt` 是实际结单时间，`closeMethod` 为 `qq`（人工指令）、`web`（网页）或 `timeout`（空闲超时）。旧版“回复即结束”的工单迁移为 `resolved`，缺少结单来源的历史记录返回 null。最近一次回复的 `humanRepliedAt` 表示送达时间，与结单时间独立。
- `pausedUntil` 是本单当前空闲期限。新单沿用该客服的 `settings.handoffPauseMinutes`，默认 30 分钟。接单、用户新消息及人工回复会刷新期限。超时任务每分钟检查一次。网页接单人可以从后台告警 `kind=handoff_timeout` 的 `detail.userId`、`detail.handoffId` 识别自己的超时提醒，也可监听原有实时变更刷新人工台。
- `recent` 按时间正序返回本工单的用户消息、转人工的机器人回复及人工回复。每项含原有的消息和投递字段及 `parts` 附件数组。处理状态更新与客服通知不计入对话。QQ 接单前向各人工客服转发，QQ 接单后只转给接单人的私聊；网页接单后续消息出现在对应网页工单中。
