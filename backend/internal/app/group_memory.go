package app

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"ai-bot/backend/internal/agent"
	"github.com/gin-gonic/gin"
)

type groupDigestResult struct {
	GroupVibe string `json:"group_vibe"`
	Memories  []struct {
		Kind       string `json:"kind"`
		Content    string `json:"content"`
		Importance int    `json:"importance"`
		TTLDays    int    `json:"ttlDays"`
	} `json:"memories"`
}

// queueDueGroupDigests schedules group episodic digests for active group chats
// that have accumulated enough new messages since the last digest.
func (a *App) queueDueGroupDigests(ctx context.Context) error {
	rows, err := a.db.Query(ctx, `SELECT c.id::text FROM conversations c
		JOIN bots b ON b.id=c.bot_id
		WHERE c.type='group' AND c.enabled AND NOT c.bot_removed AND b.enabled
		AND (c.last_group_digest_at IS NULL OR c.last_group_digest_at < now()-interval '2 hours')
		AND (SELECT count(*) FROM messages m WHERE m.conversation_id=c.id AND NOT m.context_excluded
			AND m.event_type<>'PROCESSING_ACK' AND m.content<>''
			AND m.event_at > COALESCE(c.group_digest_cursor, c.last_group_digest_at, 'epoch')) >= 15
		LIMIT 10`)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var cid string
		if rows.Scan(&cid) == nil && cid != "" {
			a.enqueueJob(ctx, "group_digest", map[string]any{"conversationId": cid}, "group_digest:"+cid)
		}
	}
	return nil
}

// jobGroupDigest summarizes recent group chat messages into group_memories and group_vibe.
func (a *App) jobGroupDigest(ctx context.Context, conversationID string) error {
	if parseUUID(conversationID) != nil {
		return nil
	}
	var botID, convType, oldVibe string
	var cursor *time.Time
	if err := a.db.QueryRow(ctx, `SELECT bot_id::text,type,COALESCE(group_vibe,''),group_digest_cursor FROM conversations WHERE id=$1`, conversationID).
		Scan(&botID, &convType, &oldVibe, &cursor); err != nil {
		return nil
	}
	if convType != "group" {
		return nil
	}
	s, _, profileID, err := a.conversationAgent(ctx, conversationID, a.agentSettings(ctx))
	if err != nil {
		return err
	}
	ctx = context.WithValue(ctx, usageScopeKey{}, usageScope{conversation: conversationID})

	since := time.Time{}
	if cursor != nil {
		since = *cursor
	}
	rows, err := a.db.Query(ctx, `SELECT m.direction,COALESCE(NULLIF(m.sender_name,''),m.sender_id,'群友'),display_mentions(m,m.content),m.event_at
		FROM messages m WHERE m.conversation_id=$1 AND m.event_at>$2 AND NOT m.context_excluded
		AND m.event_type<>'PROCESSING_ACK' AND m.content<>''
		ORDER BY m.event_at DESC LIMIT 60`, conversationID, since)
	if err != nil {
		return err
	}
	var lines []string
	var newestAt time.Time
	for rows.Next() {
		var dir, name, content string
		var at time.Time
		if rows.Scan(&dir, &name, &content, &at) == nil {
			if at.After(newestAt) {
				newestAt = at
			}
			who := name
			if dir == "outbound" {
				who = "机器人"
			}
			lines = append([]string{fmt.Sprintf("[%s] %s：%s", at.In(chinaLocation).Format("01-02 15:04"), who, agent.TruncateRunes(content, 200))}, lines...)
		}
	}
	rows.Close()
	if len(lines) == 0 {
		_, _ = a.db.Exec(ctx, "UPDATE conversations SET last_group_digest_at=now() WHERE id=$1", conversationID)
		return nil
	}

	existing, _ := a.loadGroupMemories(ctx, conversationID, 10)
	existingJSON, _ := json.Marshal(existing)
	input := fmt.Sprintf("已有群氛围：%s\n已有群记忆：%s\n\n近期群聊记录：\n%s", oldVibe, string(existingJSON), strings.Join(lines, "\n"))

	var res groupDigestResult
	if err := a.auxJSON(ctx, s, profileID, "", "", "group_digest", agent.GroupDigestPrompt, input, &res); err != nil {
		return err
	}

	vectors := map[int]string{}
	var embedProfile any
	if cl, pid, ok := a.embeddingModel(ctx, s); ok {
		for i, m := range res.Memories {
			if i >= 3 {
				break
			}
			c := strings.TrimSpace(m.Content)
			if c != "" && !privateMemory(c) && len([]rune(c)) <= 240 {
				if v, e := a.embed(ctx, cl, pid, "embedding", []string{c}); e == nil && len(v) == 1 {
					vectors[i] = vectorLiteral(v[0])
					embedProfile = pid
				}
			}
		}
	}

	tx, err := a.db.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)

	_, _ = tx.Exec(ctx, `UPDATE group_memories SET status='archived',updated_at=now() WHERE conversation_id=$1 AND status='active' AND expires_at IS NOT NULL AND expires_at<=now()`, conversationID)

	for i, m := range res.Memories {
		if i >= 3 {
			break
		}
		content := strings.TrimSpace(m.Content)
		if content == "" || privateMemory(content) || len([]rune(content)) > 240 {
			continue
		}
		kind := m.Kind
		switch kind {
		case "topic", "event", "vibe", "meme", "incident":
		default:
			kind = "topic"
		}
		imp := m.Importance
		if imp < 1 || imp > 10 {
			imp = 5
		}
		ttl := m.TTLDays
		if ttl < 1 || ttl > 30 {
			ttl = 7
		}
		exp := time.Now().AddDate(0, 0, ttl)
		_, err = tx.Exec(ctx, `INSERT INTO group_memories(conversation_id,bot_id,kind,content,importance,embedding,embedding_profile_id,expires_at)
			SELECT $1,$2,$3,$4,$5,$6::vector,$7,$8
			WHERE NOT EXISTS(SELECT 1 FROM group_memories WHERE conversation_id=$1 AND status='active' AND content=$4)`,
			conversationID, botID, kind, content, imp, nullString(vectors[i]), embedProfile, exp)
		if err != nil {
			return err
		}
	}

	// Keep up to 25 active memories per group.
	_, _ = tx.Exec(ctx, `UPDATE group_memories SET status='archived',updated_at=now() WHERE id IN (
		SELECT id FROM group_memories WHERE conversation_id=$1 AND status='active' ORDER BY importance DESC, updated_at DESC OFFSET 25)`, conversationID)

	vibe := agent.TruncateRunes(strings.TrimSpace(res.GroupVibe), 120)
	if vibe == "" {
		vibe = oldVibe
	}
	if _, err = tx.Exec(ctx, `UPDATE conversations SET group_vibe=$2,last_group_digest_at=now(),group_digest_cursor=GREATEST(COALESCE(group_digest_cursor,'epoch'),$3) WHERE id=$1`,
		conversationID, vibe, newestAt); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

