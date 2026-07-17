package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	"ai-bot/backend/internal/domain"
	"ai-bot/backend/internal/knowledge"
	"ai-bot/backend/internal/openai"
	"ai-bot/backend/internal/qq"
	"github.com/jackc/pgx/v5"
)

func (a *App) RunWorkers(ctx context.Context) {
	var wg sync.WaitGroup
	for name, fn := range map[string]func(context.Context) error{"inbox": a.processInbox, "documents": a.processDocument, "outbox": a.processOutbox} {
		name, fn := name, fn
		wg.Add(1)
		go func() { defer wg.Done(); a.workerLoop(ctx, name, fn) }()
	}
	wg.Add(1)
	go func() { defer wg.Done(); a.maintenanceLoop(ctx) }()
	<-ctx.Done()
	wg.Wait()
}
func (a *App) maintenanceLoop(ctx context.Context) {
	run := func() {
		if err := a.processMaintenance(ctx); err != nil && !errors.Is(err, context.Canceled) {
			a.log.Error("maintenance error", "error", err)
		}
	}
	run()
	ticker := time.NewTicker(time.Minute)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			run()
		}
	}
}
func (a *App) processMaintenance(ctx context.Context) error {
	settings, err := a.runtimeSettings(ctx)
	if err != nil {
		return fmt.Errorf("读取消息保留设置失败: %w", err)
	}
	cutoff := time.Now().UTC().AddDate(0, 0, -settings.MessageRetentionDays)
	tx, err := a.db.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	if _, err = tx.Exec(ctx, "DELETE FROM messages WHERE created_at<$1", cutoff); err != nil {
		return fmt.Errorf("清理过期消息失败: %w", err)
	}
	if _, err = tx.Exec(ctx, "DELETE FROM webhook_events WHERE created_at<$1", cutoff); err != nil {
		return fmt.Errorf("清理过期 Webhook 事件失败: %w", err)
	}
	if _, err = tx.Exec(ctx, "DELETE FROM admin_sessions WHERE expires_at<now()"); err != nil {
		return fmt.Errorf("清理过期管理会话失败: %w", err)
	}
	if err = tx.Commit(ctx); err != nil {
		return err
	}
	if err = a.images.CleanupOlderThan(cutoff); err != nil {
		return fmt.Errorf("清理过期图片缓存失败: %w", err)
	}
	return nil
}
func (a *App) workerLoop(ctx context.Context, name string, fn func(context.Context) error) {
	ticker := time.NewTicker(a.cfg.WorkerPoll)
	defer ticker.Stop()
	for {
		if err := fn(ctx); err != nil && !errors.Is(err, pgx.ErrNoRows) && !errors.Is(err, context.Canceled) {
			a.log.Error("worker error", "worker", name, "error", err)
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

type inboxJob struct {
	TaskID, MessageID, BotID, ConversationID, ConversationType, PlatformConversationID, PlatformMessageID, Content, SystemPrompt, EventType string
	ContextLimit, Attempts                                                                                                                  int
	Enabled                                                                                                                                 bool
	Deadline                                                                                                                                *time.Time
	ChatProfileID                                                                                                                           *string
}

func (a *App) claimInbox(ctx context.Context) (inboxJob, error) {
	tx, err := a.db.Begin(ctx)
	if err != nil {
		return inboxJob{}, err
	}
	defer tx.Rollback(ctx)
	var j inboxJob
	err = tx.QueryRow(ctx, `SELECT t.id::text,m.id::text,m.bot_id::text,m.conversation_id::text,c.type,c.platform_id,COALESCE(m.platform_message_id,''),m.content,c.system_prompt,c.context_limit,c.enabled,m.reply_deadline,t.attempts,c.chat_profile_id::text,m.event_type FROM inbox_tasks t JOIN messages m ON m.id=t.message_id JOIN conversations c ON c.id=m.conversation_id WHERE t.status='pending' AND t.next_attempt_at<=now() ORDER BY m.reply_deadline NULLS LAST,t.created_at FOR UPDATE OF t SKIP LOCKED LIMIT 1`).Scan(&j.TaskID, &j.MessageID, &j.BotID, &j.ConversationID, &j.ConversationType, &j.PlatformConversationID, &j.PlatformMessageID, &j.Content, &j.SystemPrompt, &j.ContextLimit, &j.Enabled, &j.Deadline, &j.Attempts, &j.ChatProfileID, &j.EventType)
	if err != nil {
		return inboxJob{}, err
	}
	_, err = tx.Exec(ctx, "UPDATE inbox_tasks SET status='processing',locked_at=now(),attempts=attempts+1,updated_at=now() WHERE id=$1", j.TaskID)
	if err != nil {
		return inboxJob{}, err
	}
	return j, tx.Commit(ctx)
}
func (a *App) processInbox(ctx context.Context) error {
	j, err := a.claimInbox(ctx)
	if err != nil {
		return err
	}
	if !j.Enabled || (j.Deadline != nil && time.Now().After(*j.Deadline)) {
		return a.finishInbox(ctx, j.TaskID, "skipped", nil)
	}
	profileID := ""
	if j.ChatProfileID != nil {
		profileID = *j.ChatProfileID
	}
	if profileID == "" {
		_ = a.db.QueryRow(ctx, "SELECT COALESCE(default_chat_profile_id::text,'') FROM bots WHERE id=$1", j.BotID).Scan(&profileID)
	}
	if profileID == "" {
		if err = a.db.QueryRow(ctx, "SELECT id::text FROM model_profiles WHERE kind='chat' AND enabled ORDER BY is_default DESC,created_at LIMIT 1").Scan(&profileID); err != nil {
			return a.retryInbox(ctx, j, fmt.Errorf("no chat model configured"))
		}
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
	systemPrompt := strings.TrimSpace(j.SystemPrompt) + "\n\n如需让机器人向 QQ 用户发送图片，请在回复中使用 Markdown 图片语法 ![图片说明](https://公开图片地址)，每次回复最多发送一张图片。"
	messages := []domain.ChatMessage{{Role: "system", Content: strings.TrimSpace(systemPrompt)}}
	retrievalStarted := time.Now()
	var kbContext string
	var hits []map[string]any
	if strings.TrimSpace(j.Content) != "" {
		kbContext, hits, err = a.retrieveForConversation(ctx, j.ConversationID, j.Content)
	}
	retrievalLatency := time.Since(retrievalStarted).Milliseconds()
	if err != nil {
		a.log.Warn("knowledge retrieval failed", "message", j.MessageID, "error", err)
	}
	if kbContext != "" {
		messages = append(messages, domain.ChatMessage{Role: "system", Content: "以下是知识库检索结果。仅在相关时使用，并优先忠于资料：\n" + kbContext})
	}
	contextStarted := time.Now()
	historyLimit := contextHistoryLimit(j.ContextLimit)
	if historyLimit > 0 {
		rows, queryErr := a.db.Query(ctx, `SELECT direction,content,sender_name FROM messages WHERE conversation_id=$1 AND id<>$2 AND content<>'' AND context_excluded=false AND event_type<>'PROCESSING_ACK' ORDER BY event_at DESC LIMIT $3`, j.ConversationID, j.MessageID, historyLimit)
		if queryErr == nil {
			type hist struct{ dir, content, name string }
			var hs []hist
			for rows.Next() {
				var h hist
				_ = rows.Scan(&h.dir, &h.content, &h.name)
				hs = append(hs, h)
			}
			rows.Close()
			for i := len(hs) - 1; i >= 0; i-- {
				role := "user"
				if hs[i].dir == "outbound" {
					role = "assistant"
				}
				content := hs[i].content
				if hs[i].name != "" && role == "user" {
					content = hs[i].name + ": " + content
				}
				messages = append(messages, domain.ChatMessage{Role: role, Content: content})
			}
		}
	}
	userContent := strings.TrimSpace(j.Content)
	if userContent == "" && len(imageParts) > 0 {
		userContent = "请分析用户发送的图片并回答。"
	}
	messages = append(messages, domain.ChatMessage{Role: "user", Content: userContent, Parts: imageParts})
	contextLatency := time.Since(contextStarted).Milliseconds()
	contextJSON, _ := json.Marshal(messages)
	_, _ = a.db.Exec(ctx, "UPDATE agent_runs SET context_latency_ms=$1,retrieval_latency_ms=$2,context_messages=$3::jsonb WHERE id=$4", contextLatency, retrievalLatency, string(contextJSON), runID)
	start := time.Now()
	result, chatErr := client.Chat(ctx, messages)
	latency := time.Since(start).Milliseconds()
	_, _ = a.db.Exec(ctx, "INSERT INTO model_calls(agent_run_id,profile_id,kind,input_tokens,output_tokens,latency_ms,error) VALUES($1,$2,'chat',$3,$4,$5,$6)", runID, profileID, result.InputTokens, result.OutputTokens, latency, errorText(chatErr))
	if chatErr != nil {
		_, _ = a.db.Exec(ctx, "UPDATE agent_runs SET status='failed',error=$1,completed_at=now() WHERE id=$2", chatErr.Error(), runID)
		if openai.IsHTTPStatus(chatErr, 403) {
			return a.excludeModeratedMessage(ctx, j, chatErr)
		}
		return a.retryInbox(ctx, j, chatErr)
	}
	hitsJSON, _ := json.Marshal(hits)
	tx, err := a.db.Begin(ctx)
	if err != nil {
		return a.retryInbox(ctx, j, err)
	}
	defer tx.Rollback(ctx)
	responseText := strings.TrimSpace(result.Content)
	_, responseParts := extractOutboundImage(result.Content)
	partsJSON, _ := json.Marshal(responseParts)
	var outID string
	err = tx.QueryRow(ctx, `INSERT INTO messages(channel,bot_id,conversation_id,direction,event_type,content,parts,reply_to_message_id,status,event_at,reply_deadline) VALUES('qq',$1,$2,'outbound','AI_RESPONSE',$3,$4::jsonb,$5,'pending',now(),$6) RETURNING id::text`, j.BotID, j.ConversationID, responseText, string(partsJSON), j.PlatformMessageID, j.Deadline).Scan(&outID)
	if err == nil {
		_, err = tx.Exec(ctx, "INSERT INTO outbox_tasks(message_id,msg_seq) VALUES($1,2)", outID)
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
	return tx.Commit(ctx)
}

func contextHistoryLimit(totalMessages int) int {
	if totalMessages <= 1 {
		return 0
	}
	return totalMessages - 1
}

func (a *App) excludeModeratedMessage(ctx context.Context, j inboxJob, cause error) error {
	tx, err := a.db.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	reason := "模型返回 HTTP 403，已从后续会话上下文排除"
	if _, err = tx.Exec(ctx, `UPDATE messages SET context_excluded=true,context_exclusion_reason=$1 WHERE id=$2`, reason, j.MessageID); err != nil {
		return err
	}
	if _, err = tx.Exec(ctx, `UPDATE inbox_tasks SET status='failed',last_error=$1,next_attempt_at=now(),updated_at=now() WHERE id=$2`, cause.Error(), j.TaskID); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func (a *App) retrieveForConversation(ctx context.Context, conversationID, query string) (string, []map[string]any, error) {
	rows, err := a.db.Query(ctx, `SELECT k.id::text,k.embedding_profile_id::text FROM conversation_knowledge_bases ck JOIN knowledge_bases k ON k.id=ck.knowledge_base_id WHERE ck.conversation_id=$1`, conversationID)
	if err != nil {
		return "", nil, err
	}
	defer rows.Close()
	type kb struct{ id, pid string }
	var kbs []kb
	for rows.Next() {
		var k kb
		if err = rows.Scan(&k.id, &k.pid); err != nil {
			return "", nil, err
		}
		kbs = append(kbs, k)
	}
	var all []map[string]any
	for _, k := range kbs {
		cl, _, e := a.loadModel(ctx, k.pid)
		if e != nil {
			continue
		}
		v, e := cl.Embed(ctx, []string{query})
		if e != nil {
			continue
		}
		rs, e := a.db.Query(ctx, `SELECT id::text,document_id::text,content,1-(embedding <=> $1::vector) AS score FROM knowledge_chunks WHERE knowledge_base_id=$2 ORDER BY embedding <=> $1::vector LIMIT 8`, vectorLiteral(v[0]), k.id)
		if e != nil {
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
	return b.String(), all, nil
}
func hitScore(hit map[string]any) float64 {
	switch v := hit["score"].(type) {
	case float64:
		return v
	case float32:
		return float64(v)
	default:
		return 0
	}
}
func (a *App) finishInbox(ctx context.Context, id, status string, err error) error {
	_, e := a.db.Exec(ctx, "UPDATE inbox_tasks SET status=$1,last_error=$2,updated_at=now() WHERE id=$3", status, errorText(err), id)
	return e
}
func (a *App) retryInbox(ctx context.Context, j inboxJob, cause error) error {
	status := "pending"
	if j.Attempts+1 >= 3 {
		status = "failed"
	}
	_, err := a.db.Exec(ctx, "UPDATE inbox_tasks SET status=$1,last_error=$2,next_attempt_at=now()+interval '5 seconds',updated_at=now() WHERE id=$3", status, cause.Error(), j.TaskID)
	return err
}

type documentJob struct {
	ID, KBID, StorageKey, ProfileID string
	Attempts                        int
}

func (a *App) claimDocument(ctx context.Context) (documentJob, error) {
	tx, err := a.db.Begin(ctx)
	if err != nil {
		return documentJob{}, err
	}
	defer tx.Rollback(ctx)
	var j documentJob
	err = tx.QueryRow(ctx, `SELECT d.id::text,d.knowledge_base_id::text,d.storage_key,k.embedding_profile_id::text,d.attempts FROM knowledge_documents d JOIN knowledge_bases k ON k.id=d.knowledge_base_id WHERE d.status='pending' AND d.next_attempt_at<=now() ORDER BY d.created_at FOR UPDATE OF d SKIP LOCKED LIMIT 1`).Scan(&j.ID, &j.KBID, &j.StorageKey, &j.ProfileID, &j.Attempts)
	if err != nil {
		return j, err
	}
	_, err = tx.Exec(ctx, "UPDATE knowledge_documents SET status='processing',attempts=attempts+1,updated_at=now() WHERE id=$1", j.ID)
	if err != nil {
		return j, err
	}
	return j, tx.Commit(ctx)
}
func (a *App) processDocument(ctx context.Context) error {
	j, err := a.claimDocument(ctx)
	if err != nil {
		return err
	}
	data, err := a.files.Read(ctx, j.StorageKey)
	if err != nil {
		return a.retryDocumentJob(ctx, j, fmt.Errorf("读取原始文件失败: %w", err))
	}
	chunks := knowledge.Chunk(string(data), 1000, 150)
	if len(chunks) == 0 {
		return a.retryDocumentJob(ctx, j, fmt.Errorf("文档内容为空，无法建立索引"))
	}
	client, kind, _, configuredDimension, err := a.loadModelDetails(ctx, j.ProfileID)
	if err != nil {
		return a.retryDocumentJob(ctx, j, fmt.Errorf("加载 Embedding 模型配置失败: %w", err))
	}
	if kind != "embedding" {
		return a.retryDocumentJob(ctx, j, fmt.Errorf("模型配置错误：知识库绑定的配置类型为 %s，不是 embedding", kind))
	}
	vectors := make([][]float32, 0, len(chunks))
	actualDimension := 0
	for start := 0; start < len(chunks); start += 16 {
		end := start + 16
		if end > len(chunks) {
			end = len(chunks)
		}
		v, e := client.Embed(ctx, chunks[start:end])
		if e != nil {
			return a.retryDocumentJob(ctx, j, fmt.Errorf("生成 Embedding 失败（批次 %d-%d，共 %d 个分块）: %w", start+1, end, len(chunks), e))
		}
		if len(v) != end-start {
			return a.retryDocumentJob(ctx, j, fmt.Errorf("Embedding 批次返回数量异常（批次 %d-%d）：期望 %d，实际 %d", start+1, end, end-start, len(v)))
		}
		for i, vector := range v {
			if len(vector) == 0 {
				return a.retryDocumentJob(ctx, j, fmt.Errorf("Embedding 返回空向量（分块 %d）", start+i+1))
			}
			if configuredDimension != nil && len(vector) != *configuredDimension {
				return a.retryDocumentJob(ctx, j, fmt.Errorf("Embedding 维度不匹配（分块 %d）：配置为 %d，实际返回 %d", start+i+1, *configuredDimension, len(vector)))
			}
			if actualDimension == 0 {
				actualDimension = len(vector)
			} else if len(vector) != actualDimension {
				return a.retryDocumentJob(ctx, j, fmt.Errorf("Embedding 维度不一致（分块 %d）：首个向量为 %d，当前向量为 %d", start+i+1, actualDimension, len(vector)))
			}
		}
		vectors = append(vectors, v...)
	}
	tx, err := a.db.Begin(ctx)
	if err != nil {
		return a.retryDocumentJob(ctx, j, fmt.Errorf("开始写入向量数据库失败: %w", err))
	}
	defer tx.Rollback(ctx)
	_, err = tx.Exec(ctx, "DELETE FROM knowledge_chunks WHERE document_id=$1", j.ID)
	for i, ch := range chunks {
		if err == nil {
			_, err = tx.Exec(ctx, "INSERT INTO knowledge_chunks(knowledge_base_id,document_id,chunk_index,content,embedding) VALUES($1,$2,$3,$4,$5::vector)", j.KBID, j.ID, i, ch, vectorLiteral(vectors[i]))
		}
	}
	if err == nil {
		_, err = tx.Exec(ctx, "UPDATE knowledge_documents SET status='ready',last_error=NULL,updated_at=now() WHERE id=$1", j.ID)
	}
	if err != nil {
		_ = tx.Rollback(ctx)
		return a.retryDocumentJob(ctx, j, fmt.Errorf("写入向量数据库失败: %w", err))
	}
	if err = tx.Commit(ctx); err != nil {
		return a.retryDocumentJob(ctx, j, fmt.Errorf("提交向量数据库事务失败: %w", err))
	}
	return nil
}
func (a *App) retryDocumentJob(ctx context.Context, j documentJob, cause error) error {
	status := "pending"
	if j.Attempts+1 >= 3 {
		status = "failed"
	}
	_, err := a.db.Exec(ctx, "UPDATE knowledge_documents SET status=$1,last_error=$2,next_attempt_at=now()+interval '10 seconds',updated_at=now() WHERE id=$3", status, cause.Error(), j.ID)
	return err
}

type outboxJob struct {
	TaskID, MessageID, BotID, ConversationType, ConversationID, ReplyTo, Text, EventType, AppID, SecretEnc string
	Sequence, Attempts                                                                                     int
	Deadline                                                                                               *time.Time
	Parts                                                                                                  []domain.ContentPart
}

func (a *App) claimOutbox(ctx context.Context) (outboxJob, error) {
	tx, err := a.db.Begin(ctx)
	if err != nil {
		return outboxJob{}, err
	}
	defer tx.Rollback(ctx)
	var j outboxJob
	var partsRaw []byte
	err = tx.QueryRow(ctx, `SELECT t.id::text,m.id::text,m.bot_id::text,c.type,c.platform_id,COALESCE(m.reply_to_message_id,''),m.content,m.event_type,m.parts,b.app_id,b.app_secret_enc,t.msg_seq,t.attempts,m.reply_deadline FROM outbox_tasks t JOIN messages m ON m.id=t.message_id JOIN conversations c ON c.id=m.conversation_id JOIN bots b ON b.id=m.bot_id WHERE t.status='pending' AND t.next_attempt_at<=now() ORDER BY m.reply_deadline NULLS LAST,t.created_at FOR UPDATE OF t SKIP LOCKED LIMIT 1`).Scan(&j.TaskID, &j.MessageID, &j.BotID, &j.ConversationType, &j.ConversationID, &j.ReplyTo, &j.Text, &j.EventType, &partsRaw, &j.AppID, &j.SecretEnc, &j.Sequence, &j.Attempts, &j.Deadline)
	if err != nil {
		return j, err
	}
	_ = json.Unmarshal(partsRaw, &j.Parts)
	_, err = tx.Exec(ctx, "UPDATE outbox_tasks SET status='processing',attempts=attempts+1,locked_at=now(),updated_at=now() WHERE id=$1", j.TaskID)
	if err != nil {
		return j, err
	}
	return j, tx.Commit(ctx)
}
func (a *App) processOutbox(ctx context.Context) error {
	j, err := a.claimOutbox(ctx)
	if err != nil {
		return err
	}
	if j.Deadline != nil && time.Now().After(*j.Deadline) {
		_, err = a.db.Exec(ctx, "UPDATE outbox_tasks SET status='expired',last_error='reply deadline exceeded',updated_at=now() WHERE id=$1", j.TaskID)
		_, _ = a.db.Exec(ctx, "UPDATE messages SET status='expired' WHERE id=$1", j.MessageID)
		return err
	}
	secret, err := a.cipher.Decrypt(j.SecretEnc)
	if err != nil {
		return a.retryOutbox(ctx, j, err)
	}
	for index := range j.Parts {
		if j.Parts[index].Type == "image" && j.Parts[index].StorageKey != "" {
			j.Parts[index].Data, err = a.images.Read(ctx, j.Parts[index].StorageKey)
			if err != nil {
				return a.retryOutbox(ctx, j, fmt.Errorf("读取待发送图片失败: %w", err))
			}
		}
	}
	client := qq.NewClient(j.AppID, secret, a.cfg.QQAPIBaseURL, a.cfg.QQTokenURL)
	format := ""
	if j.EventType == "AI_RESPONSE" {
		format = "markdown"
	}
	pid, err := client.Send(ctx, domain.OutboundMessage{ID: j.MessageID, Channel: "qq", BotID: j.BotID, ConversationType: j.ConversationType, ConversationID: j.ConversationID, ReplyToMessageID: j.ReplyTo, Text: j.Text, Format: format, Parts: j.Parts, ReplyDeadline: j.Deadline, Sequence: j.Sequence})
	if err != nil {
		if hasContentPart(j.Parts, "ark_ack") {
			_, _ = a.db.Exec(ctx, "UPDATE outbox_tasks SET status='failed',last_error=$1,updated_at=now() WHERE id=$2", err.Error(), j.TaskID)
			_, _ = a.db.Exec(ctx, "UPDATE messages SET status='failed' WHERE id=$1", j.MessageID)
			return nil
		}
		return a.retryOutbox(ctx, j, err)
	}
	tx, err := a.db.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	_, err = tx.Exec(ctx, "UPDATE outbox_tasks SET status='sent',platform_message_id=$1,updated_at=now() WHERE id=$2", pid, j.TaskID)
	if err == nil {
		_, err = tx.Exec(ctx, "UPDATE messages SET status='sent',platform_message_id=$1 WHERE id=$2", nullString(pid), j.MessageID)
	}
	if err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func hasContentPart(parts []domain.ContentPart, partType string) bool {
	for _, part := range parts {
		if part.Type == partType {
			return true
		}
	}
	return false
}
func (a *App) retryOutbox(ctx context.Context, j outboxJob, cause error) error {
	status := "pending"
	if j.Attempts+1 >= 3 {
		status = "failed"
	}
	_, err := a.db.Exec(ctx, "UPDATE outbox_tasks SET status=$1,last_error=$2,next_attempt_at=now()+interval '5 seconds',updated_at=now() WHERE id=$3", status, cause.Error(), j.TaskID)
	if status == "failed" {
		_, _ = a.db.Exec(ctx, "UPDATE messages SET status='failed' WHERE id=$1", j.MessageID)
	}
	return err
}
func errorText(err error) any {
	if err == nil {
		return nil
	}
	return err.Error()
}
