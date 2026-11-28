package app

import (
	"context"
	"math"
	"regexp"
	"sort"
	"strings"
	"time"

	"ai-bot/backend/internal/agent"
	"github.com/gin-gonic/gin"
)

type memoryCandidate struct {
	ID          string
	Kind        string
	Content     string
	Importance  int
	EmotionTag  string
	SourceWhere string
	EventDate   string
	Pinned      bool
	Resolved    bool
	AccessCount int
	Similarity  float64
	LastTouched time.Time
	Score       float64
}

type subjectContextBundle struct {
	SubjectID     string
	Subject       subjectRecord
	Memories      []agent.Memory
	GroupMemories []agent.GroupMemoryItem
	GroupVibe     string
}

// loadMemories keeps compatibility for callers that only need the ranked memory slice.
func (a *App) loadMemories(ctx context.Context, s agent.Settings, botID, conversationID, userID, query string, embeddings *queryEmbeddings) []agent.Memory {
	bundle := a.loadSubjectContext(ctx, s, botID, conversationID, "", userID, "", query, "all", embeddings)
	return bundle.Memories
}

// loadSubjectContext resolves the cross-bot person subject, ranks cognitive memories
// using relevance + Ebbinghaus forgetting decay + importance, updates recall stats
// strictly for selected memories, and loads group episodic context for group chats.
func (a *App) loadSubjectContext(ctx context.Context, s agent.Settings, botID, conversationID, convType, userID, userName, query, memoryFocus string, embeddings *queryEmbeddings) subjectContextBundle {
	var bundle subjectContextBundle
	if convType == "" && conversationID != "" {
		_ = a.db.QueryRow(ctx, "SELECT type FROM conversations WHERE id=$1", conversationID).Scan(&convType)
	}
	if convType == "group" && conversationID != "" {
		bundle.GroupMemories, bundle.GroupVibe = a.loadGroupMemories(ctx, conversationID, 5)
	}
	if strings.TrimSpace(userID) == "" {
		return bundle
	}
	subjectID, err := a.resolveSubject(ctx, botID, conversationID, convType, userID, userName, "")
	if err != nil || subjectID == "" {
		if err != nil {
			a.log.Warn("resolve subject failed", "error", err)
		}
		return bundle
	}
	bundle.SubjectID = subjectID
	if rec, err := a.loadSubjectRecord(ctx, subjectID); err == nil {
		bundle.Subject = rec
	}

	limit := s.MaxMemories
	if limit <= 0 {
		limit = 8
	}
	var qVec string
	var qDims int
	var embedPID string
	hasQuery := false
	if cl, pid, ok := a.embeddingModel(ctx, s); ok && strings.TrimSpace(query) != "" {
		var searchable bool
		err := a.db.QueryRow(ctx, `SELECT EXISTS(SELECT FROM user_memories WHERE subject_id=$1 AND status='active'
			AND (expires_at IS NULL OR expires_at>now()) AND (valid_until IS NULL OR valid_until>now())
			AND embedding IS NOT NULL AND embedding_profile_id=$2)`, subjectID, pid).Scan(&searchable)
		if searchable || err != nil {
			if v, e := embeddings.get(a, ctx, cl, pid, query); e == nil && len(v) > 0 {
				qVec = vectorLiteral(v)
				qDims = len(v)
				embedPID = pid
				hasQuery = true
			}
		}
	}

	var rowsSQL string
	var args []any
	if hasQuery {
		rowsSQL = `SELECT id::text,kind,content,importance,emotion_tag,
			CASE WHEN source_conversation_name<>'' THEN source_conversation_name ELSE source_bot_name END,
			to_char(coalesce(event_at,created_at) AT TIME ZONE 'UTC','YYYY-MM-DD'),
			pinned,resolved_at IS NOT NULL,access_count,
			CASE WHEN embedding IS NOT NULL AND embedding_profile_id=$2 AND vector_dims(embedding)=$4
				THEN GREATEST(0, 1 - (embedding <=> $3::vector))
				ELSE 0.45 END AS sim,
			coalesce(last_used_at,updated_at,created_at)
			FROM user_memories
			WHERE subject_id=$1 AND status='active' AND (expires_at IS NULL OR expires_at>now()) AND (valid_until IS NULL OR valid_until>now())
			ORDER BY pinned DESC, sim DESC, updated_at DESC LIMIT 80`
		args = []any{subjectID, embedPID, qVec, qDims}
	} else {
		rowsSQL = `SELECT id::text,kind,content,importance,emotion_tag,
			CASE WHEN source_conversation_name<>'' THEN source_conversation_name ELSE source_bot_name END,
			to_char(coalesce(event_at,created_at) AT TIME ZONE 'UTC','YYYY-MM-DD'),
			pinned,resolved_at IS NOT NULL,access_count,0.5::float8 AS sim,
			coalesce(last_used_at,updated_at,created_at)
			FROM user_memories
			WHERE subject_id=$1 AND status='active' AND (expires_at IS NULL OR expires_at>now()) AND (valid_until IS NULL OR valid_until>now())
			ORDER BY pinned DESC, importance DESC, updated_at DESC LIMIT 80`
		args = []any{subjectID}
	}

	rows, err := a.db.Query(ctx, rowsSQL, args...)
	if err != nil {
		a.log.Warn("load memories failed", "error", err)
	} else {
		defer rows.Close()
		var cands []memoryCandidate
		for rows.Next() {
			var mc memoryCandidate
			if rows.Scan(&mc.ID, &mc.Kind, &mc.Content, &mc.Importance, &mc.EmotionTag, &mc.SourceWhere, &mc.EventDate, &mc.Pinned, &mc.Resolved, &mc.AccessCount, &mc.Similarity, &mc.LastTouched) == nil {
				cands = append(cands, mc)
			}
		}
		ranked := rankCognitiveMemories(cands, limit, memoryFocus, hasQuery, time.Now())
		recalledIDs := make([]string, 0, len(ranked))
		for _, r := range ranked {
			recalledIDs = append(recalledIDs, r.ID)
			display := r.Content
			if r.EventDate != "" {
				display = r.EventDate + " · " + r.Content
			}
			bundle.Memories = append(bundle.Memories, agent.Memory{
				ID:          r.ID,
				Kind:        r.Kind,
				Content:     display,
				Importance:  r.Importance,
				EmotionTag:  r.EmotionTag,
				SourceWhere: r.SourceWhere,
				EventDate:   r.EventDate,
				Resolved:    r.Resolved,
				Score:       math.Round(r.Score*1000) / 1000,
			})
		}
		if len(recalledIDs) > 0 {
			_, _ = a.db.Exec(ctx, "UPDATE user_memories SET last_used_at=now(),access_count=access_count+1 WHERE id=ANY($1::uuid[])", recalledIDs)
		}
	}

	profile := strings.TrimSpace(bundle.Subject.ProfileContent)
	if profile == "" && conversationID != "" {
		_ = a.db.QueryRow(ctx, "SELECT content FROM user_profiles WHERE conversation_id=$1 AND platform_user_id=$2", conversationID, userID).Scan(&profile)
		profile = strings.TrimSpace(profile)
	}
	if profile != "" {
		bundle.Memories = append([]agent.Memory{{Kind: "profile", Content: "用户档案：" + profile}}, bundle.Memories...)
	}
	return bundle
}

