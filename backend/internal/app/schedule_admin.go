package app

import (
	"github.com/gin-gonic/gin"
	"github.com/jackc/pgx/v5"
	"strings"
	"time"
)

type scheduleInput struct {
	Name, SendTime, Instruction, Source, Format, ServiceID string
	Enabled, AllGroups                                     bool
	GroupIDs                                               []string
}

func (a *App) scheduledPostRoutes(r *gin.RouterGroup) {
	for _, path := range []string{"/schedules", "/conversations/:id/schedules"} {
		g := r.Group(path)
		g.GET("", a.listSchedules)
		g.POST("", a.saveSchedule)
		g.PUT("/:item", a.saveSchedule)
		g.DELETE("/:item", a.deleteSchedule)
	}
	r.POST("/schedules/:item/test", a.testSchedule)
	r.GET("/schedules/:item/runs", func(c *gin.Context) {
		queryList(c, a, `SELECT r.id,r.created_at AS "createdAt",r.is_test AS "isTest",c.name AS "groupName",b.name AS "botName",
 CASE WHEN o.status IN ('sent','failed','expired') THEN o.status WHEN j.status='failed' THEN 'failed' WHEN (s.revision<>r.revision OR NOT c.enabled OR NOT b.enabled OR (NOT s.enabled AND NOT r.is_test)) THEN 'cancelled'
 ELSE coalesce(o.status,j.status,'pending') END AS status,coalesce(o.last_error,j.last_error,'') AS error
 FROM (SELECT * FROM schedule_runs WHERE schedule_id=$1 ORDER BY created_at DESC LIMIT 10) r
 JOIN scheduled_posts s ON s.id=r.schedule_id JOIN schedule_deliveries d ON d.run_id=r.id JOIN conversations c ON c.id=d.conversation_id JOIN bots b ON b.id=c.bot_id
 LEFT JOIN agent_jobs j ON j.id=d.job_id LEFT JOIN outbox_tasks o ON o.message_id=d.message_id ORDER BY r.created_at DESC,c.name,c.id`, c.Param("item"))
	})
}

// The former group endpoint can manage single-group tasks only.
const singleSchedule = `(NOT s.all_groups AND ARRAY(SELECT conversation_id FROM schedule_targets WHERE schedule_id=s.id)=ARRAY[$1::uuid])`

func (a *App) listSchedules(c *gin.Context) {
	q := `SELECT s.id,s.name,to_char(s.send_time,'HH24:MI') AS "sendTime",s.instruction,s.format,s.source,s.enabled,s.all_groups AS "allGroups",
 ARRAY(SELECT conversation_id::text FROM schedule_targets WHERE schedule_id=s.id ORDER BY conversation_id) AS "groupIds",
 coalesce((SELECT service_id::text FROM schedule_targets WHERE schedule_id=s.id LIMIT 1),'') AS "serviceId"
 FROM scheduled_posts s`
	if id := c.Param("id"); id != "" {
		queryList(c, a, q+" WHERE "+singleSchedule+" ORDER BY s.send_time,s.id", id)
		return
	}
	queryList(c, a, q+" ORDER BY s.send_time,s.id")
}

func (a *App) deleteSchedule(c *gin.Context) {
	q := "DELETE FROM scheduled_posts s WHERE s.id=$1"
	args := []any{c.Param("item")}
	if id := c.Param("id"); id != "" {
		q = "DELETE FROM scheduled_posts s WHERE " + singleSchedule + " AND s.id=$2"
		args = []any{id, c.Param("item")}
	}
	tag, err := a.db.Exec(c, q, args...)
	if err != nil {
		fail(c, 500, "database_error", "删除失败")
		return
	}
	if tag.RowsAffected() == 0 {
		fail(c, 404, "not_found", "任务不存在")
		return
	}
	ok(c, gin.H{"deleted": true})
}

