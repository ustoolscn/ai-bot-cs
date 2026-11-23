package app

import (
	"context"
	"github.com/jackc/pgx/v5"
)

func (a *App) quarantine(ctx context.Context, tx pgx.Tx, mid string) error {
	var cid string
	err := tx.QueryRow(ctx, `UPDATE messages SET context_excluded=true,context_exclusion_reason='模型返回 HTTP 403，已从后续会话上下文排除' WHERE id=$1 RETURNING conversation_id::text`, mid).Scan(&cid)
	if err != nil {
		return err
	}
	if _, err = tx.Exec(ctx, `UPDATE conversations SET moderation_cutoff=greatest(moderation_cutoff,(SELECT event_at FROM messages WHERE id=$2)) WHERE id=$1`, cid, mid); err != nil {
		return err
	}
	if _, err = tx.Exec(ctx, "UPDATE group_reviews SET status='excluded',reason='上游模型审核拒绝' WHERE message_id=$1 AND status='pending'", mid); err != nil {
		return err
	}
	_, err = tx.Exec(ctx, "DELETE FROM conversation_summaries WHERE conversation_id=$1", cid)
	return err
}
func (a *App) quarantineBackground(ctx context.Context, mid string) error {
	tx, err := a.db.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	if err = a.quarantine(ctx, tx, mid); err != nil {
		return err
	}
	return tx.Commit(ctx)
}
