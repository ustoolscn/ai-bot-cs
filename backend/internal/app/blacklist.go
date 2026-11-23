package app

import (
	"context"
	"strings"

	"ai-bot/backend/internal/qq"
	"github.com/gin-gonic/gin"
	"github.com/jackc/pgx/v5"
)

func (a *App) listBlockedUsers(c *gin.Context) {
	queryList(c, a, `SELECT bu.id::text,bu.bot_id::text AS "botId",b.name AS "botName",bu.conversation_id::text AS "conversationId",bu.platform_user_id AS "userId",bu.reason,bu.auto_kick AS "autoKick",bu.created_at AS "createdAt",
 COALESCE((SELECT display_name FROM conversation_members cm JOIN conversations cc ON cc.id=cm.conversation_id WHERE cc.bot_id=bu.bot_id AND cm.platform_user_id=bu.platform_user_id AND (bu.conversation_id IS NULL OR cm.conversation_id=bu.conversation_id) ORDER BY cm.last_seen_at DESC LIMIT 1),'') AS "displayName",
 COALESCE((SELECT jsonb_agg(jsonb_build_object('id',ba.id,'conversationId',ba.conversation_id,'status',ba.status,'attempts',ba.attempts,'error',ba.last_error,'lastAttemptAt',ba.last_attempt_at,'updatedAt',ba.updated_at) ORDER BY ba.updated_at DESC) FROM blacklist_actions ba WHERE ba.blocked_user_id=bu.id),'[]') AS actions
 FROM blocked_users bu JOIN bots b ON b.id=bu.bot_id WHERE ($1='' OR bu.bot_id::text=$1) AND ($2='' OR bu.conversation_id::text=$2 OR (bu.conversation_id IS NULL AND EXISTS(SELECT 1 FROM conversations fc WHERE fc.id::text=$2 AND fc.bot_id=bu.bot_id))) ORDER BY bu.created_at DESC LIMIT 500`, c.Query("botId"), c.Query("conversationId"))
}

func (a *App) addBlockedUser(c *gin.Context) {
	var in struct {
		BotID          string `json:"botId"`
		ConversationID string `json:"conversationId"`
		UserID         string `json:"userId"`
		Reason         string `json:"reason"`
		AutoKick       *bool  `json:"autoKick"`
	}
	if c.ShouldBindJSON(&in) != nil || parseUUID(in.BotID) != nil || (in.ConversationID != "" && parseUUID(in.ConversationID) != nil) {
		fail(c, 400, "invalid_request", "botId 和 conversationId 必须是有效ID")
		return
	}
	in.UserID = strings.TrimSpace(in.UserID)
	in.Reason = strings.TrimSpace(in.Reason)
	if in.UserID == "" || len(in.UserID) > 256 || len([]rune(in.Reason)) > 1000 {
		fail(c, 400, "invalid_request", "请选择成员，原因不超过1000字")
		return
	}
	autoKick := true
	if in.AutoKick != nil {
		autoKick = *in.AutoKick
	}
	tx, err := a.db.Begin(c)
	if err != nil {
		fail(c, 500, "database_error", "保存失败")
		return
	}
	defer tx.Rollback(c)
	// Serialize bot-wide and group-scoped edits for this platform identity.
	if _, err = tx.Exec(c, `SELECT pg_advisory_xact_lock(hashtextextended($1::text,0))`, in.BotID+":"+in.UserID); err != nil {
		fail(c, 500, "database_error", "保存失败")
		return
	}
	var known bool
	err = tx.QueryRow(c, `SELECT EXISTS(SELECT 1 FROM conversation_members cm JOIN conversations cc ON cc.id=cm.conversation_id WHERE cc.bot_id=$1 AND cc.type='group' AND cm.platform_user_id=$2 AND ($3='' OR cc.id::text=$3))`, in.BotID, in.UserID, in.ConversationID).Scan(&known)
	if err != nil || !known {
		fail(c, 400, "unknown_member", "只能选择本机器人已见过的群成员（member_openid），不能直接输入QQ号或按昵称匹配")
		return
	}
	var id string
	err = tx.QueryRow(c, `SELECT id::text FROM blocked_users WHERE bot_id=$1 AND platform_user_id=$2 AND conversation_id IS NOT DISTINCT FROM $3::uuid LIMIT 1`, in.BotID, in.UserID, nullString(in.ConversationID)).Scan(&id)
	if err == pgx.ErrNoRows {
		err = tx.QueryRow(c, `INSERT INTO blocked_users(bot_id,conversation_id,platform_user_id,reason,auto_kick) VALUES($1,$2,$3,$4,$5) RETURNING id::text`, in.BotID, nullString(in.ConversationID), in.UserID, in.Reason, autoKick).Scan(&id)
	} else if err == nil {
		_, err = tx.Exec(c, `UPDATE blocked_users SET reason=$1,auto_kick=$2 WHERE id=$3`, in.Reason, autoKick, id)
	}
	if err == nil {
		if !autoKick {
			_, err = tx.Exec(c, `UPDATE blacklist_actions SET status='disabled',updated_at=now() WHERE blocked_user_id=$1`, id)
		} else {
			_, err = tx.Exec(c, `UPDATE blacklist_actions SET last_attempt_at=to_timestamp(0) WHERE blocked_user_id=$1 AND status='failed'`, id)
		}
	}
	if err == nil {
		err = a.excludeBlockedHistory(c, tx, in.BotID, in.ConversationID, in.UserID)
	}
	if err == nil && autoKick {
		rows, e := tx.Query(c, `SELECT cc.id::text FROM conversations cc JOIN conversation_members cm ON cm.conversation_id=cc.id WHERE cc.bot_id=$1 AND cc.type='group' AND cm.platform_user_id=$2 AND cm.active AND ($3='' OR cc.id::text=$3)`, in.BotID, in.UserID, in.ConversationID)
		if e != nil {
			err = e
		} else {
			ids, e := pgx.CollectRows(rows, pgx.RowTo[string])
			err = e
			for _, cid := range ids {
				if err == nil {
					err = a.queueBlockedKick(c, tx, in.BotID, cid, in.UserID)
				}
			}
		}
	}
	if err == nil {
		err = tx.Commit(c)
	}
	if err != nil {
		fail(c, 500, "database_error", "保存失败")
		return
	}
	a.audit(c, "blacklist.add", "blocked_user", id, in)
	ok(c, gin.H{"id": id, "autoKick": autoKick, "identityType": "member_openid"})
}

