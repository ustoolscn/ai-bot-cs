package app

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"ai-bot/backend/internal/domain"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

func (a *App) operatorRoutes(r *gin.RouterGroup) {
	r.GET("/bots/:id/human-operators", func(c *gin.Context) {
		queryList(c, a, `SELECT id::text AS "conversationId",platform_id AS "userOpenId",name,human_operator AS enabled FROM conversations WHERE bot_id=$1 AND type='private' ORDER BY name,id`, c.Param("id"))
	})
	r.PUT("/bots/:id/human-operators/:item", func(c *gin.Context) {
		var in struct{ Enabled *bool }
		if c.ShouldBindJSON(&in) != nil || in.Enabled == nil || uuid.Validate(c.Param("item")) != nil {
			fail(c, 400, "invalid_request", "请选择私聊会话并填写 enabled")
			return
		}
		tag, err := a.db.Exec(c, "UPDATE conversations SET human_operator=$3 WHERE id=$2 AND bot_id=$1 AND type='private'", c.Param("id"), c.Param("item"), *in.Enabled)
		if err != nil {
			fail(c, 500, "database_error", "保存客服失败")
			return
		}
		if tag.RowsAffected() == 0 {
			fail(c, 404, "not_found", "私聊会话不存在")
			return
		}
		ok(c, gin.H{"enabled": *in.Enabled})
	})
}

// Link each inbound turn once. The notification and its outbox entry commit together.
func (a *App) handoffTurn(ctx context.Context, bot, conv, user, name, mid, reason string, idleMinutes int) (bool, error) {
	tx, err := a.db.Begin(ctx)
	if err != nil {
		return false, err
	}
	defer tx.Rollback(ctx)
	handled, err := linkHandoffTurn(ctx, tx, bot, conv, user, name, mid, reason, idleMinutes)
	if _, closed := err.(hoursClosed); closed {
		if commitErr := tx.Commit(ctx); commitErr != nil {
			return false, commitErr
		}
		return false, err
	}
	if err != nil {
		return false, err
	}
	return handled, tx.Commit(ctx)
}

