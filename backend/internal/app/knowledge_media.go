package app

import (
	"ai-bot/backend/internal/domain"
	"context"
	"github.com/gin-gonic/gin"
	"github.com/jackc/pgx/v5"
	"regexp"
	"strings"
)

var citationImage = regexp.MustCompile(`\[\[kb-image:([^\]\n]{1,100})\]\]`)

func selectedKnowledgeImage(answer string, hits []map[string]any) (string, string) {
	var selected string
	cleaned := citationImage.ReplaceAllStringFunc(answer, func(token string) string {
		id := strings.TrimSuffix(strings.TrimPrefix(token, "[[kb-image:"), "]]")
		if selected == "" {
			for _, h := range hits {
				if h["id"] == id && h["hasImage"] == true {
					selected = id
					break
				}
			}
		}
		return ""
	})
	return strings.TrimSpace(cleaned), selected
}

func (a *App) prepareKnowledgeImage(ctx context.Context, id string) (domain.ContentPart, error) {
	var mime string
	var data []byte
	err := a.db.QueryRow(ctx, "SELECT mime_type,data FROM knowledge_chunk_images WHERE chunk_id=$1", id).Scan(&mime, &data)
	if err != nil {
		return domain.ContentPart{}, err
	}
	name := "知识库配图" + imageExtension(mime)
	key, err := a.images.Save(ctx, name, data)
	return domain.ContentPart{Type: "image", ContentType: mime, Filename: name, StorageKey: key, SizeBytes: int64(len(data))}, err
}
func (a *App) knowledgeImage(c *gin.Context) {
	var mime string
	var data []byte
	err := a.db.QueryRow(c, `SELECT i.mime_type,i.data FROM knowledge_chunk_images i JOIN knowledge_chunks ch ON ch.id=i.chunk_id WHERE ch.knowledge_base_id=$1 AND ch.id=$2`, c.Param("id"), c.Param("chunkId")).Scan(&mime, &data)
	if err != nil {
		fail(c, 404, "not_found", "配图不存在")
		return
	}
	c.Header("Cache-Control", "private, no-store")
	c.Data(200, mime, data)
}

// The switch is explicit because re-indexing makes upstream model calls.
func (a *App) changeKnowledgeProfile(c *gin.Context) {
	var in struct {
		Profile string `json:"embeddingProfileId"`
	}
	if c.ShouldBindJSON(&in) != nil || in.Profile == "" {
		fail(c, 400, "invalid_request", "请选择向量模型配置")
		return
	}
	ctx := c.Request.Context()
	id := c.Param("id")
	tx, e := a.db.Begin(ctx)
	if e != nil {
		fail(c, 500, "database_error", "无法切换模型")
		return
	}
	defer tx.Rollback(ctx)
	var current *string
	if e = tx.QueryRow(ctx, "SELECT embedding_profile_id::text FROM knowledge_bases WHERE id=$1 FOR UPDATE", id).Scan(&current); e != nil {
		fail(c, 404, "not_found", "知识库不存在")
		return
	}
	if current != nil && *current == in.Profile {
		ok(c, gin.H{"changed": false})
		return
	}
	var model string
	if e = tx.QueryRow(ctx, "SELECT model FROM model_profiles WHERE id=$1 AND kind='embedding' AND enabled FOR SHARE", in.Profile).Scan(&model); e != nil {
		fail(c, 400, "invalid_embedding_profile", "Embedding 配置无效")
		return
	}
	// Synchronize with claims that were already underway before the KB lock.
	rows, e := tx.Query(ctx, "SELECT status FROM knowledge_documents WHERE knowledge_base_id=$1 FOR UPDATE", id)
	if e != nil {
		fail(c, 500, "database_error", "无法读取文档")
		return
	}
	states, e := pgx.CollectRows(rows, pgx.RowTo[string])
	if e != nil {
		fail(c, 500, "database_error", "无法读取文档")
		return
	}
	for _, st := range states {
		if st == "processing" {
			fail(c, 409, "document_processing", "请等待当前索引任务完成后再切换")
			return
		}
	}
	_, e = tx.Exec(ctx, "UPDATE knowledge_bases SET embedding_profile_id=$2,embedding_model=$3 WHERE id=$1", id, in.Profile, model)
	if e == nil {
		_, e = tx.Exec(ctx, "DELETE FROM knowledge_chunks WHERE knowledge_base_id=$1", id)
	}
	if e == nil {
		_, e = tx.Exec(ctx, "UPDATE knowledge_documents SET status='pending',attempts=0,next_attempt_at=now(),last_error=NULL,updated_at=now() WHERE knowledge_base_id=$1 AND status<>'unindexed'", id)
	}
	if e == nil {
		_, e = tx.Exec(ctx, `WITH entries AS (UPDATE knowledge_entries SET index_status='pending',index_error='' WHERE knowledge_base_id=$1 AND status='published' RETURNING id) INSERT INTO agent_jobs(kind,payload) SELECT 'index_entry',jsonb_build_object('entryId',id) FROM entries`, id)
	}
	if e != nil || tx.Commit(ctx) != nil {
		fail(c, 500, "database_error", "切换模型并重建索引失败")
		return
	}
	ok(c, gin.H{"changed": true})
}
