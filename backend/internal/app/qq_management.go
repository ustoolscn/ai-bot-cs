package app

import (
	"ai-bot/backend/internal/qq"
	"errors"
	"github.com/gin-gonic/gin"
	"github.com/jackc/pgx/v5"
	"net/url"
	"time"
)

func (a *App) groupClient(c *gin.Context) (*qq.Client, string) {
	var group, app, enc string
	err := a.db.QueryRow(c.Request.Context(), `SELECT c.platform_id,b.app_id,b.app_secret_enc FROM conversations c JOIN bots b ON b.id=c.bot_id WHERE c.id=$1 AND c.type='group' AND c.channel='qq'`, c.Param("id")).Scan(&group, &app, &enc)
	if errors.Is(err, pgx.ErrNoRows) {
		fail(c, 404, "not_found", "群聊不存在")
		return nil, ""
	}
	if err != nil {
		fail(c, 500, "database_error", "读取群资料失败")
		return nil, ""
	}
	secret, err := a.cipher.Decrypt(enc)
	if err != nil {
		fail(c, 500, "secret_error", "读取机器人凭证失败")
		return nil, ""
	}
	return qq.NewClient(app, secret, a.cfg.QQAPIBaseURL, a.cfg.QQTokenURL), group
}
func (a *App) qqGroupRead(c *gin.Context) {
	client, group := a.groupClient(c)
	if client == nil {
		return
	}
	ctx := c.Request.Context()
	query := url.Values{}
	var path string
	switch c.Param("section") {
	case "limits":
		var extended bool
		if err := a.db.QueryRow(ctx, "SELECT qq_extended_replies FROM conversations WHERE id=$1", c.Param("id")).Scan(&extended); err != nil {
			fail(c, 500, "database_error", "读取回复额度失败")
			return
		}
		ok(c, gin.H{"extended": extended})
		return
	case "profile":
		info, err := client.GroupInfo(ctx, group)
		if err != nil {
			fail(c, 502, "qq_group_info", err.Error())
			return
		}
		ok(c, info)
		return
	case "applications":
		path = "join_request_list"
		query.Set("limit", "50")
		if cursor := c.Query("cursor"); cursor != "" {
			query.Set("cursor", cursor)
		}
	case "mutes":
		path = "restrict_chat_setting"
	default:
		fail(c, 404, "not_found", "未知操作")
		return
	}
	data, err := client.GroupManagement(ctx, group, path, query)
	if err != nil {
		fail(c, 502, "qq_read", err.Error())
		return
	}
	ok(c, data)
}
func (a *App) qqGroupAction(c *gin.Context) {
	if c.Param("section") == "limits" {
		var in struct{ Extended *bool }
		if c.ShouldBindJSON(&in) != nil || in.Extended == nil {
			fail(c, 400, "invalid", "需要指定群回复额度")
			return
		}
		tag, err := a.db.Exec(c.Request.Context(), "UPDATE conversations SET qq_extended_replies=$2 WHERE id=$1 AND type='group'", c.Param("id"), *in.Extended)
		if err != nil || tag.RowsAffected() == 0 {
			fail(c, 400, "invalid", "更新群回复额度失败")
			return
		}
		ok(c, gin.H{"extended": *in.Extended})
		return
	}
	var p struct {
		Member  string `json:"member" binding:"required,max=256"`
		Op      string
		ID      string
		Reason  string
		Block   bool
		Minutes int
	}
	if c.ShouldBindJSON(&p) != nil {
		fail(c, 400, "invalid", "请选择成员")
		return
	}
	client, group := a.groupClient(c)
	if client == nil {
		return
	}
	ctx := c.Request.Context()
	var err error
	switch c.Param("section") {
	case "applications":
		if (p.Op != "approve" && p.Op != "decline") || p.ID == "" || len(p.Reason) > 1000 || (p.Op == "approve" && (p.Block || p.Reason != "")) {
			fail(c, 400, "invalid", "审批参数无效")
			return
		}
		err = client.ReviewApplication(ctx, group, p.Member, p.ID, p.Op, p.Reason, p.Block)
	case "mutes":
		if (p.Op != "add" && p.Op != "update" && p.Op != "del") || (p.Op != "del" && (p.Minutes < 1 || p.Minutes > 43200)) {
			fail(c, 400, "invalid", "禁言时长应在 1 分钟至 30 天之间")
			return
		}
		err = client.SetMute(ctx, group, p.Member, p.Op, time.Now().Add(time.Duration(p.Minutes)*time.Minute))
	default:
		fail(c, 404, "not_found", "未知操作")
		return
	}
	if err != nil {
		fail(c, 502, "qq_action", err.Error())
		return
	}
	a.audit(c, "qq."+c.Param("section")+"."+p.Op, "conversation", c.Param("id"), gin.H{"member": p.Member, "minutes": p.Minutes, "block": p.Block})
	ok(c, gin.H{"ok": true})
}
