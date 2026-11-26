package app

import (
	"context"
	"encoding/json"
	"errors"
	"strings"

	"ai-bot/backend/internal/domain"
	"github.com/gin-gonic/gin"
	"github.com/jackc/pgx/v5"
)

// --- handoffs & human replies ---

func (a *App) listHandoffs(c *gin.Context) {
	status := c.DefaultQuery("status", "active")
	rows, err := a.db.Query(c, `SELECT h.id::text,h.bot_id::text AS "botId",h.conversation_id::text AS "conversationId",c.name AS "conversationName",c.type AS "conversationType",
		h.platform_user_id AS "userId",h.display_name AS "displayName",h.message_id::text AS "messageId",COALESCE(m.content,'') AS "messageContent",
		h.ticket_number::text AS "ticketNumber",hr.sender_name AS "humanRepliedBy",hr.content AS "humanReplyContent",CASE WHEN hot.status='sent' THEN hot.updated_at END AS "humanRepliedAt",
   COALESCE((SELECT json_agg(json_build_object('conversationId',n.conversation_id,'status',t.status,'error',t.last_error) ORDER BY n.created_at)
    FROM messages n JOIN outbox_tasks t ON t.message_id=n.id WHERE n.handoff_id=h.id AND n.event_type='HANDOFF_NOTICE'),'[]') AS notifications,h.reason,h.status,h.claimed_by_id::text AS "claimedById",h.claimed_by_name AS "claimedByName",h.claimed_via AS "claimedVia",h.claimed_at AS "claimedAt",h.close_method AS "closeMethod",h.paused_until AS "pausedUntil",h.created_at AS "createdAt",h.resolved_at AS "resolvedAt",
		COALESCE((SELECT json_agg(x ORDER BY x."eventAt") FROM (SELECT mm.id::text AS id,mm.direction,mm.sender_name AS "senderName",mm.content,mm.parts,mm.event_type AS "eventType",mm.event_at AS "eventAt",mm.status,COALESCE(ot.last_error,'') AS "deliveryError"
			FROM messages mm LEFT JOIN outbox_tasks ot ON ot.message_id=mm.id WHERE mm.conversation_id=h.conversation_id AND mm.handoff_id=h.id
			AND (mm.direction='inbound' OR mm.event_type IN ('HUMAN_REPLY','AI_RESPONSE')) ORDER BY mm.event_at DESC) x),'[]') AS recent
		FROM handoffs h JOIN conversations c ON c.id=h.conversation_id LEFT JOIN messages m ON m.id=h.message_id LEFT JOIN messages hr ON hr.id=h.reply_id LEFT JOIN outbox_tasks hot ON hot.message_id=hr.id
		WHERE ($1='all' OR h.status=$1 OR ($1='active' AND h.status IN ('open','processing'))) ORDER BY h.created_at DESC LIMIT 200`, status)
	if err != nil {
		fail(c, 500, "database_error", "读取工单失败")
		return
	}
	items, err := rowsJSON(rows)
	if err != nil {
		fail(c, 500, "database_error", "读取工单失败")
		return
	}
	for _, item := range items {
		recent, _ := item["recent"].([]any)
		for _, value := range recent {
			if m, ok := value.(map[string]any); ok {
				id, _ := m["id"].(string)
				m["parts"] = publicContentParts(id, m["parts"])
			}
		}
	}
	ok(c, items)
}

func webActor(c *gin.Context) ticketActor {
	return ticketActor{"web", c.GetString("userId"), c.GetString("username")}
}
func (a *App) claimHandoff(c *gin.Context)   { a.webTicketAction(c, true) }
func (a *App) resolveHandoff(c *gin.Context) { a.webTicketAction(c, false) }
func (a *App) webTicketAction(c *gin.Context, claim bool) {
	id := c.Param("id")
	if parseUUID(id) != nil {
		fail(c, 400, "invalid_id", "工单编号无效")
		return
	}
	err := pgx.BeginFunc(c, a.db, func(tx pgx.Tx) error { return ticketAction(c, tx, id, webActor(c), claim) })
	var conflict ticketConflict
	switch {
	case errors.As(err, &conflict):
		fail(c, 409, "ticket_conflict", err.Error())
	case errors.Is(err, pgx.ErrNoRows):
		fail(c, 404, "not_found", "工单不存在")
	case err != nil:
		fail(c, 500, "database_error", "更新工单失败")
	case claim:
		ok(c, gin.H{"status": "processing"})
	default:
		ok(c, gin.H{"status": "resolved", "resolved": true})
	}
}

