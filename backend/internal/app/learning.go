package app

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/jackc/pgx/v5"
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
	return insertCandidate(ctx, a.db, kbID, question, aliases, answer, source, evidence, confidence, replyImage{})
}

type rowQuerier interface {
	QueryRow(context.Context, string, ...any) pgx.Row
}

func insertCandidate(ctx context.Context, db rowQuerier, kbID, question string, aliases []string, answer, source string, evidence any, confidence float64, img replyImage) (string, error) {
	aj, _ := json.Marshal(cleanAliases(aliases))
	ej, _ := json.Marshal(evidence)
	var kb any
	if kbID != "" {
		kb = kbID
	}
	items, e := img.plan.resolve(nil)
	if e != nil {
		return "", e
	}
	meta, data := materialColumns(items)
	var id string
	err := db.QueryRow(ctx, `INSERT INTO knowledge_candidates(knowledge_base_id,question,aliases,answer,source,evidence,confidence,image_mime,image_data,material_meta,material_data) VALUES($1,$2,$3::jsonb,$4,$5,$6::jsonb,$7,$8,$9,$10,$11) RETURNING id::text`,
		kb, strings.TrimSpace(question), string(aj), strings.TrimSpace(answer), source, string(ej), confidence, nullString(img.mime), img.data, meta, data).Scan(&id)
	return id, err
}

// approveCandidate publishes a candidate and queues indexing in one transaction.
func (a *App) approveCandidate(ctx context.Context, candidateID, kbID, question string, aliases []string, answer, reviewer, note string, replacement *replyImage) (string, error) {
	tx, err := a.db.Begin(ctx)
	if err != nil {
		return "", err
	}
	defer tx.Rollback(ctx)
	id, err := publishCandidate(ctx, tx, candidateID, kbID, question, aliases, answer, reviewer, note, replacement)
	if err != nil {
		return "", err
	}
	return id, tx.Commit(ctx)
}

func publishCandidate(ctx context.Context, tx pgx.Tx, candidateID, kbID, question string, aliases []string, answer, reviewer, note string, replacement *replyImage) (string, error) {
	if kbID == "" {
		return "", fmt.Errorf("请选择要发布到的知识库")
	}
	var source string
	var evidence []byte
	var img replyImage
	if err := tx.QueryRow(ctx, "SELECT source,evidence FROM knowledge_candidates WHERE id=$1 AND status='pending' FOR UPDATE", candidateID).Scan(&source, &evidence); err != nil {
		return "", fmt.Errorf("候选不存在或已处理")
	}
	old, err := loadMaterials(ctx, tx, "knowledge_candidates", candidateID, false)
	if err != nil {
		return "", err
	}
	if replacement == nil {
		img = replyImage{plan: uploadedPlan(old)}
	} else if replacement.plan != nil {
		items, err := replacement.plan.resolve(old)
		if err != nil {
			return "", err
		}
		img = replyImage{plan: uploadedPlan(items)}
	} else {
		img = *replacement
	}
	entryID, err := insertEntry(ctx, tx, kbID, question, aliases, answer, source, evidence, reviewer, img)
	if err != nil {
		return "", err
	}
	var rev any
	if reviewer != "" {
		rev = reviewer
	}
	_, err = tx.Exec(ctx, "UPDATE knowledge_candidates SET status='approved',entry_id=$1,knowledge_base_id=$2,review_note=$3,reviewed_by=$4,reviewed_at=now(),updated_at=now() WHERE id=$5", entryID, kbID, note, rev, candidateID)
	if err != nil {
		return "", err
	}
	_, err = tx.Exec(ctx, "UPDATE knowledge_gaps SET status='resolved' WHERE candidate_id=$1", candidateID)
	return entryID, err
}

func (a *App) createEntry(ctx context.Context, kbID, question string, aliases []string, answer, source string, refs []byte, creator string, img replyImage) (string, error) {
	tx, err := a.db.Begin(ctx)
	if err != nil {
		return "", err
	}
	defer tx.Rollback(ctx)
	id, err := insertEntry(ctx, tx, kbID, question, aliases, answer, source, refs, creator, img)
	if err != nil {
		return "", err
	}
	return id, tx.Commit(ctx)
}

func insertEntry(ctx context.Context, tx pgx.Tx, kbID, question string, aliases []string, answer, source string, refs []byte, creator string, img replyImage) (string, error) {
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
	items, e := img.plan.resolve(nil)
	if e != nil {
		return "", e
	}
	meta, data := materialColumns(items)
	var id string
	if err := tx.QueryRow(ctx, `INSERT INTO knowledge_entries(knowledge_base_id,question,aliases,answer,source,source_refs,created_by,image_mime,image_data,material_meta,material_data) VALUES($1,$2,$3::jsonb,$4,$5,$6::jsonb,$7,$8,$9,$10,$11) RETURNING id::text`,
		kbID, question, string(aj), answer, source, string(refs), by, nullString(img.mime), img.data, meta, data).Scan(&id); err != nil {
		return "", err
	}
	_, err := tx.Exec(ctx, `INSERT INTO agent_jobs(kind,payload,dedupe_key) VALUES('index_entry',jsonb_build_object('entryId',$1::text),'index_entry:'||$1::text)`, id)
	return id, err
}

