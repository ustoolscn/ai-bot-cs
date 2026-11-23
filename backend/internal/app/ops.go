package app

import (
	"encoding/json"
	"errors"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/jackc/pgx/v5"
)

// --- handoffs & human replies ---

func (a *App) listHandoffs(c *gin.Context) {
	status := c.DefaultQuery("status", "open")
	queryList(c, a, `SELECT h.id::text,h.bot_id::text AS "botId",h.conversation_id::text AS "conversationId",c.name AS "conversationName",c.type AS "conversationType",
		h.platform_user_id AS "userId",h.display_name AS "displayName",h.message_id::text AS "messageId",COALESCE(m.content,'') AS "messageContent",
		h.ticket_number::text AS "ticketNumber",hr.sender_name AS "humanRepliedBy",hr.content AS "humanReplyContent",CASE WHEN h.status='replied' THEN h.resolved_at END AS "humanRepliedAt",
   COALESCE((SELECT json_agg(json_build_object('conversationId',n.conversation_id,'status',t.status,'error',t.last_error) ORDER BY n.created_at)
    FROM messages n JOIN outbox_tasks t ON t.message_id=n.id WHERE n.handoff_id=h.id AND n.event_type='HANDOFF_NOTICE'),'[]') AS notifications,h.reason,h.status,h.paused_until AS "pausedUntil",h.created_at AS "createdAt",h.resolved_at AS "resolvedAt",
		COALESCE((SELECT json_agg(x ORDER BY x."eventAt") FROM (SELECT mm.id::text AS id,mm.direction,mm.sender_name AS "senderName",mm.content,mm.event_type AS "eventType",mm.event_at AS "eventAt",mm.status,COALESCE(ot.last_error,'') AS "deliveryError"
			FROM messages mm LEFT JOIN outbox_tasks ot ON ot.message_id=mm.id WHERE mm.conversation_id=h.conversation_id AND mm.event_type<>'PROCESSING_ACK' AND mm.content<>'' AND mm.event_at>h.created_at-interval '30 minutes'
			AND (mm.sender_id=h.platform_user_id OR mm.direction='outbound') ORDER BY mm.event_at DESC LIMIT 30) x),'[]') AS recent
		FROM handoffs h JOIN conversations c ON c.id=h.conversation_id LEFT JOIN messages m ON m.id=h.message_id LEFT JOIN messages hr ON hr.id=h.reply_id
		WHERE ($1='all' OR h.status=$1) ORDER BY h.created_at DESC LIMIT 200`, status)
}

func (a *App) resolveHandoff(c *gin.Context) {
	tag, err := a.db.Exec(c, "UPDATE handoffs SET status='resolved',resolved_by=$1,resolved_at=now(),paused_until=now(),updated_at=now() WHERE id=$2 AND status='open' AND NOT EXISTS(SELECT FROM outbox_tasks WHERE message_id=handoffs.reply_id AND status NOT IN ('expired','failed'))", c.GetString("userId"), c.Param("id"))
	if err != nil || tag.RowsAffected() == 0 {
		fail(c, 404, "not_found", "工单不存在或已处理")
		return
	}
	ok(c, gin.H{"resolved": true})
}

// humanReply queues an admin reply; delivery falls back to active sending when needed.
func (a *App) humanReply(c *gin.Context) {
	var in struct {
		Text  string `json:"text"`
		Learn bool   `json:"learn"`
	}
	if c.ShouldBindJSON(&in) != nil || strings.TrimSpace(in.Text) == "" {
		fail(c, 400, "invalid_request", "回复内容必填")
		return
	}
	tx, err := a.db.Begin(c)
	if err != nil {
		fail(c, 500, "database_error", "发送失败")
		return
	}
	defer tx.Rollback(c)
	outID, err := queueHuman(c, tx, c.Param("id"), c.GetString("username"), strings.TrimSpace(in.Text))
	if errors.Is(err, pgx.ErrNoRows) {
		fail(c, 404, "not_found", "消息不存在或不可回复")
		return
	}
	if errors.Is(err, errTicketHandled) {
		fail(c, 409, "ticket_handled", err.Error())
		return
	}
	if err != nil || tx.Commit(c) != nil {
		fail(c, 500, "database_error", "发送失败")
		return
	}
	if in.Learn {
		a.enqueueJob(c, "staff_answer", map[string]any{"messageId": outID}, "staff_answer:"+outID)
	}
	a.audit(c, "message.human_reply", "message", c.Param("id"), map[string]any{"replyId": outID})
	ok(c, gin.H{"id": outID})
}

// --- members / staff ---

func (a *App) listMembers(c *gin.Context) {
	queryList(c, a, `SELECT cm.platform_user_id AS "userId",cm.display_name AS "displayName",cm.qq_avatar AS avatar,cm.qq_joined_at AS "joinedAt",cm.qq_union_id AS "unionId",cm.active,cm.qq_role AS "role",cm.is_bot AS "isBot",cm.is_staff AS "isStaff",
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
