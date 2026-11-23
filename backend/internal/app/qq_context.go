package app

import (
	"ai-bot/backend/internal/agent"
	"ai-bot/backend/internal/qq"
	"context"
	"errors"
	"fmt"
	"github.com/jackc/pgx/v5"
	"strings"
	"time"
)

func parseQQTime(s string) *time.Time {
	v, e := time.Parse(time.RFC3339, s)
	if e != nil {
		return nil
	}
	return &v
}
func (a *App) qqContext(ctx context.Context, j inboxJob) (string, error) {
	var raw []byte
	if err := a.db.QueryRow(ctx, "SELECT raw_event FROM messages WHERE id=$1", j.MessageID).Scan(&raw); err != nil {
		return "", err
	}
	details := qq.DescribeEvent(raw)
	if details.Reference != "" {
		var who, body string
		var hidden bool
		err := a.db.QueryRow(ctx, `SELECT CASE WHEN direction='outbound' THEN '机器人' ELSE COALESCE(NULLIF(sender_name,''),sender_id) END,content,NOT (`+visibleContextSQL+`) FROM messages m WHERE conversation_id=$1 AND qq_ref_idx=$2 AND id<>$3 ORDER BY created_at DESC LIMIT 1`, j.ConversationID, details.Reference, j.MessageID).Scan(&who, &body, &hidden)
		switch {
		case err == nil && hidden:
			details.Elements = "引用内容不可用"
		case err == nil:
			if strings.TrimSpace(body) == "" {
				body = details.Elements
			}
			details.Elements = fmt.Sprintf("%s：%s", who, body)
		case !errors.Is(err, pgx.ErrNoRows):
			return "", err
		case details.Elements == "":
			details.Elements = "未取得原文"
		}
	}
	parts := []string{j.Content}
	if details.Elements != "" {
		label := "消息元素"
		if details.Reference != "" || details.IsQuote {
			label = "用户引用的原文"
		}
		parts = append(parts, label+"："+agent.TruncateRunes(details.Elements, 4000))
	}
	if details.Extra != "" {
		parts = append(parts, agent.TruncateRunes(details.Extra, 1600))
	}
	return strings.TrimSpace(strings.Join(parts, "\n")), nil
}
