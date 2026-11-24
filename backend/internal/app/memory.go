package app

import (
	"context"
	"regexp"
	"strings"
	"time"

	"ai-bot/backend/internal/agent"
	"github.com/gin-gonic/gin"
)

// loadMemories returns pinned memories first, then the most relevant (by
// vector similarity when an embedding model is configured) or most recent.
func (a *App) loadMemories(ctx context.Context, s agent.Settings, botID, conversationID, userID, query string, embeddings *queryEmbeddings) []agent.Memory {
	limit := s.MaxMemories
	var rowsSQL string
	args := []any{botID, conversationID, userID, limit}
	if cl, pid, ok := a.embeddingModel(ctx, s); ok && strings.TrimSpace(query) != "" {
		var searchable bool
		err := a.db.QueryRow(ctx, `SELECT EXISTS(SELECT FROM user_memories WHERE bot_id=$1 AND conversation_id=$2 AND platform_user_id=$3 AND status='active'
			AND (expires_at IS NULL OR expires_at>now()) AND embedding IS NOT NULL AND embedding_profile_id=$4)`, botID, conversationID, userID, pid).Scan(&searchable)
		if searchable || err != nil {
			if v, err := embeddings.get(a, ctx, cl, pid, query); err == nil {
				rowsSQL = `SELECT kind,to_char(coalesce(event_at,created_at) AT TIME ZONE 'UTC','YYYY-MM-DD') || ' · ' || content FROM user_memories WHERE bot_id=$1 AND conversation_id=$2 AND platform_user_id=$3 AND status='active' AND (expires_at IS NULL OR expires_at>now())
				ORDER BY pinned DESC, CASE WHEN embedding IS NOT NULL AND embedding_profile_id=$5 AND vector_dims(embedding)=$7 THEN embedding <=> $6::vector ELSE 1 END, updated_at DESC LIMIT $4`
				args = append(args, pid, vectorLiteral(v), len(v))
			}
		}
	}
	if rowsSQL == "" {
		rowsSQL = `SELECT kind,to_char(coalesce(event_at,created_at) AT TIME ZONE 'UTC','YYYY-MM-DD') || ' · ' || content FROM user_memories WHERE bot_id=$1 AND conversation_id=$2 AND platform_user_id=$3 AND status='active' AND (expires_at IS NULL OR expires_at>now()) ORDER BY pinned DESC, updated_at DESC LIMIT $4`
	}
	rows, err := a.db.Query(ctx, rowsSQL, args...)
	if err != nil {
		a.log.Warn("load memories failed", "error", err)
		return nil
	}
	defer rows.Close()
	var out []agent.Memory
	for rows.Next() {
		var m agent.Memory
		if rows.Scan(&m.Kind, &m.Content) == nil {
			out = append(out, m)
		}
	}
	if len(out) > 0 {
		_, _ = a.db.Exec(ctx, "UPDATE user_memories SET last_used_at=now() WHERE bot_id=$1 AND conversation_id=$2 AND platform_user_id=$3 AND status='active'", botID, conversationID, userID)
	}
	var profile string
	if err := a.db.QueryRow(ctx, "SELECT content FROM user_profiles WHERE conversation_id=$1 AND platform_user_id=$2", conversationID, userID).Scan(&profile); err == nil && profile != "" {
		out = append([]agent.Memory{{Kind: "profile", Content: "用户档案：" + profile}}, out...)
	}
	return out
}

var sensitiveMemory = []string{"sk-", "密码", "password", "token", "密钥", "api key", "apikey", "身份证", "银行卡", "api_key", "私钥", "private key", "bearer ", "ghp_", "github_pat_", "akia"}

var phoneMemory = regexp.MustCompile(`(?:^|[^0-9])(?:\+?\d[ ()-]*){10,18}(?:$|[^0-9])`)

func privateMemory(content string) bool {
	if phoneMemory.MatchString(content) {
		return true
	}
	lower := strings.ToLower(content)
	for _, w := range sensitiveMemory {
		if strings.Contains(lower, w) {
			return true
		}
	}
	return false
}

