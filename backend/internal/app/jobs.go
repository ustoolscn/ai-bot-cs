package app

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"

	"ai-bot/backend/internal/agent"
	"ai-bot/backend/internal/domain"
)

func itoa(n int) string { return strconv.Itoa(n) }

// enqueueJob adds a background job; a pending job with the same dedupe key is reused.
func (a *App) enqueueJob(ctx context.Context, kind string, payload map[string]any, dedupe string) {
	b, _ := json.Marshal(payload)
	var key any
	if dedupe != "" {
		key = dedupe
	}
	if _, err := a.db.Exec(ctx, `INSERT INTO agent_jobs(kind,payload,dedupe_key) VALUES($1,$2::jsonb,$3) ON CONFLICT (dedupe_key) WHERE dedupe_key IS NOT NULL AND status IN ('pending','processing') DO UPDATE SET payload=EXCLUDED.payload,updated_at=now()`, kind, string(b), key); err != nil {
		a.log.Warn("enqueue job failed", "kind", kind, "error", err)
	}
}

type agentJob struct {
	ID, Kind string
	Payload  map[string]any
	Attempts int
}

func (a *App) claimAgentJob(ctx context.Context, kinds []string, exclude bool) (agentJob, error) {
	tx, err := a.db.Begin(ctx)
	if err != nil {
		return agentJob{}, err
	}
	defer tx.Rollback(ctx)
	cond := "kind = ANY($1)"
	if exclude {
		cond = "NOT (kind = ANY($1))"
	}
	var j agentJob
	var raw []byte
	err = tx.QueryRow(ctx, `SELECT id::text,kind,payload,attempts FROM agent_jobs WHERE status='pending' AND next_attempt_at<=now() AND `+cond+` ORDER BY created_at FOR UPDATE SKIP LOCKED LIMIT 1`, kinds).Scan(&j.ID, &j.Kind, &raw, &j.Attempts)
	if err != nil {
		return j, err
	}
	_ = json.Unmarshal(raw, &j.Payload)
	if _, err = tx.Exec(ctx, "UPDATE agent_jobs SET status='processing',attempts=attempts+1,locked_at=now(),updated_at=now() WHERE id=$1", j.ID); err != nil {
		return j, err
	}
	return j, tx.Commit(ctx)
}

func (a *App) processAgentJob(ctx context.Context) error {
	j, err := a.claimAgentJob(ctx, []string{"eval_run"}, true)
	if err != nil {
		return err
	}
	return a.runJob(ctx, j)
}

func (a *App) processEvalJob(ctx context.Context) error {
	j, err := a.claimAgentJob(ctx, []string{"eval_run"}, false)
	if err != nil {
		return err
	}
	return a.runJob(ctx, j)
}

func (a *App) runJob(ctx context.Context, j agentJob) error {
	var err error
	str := func(k string) string { v, _ := j.Payload[k].(string); return v }
	switch j.Kind {
	case "blacklist_kick":
		err = a.jobBlacklistKick(ctx, str("actionId"))
	case "evaluate":
		err = a.jobEvaluate(ctx, str("runId"), str("messageId"), str("profileId"))
	case "summarize":
		err = a.jobSummarize(ctx, str("conversationId"), str("scope"), str("until"), str("profileId"))
	case "staff_history":
		err = a.jobStaffHistory(ctx, str("conversationId"))
	case "staff_history_answer":
		err = a.mineStaffAnswer(ctx, str("messageId"), true)
	case "staff_answer":
		err = a.jobStaffAnswer(ctx, str("messageId"))
	case "index_entry":
		err = a.jobIndexEntry(ctx, str("entryId"))
	case "eval_run":
		err = a.jobEvalRun(ctx, str("runId"))
	default:
		err = fmt.Errorf("unknown job kind %s", j.Kind)
	}
	if err == nil {
		_, e := a.db.Exec(ctx, "UPDATE agent_jobs SET status='completed',last_error=NULL,updated_at=now() WHERE id=$1", j.ID)
		return e
	}
	status := "pending"
	if j.Attempts+1 >= 3 {
		status = "failed"
	}
	a.log.Warn("agent job failed", "kind", j.Kind, "attempt", j.Attempts+1, "error", err)
	_, e := a.db.Exec(ctx, "UPDATE agent_jobs SET status=$1,last_error=$2,next_attempt_at=now()+interval '30 seconds',updated_at=now() WHERE id=$3", status, err.Error(), j.ID)
	return e
}

