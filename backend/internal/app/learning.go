package app

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/gin-gonic/gin"
)

func cleanAliases(in []string) []string {
	out := []string{}
	seen := map[string]bool{}
	for _, a := range in {
		a = strings.TrimSpace(a)
		if a == "" || seen[a] || len(out) >= 10 {
			continue
		}
		seen[a] = true
		out = append(out, a)
	}
	return out
}

func (a *App) createCandidate(ctx context.Context, kbID, question string, aliases []string, answer, source string, evidence any, confidence float64) (string, error) {
	aj, _ := json.Marshal(cleanAliases(aliases))
	ej, _ := json.Marshal(evidence)
	var kb any
	if kbID != "" {
		kb = kbID
	}
	var id string
	err := a.db.QueryRow(ctx, `INSERT INTO knowledge_candidates(knowledge_base_id,question,aliases,answer,source,evidence,confidence) VALUES($1,$2,$3::jsonb,$4,$5,$6::jsonb,$7) RETURNING id::text`,
		kb, strings.TrimSpace(question), string(aj), strings.TrimSpace(answer), source, string(ej), confidence).Scan(&id)
	return id, err
}

// approveCandidate publishes a candidate as a new FAQ entry (or merges it into mergeEntryID).
func (a *App) approveCandidate(ctx context.Context, candidateID, kbID, question string, aliases []string, answer, reviewer, note string) (string, error) {
	if kbID == "" {
		return "", fmt.Errorf("请选择要发布到的知识库")
	}
	var source string
	var evidence []byte
	if err := a.db.QueryRow(ctx, "SELECT source,evidence FROM knowledge_candidates WHERE id=$1 AND status='pending'", candidateID).Scan(&source, &evidence); err != nil {
		return "", fmt.Errorf("候选不存在或已处理")
	}
	entryID, err := a.createEntry(ctx, kbID, question, aliases, answer, source, evidence, reviewer)
	if err != nil {
		return "", err
	}
	var rev any
	if reviewer != "" {
		rev = reviewer
	}
	_, err = a.db.Exec(ctx, "UPDATE knowledge_candidates SET status='approved',entry_id=$1,knowledge_base_id=$2,review_note=$3,reviewed_by=$4,reviewed_at=now(),updated_at=now() WHERE id=$5", entryID, kbID, note, rev, candidateID)
	_, _ = a.db.Exec(ctx, "UPDATE knowledge_gaps SET status='resolved' WHERE candidate_id=$1", candidateID)
	return entryID, err
}

func (a *App) createEntry(ctx context.Context, kbID, question string, aliases []string, answer, source string, refs []byte, creator string) (string, error) {
	question, answer = strings.TrimSpace(question), strings.TrimSpace(answer)
	if question == "" || answer == "" {
		return "", fmt.Errorf("问题和答案必填")
	}
	aj, _ := json.Marshal(cleanAliases(aliases))
	if len(refs) == 0 {
		refs = []byte("[]")
	}
	var by any
	if creator != "" {
		by = creator
	}
	var id string
	if err := a.db.QueryRow(ctx, `INSERT INTO knowledge_entries(knowledge_base_id,question,aliases,answer,source,source_refs,created_by) VALUES($1,$2,$3::jsonb,$4,$5,$6::jsonb,$7) RETURNING id::text`,
		kbID, question, string(aj), answer, source, string(refs), by).Scan(&id); err != nil {
		return "", err
	}
	a.enqueueJob(ctx, "index_entry", map[string]any{"entryId": id}, "index_entry:"+id)
	return id, nil
}

// --- candidates ---