// --- candidates ---

func (a *App) listCandidates(c *gin.Context) {
	status := c.DefaultQuery("status", "pending")
	queryList(c, a, `SELECT k.id::text,`+materialListSQL("k", "candidates")+` AS materials,k.image_data IS NOT NULL AS "hasImage",CASE WHEN k.image_data IS NOT NULL THEN '/api/knowledge/candidates/'||k.id::text||'/image' END AS "imageUrl",k.knowledge_base_id::text AS "knowledgeBaseId",kb.name AS "knowledgeBaseName",k.question,k.aliases,k.answer,k.source,k.evidence,k.confidence,k.status,k.entry_id::text AS "entryId",k.review_note AS "reviewNote",k.created_at AS "createdAt",k.reviewed_at AS "reviewedAt"
		FROM knowledge_candidates k LEFT JOIN knowledge_bases kb ON kb.id=k.knowledge_base_id WHERE ($1='all' OR k.status=$1) AND ($2='' OR k.evidence @> jsonb_build_array(jsonb_build_object('conversationId',$2::text))) ORDER BY k.created_at DESC LIMIT 300`, status, c.Query("conversationId"))
}

func (a *App) approveCandidateAPI(c *gin.Context) {
	var in entryInput
	img, err := in.bind(c)
	if err != nil {
		fail(c, 400, "invalid_request", err.Error())
		return
	}
	var replacement *replyImage
	if img.plan != nil || img.data != nil || in.RemoveImage {
		replacement = &img
	}
	id, err := a.approveCandidate(c, c.Param("id"), in.KnowledgeBaseID, in.Question, in.Aliases, in.Answer, c.GetString("userId"), in.Note, replacement)
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
	queryList(c, a, `SELECT e.id::text,`+materialListSQL("e", "entries")+` AS materials,e.image_data IS NOT NULL AS "hasImage",CASE WHEN e.image_data IS NOT NULL THEN '/api/knowledge/entries/'||e.id::text||'/image' END AS "imageUrl",e.knowledge_base_id::text AS "knowledgeBaseId",kb.name AS "knowledgeBaseName",e.question,e.aliases,e.answer,e.source,e.status,e.index_status AS "indexStatus",e.index_error AS "indexError",e.version,e.hit_count AS "hitCount",e.helpful_count AS "helpfulCount",e.unhelpful_count AS "unhelpfulCount",e.last_hit_at AS "lastHitAt",e.created_at AS "createdAt",e.updated_at AS "updatedAt"
		FROM knowledge_entries e JOIN knowledge_bases kb ON kb.id=e.knowledge_base_id WHERE `+strings.Join(where, " AND ")+` ORDER BY e.updated_at DESC LIMIT 500`, args...)
}

type entryInput struct {
	KnowledgeBaseID string   `json:"knowledgeBaseId" form:"knowledgeBaseId"`
	Question        string   `json:"question" form:"question"`
	Aliases         []string `json:"aliases" form:"aliases"`
	Answer          string   `json:"answer" form:"answer"`
	Status          string   `json:"status" form:"status"`
	Note            string   `json:"note" form:"note"`
	Materials       []string `json:"materials" form:"-"`
	RemoveImage     bool     `json:"removeImage" form:"removeImage"`
}

func (in *entryInput) bind(c *gin.Context) (replyImage, error) {
	img, err := bindReplyImage(c, in)
	if in.Materials != nil {
		img.plan = &materialPlan{order: in.Materials}
	}
	if err == nil && in.RemoveImage && (img.data != nil || img.plan != nil) {
		err = fmt.Errorf("替换图片和移除图片不能同时选择")
	}
	if err == nil && in.RemoveImage {
		img.plan = &materialPlan{}
	}
	return img, err
}

func (a *App) createEntryAPI(c *gin.Context) {
	var in entryInput
	img, err := in.bind(c)
	if err != nil {
		fail(c, 400, "invalid_request", err.Error())
		return
	}
	if in.KnowledgeBaseID == "" {
		fail(c, 400, "invalid_request", "知识库、问题和答案必填")
		return
	}
	id, err := a.createEntry(c, in.KnowledgeBaseID, in.Question, in.Aliases, in.Answer, "manual", nil, c.GetString("userId"), img)
	if err != nil {
		fail(c, 400, "create_failed", err.Error())
		return
	}
	created(c, gin.H{"id": id})
}