// humanReply queues an admin reply; delivery falls back to active sending when needed.
func (a *App) humanReply(c *gin.Context) {
	var in struct {
		Text  string `json:"text" form:"text"`
		Learn bool   `json:"learn" form:"learn"`
	}
	img, err := bindReplyImage(c, &in)
	if err != nil {
		fail(c, 400, "invalid_request", err.Error())
		return
	}
	items, err := img.plan.resolve(nil)
	if err != nil {
		fail(c, 400, "invalid_request", err.Error())
		return
	}
	if len(items) == 0 && img.data == nil && strings.TrimSpace(in.Text) == "" {
		fail(c, 400, "invalid_request", "回复内容必填")
		return
	}
	tx, err := a.db.Begin(c)
	if err != nil {
		fail(c, 500, "database_error", "发送失败")
		return
	}
	defer tx.Rollback(c)
	outID, err := queueHuman(c, tx, c.Param("id"), webActor(c), strings.TrimSpace(in.Text))
	if errors.Is(err, pgx.ErrNoRows) {
		fail(c, 404, "not_found", "消息不存在或不可回复")
		return
	}
	var conflict ticketConflict
	if errors.As(err, &conflict) {
		fail(c, 409, "ticket_handled", err.Error())
		return
	}
	if err == nil && img.data != nil {
		name := "人工回复" + imageExtension(img.mime)
		var key string
		key, err = a.images.Save(c, name, img.data)
		if err == nil {
			_, err = tx.Exec(c, "UPDATE messages SET parts=$1 WHERE id=$2", []domain.ContentPart{{Type: "image", Filename: name, ContentType: img.mime, SizeBytes: int64(len(img.data)), StorageKey: key}}, outID)
			if err != nil {
				_ = a.images.Delete(c, key)
			}
		}
	}
	ids := []string{outID}
	if err == nil && len(items) > 0 {
		var parts []domain.ContentPart
		parts, err = a.storeMaterials(c, items)
		if err == nil {
			ids, err = queueHumanMaterials(c, tx, outID, parts)
		}
		if err != nil {
			for _, p := range parts {
				_ = a.images.Delete(c, p.StorageKey)
			}
		}
	}
	if err != nil || tx.Commit(c) != nil {
		fail(c, 500, "database_error", "发送失败")
		return
	}
	if in.Learn {
		a.enqueueJob(c, "staff_answer", map[string]any{"messageId": outID}, "staff_answer:"+outID)
	}
	a.audit(c, "message.human_reply", "message", c.Param("id"), map[string]any{"replyId": outID})
	ok(c, gin.H{"id": outID, "messageIds": ids})
}