func linkHandoffTurn(ctx context.Context, tx pgx.Tx, bot, conv, user, name, mid, reason string, idleMinutes int) (bool, error) {
	var existing *string
	if err := tx.QueryRow(ctx, "SELECT m.handoff_id::text FROM conversations c JOIN messages m ON m.conversation_id=c.id WHERE m.id=$1 FOR NO KEY UPDATE OF c,m", mid).Scan(&existing); err != nil {
		return false, err
	}
	if existing != nil {
		return true, nil
	}
	var id string
	var expired bool
	err := tx.QueryRow(ctx, "SELECT id::text,coalesce(paused_until<=now(),false) FROM handoffs WHERE conversation_id=$1 AND platform_user_id=$2 AND status IN ('open','processing') AND ($4::boolean OR created_at <= (SELECT created_at FROM messages WHERE id=$3)) FOR NO KEY UPDATE", conv, user, mid, idleMinutes > 0).Scan(&id, &expired)
	if err == nil && expired {
		if e := closeTicket(ctx, tx, id, "timeout"); e != nil {
			return false, e
		}
		err = pgx.ErrNoRows
	}
	if errors.Is(err, pgx.ErrNoRows) {
		if idleMinutes == 0 {
			return false, nil
		}
		var hours handoffHours
		err = tx.QueryRow(ctx, "SELECT coalesce(s.handoff_hours,'{}'::jsonb) FROM conversations c LEFT JOIN bot_services s ON s.id=c.service_id WHERE c.id=$1", conv).Scan(&hours)
		if err != nil {
			return false, err
		}
		if !hours.open(time.Now()) {
			return false, hours.notice()
		}
		err = tx.QueryRow(ctx, `INSERT INTO handoffs(bot_id,conversation_id,platform_user_id,display_name,message_id,reason,paused_until,idle_minutes) VALUES($1,$2,$3,$4,$5,$6,now()+$7::int*interval '1 minute',$7) RETURNING id::text`, bot, conv, user, name, mid, reason, idleMinutes).Scan(&id)
	}
	if err != nil {
		return false, err
	}
	if _, err = tx.Exec(ctx, "UPDATE messages SET handoff_id=$2 WHERE id=$1", mid, id); err != nil {
		return false, err
	}
	if _, err = tx.Exec(ctx, "UPDATE handoffs SET updated_at=now(),paused_until=now()+idle_minutes*interval '1 minute' WHERE id=$1", id); err != nil {
		return false, err
	}
	_, err = tx.Exec(ctx, `WITH notes AS (
	INSERT INTO messages(channel,bot_id,conversation_id,direction,event_type,content,parts,status,context_excluded,handoff_id)
	SELECT 'qq',h.bot_id,op.id,'outbound','HANDOFF_NOTICE',format('工单 #%s｜%s｜%s（%s）',h.ticket_number,c.name,h.display_name,h.platform_user_id)
	|| E'\n转人工原因：'||display_mentions(m,h.reason)|| E'\n用户消息：'||display_mentions(m,m.content)||CASE WHEN m.parts @> '[{"type":"image"}]' THEN ' [图片见网页人工台]' ELSE '' END
	||CASE WHEN h.status='open' THEN format(E'\n请先发送 #%s 接单',h.ticket_number) ELSE format(E'\n回复：#%s 内容；结单：#%s 结单',h.ticket_number,h.ticket_number) END,
	m.parts,'pending',true,h.id FROM handoffs h JOIN conversations c ON c.id=h.conversation_id JOIN messages m ON m.id=$2
	JOIN conversations op ON op.bot_id=h.bot_id AND op.human_operator AND op.id<>c.id
	WHERE h.id=$1 AND (h.status='open' OR (h.claimed_via='qq' AND h.claimed_by_id=op.id))
	RETURNING id) INSERT INTO outbox_tasks(message_id) SELECT id FROM notes`, id, mid)
	return true, err
}

type ticketActor struct{ via, id, name string }
type ticketConflict string

func (e ticketConflict) Error() string { return string(e) }

type ticketState struct {
	number                   int64
	status, via, owner, name string
	expired                  bool
}

func lockTicket(ctx context.Context, tx pgx.Tx, id string) (ticketState, error) {
	var t ticketState
	// Keep the conversation-before-ticket lock order used by message ingestion.
	if _, err := tx.Exec(ctx, "SELECT c.id FROM conversations c JOIN handoffs h ON h.conversation_id=c.id WHERE h.id=$1 FOR NO KEY UPDATE OF c", id); err != nil {
		return t, err
	}
	err := tx.QueryRow(ctx, `SELECT ticket_number,status,coalesce(claimed_via,''),coalesce(claimed_by_id::text,''),coalesce(claimed_by_name,''),coalesce(paused_until<=now(),false) FROM handoffs WHERE id=$1 FOR NO KEY UPDATE`, id).Scan(&t.number, &t.status, &t.via, &t.owner, &t.name, &t.expired)
	return t, err
}

func (t ticketState) authorize(actor ticketActor, claim bool) error {
	switch {
	case t.status == "resolved" || t.expired:
		return ticketConflict(fmt.Sprintf("#%d 已结单", t.number))
	case t.status == "open" && !claim:
		return ticketConflict(fmt.Sprintf("请先发送 #%d 接单", t.number))
	case t.status == "processing" && (actor.via != t.via || actor.id != t.owner):
		return ticketConflict(fmt.Sprintf("#%d 已由 %s 接单", t.number, t.name))
	}
	return nil
}

