package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"time"

	"ai-bot/backend/internal/agent"
	"ai-bot/backend/internal/domain"
	"ai-bot/backend/internal/knowledge"
	"ai-bot/backend/internal/qq"
	"github.com/jackc/pgx/v5"
)

func (a *App) RunWorkers(ctx context.Context) {
	var wg sync.WaitGroup
	wg.Add(1)
	go func() { defer wg.Done(); a.runAdminEvents(ctx) }()
	wg.Go(func() { dispatchJobs(ctx, a, "inbox", a.claimInbox, a.handleInbox) })
	wg.Go(func() { dispatchJobs(ctx, a, "outbox", a.claimOutbox, a.handleOutbox) })
	wg.Go(func() { dispatchJobs(ctx, a, "group-reviews", a.claimGuardReview, a.runJob) })
	for name, fn := range map[string]func(context.Context) error{"group-actions": a.processGuardAction, "documents": a.processDocument, "retractions": a.processRetraction, "agent-jobs": a.processAgentJob, "eval": a.processEvalJob} {
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
		if err := a.cleanDeletedKnowledgeFiles(ctx, nil); err != nil {
			a.log.Error("清理已删除知识库的文件失败", "error", err)
		}
		if err := a.queueScheduledPosts(ctx, time.Now()); err != nil && !errors.Is(err, context.Canceled) {
			a.log.Error("scheduled posts", "error", err)
		}
		if err := a.queueDueGroupDigests(ctx); err != nil && !errors.Is(err, context.Canceled) {
			a.log.Error("group digests", "error", err)
		}
		if err := a.queueDueProactiveTriggers(ctx, time.Now()); err != nil && !errors.Is(err, context.Canceled) {
			a.log.Error("proactive triggers", "error", err)
		}
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
	if err := a.expireHandoffs(ctx); err != nil {
		return err
	}
	settings, err := a.runtimeSettings(ctx)
	if err != nil {
		return fmt.Errorf("读取消息保留设置失败: %w", err)
	}
	if _, err = a.db.Exec(ctx, "UPDATE agent_jobs SET status='pending' WHERE kind='group_review' AND status='processing' AND locked_at<now()-interval '5 minutes'"); err != nil {
		return err
	}
	if _, err = a.db.Exec(ctx, "UPDATE group_guard_actions SET status='unknown',detail='处理被中断，请核实 QQ 里的实际状态' WHERE status='processing' AND updated_at<now()-interval '5 minutes'"); err != nil {
		return err
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
	if _, err = tx.Exec(ctx, "DELETE FROM agent_jobs WHERE status IN ('completed','failed') AND updated_at<now()-interval '7 days'"); err != nil {
		return fmt.Errorf("清理后台任务失败: %w", err)
	}
	if _, err = tx.Exec(ctx, "DELETE FROM system_alerts WHERE created_at<$1", cutoff); err != nil {
		return fmt.Errorf("清理告警失败: %w", err)
	}
	if _, err = tx.Exec(ctx, "DELETE FROM scheduled_web_pages WHERE expires_at<=now()"); err != nil {
		return fmt.Errorf("清理过期定时网页失败: %w", err)
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

// Each successful claim reserves its conversation in the database. Dispatch
// immediately so model and network latency never hold up the next claim.
func dispatchJobs[J any](ctx context.Context, a *App, name string, claim func(context.Context) (J, error), handle func(context.Context, J) error) {
	var running sync.WaitGroup
	defer running.Wait()
	wake := make(chan struct{}, 1)
	ticker := time.NewTicker(a.cfg.WorkerPoll)
	defer ticker.Stop()
	for ctx.Err() == nil {
		job, err := claim(ctx)
		if err == nil {
			running.Go(func() {
				if err := handle(ctx, job); err != nil && ctx.Err() == nil {
					a.log.Error("worker error", "worker", name, "error", err)
				}
				select {
				case wake <- struct{}{}:
				default:
				}
			})
			continue
		}
		if !errors.Is(err, pgx.ErrNoRows) && ctx.Err() == nil {
			a.log.Error("claim error", "worker", name, "error", err)
		}
		select {
		case <-ctx.Done():
			return
		case <-wake:
		case <-ticker.C:
		}
	}
}

type inboxJob struct {
	TaskID, MessageID, BotID, ConversationID, ConversationType, PlatformConversationID, PlatformMessageID, Content, SystemPrompt, EventType, TriggerMode string
	HasImage, MentionsBot                                                                                                                                bool
	ContextLimit, Attempts                                                                                                                               int
	Enabled                                                                                                                                              bool
	Deadline                                                                                                                                             *time.Time
}

func (a *App) claimInbox(ctx context.Context) (inboxJob, error) {
	// Reject a stale candidate if a webhook committed a new segment while locking it.
	tx, err := a.db.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead})
	if err != nil {
		return inboxJob{}, err
	}
	defer tx.Rollback(ctx)
	var j inboxJob
	err = tx.QueryRow(ctx, `SELECT t.id::text,m.id::text,m.bot_id::text,m.conversation_id::text,c.type,c.platform_id,COALESCE(m.platform_message_id,''),display_mentions(m,m.content),c.system_prompt,c.context_limit,c.enabled AND NOT c.bot_removed,m.reply_deadline,t.attempts,m.event_type,c.trigger_mode,m.parts @> '[{"type":"image"}]'::jsonb,COALESCE((m.raw_event#>'{d,mentions}') @> '[{"is_you":true}]'::jsonb,false) FROM inbox_tasks t JOIN messages m ON m.id=t.message_id JOIN conversations c ON c.id=m.conversation_id WHERE t.status='pending' AND t.next_attempt_at<=now()
    AND NOT EXISTS (SELECT FROM inbox_tasks active JOIN messages x ON x.id=active.message_id
      WHERE x.conversation_id=c.id AND active.status='processing')
    AND COALESCE(m.reply_batch,m.id) = (SELECT COALESCE(x.reply_batch,x.id)
      FROM messages x JOIN inbox_tasks head ON head.message_id=x.id
      WHERE x.conversation_id=c.id AND head.status='pending' ORDER BY x.created_at,x.id LIMIT 1)
    AND NOT EXISTS (SELECT FROM messages x JOIN group_reviews r ON r.message_id=x.id
      JOIN agent_jobs review ON review.dedupe_key='review:'||x.id::text
      WHERE x.conversation_id=c.id AND r.queue_reply AND r.status='pending'
      AND review.status IN ('pending','processing') AND (x.created_at,x.id)<(m.created_at,m.id))
	AND (m.reply_batch IS NULL OR NOT EXISTS (
      SELECT FROM messages other LEFT JOIN inbox_tasks ot ON ot.message_id=other.id
      WHERE other.reply_batch=m.reply_batch AND (
        other.reply_due_at>now()
        OR (ot.status='pending' AND (other.event_at,other.created_at,other.id)>(m.event_at,m.created_at,m.id))
        OR EXISTS (SELECT FROM group_reviews r JOIN agent_jobs job ON job.dedupe_key='review:'||r.message_id::text
          WHERE r.message_id=other.id AND r.status='pending' AND job.status IN ('pending','processing')))))
    AND (m.handoff_id IS NULL OR NOT EXISTS (
	  SELECT FROM messages prior JOIN outbox_tasks pt ON pt.message_id=prior.id
	  WHERE prior.handoff_id=m.handoff_id AND prior.conversation_id=m.conversation_id
	  AND pt.status IN ('pending','processing') AND (prior.created_at,prior.id)<(m.created_at,m.id)))
	ORDER BY m.reply_deadline NULLS LAST,t.created_at FOR UPDATE OF c,t SKIP LOCKED LIMIT 1`).Scan(&j.TaskID, &j.MessageID, &j.BotID, &j.ConversationID, &j.ConversationType, &j.PlatformConversationID, &j.PlatformMessageID, &j.Content, &j.SystemPrompt, &j.ContextLimit, &j.Enabled, &j.Deadline, &j.Attempts, &j.EventType, &j.TriggerMode, &j.HasImage, &j.MentionsBot)
	if err != nil {
		return inboxJob{}, err
	}
	_, err = tx.Exec(ctx, "UPDATE inbox_tasks SET status='processing',locked_at=now(),attempts=attempts+1,updated_at=now() WHERE id=$1", j.TaskID)
	if err != nil {
		return inboxJob{}, err
	}
	_, err = tx.Exec(ctx, `UPDATE inbox_tasks t SET status='merged',updated_at=now() FROM messages m,messages anchor
    WHERE anchor.id=$1 AND m.reply_batch=anchor.reply_batch AND t.message_id=m.id AND t.status='pending'`, j.MessageID)
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
	if err = a.quarantine(ctx, tx, j.MessageID); err != nil {
		return err
	}

	if _, err = tx.Exec(ctx, `UPDATE inbox_tasks SET status='failed',last_error=$1,next_attempt_at=now(),updated_at=now() WHERE id=$2 AND status='processing'`, cause.Error(), j.TaskID); err != nil {
		return err
	}
	if _, err = tx.Exec(ctx, "UPDATE agent_runs SET status='failed',error=$2,completed_at=now() WHERE message_id=$1 AND completed_at IS NULL", j.MessageID, cause.Error()); err != nil {
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
	_, e := a.db.Exec(ctx, "UPDATE inbox_tasks SET status=$1,last_error=$2,updated_at=now() WHERE id=$3 AND status='processing'", status, errorText(err), id)
	return e
}
func (a *App) retryInbox(ctx context.Context, j inboxJob, cause error) error {
	status := "pending"
	if j.Attempts+1 >= 3 {
		status = "failed"
	}
	_, err := a.db.Exec(ctx, "UPDATE inbox_tasks SET status=$1,last_error=$2,next_attempt_at=now()+interval '5 seconds',updated_at=now() WHERE id=$3 AND status='processing'", status, cause.Error(), j.TaskID)
	if err == nil && status == "failed" {
		err = a.scheduleProcessingAckRetraction(ctx, j.BotID, j.ConversationID, j.PlatformMessageID)
	}
	return err
}

type documentJob struct {
	ID, KBID, StorageKey, ProfileID, Name string
	Attempts                              int
}

func (a *App) claimDocument(ctx context.Context) (documentJob, error) {
	tx, err := a.db.Begin(ctx)
	if err != nil {
		return documentJob{}, err
	}
	defer tx.Rollback(ctx)
	var j documentJob
	err = tx.QueryRow(ctx, `SELECT d.id::text,d.knowledge_base_id::text,d.storage_key,k.embedding_profile_id::text,d.attempts,d.name FROM knowledge_documents d JOIN knowledge_bases k ON k.id=d.knowledge_base_id WHERE d.status='pending' AND d.next_attempt_at<=now() ORDER BY d.created_at FOR UPDATE OF d SKIP LOCKED FOR SHARE OF k SKIP LOCKED LIMIT 1`).Scan(&j.ID, &j.KBID, &j.StorageKey, &j.ProfileID, &j.Attempts, &j.Name)
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
	client, kind, _, configuredDimension, err := a.loadModelDetails(ctx, j.ProfileID)
	if err != nil {
		return a.retryDocumentJob(ctx, j, fmt.Errorf("加载 Embedding 模型配置失败: %w", err))
	}
	if kind != "embedding" {
		return a.retryDocumentJob(ctx, j, fmt.Errorf("模型配置错误：知识库绑定的配置类型为 %s，不是 embedding", kind))
	}
	var sections []knowledge.Section
	if client.Provider == "google" {
		resolve := func(name string) ([]byte, error) {
			var keys []string
			e := a.db.QueryRow(ctx, "SELECT ARRAY(SELECT storage_key FROM knowledge_documents WHERE knowledge_base_id=$1 AND name=$2 LIMIT 2)", j.KBID, name).Scan(&keys)
			if e != nil {
				return nil, e
			}
			if len(keys) != 1 {
				return nil, fmt.Errorf("Markdown 配图 %s 需要在同一知识库中有一个同名图片，请上传或排除重名后重试", name)
			}
			return a.files.Read(ctx, keys[0])
		}
		sections, err = knowledge.Multimodal(ctx, j.StorageKey, data, resolve)
	} else {
		var body string
		body, err = knowledge.Extract(ctx, j.StorageKey, data)
		for _, ch := range knowledge.Chunk(body, 1000, 150) {
			sections = append(sections, knowledge.Section{Text: ch})
		}
	}
	if err != nil {
		return a.retryDocumentJob(ctx, j, err)
	}
	if len(sections) == 0 {
		return a.retryDocumentJob(ctx, j, fmt.Errorf("文档没有可索引的文字或图片"))
	}
	chunks := make([]string, len(sections))
	for i, s := range sections {
		chunks[i] = s.Text
		if s.Text == "" && len(s.Image) > 0 {
			chunks[i] = "配图：" + j.Name
		}
	}
	vectors := make([][]float32, 0, len(chunks))
	actualDimension := 0
	for start := 0; start < len(chunks); {
		end := start + 1
		var v [][]float32
		var e error
		if s := sections[start]; len(s.Image) > 0 {
			began := time.Now()
			var vec []float32
			var usage domain.ChatResult
			vec, usage, e = client.GoogleImage(ctx, s.Text, s.MIME, s.Image)
			a.recordModelCall(ctx, "", "", j.ProfileID, "embedding_index", usage, time.Since(began).Milliseconds(), e)
			v = [][]float32{vec}
		} else {
			for end < len(chunks) && end-start < 16 && len(sections[end].Image) == 0 {
				end++
			}
			v, e = a.embed(ctx, client, j.ProfileID, "embedding_index", chunks[start:end])
		}
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
		start = end
	}
	tx, err := a.db.Begin(ctx)
	if err != nil {
		return a.retryDocumentJob(ctx, j, fmt.Errorf("开始写入向量数据库失败: %w", err))
	}
	defer tx.Rollback(ctx)
	var exists int
	if err = tx.QueryRow(ctx, "SELECT 1 FROM knowledge_bases WHERE id=$1 FOR SHARE", j.KBID).Scan(&exists); errors.Is(err, pgx.ErrNoRows) {
		return nil
	} else if err != nil {
		return err
	}
	_, err = tx.Exec(ctx, "DELETE FROM knowledge_chunks WHERE document_id=$1", j.ID)
	for i, ch := range chunks {
		if err == nil {
			var id string
			err = tx.QueryRow(ctx, "INSERT INTO knowledge_chunks(knowledge_base_id,document_id,chunk_index,content,embedding) VALUES($1,$2,$3,$4,$5::vector) RETURNING id::text", j.KBID, j.ID, i, ch, vectorLiteral(vectors[i])).Scan(&id)
			if err == nil && len(sections[i].Image) > 0 {
				_, err = tx.Exec(ctx, "INSERT INTO knowledge_chunk_images(chunk_id,mime_type,data) VALUES($1,$2,$3)", id, sections[i].MIME, sections[i].Image)
			}
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
	MaterialBatch                                                                                                                bool
	Extended                                                                                                                     bool
	TaskID, MessageID, BotID, ConversationRecordID, ConversationType, ConversationID, ReplyTo, Text, EventType, AppID, SecretEnc string
	Sequence, Attempts                                                                                                           int
	Deadline                                                                                                                     *time.Time
	Parts                                                                                                                        []domain.ContentPart
	Payload                                                                                                                      []byte
}

func (a *App) claimOutbox(ctx context.Context) (outboxJob, error) {
	tx, err := a.db.Begin(ctx)
	if err != nil {
		return outboxJob{}, err
	}
	defer tx.Rollback(ctx)
	var j outboxJob
	var partsRaw []byte
	err = tx.QueryRow(ctx, `SELECT t.id::text,m.id::text,m.bot_id::text,c.id::text,c.type,c.platform_id,COALESCE(m.reply_to_message_id,''),display_mentions(m,m.content),m.event_type,m.parts,b.app_id,b.app_secret_enc,t.msg_seq,t.attempts,m.reply_deadline,m.qq_send_payload,c.qq_extended_replies,m.material_batch IS NOT NULL FROM outbox_tasks t JOIN messages m ON m.id=t.message_id JOIN conversations c ON c.id=m.conversation_id JOIN bots b ON b.id=m.bot_id WHERE t.status='pending' AND t.next_attempt_at<=now()
    AND NOT EXISTS (SELECT FROM messages prior JOIN outbox_tasks pt ON pt.message_id=prior.id
      WHERE prior.conversation_id=c.id AND (pt.status='processing' OR
        (pt.status='pending' AND (prior.created_at,prior.id)<(m.created_at,m.id))))
	ORDER BY m.reply_deadline NULLS LAST,t.created_at FOR UPDATE OF c,t SKIP LOCKED LIMIT 1`).Scan(&j.TaskID, &j.MessageID, &j.BotID, &j.ConversationRecordID, &j.ConversationType, &j.ConversationID, &j.ReplyTo, &j.Text, &j.EventType, &partsRaw, &j.AppID, &j.SecretEnc, &j.Sequence, &j.Attempts, &j.Deadline, &j.Payload, &j.Extended, &j.MaterialBatch)
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
	return a.handleOutbox(ctx, j)
}

func (a *App) handleOutbox(ctx context.Context, j outboxJob) error {
	var err error
	if j.EventType == "AI_RESPONSE" && j.ReplyTo != "" {
		var blocked bool
		err = a.db.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM messages m WHERE m.conversation_id=$1 AND m.platform_message_id=$2 AND m.direction='inbound' AND (m.context_excluded OR EXISTS(SELECT FROM handoffs h WHERE h.conversation_id=m.conversation_id AND h.platform_user_id=m.sender_id AND h.status IN ('open','processing') AND h.message_id IS DISTINCT FROM m.id) OR EXISTS(SELECT 1 FROM blocked_users bu WHERE bu.bot_id=m.bot_id AND bu.platform_user_id=m.sender_id AND (bu.conversation_id IS NULL OR bu.conversation_id=m.conversation_id))))`, j.ConversationRecordID, j.ReplyTo).Scan(&blocked)
		if err != nil {
			return a.retryOutbox(ctx, j, err)
		}
		if blocked {
			_, err = a.db.Exec(ctx, `UPDATE outbox_tasks SET status='expired',last_error='blacklisted or excluded',updated_at=now() WHERE id=$1 AND status='processing'`, j.TaskID)
			return err
		}
	}
	if j.EventType == "HANDOFF_NOTICE" {
		var enabled bool
		if err = a.db.QueryRow(ctx, "SELECT human_operator FROM conversations WHERE id=$1", j.ConversationRecordID).Scan(&enabled); err != nil {
			return a.retryOutbox(ctx, j, err)
		}
		if !enabled {
			_, err = a.db.Exec(ctx, "UPDATE outbox_tasks SET status='expired',last_error='operator removed',updated_at=now() WHERE id=$1", j.TaskID)
			return err
		}
	}
	if j.EventType == "SCHEDULED_POST" || j.EventType == "DAILY_REPORT" {
		var enabled bool
		if err = a.db.QueryRow(ctx, "SELECT EXISTS(SELECT FROM schedule_deliveries d JOIN schedule_runs r ON r.id=d.run_id JOIN scheduled_posts s ON s.id=r.schedule_id AND s.revision=r.revision JOIN schedule_recipients dest ON dest.schedule_id=s.id AND dest.conversation_id=d.conversation_id WHERE d.message_id=$1 AND (s.enabled OR r.is_test))", j.MessageID).Scan(&enabled); err != nil {
			return a.retryOutbox(ctx, j, err)
		}
		if !enabled {
			_, err = a.db.Exec(ctx, "UPDATE outbox_tasks SET status='expired',updated_at=now() WHERE id=$1", j.TaskID)
			return err
		}
	}
	if j.EventType == "PROCESSING_ACK" {
		_, err = a.db.Exec(ctx, "UPDATE outbox_tasks SET status='expired',updated_at=now() WHERE id=$1", j.TaskID)
		return err
	}
	if !j.MaterialBatch && j.EventType != "HUMAN_REPLY" && j.EventType != "HANDOFF_UPDATE" && j.Deadline != nil && time.Now().After(*j.Deadline) {
		_, err = a.db.Exec(ctx, `WITH expired AS (UPDATE outbox_tasks SET status='expired',last_error='reply deadline exceeded',updated_at=now() WHERE id=$1 AND status='processing' RETURNING message_id)
		UPDATE messages SET status='expired' WHERE id IN (SELECT message_id FROM expired)`, j.TaskID)
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
		if (j.Parts[index].Type == "image" || j.Parts[index].Type == "video") && j.Parts[index].StorageKey != "" {
			j.Parts[index].Data, err = a.images.Read(ctx, j.Parts[index].StorageKey)
			if err != nil {
				return a.retryOutbox(ctx, j, fmt.Errorf("读取待发送图片失败: %w", err))
			}
		}
	}
	client := qq.NewClient(j.AppID, secret, a.cfg.QQAPIBaseURL, a.cfg.QQTokenURL)
	if j.EventType == "AI_RESPONSE" || j.EventType == "SCHEDULED_POST" {
		// Pending rows may have been created before reply normalization.
		if hasContentPart(j.Parts, "image") {
			j.Text, _ = extractOutboundImage(j.Text)
		}
		j.Text = agent.PlainLinks(j.Text)
	}
	format := ""
	if j.EventType == "SCHEDULED_POST" && hasContentPart(j.Parts, "image") {
		format = "image-only" // Also suppress captions on previously queued images.
	}
	if j.EventType == "AI_RESPONSE" && !hasContentPart(j.Parts, "image") && !hasContentPart(j.Parts, "video") {
		format = "markdown"
	}
	sendCtx := ctx
	cancelSend := func() {}
	if hasContentPart(j.Parts, "ark_ack") {
		sendCtx, cancelSend = context.WithTimeout(ctx, 5*time.Second)
		defer cancelSend()
	}
	replyTo := j.ReplyTo
	active := j.EventType == "SCHEDULED_POST" || (j.EventType == "PROACTIVE_CARE" && j.ReplyTo == "") || humanReplyNeedsActive(j, time.Now())
	if active {
		replyTo = ""
	}
	if j.EventType == "AI_RESPONSE" && j.ConversationType == "private" && j.Sequence > 1 {
		if svcSettings, _, _, e := a.conversationAgent(ctx, j.ConversationRecordID, a.agentSettings(ctx)); e == nil && svcSettings.Companion.Enabled && svcSettings.Companion.TypingIndicator {
			go a.sendPrivateTyping(ctx, j.BotID, j.ConversationID, j.ReplyTo, 3)
		}
	}
	if j.MaterialBatch && j.Text == "" {
		format = "image-only"
	}
	var mention, mentionName string
	var raw []byte
	if j.EventType == "AI_RESPONSE" && j.ConversationType == "group" && j.Sequence == 1 {
		if err := a.db.QueryRow(ctx, `SELECT sender_id,sender_name FROM messages WHERE conversation_id=$1 AND platform_message_id=$2 AND direction='inbound'`, j.ConversationRecordID, j.ReplyTo).Scan(&mention, &mentionName); err != nil {
			return a.retryOutbox(ctx, j, err)
		}
	}
	if (j.EventType == "HUMAN_REPLY" || j.EventType == "HANDOFF_UPDATE") && j.ReplyTo != "" && j.ConversationType == "group" {
		if err = a.db.QueryRow(ctx, "SELECT raw_event,COALESCE(NULLIF(NULLIF(sender_name,''),sender_id),(SELECT nullif(display_name,messages.sender_id) FROM conversation_members WHERE conversation_id=messages.conversation_id AND platform_user_id=messages.sender_id),'群成员') FROM messages WHERE conversation_id=$1 AND platform_message_id=$2", j.ConversationRecordID, j.ReplyTo).Scan(&raw, &mentionName); err != nil {
			return a.retryOutbox(ctx, j, err)
		}
	}
	reference := qq.ReferenceIndex(raw)
	if (j.EventType == "HUMAN_REPLY" || j.EventType == "HANDOFF_UPDATE") && reference == "" && mentionName != "" {
		j.Text = "@" + mentionName + " " + j.Text
	}

	if j.EventType == "HUMAN_REPLY" {
		j.Text = "【人工客服】" + j.Text
	}
	if j.EventType == "AI_RESPONSE" && j.ConversationType == "private" && j.Sequence == 1 {
		var own string
		err = a.db.QueryRow(ctx, `SELECT COALESCE((SELECT o.qq_ref_idx FROM messages o WHERE o.conversation_id=i.conversation_id AND o.direction='outbound' AND o.status='sent' AND NOT o.context_excluded AND o.qq_ref_idx<>'' AND (i.raw_event#>'{d,message_scene,ext}') ? ('ref_msg_idx='||o.qq_ref_idx) LIMIT 1),'') FROM messages i WHERE i.conversation_id=$1 AND i.platform_message_id=$2`, j.ConversationRecordID, j.ReplyTo).Scan(&own)
		if err != nil {
			return a.retryOutbox(ctx, j, err)
		}
		if own != "" {
			reference = own
		} else {
			if err = a.db.QueryRow(ctx, "SELECT qq_ref_idx FROM messages WHERE conversation_id=$1 AND platform_message_id=$2", j.ConversationRecordID, j.ReplyTo).Scan(&reference); err != nil {
				return a.retryOutbox(ctx, j, err)
			}
		}
	}
	if j.EventType == "AI_RESPONSE" && j.ConversationType == "private" {
		var selected string
		var inbound bool
		if err = a.db.QueryRow(ctx, `SELECT COALESCE(q.qq_ref_idx,''),coalesce(q.direction='inbound',false) FROM messages m LEFT JOIN messages q ON q.id=m.qq_quote_target AND q.conversation_id=m.conversation_id AND (q.status='sent' OR (q.direction='inbound' AND q.platform_message_id=m.reply_to_message_id)) AND NOT q.context_excluded WHERE m.id=$1`, j.MessageID).Scan(&selected, &inbound); err != nil {
			return a.retryOutbox(ctx, j, err)
		}
		if inbound || selected != "" {
			reference = selected
		}
	}
	// Linearize AI delivery with webhook arrival. Hold the conversation lock
	// from the final eligibility check through the QQ receipt commit.
	var tx pgx.Tx
	superseded := errors.New("reply superseded")
	if j.EventType == "AI_RESPONSE" {
		client.BeforeMessage = func(ctx context.Context) error {
			var err error
			tx, err = a.db.Begin(ctx)
			if err != nil {
				return err
			}
			if _, err = tx.Exec(ctx, "SELECT id FROM conversations WHERE id=$1 FOR NO KEY UPDATE", j.ConversationRecordID); err != nil {
				return err
			}
			var active bool
			if err = tx.QueryRow(ctx, "SELECT status='processing' FROM outbox_tasks WHERE id=$1", j.TaskID).Scan(&active); err != nil {
				return err
			}
			if !active {
				return superseded
			}
			return nil
		}
	}
	defer func() {
		if tx != nil {
			_ = tx.Rollback(ctx)
		}
	}()
	var receipt qq.Receipt
	if len(j.Payload) > 0 {
		var p map[string]any
		if err = json.Unmarshal(j.Payload, &p); err != nil {
			return a.retryOutbox(ctx, j, err)
		}
		if replyTo != "" {
			p["msg_id"] = replyTo
			p["msg_seq"] = j.Sequence
		}
		receipt, err = client.SendConfigured(sendCtx, qq.Prefix(j.ConversationType, j.ConversationID), p)
	} else {
		receipt, err = client.SendWithReceipt(sendCtx, domain.OutboundMessage{ID: j.MessageID, Channel: "qq", BotID: j.BotID, ConversationType: j.ConversationType, ConversationID: j.ConversationID, ReplyToMessageID: replyTo, MentionUserID: mention, MentionUserName: mentionName, ReferenceMessageID: reference, Text: j.Text, Format: format, Parts: j.Parts, ReplyDeadline: j.Deadline, Sequence: j.Sequence})
	}
	if err != nil {
		if tx != nil {
			_ = tx.Rollback(ctx)
		}
		if errors.Is(err, superseded) {
			return nil
		}
		if hasContentPart(j.Parts, "ark_ack") {
			a.log.Warn("QQ processing ARK failed", "bot_id", j.BotID, "conversation_type", j.ConversationType, "conversation_id", j.ConversationID, "error", err)
			_, _ = a.db.Exec(ctx, "UPDATE outbox_tasks SET status='failed',last_error=$1,updated_at=now() WHERE id=$2", err.Error(), j.TaskID)
			_, _ = a.db.Exec(ctx, "UPDATE messages SET status='failed' WHERE id=$1", j.MessageID)
			return nil
		}
		if active {
			err = fmt.Errorf("qq_active_send_failed: QQ 主动消息发送失败，请检查主动消息权限、接收设置和平台频率额度：%w", err)
		}
		return a.retryOutbox(ctx, j, err)
	}
	if tx == nil {
		tx, err = a.db.Begin(ctx)
		if err != nil {
			return err
		}
	}
	_, err = tx.Exec(ctx, "UPDATE outbox_tasks SET status='sent',platform_message_id=$1,updated_at=now() WHERE id=$2", receipt.ID, j.TaskID)
	if err == nil {
		_, err = tx.Exec(ctx, "UPDATE messages SET status='sent',platform_message_id=$1,qq_ref_idx=$3,qq_sent_at=$4,qq_delivered_text=CASE WHEN event_type='HUMAN_REPLY' THEN $5::text END WHERE id=$2", nullString(receipt.ID), j.MessageID, receipt.Ext.Index, parseQQTime(receipt.Timestamp), j.Text)
	}

	if err == nil && j.EventType == "AI_RESPONSE" {
		_, err = tx.Exec(ctx, scheduleProcessingAckRetractionSQL, j.BotID, j.ConversationRecordID, j.ReplyTo)
	}
	if err != nil {
		return err
	}
	return tx.Commit(ctx)
}

// Keep the persisted reply association for history and sequence accounting.
func humanReplyNeedsActive(j outboxJob, now time.Time) bool {
	limit := qqReplyLimit(j.ConversationType, j.Extended)
	return (j.MaterialBatch || j.EventType == "HUMAN_REPLY" || j.EventType == "HANDOFF_UPDATE") && (j.ReplyTo == "" || j.Sequence > limit || (j.Deadline != nil && !now.Before(*j.Deadline)))
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
	if status == "failed" && j.MaterialBatch {
		// Subsequent steps must not overtake a permanently failed step. The
		// existing retry action requeues failed rows, preserving their order.
		_, err := a.db.Exec(ctx, `WITH stopped AS (
 UPDATE outbox_tasks t SET status='failed',last_error=CASE WHEN m.id=first.id THEN $2 ELSE '前序素材发送失败，等待重试' END,updated_at=now()
 FROM messages first JOIN messages m ON m.material_batch=first.material_batch AND m.material_index>=first.material_index
 WHERE first.id=$1 AND t.message_id=m.id AND t.status IN ('processing','pending') RETURNING t.message_id)
 UPDATE messages SET status='failed' WHERE id IN (SELECT message_id FROM stopped)`, j.MessageID, cause.Error())
		return err
	}
	tag, err := a.db.Exec(ctx, "UPDATE outbox_tasks SET status=$1,last_error=$2,next_attempt_at=now()+interval '5 seconds',updated_at=now() WHERE id=$3 AND status='processing'", status, cause.Error(), j.TaskID)
	if err == nil && tag.RowsAffected() > 0 && status == "failed" {
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
