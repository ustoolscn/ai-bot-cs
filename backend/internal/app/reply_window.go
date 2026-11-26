package app

import (
	"context"
	"errors"
	"strings"

	"ai-bot/backend/internal/agent"
	"github.com/jackc/pgx/v5"
)

// The webhook already holds the conversation row lock. Stamp each arrival once,
// including messages still awaiting review. A late worker cannot extend a window.
func joinReplyWindow(ctx context.Context, tx pgx.Tx, cid, sender, mid string, trigger bool) (bool, error) {
	defaults := agent.DefaultSettings()
	// A closed quiet window stays closed. Move unfinished work into the
	// window stamped for this arrival, even when the new text has no mention.
	rows, err := tx.Query(ctx, `SELECT m.id::text,coalesce(m.reply_batch,m.id)::text
	FROM messages m
	WHERE m.conversation_id=$1 AND m.sender_id=$2 AND $2<>'' AND m.id<>$3 AND m.id IN (
		SELECT message_id FROM inbox_tasks WHERE status='processing' OR (status='pending' AND attempts>0)
		UNION ALL
		SELECT r.message_id FROM group_reviews r JOIN agent_jobs j ON j.dedupe_key='review:'||r.message_id::text
		WHERE r.queue_reply AND r.status='pending' AND j.status IN ('pending','processing')
		UNION ALL
		SELECT i.id FROM outbox_tasks q JOIN messages o ON o.id=q.message_id
		JOIN messages i ON i.bot_id=o.bot_id AND i.platform_message_id=o.reply_to_message_id AND i.direction='inbound'
		JOIN inbox_tasks t ON t.message_id=i.id AND t.status='completed'
		WHERE o.conversation_id=$1 AND o.event_type='AI_RESPONSE' AND q.status IN ('pending','processing'))`, cid, sender, mid)
	if err != nil {
		return false, err
	}
	type previousTurn struct{ id, batch string }
	previous, err := pgx.CollectRows(rows, func(r pgx.CollectableRow) (previousTurn, error) {
		var p previousTurn
		err := r.Scan(&p.id, &p.batch)
		return p, err
	})
	if err != nil {
		return false, err
	}
	var joined bool
	err = tx.QueryRow(ctx, `WITH tick AS MATERIALIZED (SELECT clock_timestamp() AS at),
    config AS (SELECT coalesce((s.settings->>'replyQuietSeconds')::int,(g.value->>'replyQuietSeconds')::int,$5::int) AS quiet,
       coalesce((s.settings->>'replyMaxWaitSeconds')::int,(g.value->>'replyMaxWaitSeconds')::int,$6::int) AS maximum
       FROM conversations c LEFT JOIN bot_services s ON s.id=c.service_id
       LEFT JOIN system_settings g ON g.key='agent_settings' WHERE c.id=$1),
    previous AS (SELECT reply_batch,reply_max_at FROM messages,tick WHERE conversation_id=$1 AND sender_id=$2 AND $2<>'' AND reply_batch IS NOT NULL AND reply_due_at>tick.at ORDER BY reply_due_at DESC LIMIT 1)
    UPDATE messages m SET reply_batch=coalesce(p.reply_batch,m.id),
      reply_max_at=coalesce(p.reply_max_at,t.at+config.maximum*interval '1 second'),
      reply_due_at=least(t.at+config.quiet*interval '1 second',coalesce(p.reply_max_at,t.at+config.maximum*interval '1 second'))
    FROM tick t CROSS JOIN config LEFT JOIN previous p ON true
    WHERE m.id=$3 AND m.reply_batch IS NULL AND ($4 OR p.reply_batch IS NOT NULL)
    RETURNING true`, cid, sender, mid, trigger || len(previous) > 0, defaults.ReplyQuietSeconds, defaults.ReplyMaxWaitSeconds).Scan(&joined)
	if errors.Is(err, pgx.ErrNoRows) {
		err = tx.QueryRow(ctx, "SELECT reply_batch IS NOT NULL FROM messages WHERE id=$1", mid).Scan(&joined)
		return joined, err
	}
	if err != nil {
		return false, err
	}
	for _, p := range previous {
		if _, err = tx.Exec(ctx, `UPDATE messages SET reply_batch=(SELECT reply_batch FROM messages WHERE id=$1)
		WHERE conversation_id=$2 AND sender_id=$3 AND (reply_batch=$4 OR id=$4)`, mid, cid, sender, p.batch); err != nil {
			return false, err
		}
		if _, err = tx.Exec(ctx, `UPDATE inbox_tasks SET status='merged',updated_at=now() WHERE message_id=$1 AND status IN ('pending','processing','completed')`, p.id); err != nil {
			return false, err
		}
		if _, err = tx.Exec(ctx, `WITH cancelled AS (UPDATE outbox_tasks q SET status='merged',last_error=NULL,updated_at=now()
		FROM messages o,messages i WHERE i.id=$1 AND o.conversation_id=i.conversation_id AND o.reply_to_message_id=i.platform_message_id
		AND o.event_type='AI_RESPONSE' AND q.message_id=o.id AND q.status IN ('pending','processing') RETURNING q.message_id)
		UPDATE messages SET status='merged',context_excluded=true,context_exclusion_reason='reply_merged' WHERE id IN (SELECT message_id FROM cancelled)`, p.id); err != nil {
			return false, err
		}
	}
	return joined, nil
}

type replySegment struct {
	ID, Content       string
	HasImage, Mention bool
}

func (a *App) replySegments(ctx context.Context, mid string) ([]replySegment, error) {
	rows, err := a.db.Query(ctx, `SELECT m.id::text,display_mentions(m,m.content),m.parts @> '[{"type":"image"}]',m.event_type='GROUP_AT_MESSAGE_CREATE' OR coalesce((m.raw_event#>'{d,mentions}') @> '[{"is_you":true}]',false)
    FROM messages anchor JOIN messages m ON m.reply_batch=anchor.reply_batch OR m.id=anchor.id
    JOIN inbox_tasks t ON t.message_id=m.id WHERE anchor.id=$1 AND (m.id=anchor.id OR t.status='merged') AND `+visibleContextSQL+`
    ORDER BY m.event_at,m.created_at,m.id`, mid)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, pgx.RowToStructByPos[replySegment])
}

func segmentText(segments []replySegment) string {
	var lines []string
	for _, segment := range segments {
		if segment.Content != "" {
			lines = append(lines, segment.Content)
		}
	}
	return strings.Join(lines, "\n")
}