func ticketAction(ctx context.Context, tx pgx.Tx, id string, who ticketActor, claim bool) error {
	t, err := lockTicket(ctx, tx, id)
	if err != nil {
		return err
	}
	if err = t.authorize(who, claim); err != nil {
		return err
	}
	if !claim {
		return closeTicket(ctx, tx, id, who.via)
	}
	if t.status == "processing" {
		return nil
	}
	_, err = tx.Exec(ctx, `UPDATE handoffs SET status='processing',claimed_by_id=$2,claimed_via=$3,claimed_by_name=$4,claimed_at=now(),updated_at=now(),paused_until=now()+idle_minutes*interval '1 minute' WHERE id=$1`, id, who.id, who.via, who.name)
	if err != nil {
		return err
	}
	if err = ticketUserNotice(ctx, tx, id, "人工客服已接入，请稍等。"); err != nil {
		return err
	}
	_, err = tx.Exec(ctx, `WITH notes AS(INSERT INTO messages(channel,bot_id,conversation_id,direction,event_type,content,status,context_excluded,handoff_id)
	SELECT 'qq',h.bot_id,op.id,'outbound','HANDOFF_NOTICE',format('#%s 已由 %s 接单',h.ticket_number,h.claimed_by_name),'pending',true,h.id
	FROM handoffs h JOIN conversations op ON op.bot_id=h.bot_id AND op.human_operator WHERE h.id=$1 AND (h.claimed_via<>'qq' OR op.id<>h.claimed_by_id) RETURNING id)
	INSERT INTO outbox_tasks(message_id) SELECT id FROM notes`, id)
	return err
}

// The latest user message anchors passive QQ delivery, with active fallback
// after the platform window or passive reply quota has been exhausted.
func ticketUserNotice(ctx context.Context, tx pgx.Tx, id, body string) error {
	_, err := tx.Exec(ctx, `WITH note AS(INSERT INTO messages(channel,bot_id,conversation_id,direction,event_type,content,status,context_excluded,handoff_id,reply_to_message_id,reply_deadline)
	SELECT 'qq',h.bot_id,h.conversation_id,'outbound','HANDOFF_UPDATE',$2,'pending',true,h.id,m.platform_message_id,m.event_at+interval '1 hour'
	FROM handoffs h LEFT JOIN LATERAL(SELECT platform_message_id,event_at FROM messages WHERE handoff_id=h.id AND direction='inbound' ORDER BY event_at DESC LIMIT 1)m ON true WHERE h.id=$1
	RETURNING id,conversation_id,reply_to_message_id)
	INSERT INTO outbox_tasks(message_id,msg_seq) SELECT n.id,coalesce((SELECT max(t.msg_seq) FROM outbox_tasks t JOIN messages m ON m.id=t.message_id WHERE m.conversation_id=n.conversation_id AND m.reply_to_message_id=n.reply_to_message_id),0)+1 FROM note n`, id, body)
	return err
}

func closeTicket(ctx context.Context, tx pgx.Tx, id, method string) error {
	var owner, via, name string
	var number int64
	err := tx.QueryRow(ctx, `UPDATE handoffs SET status='resolved',resolved_at=now(),paused_until=now(),updated_at=now(),close_method=$2,resolved_by=CASE WHEN $2='web' THEN claimed_by_id END WHERE id=$1
	RETURNING coalesce(claimed_by_id::text,''),coalesce(claimed_via,''),coalesce(claimed_by_name,''),ticket_number`, id, method).Scan(&owner, &via, &name, &number)
	if err != nil {
		return err
	}
	body := "本次人工服务已结束，机器人将继续为您服务。"
	if method == "timeout" && owner == "" {
		body = "人工客服暂时未能接单，先由机器人继续为您服务。"
	}
	if err = ticketUserNotice(ctx, tx, id, body); err != nil {
		return err
	}
	if method == "timeout" && via == "qq" {
		_, err = tx.Exec(ctx, `WITH note AS(INSERT INTO messages(channel,bot_id,conversation_id,direction,event_type,content,status,context_excluded,handoff_id) SELECT 'qq',h.bot_id,op.id,'outbound','HANDOFF_NOTICE',format('工单 #%s 因双方长时间未发言，已自动结单。',h.ticket_number),'pending',true,h.id FROM handoffs h JOIN conversations op ON op.id=h.claimed_by_id AND op.human_operator WHERE h.id=$1 RETURNING id) INSERT INTO outbox_tasks(message_id) SELECT id FROM note`, id)
	} else if method == "timeout" && via == "web" {
		_, err = tx.Exec(ctx, `INSERT INTO system_alerts(kind,title,detail) VALUES('handoff_timeout',$1,jsonb_build_object('handoffId',$2::text,'userId',$3::text))`, fmt.Sprintf("%s 的工单 #%d 已超时结单", name, number), id, owner)
	}
	return err
}

