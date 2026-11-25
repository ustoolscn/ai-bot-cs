package app

import (
	"ai-bot/backend/internal/domain"
	"ai-bot/backend/internal/qq"
	"context"
	"encoding/json"
	"errors"
	"github.com/gin-gonic/gin"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
	"unicode/utf8"
)

func (a *App) qqAssets(c *gin.Context) {
	queryList(c, a, `SELECT id::text,filename,file_uuid,raw_url,expires_at FROM qq_media_assets WHERE conversation_id=$1 AND (expires_at IS NULL OR expires_at>now()) ORDER BY created_at DESC LIMIT 100`, c.Param("id"))
}
func (a *App) qqDeliveryOptions(c *gin.Context) {
	queryList(c, a, `SELECT * FROM (SELECT m.id::text AS id,'message' AS kind,m.direction,m.qq_ref_idx<>'' AS quotable,left(display_mentions(m,m.content),100) AS label,COALESCE(m.qq_sent_at,m.event_at) AS at,m.direction='inbound' AND m.reply_deadline>now() AS passive FROM messages m WHERE m.conversation_id=$1 AND NOT m.context_excluded AND m.status<>'retracted' AND m.platform_message_id IS NOT NULL AND (m.qq_ref_idx<>'' OR m.reply_deadline>now()) UNION ALL SELECT w.id::text,'event','',false,concat_ws(' · ',w.event_type,NULLIF(w.raw_event#>>'{d,data,resolved,button_data}','')),w.created_at,false FROM webhook_events w JOIN conversations c ON c.bot_id=w.bot_id AND c.id=$1 WHERE w.created_at>now()-interval '1 hour' AND w.event_type IN ('INTERACTION_CREATE','GROUP_ADD_ROBOT','GROUP_MSG_RECEIVE','C2C_MSG_RECEIVE','FRIEND_ADD') AND c.platform_id IN(w.raw_event#>>'{d,group_openid}',w.raw_event#>>'{d,user_openid}',w.raw_event#>>'{d,resolved,group_openid}',w.raw_event#>>'{d,resolved,user_openid}')) t ORDER BY at DESC LIMIT 150`, c.Param("id"))
}
func (a *App) qqUpload(c *gin.Context) {
	ctx := c.Request.Context()
	_ = http.NewResponseController(c.Writer).SetReadDeadline(time.Now().Add(10 * time.Minute))
	id := c.Param("id")
	var group, kind, app, enc string
	if err := a.db.QueryRow(ctx, `SELECT c.platform_id,c.type,b.app_id,b.app_secret_enc FROM conversations c JOIN bots b ON b.id=c.bot_id WHERE c.id=$1 AND b.enabled AND c.enabled AND NOT c.bot_removed`, id).Scan(&group, &kind, &app, &enc); err != nil {
		fail(c, 404, "not_found", "会话不存在或已停用")
		return
	}
	secret, err := a.cipher.Decrypt(enc)
	if err != nil {
		fail(c, 500, "secret_error", "读取凭证失败")
		return
	}
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, qq.MaxMediaSize+(1<<20))
	if err = c.Request.ParseMultipartForm(5 << 20); err != nil {
		fail(c, 400, "upload", "文件过大或表单无效")
		return
	}
	defer c.Request.MultipartForm.RemoveAll()
	var in struct {
		Kind   int    `form:"kind" binding:"min=1,max=4"`
		Direct bool   `form:"direct"`
		URL    string `form:"url"`
		Name   string `form:"name"`
	}
	if c.ShouldBind(&in) != nil {
		fail(c, 400, "invalid", "文件类型无效")
		return
	}
	var reader io.ReaderAt
	var size int64
	f, h, e := c.Request.FormFile("file")
	if e == nil {
		defer f.Close()
		reader = f
		size = h.Size
		in.Name = h.Filename
	} else if !errors.Is(e, http.ErrMissingFile) {
		fail(c, 400, "file", "读取文件失败")
		return
	}
	var directID string
	if in.Direct {
		parts := make([]domain.ContentPart, 0, 1)
		if in.Kind == 1 {
			part := domain.ContentPart{Type: "image", Filename: in.Name, SizeBytes: size}
			if httpsLink(in.URL) {
				part.URL = in.URL
			}
			if reader != nil && size <= maxInboundImageSize {
				data, e := io.ReadAll(io.NewSectionReader(reader, 0, size))
				if e != nil {
					fail(c, 400, "file", "读取图片失败")
					return
				}
				part.ContentType = http.DetectContentType(data)
				if ext := imageExtension(part.ContentType); ext != "" {
					part.StorageKey, err = a.images.Save(ctx, "upload"+ext, data)
					if err != nil {
						fail(c, 500, "storage", "保存图片失败")
						return
					}
				}
			}
			parts = append(parts, part)
		}
		err = a.db.QueryRow(ctx, `INSERT INTO messages(channel,bot_id,conversation_id,direction,event_type,sender_id,sender_name,content,parts,status) SELECT 'qq',bot_id,id,'outbound','QQ_MEDIA_SEND',$2,$3,$4,$5,'processing' FROM conversations WHERE id=$1 RETURNING id::text`, id, c.GetString("userId"), c.GetString("username"), in.Name, parts).Scan(&directID)
		if err != nil {
			fail(c, 500, "database_error", "保存发送记录失败")
			return
		}
	}
	media, err := qq.NewClient(app, secret, a.cfg.QQAPIBaseURL, a.cfg.QQTokenURL).Upload(ctx, qq.Prefix(kind, group), in.Name, in.URL, in.Kind, reader, size, in.Direct)
	if in.Direct {
		writeCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
		defer cancel()
		status := "sent"
		if err != nil {
			status = "failed"
		}
		if _, saveErr := a.db.Exec(writeCtx, `UPDATE messages SET status=$2,platform_message_id=$3,delivery_error=$4 WHERE id=$1`, directID, status, nullString(media.ID), errorText(err)); saveErr != nil {
			fail(c, 500, "database_error", "保存发送结果失败")
			return
		}
	}
	if err != nil {
		fail(c, 502, "qq_upload", err.Error())
		return
	}
	tx, err := a.db.Begin(ctx)
	if err != nil {
		fail(c, 500, "database_error", "QQ 已处理，保存回执失败")
		return
	}
	defer tx.Rollback(ctx)
	if media.FileInfo != "" {
		_, err = tx.Exec(ctx, `INSERT INTO qq_media_assets(conversation_id,filename,file_uuid,file_info,raw_url,expires_at) VALUES($1,$2,$3,$4,$5,CASE WHEN $6::int>0 THEN now()+$6*interval '1 second' END)`, id, in.Name, media.FileUUID, media.FileInfo, media.RawURL, media.TTL)
	}
	if err != nil || tx.Commit(ctx) != nil {
		fail(c, 500, "database_error", "QQ 已处理，保存回执失败")
		return
	}
	a.audit(c, "qq.media.upload", "conversation", id, gin.H{"sent": in.Direct})
	ok(c, gin.H{"sent": in.Direct, "file_uuid": media.FileUUID, "id": directID})
}

