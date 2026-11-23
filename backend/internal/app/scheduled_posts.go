package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"ai-bot/backend/internal/domain"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// The parent row is locked by the caller. Each run snapshots its eligible recipients.
func createScheduleRun(ctx context.Context, tx pgx.Tx, id, source string, now time.Time, groups []string, testID string) (int, error) {
	test := testID != ""
	runID := testID
	if !test {
		runID = uuid.NewString()
	}
	var inserted string
	err := tx.QueryRow(ctx, `INSERT INTO schedule_runs(id,schedule_id,revision,run_date,deadline,is_test)
 SELECT $1,id,revision,($3::timestamptz AT TIME ZONE 'Asia/Shanghai')::date,
 CASE WHEN $4 THEN $3::timestamptz+interval '1 hour' ELSE
 (((($3::timestamptz AT TIME ZONE 'Asia/Shanghai')::date+send_time) AT TIME ZONE 'Asia/Shanghai')+interval '1 hour') END,$4 FROM scheduled_posts WHERE id=$2
 ON CONFLICT DO NOTHING RETURNING id::text`, runID, id, now, test).Scan(&inserted)
	if errors.Is(err, pgx.ErrNoRows) {
		return 0, nil
	}
	if err != nil {
		return 0, err
	}
	tag, err := tx.Exec(ctx, `INSERT INTO schedule_deliveries(run_id,conversation_id,job_id)
 SELECT $1,conversation_id,CASE WHEN $4='group' THEN gen_random_uuid() ELSE $1::uuid END FROM schedule_recipients
 WHERE schedule_id=$2 AND ($3::uuid[] IS NULL OR conversation_id=ANY($3))`, runID, id, groups, source)
	if err != nil {
		return 0, err
	}
	_, err = tx.Exec(ctx, `INSERT INTO agent_jobs(id,kind,payload) SELECT DISTINCT job_id,'scheduled_post','{}'::jsonb FROM schedule_deliveries WHERE run_id=$1`, runID)
	return int(tag.RowsAffected()), err
}

func (a *App) queueScheduledPosts(ctx context.Context, now time.Time) error {
	tx, err := a.db.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	if _, err = tx.Exec(ctx, `UPDATE agent_jobs SET status='pending',locked_at=NULL WHERE kind='scheduled_post' AND status='processing' AND locked_at<now()-interval '5 minutes'`); err != nil {
		return err
	}
	rows, err := tx.Query(ctx, `WITH clock AS (SELECT $1::timestamptz AT TIME ZONE 'Asia/Shanghai' AS t)
 SELECT s.id::text,s.source FROM scheduled_posts s,clock WHERE enabled AND last_run_date IS DISTINCT FROM t::date
 AND t BETWEEN t::date+send_time AND t::date+send_time+interval '1 hour'
 AND updated_at AT TIME ZONE 'Asia/Shanghai' <= t::date+send_time AND EXISTS(SELECT FROM schedule_recipients WHERE schedule_id=s.id)
 ORDER BY s.id FOR UPDATE OF s SKIP LOCKED`, now)
	if err != nil {
		return err
	}
	type due struct{ ID, Source string }
	tasks, err := pgx.CollectRows(rows, pgx.RowToStructByPos[due])
	if err != nil {
		return err
	}
	for _, t := range tasks {
		if _, err = createScheduleRun(ctx, tx, t.ID, t.Source, now, nil, ""); err != nil {
			return err
		}
		if _, err = tx.Exec(ctx, "UPDATE scheduled_posts SET last_run_date=($2::timestamptz AT TIME ZONE 'Asia/Shanghai')::date WHERE id=$1", t.ID, now); err != nil {
			return err
		}
	}
	return tx.Commit(ctx)
}

