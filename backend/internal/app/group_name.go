package app

import (
	"context"
	"errors"

	"ai-bot/backend/internal/qq"
	"github.com/gin-gonic/gin"
	"github.com/jackc/pgx/v5"
)

func (a *App) syncGroupName(c *gin.Context) {
	ctx := c.Request.Context()
	id := c.Param("id")
	var group, previousName, appID, encrypted string
	err := a.db.QueryRow(ctx, `SELECT c.platform_id,c.name,b.app_id,b.app_secret_enc FROM conversations c JOIN bots b ON b.id=c.bot_id WHERE c.id=$1 AND c.channel='qq' AND c.type='group'`, id).Scan(&group, &previousName, &appID, &encrypted)
	if errors.Is(err, pgx.ErrNoRows) {
		fail(c, 404, "not_found", "群聊不存在")
		return
	}
	if err != nil {
		fail(c, 500, "database_error", "读取群配置失败")
		return
	}
	secret, err := a.cipher.Decrypt(encrypted)
	if err != nil {
		fail(c, 500, "secret_error", "读取机器人凭证失败")
		return
	}
	info, err := qq.NewClient(appID, secret, a.cfg.QQAPIBaseURL, a.cfg.QQTokenURL).GroupInfo(ctx, group)
	if errors.Is(err, qq.ErrGroupInfoPermission) {
		fail(c, 403, "qq_group_info_permission", err.Error())
		return
	}
	if err != nil {
		fail(c, 502, "qq_group_info_failed", err.Error())
		return
	}
	result, err := a.db.Exec(ctx, `UPDATE conversations SET name=$2,qq_group_info=qq_group_info||$4::jsonb,qq_group_info_checked_at=now() WHERE id=$1 AND name=$3`, id, info.Name, previousName, info)
	if err != nil {
		fail(c, 500, "database_error", "保存群名失败")
		return
	}
	if result.RowsAffected() == 0 {
		fail(c, 409, "conversation_changed", "会话已变更，请刷新后重试")
		return
	}
	ok(c, gin.H{"name": info.Name, "info": info})
}

const missingGroupName = `(c.name='' OR c.name=c.platform_id OR c.name LIKE 'QQ 群聊 · %')`

const needsGroupInfo = `(c.qq_group_info_checked_at IS NULL OR c.qq_group_info_checked_at < now()-interval '1 day') AND (` + missingGroupName + ` OR coalesce(c.qq_group_info->>'group_avatar','')='')`

func (a *App) refreshMissingGroupInfo(ctx context.Context, id string) error {
	var group, appID, encrypted string
	err := a.db.QueryRow(ctx, `SELECT c.platform_id,b.app_id,b.app_secret_enc FROM conversations c JOIN bots b ON b.id=c.bot_id WHERE c.id=$1 AND c.type='group' AND NOT c.bot_removed AND `+needsGroupInfo, id).Scan(&group, &appID, &encrypted)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	secret, err := a.cipher.Decrypt(encrypted)
	if err != nil {
		return err
	}
	info, err := qq.NewClient(appID, secret, a.cfg.QQAPIBaseURL, a.cfg.QQTokenURL).GroupInfo(ctx, group)
	if errors.Is(err, qq.ErrGroupInfoPermission) {
		_, err = a.db.Exec(ctx, "UPDATE conversations SET qq_group_info_checked_at=now() WHERE id=$1", id)
		return err
	}
	if err != nil {
		return err
	}
	_, err = a.db.Exec(ctx, `UPDATE conversations c SET name=CASE WHEN `+missingGroupName+` THEN $2 ELSE c.name END,qq_group_info=qq_group_info||$3::jsonb,qq_group_info_checked_at=now() WHERE id=$1 AND NOT c.bot_removed AND `+needsGroupInfo, id, info.Name, info)
	return err
}