type qqSendForm struct {
	Kind, Text, Title, Description, URL, Picture, Keyboard, Reference, Reply, Event, Asset string
	Keys                                                                                   json.RawMessage
	Wakeup, PrivateReply                                                                   bool
	Seconds                                                                                int
}

func httpsLink(s string) bool {
	u, e := url.Parse(s)
	return e == nil && u.Scheme == "https" && u.Host != "" && u.User == nil
}
func (i qqSendForm) payload(kind string) (map[string]any, error) {
	p := map[string]any{}
	invalid := errors.New("消息参数不符合 QQ 接口要求")
	switch i.Kind {
	case "text", "markdown":
		if strings.TrimSpace(i.Text) == "" {
			return nil, invalid
		}
		if i.Kind == "text" {
			p["msg_type"] = 0
			p["content"] = i.Text
		} else {
			p["msg_type"] = 2
			p["markdown"] = gin.H{"content": i.Text, "force_verify_image_resource": true}
		}
	case "card":
		if n := utf8.RuneCountInString(i.Title); n < 1 || n > 50 || utf8.RuneCountInString(i.Description) < 1 || utf8.RuneCountInString(i.Description) > 200 || !httpsLink(i.URL) || !httpsLink(i.Picture) || len(i.URL) > 1024 || len(i.Picture) > 1024 {
			return nil, invalid
		}
		p["msg_type"] = 8
		p["card"] = gin.H{"type": "tuwen", "content": gin.H{"title": i.Title, "description": i.Description, "url": i.URL, "pic_url": i.Picture}}
	case "typing":
		if kind != "private" || i.Seconds < 1 || i.Seconds > 60 {
			return nil, invalid
		}
		p["msg_type"] = 6
		p["input_notify"] = gin.H{"input_type": 1, "input_second": i.Seconds}
	case "media":
		if i.Asset == "" {
			return nil, invalid
		}
		p["msg_type"] = 7
	default:
		return nil, invalid
	}
	if i.Wakeup {
		if kind != "private" || i.Event != "" || i.Reply != "" {
			return nil, invalid
		}
		p["is_wakeup"] = true
	}
	if i.Keyboard != "" {
		p["keyboard"] = gin.H{"id": i.Keyboard}
	}
	if len(i.Keys) > 0 {
		var k struct{ Rows []json.RawMessage }
		if i.Keyboard != "" || json.Unmarshal(i.Keys, &k) != nil || len(k.Rows) == 0 {
			return nil, invalid
		}
		p["keyboard"] = gin.H{"content": i.Keys}
	}
	return p, nil
}
func (a *App) qqQueue(c *gin.Context) {
	ctx := c.Request.Context()
	var in qqSendForm
	if c.ShouldBindJSON(&in) != nil || (in.Event != "" && in.Reply != "") {
		fail(c, 400, "invalid", "消息参数无效")
		return
	}
	tx, err := a.db.Begin(ctx)
	if err != nil {
		fail(c, 500, "database_error", "创建发送任务失败")
		return
	}
	defer tx.Rollback(ctx)
	id := c.Param("id")
	var kind, bot, target string
	var extended bool
	if err = tx.QueryRow(ctx, `SELECT c.type,c.bot_id::text,c.platform_id,c.qq_extended_replies FROM conversations c JOIN bots b ON b.id=c.bot_id WHERE c.id=$1 AND c.enabled AND NOT c.bot_removed AND b.enabled FOR UPDATE OF c`, id).Scan(&kind, &bot, &target, &extended); err != nil {
		fail(c, 404, "not_found", "会话不存在或已停用")
		return
	}
	var reply, sender string
	var deadline *time.Time
	seq := 1
	if in.Reply != "" {
		err = tx.QueryRow(ctx, `SELECT platform_message_id,sender_id,reply_deadline FROM messages WHERE id=$1 AND conversation_id=$2 AND direction='inbound' AND NOT context_excluded AND reply_deadline>now()`, in.Reply, id).Scan(&reply, &sender, &deadline)
		if err != nil {
			fail(c, 400, "reply", "可回复时限已过或消息不存在")
			return
		}
		if err = tx.QueryRow(ctx, `SELECT COALESCE(max(t.msg_seq),0)+1 FROM outbox_tasks t JOIN messages m ON m.id=t.message_id WHERE m.bot_id=$1 AND m.reply_to_message_id=$2`, bot, reply).Scan(&seq); err != nil {
			fail(c, 500, "database_error", "分配回复序号失败")
			return
		}
	}
	if reply != "" && seq > qqReplyLimit(kind, extended) {
		fail(c, 400, "quota", "这条消息的被动回复次数已用完")
		return
	}
	if in.PrivateReply {
		if kind != "group" || reply == "" || in.Event != "" || in.Reference != "" || in.Asset != "" {
			fail(c, 400, "invalid", "请选择群内收到的原消息")
			return
		}
		var private string
		err = tx.QueryRow(ctx, `SELECT p.id::text FROM conversations p WHERE p.bot_id=$1 AND p.type='private' AND p.enabled AND (p.platform_id=$2 OR EXISTS(SELECT FROM conversation_members g JOIN conversation_members u ON u.qq_union_id=g.qq_union_id AND u.conversation_id=p.id AND u.platform_user_id=p.platform_id WHERE g.conversation_id=$3 AND g.platform_user_id=$2 AND g.qq_union_id<>'')) ORDER BY (p.platform_id=$2) DESC LIMIT 1`, bot, sender, id).Scan(&private)
		if err != nil {
			fail(c, 400, "private", "没有匹配到该发言人的私聊会话")
			return
		}
		id = private
		kind = "private"
	}
	p, e := in.payload(kind)
	if e != nil {
		fail(c, 400, "invalid", e.Error())
		return
	}
	if in.Event != "" {
		var event string
		var expires time.Time
		err = tx.QueryRow(ctx, `SELECT platform_event_id,created_at+interval '1 hour' FROM webhook_events WHERE id=$1 AND bot_id=$2 AND created_at>now()-interval '1 hour' AND $3::text IN(raw_event#>>'{d,group_openid}',raw_event#>>'{d,user_openid}',raw_event#>>'{d,resolved,group_openid}',raw_event#>>'{d,resolved,user_openid}') AND event_type=ANY($4::text[])`, in.Event, bot, target, allowedReplyEvents(kind)).Scan(&event, &expires)
		if err != nil {
			fail(c, 400, "invalid", "事件不可用于此会话")
			return
		}
		p["event_id"] = event
		deadline = &expires
	}
	if in.Reference != "" {
		var index string
		err = tx.QueryRow(ctx, `SELECT qq_ref_idx FROM messages WHERE id=$1 AND conversation_id=$2 AND qq_ref_idx<>'' AND NOT context_excluded AND status<>'retracted'`, in.Reference, id).Scan(&index)
		if err != nil {
			fail(c, 400, "reference", "引用目标不可用")
			return
		}
		p["message_reference"] = gin.H{"message_id": index}
	}
	if in.Kind == "media" {
		var file string
		var expires *time.Time
		err = tx.QueryRow(ctx, `SELECT file_info,expires_at FROM qq_media_assets WHERE id=$1 AND conversation_id=$2 AND (expires_at IS NULL OR expires_at>now())`, in.Asset, id).Scan(&file, &expires)
		if err != nil {
			fail(c, 400, "media", "素材不存在或已过期")
			return
		}
		if expires != nil && (deadline == nil || expires.Before(*deadline)) {
			deadline = expires
		}
		p["media"] = gin.H{"file_info": file}
	}
	label := in.Text
	if label == "" {
		label = in.Title
	}
	if label == "" {
		label = in.Kind
	}
	var out string
	err = tx.QueryRow(ctx, `INSERT INTO messages(channel,bot_id,conversation_id,direction,event_type,content,status,reply_to_message_id,reply_deadline,qq_send_payload,sender_id,sender_name) VALUES('qq',$1,$2,'outbound','QQ_ADMIN_SEND',$3,'pending',$4,$5,$6,$7,$8) RETURNING id::text`, bot, id, label, nullString(reply), deadline, p, c.GetString("userId"), c.GetString("username")).Scan(&out)
	if err == nil {
		_, err = tx.Exec(ctx, "INSERT INTO outbox_tasks(message_id,msg_seq) VALUES($1,$2)", out, seq)
	}
	if err != nil || tx.Commit(ctx) != nil {
		fail(c, 500, "database_error", "创建发送任务失败")
		return
	}
	a.audit(c, "qq.send", "message", out, nil)
	ok(c, gin.H{"id": out})
}
func qqReplyLimit(kind string, extended bool) int {
	if kind == "private" {
		return 4
	}
	if extended {
		return 50
	}
	return 5
}
func allowedReplyEvents(kind string) []string {
	if kind == "private" {
		return []string{"INTERACTION_CREATE", "C2C_MSG_RECEIVE", "FRIEND_ADD"}
	}
	return []string{"INTERACTION_CREATE", "GROUP_ADD_ROBOT", "GROUP_MSG_RECEIVE"}
}