// queueHuman has already locked the conversation and authorized the ticket.
func queueHumanMaterials(ctx context.Context, tx pgx.Tx, first string, parts []domain.ContentPart) ([]string, error) {
	var used int
	err := tx.QueryRow(ctx, `SELECT coalesce(max(t.msg_seq),0) FROM messages m JOIN outbox_tasks t ON t.message_id=m.id JOIN messages anchor ON anchor.id=$1 WHERE m.conversation_id=anchor.conversation_id AND m.reply_to_message_id=anchor.reply_to_message_id AND m.id<>anchor.id`, first).Scan(&used)
	if err != nil {
		return nil, err
	}
	ids := make([]string, 0, len(parts))
	for i, part := range parts {
		id := first
		if i == 0 {
			_, err = tx.Exec(ctx, "UPDATE messages SET material_batch=id,material_index=0,parts=$1 WHERE id=$2", []domain.ContentPart{part}, id)
			if err == nil {
				_, err = tx.Exec(ctx, "UPDATE outbox_tasks SET msg_seq=$1 WHERE message_id=$2", used+1, id)
			}
		} else {
			err = tx.QueryRow(ctx, `INSERT INTO messages(channel,bot_id,conversation_id,direction,event_type,sender_name,content,reply_to_message_id,status,reply_deadline,handoff_id,parts,material_batch,material_index,created_at,event_at)
 SELECT channel,bot_id,conversation_id,direction,event_type,sender_name,'',reply_to_message_id,'pending',reply_deadline,handoff_id,$2,id,$3,clock_timestamp(),clock_timestamp() FROM messages WHERE id=$1 RETURNING id::text`, first, []domain.ContentPart{part}, i).Scan(&id)
			if err == nil {
				_, err = tx.Exec(ctx, "INSERT INTO outbox_tasks(message_id,msg_seq) VALUES($1,$2)", id, used+i+1)
			}
		}
		if err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, nil
}

// --- members / staff ---

func (a *App) listMembers(c *gin.Context) {
	queryList(c, a, `SELECT coalesce((SELECT used FROM image_daily_usage WHERE conversation_id=cm.conversation_id AND user_id=cm.platform_user_id AND day=(now() AT TIME ZONE 'Asia/Shanghai')::date),0) AS "imagesToday",cm.platform_user_id AS "userId",cm.display_name AS "displayName",cm.qq_avatar AS avatar,cm.qq_joined_at AS "joinedAt",cm.qq_union_id AS "unionId",cm.active,cm.qq_role AS "role",cm.is_bot AS "isBot",cm.is_staff AS "isStaff",
        EXISTS(SELECT 1 FROM blocked_users bu JOIN conversations bc ON bc.bot_id=bu.bot_id WHERE bc.id=cm.conversation_id AND bu.platform_user_id=cm.platform_user_id AND (bu.conversation_id IS NULL OR bu.conversation_id=cm.conversation_id)) AS "isBlocked",
        COALESCE((SELECT jsonb_agg(bu.id::text) FROM blocked_users bu JOIN conversations bc ON bc.bot_id=bu.bot_id WHERE bc.id=cm.conversation_id AND bu.platform_user_id=cm.platform_user_id AND (bu.conversation_id IS NULL OR bu.conversation_id=cm.conversation_id)),'[]') AS "blockedUserIds",cm.first_seen_at AS "firstSeenAt",cm.last_seen_at AS "lastSeenAt",
		(SELECT count(*) FROM user_memories um JOIN conversations c ON c.bot_id=um.bot_id WHERE c.id=cm.conversation_id AND um.conversation_id=c.id AND um.platform_user_id=cm.platform_user_id AND um.status='active') AS "memoryCount",
		(SELECT count(*) FROM messages m WHERE m.conversation_id=cm.conversation_id AND m.sender_id=cm.platform_user_id) AS "messageCount"
		FROM conversation_members cm WHERE cm.conversation_id=$1 AND cm.active ORDER BY cm.is_staff DESC,cm.last_seen_at DESC,cm.platform_user_id`, c.Param("id"))
}

func (a *App) updateMember(c *gin.Context) {
	var in struct {
		IsStaff bool `json:"isStaff"`
	}
	if c.ShouldBindJSON(&in) != nil {
		fail(c, 400, "invalid_request", "参数格式无效")
		return
	}
	tag, err := a.db.Exec(c, "UPDATE conversation_members SET is_staff=$1 WHERE conversation_id=$2 AND platform_user_id=$3", in.IsStaff, c.Param("id"), c.Param("userId"))
	if err != nil || tag.RowsAffected() == 0 {
		fail(c, 404, "not_found", "成员不存在")
		return
	}
	_, _ = a.db.Exec(c, "DELETE FROM conversation_staff_styles WHERE conversation_id=$1", c.Param("id"))
	if in.IsStaff {
		a.enqueueJob(c, "staff_history", map[string]any{"conversationId": c.Param("id")}, "staff_history:"+c.Param("id"))
	}
	a.audit(c, "conversation.member.staff", "conversation_member", c.Param("userId"), map[string]any{"conversationId": c.Param("id"), "isStaff": in.IsStaff})
	ok(c, gin.H{"updated": true})
}

func (a *App) getSummary(c *gin.Context) {
	queryList(c, a, `SELECT scope_key AS "scope",summary,covered_until AS "coveredUntil",updated_at AS "updatedAt" FROM conversation_summaries WHERE conversation_id=$1 ORDER BY updated_at DESC LIMIT 100`, c.Param("id"))
}

// --- skills ---

func (a *App) listSkills(c *gin.Context) {
	queryList(c, a, `SELECT id::text,name,description,instructions,tools,enabled,sort_order AS "sortOrder",updated_at AS "updatedAt" FROM skills ORDER BY sort_order,name`)
}

func (a *App) saveSkill(c *gin.Context) {
	var in struct {
		Name         string   `json:"name"`
		Description  string   `json:"description"`
		Instructions string   `json:"instructions"`
		Tools        []string `json:"tools"`
		Enabled      bool     `json:"enabled"`
		SortOrder    int      `json:"sortOrder"`
	}
	if c.ShouldBindJSON(&in) != nil || strings.TrimSpace(in.Name) == "" || strings.TrimSpace(in.Instructions) == "" {
		fail(c, 400, "invalid_request", "技能名称和说明必填")
		return
	}
	if in.Tools == nil {
		in.Tools = []string{}
	}
	tools, _ := json.Marshal(in.Tools)
	var err error
	if id := c.Param("id"); id != "" {
		_, err = a.db.Exec(c, "UPDATE skills SET name=$1,description=$2,instructions=$3,tools=$4::jsonb,enabled=$5,sort_order=$6,updated_at=now() WHERE id=$7", strings.TrimSpace(in.Name), in.Description, in.Instructions, string(tools), in.Enabled, in.SortOrder, id)
	} else {
		_, err = a.db.Exec(c, "INSERT INTO skills(name,description,instructions,tools,enabled,sort_order) VALUES($1,$2,$3,$4::jsonb,$5,$6)", strings.TrimSpace(in.Name), in.Description, in.Instructions, string(tools), in.Enabled, in.SortOrder)
	}
	if err != nil {
		fail(c, 400, "save_failed", "保存失败，技能名称可能重复")
		return
	}
	ok(c, gin.H{"saved": true})
}

func (a *App) deleteSkill(c *gin.Context) {
	_, _ = a.db.Exec(c, "DELETE FROM skills WHERE id=$1", c.Param("id"))
	ok(c, gin.H{"deleted": true})
}

// --- tools ---

func (a *App) listTools(c *gin.Context) {
	var builtins []gin.H
	for _, t := range builtinTools {
		builtins = append(builtins, gin.H{"name": t.spec.Name, "description": t.spec.Description, "parameters": t.spec.Parameters, "requiresLiveData": t.needsLive, "requiresKnowledgeBase": t.needsKB})
	}
	tools, err := a.loadCustomTools(c, false)
	if err != nil {
		fail(c, 500, "database_error", "读取工具失败")
		return
	}
	enabled := map[string]bool{}
	rows, _ := a.db.Query(c, "SELECT name,enabled FROM tool_definitions WHERE kind='http'")
	for rows != nil && rows.Next() {
		var n string
		var e bool
		_ = rows.Scan(&n, &e)
		enabled[n] = e
	}
	if rows != nil {
		rows.Close()
	}
	custom := []gin.H{}
	for _, t := range tools {
		headers := []string{}
		for k := range t.Config.Headers {
			headers = append(headers, k)
		}
		custom = append(custom, gin.H{"id": t.ID, "name": t.Name, "description": t.Description, "parameters": t.Parameters, "enabled": enabled[t.Name],
			"method": t.Config.Method, "url": t.Config.URL, "headerNames": headers, "timeoutSeconds": t.Config.TimeoutSeconds})
	}
	ok(c, gin.H{"builtin": builtins, "custom": custom})
}

func (a *App) saveTool(c *gin.Context) {
	var in struct {
		Name           string            `json:"name"`
		Description    string            `json:"description"`
		Enabled        bool              `json:"enabled"`
		Method         string            `json:"method"`
		URL            string            `json:"url"`
		Headers        map[string]string `json:"headers"`
		KeepHeaders    bool              `json:"keepHeaders"`
		TimeoutSeconds int               `json:"timeoutSeconds"`
		Parameters     map[string]any    `json:"parameters"`
	}
	if c.ShouldBindJSON(&in) != nil || !toolNamePattern.MatchString(in.Name) || isBuiltinTool(in.Name) {
		fail(c, 400, "invalid_request", "工具名称只能包含字母、数字、下划线和短横线，且不能与内置工具重名")
		return
	}
	if !strings.HasPrefix(in.URL, "https://") && !strings.HasPrefix(in.URL, "http://") {
		fail(c, 400, "invalid_request", "接口地址必须以 http:// 或 https:// 开头")
		return
	}
	in.Method = strings.ToUpper(in.Method)
	if in.Method != "GET" && in.Method != "POST" {
		in.Method = "GET"
	}
	if in.Parameters == nil {
		in.Parameters = objSchema(map[string]any{})
	}
	if t, _ := in.Parameters["type"].(string); t != "object" {
		fail(c, 400, "invalid_request", "参数定义必须是 type=object 的 JSON Schema")
		return
	}
	id := c.Param("id")
	if id != "" && in.KeepHeaders {
		tools, _ := a.loadCustomTools(c, false)
		for _, t := range tools {
			if t.ID == id {
				in.Headers = t.Config.Headers
			}
		}
	}
	cfg, _ := json.Marshal(httpToolConfig{Method: in.Method, URL: in.URL, Headers: in.Headers, TimeoutSeconds: in.TimeoutSeconds})
	enc, err := a.cipher.Encrypt(string(cfg))
	if err != nil {
		fail(c, 500, "secret_error", "加密失败")
		return
	}
	params, _ := json.Marshal(in.Parameters)
	if id != "" {
		_, err = a.db.Exec(c, "UPDATE tool_definitions SET name=$1,description=$2,enabled=$3,config_enc=$4,parameters=$5::jsonb,kind='http',updated_at=now() WHERE id=$6", in.Name, in.Description, in.Enabled, enc, string(params), id)
	} else {
		_, err = a.db.Exec(c, "INSERT INTO tool_definitions(name,description,enabled,config_enc,parameters,kind) VALUES($1,$2,$3,$4,$5::jsonb,'http')", in.Name, in.Description, in.Enabled, enc, string(params))
	}
	if err != nil {
		fail(c, 400, "save_failed", "保存失败，工具名称可能重复")
		return
	}
	a.audit(c, "tool.save", "tool_definition", in.Name, map[string]any{"url": in.URL, "method": in.Method})
	ok(c, gin.H{"saved": true})
}

func (a *App) deleteTool(c *gin.Context) {
	_, _ = a.db.Exec(c, "DELETE FROM tool_definitions WHERE id=$1", c.Param("id"))
	ok(c, gin.H{"deleted": true})
}

func (a *App) testTool(c *gin.Context) {
	var in struct {
		Arguments map[string]any `json:"arguments"`
	}
	_ = c.ShouldBindJSON(&in)
	tools, _ := a.loadCustomTools(c, false)
	for _, t := range tools {
		if t.ID == c.Param("id") {
			res, err := a.callHTTPTool(c, t, in.Arguments)
			if err != nil {
				fail(c, 502, "tool_error", err.Error())
				return
			}
			ok(c, gin.H{"result": res})
			return
		}
	}
	fail(c, 404, "not_found", "工具不存在")
}
