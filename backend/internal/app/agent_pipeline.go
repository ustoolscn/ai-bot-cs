package app

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"

	"ai-bot/backend/internal/agent"
	"ai-bot/backend/internal/domain"
	"ai-bot/backend/internal/openai"
)

// agentStep is one recorded step of an agent run (shown in the admin UI).
type agentStep struct {
	Type      string `json:"type"` // rewrite | model | tool
	Name      string `json:"name,omitempty"`
	Input     string `json:"input,omitempty"`
	Output    string `json:"output,omitempty"`
	Error     string `json:"error,omitempty"`
	LatencyMs int64  `json:"latencyMs"`
}

func (a *App) processInbox(ctx context.Context) error {
	j, err := a.claimInbox(ctx)
	if err != nil {
		return err
	}
	if !j.Enabled || (j.Deadline != nil && time.Now().After(*j.Deadline)) {
		if err := a.finishInbox(ctx, j.TaskID, "skipped", nil); err != nil {
			return err
		}
		return a.scheduleProcessingAckRetraction(ctx, j.BotID, j.ConversationID, j.PlatformMessageID)
	}
	settings := a.agentSettings(ctx)

	var senderID, senderName string
	var eventAt time.Time
	var isStaff bool
	_ = a.db.QueryRow(ctx, `SELECT m.sender_id,m.sender_name,m.event_at,COALESCE(cm.is_staff,false) FROM messages m LEFT JOIN conversation_members cm ON cm.conversation_id=m.conversation_id AND cm.platform_user_id=m.sender_id WHERE m.id=$1`, j.MessageID).Scan(&senderID, &senderName, &eventAt, &isStaff)

	// A user handed off to a human is left to the human while the pause lasts.
	var paused bool
	_ = a.db.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM handoffs WHERE conversation_id=$1 AND platform_user_id=$2 AND status='open' AND paused_until>now())`, j.ConversationID, senderID).Scan(&paused)
	if paused && !isStaff {
		if err := a.finishInbox(ctx, j.TaskID, "handoff", nil); err != nil {
			return err
		}
		return a.scheduleProcessingAckRetraction(ctx, j.BotID, j.ConversationID, j.PlatformMessageID)
	}

	profileID := a.resolveChatProfile(ctx, j.ChatProfileID, j.BotID)
	if profileID == "" {
		return a.retryInbox(ctx, j, fmt.Errorf("no chat model configured"))
	}
	client, _, err := a.loadModel(ctx, profileID)
	if err != nil {
		return a.retryInbox(ctx, j, err)
	}
	var runID string
	if err = a.db.QueryRow(ctx, "INSERT INTO agent_runs(message_id) VALUES($1) RETURNING id::text", j.MessageID).Scan(&runID); err != nil {
		return a.retryInbox(ctx, j, err)
	}
	imageParts, err := a.prepareMessageImages(ctx, j.MessageID)
	if err != nil {
		_, _ = a.db.Exec(ctx, "UPDATE agent_runs SET status='failed',error=$1,completed_at=now() WHERE id=$2", err.Error(), runID)
		return a.retryInbox(ctx, j, err)
	}

	rt := &turnRuntime{settings: settings, botID: j.BotID, conversationID: j.ConversationID, conversationType: j.ConversationType,
		senderID: senderID, senderName: senderName, messageID: j.MessageID, runID: runID}
	rt.knowledgeBases = a.conversationKnowledgeBases(ctx, j.ConversationID)
	rt.snapshot, rt.snapshotStale, rt.snapshotOK = a.liveSnapshot(ctx, settings)

	// Working memory.
	contextStarted := time.Now()
	scope := threadScope(j.ConversationType, senderID, settings)
	turns, oldestKept, dropped := a.loadHistory(ctx, j.ConversationID, j.MessageID, eventAt, scope, settings, contextHistoryLimit(j.ContextLimit))
	var summary string
	if settings.SummaryEnabled {
		_ = a.db.QueryRow(ctx, "SELECT summary FROM conversation_summaries WHERE conversation_id=$1 AND scope_key=$2", j.ConversationID, scope).Scan(&summary)
		if dropped > 0 && !oldestKept.IsZero() {
			a.enqueueJob(ctx, "summarize", map[string]any{"conversationId": j.ConversationID, "scope": scope, "until": oldestKept, "botId": j.BotID, "profileId": profileID}, "summarize:"+j.ConversationID+":"+scope)
		}
	}
	var memories []agent.Memory
	if settings.MemoryEnabled && senderID != "" {
		memories = a.loadMemories(ctx, settings, j.BotID, senderID, j.Content)
	}
	contextLatency := time.Since(contextStarted).Milliseconds()

	// Understand: rewrite the question using recent context.
	var steps []agentStep
	query := strings.TrimSpace(j.Content)
	if settings.RewriteQuery && query != "" && len(turns) > 0 {
		if rewritten, step := a.rewriteQuery(ctx, settings, profileID, runID, turns, query); rewritten != "" {
			query = rewritten
			steps = append(steps, step)
		} else if step.Error != "" {
			steps = append(steps, step)
		}
	}

	retrievalStarted := time.Now()
	var kbContext string
	if query != "" && len(rt.knowledgeBases) > 0 {
		var hits []map[string]any
		kbContext, hits, err = a.retrieve(ctx, rt.knowledgeBases, query)
		if err != nil {
			a.log.Warn("knowledge retrieval failed", "message", j.MessageID, "error", err)
		}
		rt.hits = append(rt.hits, hits...)
	}
	retrievalLatency := time.Since(retrievalStarted).Milliseconds()

	userContent := strings.TrimSpace(j.Content)
	if userContent == "" && len(imageParts) > 0 {
		userContent = "请分析用户发送的图片并回答。"
	}
	in := agentInput{rt: rt, client: client, profileID: profileID, businessPrompt: j.SystemPrompt, senderIsStaff: isStaff,
		summary: summary, memories: memories, knowledge: kbContext, history: turns, userContent: userContent, imageParts: imageParts, deadline: j.Deadline}
	out := a.runAgent(ctx, in)
	steps = append(steps, out.steps...)
	contextJSON, _ := json.Marshal(out.messages)
	stepsJSON, _ := json.Marshal(steps)
	_, _ = a.db.Exec(ctx, "UPDATE agent_runs SET context_latency_ms=$1,retrieval_latency_ms=$2,context_messages=$3::jsonb,rewritten_query=$4,steps=$5::jsonb WHERE id=$6", contextLatency, retrievalLatency, string(contextJSON), rewrittenOrEmpty(query, j.Content), string(stepsJSON), runID)
	if out.err != nil {
		_, _ = a.db.Exec(ctx, "UPDATE agent_runs SET status='failed',error=$1,completed_at=now() WHERE id=$2", out.err.Error(), runID)
		if openai.IsHTTPStatus(out.err, 403) {
			return a.excludeModeratedMessage(ctx, j, out.err)
		}
		return a.retryInbox(ctx, j, out.err)
	}

	replyParts := a.shapeReply(settings, j.ConversationType, out.content)
	hitsJSON, _ := json.Marshal(rt.hits)
	tx, err := a.db.Begin(ctx)
	if err != nil {
		return a.retryInbox(ctx, j, err)
	}
	defer tx.Rollback(ctx)
	for i, part := range replyParts {
		if err != nil {
			break
		}
		_, mediaParts := extractOutboundImage(part)
		partsJSON, _ := json.Marshal(mediaParts)
		var outID string
		err = tx.QueryRow(ctx, `INSERT INTO messages(channel,bot_id,conversation_id,direction,event_type,content,parts,reply_to_message_id,status,event_at,reply_deadline) VALUES('qq',$1,$2,'outbound','AI_RESPONSE',$3,$4::jsonb,$5,'pending',now()+($6 * interval '1 millisecond'),$7) RETURNING id::text`, j.BotID, j.ConversationID, part, string(partsJSON), j.PlatformMessageID, i, j.Deadline).Scan(&outID)
		if err == nil {
			_, err = tx.Exec(ctx, "INSERT INTO outbox_tasks(message_id,msg_seq,next_attempt_at) VALUES($1,$2,now()+($3 * interval '1500 milliseconds'))", outID, 2+i, i)
		}
	}
	if err == nil {
		_, err = tx.Exec(ctx, "UPDATE agent_runs SET status='completed',retrieved_chunks=$1::jsonb,completed_at=now() WHERE id=$2", string(hitsJSON), runID)
	}
	if err == nil {
		_, err = tx.Exec(ctx, "UPDATE inbox_tasks SET status='completed',updated_at=now() WHERE id=$1", j.TaskID)
	}
	if err != nil {
		_ = tx.Rollback(ctx)
		return a.retryInbox(ctx, j, err)
	}
	if err = tx.Commit(ctx); err != nil {
		return err
	}
	a.recordEntryHits(ctx, rt.hits)
	if settings.EvaluationEnabled || settings.MemoryEnabled {
		a.enqueueJob(ctx, "evaluate", map[string]any{"runId": runID, "messageId": j.MessageID, "profileId": profileID}, "evaluate:"+runID)
	}
	return nil
}

func rewrittenOrEmpty(query, original string) string {
	if strings.TrimSpace(query) == strings.TrimSpace(original) {
		return ""
	}
	return query
}

// shapeReply applies style post-processing and splits long replies.
func (a *App) shapeReply(s agent.Settings, conversationType, content string) []string {
	text := strings.TrimSpace(content)
	if s.SanitizeMarkdown {
		text = agent.SanitizeReply(text)
	}
	if text == "" {
		text = strings.TrimSpace(content)
	}
	if !s.SplitLongReplies || markdownImagePattern.MatchString(text) || imageMarkerPattern.MatchString(text) {
		return []string{text}
	}
	parts := agent.SplitReply(text, s.ReplyChars(conversationType), s.MaxReplyParts)
	if len(parts) == 0 {
		return []string{text}
	}
	return parts
}

func threadScope(conversationType, senderID string, s agent.Settings) string {
	if conversationType == "group" && s.GroupThreadMode && senderID != "" {
		return senderID
	}
	return ""
}

type historyRow struct {
	id, direction, senderID, senderName, content, eventType, platformID, replyTo string
	parts                                                                        []domain.ContentPart
	staff                                                                        bool
	at                                                                           time.Time
}

// loadHistory returns working-memory turns (chronological) for the thread,
// the timestamp of the oldest kept turn, and how many older thread turns were
// dropped because of the token budget.
func (a *App) loadHistory(ctx context.Context, conversationID, currentMessageID string, before time.Time, scope string, s agent.Settings, maxTurns int) ([]agent.Turn, time.Time, int) {
	if maxTurns <= 0 {
		return nil, time.Time{}, 0
	}
	fetch := maxTurns * 4
	if fetch < 40 {
		fetch = 40
	}
	if fetch > 400 {
		fetch = 400
	}
	if before.IsZero() {
		before = time.Now().Add(time.Minute)
	}
	rows, err := a.db.Query(ctx, `SELECT m.id::text,m.direction,m.sender_id,m.sender_name,m.content,m.parts,m.event_type,COALESCE(m.platform_message_id,''),COALESCE(m.reply_to_message_id,''),COALESCE(cm.is_staff,false),m.event_at
		FROM messages m LEFT JOIN conversation_members cm ON cm.conversation_id=m.conversation_id AND cm.platform_user_id=m.sender_id
		WHERE m.conversation_id=$1 AND m.id<>$2 AND m.event_at<=$3 AND m.context_excluded=false AND m.event_type NOT IN ('PROCESSING_ACK')
		AND (m.content<>'' OR m.parts::text LIKE '%"image"%')
		ORDER BY m.event_at DESC LIMIT $4`, conversationID, currentMessageID, before, fetch)
	if err != nil {
		a.log.Warn("load history failed", "error", err)
		return nil, time.Time{}, 0
	}
	var hs []historyRow
	for rows.Next() {
		var h historyRow
		var partsRaw []byte
		if rows.Scan(&h.id, &h.direction, &h.senderID, &h.senderName, &h.content, &partsRaw, &h.eventType, &h.platformID, &h.replyTo, &h.staff, &h.at) == nil {
			_ = json.Unmarshal(partsRaw, &h.parts)
			hs = append(hs, h)
		}
	}
	rows.Close()
	// chronological
	for i, k := 0, len(hs)-1; i < k; i, k = i+1, k-1 {
		hs[i], hs[k] = hs[k], hs[i]
	}
	if scope != "" {
		mine := map[string]bool{}
		for _, h := range hs {
			if h.direction == "inbound" && h.senderID == scope && h.platformID != "" {
				mine[h.platformID] = true
			}
		}
		filtered := hs[:0]
		for _, h := range hs {
			switch {
			case h.direction == "inbound" && (h.senderID == scope || h.staff):
				filtered = append(filtered, h)
			case h.direction == "outbound" && mine[h.replyTo]:
				filtered = append(filtered, h)
			}
		}
		hs = filtered
	}
	turns := make([]agent.Turn, 0, len(hs))
	for _, h := range hs {
		content := strings.TrimSpace(h.content)
		if hasContentPart(h.parts, "image") {
			content = strings.TrimSpace(content + " [图片]")
		}
		t := agent.Turn{Role: "user", Name: h.senderName, Content: content, Staff: h.staff}
		if h.direction == "outbound" {
			t = agent.Turn{Role: "assistant", Content: content}
		}
		turns = append(turns, t)
	}
	kept, dropped := agent.FitHistory(turns, s.HistoryTokenBudget, maxTurns)
	var oldest time.Time
	if len(kept) > 0 {
		oldest = hs[len(hs)-len(kept)].at
	}
	return kept, oldest, dropped
}

func historyMessages(turns []agent.Turn, conversationType string) []domain.ChatMessage {
	out := make([]domain.ChatMessage, 0, len(turns))
	for _, t := range turns {
		if t.Role == "assistant" {
			out = append(out, domain.ChatMessage{Role: "assistant", Content: t.Content})
			continue
		}
		content := t.Content
		switch {
		case t.Staff:
			content = "【官方客服】" + firstNonEmpty(t.Name, "客服") + ": " + content
		case t.Name != "" && conversationType == "group":
			content = t.Name + ": " + content
		}
		out = append(out, domain.ChatMessage{Role: "user", Content: content})
	}
	return out
}

func (a *App) rewriteQuery(ctx context.Context, s agent.Settings, profileID, runID string, turns []agent.Turn, query string) (string, agentStep) {
	step := agentStep{Type: "rewrite", Name: "问题改写", Input: query}
	cl, auxID, err := a.auxModel(ctx, s, profileID)
	if err != nil {
		step.Error = err.Error()
		return "", step
	}
	recent := turns
	if len(recent) > 6 {
		recent = recent[len(recent)-6:]
	}
	var b strings.Builder
	for _, t := range recent {
		role := "用户"
		if t.Role == "assistant" {
			role = "客服"
		}
		fmt.Fprintf(&b, "%s：%s\n", role, agent.TruncateRunes(t.Content, 300))
	}
	fmt.Fprintf(&b, "用户最新一句：%s", query)
	rctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	started := time.Now()
	res, err := cl.ChatWith(rctx, []domain.ChatMessage{{Role: "system", Content: agent.RewritePrompt}, {Role: "user", Content: b.String()}}, domain.ChatOptions{DisableWebSearch: true})
	step.LatencyMs = time.Since(started).Milliseconds()
	a.recordModelCall(ctx, runID, auxID, "rewrite", res, step.LatencyMs, err)
	if err != nil {
		step.Error = err.Error()
		return "", step
	}
	out := strings.TrimSpace(strings.Trim(strings.TrimSpace(res.Content), "\"“”"))
	if out == "" || len([]rune(out)) > 300 {
		return "", step
	}
	step.Output = out
	return out, step
}

func (a *App) recordModelCall(ctx context.Context, runID, profileID, kind string, res domain.ChatResult, latency int64, err error) {
	var run any
	if runID != "" {
		run = runID
	}
	var profile any
	if profileID != "" {
		profile = profileID
	}
	_, _ = a.db.Exec(ctx, "INSERT INTO model_calls(agent_run_id,profile_id,kind,input_tokens,output_tokens,latency_ms,error) VALUES($1,$2,$3,$4,$5,$6,$7)", run, profile, kind, res.InputTokens, res.OutputTokens, latency, errorText(err))
}

type agentInput struct {
	rt             *turnRuntime
	client         *openai.Client
	profileID      string
	businessPrompt string
	senderIsStaff  bool
	summary        string
	memories       []agent.Memory
	knowledge      string
	history        []agent.Turn
	userContent    string
	imageParts     []domain.ChatContentPart
	deadline       *time.Time
}

type agentOutput struct {
	content  string
	messages []domain.ChatMessage
	steps    []agentStep
	err      error
}

// runAgent builds the prompt and runs the model/tool loop.
func (a *App) runAgent(ctx context.Context, in agentInput) agentOutput {
	rt := in.rt
	s := rt.settings
	var tools []domain.ToolSpec
	custom := map[string]customTool{}
	if s.ToolsEnabled {
		tools, custom = a.availableTools(ctx, rt)
	}
	var skills []agent.SkillPrompt
	if s.ToolsEnabled {
		skills = a.enabledSkills(ctx)
	}
	live := ""
	if rt.snapshotOK && s.LiveData.InjectSnapshot {
		live = rt.snapshot.PromptSummary(rt.snapshotStale)
	}
	systems := agent.BuildSystemPrompt(agent.PromptInput{Settings: s, ConversationType: rt.conversationType, BusinessPrompt: in.businessPrompt,
		SenderName: rt.senderName, SenderIsStaff: in.senderIsStaff, Now: time.Now().In(chinaLocation), Summary: in.summary, Memories: in.memories,
		Knowledge: in.knowledge, LiveSnapshot: live, Skills: skills, ToolsAvailable: len(tools) > 0})
	messages := make([]domain.ChatMessage, 0, len(systems)+len(in.history)+1)
	for _, sp := range systems {
		messages = append(messages, domain.ChatMessage{Role: "system", Content: sp})
	}
	messages = append(messages, historyMessages(in.history, rt.conversationType)...)
	messages = append(messages, domain.ChatMessage{Role: "user", Content: in.userContent, Parts: in.imageParts})

	out := agentOutput{}
	opts := domain.ChatOptions{Tools: tools, MaxOutputTokens: s.MaxOutputTokens}
	for step := 0; ; step++ {
		final := len(opts.Tools) == 0 || step >= s.MaxToolSteps
		if in.deadline != nil && time.Until(*in.deadline) < 45*time.Second {
			final = true
		}
		callOpts := opts
		if final {
			callOpts.Tools = nil
		}
		started := time.Now()
		res, err := in.client.ChatWith(ctx, messages, callOpts)
		latency := time.Since(started).Milliseconds()
		a.recordModelCall(ctx, rt.runID, in.profileID, "chat", res, latency, err)
		ms := agentStep{Type: "model", Name: fmt.Sprintf("第 %d 次模型调用", step+1), Output: agent.TruncateRunes(res.Content, 500), LatencyMs: latency}
		if err != nil {
			ms.Error = err.Error()
			out.steps = append(out.steps, ms)
			out.err = err
			out.messages = messages
			return out
		}
		out.steps = append(out.steps, ms)
		if final || len(res.ToolCalls) == 0 {
			out.content = res.Content
			if strings.TrimSpace(out.content) == "" && rt.handoff {
				out.content = "已经帮你转给人工客服了，请稍等一下～"
			}
			if strings.TrimSpace(out.content) == "" {
				out.err = fmt.Errorf("模型返回空回复")
			}
			out.messages = messages
			return out
		}
		for i := range res.ToolCalls {
			if res.ToolCalls[i].ID == "" {
				res.ToolCalls[i].ID = fmt.Sprintf("call_%d_%d", step, i)
			}
		}
		messages = append(messages, domain.ChatMessage{Role: "assistant", Content: res.Content, ToolCalls: res.ToolCalls})
		for _, call := range res.ToolCalls {
			started := time.Now()
			result, terr := a.executeTool(ctx, rt, call, custom)
			lat := time.Since(started).Milliseconds()
			ts := agentStep{Type: "tool", Name: call.Name, Input: call.Arguments, Output: agent.TruncateRunes(result, 800), LatencyMs: lat}
			if terr != nil {
				ts.Error = terr.Error()
				result = jsonString(map[string]any{"error": terr.Error()})
			}
			out.steps = append(out.steps, ts)
			if !rt.dryRun && rt.runID != "" {
				var resultJSON any
				if json.Valid([]byte(result)) {
					resultJSON = result
				} else {
					resultJSON = jsonString(map[string]any{"text": result})
				}
				args := call.Arguments
				if !json.Valid([]byte(args)) {
					args = "{}"
				}
				_, _ = a.db.Exec(ctx, "INSERT INTO tool_calls(agent_run_id,tool_name,arguments,result,error) VALUES($1,$2,$3::jsonb,$4::jsonb,$5)", rt.runID, call.Name, args, resultJSON, errorText(terr))
			}
			messages = append(messages, domain.ChatMessage{Role: "tool", ToolCallID: call.ID, Content: agent.TruncateRunes(result, 6000)})
		}
	}
}

var chinaLocation = time.FixedZone("CST", 8*3600)

func (a *App) conversationKnowledgeBases(ctx context.Context, conversationID string) []string {
	rows, err := a.db.Query(ctx, `SELECT knowledge_base_id::text FROM conversation_knowledge_bases WHERE conversation_id=$1`, conversationID)
	if err != nil {
		return nil
	}
	defer rows.Close()
	var ids []string
	for rows.Next() {
		var id string
		if rows.Scan(&id) == nil {
			ids = append(ids, id)
		}
	}
	return ids
}

// retrieve searches document chunks and FAQ entries of the given knowledge bases.
func (a *App) retrieve(ctx context.Context, kbIDs []string, query string) (string, []map[string]any, error) {
	var all []map[string]any
	var lastErr error
	for _, kbID := range kbIDs {
		var pid *string
		if err := a.db.QueryRow(ctx, "SELECT embedding_profile_id::text FROM knowledge_bases WHERE id=$1", kbID).Scan(&pid); err != nil || pid == nil {
			continue
		}
		cl, _, e := a.loadModel(ctx, *pid)
		if e != nil {
			lastErr = e
			continue
		}
		v, e := cl.Embed(ctx, []string{query})
		if e != nil {
			lastErr = e
			continue
		}
		rs, e := a.db.Query(ctx, `SELECT c.id::text,c.document_id::text,c.entry_id::text AS entry_id,c.content,1-(c.embedding <=> $1::vector) AS score
			FROM knowledge_chunks c LEFT JOIN knowledge_entries e ON e.id=c.entry_id
			WHERE c.knowledge_base_id=$2 AND (c.entry_id IS NULL OR e.status='published')
			ORDER BY c.embedding <=> $1::vector LIMIT 8`, vectorLiteral(v[0]), kbID)
		if e != nil {
			lastErr = e
			continue
		}
		d, _ := rowsJSON(rs)
		all = append(all, d...)
	}
	sort.SliceStable(all, func(i, j int) bool { return hitScore(all[i]) > hitScore(all[j]) })
	if len(all) > 5 {
		all = all[:5]
	}
	var b strings.Builder
	for i, h := range all {
		fmt.Fprintf(&b, "[%d] %v\n", i+1, h["content"])
	}
	if len(all) == 0 && lastErr != nil {
		return "", nil, lastErr
	}
	return b.String(), all, nil
}

func (a *App) recordEntryHits(ctx context.Context, hits []map[string]any) {
	seen := map[string]bool{}
	for _, h := range hits {
		id, _ := h["entry_id"].(string)
		if id == "" || seen[id] || hitScore(h) < 0.5 {
			continue
		}
		seen[id] = true
		_, _ = a.db.Exec(ctx, "UPDATE knowledge_entries SET hit_count=hit_count+1,last_hit_at=now() WHERE id=$1", id)
	}
}

func (a *App) enabledSkills(ctx context.Context) []agent.SkillPrompt {
	rows, err := a.db.Query(ctx, "SELECT name,description,instructions FROM skills WHERE enabled ORDER BY sort_order,name")
	if err != nil {
		return nil
	}
	defer rows.Close()
	var out []agent.SkillPrompt
	for rows.Next() {
		var s agent.SkillPrompt
		if rows.Scan(&s.Name, &s.Description, &s.Instructions) == nil {
			out = append(out, s)
		}
	}
	return out
}