// rankCognitiveMemories combines semantic relevance, Ebbinghaus retention decay,
// importance, focus boost, and resolved-issue penalty. Pinned memories are prioritized.
func rankCognitiveMemories(cands []memoryCandidate, limit int, memoryFocus string, hasQuery bool, now time.Time) []memoryCandidate {
	if limit <= 0 || len(cands) == 0 {
		return nil
	}
	for i := range cands {
		c := &cands[i]
		imp := c.Importance
		if imp < 1 {
			imp = 5
		} else if imp > 10 {
			imp = 10
		}
		days := now.Sub(c.LastTouched).Hours() / 24.0
		if days < 0 {
			days = 0
		}
		stabilityDays := 14.0 * (1.0 + math.Log(1.0+float64(max(0, c.AccessCount)))) * (0.4 + 0.12*float64(imp))
		retention := math.Exp(-days / stabilityDays)
		impNorm := float64(imp) / 10.0

		focusBoost := 0.0
		switch memoryFocus {
		case "inside_jokes":
			if c.Kind == "inside_joke" || c.Kind == "episode" {
				focusBoost = 0.20
			}
		case "episodes":
			if c.Kind == "episode" || c.Kind == "social" || c.Kind == "inside_joke" {
				focusBoost = 0.18
			}
		case "issues":
			if c.Kind == "issue" || c.Kind == "fact" {
				focusBoost = 0.18
			}
		case "facts":
			if c.Kind == "fact" || c.Kind == "preference" {
				focusBoost = 0.15
			}
		}

		var score float64
		if hasQuery {
			score = 0.52*c.Similarity + 0.25*retention + 0.23*impNorm + focusBoost
		} else {
			score = 0.45*retention + 0.35*impNorm + 0.20*c.Similarity + focusBoost
		}
		if c.Resolved {
			score *= 0.45
		}
		if c.Pinned {
			score += 10.0
		}
		c.Score = score
	}
	sort.SliceStable(cands, func(i, j int) bool {
		if cands[i].Score != cands[j].Score {
			return cands[i].Score > cands[j].Score
		}
		return cands[i].LastTouched.After(cands[j].LastTouched)
	})
	if len(cands) > limit {
		return cands[:limit]
	}
	return cands
}