func (a *App) deleteBlockedUser(c *gin.Context) {
	if parseUUID(c.Param("id")) != nil {
		fail(c, 400, "invalid_request", "ID无效")
		return
	}
	tag, err := a.db.Exec(c, `DELETE FROM blocked_users WHERE id=$1`, c.Param("id"))
	if err != nil {
		fail(c, 500, "database_error", "删除失败")
		return
	}
	if tag.RowsAffected() == 0 {
		fail(c, 404, "not_found", "黑名单记录不存在")
		return
	}
	a.audit(c, "blacklist.delete", "blocked_user", c.Param("id"), nil)
	ok(c, gin.H{"deleted": true})
}

func (a *App) excludeBlockedHistory(ctx context.Context, tx pgx.Tx, botID, convID, userID string) error {
	// Existing messages remain visible for auditing but can no longer reach the model.
	_, err := tx.Exec(ctx, `UPDATE messages SET context_excluded=true,context_exclusion_reason='blacklist' WHERE bot_id=$1 AND sender_id=$2 AND direction='inbound' AND ($3='' OR conversation_id::text=$3)`, botID, userID, convID)
	if err != nil {
		return err
	}
	_, err = tx.Exec(ctx, `UPDATE messages o SET context_excluded=true,context_exclusion_reason='blacklist' WHERE o.bot_id=$1 AND o.direction='outbound' AND EXISTS(SELECT 1 FROM messages i WHERE i.conversation_id=o.conversation_id AND i.platform_message_id=o.reply_to_message_id AND i.sender_id=$2 AND ($3='' OR i.conversation_id::text=$3))`, botID, userID, convID)
	if err != nil {
		return err
	}
	_, err = tx.Exec(ctx, `DELETE FROM conversation_summaries WHERE conversation_id IN(SELECT conversation_id FROM conversation_members cm JOIN conversations cc ON cc.id=cm.conversation_id WHERE cc.bot_id=$1 AND cm.platform_user_id=$2 AND ($3='' OR cc.id::text=$3))`, botID, userID, convID)
	if err != nil {
		return err
	}
	_, err = tx.Exec(ctx, `DELETE FROM conversation_staff_styles WHERE conversation_id IN(SELECT conversation_id FROM conversation_members cm JOIN conversations cc ON cc.id=cm.conversation_id WHERE cc.bot_id=$1 AND cm.platform_user_id=$2 AND ($3='' OR cc.id::text=$3))`, botID, userID, convID)
	if err != nil {
		return err
	}
	if _, err = tx.Exec(ctx, `DELETE FROM user_profiles p USING conversations c WHERE p.conversation_id=c.id AND c.bot_id=$1 AND p.platform_user_id=$2 AND ($3='' OR c.id::text=$3)`, botID, userID, convID); err != nil {
		return err
	}
	_, err = tx.Exec(ctx, `UPDATE user_memories SET status='archived',updated_at=now() WHERE bot_id=$1 AND platform_user_id=$2 AND ($3='' OR conversation_id::text=$3)`, botID, userID, convID)
	return err
}

