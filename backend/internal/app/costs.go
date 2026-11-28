package app

import (
	"context"
	"encoding/json"
	"time"

	"ai-bot/backend/internal/domain"
	"ai-bot/backend/internal/openai"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

type usageScopeKey struct{}
type usageScope struct{ run, message, conversation string }

func (a *App) embed(ctx context.Context, cl *openai.Client, profile, kind string, inputs []string) ([][]float32, error) {
	start := time.Now()
	embed := cl.EmbedWithUsage
	if cl.Provider == "google" {
		embed = func(ctx context.Context, texts []string) ([][]float32, domain.ChatResult, error) {
			return cl.GoogleTexts(ctx, texts, kind == "embedding_index")
		}
	}
	vectors, usage, err := embed(ctx, inputs)
	a.recordModelCall(ctx, "", "", profile, kind, usage, time.Since(start).Milliseconds(), err)
	return vectors, err
}

func (a *App) costs(c *gin.Context) {
	var days int
	switch c.DefaultQuery("range", "all") {
	case "today":
		days = 1
	case "7d":
		days = 7
	case "30d":
		days = 30
	case "all":
	default:
		fail(c, 400, "invalid_range", "统计范围无效")
		return
	}
	var result json.RawMessage
	err := a.db.QueryRow(c, `
WITH bounds AS (
 SELECT date_trunc('day',now(),'Asia/Shanghai') AS midnight
), selected AS (
 SELECT p.*,coalesce(c.id::text, fallback.id) AS cid,coalesce(nullif(c.name,''),c.platform_id,fallback.name) AS cname,
        coalesce(b.id::text,fallback.id) AS bid,coalesce(b.name,fallback.name) AS bname
 FROM priced_model_calls p
 LEFT JOIN messages m ON m.id=p.message_id
 LEFT JOIN conversations c ON c.id=coalesce(p.conversation_id,m.conversation_id)
 LEFT JOIN bots b ON b.id=c.bot_id
 CROSS JOIN LATERAL (SELECT CASE WHEN p.kind='embedding_index' THEN 'knowledge-index' ELSE 'system' END AS id,
                            CASE WHEN p.kind='embedding_index' THEN '知识库索引' ELSE '系统任务' END AS name) fallback
 WHERE $1::int=0 OR p.created_at >= (SELECT midnight FROM bounds)-($1-1)*interval '24 hours'
), kinds AS (
 SELECT k.kind,coalesce(sum(s.cost),0) AS cost,coalesce(sum(input_tokens),0) AS "inputTokens",
 coalesce(sum(output_tokens),0) AS "outputTokens",coalesce(sum(images),0) AS images
 FROM (VALUES ('chat'),('embedding'),('image')) k(kind) LEFT JOIN selected s ON s.billing_kind=k.kind GROUP BY k.kind
), conversations_used AS (
 SELECT cid AS id,cname AS name,sum(cost) AS cost FROM selected GROUP BY cid,cname
), bots_used AS (
 SELECT bid AS id,bname AS name,sum(cost) AS cost FROM selected GROUP BY bid,bname
)
SELECT jsonb_build_object(
 'today',coalesce(sum(cost) FILTER(WHERE created_at >= (SELECT midnight FROM bounds)),0),'total',coalesce(sum(cost),0),
 'byKind',(SELECT jsonb_agg(k ORDER BY kind) FROM kinds k),
 'byConversation',coalesce((SELECT jsonb_agg(g ORDER BY cost DESC,id) FROM conversations_used g),'[]'),
 'byBot',coalesce((SELECT jsonb_agg(b ORDER BY cost DESC,id) FROM bots_used b),'[]'),
 'unpricedModels',coalesce((SELECT jsonb_agg(DISTINCT profile_name ORDER BY profile_name) FROM selected WHERE unpriced),'[]')
) FROM priced_model_calls`, days).Scan(&result)
	if err != nil {
		a.log.Error("read usage costs", "error", err)
		fail(c, 500, "database_error", "读取消费统计失败")
		return
	}
	ok(c, result)
}

// Both routes operate on the same visible failures as the dashboard.
func (a *App) dismissFailed(c *gin.Context) {
	id := c.Param("id")
	if id != "" && uuid.Validate(id) != nil {
		fail(c, 400, "invalid_id", "任务 ID 无效")
		return
	}
	var count int64
	err := a.db.QueryRow(c, `WITH inbox AS (
 UPDATE inbox_tasks SET dismissed_at=now() WHERE status='failed' AND dismissed_at IS NULL AND ($1='' OR id=nullif($1,'')::uuid) RETURNING id
), outbox AS (
 UPDATE outbox_tasks t SET dismissed_at=now() FROM messages m WHERE t.message_id=m.id AND m.event_type<>'PROCESSING_ACK' AND (t.status='failed' OR (t.status='expired' AND coalesce(t.last_error,'') NOT IN ('blacklisted or excluded','operator removed')))
 AND t.dismissed_at IS NULL AND ($1='' OR t.id=nullif($1,'')::uuid) RETURNING t.id
), docs AS (
 UPDATE knowledge_documents SET dismissed_at=now() WHERE status='failed' AND dismissed_at IS NULL AND ($1='' OR id=nullif($1,'')::uuid) RETURNING id
) SELECT (SELECT count(*) FROM inbox)+(SELECT count(*) FROM outbox)+(SELECT count(*) FROM docs)`, id).Scan(&count)
	if err != nil {
		fail(c, 500, "database_error", "忽略失败任务失败")
		return
	}
	ok(c, gin.H{"dismissed": count})
}
