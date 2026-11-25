package app

import (
	"encoding/json"
	"github.com/gin-gonic/gin"
	"slices"
	"strconv"
)

func (a *App) chatHistory(c *gin.Context) {
	cid, before := c.Param("id"), c.Query("before")
	limit, err := strconv.Atoi(c.DefaultQuery("limit", "50"))
	if parseUUID(cid) != nil || (before != "" && parseUUID(before) != nil) || err != nil || limit < 1 || limit > 200 {
		fail(c, 400, "invalid_request", "分页参数无效")
		return
	}
	rows, err := a.db.Query(c, `SELECT m.id::text,m.direction,
 CASE WHEN m.event_type IN ('GROUP_MEMBER_ADD','GROUP_MEMBER_REMOVE','GROUP_ADD_ROBOT','GROUP_DEL_ROBOT') THEN 'system' WHEN m.direction='inbound' THEN 'user' WHEN m.event_type IN ('QQ_ADMIN_SEND','HUMAN_REPLY','QQ_MEDIA_SEND') THEN 'human' ELSE 'bot' END AS role,
 CASE WHEN m.event_type='AI_RESPONSE' THEN 'reply' WHEN m.event_type='SCHEDULED_POST' THEN 'scheduled' WHEN m.event_type='DAILY_REPORT' THEN 'report' WHEN m.event_type IN ('HANDOFF_NOTICE','HANDOFF_UPDATE','OPERATOR_ACK') THEN 'notice' WHEN m.event_type='WELCOME' THEN 'welcome' WHEN m.event_type IN ('HUMAN_REPLY','QQ_ADMIN_SEND','QQ_MEDIA_SEND') THEN 'manual' ELSE 'message' END AS kind,
 CASE WHEN m.direction='inbound' THEN coalesce(nullif(cm.qq_avatar,''),m.raw_event#>>'{d,author,avatar}','') ELSE '' END AS "senderAvatar",
 m.sender_id AS "senderId",coalesce(nullif(nullif(m.sender_name,''),m.sender_id),nullif(nullif(cm.display_name,''),m.sender_id),nullif(nullif(cm.qq_username,''),m.sender_id),CASE WHEN m.direction='inbound' THEN 'QQ用户' WHEN m.event_type IN ('QQ_ADMIN_SEND','HUMAN_REPLY','QQ_MEDIA_SEND') THEN '客服' ELSE b.name END) AS "senderName",
 CASE WHEN m.qq_delivered_text IS NOT NULL THEN m.qq_delivered_text WHEN m.event_type='HUMAN_REPLY' AND m.content NOT LIKE '【人工客服】%' THEN '【人工客服】'||m.content ELSE m.content END AS content,message_mention_names(m,m.content) AS "mentionNames",m.parts,m.event_at AS at,
 CASE WHEN m.direction='outbound' THEN CASE WHEN coalesce(t.status,m.status) IN ('failed','expired') THEN 'failed' ELSE coalesce(t.status,m.status) END END AS status,coalesce(t.last_error,nullif(m.delivery_error,'')) AS error,
 ref.id::text AS "replyToId",CASE WHEN m.direction='inbound' THEN m.id::text END AS "messageId",
 coalesce(m.direction='inbound' AND NOT m.context_excluded AND m.status<>'retracted' AND m.platform_message_id IS NOT NULL AND m.reply_deadline>now(),false) AS "canReply",
 (NOT m.context_excluded AND m.status<>'retracted' AND m.platform_message_id IS NOT NULL AND m.qq_ref_idx<>'') AS quotable
 FROM messages m JOIN bots b ON b.id=m.bot_id LEFT JOIN conversation_members cm ON cm.conversation_id=m.conversation_id AND cm.platform_user_id=m.sender_id
 LEFT JOIN outbox_tasks t ON t.message_id=m.id LEFT JOIN messages ref ON ref.conversation_id=m.conversation_id AND ref.platform_message_id=m.reply_to_message_id
 WHERE m.conversation_id=$1 AND m.event_type<>'PROCESSING_ACK' AND ($2::uuid IS NULL OR (m.event_at,m.id)<(SELECT event_at,id FROM messages WHERE conversation_id=$1 AND id=$2))
 ORDER BY m.event_at DESC,m.id DESC LIMIT $3`, cid, nullString(before), limit+1)
	if err != nil {
		fail(c, 500, "database_error", "读取聊天记录失败")
		return
	}
	items, err := rowsJSON(rows)
	if err != nil {
		fail(c, 500, "database_error", "读取聊天记录失败")
		return
	}
	var next any
	if len(items) > limit {
		items = items[:limit]
		next = items[limit-1]["id"]
	}
	slices.Reverse(items)
	for _, item := range items {
		item["attachments"] = publicContentParts(item["id"].(string), item["parts"])
		delete(item, "parts")
	}
	ok(c, gin.H{"items": items, "nextBefore": next})
}

func (a *App) mentionNames(c *gin.Context) {
	var in struct {
		IDs []string `json:"ids"`
	}
	if c.ShouldBindJSON(&in) != nil || in.IDs == nil || len(in.IDs) > 500 {
		fail(c, 400, "invalid_request", "请提供最多 500 个用户编号")
		return
	}
	var names json.RawMessage
	err := a.db.QueryRow(c, `WITH ids AS (SELECT DISTINCT unnest($1::text[]) AS id), names AS (
 SELECT id,coalesce((SELECT coalesce(nullif(nullif(display_name,ids.id),''),nullif(nullif(qq_username,ids.id),'')) AS name FROM conversation_members cm WHERE platform_user_id=ids.id ORDER BY (coalesce(nullif(nullif(display_name,ids.id),''),nullif(nullif(qq_username,ids.id),'')) IS NOT NULL) DESC,last_seen_at DESC LIMIT 1),
 (SELECT sender_name FROM messages WHERE sender_id=ids.id AND sender_name NOT IN ('',ids.id) ORDER BY event_at DESC LIMIT 1)) AS name FROM ids)
 SELECT coalesce(jsonb_object_agg(id,name) FILTER(WHERE name IS NOT NULL),'{}') FROM names`, in.IDs).Scan(&names)
	if err != nil {
		fail(c, 500, "database_error", "查询昵称失败")
		return
	}
	ok(c, names)
}