// auxJSON runs the auxiliary model with a system prompt and decodes JSON output.
func (a *App) auxJSON(ctx context.Context, s agent.Settings, fallbackProfile, runID, kind, system, user string, out any) error {
	cl, pid, err := a.auxModel(ctx, s, fallbackProfile)
	if err != nil {
		return fmt.Errorf("辅助模型不可用: %w", err)
	}
	cctx, cancel := context.WithTimeout(ctx, 60*time.Second)
	defer cancel()
	started := time.Now()
	res, err := cl.ChatWith(cctx, []domain.ChatMessage{{Role: "system", Content: system}, {Role: "user", Content: user}}, domain.ChatOptions{DisableWebSearch: true})
	a.recordModelCall(ctx, runID, pid, kind, res, time.Since(started).Milliseconds(), err)
	if err != nil {
		return err
	}
	raw := agent.ExtractJSON(res.Content)
	if raw == "" {
		return fmt.Errorf("辅助模型未返回 JSON")
	}
	return json.Unmarshal([]byte(raw), out)
}

func (a *App) auxText(ctx context.Context, s agent.Settings, fallbackProfile, kind, system, user string) (string, error) {
	cl, pid, err := a.auxModel(ctx, s, fallbackProfile)
	if err != nil {
		return "", err
	}
	cctx, cancel := context.WithTimeout(ctx, 60*time.Second)
	defer cancel()
	started := time.Now()
	res, err := cl.ChatWith(cctx, []domain.ChatMessage{{Role: "system", Content: system}, {Role: "user", Content: user}}, domain.ChatOptions{DisableWebSearch: true})
	a.recordModelCall(ctx, "", pid, kind, res, time.Since(started).Milliseconds(), err)
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(res.Content), nil
}

type evaluation struct {
	Answered         bool   `json:"answered"`
	KnowledgeGap     string `json:"knowledge_gap"`
	PreviousFeedback string `json:"previous_feedback"`
	TooLong          bool   `json:"too_long"`
	Memories         []struct {
		Kind       string  `json:"kind"`
		Content    string  `json:"content"`
		Confidence float64 `json:"confidence"`
	} `json:"memories"`
}

