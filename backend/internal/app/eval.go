package app

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	"ai-bot/backend/internal/agent"
	"github.com/gin-gonic/gin"
)

func (a *App) listEvalCases(c *gin.Context) {
	queryList(c, a, `SELECT id::text,question,expected,source,entry_id::text AS "entryId",enabled,updated_at AS "updatedAt" FROM eval_cases ORDER BY created_at DESC LIMIT 1000`)
}

func (a *App) saveEvalCase(c *gin.Context) {
	var in struct {
		Question string `json:"question"`
		Expected string `json:"expected"`
		Enabled  bool   `json:"enabled"`
	}
	if c.ShouldBindJSON(&in) != nil || strings.TrimSpace(in.Question) == "" {
		fail(c, 400, "invalid_request", "问题必填")
		return
	}
	var err error
	if id := c.Param("id"); id != "" {
		_, err = a.db.Exec(c, "UPDATE eval_cases SET question=$1,expected=$2,enabled=$3,updated_at=now() WHERE id=$4", strings.TrimSpace(in.Question), strings.TrimSpace(in.Expected), in.Enabled, id)
	} else {
		_, err = a.db.Exec(c, "INSERT INTO eval_cases(question,expected,enabled) VALUES($1,$2,$3)", strings.TrimSpace(in.Question), strings.TrimSpace(in.Expected), in.Enabled)
	}
	if err != nil {
		fail(c, 500, "database_error", "保存失败")
		return
	}
	ok(c, gin.H{"saved": true})
}

func (a *App) deleteEvalCase(c *gin.Context) {
	_, _ = a.db.Exec(c, "DELETE FROM eval_cases WHERE id=$1", c.Param("id"))
	ok(c, gin.H{"deleted": true})
}

func (a *App) evalCasesFromEntries(c *gin.Context) {
	tag, err := a.db.Exec(c, `INSERT INTO eval_cases(question,expected,source,entry_id) SELECT question,answer,'entry',id FROM knowledge_entries WHERE status='published' AND source<>'announcement' ON CONFLICT (entry_id) WHERE entry_id IS NOT NULL DO NOTHING`)
	if err != nil {
		fail(c, 500, "database_error", "生成失败")
		return
	}
	ok(c, gin.H{"created": tag.RowsAffected()})
}

func (a *App) listEvalRuns(c *gin.Context) {
	queryList(c, a, `SELECT r.id::text,r.name,r.chat_profile_id::text AS "chatProfileId",COALESCE(mp.name,'') AS "chatProfileName",r.knowledge_base_ids AS "knowledgeBaseIds",r.status,r.total,r.completed,r.avg_score AS "avgScore",r.avg_length AS "avgLength",r.error,r.created_at AS "createdAt",r.finished_at AS "finishedAt"
		FROM eval_runs r LEFT JOIN model_profiles mp ON mp.id=r.chat_profile_id ORDER BY r.created_at DESC LIMIT 100`)
}

func (a *App) getEvalRun(c *gin.Context) {
	queryList(c, a, `SELECT id::text,case_id::text AS "caseId",question,expected,answer,score,verdict,tools_used AS "toolsUsed",latency_ms AS "latencyMs",error FROM eval_results WHERE run_id=$1 ORDER BY created_at`, c.Param("id"))
}

func (a *App) createEvalRun(c *gin.Context) {
	var in struct {
		Name             string   `json:"name"`
		ChatProfileID    string   `json:"chatProfileId"`
		KnowledgeBaseIDs []string `json:"knowledgeBaseIds"`
	}
	if c.ShouldBindJSON(&in) != nil {
		fail(c, 400, "invalid_request", "参数格式无效")
		return
	}
	if in.ChatProfileID == "" {
		in.ChatProfileID = a.defaultChatProfile(c)
	}
	if in.ChatProfileID == "" {
		fail(c, 400, "no_model", "请先配置对话模型")
		return
	}
	var total int
	_ = a.db.QueryRow(c, "SELECT count(*) FROM eval_cases WHERE enabled").Scan(&total)
	if total == 0 {
		fail(c, 400, "no_cases", "没有启用的评测用例")
		return
	}
	if in.KnowledgeBaseIDs == nil {
		in.KnowledgeBaseIDs = []string{}
	}
	kbs, _ := json.Marshal(in.KnowledgeBaseIDs)
	name := strings.TrimSpace(in.Name)
	if name == "" {
		name = "评测 " + time.Now().In(chinaLocation).Format("01-02 15:04")
	}
	var id string
	if err := a.db.QueryRow(c, "INSERT INTO eval_runs(name,chat_profile_id,knowledge_base_ids,total,created_by) VALUES($1,$2,$3::jsonb,$4,$5) RETURNING id::text", name, in.ChatProfileID, string(kbs), total, c.GetString("userId")).Scan(&id); err != nil {
		fail(c, 500, "database_error", "创建失败")
		return
	}
	a.enqueueJob(c, "eval_run", map[string]any{"runId": id}, "eval_run:"+id)
	created(c, gin.H{"id": id})
}

