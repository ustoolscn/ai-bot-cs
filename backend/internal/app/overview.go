package app

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/gin-gonic/gin"
)

type overviewPipeline struct {
	Outcome      string          `json:"outcome"`
	TicketNumber *string         `json:"ticketNumber,omitempty"`
	Error        string          `json:"error,omitempty"`
	Sender       string          `json:"sender"`
	SenderID     string          `json:"senderId"`
	MentionNames json.RawMessage `json:"mentionNames"`
	Timeline     json.RawMessage `json:"timeline"`
	ID           string          `json:"id"`
	Time         time.Time       `json:"time"`
	Bot          string          `json:"bot"`
	Conversation string          `json:"conversation"`
	Content      string          `json:"content"`
	EventType    string          `json:"eventType"`
	EventMS      int64           `json:"eventMs"`
	ContextMS    int64           `json:"contextMs"`
	ContextLabel string          `json:"contextLabel"`
	ContextState string          `json:"contextStatus"`
	Retrieval    map[string]any  `json:"retrieval"`
	Model        map[string]any  `json:"model"`
	Delivery     map[string]any  `json:"delivery"`
	TotalMS      int64           `json:"totalMs"`
}

func (a *App) overview(c *gin.Context) {
	var bots, conversations, messages24h, totalEvents, triggered, successful, failedHandled, repliedCount, triggeredConversations int64
	var inboxPending, outboxPending, documentPending, documentProcessing, failedTasks int64
	var readyDocuments, totalDocuments, failedDocuments int64
	var averageLatencyMS float64
	var slowCalls, modelCalls, sentDeliveries, totalDeliveries int64

	_ = a.db.QueryRow(c, "SELECT count(*) FROM bots").Scan(&bots)
	_ = a.db.QueryRow(c, "SELECT count(*) FROM conversations").Scan(&conversations)
	_ = a.db.QueryRow(c, "SELECT count(*) FROM messages WHERE created_at>now()-interval '24 hours' AND event_type<>'PROCESSING_ACK'").Scan(&messages24h)
	_ = a.db.QueryRow(c, "SELECT count(*) FROM webhook_events WHERE created_at>now()-interval '24 hours'").Scan(&totalEvents)
	_ = a.db.QueryRow(c, `WITH recent_inbox AS (
		SELECT
			m.conversation_id,
			CASE
				WHEN t.status = 'merged' THEN false
				WHEN t.status = 'failed' OR ar.status = 'failed' OR coalesce(delivery.failed, false) THEN true
				ELSE false
			END AS is_failed,
			CASE
				WHEN t.status = 'merged' THEN false
				WHEN t.status = 'failed' OR ar.status = 'failed' OR coalesce(delivery.failed, false) THEN false
				WHEN t.status IN ('pending', 'processing') THEN true
				WHEN t.status = 'completed' AND coalesce(delivery.waiting, false) THEN true
				ELSE false
			END AS is_in_progress,
			coalesce(delivery.sent, false) AS has_sent
		FROM inbox_tasks t
		JOIN messages m ON m.id = t.message_id
		LEFT JOIN LATERAL (
			SELECT status FROM agent_runs WHERE message_id = m.id ORDER BY started_at DESC LIMIT 1
		) ar ON true
		LEFT JOIN LATERAL (
			SELECT
				bool_or(
					coalesce(ot.status, x.status) = 'failed'
					OR (
						coalesce(ot.status, x.status) = 'expired'
						AND coalesce(ot.last_error, '') NOT IN ('blacklisted or excluded', 'operator removed')
					)
				) AS failed,
				bool_or(coalesce(ot.status, x.status) IN ('pending', 'processing')) AS waiting,
				bool_or(coalesce(ot.status, x.status) = 'sent') AS sent
			FROM messages x
			LEFT JOIN outbox_tasks ot ON ot.message_id = x.id
			WHERE x.conversation_id = m.conversation_id
				AND x.direction = 'outbound'
				AND x.event_type <> 'PROCESSING_ACK'
				AND x.status <> 'merged'
				AND coalesce(ot.status, '') <> 'merged'
				AND coalesce(m.platform_message_id, '') <> ''
				AND x.reply_to_message_id = m.platform_message_id
		) delivery ON true
		WHERE t.created_at > now() - interval '24 hours'
	)
	SELECT
		count(*) FILTER (WHERE NOT is_in_progress),
		count(*) FILTER (WHERE NOT is_in_progress AND NOT is_failed),
		count(*) FILTER (WHERE NOT is_in_progress AND is_failed),
		count(*) FILTER (WHERE NOT is_in_progress AND NOT is_failed AND has_sent),
		count(DISTINCT conversation_id)
	FROM recent_inbox`).Scan(&triggered, &successful, &failedHandled, &repliedCount, &triggeredConversations)
	_ = a.db.QueryRow(c, `SELECT COALESCE(avg(EXTRACT(EPOCH FROM (completed_at-started_at))*1000),0) FROM agent_runs WHERE completed_at IS NOT NULL AND started_at>now()-interval '24 hours'`).Scan(&averageLatencyMS)
	_ = a.db.QueryRow(c, `SELECT count(*) FILTER(WHERE latency_ms>5000),count(*) FROM model_calls WHERE created_at>now()-interval '24 hours'`).Scan(&slowCalls, &modelCalls)
	_ = a.db.QueryRow(c, `SELECT count(*) FILTER(WHERE t.status='sent'),count(*) FILTER(WHERE t.status IN ('sent','failed','expired')) FROM outbox_tasks t JOIN messages m ON m.id=t.message_id WHERE t.created_at>now()-interval '24 hours' AND m.event_type<>'PROCESSING_ACK'`).Scan(&sentDeliveries, &totalDeliveries)
	_ = a.db.QueryRow(c, "SELECT count(*) FROM inbox_tasks WHERE status IN ('pending','processing')").Scan(&inboxPending)
	_ = a.db.QueryRow(c, `SELECT count(*) FROM outbox_tasks t JOIN messages m ON m.id=t.message_id WHERE t.status IN ('pending','processing') AND m.event_type<>'PROCESSING_ACK'`).Scan(&outboxPending)
	_ = a.db.QueryRow(c, "SELECT count(*) FROM knowledge_documents WHERE status='pending'").Scan(&documentPending)
	_ = a.db.QueryRow(c, "SELECT count(*) FROM knowledge_documents WHERE status='processing'").Scan(&documentProcessing)
	_ = a.db.QueryRow(c, "SELECT count(*) FROM knowledge_documents WHERE status='ready'").Scan(&readyDocuments)
	_ = a.db.QueryRow(c, "SELECT count(*) FROM knowledge_documents WHERE status='failed'").Scan(&failedDocuments)
	_ = a.db.QueryRow(c, "SELECT count(*) FROM knowledge_documents").Scan(&totalDocuments)
	_ = a.db.QueryRow(c, `SELECT (SELECT count(*) FROM inbox_tasks WHERE status='failed' AND dismissed_at IS NULL)+(SELECT count(*) FROM outbox_tasks t JOIN messages m ON m.id=t.message_id WHERE (t.status='failed' OR (t.status='expired' AND coalesce(t.last_error,'') NOT IN ('blacklisted or excluded','operator removed'))) AND t.dismissed_at IS NULL AND m.event_type<>'PROCESSING_ACK')+(SELECT count(*) FROM knowledge_documents WHERE status='failed' AND dismissed_at IS NULL)`).Scan(&failedTasks)

	var active, stages json.RawMessage
	err := a.db.QueryRow(c, `SELECT coalesce(jsonb_agg(to_jsonb(q)),'[]') FROM (
 SELECT t.id::text AS "taskId",m.id::text AS id,round(extract(epoch FROM now()-t.locked_at)*1000)::bigint AS "elapsedMs",b.id::text AS "botId",b.name AS bot,c.id::text AS "conversationId",coalesce(nullif(c.name,''),c.platform_id) AS conversation,m.sender_id AS "userId",coalesce(nullif(m.sender_name,''),m.sender_id) AS sender,m.content AS question,message_mention_names(m,m.content) AS "mentionNames",coalesce((SELECT label FROM message_stages WHERE message_id=m.id ORDER BY id DESC LIMIT 1),'接单检查') AS stage
 FROM inbox_tasks t JOIN messages m ON m.id=t.message_id JOIN conversations c ON c.id=m.conversation_id JOIN bots b ON b.id=m.bot_id WHERE t.status='processing' ORDER BY t.locked_at) q`).Scan(&active)
	if err != nil {
		fail(c, 500, "database_error", "读取处理中任务失败")
		return
	}
	err = a.db.QueryRow(c, `SELECT coalesce(jsonb_agg(to_jsonb(q) ORDER BY q."startOffsetMs",q.label),'[]') FROM (SELECT label,avg(offset_ms)::float8 AS "startOffsetMs",round(avg(ms))::bigint AS "averageMs",count(*) AS samples FROM (
 SELECT label,extract(epoch FROM s.started_at-m.created_at)*1000 AS offset_ms,extract(epoch FROM finished_at-started_at)*1000 AS ms FROM message_stages s JOIN messages m ON m.id=s.message_id WHERE finished_at>=now()-interval '24 hours'
 UNION ALL SELECT '等待处理',extract(epoch FROM t.created_at-m.created_at)*1000,extract(epoch FROM locked_at-t.created_at)*1000 FROM inbox_tasks t JOIN messages m ON m.id=t.message_id WHERE locked_at>=now()-interval '24 hours'
 UNION ALL SELECT delays.label,extract(epoch FROM delays.started-i.created_at)*1000,extract(epoch FROM delays.duration)*1000 FROM outbox_tasks t JOIN messages m ON m.id=t.message_id
 JOIN messages i ON i.conversation_id=m.conversation_id AND i.platform_message_id=m.reply_to_message_id AND i.direction='inbound'
 CROSS JOIN LATERAL (VALUES ('等待投递',t.created_at,t.locked_at-t.created_at),('QQ 投递',t.locked_at,t.updated_at-t.locked_at)) AS delays(label,started,duration)
 WHERE m.event_type='AI_RESPONSE' AND t.status IN ('sent','failed','expired') AND t.updated_at>=now()-interval '24 hours' AND t.locked_at IS NOT NULL
 ) timings GROUP BY label) q`).Scan(&stages)
	if err != nil {
		fail(c, 500, "database_error", "读取环节耗时失败")
		return
	}
	var entries, indexed, queued, processing int64
	err = a.db.QueryRow(c, `SELECT count(*),count(*) FILTER(WHERE index_status='ready') FROM knowledge_entries WHERE status='published'`).Scan(&entries, &indexed)
	if err != nil {
		fail(c, 500, "database_error", "读取知识索引失败")
		return
	}
	err = a.db.QueryRow(c, `SELECT count(*) FILTER(WHERE status='pending'),count(*) FILTER(WHERE status='processing') FROM (SELECT status FROM inbox_tasks UNION ALL SELECT t.status FROM outbox_tasks t JOIN messages m ON m.id=t.message_id WHERE m.event_type<>'PROCESSING_ACK') tasks`).Scan(&queued, &processing)
	if err != nil {
		fail(c, 500, "database_error", "读取队列失败")
		return
	}
	var ratio float64
	if totalDocuments+entries > 0 {
		ratio = float64(readyDocuments+indexed) / float64(totalDocuments+entries)
	}
	pipelines := a.overviewPipelines(c)
	alerts := a.overviewAlerts(c)
	knowledgeQueues := a.overviewKnowledgeQueues(c)

	ok(c, gin.H{
		"activeTasks": active, "stageAverages": stages, "queuedTasks": queued, "processingTasks": processing, "knowledge": gin.H{"documentCount": totalDocuments, "entryCount": entries, "indexedCount": readyDocuments + indexed, "indexedRatio": ratio},
		"bots": bots, "conversations": conversations, "messages24h": messages24h,
		"totalEvents": totalEvents, "triggered": triggered, "successful": successful, "failedHandled": failedHandled, "repliedCount": repliedCount,
		"triggeredConversations": triggeredConversations, "averageLatencyMs": int64(averageLatencyMS),
		"slowCalls": slowCalls, "modelCalls": modelCalls, "sentDeliveries": sentDeliveries, "totalDeliveries": totalDeliveries,
		"pendingTasks": inboxPending + outboxPending, "pendingInbox": inboxPending, "pendingOutbox": outboxPending,
		"pendingDocuments": documentPending, "processingDocuments": documentProcessing, "failedTasks": failedTasks,
		"readyDocuments": readyDocuments, "failedDocuments": failedDocuments, "totalDocuments": totalDocuments,
		"pipelines": pipelines, "alerts": alerts, "knowledgeQueues": knowledgeQueues, "version": "0.2.0",
	})
}