func (a *App) loadGroupMemories(ctx context.Context, conversationID string, limit int) ([]agent.GroupMemoryItem, string) {
	if limit <= 0 {
		limit = 5
	}
	var vibe string
	_ = a.db.QueryRow(ctx, "SELECT COALESCE(group_vibe,'') FROM conversations WHERE id=$1", conversationID).Scan(&vibe)
	rows, err := a.db.Query(ctx, `SELECT id::text,kind,content,importance,to_char(created_at AT TIME ZONE 'UTC','YYYY-MM-DD')
		FROM group_memories WHERE conversation_id=$1 AND status='active' AND (expires_at IS NULL OR expires_at>now())
		ORDER BY importance DESC,updated_at DESC LIMIT $2`, conversationID, limit)
	if err != nil {
		return nil, vibe
	}
	defer rows.Close()
	var out []agent.GroupMemoryItem
	for rows.Next() {
		var item agent.GroupMemoryItem
		if rows.Scan(&item.ID, &item.Kind, &item.Content, &item.Importance, &item.EventDate) == nil {
			out = append(out, item)
		}
	}
	return out, vibe
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

func normalizeMemoryKind(kind string) string {
	switch kind {
	case "fact", "preference", "issue", "episode", "inside_joke", "social", "profile":
		return kind
	default:
		return "fact"
	}
}

type saveMemoryOptions struct {
	Importance int
	EmotionTag string
}

// saveMemory stores a memory unless it is sensitive or a near-duplicate.
func (a *App) saveMemory(ctx context.Context, s agent.Settings, botID, conversationID, userID, userName, kind, content string, confidence float64, source, messageID string, eventAt *time.Time, opts ...saveMemoryOptions) error {
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
	kind = normalizeMemoryKind(kind)
	importance := 5
	emotionTag := ""
	if len(opts) > 0 {
		if opts[0].Importance >= 1 && opts[0].Importance <= 10 {
			importance = opts[0].Importance
		}
		emotionTag = agent.TruncateRunes(strings.TrimSpace(opts[0].EmotionTag), 20)
	}

	var convType, convName, botName string
	if conversationID != "" {
		_ = a.db.QueryRow(ctx, `SELECT c.type,COALESCE(NULLIF(c.name,''),c.platform_id,''),COALESCE(b.name,'') FROM conversations c JOIN bots b ON b.id=c.bot_id WHERE c.id=$1`, conversationID).Scan(&convType, &convName, &botName)
	} else if botID != "" {
		_ = a.db.QueryRow(ctx, `SELECT COALESCE(name,'') FROM bots WHERE id=$1`, botID).Scan(&botName)
	}

	subjectID, err := a.resolveSubject(ctx, botID, conversationID, convType, userID, userName, "")
	if err != nil {
		return err
	}

	var exists bool
	if subjectID != "" {
		_ = a.db.QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM user_memories WHERE subject_id=$1 AND status='active' AND content=$2)", subjectID, content).Scan(&exists)
		if exists {
			_, err := a.db.Exec(ctx, "UPDATE user_memories SET event_at=coalesce($3,event_at),updated_at=now(),confidence=GREATEST(confidence,$4),importance=GREATEST(importance,$5),emotion_tag=COALESCE(NULLIF($6,''),emotion_tag) WHERE subject_id=$1 AND status='active' AND content=$2", subjectID, content, eventAt, confidence, importance, emotionTag)
			return err
		}
	}

	var embedding, profile any
	if cl, pid, ok := a.embeddingModel(ctx, s); ok {
		if v, err := a.embed(context.WithValue(ctx, usageScopeKey{}, usageScope{conversation: conversationID, message: messageID}), cl, pid, "embedding", []string{content}); err == nil && len(v) == 1 {
			if subjectID != "" && source != "admin" {
				var dup bool
				_ = a.db.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM user_memories WHERE subject_id=$1 AND status='active' AND embedding IS NOT NULL AND embedding_profile_id=$2 AND vector_dims(embedding)=$4 AND 1-(embedding <=> $3::vector) > 0.93)`, subjectID, pid, vectorLiteral(v[0]), len(v[0])).Scan(&dup)
				if dup {
					return nil
				}
			}
			embedding, profile = vectorLiteral(v[0]), pid
		}
	}
	var subjPtr any
	if subjectID != "" {
		subjPtr = subjectID
	}
	_, err = a.db.Exec(ctx, `INSERT INTO user_memories(bot_id,conversation_id,subject_id,platform_user_id,display_name,kind,content,embedding,embedding_profile_id,confidence,importance,emotion_tag,source,source_message_id,source_conversation_name,source_bot_name,event_at)
		VALUES($1,$2,$3,$4,$5,$6,$7,$8::vector,$9,$10,$11,$12,$13,$14,$15,$16,$17)`,
		botID, conversationID, subjPtr, userID, userName, kind, content, embedding, profile, confidence, importance, emotionTag, source, nullString(messageID), convName, botName, eventAt)
	if err == nil && subjectID != "" {
		// Keep up to 80 active non-pinned memories per global subject.
		_, _ = a.db.Exec(ctx, `UPDATE user_memories SET status='archived',updated_at=now() WHERE id IN (
			SELECT id FROM user_memories WHERE subject_id=$1 AND status='active' AND NOT pinned
			ORDER BY importance DESC, COALESCE(last_used_at,updated_at) DESC OFFSET 80)`, subjectID)
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
          FILTER (WHERE m.display_name<>''))[1],'') AS "displayName",
        (array_agg(m.subject_id::text) FILTER (WHERE m.subject_id IS NOT NULL))[1] AS "subjectId",
        sum(m.n) AS count
        FROM (SELECT bot_id,conversation_id,subject_id,platform_user_id,display_name,updated_at,status,1 AS n FROM user_memories
 UNION ALL SELECT c.bot_id,p.conversation_id,NULL::uuid,p.platform_user_id,p.display_name,p.updated_at,'active',0 FROM user_profiles p JOIN conversations c ON c.id=p.conversation_id WHERE p.content<>'') m JOIN bots b ON b.id=m.bot_id LEFT JOIN conversations c ON c.id=m.conversation_id
        WHERE m.status='active' GROUP BY m.bot_id,b.name,m.conversation_id,c.id,m.platform_user_id
        ORDER BY b.name,"conversationName","displayName",m.platform_user_id`)
}

func (a *App) listMemories(c *gin.Context) {
	args := []any{}
	where := []string{"m.status='active'"}
	if v := c.Query("subjectId"); v != "" {
		if parseUUID(v) != nil {
			fail(c, 400, "invalid_request", "无效的人物 ID")
			return
		}
		args = append(args, v)
		where = append(where, "m.subject_id=$"+itoa(len(args)))
	}
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
	queryList(c, a, `SELECT m.id::text,m.subject_id::text AS "subjectId",m.bot_id::text AS "botId",b.name AS "botName",
		m.conversation_id::text AS "conversationId",m.source_conversation_name AS "sourceConversationName",m.source_bot_name AS "sourceBotName",
		m.platform_user_id AS "userId",m.display_name AS "displayName",m.kind,m.content,(m.embedding IS NOT NULL) AS "hasEmbedding",
		m.confidence,m.importance,m.emotion_tag AS "emotionTag",m.access_count AS "accessCount",m.resolved_at AS "resolvedAt",
		m.pinned,m.source,m.source_message_id::text AS "sourceMessageId",m.expires_at AS "expiresAt",m.last_used_at AS "lastUsedAt",
		coalesce(m.event_at,m.created_at) AS "eventAt",m.created_at AS "createdAt",m.updated_at AS "updatedAt"
		FROM user_memories m JOIN bots b ON b.id=m.bot_id WHERE `+strings.Join(where, " AND ")+` ORDER BY m.pinned DESC, m.updated_at DESC LIMIT 300`, args...)
}

func (a *App) createMemory(c *gin.Context) {
	var in struct {
		EventAt                                                   *time.Time `json:"eventAt"`
		BotID, ConversationID, UserID, DisplayName, Kind, Content string
		SubjectID                                                 string `json:"subjectId"`
		Importance                                                int    `json:"importance"`
		EmotionTag                                                string `json:"emotionTag"`
		Pinned                                                    bool
	}
	if c.ShouldBindJSON(&in) != nil || strings.TrimSpace(in.Content) == "" {
		fail(c, 400, "invalid_request", "内容必填")
		return
	}
	if privateMemory(in.Content) || len([]rune(in.Content)) > 300 {
		fail(c, 400, "invalid_memory", "内容过长或含敏感信息")
		return
	}
	if in.SubjectID != "" && (in.BotID == "" || in.ConversationID == "" || in.UserID == "") {
		_ = a.db.QueryRow(c, `SELECT COALESCE(l.bot_id::text,cv.bot_id::text,''),COALESCE(l.last_conversation_id::text,''),l.identity_value,COALESCE(NULLIF(l.display_name,''),s.display_name,'')
			FROM memory_subjects s JOIN user_identity_links l ON l.subject_id=s.id LEFT JOIN conversations cv ON cv.id=l.last_conversation_id
			WHERE s.id=$1 AND l.identity_type<>'qq_union_id' ORDER BY l.updated_at DESC LIMIT 1`, in.SubjectID).Scan(&in.BotID, &in.ConversationID, &in.UserID, &in.DisplayName)
	}
	if in.BotID == "" || in.ConversationID == "" || in.UserID == "" {
		fail(c, 400, "invalid_request", "机器人、会话、用户和内容必填")
		return
	}
	var valid bool
	if err := a.db.QueryRow(c, "SELECT EXISTS(SELECT 1 FROM conversations WHERE id=$1 AND bot_id=$2)", in.ConversationID, in.BotID).Scan(&valid); err != nil || !valid {
		fail(c, 400, "invalid_conversation", "会话与机器人不匹配")
		return
	}
	s := a.agentSettings(c)
	if err := a.saveMemory(c, s, in.BotID, in.ConversationID, in.UserID, in.DisplayName, in.Kind, in.Content, 1, "admin", "", in.EventAt, saveMemoryOptions{Importance: in.Importance, EmotionTag: in.EmotionTag}); err != nil {
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
		EventAt    *time.Time `json:"eventAt"`
		Content    string     `json:"content"`
		Kind       string     `json:"kind"`
		Pinned     bool       `json:"pinned"`
		Importance *int       `json:"importance"`
		EmotionTag *string    `json:"emotionTag"`
		Resolved   *bool      `json:"resolved"`
	}
	if c.ShouldBindJSON(&in) != nil || strings.TrimSpace(in.Content) == "" {
		fail(c, 400, "invalid_request", "内容必填")
		return
	}
	if privateMemory(in.Content) || len([]rune(in.Content)) > 300 {
		fail(c, 400, "invalid_memory", "内容过长或含敏感信息")
		return
	}
	var kind any
	if strings.TrimSpace(in.Kind) != "" {
		kind = normalizeMemoryKind(in.Kind)
	}
	var imp any
	if in.Importance != nil && *in.Importance >= 1 && *in.Importance <= 10 {
		imp = *in.Importance
	}
	var emo any
	if in.EmotionTag != nil {
		emo = agent.TruncateRunes(strings.TrimSpace(*in.EmotionTag), 20)
	}
	tag, err := a.db.Exec(c, `UPDATE user_memories SET content=$1,kind=COALESCE($2,kind),pinned=$3,embedding=NULL,event_at=coalesce($5,event_at),
		importance=COALESCE($6,importance),emotion_tag=COALESCE($7,emotion_tag),
		resolved_at=CASE WHEN $8::bool IS TRUE THEN COALESCE(resolved_at,now()) WHEN $8::bool IS FALSE THEN NULL ELSE resolved_at END,
		updated_at=now() WHERE id=$4`, strings.TrimSpace(in.Content), kind, in.Pinned, c.Param("id"), in.EventAt, imp, emo, in.Resolved)
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