func (a *App) listCandidates(c *gin.Context) {
	status := c.DefaultQuery("status", "pending")
	queryList(c, a, `SELECT k.id::text,k.knowledge_base_id::text AS "knowledgeBaseId",kb.name AS "knowledgeBaseName",k.question,k.aliases,k.answer,k.source,k.evidence,k.confidence,k.status,k.entry_id::text AS "entryId",k.review_note AS "reviewNote",k.created_at AS "createdAt",k.reviewed_at AS "reviewedAt"
		FROM knowledge_candidates k LEFT JOIN knowledge_bases kb ON kb.id=k.knowledge_base_id WHERE ($1='all' OR k.status=$1) AND ($2='' OR k.evidence @> jsonb_build_array(jsonb_build_object('conversationId',$2::text))) ORDER BY k.created_at DESC LIMIT 300`, status, c.Query("conversationId"))
}

func (a *App) approveCandidateAPI(c *gin.Context) {
	var in struct {
		KnowledgeBaseID string   `json:"knowledgeBaseId"`
		Question        string   `json:"question"`
		Aliases         []string `json:"aliases"`
		Answer          string   `json:"answer"`
		Note            string   `json:"note"`
	}
	if c.ShouldBindJSON(&in) != nil {
		fail(c, 400, "invalid_request", "参数格式无效")
		return
	}
	id, err := a.approveCandidate(c, c.Param("id"), in.KnowledgeBaseID, in.Question, in.Aliases, in.Answer, c.GetString("userId"), in.Note)
	if err != nil {
		fail(c, 400, "approve_failed", err.Error())
		return
	}
	a.audit(c, "knowledge.candidate.approve", "knowledge_candidate", c.Param("id"), map[string]any{"entryId": id})
	ok(c, gin.H{"entryId": id})
}

func (a *App) rejectCandidate(c *gin.Context) {
	var in struct {
		Note string `json:"note"`
	}
	_ = c.ShouldBindJSON(&in)
	tag, err := a.db.Exec(c, "UPDATE knowledge_candidates SET status='rejected',review_note=$1,reviewed_by=$2,reviewed_at=now(),updated_at=now() WHERE id=$3 AND status='pending'", in.Note, c.GetString("userId"), c.Param("id"))
	if err != nil || tag.RowsAffected() == 0 {
		fail(c, 404, "not_found", "候选不存在或已处理")
		return
	}
	ok(c, gin.H{"rejected": true})
}

// --- entries ---

func (a *App) listEntries(c *gin.Context) {
	args := []any{}
	where := []string{"1=1"}
	if kb := c.Query("knowledgeBaseId"); kb != "" {
		args = append(args, kb)
		where = append(where, "e.knowledge_base_id=$"+itoa(len(args)))
	}
	if st := c.Query("status"); st != "" && st != "all" {
		args = append(args, st)
		where = append(where, "e.status=$"+itoa(len(args)))
	}
	if q := strings.TrimSpace(c.Query("q")); q != "" {
		args = append(args, "%"+q+"%")
		n := itoa(len(args))
		where = append(where, "(e.question ILIKE $"+n+" OR e.answer ILIKE $"+n+")")
	}
	queryList(c, a, `SELECT e.id::text,e.knowledge_base_id::text AS "knowledgeBaseId",kb.name AS "knowledgeBaseName",e.question,e.aliases,e.answer,e.source,e.status,e.index_status AS "indexStatus",e.index_error AS "indexError",e.version,e.hit_count AS "hitCount",e.helpful_count AS "helpfulCount",e.unhelpful_count AS "unhelpfulCount",e.last_hit_at AS "lastHitAt",e.created_at AS "createdAt",e.updated_at AS "updatedAt"
		FROM knowledge_entries e JOIN knowledge_bases kb ON kb.id=e.knowledge_base_id WHERE `+strings.Join(where, " AND ")+` ORDER BY e.updated_at DESC LIMIT 500`, args...)
}

type entryInput struct {
	KnowledgeBaseID string   `json:"knowledgeBaseId"`
	Question        string   `json:"question"`
	Aliases         []string `json:"aliases"`
	Answer          string   `json:"answer"`
	Status          string   `json:"status"`
}