func (a *App) overviewPipelines(c *gin.Context) []overviewPipeline {
	rows, err := a.db.Query(c, `
		SELECT CASE WHEN h.id IS NOT NULL THEN 'handoff'
 WHEN delivery.failed OR t.status='failed' THEN 'failed'
 WHEN t.status='merged' THEN 'merged'
 WHEN delivery.waiting OR t.status IN ('processing','pending') OR gr.status='pending' THEN 'processing'
 WHEN delivery.sent THEN 'replied'
 WHEN ar.status='failed' THEN 'failed'
 ELSE 'skipped' END,h.ticket_number::text,
 left(coalesce(nullif(delivery.error,''),nullif(t.last_error,''),nullif(ar.error,''),nullif(mc.error,''),'消息处理失败'),120),coalesce(nullif(m.sender_name,''),nullif(cm.display_name,''),cm.qq_username,''),m.sender_id,message_mention_names(m,m.content),message_timeline(m),m.id::text,m.event_at,b.name,c.name,left(m.content,160),m.event_type,
		       GREATEST(EXTRACT(EPOCH FROM (t.created_at-m.created_at))*1000,0)::bigint,
		       COALESCE(ar.context_latency_ms,0),COALESCE(ar.retrieval_latency_ms,0),
		       CASE WHEN jsonb_typeof(ar.retrieved_chunks)='array' THEN jsonb_array_length(ar.retrieved_chunks) ELSE 0 END,
		       COALESCE(t.status,'skipped'),COALESCE(ar.status,'pending'),
		       COALESCE(mc.latency_ms,0),COALESCE(mp.name,mp.model,''),COALESCE(mc.error,''),
		       COALESCE(ot.status,'pending'),
		       COALESCE(GREATEST(EXTRACT(EPOCH FROM (ot.updated_at-ot.created_at))*1000,0),0)::bigint,
		       COALESCE(GREATEST(EXTRACT(EPOCH FROM (COALESCE(ot.updated_at,ar.completed_at,t.updated_at)-m.created_at))*1000,0),0)::bigint
		FROM messages m
 LEFT JOIN group_reviews gr ON gr.message_id=m.id LEFT JOIN handoffs h ON h.id=m.handoff_id LEFT JOIN conversation_members cm ON cm.conversation_id=m.conversation_id AND cm.platform_user_id=m.sender_id
 LEFT JOIN inbox_tasks t ON t.message_id=m.id
		JOIN conversations c ON c.id=m.conversation_id
		JOIN bots b ON b.id=m.bot_id
		LEFT JOIN LATERAL (SELECT * FROM agent_runs WHERE message_id=m.id ORDER BY started_at DESC LIMIT 1) ar ON true
		LEFT JOIN LATERAL (SELECT * FROM model_calls WHERE agent_run_id=ar.id ORDER BY created_at DESC LIMIT 1) mc ON true
		LEFT JOIN model_profiles mp ON mp.id=mc.profile_id
		LEFT JOIN LATERAL (SELECT * FROM messages WHERE conversation_id=m.conversation_id AND direction='outbound' AND event_type<>'PROCESSING_ACK' AND reply_to_message_id=m.platform_message_id ORDER BY created_at DESC LIMIT 1) om ON true
		LEFT JOIN outbox_tasks ot ON ot.message_id=om.id
		LEFT JOIN LATERAL (
 SELECT bool_or(coalesce(task.status,x.status)='failed' OR (coalesce(task.status,x.status)='expired' AND coalesce(task.last_error,'') NOT IN ('blacklisted or excluded','operator removed'))) AS failed,
 bool_or(coalesce(task.status,x.status) IN ('pending','processing')) AS waiting,
 bool_or(coalesce(task.status,x.status)='sent') AS sent,
 max(task.last_error) FILTER(WHERE coalesce(task.status,x.status)='failed' OR (coalesce(task.status,x.status)='expired' AND coalesce(task.last_error,'') NOT IN ('blacklisted or excluded','operator removed'))) AS error
 FROM messages x LEFT JOIN outbox_tasks task ON task.message_id=x.id WHERE x.conversation_id=m.conversation_id
 AND x.direction='outbound' AND x.event_type<>'PROCESSING_ACK' AND x.status<>'merged' AND coalesce(task.status,'')<>'merged' AND coalesce(m.platform_message_id,'')<>'' AND x.reply_to_message_id=m.platform_message_id) delivery ON true
 WHERE t.id IS NOT NULL OR gr.message_id IS NOT NULL
 ORDER BY m.event_at DESC LIMIT 10`)
	if err != nil {
		a.log.Warn("overview pipelines", "error", err)
		return []overviewPipeline{}
	}
	defer rows.Close()
	out := make([]overviewPipeline, 0, 10)
	for rows.Next() {
		var p overviewPipeline
		var contextMS, retrievalMS, hits, modelMS, deliveryMS int64
		var taskStatus, runStatus, modelName, modelError, deliveryStatus string
		if err := rows.Scan(&p.Outcome, &p.TicketNumber, &p.Error, &p.Sender, &p.SenderID, &p.MentionNames, &p.Timeline, &p.ID, &p.Time, &p.Bot, &p.Conversation, &p.Content, &p.EventType, &p.EventMS, &contextMS, &retrievalMS, &hits, &taskStatus, &runStatus, &modelMS, &modelName, &modelError, &deliveryStatus, &deliveryMS, &p.TotalMS); err != nil {
			a.log.Warn("scan overview pipeline", "error", err)
			continue
		}
		if p.Outcome != "failed" {
			p.Error = ""
		}
		p.ContextMS = contextMS
		switch {
		case taskStatus == "failed":
			p.ContextLabel, p.ContextState = "失败", "failed"
		case taskStatus == "skipped":
			p.ContextLabel, p.ContextState = "已跳过", "warning"
		case runStatus == "failed":
			p.ContextLabel, p.ContextState = "失败", "failed"
		case runStatus == "pending":
			p.ContextLabel, p.ContextState = "排队", "pending"
		default:
			p.ContextLabel, p.ContextState = "构建", "success"
		}
		retrievalStatus := "success"
		if runStatus == "pending" {
			retrievalStatus = "pending"
		} else if runStatus == "failed" && retrievalMS == 0 {
			retrievalStatus = "failed"
		} else if retrievalMS > 5000 {
			retrievalStatus = "warning"
		}
		modelStatus := "pending"
		if modelError != "" {
			modelStatus = "failed"
		} else if modelName != "" {
			modelStatus = "success"
		}
		p.Retrieval = map[string]any{"status": retrievalStatus, "ms": retrievalMS, "hit": fmt.Sprintf("%d", hits)}
		p.Model = map[string]any{"status": modelStatus, "ms": modelMS, "name": modelName}
		p.Delivery = map[string]any{"status": deliveryOverviewStatus(deliveryStatus), "ms": deliveryMS}
		out = append(out, p)
	}
	return out
}

