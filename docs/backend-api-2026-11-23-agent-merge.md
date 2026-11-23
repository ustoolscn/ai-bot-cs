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
- 学习及人工使用 `evaluationEnabled,staffMiningEnabled,autoPublishStaff,autoPublishMinConfidence,learningKnowledgeBaseId,handoffPauseMinutes`。
- `liveData` 支持 `enabled,baseUrl,userId,refreshSeconds,syncAnnouncements,announcementKnowledgeBaseId,injectSnapshot,priceNote,statusPageUrl`。`hasAccessToken` 只读。设置令牌使用**请求顶层**的 `liveDataAccessToken`；省略保留同站令牌，空串清除，更换网站请重新输入令牌。
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