// jobEvaluate analyses a finished turn: quality, knowledge gaps, implicit
// feedback on the previous answer and long-term user memories.
func (a *App) jobEvaluate(ctx context.Context, runID, messageID, profileID string) error {
	if excluded, err := a.messageExcluded(ctx, messageID); err != nil {
		return err
	} else if excluded {
		return nil
	}
	s := a.agentSettings(ctx)
	var botID, convID, convType, senderID, senderName, question, platformID string
	var eventAt time.Time
	if err := a.db.QueryRow(ctx, `SELECT m.bot_id::text,m.conversation_id::text,c.type,m.sender_id,m.sender_name,m.content,COALESCE(m.platform_message_id,''),m.event_at FROM messages m JOIN conversations c ON c.id=m.conversation_id WHERE m.id=$1`, messageID).
		Scan(&botID, &convID, &convType, &senderID, &senderName, &question, &platformID, &eventAt); err != nil {
		return nil // message deleted
	}
	var answer string
	_ = a.db.QueryRow(ctx, `SELECT COALESCE(string_agg(content, E'\n' ORDER BY event_at),'') FROM messages WHERE conversation_id=$1 AND direction='outbound' AND event_type='AI_RESPONSE' AND reply_to_message_id=$2`, convID, platformID).Scan(&answer)
	if strings.TrimSpace(answer) == "" {
		return nil
	}
	// Previous bot answer to the same user within 30 minutes.
	var prevAnswerID, prevAnswer, prevQuestion string
	_ = a.db.QueryRow(ctx, `SELECT o.id::text,o.content,i.content FROM messages i JOIN messages o ON o.conversation_id=i.conversation_id AND o.reply_to_message_id=i.platform_message_id AND o.direction='outbound' AND o.event_type='AI_RESPONSE'
		WHERE i.conversation_id=$1 AND i.direction='inbound' AND i.sender_id=$2 AND i.event_at<$3 AND i.event_at>$3-interval '30 minutes' AND i.id<>$4
		ORDER BY i.event_at DESC, o.event_at DESC LIMIT 1`, convID, senderID, eventAt, messageID).Scan(&prevAnswerID, &prevAnswer, &prevQuestion)

	var b strings.Builder
	if prevAnswer != "" {
		fmt.Fprintf(&b, "（上一轮）用户：%s\n（上一轮）客服：%s\n\n", agent.TruncateRunes(prevQuestion, 300), agent.TruncateRunes(prevAnswer, 500))
	}
	fmt.Fprintf(&b, "（本轮）用户：%s\n（本轮）客服：%s", agent.TruncateRunes(question, 800), agent.TruncateRunes(answer, 1200))
	var ev evaluation
	if err := a.auxJSON(ctx, s, profileID, runID, "evaluate", agent.EvaluationPrompt, b.String(), &ev); err != nil {
		return err
	}
	evJSON, _ := json.Marshal(ev)
	_, _ = a.db.Exec(ctx, "UPDATE agent_runs SET evaluation=$1::jsonb WHERE id=$2", string(evJSON), runID)

	if s.MemoryEnabled && senderID != "" {
		for i, m := range ev.Memories {
			if i >= 3 || m.Confidence < 0.6 {
				continue
			}
			if err := a.saveMemory(ctx, s, botID, senderID, senderName, m.Kind, m.Content, m.Confidence, "extracted", messageID); err != nil {
				a.log.Warn("save memory failed", "error", err)
			}
		}
	}
	if !s.EvaluationEnabled {
		return nil
	}
	if prevAnswerID != "" && (ev.PreviousFeedback == "helpful" || ev.PreviousFeedback == "unhelpful") {
		var exists bool
		_ = a.db.QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM message_feedback WHERE message_id=$1 AND source='user_implicit')", prevAnswerID).Scan(&exists)
		if !exists {
			_, _ = a.db.Exec(ctx, "INSERT INTO message_feedback(message_id,source,rating,comment) VALUES($1,'user_implicit',$2,$3)", prevAnswerID, ev.PreviousFeedback, "根据用户后续发言自动判断")
			a.applyEntryFeedback(ctx, prevAnswerID, ev.PreviousFeedback == "helpful")
		}
	}
	if ev.TooLong {
		_, _ = a.db.Exec(ctx, "INSERT INTO message_feedback(message_id,source,rating,comment) SELECT id,'evaluator','too_long','质检判断回答偏长' FROM messages WHERE conversation_id=$1 AND reply_to_message_id=$2 AND event_type='AI_RESPONSE' ORDER BY event_at LIMIT 1", convID, platformID)
	}
	if gap := strings.TrimSpace(ev.KnowledgeGap); gap != "" && !ev.Answered {
		a.recordGap(ctx, s, gap, messageID)
	}
	return nil
}

// applyEntryFeedback updates helpful/unhelpful counters of FAQ entries used
// to produce the given answer message.
func (a *App) applyEntryFeedback(ctx context.Context, answerMessageID string, helpful bool) {
	col := "unhelpful_count"
	if helpful {
		col = "helpful_count"
	}
	_, _ = a.db.Exec(ctx, `UPDATE knowledge_entries SET `+col+`=`+col+`+1 WHERE id IN (
		SELECT (h->>'entry_id')::uuid FROM messages o
		JOIN messages i ON i.conversation_id=o.conversation_id AND i.platform_message_id=o.reply_to_message_id AND i.direction='inbound'
		JOIN LATERAL (SELECT retrieved_chunks FROM agent_runs WHERE message_id=i.id ORDER BY started_at DESC LIMIT 1) r ON true
		CROSS JOIN LATERAL jsonb_array_elements(r.retrieved_chunks) h
		WHERE o.id=$1 AND COALESCE(h->>'entry_id','')<>'' AND (h->>'score')::float>=0.5)`, answerMessageID)
	if !helpful {
		_, _ = a.db.Exec(ctx, `UPDATE knowledge_entries SET status='review' WHERE status='published' AND source<>'announcement' AND unhelpful_count>=3 AND unhelpful_count > helpful_count*2`)
	}
}