func deliveryOverviewStatus(status string) string {
	switch status {
	case "sent":
		return "success"
	case "failed", "expired":
		return "failed"
	case "processing":
		return "processing"
	default:
		return "pending"
	}
}

func (a *App) overviewAlerts(c *gin.Context) []map[string]any {
	rows, err := a.db.Query(c, `SELECT failures.*,CASE
 WHEN detail ~* 'deadline exceeded|timeout|timed out' THEN '等待服务响应超时'
 WHEN detail ~* 'rate limit|too many requests|http 429' THEN '服务请求过于频繁'
 WHEN detail ~* 'unauthorized|forbidden|permission denied' THEN '服务拒绝访问，请检查凭据和权限'
 ELSE NULL END AS reason FROM (
 SELECT t.id::text,t.updated_at AS time,b.name AS bot,'inbox' AS kind,m.id::text AS "messageId",NULL::text AS "documentId",NULL::text AS "knowledgeBaseId",
 '处理用户消息失败：'||left(display_mentions(m,m.content),80) AS content,t.last_error AS detail,'消息未生成回复' AS impact,'critical' AS level,false AS recovered
 FROM inbox_tasks t JOIN messages m ON m.id=t.message_id JOIN bots b ON b.id=m.bot_id WHERE t.status='failed' AND t.dismissed_at IS NULL
 UNION ALL SELECT t.id::text,t.updated_at,b.name,'outbox',i.id::text,NULL,NULL,
 CASE WHEN t.status='expired' THEN '回复已超过发送时限：' ELSE '发送 QQ 消息失败：' END||left(display_mentions(m,m.content),80),t.last_error,'回复未送达','critical',false
 FROM outbox_tasks t JOIN messages m ON m.id=t.message_id JOIN bots b ON b.id=m.bot_id
 LEFT JOIN messages i ON i.conversation_id=m.conversation_id AND i.direction='inbound' AND i.platform_message_id=m.reply_to_message_id
 WHERE (t.status='failed' OR (t.status='expired' AND coalesce(t.last_error,'') NOT IN ('blacklisted or excluded','operator removed'))) AND t.dismissed_at IS NULL AND m.event_type<>'PROCESSING_ACK'
 UNION ALL SELECT d.id::text,d.updated_at,'知识索引','document',NULL,d.id::text,d.knowledge_base_id::text,
 '文档索引失败：'||d.name,d.last_error,'知识检索可能不完整','warning',false
 FROM knowledge_documents d WHERE d.status='failed' AND d.dismissed_at IS NULL) failures ORDER BY time DESC LIMIT 8`)
	if err == nil {
		var items []map[string]any
		items, err = rowsJSON(rows)
		if err == nil {
			for _, item := range items {
				if reason, ok := item["reason"].(string); ok {
					item["content"] = fmt.Sprintf("%s（%s）", item["content"], reason)
				}
				delete(item, "reason")
			}
			return items
		}
	}
	a.log.Warn("overview alerts", "error", err)
	return []map[string]any{}
}

func (a *App) overviewKnowledgeQueues(c *gin.Context) []map[string]any {
	rows, err := a.db.Query(c, `SELECT k.id::text,k.name,count(d.id),count(d.id) FILTER(WHERE d.status='ready'),count(d.id) FILTER(WHERE d.status='processing'),count(d.id) FILTER(WHERE d.status='pending') FROM knowledge_bases k JOIN knowledge_documents d ON d.knowledge_base_id=k.id GROUP BY k.id,k.name HAVING count(d.id) FILTER(WHERE d.status IN ('pending','processing'))>0 ORDER BY min(d.created_at) LIMIT 8`)
	if err != nil {
		return []map[string]any{}
	}
	defer rows.Close()
	out := make([]map[string]any, 0, 8)
	for rows.Next() {
		var id, name string
		var total, ready, processing, pending int64
		if rows.Scan(&id, &name, &total, &ready, &processing, &pending) == nil {
			progress := 0
			if total > 0 {
				progress = int(ready * 100 / total)
			}
			eta := "等待处理"
			if processing > 0 {
				eta = "正在索引"
			}
			out = append(out, map[string]any{"id": id, "name": name, "progress": progress, "eta": eta, "pending": pending, "processing": processing})
		}
	}
	return out
}
