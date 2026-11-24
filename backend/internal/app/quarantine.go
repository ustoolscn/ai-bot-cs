package app

import (
	"context"
	"time"

	"github.com/jackc/pgx/v5"
)

const clearModerationStreakSQL = `UPDATE conversations SET moderation_pending_at=NULL,moderation_pending_event_at=NULL
    WHERE id=$1 AND moderation_pending_at IS NOT NULL`

func (a *App) quarantine(ctx context.Context, tx pgx.Tx, mid string) error {
	// Serialize failures with reply creation and successful turn completion.
	var cid string
	var eventAt time.Time
	var recent bool
	err := tx.QueryRow(ctx, `SELECT c.id::text,m.event_at,
        coalesce(c.moderation_pending_at >= clock_timestamp()-interval '10 minutes',false)
        FROM messages m JOIN conversations c ON c.id=m.conversation_id
        WHERE m.id=$1 FOR UPDATE OF c`, mid).Scan(&cid, &eventAt, &recent)
	if err != nil {
		return err
	}
	const reason = "模型返回 HTTP 403，已从后续会话上下文排除"
	tag, err := tx.Exec(ctx, `UPDATE messages SET context_excluded=true,context_exclusion_reason=$2
        WHERE id=$1 AND (NOT context_excluded OR context_exclusion_reason='pending_review')`, mid, reason)
	if err != nil {
		return err
	}
	if _, err = tx.Exec(ctx, `UPDATE messages o SET context_excluded=true,context_exclusion_reason=$2
        FROM messages i WHERE i.id=$1 AND i.direction='inbound' AND o.conversation_id=i.conversation_id
        AND o.direction='outbound' AND o.event_type='AI_RESPONSE' AND o.reply_to_message_id=i.platform_message_id
        AND NOT o.context_excluded`, mid, reason); err != nil {
		return err
	}
	if _, err = tx.Exec(ctx, "UPDATE group_reviews SET status='excluded',reason='上游模型审核拒绝' WHERE message_id=$1 AND status='pending'", mid); err != nil {
		return err
	}
	// A retry or another job reporting the same message is one failure.
	if tag.RowsAffected() == 0 {
		return nil
	}
	if !recent {
		_, err = tx.Exec(ctx, `UPDATE conversations SET moderation_pending_at=clock_timestamp(),moderation_pending_event_at=$2,
           moderation_revision=moderation_revision+1 WHERE id=$1`, cid, eventAt)
		return err
	}
	if _, err = tx.Exec(ctx, `UPDATE conversations SET moderation_cutoff=greatest(moderation_cutoff,moderation_pending_event_at,$2),
        moderation_pending_at=NULL,moderation_pending_event_at=NULL,moderation_revision=moderation_revision+1 WHERE id=$1`, cid, eventAt); err != nil {
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