func (a *App) updateEntryAPI(c *gin.Context) {
	var in entryInput
	img, err := in.bind(c)
	if err != nil {
		fail(c, 400, "invalid_request", err.Error())
		return
	}
	if strings.TrimSpace(in.Question) == "" || strings.TrimSpace(in.Answer) == "" {
		fail(c, 400, "invalid_request", "问题和答案必填")
		return
	}
	switch in.Status {
	case "published", "archived", "review":
	default:
		in.Status = "published"
	}
	aj, _ := json.Marshal(cleanAliases(in.Aliases))
	tx, err := a.db.Begin(c)
	if err != nil {
		fail(c, 500, "database_error", "更新失败")
		return
	}
	defer tx.Rollback(c)
	var meta []byte
	var data [][]byte
	if img.plan != nil {
		old, e := loadMaterials(c, tx, "knowledge_entries", c.Param("id"), true)
		if e != nil {
			fail(c, 404, "not_found", "条目不存在")
			return
		}
		items, e := img.plan.resolve(old)
		if e != nil {
			fail(c, 400, "invalid_request", e.Error())
			return
		}
		meta, data = materialColumns(items)
	}
	tag, err := tx.Exec(c, `UPDATE knowledge_entries SET material_meta=coalesce($9,material_meta),material_data=coalesce($10,material_data),image_mime=CASE WHEN $6 THEN $7 ELSE image_mime END,image_data=CASE WHEN $6 THEN $8 ELSE image_data END,question=$1,aliases=$2::jsonb,answer=$3,status=$4,version=version+1,index_status='pending',updated_at=now(),
		helpful_count=CASE WHEN status='review' AND $4='published' THEN 0 ELSE helpful_count END,unhelpful_count=CASE WHEN status='review' AND $4='published' THEN 0 ELSE unhelpful_count END WHERE id=$5`,
		strings.TrimSpace(in.Question), string(aj), strings.TrimSpace(in.Answer), in.Status, c.Param("id"), img.data != nil || in.RemoveImage, nullString(img.mime), img.data, meta, data)
	if err != nil || tag.RowsAffected() == 0 {
		fail(c, 404, "not_found", "条目不存在")
		return
	}
	_, err = tx.Exec(c, "DELETE FROM knowledge_chunks WHERE entry_id=$1", c.Param("id"))
	if err == nil {
		_, err = tx.Exec(c, `INSERT INTO agent_jobs(kind,payload) VALUES('index_entry',jsonb_build_object('entryId',$1::text))`, c.Param("id"))
	}
	if err != nil || tx.Commit(c) != nil {
		fail(c, 500, "database_error", "保存问答索引任务失败")
		return
	}
	ok(c, gin.H{"updated": true})
}

func (a *App) deleteEntryAPI(c *gin.Context) {
	_, _ = a.db.Exec(c, "DELETE FROM knowledge_entries WHERE id=$1", c.Param("id"))
	ok(c, gin.H{"deleted": true})
}

// --- gaps ---

func (a *App) listGaps(c *gin.Context) {
	status := c.DefaultQuery("status", "open")
	queryList(c, a, `SELECT g.id::text,g.knowledge_base_id::text AS "knowledgeBaseId",k.name AS "knowledgeBaseName",question,occurrences,sample_message_ids AS "sampleMessageIds",status,candidate_id::text AS "candidateId",first_seen_at AS "firstSeenAt",last_seen_at AS "lastSeenAt"
		FROM knowledge_gaps g LEFT JOIN knowledge_bases k ON k.id=g.knowledge_base_id WHERE ($1='all' OR status=$1) AND ($2='' OR coalesce(g.knowledge_base_id::text,'unassigned')=$2) ORDER BY occurrences DESC,last_seen_at DESC LIMIT 300`, status, c.Query("knowledgeBaseId"))
}

func (a *App) answerGap(c *gin.Context) {
	var in entryInput
	img, err := in.bind(c)
	if err != nil {
		fail(c, 400, "invalid_request", err.Error())
		return
	}
	if in.KnowledgeBaseID == "" {
		fail(c, 400, "invalid_request", "知识库、问题和答案必填")
		return
	}
	var question string
	tx, err := a.db.Begin(c)
	if err != nil {
		fail(c, 500, "database_error", "保存失败")
		return
	}
	defer tx.Rollback(c)
	if err := tx.QueryRow(c, "SELECT question FROM knowledge_gaps WHERE id=$1 AND status<>'resolved' FOR UPDATE", c.Param("id")).Scan(&question); err != nil {
		fail(c, 404, "not_found", "缺口不存在")
		return
	}
	if strings.TrimSpace(in.Question) == "" {
		in.Question = question
	}
	refs, _ := json.Marshal([]map[string]any{{"gapId": c.Param("id")}})
	id, err := insertEntry(c, tx, in.KnowledgeBaseID, in.Question, in.Aliases, in.Answer, "gap", refs, c.GetString("userId"), img)
	if err != nil {
		fail(c, 400, "create_failed", err.Error())
		return
	}
	_, err = tx.Exec(c, "UPDATE knowledge_gaps SET status='resolved' WHERE id=$1", c.Param("id"))
	if err != nil || tx.Commit(c) != nil {
		fail(c, 500, "database_error", "保存失败")
		return
	}
	ok(c, gin.H{"entryId": id})
}

