package app

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"ai-bot/backend/internal/agent"
	"ai-bot/backend/internal/qq"
	"github.com/gin-gonic/gin"
)

// sendPrivateTyping sends a best-effort QQ "typing..." notification for private chats.
func (a *App) sendPrivateTyping(ctx context.Context, botID, platformUserID, replyToMsgID string, seconds int) {
	if botID == "" || platformUserID == "" || seconds < 1 || seconds > 60 {
		return
	}
	var appID, secretEnc string
	if err := a.db.QueryRow(ctx, "SELECT app_id,app_secret_enc FROM bots WHERE id=$1 AND enabled", botID).Scan(&appID, &secretEnc); err != nil {
		return
	}
	secret, err := a.cipher.Decrypt(secretEnc)
	if err != nil {
		return
	}
	tctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	payload := map[string]any{
		"msg_type": 6,
		"input_notify": map[string]any{
			"input_type":   1,
			"input_second": seconds,
		},
	}
	if replyToMsgID != "" {
		payload["msg_id"] = replyToMsgID
	}
	if _, err := qq.NewClient(appID, secret, a.cfg.QQAPIBaseURL, a.cfg.QQTokenURL).SendConfigured(tctx, qq.Prefix("private", platformUserID), payload); err != nil {
		a.log.Debug("private typing indicator skipped", "bot_id", botID, "error", err)
	}
}

// queueDueProactiveTriggers dispatches due proactive care triggers into agent_jobs.
func (a *App) queueDueProactiveTriggers(ctx context.Context, now time.Time) error {
	rows, err := a.db.Query(ctx, `SELECT id::text FROM proactive_triggers WHERE status='pending' AND scheduled_at<=$1 ORDER BY scheduled_at LIMIT 20`, now)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var id string
		if rows.Scan(&id) == nil && id != "" {
			a.enqueueJob(ctx, "proactive_send", map[string]any{"triggerId": id}, "proactive:"+id)
		}
	}
	return nil
}

type proactiveGateDecision struct {
	ShouldSend bool   `json:"shouldSend"`
	Reason     string `json:"reason"`
	Draft      string `json:"draft"`
}

