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
func (a *App) handoffTurn(ctx context.Context, bot, conv, user, name, mid, reason string, pause time.Time, create bool) (bool, error) {
	tx, err := a.db.Begin(ctx)
	if err != nil {
		return false, err
	}
	defer tx.Rollback(ctx)
	var existing *string
	if err = tx.QueryRow(ctx, "SELECT m.handoff_id::text FROM conversations c JOIN messages m ON m.conversation_id=c.id WHERE m.id=$1 FOR UPDATE OF c,m", mid).Scan(&existing); err != nil {
		return false, err
	}
	if existing != nil {
		return true, nil
	}
	var id string
	err = tx.QueryRow(ctx, "SELECT id::text FROM handoffs WHERE conversation_id=$1 AND platform_user_id=$2 AND status='open' AND paused_until>now() ORDER BY created_at DESC LIMIT 1 FOR UPDATE", conv, user).Scan(&id)
	if errors.Is(err, pgx.ErrNoRows) {
		if !create {
			return false, nil
		}
		err = tx.QueryRow(ctx, "INSERT INTO handoffs(bot_id,conversation_id,platform_user_id,display_name,message_id,reason,paused_until) VALUES($1,$2,$3,$4,$5,$6,$7) RETURNING id::text", bot, conv, user, name, mid, reason, pause).Scan(&id)
	}
	if err != nil {
		return false, err
	}
	if _, err = tx.Exec(ctx, "UPDATE messages SET handoff_id=$2 WHERE id=$1", mid, id); err != nil {
		return false, err
	}
	_, err = tx.Exec(ctx, `WITH notes AS (
 INSERT INTO messages(channel,bot_id,conversation_id,direction,event_type,content,status,context_excluded,handoff_id)
 SELECT 'qq',h.bot_id,op.id,'outbound','HANDOFF_NOTICE',
 format('人工工单 #%s｜%s｜%s（%s）',h.ticket_number,c.name,h.display_name,h.platform_user_id)
 ||E'\n转人工原因：'||h.reason||COALESCE((SELECT E'\n最近发言：'||string_agg(x.body,E'\n' ORDER BY x.event_at) FROM
 (SELECT left(prev.content,240) AS body,prev.event_at FROM messages prev WHERE prev.conversation_id=c.id AND prev.sender_id=h.platform_user_id AND prev.direction='inbound' AND NOT prev.context_excluded AND prev.status<>'retracted' AND prev.event_at>COALESCE(c.moderation_cutoff,'-infinity') AND prev.event_at BETWEEN m.event_at-interval '30 minutes' AND m.event_at AND prev.id<>m.id AND prev.content<>'' ORDER BY prev.event_at DESC LIMIT 2) x),'')
 ||E'\n用户消息：'||m.content||CASE WHEN m.parts @> '[{"type":"image"}]' THEN ' [图片见网页人工台]' ELSE '' END||E'\n回复格式：#'||h.ticket_number||' 回复内容',
 'pending',true,h.id FROM handoffs h JOIN conversations c ON c.id=h.conversation_id JOIN messages m ON m.id=$2
 JOIN conversations op ON op.bot_id=h.bot_id AND op.human_operator AND op.id<>c.id WHERE h.id=$1
 RETURNING id) INSERT INTO outbox_tasks(message_id) SELECT id FROM notes`, id, mid)
	if err != nil {
		return false, err
	}
	return true, tx.Commit(ctx)
}

var errTicketHandled = errors.New("工单已处理或回复正在发送")

func queueHuman(ctx context.Context, tx pgx.Tx, mid, actor, body string) (string, error) {
	var bot, conv, platform string
	var deadline time.Time
	var ticket *string
	err := tx.QueryRow(ctx, `SELECT m.bot_id::text,c.id::text,m.platform_message_id,m.event_at+interval '1 hour',m.handoff_id::text FROM messages m JOIN conversations c ON c.id=m.conversation_id WHERE m.id=$1 AND direction='inbound' AND platform_message_id IS NOT NULL FOR UPDATE OF c,m`, mid).Scan(&bot, &conv, &platform, &deadline, &ticket)
	if err != nil {
		return "", err
	}
	if ticket != nil {
		var available bool
		err = tx.QueryRow(ctx, `SELECT h.status='open' AND (h.reply_id IS NULL OR EXISTS(SELECT FROM outbox_tasks WHERE message_id=h.reply_id AND status IN ('failed','expired'))) FROM handoffs h WHERE h.id=$1 FOR UPDATE`, *ticket).Scan(&available)
		if err != nil {
			return "", err
		}
		if !available {
			return "", errTicketHandled
		}
	}
	var id string
	err = tx.QueryRow(ctx, `INSERT INTO messages(channel,bot_id,conversation_id,direction,event_type,sender_name,content,reply_to_message_id,status,reply_deadline,handoff_id)
 VALUES('qq',$1,$2,'outbound','HUMAN_REPLY',$3,$4,$5,'pending',$6,$7) RETURNING id::text`, bot, conv, actor, body, platform, deadline, ticket).Scan(&id)
	if err != nil {
		return "", err
	}
	_, err = tx.Exec(ctx, `INSERT INTO outbox_tasks(message_id,msg_seq) SELECT $1,COALESCE(max(t.msg_seq),1)+1 FROM outbox_tasks t JOIN messages m ON m.id=t.message_id WHERE m.conversation_id=$2 AND m.reply_to_message_id=$3`, id, conv, platform)
	if err == nil && ticket != nil {
		_, err = tx.Exec(ctx, "UPDATE handoffs SET reply_id=$1 WHERE id=$2", id, *ticket)
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
	hint := "请按通知中的格式回复：#工单号 回复内容"
	if len(fields) >= 2 && strings.HasPrefix(fields[0], "#") {
		number, e := strconv.ParseInt(strings.TrimPrefix(fields[0], "#"), 10, 64)
		if e == nil {
			var target string
			err := tx.QueryRow(ctx, "SELECT message_id::text FROM handoffs WHERE ticket_number=$1 AND bot_id=$2", number, msg.BotID).Scan(&target)
			switch {
			case errors.Is(err, pgx.ErrNoRows):
				hint = "未找到此工单"
			case err != nil:
				return false, err
			default:
				_, err = queueHuman(ctx, tx, target, firstNonEmpty(msg.SenderName, msg.SenderID), strings.TrimSpace(strings.TrimPrefix(input, fields[0])))
				if err != nil && !errors.Is(err, errTicketHandled) {
					return false, err
				}
				hint = fmt.Sprintf("工单 #%d 回复已排队", number)
				if err != nil {
					hint = err.Error()
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

const handoffStatusColumns = `hs.state AS "handoffStatus",h.ticket_number::text AS "ticketNumber",CASE WHEN h.status='replied' THEN h.resolved_at END AS "humanRepliedAt",hr.sender_name AS "humanRepliedBy",hr.content AS "humanReplyContent",hot.status AS "humanDeliveryStatus",hot.last_error AS "humanDeliveryError"`
const handoffStatusJoin = `LEFT JOIN handoffs h ON h.id=m.handoff_id LEFT JOIN messages hr ON hr.id=h.reply_id LEFT JOIN outbox_tasks hot ON hot.message_id=h.reply_id
 LEFT JOIN LATERAL (SELECT CASE h.status WHEN 'open' THEN 'waiting_human' WHEN 'replied' THEN 'human_replied' WHEN 'resolved' THEN 'human_closed' END AS state) hs ON true`
