package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"time"

	"ai-bot/backend/internal/domain"
	"ai-bot/backend/internal/knowledge"
	"ai-bot/backend/internal/qq"
	"github.com/jackc/pgx/v5"
)

func (a *App) RunWorkers(ctx context.Context) {
	var wg sync.WaitGroup
	for name, fn := range map[string]func(context.Context) error{"inbox": a.processInbox, "documents": a.processDocument, "outbox": a.processOutbox, "retractions": a.processRetraction, "agent-jobs": a.processAgentJob, "eval": a.processEvalJob} {
		name, fn := name, fn
		wg.Add(1)
		go func() { defer wg.Done(); a.workerLoop(ctx, name, fn) }()
	}
	wg.Add(1)
	go func() { defer wg.Done(); a.maintenanceLoop(ctx) }()
	wg.Add(1)
	go func() { defer wg.Done(); a.liveDataLoop(ctx) }()
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
	if _, err = tx.Exec(ctx, scheduleProcessingAckRetractionSQL, j.BotID, j.ConversationID, j.PlatformMessageID); err != nil {
		return err
	}
	return tx.Commit(ctx)
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
	if err == nil && status == "failed" {
		err = a.scheduleProcessingAckRetraction(ctx, j.BotID, j.ConversationID, j.PlatformMessageID)
	}
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
	TaskID, MessageID, BotID, ConversationRecordID, ConversationType, ConversationID, ReplyTo, Text, EventType, AppID, SecretEnc string
	Sequence, Attempts                                                                                                           int
	Deadline                                                                                                                     *time.Time
	Parts                                                                                                                        []domain.ContentPart
}

