package app

import (
	"fmt"
	"time"

	"github.com/gin-gonic/gin"
)

type overviewPipeline struct {
	ID           string         `json:"id"`
	Time         time.Time      `json:"time"`
	Bot          string         `json:"bot"`
	Conversation string         `json:"conversation"`
	Content      string         `json:"content"`
	EventMS      int64          `json:"eventMs"`
	ContextMS    int64          `json:"contextMs"`
	Retrieval    map[string]any `json:"retrieval"`
	Model        map[string]any `json:"model"`
	Delivery     map[string]any `json:"delivery"`
	TotalMS      int64          `json:"totalMs"`
}

func (a *App) overview(c *gin.Context) {
	var bots, conversations, messages24h, totalEvents, triggered, successful, triggeredConversations int64
	var inboxPending, outboxPending, documentPending, documentProcessing, failedTasks int64
	var readyDocuments, totalDocuments, failedDocuments int64
	var averageLatencyMS float64
	var slowCalls, modelCalls, sentDeliveries, totalDeliveries int64

	_ = a.db.QueryRow(c, "SELECT count(*) FROM bots").Scan(&bots)
	_ = a.db.QueryRow(c, "SELECT count(*) FROM conversations").Scan(&conversations)
	_ = a.db.QueryRow(c, "SELECT count(*) FROM messages WHERE created_at>now()-interval '24 hours'").Scan(&messages24h)
	_ = a.db.QueryRow(c, "SELECT count(*) FROM webhook_events WHERE created_at>now()-interval '24 hours'").Scan(&totalEvents)
	_ = a.db.QueryRow(c, `SELECT count(*),count(*) FILTER(WHERE t.status='completed'),count(DISTINCT m.conversation_id) FROM inbox_tasks t JOIN messages m ON m.id=t.message_id WHERE t.created_at>now()-interval '24 hours'`).Scan(&triggered, &successful, &triggeredConversations)
	_ = a.db.QueryRow(c, `SELECT COALESCE(avg(EXTRACT(EPOCH FROM (completed_at-started_at))*1000),0) FROM agent_runs WHERE completed_at IS NOT NULL AND started_at>now()-interval '24 hours'`).Scan(&averageLatencyMS)
	_ = a.db.QueryRow(c, `SELECT count(*) FILTER(WHERE latency_ms>5000),count(*) FROM model_calls WHERE created_at>now()-interval '24 hours'`).Scan(&slowCalls, &modelCalls)
	_ = a.db.QueryRow(c, `SELECT count(*) FILTER(WHERE status='sent'),count(*) FILTER(WHERE status IN ('sent','failed','expired')) FROM outbox_tasks WHERE created_at>now()-interval '24 hours'`).Scan(&sentDeliveries, &totalDeliveries)
	_ = a.db.QueryRow(c, "SELECT count(*) FROM inbox_tasks WHERE status IN ('pending','processing')").Scan(&inboxPending)
	_ = a.db.QueryRow(c, "SELECT count(*) FROM outbox_tasks WHERE status IN ('pending','processing')").Scan(&outboxPending)
	_ = a.db.QueryRow(c, "SELECT count(*) FROM knowledge_documents WHERE status='pending'").Scan(&documentPending)
	_ = a.db.QueryRow(c, "SELECT count(*) FROM knowledge_documents WHERE status='processing'").Scan(&documentProcessing)
	_ = a.db.QueryRow(c, "SELECT count(*) FROM knowledge_documents WHERE status='ready'").Scan(&readyDocuments)
	_ = a.db.QueryRow(c, "SELECT count(*) FROM knowledge_documents WHERE status='failed'").Scan(&failedDocuments)
	_ = a.db.QueryRow(c, "SELECT count(*) FROM knowledge_documents").Scan(&totalDocuments)
	_ = a.db.QueryRow(c, `SELECT (SELECT count(*) FROM inbox_tasks WHERE status='failed')+(SELECT count(*) FROM outbox_tasks WHERE status='failed')+(SELECT count(*) FROM knowledge_documents WHERE status='failed')`).Scan(&failedTasks)

	pipelines := a.overviewPipelines(c)
	alerts := a.overviewAlerts(c)
	knowledgeQueues := a.overviewKnowledgeQueues(c)

	ok(c, gin.H{
		"bots": bots, "conversations": conversations, "messages24h": messages24h,
		"totalEvents": totalEvents, "triggered": triggered, "successful": successful,
		"triggeredConversations": triggeredConversations, "averageLatencyMs": int64(averageLatencyMS),
		"slowCalls": slowCalls, "modelCalls": modelCalls, "sentDeliveries": sentDeliveries, "totalDeliveries": totalDeliveries,
		"pendingTasks": inboxPending + outboxPending, "pendingInbox": inboxPending, "pendingOutbox": outboxPending,
		"pendingDocuments": documentPending, "processingDocuments": documentProcessing, "failedTasks": failedTasks,
		"readyDocuments": readyDocuments, "failedDocuments": failedDocuments, "totalDocuments": totalDocuments,
		"pipelines": pipelines, "alerts": alerts, "knowledgeQueues": knowledgeQueues, "version": "0.2.0",
	})
}

