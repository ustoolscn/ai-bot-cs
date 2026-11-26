package app

import (
	"context"
	"errors"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

func (a *App) deleteKB(c *gin.Context) {
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		fail(c, 400, "invalid_id", "知识库编号无效")
		return
	}
	tx, err := a.db.Begin(c)
	if err != nil {
		fail(c, 500, "database_error", "无法开始删除知识库，请稍后重试")
		return
	}
	defer tx.Rollback(c)
	var locked int
	if err = tx.QueryRow(c, "SELECT 1 FROM knowledge_bases WHERE id=$1 FOR UPDATE", id).Scan(&locked); errors.Is(err, pgx.ErrNoRows) {
		fail(c, 404, "not_found", "知识库不存在")
		return
	} else if err != nil {
		fail(c, 500, "database_error", "读取待删除的知识库失败")
		return
	}
	for _, step := range []struct{ sql, reason string }{
		{"INSERT INTO deleted_knowledge_files SELECT knowledge_base_id,storage_key FROM knowledge_documents WHERE knowledge_base_id=$1 ON CONFLICT DO NOTHING", "登记待清理的文档原件失败"},
		{"UPDATE bot_services SET settings=detach_knowledge_base(settings,$1::text) WHERE settings->'knowledgeBaseIds' ? $1::text OR settings->>'learningKnowledgeBaseId'=$1::text", "解除智能体绑定失败"},
		{"UPDATE system_settings SET value=detach_knowledge_base(value,$1::text) WHERE key='agent_settings' AND (value->'knowledgeBaseIds' ? $1::text OR value->>'learningKnowledgeBaseId'=$1::text)", "解除默认智能体配置绑定失败"},
		{"DELETE FROM knowledge_gaps WHERE knowledge_base_id=$1", "删除未解答记录失败"},
		{"DELETE FROM knowledge_candidates WHERE knowledge_base_id=$1", "删除知识候选失败"},
		{"DELETE FROM eval_cases v USING knowledge_entries e WHERE v.entry_id=e.id AND e.knowledge_base_id=$1", "删除关联评测用例失败"},
		{"DELETE FROM agent_jobs j USING knowledge_entries e WHERE j.kind='index_entry' AND j.payload->>'entryId'=e.id::text AND e.knowledge_base_id=$1", "删除问答索引任务失败"},
		{"DELETE FROM knowledge_bases WHERE id=$1", "删除知识库及其文档和索引失败"},
	} {
		if _, err = tx.Exec(c, step.sql, id.String()); err != nil {
			fail(c, 500, "delete_failed", step.reason)
			return
		}
	}
	if err = tx.Commit(c); err != nil {
		fail(c, 500, "database_error", "无法确认删除结果，请刷新知识库列表")
		return
	}
	if err = a.cleanDeletedKnowledgeFiles(c, &id); err != nil {
		fail(c, 500, "file_cleanup_pending", "知识库已删除，磁盘原件清理未完成，后台将自动重试")
		return
	}
	ok(c, gin.H{"deleted": true})
}

// Keep the keys across crashes and retry unlinks idempotently after commit.
func (a *App) cleanDeletedKnowledgeFiles(ctx context.Context, id *uuid.UUID) error {
	rows, err := a.db.Query(ctx, "SELECT knowledge_base_id,storage_key FROM deleted_knowledge_files WHERE $1::uuid IS NULL OR knowledge_base_id=$1", id)
	if err != nil {
		return err
	}
	type file struct {
		KB  uuid.UUID
		Key string
	}
	files, err := pgx.CollectRows(rows, pgx.RowToStructByPos[file])
	if err != nil {
		return err
	}
	var failures error
	for _, f := range files {
		if err = a.files.Delete(ctx, f.Key); err == nil {
			_, err = a.db.Exec(ctx, "DELETE FROM deleted_knowledge_files WHERE knowledge_base_id=$1 AND storage_key=$2", f.KB, f.Key)
		}
		failures = errors.Join(failures, err)
	}
	return failures
}
