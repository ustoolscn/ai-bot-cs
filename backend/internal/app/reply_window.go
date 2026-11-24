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
	var joined bool
	err := tx.QueryRow(ctx, `WITH tick AS MATERIALIZED (SELECT clock_timestamp() AS at),
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
    RETURNING true`, cid, sender, mid, trigger, defaults.ReplyQuietSeconds, defaults.ReplyMaxWaitSeconds).Scan(&joined)
	if errors.Is(err, pgx.ErrNoRows) {
		err = tx.QueryRow(ctx, "SELECT reply_batch IS NOT NULL FROM messages WHERE id=$1", mid).Scan(&joined)
	}
	return joined, err
}

type replySegment struct {
	ID, Content       string
	HasImage, Mention bool
}

func (a *App) replySegments(ctx context.Context, mid string) ([]replySegment, error) {
	rows, err := a.db.Query(ctx, `SELECT m.id::text,m.content,m.parts @> '[{"type":"image"}]',m.event_type='GROUP_AT_MESSAGE_CREATE' OR coalesce((m.raw_event#>'{d,mentions}') @> '[{"is_you":true}]',false)
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