func (a *App) overviewPipelines(c *gin.Context) []overviewPipeline {
	rows, err := a.db.Query(c, `SELECT m.id::text,m.event_at,b.name,c.name,left(m.content,160),GREATEST(EXTRACT(EPOCH FROM (t.created_at-m.created_at))*1000,0)::bigint,COALESCE(ar.context_latency_ms,0),COALESCE(ar.retrieval_latency_ms,0),COALESCE(jsonb_array_length(ar.retrieved_chunks),0),COALESCE(ar.status,'pending'),COALESCE(mc.latency_ms,0),COALESCE(mp.name,mp.model,''),COALESCE(mc.error,''),COALESCE(ot.status,'pending'),COALESCE(GREATEST(EXTRACT(EPOCH FROM (ot.updated_at-ot.created_at))*1000,0),0)::bigint,COALESCE(GREATEST(EXTRACT(EPOCH FROM (COALESCE(ot.updated_at,ar.completed_at,now())-m.created_at))*1000,0),0)::bigint FROM inbox_tasks t JOIN messages m ON m.id=t.message_id JOIN conversations c ON c.id=m.conversation_id JOIN bots b ON b.id=m.bot_id LEFT JOIN LATERAL (SELECT * FROM agent_runs WHERE message_id=m.id ORDER BY started_at DESC LIMIT 1) ar ON true LEFT JOIN LATERAL (SELECT * FROM model_calls WHERE agent_run_id=ar.id ORDER BY created_at DESC LIMIT 1) mc ON true LEFT JOIN model_profiles mp ON mp.id=mc.profile_id LEFT JOIN LATERAL (SELECT * FROM messages WHERE conversation_id=m.conversation_id AND direction='outbound' AND reply_to_message_id=m.platform_message_id ORDER BY created_at DESC LIMIT 1) om ON true LEFT JOIN outbox_tasks ot ON ot.message_id=om.id ORDER BY m.event_at DESC LIMIT 10`)
	if err != nil {
		a.log.Warn("overview pipelines", "error", err)
		return []overviewPipeline{}
	}
	defer rows.Close()
	out := make([]overviewPipeline, 0, 10)
	for rows.Next() {
		var p overviewPipeline
		var contextMS, retrievalMS, hits, modelMS, deliveryMS int64
		var runStatus, modelName, modelError, deliveryStatus string
		if err := rows.Scan(&p.ID, &p.Time, &p.Bot, &p.Conversation, &p.Content, &p.EventMS, &contextMS, &retrievalMS, &hits, &runStatus, &modelMS, &modelName, &modelError, &deliveryStatus, &deliveryMS, &p.TotalMS); err != nil {
			continue
		}
		p.ContextMS = contextMS
		retrievalStatus := "success"
		if runStatus == "pending" {
			retrievalStatus = "pending"
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
	rows, err := a.db.Query(c, `SELECT id,time,bot,content,impact FROM (SELECT t.id::text AS id,t.updated_at AS time,b.name AS bot,'入口处理失败：'||COALESCE(t.last_error,'未知错误') AS content,'消息未生成回复' AS impact FROM inbox_tasks t JOIN messages m ON m.id=t.message_id JOIN bots b ON b.id=m.bot_id WHERE t.status='failed' UNION ALL SELECT t.id::text,t.updated_at,b.name,'QQ 投递失败：'||COALESCE(t.last_error,'未知错误'),'回复未送达' FROM outbox_tasks t JOIN messages m ON m.id=t.message_id JOIN bots b ON b.id=m.bot_id WHERE t.status IN ('failed','expired') UNION ALL SELECT d.id::text,d.updated_at,'知识索引',d.name||'：'||COALESCE(d.last_error,'索引失败'),'知识检索可能不完整' FROM knowledge_documents d WHERE d.status='failed') failures ORDER BY time DESC LIMIT 8`)
	if err != nil {
		return []map[string]any{}
	}
	defer rows.Close()
	out := make([]map[string]any, 0, 8)
	for rows.Next() {
		var id, bot, content, impact string
		var at time.Time
		if rows.Scan(&id, &at, &bot, &content, &impact) == nil {
			level := "warning"
			if bot != "知识索引" {
				level = "critical"
			}
			out = append(out, map[string]any{"id": id, "level": level, "time": at, "bot": bot, "content": content, "impact": impact, "recovered": false})
		}
	}
	return out
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