func expireTicket(ctx context.Context, tx pgx.Tx, id string) (bool, error) {
	t, err := lockTicket(ctx, tx, id)
	if err != nil || !t.expired || t.status == "resolved" {
		return false, err
	}
	return true, closeTicket(ctx, tx, id, "timeout")
}

func (a *App) expireHandoffs(ctx context.Context) error {
	rows, err := a.db.Query(ctx, "SELECT id::text FROM handoffs WHERE status IN ('open','processing') AND paused_until<=now() ORDER BY paused_until")
	if err != nil {
		return err
	}
	ids, err := pgx.CollectRows(rows, pgx.RowTo[string])
	if err != nil {
		return err
	}
	for _, id := range ids {
		err = pgx.BeginFunc(ctx, a.db, func(tx pgx.Tx) error { _, e := expireTicket(ctx, tx, id); return e })
		if err != nil && !errors.Is(err, pgx.ErrNoRows) {
			return err
		}
	}
	return nil
}

func queueHuman(ctx context.Context, tx pgx.Tx, mid string, actor ticketActor, body string) (string, error) {
	var bot, conv, platform string
	var deadline time.Time
	var ticket *string
	err := tx.QueryRow(ctx, `SELECT m.bot_id::text,c.id::text,m.platform_message_id,m.event_at+interval '1 hour',coalesce(m.handoff_id,(SELECT h.id FROM handoffs h WHERE h.conversation_id=c.id AND h.platform_user_id=m.sender_id AND h.status IN ('open','processing') AND h.paused_until>now()))::text FROM messages m JOIN conversations c ON c.id=m.conversation_id WHERE m.id=$1 AND direction='inbound' AND platform_message_id IS NOT NULL FOR NO KEY UPDATE OF c,m`, mid).Scan(&bot, &conv, &platform, &deadline, &ticket)
	if err != nil {
		return "", err
	}
	if ticket != nil {
		t, e := lockTicket(ctx, tx, *ticket)
		if e != nil {
			return "", e
		}
		if e = t.authorize(actor, false); e != nil {
			return "", e
		}
	}
	var id string
	err = tx.QueryRow(ctx, `INSERT INTO messages(channel,bot_id,conversation_id,direction,event_type,sender_name,content,reply_to_message_id,status,reply_deadline,handoff_id)
 VALUES('qq',$1,$2,'outbound','HUMAN_REPLY',$3,$4,$5,'pending',$6,$7) RETURNING id::text`, bot, conv, actor.name, body, platform, deadline, ticket).Scan(&id)
	if err != nil {
		return "", err
	}
	_, err = tx.Exec(ctx, `INSERT INTO outbox_tasks(message_id,msg_seq) SELECT $1,COALESCE(max(t.msg_seq),1)+1 FROM outbox_tasks t JOIN messages m ON m.id=t.message_id WHERE m.conversation_id=$2 AND m.reply_to_message_id=$3`, id, conv, platform)
	if err == nil && ticket != nil {
		_, err = tx.Exec(ctx, "UPDATE handoffs SET reply_id=$1,updated_at=now(),paused_until=now()+idle_minutes*interval '1 minute' WHERE id=$2", id, *ticket)
	}
	return id, err
}