func (a *App) generateScheduledPost(ctx context.Context, jobID string) error {
	ctx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()
	var cid, profile, persona, style, role, source, format, instruction string
	var day, deadline time.Time
	err := a.db.QueryRow(ctx, `SELECT c.id::text,COALESCE(svc.chat_profile_id::text,
        CASE WHEN dest.service_id IS NULL THEN cfg.chat_profile_id::text END,''),COALESCE(svc.name,''),COALESCE(svc.settings->>'stylePrompt',p.prompt,''),COALESCE(svc.instructions,ag.instructions,''),
        s.source,s.format,s.instruction,r.run_date,r.deadline
 FROM schedule_deliveries d JOIN schedule_runs r ON r.id=d.run_id JOIN scheduled_posts s ON s.id=r.schedule_id AND s.revision=r.revision
 JOIN schedule_recipients dest ON dest.schedule_id=s.id AND dest.conversation_id=d.conversation_id JOIN conversations c ON c.id=d.conversation_id
 JOIN conversation_support_config cfg ON cfg.id=c.id
      LEFT JOIN bot_services svc ON svc.id=COALESCE(dest.service_id,c.service_id) AND svc.bot_id=c.bot_id
      LEFT JOIN support_agents ag ON ag.id=svc.agent_id LEFT JOIN personality_presets p ON p.id=ag.personality_id
      LEFT JOIN model_profiles mp ON mp.id=coalesce(svc.chat_profile_id,cfg.chat_profile_id)
 WHERE d.job_id=$1 AND d.message_id IS NULL AND (s.enabled OR r.is_test) ORDER BY (s.source='web' AND mp.web_search_mode<>'disabled') DESC NULLS LAST,c.id LIMIT 1`, jobID).
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
	var pageBase string
	if format == "link" {
		pageBase, err = a.pageBase(ctx)
		if err != nil {
			return err
		}
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
	a.recordModelCall(ctx, "", "", profile, "scheduled_post", res, time.Since(started).Milliseconds(), err)
	if err != nil {
		return err
	}
	text := strings.TrimSpace(res.Content)
	if text == "" {
		return fmt.Errorf("模型返回了空内容")
	}
	parts := []domain.ContentPart{}
	var pageID, document string
	if format == "image" || format == "link" {
		layoutPrompt := "将给定成稿排版为一张 1000px 宽的中文网页长图。只输出静态 HTML 和内联 CSS。完整保留标题、正文、日期与来源，来源用可见文字。高度随内容自然增长。"
		if format == "link" {
			layoutPrompt = "将给定成稿排版成适合手机阅读的中文网页。只输出静态 HTML 和内联 CSS，保留标题、日期和完整正文，将原文提供的来源链接做成可点击的超链接，不补造来源。"
		}
		start := time.Now()
		layout, e := client.ChatWith(ctx, []domain.ChatMessage{{Role: "system", Content: layoutPrompt}, {Role: "user", Content: text}}, domain.ChatOptions{DisableWebSearch: true})
		a.recordModelCall(ctx, "", "", profile, "scheduled_layout", layout, time.Since(start).Milliseconds(), e)
		if e != nil {
			return e
		}
		if format == "link" {
			document, e = staticDocument(layout.Content)
			if e != nil {
				return e
			}
			pageID = uuid.NewString()
			text = pageBase + "/p/" + pageID
		} else {
			pic, e := a.htmlImage(ctx, layout.Content)
			if e != nil {
				return e
			}
			parts = append(parts, pic)
			text = "定时推送"
		}
	}
	raw, _ := json.Marshal(parts)
	// Recheck that this exact occurrence is still active after model/render latency.
	_, err = a.db.Exec(ctx, `WITH target AS (
       SELECT d.run_id,d.conversation_id AS cid,dest.bot_id,gen_random_uuid() AS mid FROM schedule_deliveries d JOIN schedule_runs r ON r.id=d.run_id
 JOIN scheduled_posts s ON s.id=r.schedule_id AND s.revision=r.revision JOIN schedule_recipients dest ON dest.schedule_id=s.id AND dest.conversation_id=d.conversation_id
 WHERE d.job_id=$1 AND d.message_id IS NULL AND (s.enabled OR r.is_test) AND now()<$4 FOR UPDATE OF s,d
    ), msg AS (
       INSERT INTO messages(id,channel,bot_id,conversation_id,direction,event_type,content,parts,status,reply_deadline)
       SELECT mid,'qq',bot_id,cid,'outbound','SCHEDULED_POST',$2,$3::jsonb,'pending',$4 FROM target RETURNING id
    ), marked AS (
       UPDATE schedule_deliveries d SET message_id=m.id FROM msg m JOIN target t ON t.mid=m.id
       WHERE d.run_id=t.run_id AND d.conversation_id=t.cid RETURNING m.id
    ), page AS (
       INSERT INTO scheduled_web_pages(id,document) SELECT $5::uuid,$6::text WHERE $5::uuid IS NOT NULL AND EXISTS(SELECT FROM marked)
    ) INSERT INTO outbox_tasks(message_id,msg_seq) SELECT id,1 FROM marked`, jobID, text, string(raw), deadline, nullString(pageID), document)
	return err
}