func (a *App) ignoreGap(c *gin.Context) {
	_, _ = a.db.Exec(c, "UPDATE knowledge_gaps SET status='ignored' WHERE id=$1", c.Param("id"))
	ok(c, gin.H{"ignored": true})
}

// --- feedback ---

func (a *App) createFeedback(c *gin.Context) {
	var in struct {
		AnswerID        string `json:"answerId" form:"answerId"`
		Rating          string `json:"rating" form:"rating"`
		Comment         string `json:"comment" form:"comment"`
		CorrectedAnswer string `json:"correctedAnswer" form:"correctedAnswer"`
		Publish         bool   `json:"publish" form:"publish"`
		KnowledgeBaseID string `json:"knowledgeBaseId" form:"knowledgeBaseId"`
		Question        string `json:"question" form:"question"`
	}
	img, err := bindReplyImage(c, &in)
	if err != nil {
		fail(c, 400, "invalid_request", err.Error())
		return
	}
	if _, err := img.plan.resolve(nil); err != nil {
		fail(c, 400, "invalid_request", err.Error())
		return
	}
	if (img.data != nil || img.plan != nil && len(img.plan.order) > 0) && strings.TrimSpace(in.CorrectedAnswer) == "" {
		fail(c, 400, "invalid_request", "附图纠错需要填写纠正答案")
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
	tx, err := a.db.Begin(c)
	if err != nil {
		fail(c, 500, "database_error", "保存失败")
		return
	}
	defer tx.Rollback(c)
	if _, err := tx.Exec(c, "INSERT INTO message_feedback(message_id,source,rating,comment,corrected_answer,created_by) VALUES($1,'admin',$2,$3,$4,$5)", answerID, in.Rating, in.Comment, strings.TrimSpace(in.CorrectedAnswer), c.GetString("userId")); err != nil {
		fail(c, 500, "database_error", "保存失败")
		return
	}
	result := gin.H{"saved": true}
	if corrected := strings.TrimSpace(in.CorrectedAnswer); corrected != "" {
		q := firstNonEmpty(strings.TrimSpace(in.Question), question)
		evidence := []map[string]any{{"messageId": c.Param("id"), "answerId": answerID, "question": question}}
		candID, err := insertCandidate(c, tx, in.KnowledgeBaseID, q, nil, corrected, "feedback", evidence, 0.95, img)
		if err != nil {
			fail(c, 400, "candidate_failed", "无法创建候选知识，请检查知识库")
			return
		}
		if in.Publish {
			entryID, err := publishCandidate(c, tx, candID, in.KnowledgeBaseID, q, nil, corrected, c.GetString("userId"), "管理员纠错直接发布", nil)
			if err != nil {
				fail(c, 400, "publish_failed", err.Error())
				return
			}
			result["entryId"] = entryID
		}
		result["candidateId"] = candID
	}
	if tx.Commit(c) != nil {
		fail(c, 500, "database_error", "保存失败")
		return
	}
	if in.Rating == "good" || in.Rating == "wrong" || in.Rating == "bad" {
		a.applyEntryFeedback(c, answerID, in.Rating == "good")
	}
	ok(c, result)
}

func (a *App) listFeedback(c *gin.Context) {
	queryList(c, a, `SELECT f.id::text,f.knowledge_base_id::text AS "knowledgeBaseId",k.name AS "knowledgeBaseName",f.is_read AS "read",f.source,f.rating,f.comment,f.corrected_answer AS "correctedAnswer",f.created_at AS "createdAt",o.content AS answer,
		COALESCE(i.content,'') AS question,i.id::text AS "messageId",c.name AS "conversationName"
		FROM message_feedback f LEFT JOIN knowledge_bases k ON k.id=f.knowledge_base_id JOIN messages o ON o.id=f.message_id JOIN conversations c ON c.id=o.conversation_id
		LEFT JOIN messages i ON i.conversation_id=o.conversation_id AND i.platform_message_id=o.reply_to_message_id AND i.direction='inbound'
		WHERE ($1='' OR f.rating=$1) AND ($2='' OR coalesce(f.knowledge_base_id::text,'unassigned')=$2) ORDER BY f.created_at DESC LIMIT 300`, c.Query("rating"), c.Query("knowledgeBaseId"))
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