func (a *App) saveSchedule(c *gin.Context) {
	var in scheduleInput
	if c.ShouldBindJSON(&in) != nil {
		fail(c, 400, "invalid_request", "任务格式有误")
		return
	}
	group := c.Param("id")
	if group != "" {
		in.GroupIDs = []string{group}
		in.AllGroups = false
	} else {
		in.ServiceID = ""
	}
	in.Instruction = strings.TrimSpace(in.Instruction)
	in.Name = strings.TrimSpace(in.Name)
	if in.Name == "" && group != "" {
		in.Name = string([]rune(in.Instruction)[:min(40, len([]rune(in.Instruction)))])
	}
	at, err := time.Parse("15:04", in.SendTime)
	if in.Name == "" || len([]rune(in.Name)) > 120 || err != nil || at.Format("15:04") != in.SendTime || in.Instruction == "" || len([]rune(in.Instruction)) > 4000 ||
		(in.Source != "web" && in.Source != "group" && in.Source != "instruction") || (in.Format != "text" && in.Format != "image") || (!in.AllGroups && len(in.GroupIDs) == 0) ||
		(in.ServiceID != "" && parseUUID(in.ServiceID) != nil) {
		fail(c, 400, "invalid_request", "请检查任务名称、时间、内容和目标群")
		return
	}
	if in.AllGroups {
		in.GroupIDs = nil
	}
	for _, id := range in.GroupIDs {
		if parseUUID(id) != nil {
			fail(c, 400, "invalid_groups", "无效的群")
			return
		}
	}
	tx, err := a.db.Begin(c)
	if err != nil {
		fail(c, 500, "database_error", "保存失败")
		return
	}
	defer tx.Rollback(c)
	id := c.Param("item")
	args := []any{in.Name, in.SendTime, in.Instruction, in.Source, in.Format, in.Enabled, in.AllGroups}
	if id == "" {
		err = tx.QueryRow(c, `INSERT INTO scheduled_posts(name,send_time,instruction,source,format,enabled,all_groups)
 VALUES($1,$2::text::time,$3,$4,$5,$6,$7) RETURNING id::text`, args...).Scan(&id)
	} else {
		if group != "" {
			var found string
			err = tx.QueryRow(c, "SELECT s.id FROM scheduled_posts s WHERE "+singleSchedule+" AND s.id=$2 FOR UPDATE", group, id).Scan(&found)
		}
		if err == nil {
			err = tx.QueryRow(c, `UPDATE scheduled_posts SET name=$1,send_time=$2::text::time,instruction=$3,source=$4,format=$5,enabled=$6,all_groups=$7,
 revision=gen_random_uuid(),updated_at=now() WHERE id=$8 RETURNING id::text`, append(args, id)...).Scan(&id)
		}
	}
	if err == pgx.ErrNoRows {
		fail(c, 404, "not_found", "任务不存在")
		return
	}
	if err == nil {
		_, err = tx.Exec(c, "DELETE FROM schedule_targets WHERE schedule_id=$1 AND NOT(conversation_id=ANY(coalesce($2::uuid[],'{}')))", id, in.GroupIDs)
	}
	if err == nil && len(in.GroupIDs) > 0 {
		tag, e := tx.Exec(c, `INSERT INTO schedule_targets(schedule_id,conversation_id,service_id) SELECT $1,c.id,$3::uuid FROM conversations c
 WHERE c.type='group' AND c.id=ANY($2::uuid[]) AND ($3::uuid IS NULL OR EXISTS(SELECT FROM bot_services b WHERE b.id=$3 AND b.bot_id=c.bot_id))
 ON CONFLICT(schedule_id,conversation_id) DO UPDATE SET service_id=CASE WHEN $4 THEN excluded.service_id ELSE schedule_targets.service_id END`, id, in.GroupIDs, nullString(in.ServiceID), group != "")
		err = e
		if err == nil && int(tag.RowsAffected()) != len(in.GroupIDs) {
			fail(c, 400, "invalid_groups", "目标包含重复或无效的群")
			return
		}
	}
	if err != nil || tx.Commit(c) != nil {
		fail(c, 500, "database_error", "保存失败")
		return
	}
	ok(c, gin.H{"id": id})
}

func (a *App) testSchedule(c *gin.Context) {
	var in struct {
		GroupIDs  []string
		RequestID string
	}
	if c.ShouldBindJSON(&in) != nil || len(in.GroupIDs) == 0 || parseUUID(in.RequestID) != nil {
		fail(c, 400, "invalid_request", "请选择测试群")
		return
	}
	for _, id := range in.GroupIDs {
		if parseUUID(id) != nil {
			fail(c, 400, "invalid_groups", "测试群无效")
			return
		}
	}
	tx, err := a.db.Begin(c)
	if err != nil {
		fail(c, 500, "database_error", "无法测试")
		return
	}
	defer tx.Rollback(c)
	var source string
	err = tx.QueryRow(c, "SELECT source FROM scheduled_posts WHERE id=$1 FOR UPDATE", c.Param("item")).Scan(&source)
	if err != nil {
		fail(c, 404, "not_found", "任务不存在")
		return
	}
	var existing bool
	if err = tx.QueryRow(c, "SELECT EXISTS(SELECT FROM schedule_runs WHERE id=$1 AND schedule_id=$2 AND is_test)", in.RequestID, c.Param("item")).Scan(&existing); err != nil {
		fail(c, 500, "database_error", "读取任务失败")
		return
	}
	if !existing {
		n, e := createScheduleRun(c, tx, c.Param("item"), source, time.Now(), in.GroupIDs, in.RequestID)
		if e != nil {
			fail(c, 500, "database_error", "创建测试任务失败")
			return
		}
		if n != len(in.GroupIDs) {
			fail(c, 400, "invalid_groups", "测试群须在任务范围内，且群和机器人均已启用")
			return
		}
	}
	if err = tx.Commit(c); err != nil {
		fail(c, 500, "database_error", "提交失败")
		return
	}
	ok(c, gin.H{"runId": in.RequestID})
}