// jobProactiveSend validates budget, user activity, quiet hours, and a self-critique
// anti-intrusion gate before queuing a gentle proactive care message via outbox_tasks.
func (a *App) jobProactiveSend(ctx context.Context, triggerID string) error {
	if parseUUID(triggerID) != nil {
		return nil
	}
	var subjectID, botID, convID, convType, kind, topic string
	var createdAt time.Time
	var convEnabled, botEnabled, botRemoved bool
	err := a.db.QueryRow(ctx, `SELECT COALESCE(p.subject_id::text,''),p.bot_id::text,p.conversation_id::text,c.type,p.kind,p.topic,p.created_at,
		c.enabled,c.bot_removed,b.enabled
		FROM proactive_triggers p JOIN conversations c ON c.id=p.conversation_id JOIN bots b ON b.id=p.bot_id
		WHERE p.id=$1 AND p.status='pending'`, triggerID).
		Scan(&subjectID, &botID, &convID, &convType, &kind, &topic, &createdAt, &convEnabled, &botRemoved, &botEnabled)
	if err != nil {
		return nil
	}

	markSkipped := func(reason string) error {
		_, e := a.db.Exec(ctx, "UPDATE proactive_triggers SET status='skipped',skip_reason=$2,updated_at=now() WHERE id=$1 AND status='pending'", triggerID, reason)
		return e
	}

	if !convEnabled || botRemoved || !botEnabled {
		return markSkipped("conversation_or_bot_disabled")
	}

	s, _, profileID, err := a.conversationAgent(ctx, convID, a.agentSettings(ctx))
	if err != nil {
		return err
	}
	if !s.Companion.Enabled || !s.Companion.ProactiveEnabled {
		return markSkipped("proactive_disabled")
	}
	if inQuietHours(s.Companion.QuietHoursStart, s.Companion.QuietHoursEnd, time.Now()) {
		// Postpone by 30 minutes instead of dropping or disturbing at night.
		_, e := a.db.Exec(ctx, "UPDATE proactive_triggers SET scheduled_at=now()+interval '30 minutes',updated_at=now() WHERE id=$1 AND status='pending'", triggerID)
		return e
	}

	// Daily outreach budget check per conversation/subject.
	var sentToday int
	_ = a.db.QueryRow(ctx, `SELECT count(*) FROM proactive_triggers
		WHERE status='sent' AND (conversation_id=$1 OR ($2<>'' AND subject_id::text=$2))
		AND updated_at > date_trunc('day', now() AT TIME ZONE 'Asia/Shanghai') AT TIME ZONE 'Asia/Shanghai'`, convID, subjectID).Scan(&sentToday)
	if sentToday >= max(1, s.Companion.ProactiveDailyBudget) {
		return markSkipped("daily_budget_exceeded")
	}

	// Skip if the user has already chatted again recently after the trigger was scheduled.
	var userActiveAfter bool
	_ = a.db.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM messages WHERE conversation_id=$1 AND direction='inbound' AND NOT context_excluded AND event_at > $2)`, convID, createdAt.Add(2*time.Minute)).Scan(&userActiveAfter)
	if userActiveAfter {
		return markSkipped("user_already_active")
	}

	// Check handoff or blacklist.
	var openHandoff bool
	_ = a.db.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM handoffs WHERE conversation_id=$1 AND status IN ('open','processing'))`, convID).Scan(&openHandoff)
	if openHandoff {
		return markSkipped("handoff_open")
	}

	var recent []string
	rRows, _ := a.db.Query(ctx, `SELECT direction,COALESCE(NULLIF(sender_name,''),'用户'),display_mentions(m,m.content) FROM messages m
		WHERE m.conversation_id=$1 AND NOT m.context_excluded AND m.event_type<>'PROCESSING_ACK' AND m.content<>''
		ORDER BY m.event_at DESC LIMIT 8`, convID)
	if rRows != nil {
		for rRows.Next() {
			var dir, name, txt string
			if rRows.Scan(&dir, &name, &txt) == nil {
				who := name
				if dir == "outbound" {
					who = "我"
				}
				recent = append([]string{who + "：" + agent.TruncateRunes(txt, 160)}, recent...)
			}
		}
		rRows.Close()
	}

	var subjSummary string
	if subjectID != "" {
		if rec, err := a.loadSubjectRecord(ctx, subjectID); err == nil {
			subjSummary = fmt.Sprintf("称呼：%s，关系：%s，近期情绪：%s，档案：%s", rec.DisplayName, rec.RelationshipStage, rec.MoodState.CurrentMood, agent.TruncateRunes(rec.ProfileContent, 300))
		}
	}

	ctx = context.WithValue(ctx, usageScopeKey{}, usageScope{conversation: convID})
	gateInput, _ := json.Marshal(map[string]any{
		"触发类型":   kind,
		"计划跟进事由": topic,
		"用户概览":   subjSummary,
		"机器人性格":  s.StylePrompt,
		"近期对话":   recent,
	})
	var gate proactiveGateDecision
	if err := a.auxJSON(ctx, s, profileID, "", "", "proactive_gate", agent.ProactiveGatePrompt, string(gateInput), &gate); err != nil {
		return err
	}
	draft := strings.TrimSpace(gate.Draft)
	if !gate.ShouldSend || draft == "" {
		return markSkipped(firstNonEmpty(strings.TrimSpace(gate.Reason), "gate_declined"))
	}

	// Queue via outbox_tasks; attach to the latest passive inbound reply window if still valid.
	var replyTo string
	var deadline *time.Time
	var seq = 1
	_ = a.db.QueryRow(ctx, `SELECT platform_message_id,reply_deadline FROM messages
		WHERE conversation_id=$1 AND direction='inbound' AND NOT context_excluded AND reply_deadline>now()+interval '30 seconds'
		ORDER BY event_at DESC LIMIT 1`, convID).Scan(&replyTo, &deadline)
	if replyTo != "" {
		_ = a.db.QueryRow(ctx, `SELECT COALESCE(max(t.msg_seq),0)+1 FROM outbox_tasks t JOIN messages m ON m.id=t.message_id WHERE m.bot_id=$1 AND m.reply_to_message_id=$2`, botID, replyTo).Scan(&seq)
		if seq > qqReplyLimit(convType, false) {
			replyTo = ""
			deadline = nil
			seq = 1
		}
	}

	tx, err := a.db.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)

	var msgID string
	if err = tx.QueryRow(ctx, `INSERT INTO messages(channel,bot_id,conversation_id,direction,event_type,content,parts,reply_to_message_id,status,reply_deadline)
		VALUES('qq',$1,$2,'outbound','PROACTIVE_CARE',$3,'[]'::jsonb,$4,'pending',$5) RETURNING id::text`,
		botID, convID, draft, nullString(replyTo), deadline).Scan(&msgID); err != nil {
		return err
	}
	if _, err = tx.Exec(ctx, "INSERT INTO outbox_tasks(message_id,msg_seq) VALUES($1,$2)", msgID, seq); err != nil {
		return err
	}
	if _, err = tx.Exec(ctx, "UPDATE proactive_triggers SET status='sent',sent_message_id=$2,updated_at=now() WHERE id=$1", triggerID, msgID); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

// --- admin API ---

func (a *App) listProactiveTriggersAPI(c *gin.Context) {
	status := c.DefaultQuery("status", "all")
	queryList(c, a, `SELECT p.id::text,p.subject_id::text AS "subjectId",COALESCE(s.display_name,'') AS "subjectName",
		p.conversation_id::text AS "conversationId",COALESCE(NULLIF(cv.name,''),cv.platform_id,'') AS "conversationName",
		b.name AS "botName",p.kind,p.topic,p.scheduled_at AS "scheduledAt",p.status,p.skip_reason AS "skipReason",
		p.sent_message_id::text AS "sentMessageId",p.created_at AS "createdAt"
		FROM proactive_triggers p JOIN bots b ON b.id=p.bot_id JOIN conversations cv ON cv.id=p.conversation_id
		LEFT JOIN memory_subjects s ON s.id=p.subject_id
		WHERE ($1='all' OR p.status=$1) ORDER BY p.scheduled_at DESC LIMIT 200`, status)
}

func (a *App) cancelProactiveTriggerAPI(c *gin.Context) {
	tag, err := a.db.Exec(c, "UPDATE proactive_triggers SET status='cancelled',updated_at=now() WHERE id=$1 AND status='pending'", c.Param("id"))
	if err != nil || tag.RowsAffected() == 0 {
		fail(c, 404, "not_found", "待发送的主动关怀不存在或已处理")
		return
	}
	ok(c, gin.H{"cancelled": true})
}