// recordGap clusters an unanswered question into knowledge_gaps.
func (a *App) recordGap(ctx context.Context, s agent.Settings, question, messageID string) {
	question = agent.TruncateRunes(strings.TrimSpace(question), 200)
	cl, pid, ok := a.embeddingModel(ctx, s)
	if ok {
		if v, err := cl.Embed(ctx, []string{question}); err == nil && len(v) == 1 {
			lit := vectorLiteral(v[0])
			var id string
			err := a.db.QueryRow(ctx, `SELECT id::text FROM knowledge_gaps WHERE status IN ('open','candidate') AND embedding IS NOT NULL AND embedding_profile_id=$1 AND vector_dims(embedding)=$4 AND 1-(embedding <=> $2::vector) >= $3 ORDER BY embedding <=> $2::vector LIMIT 1`, pid, lit, s.GapSimilarity, len(v[0])).Scan(&id)
			if err == nil {
				_, _ = a.db.Exec(ctx, `UPDATE knowledge_gaps SET occurrences=occurrences+1,last_seen_at=now(),sample_message_ids=CASE WHEN jsonb_array_length(sample_message_ids)<10 THEN sample_message_ids || to_jsonb($2::text) ELSE sample_message_ids END WHERE id=$1`, id, messageID)
				return
			}
			_, _ = a.db.Exec(ctx, `INSERT INTO knowledge_gaps(question,embedding,embedding_profile_id,sample_message_ids) VALUES($1,$2::vector,$3,jsonb_build_array($4::text))`, question, lit, pid, messageID)
			return
		}
	}
	tag, _ := a.db.Exec(ctx, `UPDATE knowledge_gaps SET occurrences=occurrences+1,last_seen_at=now() WHERE status IN ('open','candidate') AND question=$1`, question)
	if tag.RowsAffected() == 0 {
		_, _ = a.db.Exec(ctx, `INSERT INTO knowledge_gaps(question,sample_message_ids) VALUES($1,jsonb_build_array($2::text))`, question, messageID)
	}
}

// jobSummarize folds thread messages older than `until` into the rolling summary.
func (a *App) jobSummarize(ctx context.Context, conversationID, scope, until, profileID string) error {
	untilAt, err := time.Parse(time.RFC3339Nano, until)
	if err != nil {
		return nil
	}
	s := a.agentSettings(ctx)
	var summary string
	coveredUntil := time.Unix(0, 0).UTC()
	_ = a.db.QueryRow(ctx, "SELECT summary,covered_until FROM conversation_summaries WHERE conversation_id=$1 AND scope_key=$2", conversationID, scope).Scan(&summary, &coveredUntil)
	if !untilAt.After(coveredUntil) {
		return nil
	}
	rows, err := a.db.Query(ctx, `SELECT m.direction,m.sender_id,m.sender_name,m.content,COALESCE(m.reply_to_message_id,''),COALESCE(m.platform_message_id,'') FROM messages m
		WHERE m.conversation_id=$1 AND m.event_at>$2 AND m.event_at<$3 AND m.content<>'' AND m.context_excluded=false AND m.event_type<>'PROCESSING_ACK' ORDER BY m.event_at LIMIT 300`, conversationID, coveredUntil, untilAt)
	if err != nil {
		return err
	}
	var lines []string
	mine := map[string]bool{}
	for rows.Next() {
		var dir, sid, name, content, replyTo, pid string
		if rows.Scan(&dir, &sid, &name, &content, &replyTo, &pid) != nil {
			continue
		}
		if scope != "" {
			if dir == "inbound" && sid != scope {
				continue
			}
			if dir == "inbound" {
				mine[pid] = true
			}
			if dir == "outbound" && !mine[replyTo] {
				continue
			}
		}
		role := "用户"
		if dir == "outbound" {
			role = "客服"
		} else if name != "" {
			role = "用户(" + name + ")"
		}
		lines = append(lines, role+"："+agent.TruncateRunes(content, 300))
	}
	rows.Close()
	if len(lines) == 0 {
		_, err = a.db.Exec(ctx, `INSERT INTO conversation_summaries(conversation_id,scope_key,summary,covered_until) VALUES($1,$2,$3,$4) ON CONFLICT(conversation_id,scope_key) DO UPDATE SET covered_until=EXCLUDED.covered_until,updated_at=now()`, conversationID, scope, summary, untilAt)
		return err
	}
	input := "已有摘要：\n" + firstNonEmpty(summary, "（无）") + "\n\n新增对话：\n" + strings.Join(lines, "\n")
	out, err := a.auxText(ctx, s, profileID, "summarize", agent.SummaryPrompt, input)
	if err != nil {
		return err
	}
	_, err = a.db.Exec(ctx, `INSERT INTO conversation_summaries(conversation_id,scope_key,summary,covered_until) VALUES($1,$2,$3,$4) ON CONFLICT(conversation_id,scope_key) DO UPDATE SET summary=EXCLUDED.summary,covered_until=EXCLUDED.covered_until,updated_at=now()`, conversationID, scope, agent.TruncateRunes(out, 600), untilAt)
	return err
}