type judgeResult struct {
	Score   float64 `json:"score"`
	Verdict string  `json:"verdict"`
}

// jobEvalRun runs every enabled case through the agent in dry-run mode and grades it.
func (a *App) jobEvalRun(ctx context.Context, runID string) error {
	var profileID string
	var kbRaw []byte
	if err := a.db.QueryRow(ctx, "SELECT COALESCE(chat_profile_id::text,''),knowledge_base_ids FROM eval_runs WHERE id=$1", runID).Scan(&profileID, &kbRaw); err != nil {
		return nil
	}
	var kbs []string
	_ = json.Unmarshal(kbRaw, &kbs)
	_, _ = a.db.Exec(ctx, "UPDATE eval_runs SET status='running' WHERE id=$1", runID)
	fail := func(err error) error {
		_, _ = a.db.Exec(ctx, "UPDATE eval_runs SET status='failed',error=$1,finished_at=now() WHERE id=$2", err.Error(), runID)
		return nil
	}
	client, _, err := a.loadModel(ctx, profileID)
	if err != nil {
		return fail(fmt.Errorf("模型不可用: %w", err))
	}
	settings := a.agentSettings(ctx)
	snapshot, stale, snapOK := a.liveSnapshot(ctx, settings)
	rows, err := a.db.Query(ctx, "SELECT id::text,question,expected FROM eval_cases WHERE enabled ORDER BY created_at")
	if err != nil {
		return fail(err)
	}
	type ecase struct{ id, q, e string }
	var cases []ecase
	for rows.Next() {
		var x ecase
		if rows.Scan(&x.id, &x.q, &x.e) == nil {
			cases = append(cases, x)
		}
	}
	rows.Close()
	_, _ = a.db.Exec(ctx, "DELETE FROM eval_results WHERE run_id=$1", runID)
	_, _ = a.db.Exec(ctx, "UPDATE eval_runs SET total=$1,completed=0 WHERE id=$2", len(cases), runID)
	var scoreSum, lenSum float64
	scored := 0
	for i, cs := range cases {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		rt := &turnRuntime{settings: settings, conversationType: "private", senderName: "评测用户", knowledgeBases: kbs, snapshot: snapshot, snapshotOK: snapOK, snapshotStale: stale, dryRun: true}
		knowledge, hits, _ := a.retrieve(ctx, kbs, cs.q, &rt.embeddings)
		rt.hits = hits
		started := time.Now()
		out := a.runAgent(ctx, agentInput{rt: rt, client: client, profileID: profileID, knowledge: knowledge, userContent: cs.q})
		latency := time.Since(started).Milliseconds()
		answer := strings.Join(a.shapeReply(settings, "private", out.content), "\n\n")
		toolsUsed, _ := json.Marshal(rt.toolsUsed)
		var score any
		verdict := ""
		if out.err == nil {
			var jr judgeResult
			input := fmt.Sprintf("问题：%s\n期望要点：%s\n实际回答：%s", cs.q, firstNonEmpty(cs.e, "（未提供，按常识判断是否合理）"), answer)
			if err := a.auxJSON(ctx, settings, profileID, "", "judge", agent.JudgePrompt, input, &jr); err == nil {
				score, verdict = jr.Score, jr.Verdict
				scoreSum += jr.Score
				scored++
			} else {
				verdict = "评分失败：" + err.Error()
			}
			lenSum += float64(utf8.RuneCountInString(answer))
		}
		_, _ = a.db.Exec(ctx, "INSERT INTO eval_results(run_id,case_id,question,expected,answer,score,verdict,tools_used,latency_ms,error) VALUES($1,$2,$3,$4,$5,$6,$7,$8::jsonb,$9,$10)",
			runID, cs.id, cs.q, cs.e, answer, score, verdict, string(toolsUsed), latency, errorText(out.err))
		_, _ = a.db.Exec(ctx, "UPDATE eval_runs SET completed=$1 WHERE id=$2", i+1, runID)
	}
	var avgScore, avgLen any
	if scored > 0 {
		avgScore = scoreSum / float64(scored)
		avgLen = lenSum / float64(scored)
	}
	_, err = a.db.Exec(ctx, "UPDATE eval_runs SET status='completed',avg_score=$1,avg_length=$2,finished_at=now() WHERE id=$3", avgScore, avgLen, runID)
	return err
}
