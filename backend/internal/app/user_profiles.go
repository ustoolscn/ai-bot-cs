package app

import (
	"context"
	"encoding/json"
	"errors"
	"sort"
	"strings"
	"time"

	"ai-bot/backend/internal/agent"
	"github.com/gin-gonic/gin"
	"github.com/jackc/pgx/v5"
)

func (a *App) userProfile(c *gin.Context) {
	cid, uid := c.Param("id"), c.Param("user")
	if parseUUID(cid) != nil {
		fail(c, 400, "invalid_request", "无效会话")
		return
	}
	var content string
	var updated *time.Time
	var err error
	switch c.Request.Method {
	case "GET":
		err = a.db.QueryRow(c, `SELECT coalesce(p.content,''),p.updated_at FROM conversations c LEFT JOIN user_profiles p ON p.conversation_id=c.id AND p.platform_user_id=$2 WHERE c.id=$1`, cid, uid).Scan(&content, &updated)
	case "PUT":
		var in struct{ Content *string }
		if c.ShouldBindJSON(&in) != nil || in.Content == nil || privateMemory(*in.Content) || agent.EstimateTokens(*in.Content) > 2000 {
			fail(c, 400, "invalid_profile", "档案须在约 2000 token 以内，且不得含敏感信息")
			return
		}
		content = strings.TrimSpace(*in.Content)
		err = a.db.QueryRow(c, `INSERT INTO user_profiles(conversation_id,platform_user_id,content) SELECT id,$2,$3 FROM conversations WHERE id=$1 ON CONFLICT(conversation_id,platform_user_id) DO UPDATE SET content=excluded.content,updated_at=now() RETURNING updated_at`, cid, uid, content).Scan(&updated)
	case "DELETE":
		_, err = a.db.Exec(c, "DELETE FROM user_profiles WHERE conversation_id=$1 AND platform_user_id=$2", cid, uid)
	}
	if errors.Is(err, pgx.ErrNoRows) {
		fail(c, 404, "not_found", "会话不存在")
		return
	}
	if err != nil {
		fail(c, 500, "database_error", "读写档案失败")
		return
	}
	ok(c, gin.H{"conversationId": cid, "userId": uid, "content": content, "updatedAt": updated})
}

func (a *App) applyExtractedMemory(ctx context.Context, ev evaluation, previous, existing, cid, uid, name, bot, mid string, at time.Time, s agent.Settings) error {
	var known []struct{ ID, Content string }
	if err := json.Unmarshal([]byte(existing), &known); err != nil {
		return err
	}
	prior := map[string]string{}
	for _, row := range known {
		prior[row.ID] = row.Content
	}
	// Embeddings can be slow. Build them before locking the user's records.
	vectors := map[int]string{}
	var embeddingProfile any
	if len(ev.Memories) > 0 {
		if cl, pid, ok := a.embeddingModel(ctx, s); ok {
			for i, m := range ev.Memories {
				if i >= 3 {
					break
				}
				if m.Content != "" && !privateMemory(m.Content) && len([]rune(m.Content)) <= 300 && (m.Action == "" || m.Action == "add" || m.Action == "update") {
					if v, err := cl.Embed(ctx, []string{m.Content}); err == nil && len(v) == 1 {
						vectors[i] = vectorLiteral(v[0])
						embeddingProfile = pid
					}
				}
			}
		}
	}
	tx, err := a.db.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	if _, err = tx.Exec(ctx, "SELECT pg_advisory_xact_lock(hashtextextended($1,0))", cid+"/"+uid); err != nil {
		return err
	}
	var excluded bool
	if err = tx.QueryRow(ctx, "SELECT context_excluded FROM messages WHERE id=$1 FOR SHARE", mid).Scan(&excluded); err != nil {
		return err
	}
	if excluded {
		return nil
	}
	if ev.Profile != nil && *ev.Profile != previous && !privateMemory(*ev.Profile) {
		profile := []rune(*ev.Profile)
		limit := sort.Search(len(profile), func(i int) bool { return agent.EstimateTokens(string(profile[:i+1])) > 2000 })
		profile = profile[:limit]
		tag, err := tx.Exec(ctx, `INSERT INTO user_profiles(conversation_id,platform_user_id,display_name,content)
 SELECT $1,$2,$3,$4 WHERE $5::text='' OR EXISTS(SELECT FROM user_profiles WHERE conversation_id=$1 AND platform_user_id=$2)
 ON CONFLICT(conversation_id,platform_user_id) DO UPDATE SET content=excluded.content,display_name=excluded.display_name,updated_at=now() WHERE user_profiles.content=$5`, cid, uid, name, string(profile), previous)
		if err != nil {
			return err
		}
		if tag.RowsAffected() == 0 {
			return errors.New("用户档案已更新，重新提取")
		}
	}
	for i, m := range ev.Memories {
		if i >= 3 {
			break
		}
		if m.Confidence < 0.6 {
			continue
		}
		content := strings.TrimSpace(m.Content)
		old, found := prior[m.ID]
		if m.Action == "delete" && found {
			_, err = tx.Exec(ctx, "UPDATE user_memories SET status='archived',updated_at=now() WHERE id=$1 AND conversation_id=$2 AND platform_user_id=$3 AND content=$4 AND NOT pinned", m.ID, cid, uid, old)
		} else if content != "" && !privateMemory(content) && len([]rune(content)) <= 300 {
			event, _ := time.Parse(time.RFC3339, m.EventAt)
			switch m.Action {
			case "", "add":
				if event.IsZero() {
					event = at
				}
				kind := m.Kind
				if kind != "issue" && kind != "preference" {
					kind = "fact"
				}
				_, err = tx.Exec(ctx, `INSERT INTO user_memories(bot_id,conversation_id,platform_user_id,display_name,kind,content,confidence,source,source_message_id,event_at,embedding,embedding_profile_id) SELECT $1,$2,$3,$4,$5,$6,$7,'extracted',$8,$9,$10::vector,$11 WHERE NOT EXISTS(SELECT FROM user_memories WHERE conversation_id=$2 AND platform_user_id=$3 AND status='active' AND content=$6)`, bot, cid, uid, name, kind, content, m.Confidence, mid, event, nullString(vectors[i]), embeddingProfile)
			case "update":
				if found {
					var when any
					if !event.IsZero() {
						when = event
					}
					_, err = tx.Exec(ctx, `UPDATE user_memories SET content=$4,updated_at=now(),event_at=coalesce($5,event_at),embedding=$7::vector,embedding_profile_id=$8 WHERE id=$1 AND conversation_id=$2 AND platform_user_id=$3 AND content=$6 AND NOT pinned AND status='active'`, m.ID, cid, uid, content, when, old, nullString(vectors[i]), embeddingProfile)
				}
			}
		}
		if err != nil {
			return err
		}
	}
	_, err = tx.Exec(ctx, `UPDATE user_memories SET status='archived' WHERE id IN (SELECT id FROM user_memories WHERE conversation_id=$1 AND platform_user_id=$2 AND status='active' AND NOT pinned ORDER BY updated_at DESC,id OFFSET 50)`, cid, uid)
	if err != nil {
		return err
	}
	return tx.Commit(ctx)
}