type staffMining struct {
	Reusable   bool     `json:"reusable"`
	Question   string   `json:"question"`
	Aliases    []string `json:"aliases"`
	Answer     string   `json:"answer"`
	Confidence float64  `json:"confidence"`
}

// jobStaffAnswer turns an official staff reply into an FAQ candidate.
func (a *App) jobStaffAnswer(ctx context.Context, messageID string) error {
	return a.mineStaffAnswer(ctx, messageID, false)
}

func (a *App) mineStaffAnswer(ctx context.Context, messageID string, forced bool) error {
	if excluded, err := a.messageExcluded(ctx, messageID); err != nil {
		return err
	} else if excluded {
		return nil
	}
	s := a.agentSettings(ctx)
	if !s.StaffMiningEnabled && !forced {
		return nil
	}
	var convID, botID, content, source string
	var eventAt time.Time
	if err := a.db.QueryRow(ctx, "SELECT conversation_id::text,bot_id::text,content,event_type,event_at FROM messages WHERE id=$1", messageID).Scan(&convID, &botID, &content, &source, &eventAt); err != nil {
		return nil
	}
	var mined bool
	if err := a.db.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM knowledge_candidates WHERE source='staff' AND evidence @> jsonb_build_array(jsonb_build_object('messageId',$1::text)))`, messageID).Scan(&mined); err != nil {
		return err
	}
	if mined {
		return nil
	}
	if source != "HUMAN_REPLY" {
		var staff bool
		if err := a.db.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM messages m JOIN conversation_members cm ON cm.conversation_id=m.conversation_id AND cm.platform_user_id=m.sender_id WHERE m.id=$1 AND cm.is_staff AND NOT m.context_excluded)`, messageID).Scan(&staff); err != nil {
			return err
		}
		if !staff {
			return nil
		}
	}
	if len([]rune(strings.TrimSpace(content))) < 6 {
		return nil
	}
	rows, err := a.db.Query(ctx, `SELECT direction,sender_name,content,event_type FROM messages WHERE conversation_id=$1 AND event_at<$2 AND event_at>$2-interval '2 hours' AND content<>'' AND NOT context_excluded AND event_type<>'PROCESSING_ACK' ORDER BY event_at DESC LIMIT 15`, convID, eventAt)
	if err != nil {
		return err
	}
	var lines []string
	for rows.Next() {
		var dir, name, text, et string
		if rows.Scan(&dir, &name, &text, &et) != nil {
			continue
		}
		who := firstNonEmpty(name, "用户")
		if dir == "outbound" {
			who = "机器人"
		}
		lines = append([]string{who + "：" + agent.TruncateRunes(text, 300)}, lines...)
	}
	rows.Close()
	if len(lines) == 0 {
		return nil
	}
	label := "官方客服"
	if source == "HUMAN_REPLY" {
		label = "人工客服（管理后台）"
	}
	input := strings.Join(lines, "\n") + "\n" + label + "：" + agent.TruncateRunes(content, 1000)
	var res staffMining
	if err := a.auxJSON(ctx, s, a.resolveChatProfile(ctx, nil, botID), "", "staff_mining", agent.StaffMiningPrompt, input, &res); err != nil {
		return err
	}
	if !res.Reusable || strings.TrimSpace(res.Question) == "" || strings.TrimSpace(res.Answer) == "" {
		return nil
	}
	evidence := []map[string]any{{"messageId": messageID, "conversationId": convID, "content": agent.TruncateRunes(content, 500), "context": agent.TruncateRunes(strings.Join(lines, "\n"), 1500)}}
	candID, fresh, err := a.createStaffCandidate(ctx, messageID, s.LearningKnowledgeBaseID, res, evidence)
	if err != nil {
		return err
	}
	if !fresh {
		return nil
	}
	if s.AutoPublishStaff && res.Confidence >= s.AutoPublishMinConfidence && s.LearningKnowledgeBaseID != "" {
		_, err = a.approveCandidate(ctx, candID, s.LearningKnowledgeBaseID, res.Question, res.Aliases, res.Answer, "", "自动发布：官方客服高置信度回答")
	}
	return err
}

