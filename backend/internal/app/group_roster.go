package app

import (
	"errors"
	"time"

	"ai-bot/backend/internal/qq"
	"github.com/gin-gonic/gin"
	"github.com/jackc/pgx/v5"
)

// A transaction serializes syncs per bot while allowing membership events to
// continue. Apply the snapshot only after QQ has supplied every page.
func (a *App) syncGroupMembers(c *gin.Context) {
	ctx := c.Request.Context()
	tx, err := a.db.Begin(ctx)
	if err != nil {
		fail(c, 500, "database_error", "读取群配置失败")
		return
	}
	defer tx.Rollback(ctx)
	id := c.Param("id")
	var group, appID, secretEnc, botID string
	var synced *time.Time
	var started time.Time
	err = tx.QueryRow(ctx, `SELECT c.platform_id,b.app_id,b.app_secret_enc,b.id::text,c.roster_synced_at,clock_timestamp()
      FROM conversations c JOIN bots b ON b.id=c.bot_id WHERE c.id=$1 AND c.type='group'`, id).Scan(&group, &appID, &secretEnc, &botID, &synced, &started)
	if errors.Is(err, pgx.ErrNoRows) {
		fail(c, 404, "not_found", "群聊不存在")
		return
	}
	if err != nil {
		fail(c, 500, "database_error", "读取群配置失败")
		return
	}
	if synced != nil && started.Sub(*synced) < 5*time.Minute && c.Query("refresh") != "true" {
		ok(c, gin.H{"syncedAt": synced})
		return
	}
	var locked bool
	if err = tx.QueryRow(ctx, "SELECT pg_try_advisory_xact_lock(hashtextextended($1,0))", "roster:"+botID).Scan(&locked); err != nil {
		fail(c, 500, "database_error", "同步失败")
		return
	}
	if !locked {
		fail(c, 409, "sync_busy", "该机器人正在同步成员，请稍后刷新")
		return
	}
	secret, err := a.cipher.Decrypt(secretEnc)
	if err != nil {
		fail(c, 500, "secret_error", "读取机器人凭证失败")
		return
	}
	roster, err := qq.NewClient(appID, secret, a.cfg.QQAPIBaseURL, a.cfg.QQTokenURL).GroupMembers(ctx, group)
	if errors.Is(err, qq.ErrRosterPermission) {
		fail(c, 403, "qq_roster_permission", err.Error())
		return
	}
	if err != nil {
		fail(c, 502, "qq_roster_failed", err.Error())
		return
	}
	ids := make([]string, 0, len(roster))
	names := make([]string, 0, len(roster))
	roles := make([]string, 0, len(roster))
	bots := make([]bool, 0, len(roster))
	for _, m := range roster {
		ids = append(ids, m.ID)
		names = append(names, m.Username)
		roles = append(roles, m.Role)
		bots = append(bots, m.Bot)
	}
	if _, err = tx.Exec(ctx, "SELECT id FROM conversations WHERE id=$1 FOR UPDATE", id); err != nil {
		fail(c, 500, "database_error", "锁定群成员名单失败")
		return
	}
	_, err = tx.Exec(ctx, `INSERT INTO conversation_members(conversation_id,platform_user_id,display_name,qq_username,qq_role,is_bot,active,membership_updated_at)
      SELECT $1,id,name,name,role,bot,true,$6 FROM unnest($2::text[],$3::text[],$4::text[],$5::bool[]) m(id,name,role,bot)
      ON CONFLICT(conversation_id,platform_user_id) DO UPDATE SET active=true,
      display_name=CASE WHEN conversation_members.display_name IN ('',conversation_members.qq_username) THEN COALESCE(NULLIF(EXCLUDED.qq_username,''),conversation_members.display_name) ELSE conversation_members.display_name END,qq_username=EXCLUDED.qq_username,qq_role=EXCLUDED.qq_role,is_bot=EXCLUDED.is_bot,
      membership_updated_at=EXCLUDED.membership_updated_at WHERE conversation_members.membership_updated_at <= $6`, id, ids, names, roles, bots, started)
	if err == nil {
		_, err = tx.Exec(ctx, `UPDATE conversation_members SET active=false,membership_updated_at=$3
       WHERE conversation_id=$1 AND active AND NOT(platform_user_id=ANY($2::text[])) AND membership_updated_at <= $3`, id, ids, started)
	}
	if err == nil {
		_, err = tx.Exec(ctx, "UPDATE conversations SET roster_synced_at=$2 WHERE id=$1", id, started)
	}
	if err != nil || tx.Commit(ctx) != nil {
		fail(c, 500, "database_error", "保存群成员名单失败")
		return
	}
	ok(c, gin.H{"syncedAt": started, "count": len(ids)})
}