// queueBlockedKick is called inside the event transaction: events and actions are atomic.
func (a *App) queueBlockedKick(ctx context.Context, tx pgx.Tx, botID, convID, userID string) error {
	var actionID string
	err := tx.QueryRow(ctx, `INSERT INTO blacklist_actions(blocked_user_id,conversation_id)
 SELECT id,$2 FROM blocked_users WHERE bot_id=$1 AND platform_user_id=$3 AND auto_kick AND (conversation_id IS NULL OR conversation_id=$2)
 ORDER BY conversation_id NULLS LAST LIMIT 1
 ON CONFLICT(blocked_user_id,conversation_id) DO UPDATE SET status='pending',updated_at=now()
 WHERE blacklist_actions.status IN ('kicked','disabled') OR (blacklist_actions.status='failed' AND blacklist_actions.last_attempt_at<now()-interval '5 minutes')
 RETURNING id::text`, botID, convID, userID).Scan(&actionID)
	if err == pgx.ErrNoRows {
		return nil
	}
	if err != nil {
		return err
	}
	_, err = tx.Exec(ctx, `INSERT INTO agent_jobs(kind,payload,dedupe_key) VALUES('blacklist_kick',jsonb_build_object('actionId',$1::text),$2) ON CONFLICT (dedupe_key) WHERE dedupe_key IS NOT NULL AND status IN ('pending','processing') DO NOTHING`, actionID, "blacklist_kick:"+actionID)
	return err
}

func (a *App) jobBlacklistKick(ctx context.Context, actionID string) error {
	var botID, convID, groupID, userID, appID, encrypted string
	err := a.db.QueryRow(ctx, `SELECT bu.bot_id::text,c.id::text,c.platform_id,bu.platform_user_id,b.app_id,b.app_secret_enc FROM blacklist_actions ba JOIN blocked_users bu ON bu.id=ba.blocked_user_id JOIN conversations c ON c.id=ba.conversation_id JOIN bots b ON b.id=bu.bot_id WHERE ba.id=$1 AND ba.status='pending' AND bu.auto_kick AND c.type='group'`, actionID).Scan(&botID, &convID, &groupID, &userID, &appID, &encrypted)
	if err == pgx.ErrNoRows {
		return nil
	}
	if err != nil {
		return err
	}
	_, err = a.db.Exec(ctx, `UPDATE blacklist_actions SET attempts=attempts+1,last_attempt_at=now(),updated_at=now() WHERE id=$1`, actionID)
	if err != nil {
		return err
	}
	secret, err := a.cipher.Decrypt(encrypted)
	if err == nil {
		err = qq.NewClient(appID, secret, a.cfg.QQAPIBaseURL, a.cfg.QQTokenURL).RemoveGroupMember(ctx, groupID, userID)
	}
	if err != nil {
		detail := jsonString(map[string]any{"actionId": actionID, "botId": botID, "conversationId": convID, "userId": userID, "error": err.Error(), "requiresManualAction": true})
		tx, e := a.db.Begin(ctx)
		if e != nil {
			return e
		}
		defer tx.Rollback(ctx)
		if _, e = tx.Exec(ctx, `UPDATE blacklist_actions SET status='failed',last_error=$2,updated_at=now() WHERE id=$1`, actionID, err.Error()); e != nil {
			return e
		}
		if _, e = tx.Exec(ctx, `INSERT INTO system_alerts(kind,title,detail) SELECT 'blacklist_kick_failed','黑名单自动移除失败，请检查QQ权限或手动踢人',$1::jsonb WHERE NOT EXISTS(SELECT 1 FROM system_alerts WHERE kind='blacklist_kick_failed' AND NOT acknowledged AND detail->>'actionId'=$2)`, detail, actionID); e != nil {
			return e
		}
		return tx.Commit(ctx)
	}
	tx, err := a.db.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	if _, err = tx.Exec(ctx, `UPDATE blacklist_actions SET status='kicked',last_error='',updated_at=now() WHERE id=$1`, actionID); err != nil {
		return err
	}
	if _, err = tx.Exec(ctx, `UPDATE conversation_members SET active=false,membership_updated_at=clock_timestamp() WHERE conversation_id=$1 AND platform_user_id=$2`, convID, userID); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

// Message checks fail closed on database errors at every asynchronous boundary.
func (a *App) messageExcluded(ctx context.Context, id string) (bool, error) {
	var excluded bool
	err := a.db.QueryRow(ctx, `SELECT context_excluded OR status='retracted' OR EXISTS(SELECT 1 FROM blocked_users bu WHERE bu.bot_id=m.bot_id AND bu.platform_user_id=m.sender_id AND (bu.conversation_id IS NULL OR bu.conversation_id=m.conversation_id)) FROM messages m WHERE m.id=$1`, id).Scan(&excluded)
	return excluded, err
}