// jobIndexEntry (re)builds the vector chunk of an FAQ entry.
func (a *App) jobIndexEntry(ctx context.Context, entryID string) error {
	var kbID, question, answer, status string
	var aliasesRaw []byte
	var pid *string
	if err := a.db.QueryRow(ctx, `SELECT e.knowledge_base_id::text,e.question,e.answer,e.aliases,e.status,k.embedding_profile_id::text FROM knowledge_entries e JOIN knowledge_bases k ON k.id=e.knowledge_base_id WHERE e.id=$1`, entryID).
		Scan(&kbID, &question, &answer, &aliasesRaw, &status, &pid); err != nil {
		return nil
	}
	if status != "published" {
		_, err := a.db.Exec(ctx, "DELETE FROM knowledge_chunks WHERE entry_id=$1", entryID)
		return err
	}
	setErr := func(e error) error {
		_, _ = a.db.Exec(ctx, "UPDATE knowledge_entries SET index_status='failed',index_error=$1 WHERE id=$2", e.Error(), entryID)
		return e
	}
	if pid == nil {
		return setErr(fmt.Errorf("知识库未绑定 Embedding 模型"))
	}
	var aliases []string
	_ = json.Unmarshal(aliasesRaw, &aliases)
	text := "问：" + question
	if len(aliases) > 0 {
		text += "（相似问法：" + strings.Join(aliases, "；") + "）"
	}
	text += "\n答：" + answer
	cl, kind, err := a.loadModel(ctx, *pid)
	if err != nil || kind != "embedding" {
		return setErr(fmt.Errorf("Embedding 模型不可用"))
	}
	v, err := cl.Embed(ctx, []string{text})
	if err != nil {
		return setErr(err)
	}
	tx, err := a.db.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	if _, err = tx.Exec(ctx, "DELETE FROM knowledge_chunks WHERE entry_id=$1", entryID); err == nil {
		_, err = tx.Exec(ctx, "INSERT INTO knowledge_chunks(knowledge_base_id,entry_id,chunk_index,content,embedding,metadata) VALUES($1,$2,0,$3,$4::vector,$5::jsonb)", kbID, entryID, text, vectorLiteral(v[0]), jsonString(map[string]any{"type": "faq"}))
	}
	if err == nil {
		_, err = tx.Exec(ctx, "UPDATE knowledge_entries SET index_status='ready',index_error='' WHERE id=$1", entryID)
	}
	if err != nil {
		return setErr(err)
	}
	return tx.Commit(ctx)
}
