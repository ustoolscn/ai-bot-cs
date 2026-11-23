package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"ai-bot/backend/internal/domain"
	"github.com/gin-gonic/gin"
	"github.com/jackc/pgx/v5"
)

func (a *App) scheduledPostRoutes(r *gin.RouterGroup) {
	group := r.Group("/conversations/:id/schedules")
	group.GET("", func(c *gin.Context) {
		queryList(c, a, `SELECT s.id::text,to_char(s.send_time,'HH24:MI') AS "sendTime",s.instruction,s.source,s.format,s.enabled,
          COALESCE(s.service_id::text,'') AS "serviceId",COALESCE(o.status,j.status,'') AS status,COALESCE(o.last_error,j.last_error,'') AS error
          FROM scheduled_posts s LEFT JOIN agent_jobs j ON j.id=s.last_job_id LEFT JOIN outbox_tasks o ON o.message_id=s.last_message_id
          WHERE s.conversation_id=$1 ORDER BY s.send_time,s.id`, c.Param("id"))
	})
	group.POST("", a.saveSchedule)
	group.PUT("/:item", a.saveSchedule)
	group.DELETE("/:item", func(c *gin.Context) {
		tag, err := a.db.Exec(c, "DELETE FROM scheduled_posts WHERE conversation_id=$1 AND id=$2", c.Param("id"), c.Param("item"))
		if err != nil {
			fail(c, 500, "database_error", "删除失败")
			return
		}
		if tag.RowsAffected() == 0 {
			fail(c, 404, "not_found", "任务不存在")
			return
		}
		ok(c, gin.H{"deleted": true})
	})
}

func (a *App) saveSchedule(c *gin.Context) {
	var in struct {
		SendTime, Instruction, Source, Format, ServiceID string
		Enabled                                          bool
	}
	if c.ShouldBindJSON(&in) != nil {
		fail(c, 400, "invalid_request", "任务格式有误")
		return
	}
	in.Instruction = strings.TrimSpace(in.Instruction)
	at, err := time.Parse("15:04", in.SendTime)
	if err != nil || at.Format("15:04") != in.SendTime || in.Instruction == "" || len([]rune(in.Instruction)) > 4000 ||
		(in.Source != "web" && in.Source != "group" && in.Source != "instruction") || (in.Format != "text" && in.Format != "image") || (in.ServiceID != "" && parseUUID(in.ServiceID) != nil) {
		fail(c, 400, "invalid_request", "请填写有效的时间、内容指令、数据来源和发送格式")
		return
	}
	args := []any{c.Param("id"), in.SendTime, in.Instruction, in.Source, in.Format, nullString(in.ServiceID), in.Enabled}
	valid := `SELECT c.id FROM conversations c WHERE c.id=$1 AND c.type='group'
        AND ($6::uuid IS NULL OR EXISTS(SELECT 1 FROM bot_services b WHERE b.id=$6 AND b.bot_id=c.bot_id))`
	query := `INSERT INTO scheduled_posts(conversation_id,send_time,instruction,source,format,service_id,enabled)
        SELECT id,$2::text::time,$3,$4,$5,$6,$7 FROM (` + valid + `) g RETURNING id::text`
	if id := c.Param("item"); id != "" {
		query = `UPDATE scheduled_posts SET send_time=$2::text::time,instruction=$3,source=$4,format=$5,service_id=$6,enabled=$7,
          updated_at=now(),last_job_id=NULL,last_message_id=NULL WHERE conversation_id IN (` + valid + `) AND id=$8 RETURNING id::text`
		args = append(args, id)
	}
	var id string
	err = a.db.QueryRow(c, query, args...).Scan(&id)
	if errors.Is(err, pgx.ErrNoRows) {
		fail(c, 400, "invalid_target", "请检查群、客服及任务是否存在")
		return
	}
	if err != nil {
		fail(c, 500, "database_error", "保存任务失败")
		return
	}
	ok(c, gin.H{"id": id})
}

func (a *App) queueScheduledPosts(ctx context.Context, now time.Time) error {
	// Generation has a bounded timeout. Recover a claim abandoned by a stopped worker.
	if _, err := a.db.Exec(ctx, `UPDATE agent_jobs SET status='pending',locked_at=NULL
      WHERE kind='scheduled_post' AND status='processing' AND locked_at<now()-interval '5 minutes'`); err != nil {
		return err
	}
	_, err := a.db.Exec(ctx, `WITH clock AS (SELECT $1::timestamptz AT TIME ZONE 'Asia/Shanghai' AS local_now),
      due AS (
        UPDATE scheduled_posts s SET last_run_date=k.local_now::date,last_job_id=gen_random_uuid(),last_message_id=NULL
        FROM conversations c JOIN bots b ON b.id=c.bot_id CROSS JOIN clock k
        WHERE c.id=s.conversation_id AND s.enabled AND c.enabled AND b.enabled AND c.type='group'
          AND s.last_run_date IS DISTINCT FROM k.local_now::date
          AND k.local_now BETWEEN k.local_now::date+s.send_time AND k.local_now::date+s.send_time+interval '1 hour'
          AND s.updated_at AT TIME ZONE 'Asia/Shanghai' <= k.local_now::date+s.send_time
        RETURNING s.last_job_id
      ) INSERT INTO agent_jobs(id,kind,payload) SELECT last_job_id,'scheduled_post','{}'::jsonb FROM due`, now)
	return err
}