// saveMemory stores a memory unless it is sensitive or a near-duplicate.
func (a *App) saveMemory(ctx context.Context, s agent.Settings, botID, conversationID, userID, userName, kind, content string, confidence float64, source, messageID string, eventAt *time.Time) error {
	if messageID != "" {
		if excluded, err := a.messageExcluded(ctx, messageID); err != nil {
			return err
		} else if excluded {
			return nil
		}
	}
	content = strings.TrimSpace(content)
	if content == "" || len([]rune(content)) > 300 {
		return nil
	}
	if privateMemory(content) {
		return nil
	}
	switch kind {
	case "fact", "preference", "issue", "profile":
	default:
		kind = "fact"
	}
	var exists bool
	_ = a.db.QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM user_memories WHERE bot_id=$1 AND conversation_id=$2 AND platform_user_id=$3 AND status='active' AND content=$4)", botID, conversationID, userID, content).Scan(&exists)
	if exists {
		_, err := a.db.Exec(ctx, "UPDATE user_memories SET event_at=coalesce($6,event_at),updated_at=now(),confidence=GREATEST(confidence,$5) WHERE bot_id=$1 AND conversation_id=$2 AND platform_user_id=$3 AND status='active' AND content=$4", botID, conversationID, userID, content, confidence, eventAt)
		return err
	}
	var embedding, profile any
	if cl, pid, ok := a.embeddingModel(ctx, s); ok {
		if v, err := a.embed(context.WithValue(ctx, usageScopeKey{}, usageScope{conversation: conversationID, message: messageID}), cl, pid, "embedding", []string{content}); err == nil && len(v) == 1 {
			// Skip near-duplicates of an existing memory.
			var dup bool
			_ = a.db.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM user_memories WHERE bot_id=$1 AND conversation_id=$2 AND platform_user_id=$3 AND status='active' AND embedding IS NOT NULL AND embedding_profile_id=$4 AND vector_dims(embedding)=$6 AND 1-(embedding <=> $5::vector) > 0.93)`, botID, conversationID, userID, pid, vectorLiteral(v[0]), len(v[0])).Scan(&dup)
			if dup {
				return nil
			}
			embedding, profile = vectorLiteral(v[0]), pid
		}
	}
	_, err := a.db.Exec(ctx, `INSERT INTO user_memories(bot_id,conversation_id,platform_user_id,display_name,kind,content,embedding,embedding_profile_id,confidence,source,source_message_id,event_at) VALUES($1,$2,$3,$4,$5,$6,$7::vector,$8,$9,$10,$11,$12)`,
		botID, conversationID, userID, userName, kind, content, embedding, profile, confidence, source, nullString(messageID), eventAt)
	if err == nil {
		// Keep at most 50 active memories per user.
		_, _ = a.db.Exec(ctx, `UPDATE user_memories SET status='archived',updated_at=now() WHERE id IN (SELECT id FROM user_memories WHERE bot_id=$1 AND conversation_id=$2 AND platform_user_id=$3 AND status='active' AND NOT pinned ORDER BY updated_at DESC OFFSET 50)`, botID, conversationID, userID)
	}
	return err
}

// --- admin API ---

func (a *App) memoryDirectory(c *gin.Context) {
	queryList(c, a, `SELECT m.bot_id::text AS "botId", b.name AS "botName",
        coalesce(m.conversation_id::text,'unassigned') AS "conversationId",
        coalesce(nullif(c.name,''),c.platform_id,'来源未确定') AS "conversationName",
        coalesce(c.type,'unassigned') AS "conversationType", m.platform_user_id AS "userId",
        coalesce((array_agg(m.display_name ORDER BY m.updated_at DESC)
          FILTER (WHERE m.display_name<>''))[1],'') AS "displayName", sum(m.n) AS count
        FROM (SELECT bot_id,conversation_id,platform_user_id,display_name,updated_at,status,1 AS n FROM user_memories
 UNION ALL SELECT c.bot_id,p.conversation_id,p.platform_user_id,p.display_name,p.updated_at,'active',0 FROM user_profiles p JOIN conversations c ON c.id=p.conversation_id WHERE p.content<>'') m JOIN bots b ON b.id=m.bot_id LEFT JOIN conversations c ON c.id=m.conversation_id
        WHERE m.status='active' GROUP BY m.bot_id,b.name,m.conversation_id,c.id,m.platform_user_id
        ORDER BY b.name,"conversationName","displayName",m.platform_user_id`)
}

func (a *App) listMemories(c *gin.Context) {
	args := []any{}
	where := []string{"m.status='active'"}
	if v := c.Query("conversationId"); v == "unassigned" {
		where = append(where, "m.conversation_id IS NULL")
	} else if v != "" {
		args = append(args, v)
		where = append(where, "m.conversation_id=$"+itoa(len(args)))
	}
	if v := c.Query("botId"); v != "" {
		args = append(args, v)
		where = append(where, "m.bot_id=$"+itoa(len(args)))
	}
	if v := c.Query("userId"); v != "" {
		args = append(args, v)
		where = append(where, "m.platform_user_id=$"+itoa(len(args)))
	}
	if v := strings.TrimSpace(c.Query("q")); v != "" {
		args = append(args, "%"+v+"%")
		n := itoa(len(args))
		where = append(where, "(m.content ILIKE $"+n+" OR m.display_name ILIKE $"+n+")")
	}
	queryList(c, a, `SELECT m.id::text,m.bot_id::text AS "botId",b.name AS "botName",m.platform_user_id AS "userId",m.display_name AS "displayName",m.kind,m.content,(m.embedding IS NOT NULL) AS "hasEmbedding",m.confidence,m.pinned,m.source,m.source_message_id::text AS "sourceMessageId",m.expires_at AS "expiresAt",m.last_used_at AS "lastUsedAt",coalesce(m.event_at,m.created_at) AS "eventAt",m.created_at AS "createdAt",m.updated_at AS "updatedAt"
		FROM user_memories m JOIN bots b ON b.id=m.bot_id WHERE `+strings.Join(where, " AND ")+` ORDER BY m.updated_at DESC LIMIT 300`, args...)
}

func (a *App) createMemory(c *gin.Context) {
	var in struct {
		EventAt                                                   *time.Time `json:"eventAt"`
		BotID, ConversationID, UserID, DisplayName, Kind, Content string
		Pinned                                                    bool
	}
	if c.ShouldBindJSON(&in) != nil || in.BotID == "" || in.ConversationID == "" || in.UserID == "" || strings.TrimSpace(in.Content) == "" {
		fail(c, 400, "invalid_request", "机器人、会话、用户和内容必填")
		return
	}
	if privateMemory(in.Content) || len([]rune(in.Content)) > 300 {
		fail(c, 400, "invalid_memory", "内容过长或含敏感信息")
		return
	}
	var valid bool
	if err := a.db.QueryRow(c, "SELECT EXISTS(SELECT 1 FROM conversations WHERE id=$1 AND bot_id=$2)", in.ConversationID, in.BotID).Scan(&valid); err != nil || !valid {
		fail(c, 400, "invalid_conversation", "会话与机器人不匹配")
		return
	}
	s := a.agentSettings(c)
	if err := a.saveMemory(c, s, in.BotID, in.ConversationID, in.UserID, in.DisplayName, in.Kind, in.Content, 1, "admin", "", in.EventAt); err != nil {
		fail(c, 500, "database_error", "保存失败")
		return
	}
	if in.Pinned {
		_, _ = a.db.Exec(c, "UPDATE user_memories SET pinned=true WHERE bot_id=$1 AND platform_user_id=$2 AND content=$3 AND conversation_id=$4", in.BotID, in.UserID, strings.TrimSpace(in.Content), in.ConversationID)
	}
	created(c, gin.H{"saved": true})
}

func (a *App) updateMemory(c *gin.Context) {
	var in struct {
		EventAt *time.Time `json:"eventAt"`
		Content string     `json:"content"`
		Kind    string     `json:"kind"`
		Pinned  bool       `json:"pinned"`
	}
	if c.ShouldBindJSON(&in) != nil || strings.TrimSpace(in.Content) == "" {
		fail(c, 400, "invalid_request", "内容必填")
		return
	}
	if privateMemory(in.Content) || len([]rune(in.Content)) > 300 {
		fail(c, 400, "invalid_memory", "内容过长或含敏感信息")
		return
	}
	tag, err := a.db.Exec(c, "UPDATE user_memories SET content=$1,kind=COALESCE(NULLIF($2,''),kind),pinned=$3,embedding=NULL,event_at=coalesce($5,event_at),updated_at=now() WHERE id=$4", strings.TrimSpace(in.Content), in.Kind, in.Pinned, c.Param("id"), in.EventAt)
	if err != nil || tag.RowsAffected() == 0 {
		fail(c, 404, "not_found", "记忆不存在")
		return
	}
	ok(c, gin.H{"updated": true})
}

func (a *App) deleteMemory(c *gin.Context) {
	_, _ = a.db.Exec(c, "DELETE FROM user_memories WHERE id=$1", c.Param("id"))
	ok(c, gin.H{"deleted": true})
}