func (a *App) createEntryAPI(c *gin.Context) {
	var in entryInput
	if c.ShouldBindJSON(&in) != nil || in.KnowledgeBaseID == "" {
		fail(c, 400, "invalid_request", "知识库、问题和答案必填")
		return
	}
	id, err := a.createEntry(c, in.KnowledgeBaseID, in.Question, in.Aliases, in.Answer, "manual", nil, c.GetString("userId"))
	if err != nil {
		fail(c, 400, "create_failed", err.Error())
		return
	}
	created(c, gin.H{"id": id})
}

func (a *App) updateEntryAPI(c *gin.Context) {
	var in entryInput
	if c.ShouldBindJSON(&in) != nil || strings.TrimSpace(in.Question) == "" || strings.TrimSpace(in.Answer) == "" {
		fail(c, 400, "invalid_request", "问题和答案必填")
		return
	}
	switch in.Status {
	case "published", "archived", "review":
	default:
		in.Status = "published"
	}
	aj, _ := json.Marshal(cleanAliases(in.Aliases))
	tag, err := a.db.Exec(c, `UPDATE knowledge_entries SET question=$1,aliases=$2::jsonb,answer=$3,status=$4,version=version+1,index_status='pending',updated_at=now(),
		helpful_count=CASE WHEN status='review' AND $4='published' THEN 0 ELSE helpful_count END,unhelpful_count=CASE WHEN status='review' AND $4='published' THEN 0 ELSE unhelpful_count END WHERE id=$5`,
		strings.TrimSpace(in.Question), string(aj), strings.TrimSpace(in.Answer), in.Status, c.Param("id"))
	if err != nil || tag.RowsAffected() == 0 {
		fail(c, 404, "not_found", "条目不存在")
		return
	}
	a.enqueueJob(c, "index_entry", map[string]any{"entryId": c.Param("id")}, "index_entry:"+c.Param("id"))
	ok(c, gin.H{"updated": true})
}

func (a *App) deleteEntryAPI(c *gin.Context) {
	_, _ = a.db.Exec(c, "DELETE FROM knowledge_entries WHERE id=$1", c.Param("id"))
	ok(c, gin.H{"deleted": true})
}

// --- gaps ---

func (a *App) listGaps(c *gin.Context) {
	status := c.DefaultQuery("status", "open")
	queryList(c, a, `SELECT id::text,question,occurrences,sample_message_ids AS "sampleMessageIds",status,candidate_id::text AS "candidateId",first_seen_at AS "firstSeenAt",last_seen_at AS "lastSeenAt"
		FROM knowledge_gaps WHERE ($1='all' OR status=$1) ORDER BY occurrences DESC,last_seen_at DESC LIMIT 300`, status)
}

func (a *App) answerGap(c *gin.Context) {
	var in entryInput
	if c.ShouldBindJSON(&in) != nil || in.KnowledgeBaseID == "" {
		fail(c, 400, "invalid_request", "知识库、问题和答案必填")
		return
	}
	var question string
	if err := a.db.QueryRow(c, "SELECT question FROM knowledge_gaps WHERE id=$1", c.Param("id")).Scan(&question); err != nil {
		fail(c, 404, "not_found", "缺口不存在")
		return
	}
	if strings.TrimSpace(in.Question) == "" {
		in.Question = question
	}
	refs, _ := json.Marshal([]map[string]any{{"gapId": c.Param("id")}})
	id, err := a.createEntry(c, in.KnowledgeBaseID, in.Question, in.Aliases, in.Answer, "gap", refs, c.GetString("userId"))
	if err != nil {
		fail(c, 400, "create_failed", err.Error())
		return
	}
	_, _ = a.db.Exec(c, "UPDATE knowledge_gaps SET status='resolved' WHERE id=$1", c.Param("id"))
	ok(c, gin.H{"entryId": id})
}

