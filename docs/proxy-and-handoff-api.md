# 模型代理与人工客服接口

以下路径包含 API 前缀，沿用管理端登录态。成功响应均以 `data` 包装。

## 代理

| 方法和路径 | 请求 | data |
| --- | --- | --- |
| GET /api/system/proxies | 无 | 数组，元素为 `{id,name,address}` |
| POST /api/system/proxies | `{name,address}` | `{id}` |
| PUT /api/system/proxies/:id | `{name,address}` | `{id}` |
| DELETE /api/system/proxies/:id | 无 | `{deleted:true}`。被任一模型引用时返回 HTTP 409，错误码为 `proxy_in_use` |
| POST /api/system/proxies/:id/test | 无 | `{reachable,latencyMs,error}`。8 秒超时，成功时 error 为空串 |
| GET /api/model-profiles | 无 | 每个模型新增 `proxyId: string 或 null` |
| POST /api/model-profiles、PUT /api/model-profiles/:id | 原模型字段，加可选 `proxyId` | 沿用模型保存响应 |

地址包含协议、主机及适用的端口，例如 `socks5://127.0.0.1:1080`。支持无账号密码的 HTTP、HTTPS、SOCKS5 代理。连通性测试通过所选代理向 `https://example.com/` 发 HEAD 请求，延迟包含建连与 TLS。要验证具体供应商与凭证，可使用已有的模型测试接口。

对话、向量、辅助模型和模型测试均按模型配置出站。proxyId 传 UUID 选代理，传 null 或空串恢复直连。更新时省略字段会保留原值。新模型默认直连，直连模式跳过进程环境代理。

## 手机 QQ 人工客服

客服用本人 QQ 加机器人好友，先给机器人发一条私聊。管理端在该机器人名下选择对应的私聊并启用，可启用多个账号。身份依据是机器人收到的 QQ 用户 OpenID。启用后此私聊用于客服操作。帐号可以先发送约定的识别信息，由管理员在消息记录核对后绑定。

| 方法和路径 | 请求 | data |
| --- | --- | --- |
| GET /api/bots/:id/human-operators | 无 | 全部私聊候选，元素为 `{conversationId,userOpenId,name,enabled}` |
| PUT /api/bots/:id/human-operators/:conversationId | `{enabled:true}` 或 `{enabled:false}` | `{enabled}` |
| POST /api/messages/:id/reply | `{text,learn?}` | `{id}`，人工回复的出站消息 ID |

转人工会向已绑定客服排队发送工单，包括群名、用户、原文、转交原因，以及最近半小时内该用户最多两条相关会话发言。图片会提示到网页人工台查看。QQ 主动私信受机器人的权限及平台额度约束。通知投递状态可在工单接口读取。

手机回复示例为 `#123 已为您处理，请重试。`。编号后的空白与正文必填。机器人私聊确认已排队，再通过原会话发送。网页和 QQ 使用同一条回复队列。两名客服同时回复同一工单时，先入队的回复生效。发送中或已处理时网页返回 HTTP 409 和 `ticket_handled`，手机会收到提示。投递失败后可以重新回复。

群内回复以 **【人工客服】** 开头。有原消息引用索引时使用 QQ 引用，否则在正文标出原发言人。被动回复凭证或次数用尽时沿用主动发送机制。

## 工单与消息结论

| 接口 | 字段 |
| --- | --- |
| GET /api/messages、GET /api/messages/:id | 转人工时 `status` 与 `handoffStatus` 同值。新增 `ticketNumber`（字符串）、`humanRepliedAt`、`humanRepliedBy`、`humanReplyContent`、`humanDeliveryStatus`、`humanDeliveryError` |
| GET /api/handoffs?status=open | 支持 `open`（默认）、`replied`、`resolved`、`all`。新增 `ticketNumber`、`humanRepliedBy`、`humanReplyContent`、`humanRepliedAt`、`notifications` |
| POST /api/handoffs/:id/resolve | 手动关闭待处理工单，成功返回 `{resolved:true}` |

| 消息 handoffStatus | 含义 | 工单 status |
| --- | --- | --- |
| null | 普通消息，沿用原 status | — |
| waiting_human | 待人工回复。排队、投递中和发送失败均保持此结论 | open |
| human_replied | QQ 已确认人工回复投递，humanRepliedAt 为确认时间 | replied |
| human_closed | 人工手动关闭 | resolved |

投递状态取 `pending`、`processing`、`sent`、`failed`、`expired`，尚无回复时 humanDeliveryStatus 为 null。每条 notifications 元素含 `{conversationId,status,error}`。投递中可以展示已提交的回复人和正文，已回复时间仅在成功后显示。接管暂停时间控制 AI 何时可以重新接话，工单在投递成功或手动关闭前可继续处理。迁移会从已有投递及审计记录回填能够关联的历史人工回复。