func (a *App) generateScheduledPost(ctx context.Context, jobID string) error {
	ctx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()
	var cid, profile, persona, style, role, source, format, instruction string
	var day, deadline time.Time
	err := a.db.QueryRow(ctx, `SELECT c.id::text,COALESCE(svc.chat_profile_id::text,
        CASE WHEN s.service_id IS NULL THEN cfg.chat_profile_id::text END,''),COALESCE(svc.name,''),COALESCE(p.prompt,''),COALESCE(ag.instructions,''),
        s.source,s.format,s.instruction,s.last_run_date,((s.last_run_date+s.send_time) AT TIME ZONE 'Asia/Shanghai')+interval '1 hour'
      FROM scheduled_posts s JOIN conversations c ON c.id=s.conversation_id JOIN bots b ON b.id=c.bot_id
      JOIN conversation_support_config cfg ON cfg.id=c.id
      LEFT JOIN bot_services svc ON svc.id=COALESCE(s.service_id,c.service_id) AND svc.bot_id=c.bot_id
      LEFT JOIN support_agents ag ON ag.id=svc.agent_id LEFT JOIN personality_presets p ON p.id=ag.personality_id
      WHERE s.last_job_id=$1 AND s.last_message_id IS NULL AND s.enabled AND c.enabled AND b.enabled`, jobID).
		Scan(&cid, &profile, &persona, &style, &role, &source, &format, &instruction, &day, &deadline)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	if time.Now().After(deadline) {
		return fmt.Errorf("已超过本次发送时间窗口")
	}
	if profile == "" {
		profile = a.defaultChatProfile(ctx)
	}
	client, kind, err := a.loadModel(ctx, profile)
	if err != nil || kind != "chat" {
		return fmt.Errorf("请为接待客服配置可用的对话模型")
	}
	if persona == "" {
		s := a.agentSettings(ctx)
		persona, style = s.PersonaName, s.StylePrompt
	}
	system := fmt.Sprintf("你是%s。%s\n职责：%s\n当前任务日期（北京时间）：%s。按管理员指令生成可直接发布到 QQ 群的成稿。", persona, style, role, day.Format("2006-01-02"))
	if source == "group" {
		start := time.Date(day.Year(), day.Month(), day.Day(), 0, 0, 0, 0, chinaLocation)
		var incoming, members, answers int
		err = a.db.QueryRow(ctx, `SELECT count(*) FILTER(WHERE direction='inbound'),count(DISTINCT NULLIF(sender_id,'')) FILTER(WHERE direction='inbound'),
          count(DISTINCT reply_to_message_id) FILTER(WHERE event_type='AI_RESPONSE' AND status='sent') FROM messages
          WHERE conversation_id=$1 AND event_at >= $2 AND event_at < $3 AND NOT context_excluded`, cid, start.AddDate(0, 0, -1), start).Scan(&incoming, &members, &answers)
		if err != nil {
			return err
		}
		system += fmt.Sprintf("\n昨日群消息 %d 条，发言 %d 人，已答复 %d 条提问。", incoming, members, answers)
	}
	if source == "web" {
		system += "\n必须实际搜索近期资料，核实发布日期。每条注明日期和可核对的来源链接，资料不足时如实说明，不补写虚构条目。"
	}
	started := time.Now()
	res, err := client.ChatWith(ctx, []domain.ChatMessage{{Role: "system", Content: system}, {Role: "user", Content: instruction}}, domain.ChatOptions{RequireWebSearch: source == "web", DisableWebSearch: source != "web"})
	a.recordModelCall(ctx, "", profile, "scheduled_post", res, time.Since(started).Milliseconds(), err)
	if err != nil {
		return err
	}
	text := strings.TrimSpace(res.Content)
	if text == "" {
		return fmt.Errorf("模型返回了空内容")
	}
	parts := []domain.ContentPart{}
	if format == "image" {
		start := time.Now()
		layout, e := client.ChatWith(ctx, []domain.ChatMessage{{Role: "system", Content: "将给定成稿排版为一张 1000px 宽的中文网页长图。只输出静态 HTML 和内联 CSS。完整保留标题、正文、日期与来源，来源用可见文字。高度随内容自然增长。"}, {Role: "user", Content: text}}, domain.ChatOptions{DisableWebSearch: true})
		a.recordModelCall(ctx, "", profile, "scheduled_layout", layout, time.Since(start).Milliseconds(), e)
		if e != nil {
			return e
		}
		pic, e := a.htmlImage(ctx, layout.Content)
		if e != nil {
			return e
		}
		parts = append(parts, pic)
		text = "定时推送"
	}
	raw, _ := json.Marshal(parts)
	// Recheck that this exact occurrence is still active after model/render latency.
	_, err = a.db.Exec(ctx, `WITH target AS (
       SELECT s.id,c.bot_id,c.id AS cid FROM scheduled_posts s JOIN conversations c ON c.id=s.conversation_id JOIN bots b ON b.id=c.bot_id
       WHERE s.last_job_id=$1 AND s.enabled AND c.enabled AND b.enabled AND s.last_message_id IS NULL AND now()<$4 FOR UPDATE OF s
    ), msg AS (
       INSERT INTO messages(channel,bot_id,conversation_id,direction,event_type,content,parts,status,reply_deadline)
       SELECT 'qq',bot_id,cid,'outbound','SCHEDULED_POST',$2,$3::jsonb,'pending',$4 FROM target RETURNING id
    ), marked AS (
       UPDATE scheduled_posts s SET last_message_id=m.id FROM msg m,target t WHERE s.id=t.id RETURNING m.id
    ) INSERT INTO outbox_tasks(message_id,msg_seq) SELECT id,1 FROM marked`, jobID, text, string(raw), deadline)
	return err
}