func (a *App) ignoreGap(c *gin.Context) {
	_, _ = a.db.Exec(c, "UPDATE knowledge_gaps SET status='ignored' WHERE id=$1", c.Param("id"))
	ok(c, gin.H{"ignored": true})
}

// --- feedback ---

func (a *App) createFeedback(c *gin.Context) {
	var in struct {
		AnswerID        string `json:"answerId"`
		Rating          string `json:"rating"`
		Comment         string `json:"comment"`
		CorrectedAnswer string `json:"correctedAnswer"`
		Publish         bool   `json:"publish"`
		KnowledgeBaseID string `json:"knowledgeBaseId"`
		Question        string `json:"question"`
	}
	if c.ShouldBindJSON(&in) != nil {
		fail(c, 400, "invalid_request", "参数格式无效")
		return
	}
	switch in.Rating {
	case "good", "bad", "too_long", "stiff", "wrong":
	default:
		fail(c, 400, "invalid_rating", "评价类型无效")
		return
	}
	// The route id is the inbound message; feedback attaches to its first AI answer.
	answerID := in.AnswerID
	var question string
	if err := a.db.QueryRow(c, "SELECT content FROM messages WHERE id=$1", c.Param("id")).Scan(&question); err != nil {
		fail(c, 404, "not_found", "消息不存在")
		return
	}
	if answerID == "" {
		_ = a.db.QueryRow(c, `SELECT o.id::text FROM messages i JOIN messages o ON o.conversation_id=i.conversation_id AND o.reply_to_message_id=i.platform_message_id AND o.direction='outbound' AND o.event_type IN ('AI_RESPONSE','HUMAN_REPLY') WHERE i.id=$1 ORDER BY o.event_at LIMIT 1`, c.Param("id")).Scan(&answerID)
	}
	if answerID == "" {
		fail(c, 400, "no_answer", "该消息还没有回复，无法评价")
		return
	}
	if _, err := a.db.Exec(c, "INSERT INTO message_feedback(message_id,source,rating,comment,corrected_answer,created_by) VALUES($1,'admin',$2,$3,$4,$5)", answerID, in.Rating, in.Comment, strings.TrimSpace(in.CorrectedAnswer), c.GetString("userId")); err != nil {
		fail(c, 500, "database_error", "保存失败")
		return
	}
	if in.Rating == "good" || in.Rating == "wrong" || in.Rating == "bad" {
		a.applyEntryFeedback(c, answerID, in.Rating == "good")
	}
	result := gin.H{"saved": true}
	if corrected := strings.TrimSpace(in.CorrectedAnswer); corrected != "" {
		q := firstNonEmpty(strings.TrimSpace(in.Question), question)
		evidence := []map[string]any{{"messageId": c.Param("id"), "answerId": answerID, "question": question}}
		candID, err := a.createCandidate(c, in.KnowledgeBaseID, q, nil, corrected, "feedback", evidence, 0.95)
		if err == nil && in.Publish {
			entryID, err := a.approveCandidate(c, candID, in.KnowledgeBaseID, q, nil, corrected, c.GetString("userId"), "管理员纠错直接发布")
			if err != nil {
				fail(c, 400, "publish_failed", err.Error())
				return
			}
			result["entryId"] = entryID
		}
		result["candidateId"] = candID
	}
	ok(c, result)
}

func (a *App) listFeedback(c *gin.Context) {
	queryList(c, a, `SELECT f.id::text,f.is_read AS "read",f.source,f.rating,f.comment,f.corrected_answer AS "correctedAnswer",f.created_at AS "createdAt",o.content AS answer,
		COALESCE(i.content,'') AS question,i.id::text AS "messageId",c.name AS "conversationName"
		FROM message_feedback f JOIN messages o ON o.id=f.message_id JOIN conversations c ON c.id=o.conversation_id
		LEFT JOIN messages i ON i.conversation_id=o.conversation_id AND i.platform_message_id=o.reply_to_message_id AND i.direction='inbound'
		WHERE ($1='' OR f.rating=$1) ORDER BY f.created_at DESC LIMIT 300`, c.Query("rating"))
}