func (a *App) claimOutbox(ctx context.Context) (outboxJob, error) {
	tx, err := a.db.Begin(ctx)
	if err != nil {
		return outboxJob{}, err
	}
	defer tx.Rollback(ctx)
	var j outboxJob
	var partsRaw []byte
	err = tx.QueryRow(ctx, `SELECT t.id::text,m.id::text,m.bot_id::text,c.id::text,c.type,c.platform_id,COALESCE(m.reply_to_message_id,''),m.content,m.event_type,m.parts,b.app_id,b.app_secret_enc,t.msg_seq,t.attempts,m.reply_deadline FROM outbox_tasks t JOIN messages m ON m.id=t.message_id JOIN conversations c ON c.id=m.conversation_id JOIN bots b ON b.id=m.bot_id WHERE t.status='pending' AND t.next_attempt_at<=now() ORDER BY m.reply_deadline NULLS LAST,t.created_at FOR UPDATE OF t SKIP LOCKED LIMIT 1`).Scan(&j.TaskID, &j.MessageID, &j.BotID, &j.ConversationRecordID, &j.ConversationType, &j.ConversationID, &j.ReplyTo, &j.Text, &j.EventType, &partsRaw, &j.AppID, &j.SecretEnc, &j.Sequence, &j.Attempts, &j.Deadline)
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
		if err == nil && j.EventType == "AI_RESPONSE" {
			err = a.scheduleProcessingAckRetraction(ctx, j.BotID, j.ConversationRecordID, j.ReplyTo)
		}
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
	sendCtx := ctx
	cancelSend := func() {}
	if hasContentPart(j.Parts, "ark_ack") {
		sendCtx, cancelSend = context.WithTimeout(ctx, 5*time.Second)
	}
	pid, err := client.Send(sendCtx, domain.OutboundMessage{ID: j.MessageID, Channel: "qq", BotID: j.BotID, ConversationType: j.ConversationType, ConversationID: j.ConversationID, ReplyToMessageID: j.ReplyTo, Text: j.Text, Format: format, Parts: j.Parts, ReplyDeadline: j.Deadline, Sequence: j.Sequence})
	cancelSend()
	if err != nil {
		if hasContentPart(j.Parts, "ark_ack") {
			a.log.Warn("QQ processing ARK failed", "bot_id", j.BotID, "conversation_type", j.ConversationType, "conversation_id", j.ConversationID, "error", err)
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
	if err == nil && j.EventType == "AI_RESPONSE" {
		_, err = tx.Exec(ctx, scheduleProcessingAckRetractionSQL, j.BotID, j.ConversationRecordID, j.ReplyTo)
	}
	if err != nil {
		return err
	}
	return tx.Commit(ctx)
}

const scheduleProcessingAckRetractionSQL = `INSERT INTO message_retractions(message_id)
	SELECT id FROM messages
	WHERE bot_id=$1 AND conversation_id=$2 AND reply_to_message_id=$3 AND event_type='PROCESSING_ACK'
	ORDER BY created_at DESC LIMIT 1
	ON CONFLICT(message_id) DO NOTHING`

func (a *App) scheduleProcessingAckRetraction(ctx context.Context, botID, conversationID, replyTo string) error {
	_, err := a.db.Exec(ctx, scheduleProcessingAckRetractionSQL, botID, conversationID, replyTo)
	return err
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
	a.log.Warn("QQ reply delivery failed", "bot_id", j.BotID, "conversation_type", j.ConversationType, "conversation_id", j.ConversationID, "attempt", j.Attempts+1, "error", cause)
	status := "pending"
	if j.Attempts+1 >= 3 {
		status = "failed"
	}
	_, err := a.db.Exec(ctx, "UPDATE outbox_tasks SET status=$1,last_error=$2,next_attempt_at=now()+interval '5 seconds',updated_at=now() WHERE id=$3", status, cause.Error(), j.TaskID)
	if status == "failed" {
		_, _ = a.db.Exec(ctx, "UPDATE messages SET status='failed' WHERE id=$1", j.MessageID)
		if err == nil && j.EventType == "AI_RESPONSE" {
			err = a.scheduleProcessingAckRetraction(ctx, j.BotID, j.ConversationRecordID, j.ReplyTo)
		}
	}
	return err
}

type retractionJob struct {
	TaskID, MessageID, BotID, ConversationType, ConversationID, PlatformMessageID, AppID, SecretEnc string
	Attempts                                                                                        int
	ExpiresAt                                                                                       time.Time
}

func (a *App) claimRetraction(ctx context.Context) (retractionJob, error) {
	_, _ = a.db.Exec(ctx, "UPDATE message_retractions SET status='expired',updated_at=now() WHERE status='pending' AND expires_at<=now()")
	tx, err := a.db.Begin(ctx)
	if err != nil {
		return retractionJob{}, err
	}
	defer tx.Rollback(ctx)
	var j retractionJob
	err = tx.QueryRow(ctx, `SELECT r.id::text,m.id::text,m.bot_id::text,c.type,c.platform_id,m.platform_message_id,b.app_id,b.app_secret_enc,r.attempts,r.expires_at
		FROM message_retractions r
		JOIN messages m ON m.id=r.message_id
		JOIN conversations c ON c.id=m.conversation_id
		JOIN bots b ON b.id=m.bot_id
		WHERE r.status='pending' AND r.next_attempt_at<=now() AND r.expires_at>now() AND m.platform_message_id IS NOT NULL
		ORDER BY r.created_at FOR UPDATE OF r SKIP LOCKED LIMIT 1`).Scan(&j.TaskID, &j.MessageID, &j.BotID, &j.ConversationType, &j.ConversationID, &j.PlatformMessageID, &j.AppID, &j.SecretEnc, &j.Attempts, &j.ExpiresAt)
	if err != nil {
		return j, err
	}
	if _, err = tx.Exec(ctx, "UPDATE message_retractions SET status='processing',attempts=attempts+1,locked_at=now(),updated_at=now() WHERE id=$1", j.TaskID); err != nil {
		return j, err
	}
	return j, tx.Commit(ctx)
}

func (a *App) processRetraction(ctx context.Context) error {
	j, err := a.claimRetraction(ctx)
	if err != nil {
		return err
	}
	secret, err := a.cipher.Decrypt(j.SecretEnc)
	if err != nil {
		return a.retryRetraction(ctx, j, err)
	}
	client := qq.NewClient(j.AppID, secret, a.cfg.QQAPIBaseURL, a.cfg.QQTokenURL)
	retractCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	err = client.Retract(retractCtx, j.ConversationType, j.ConversationID, j.PlatformMessageID)
	cancel()
	if err != nil {
		return a.retryRetraction(ctx, j, err)
	}
	tx, err := a.db.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	if _, err = tx.Exec(ctx, "UPDATE message_retractions SET status='retracted',last_error=NULL,updated_at=now() WHERE id=$1", j.TaskID); err == nil {
		_, err = tx.Exec(ctx, "UPDATE messages SET status='retracted' WHERE id=$1", j.MessageID)
	}
	if err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func (a *App) retryRetraction(ctx context.Context, j retractionJob, cause error) error {
	a.log.Warn("QQ processing ARK retraction failed", "bot_id", j.BotID, "conversation_type", j.ConversationType, "conversation_id", j.ConversationID, "attempt", j.Attempts+1, "error", cause)
	status := "pending"
	if j.Attempts+1 >= 3 {
		status = "failed"
	} else if time.Now().Add(2 * time.Second).After(j.ExpiresAt) {
		status = "expired"
	}
	_, err := a.db.Exec(ctx, "UPDATE message_retractions SET status=$1,last_error=$2,next_attempt_at=now()+interval '2 seconds',updated_at=now() WHERE id=$3", status, cause.Error(), j.TaskID)
	return err
}
func errorText(err error) any {
	if err == nil {
		return nil
	}
	return err.Error()
}