// --- admin API ---

func (a *App) listGroupMemoriesAPI(c *gin.Context) {
	cid := c.Param("id")
	if parseUUID(cid) != nil {
		fail(c, 400, "invalid_id", "无效的会话 ID")
		return
	}
	var vibe string
	var lastDigest *time.Time
	_ = a.db.QueryRow(c, "SELECT COALESCE(group_vibe,''),last_group_digest_at FROM conversations WHERE id=$1", cid).Scan(&vibe, &lastDigest)
	rows, err := a.db.Query(c, `SELECT id::text,kind,content,importance,expires_at AS "expiresAt",created_at AS "createdAt",updated_at AS "updatedAt"
		FROM group_memories WHERE conversation_id=$1 AND status='active' AND (expires_at IS NULL OR expires_at>now())
		ORDER BY importance DESC,updated_at DESC LIMIT 50`, cid)
	if err != nil {
		fail(c, 500, "database_error", "读取群记忆失败")
		return
	}
	items, err := rowsJSON(rows)
	if err != nil {
		fail(c, 500, "database_error", "读取群记忆失败")
		return
	}
	ok(c, gin.H{"groupVibe": vibe, "lastDigestAt": lastDigest, "items": items})
}

func (a *App) triggerGroupDigestAPI(c *gin.Context) {
	cid := c.Param("id")
	if parseUUID(cid) != nil {
		fail(c, 400, "invalid_id", "无效的会话 ID")
		return
	}
	if err := a.jobGroupDigest(c.Request.Context(), cid); err != nil {
		fail(c, 502, "digest_failed", "整理群记忆失败："+err.Error())
		return
	}
	ok(c, gin.H{"digested": true})
}

func (a *App) deleteGroupMemoryAPI(c *gin.Context) {
	_, _ = a.db.Exec(c, "DELETE FROM group_memories WHERE id=$1", c.Param("id"))
	ok(c, gin.H{"deleted": true})
}