func (a *App) markFeedbackRead(c *gin.Context) {
	var in struct {
		IDs []string
		All bool
	}
	if id := c.Param("id"); id != "" {
		in.IDs = []string{id}
	} else if c.ShouldBindJSON(&in) != nil || in.All == (len(in.IDs) > 0) || len(in.IDs) > 1000 {
		fail(c, 400, "invalid_request", "请指定反馈编号或 all")
		return
	}
	for _, id := range in.IDs {
		if parseUUID(id) != nil {
			fail(c, 400, "invalid_ids", "反馈编号无效")
			return
		}
	}
	tag, err := a.db.Exec(c, "UPDATE message_feedback SET is_read=true WHERE NOT is_read AND ($1 OR id=ANY($2::uuid[]))", in.All, in.IDs)
	if err != nil {
		fail(c, 500, "database_error", "标记已读失败")
		return
	}
	ok(c, gin.H{"updated": tag.RowsAffected()})
}

func (a *App) learningStats(c *gin.Context) {
	var pending, entries, openGaps, feedback7d, bad7d, unread, memories, openHandoffs int
	err := a.db.QueryRow(c, `SELECT (SELECT count(*) FROM knowledge_candidates WHERE status='pending'),
		(SELECT count(*) FROM knowledge_entries WHERE status='published'),
		(SELECT count(*) FROM knowledge_gaps WHERE status='open'),
		(SELECT count(*) FROM message_feedback WHERE created_at>now()-interval '7 days'),
		(SELECT count(*) FROM message_feedback WHERE created_at>now()-interval '7 days' AND rating IN ('bad','wrong','unhelpful')),
		(SELECT count(*) FROM message_feedback WHERE NOT is_read AND rating IN ('bad','wrong','unhelpful')),
 (SELECT count(*) FROM user_memories WHERE status='active'),
		(SELECT count(*) FROM handoffs WHERE status IN ('open','processing'))`).Scan(&pending, &entries, &openGaps, &feedback7d, &bad7d, &unread, &memories, &openHandoffs)
	if err != nil {
		fail(c, 500, "database_error", "读取学习统计失败")
		return
	}
	ok(c, gin.H{"unreadNegativeFeedback": unread, "pendingCandidates": pending, "publishedEntries": entries, "openGaps": openGaps, "feedback7d": feedback7d, "negativeFeedback7d": bad7d, "activeMemories": memories, "openHandoffs": openHandoffs})
}

func (a *App) bulkDeleteLearning(c *gin.Context) {
	tables := map[string]string{"candidates": "knowledge_candidates", "entries": "knowledge_entries", "gaps": "knowledge_gaps", "feedback": "message_feedback", "memories": "user_memories"}
	table, valid := tables[c.Param("collection")]
	if !valid {
		fail(c, 404, "not_found", "未知的知识审核列表")
		return
	}
	var in struct{ IDs []string }
	if c.ShouldBindJSON(&in) != nil || len(in.IDs) == 0 || len(in.IDs) > 1000 {
		fail(c, 400, "invalid_ids", "请勾选 1 到 1000 条记录")
		return
	}
	for _, id := range in.IDs {
		if parseUUID(id) != nil {
			fail(c, 400, "invalid_ids", "记录编号无效")
			return
		}
	}
	// Table names come exclusively from the allowlist. PostgreSQL applies FK cleanup atomically.
	tag, err := a.db.Exec(c, "DELETE FROM "+table+" WHERE id=ANY($1::uuid[])", in.IDs)
	if err != nil {
		fail(c, 500, "delete_failed", "删除失败")
		return
	}
	ok(c, gin.H{"deleted": tag.RowsAffected()})
}