func (a *App) operatorCommand(ctx context.Context, tx pgx.Tx, msg *domain.InboundMessage, cid, mid string) (bool, error) {
	if msg.ConversationType != "private" {
		return false, nil
	}
	var enabled, handled bool
	if err := tx.QueryRow(ctx, "SELECT c.human_operator,m.context_excluded FROM conversations c JOIN messages m ON m.conversation_id=c.id WHERE c.id=$1 AND m.id=$2", cid, mid).Scan(&enabled, &handled); err != nil {
		return false, err
	}
	if !enabled {
		return false, nil
	}
	if handled {
		return true, nil
	}
	input := strings.TrimSpace(msg.Text)
	fields := strings.Fields(input)
	hint := "请先发送 #工单号 接单，再用 #工单号 内容 回复，结束时发送 #工单号 结单。"
	if len(fields) >= 2 && strings.HasPrefix(fields[0], "#") {
		number, e := strconv.ParseInt(strings.TrimPrefix(fields[0], "#"), 10, 64)
		if e == nil {
			var target, id string
			err := tx.QueryRow(ctx, "SELECT coalesce((SELECT m.id::text FROM messages m WHERE m.handoff_id=h.id AND direction='inbound' ORDER BY event_at DESC LIMIT 1),''),id::text FROM handoffs h WHERE ticket_number=$1 AND bot_id=$2", number, msg.BotID).Scan(&target, &id)
			switch {
			case errors.Is(err, pgx.ErrNoRows):
				hint = "未找到此工单"
			case err != nil:
				return false, err
			default:
				actor := ticketActor{"qq", cid, firstNonEmpty(msg.SenderName, msg.SenderID)}
				body := strings.TrimSpace(strings.TrimPrefix(input, fields[0]))
				switch body {
				case "接单":
					err = ticketAction(ctx, tx, id, actor, true)
					hint = fmt.Sprintf("已接单 #%d", number)
				case "结单", "结束":
					err = ticketAction(ctx, tx, id, actor, false)
					hint = fmt.Sprintf("#%d 已结单", number)
				default:
					if target == "" {
						err = ticketConflict(fmt.Sprintf("#%d 的原消息已过期，无法回复", number))
					} else {
						_, err = queueHuman(ctx, tx, target, actor, body)
					}
					hint = fmt.Sprintf("工单 #%d 回复已排队", number)
				}
				var conflict ticketConflict
				if errors.As(err, &conflict) {
					hint = err.Error()
				} else if err != nil {
					return false, err
				}
			}
		}
	}
	if _, err := tx.Exec(ctx, "UPDATE messages SET context_excluded=true,context_exclusion_reason='human_operator' WHERE id=$1", mid); err != nil {
		return false, err
	}
	_, err := tx.Exec(ctx, `WITH ack AS(INSERT INTO messages(channel,bot_id,conversation_id,direction,event_type,content,status,reply_to_message_id,reply_deadline,context_excluded)
 VALUES('qq',$1,$2,'outbound','OPERATOR_ACK',$3,'pending',$4,$5,true) RETURNING id) INSERT INTO outbox_tasks(message_id) SELECT id FROM ack`, msg.BotID, cid, hint, msg.PlatformMessageID, msg.ReplyDeadline)
	return true, err
}

const handoffStatusColumns = `hs.state AS "handoffStatus",h.ticket_number::text AS "ticketNumber",CASE WHEN hot.status='sent' THEN hot.updated_at END AS "humanRepliedAt",hr.sender_name AS "humanRepliedBy",hr.content AS "humanReplyContent",hot.status AS "humanDeliveryStatus",hot.last_error AS "humanDeliveryError"`
const handoffStatusJoin = `LEFT JOIN handoffs h ON h.id=m.handoff_id LEFT JOIN messages hr ON hr.id=h.reply_id LEFT JOIN outbox_tasks hot ON hot.message_id=h.reply_id
 LEFT JOIN LATERAL (SELECT CASE h.status WHEN 'open' THEN 'waiting_human' WHEN 'processing' THEN 'human_processing' WHEN 'resolved' THEN 'human_closed' END AS state) hs ON true`
