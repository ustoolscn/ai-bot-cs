package app

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"sort"
	"strings"
	"time"

	"ai-bot/backend/internal/agent"
	"github.com/gin-gonic/gin"
)

type reflectionResult struct {
	Profile        *string        `json:"profile"`
	Nicknames      []string       `json:"nicknames"`
	ArchiveIDs     []string       `json:"archive_ids"`
	MergedMemories []struct {
		Kind       string  `json:"kind"`
		Content    string  `json:"content"`
		Importance int     `json:"importance"`
		Confidence float64 `json:"confidence"`
	} `json:"merged_memories"`
	Persona map[string]any `json:"persona"`
}

// jobReflectUser performs slow-path cognitive consolidation for a global subject:
// synthesizes profile narrative, updates bigFive/communication persona incrementally,
// merges fragmented insights, and archives stale low-value trivia.
func (a *App) jobReflectUser(ctx context.Context, subjectID, profileID string) error {
	if parseUUID(subjectID) != nil {
		return nil
	}
	rec, err := a.loadSubjectRecord(ctx, subjectID)
	if err != nil {
		return nil // subject deleted or merged away
	}
	s := a.agentSettings(ctx)
	ctx = context.WithValue(ctx, usageScopeKey{}, usageScope{})

	rows, err := a.db.Query(ctx, `SELECT id::text,kind,content,importance,emotion_tag,access_count,pinned,resolved_at IS NOT NULL,
		coalesce(event_at,created_at) FROM user_memories
		WHERE subject_id=$1 AND status='active' ORDER BY pinned DESC, importance DESC, updated_at DESC LIMIT 60`, subjectID)
	if err != nil {
		return err
	}
	defer rows.Close()
	type memRow struct {
		ID         string `json:"id"`
		Kind       string `json:"kind"`
		Content    string `json:"content"`
		Importance int    `json:"importance"`
		EmotionTag string `json:"emotionTag,omitempty"`
		Hits       int    `json:"hits"`
		Pinned     bool   `json:"pinned"`
		Resolved   bool   `json:"resolved,omitempty"`
		Date       string `json:"date"`
	}
	var mems []memRow
	pinnedIDs := map[string]bool{}
	validIDs := map[string]bool{}
	for rows.Next() {
		var r memRow
		var at time.Time
		if rows.Scan(&r.ID, &r.Kind, &r.Content, &r.Importance, &r.EmotionTag, &r.Hits, &r.Pinned, &r.Resolved, &at) == nil {
			r.Date = at.Format("2006-01-02")
			mems = append(mems, r)
			validIDs[r.ID] = true
			if r.Pinned {
				pinnedIDs[r.ID] = true
			}
		}
	}
	rows.Close()
	if len(mems) == 0 && rec.ProfileContent == "" {
		_, _ = a.db.Exec(ctx, "UPDATE memory_subjects SET turns_since_reflection=0,last_reflected_at=now() WHERE id=$1", subjectID)
		return nil
	}

	personaJSON, _ := json.Marshal(rec.Persona)
	memsJSON, _ := json.Marshal(mems)
	var b strings.Builder
	fmt.Fprintf(&b, "人物昵称：%s\n关系阶段：%s（互动 %d 轮，跨 %d 天，熟悉度 %.0f，信任度 %.0f）\n",
		rec.DisplayName, rec.RelationshipStage, rec.InteractionTurns, rec.InteractionDays, rec.Familiarity, rec.Trust)
	fmt.Fprintf(&b, "已有档案：%s\n已有结构化画像：%s\n当前活跃记忆（%d 条）：%s",
		rec.ProfileContent, string(personaJSON), len(mems), string(memsJSON))

	var res reflectionResult
	if err := a.auxJSON(ctx, s, profileID, "", "", "reflect_user", agent.ReflectionPrompt+personaPrompt, b.String(), &res); err != nil {
		return err
	}

	// Precompute embeddings for any consolidated insights before acquiring lock.
	vectors := map[int]string{}
	var embedProfile any
	if len(res.MergedMemories) > 0 {
		if cl, pid, ok := a.embeddingModel(ctx, s); ok {
			for i, m := range res.MergedMemories {
				if i >= 4 {
					break
				}
				c := strings.TrimSpace(m.Content)
				if c != "" && !privateMemory(c) && len([]rune(c)) <= 300 {
					if v, e := a.embed(ctx, cl, pid, "embedding", []string{c}); e == nil && len(v) == 1 {
						vectors[i] = vectorLiteral(v[0])
						embedProfile = pid
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
	if _, err = tx.Exec(ctx, "SELECT id FROM memory_subjects WHERE id=$1 FOR UPDATE", subjectID); err != nil {
		return err
	}

	newProfile := rec.ProfileContent
	if res.Profile != nil && strings.TrimSpace(*res.Profile) != "" && !privateMemory(*res.Profile) {
		runes := []rune(strings.TrimSpace(*res.Profile))
		limit := sort.Search(len(runes), func(i int) bool { return agent.EstimateTokens(string(runes[:i+1])) > 2000 })
		newProfile = string(runes[:limit])
	}

	nicks := slices.Clone(rec.Nicknames)
	for _, n := range res.Nicknames {
		n = strings.TrimSpace(n)
		if n != "" && !privateMemory(n) && len([]rune(n)) <= 16 && !slices.Contains(nicks, n) && len(nicks) < 6 {
			nicks = append(nicks, n)
		}
	}

	var nextPersona any
	if len(res.Persona) > 0 {
		if merged := mergePersona(rec.Persona, res.Persona, time.Now()); merged != nil {
			raw, _ := json.Marshal(merged)
			nextPersona = string(raw)
		}
	}
	if nextPersona == nil && len(rec.Persona) > 0 {
		raw, _ := json.Marshal(rec.Persona)
		nextPersona = string(raw)
	}

	nickJSON, _ := json.Marshal(nonNilStrings(nicks))
	if _, err = tx.Exec(ctx, `UPDATE memory_subjects SET
		profile_content=$2,nicknames=$3::jsonb,persona=COALESCE($4::jsonb,persona),
		turns_since_reflection=0,last_reflected_at=now(),updated_at=now()
		WHERE id=$1`, subjectID, newProfile, string(nickJSON), nextPersona); err != nil {
		return err
	}

	// Mirror profile & persona to linked user_profiles rows for backward compatibility.
	_, _ = tx.Exec(ctx, `UPDATE user_profiles p SET content=$2,persona=COALESCE($3::jsonb,p.persona),updated_at=now()
		FROM user_identity_links l JOIN conversations c ON c.bot_id=l.bot_id
		WHERE l.subject_id=$1 AND l.bot_id IS NOT NULL AND p.conversation_id=c.id AND p.platform_user_id=l.identity_value`,
		subjectID, newProfile, nextPersona)

	for _, id := range res.ArchiveIDs {
		if validIDs[id] && !pinnedIDs[id] {
			_, _ = tx.Exec(ctx, "UPDATE user_memories SET status='archived',updated_at=now() WHERE id=$1 AND subject_id=$2 AND NOT pinned", id, subjectID)
		}
	}

	// Lookup a fallback bot/user link so merged memories retain valid legacy NOT NULL bot_id/platform_user_id.
	var fallbackBot, fallbackUser, fallbackConv string
	_ = tx.QueryRow(ctx, `SELECT COALESCE(bot_id::text,''),identity_value,COALESCE(last_conversation_id::text,'')
		FROM user_identity_links WHERE subject_id=$1 AND bot_id IS NOT NULL ORDER BY updated_at DESC LIMIT 1`, subjectID).
		Scan(&fallbackBot, &fallbackUser, &fallbackConv)

	if fallbackBot != "" && fallbackUser != "" {
		for i, m := range res.MergedMemories {
			if i >= 4 {
				break
			}
			content := strings.TrimSpace(m.Content)
			if content == "" || privateMemory(content) || len([]rune(content)) > 300 || m.Confidence < 0.6 {
				continue
			}
			kind := normalizeMemoryKind(m.Kind)
			imp := m.Importance
			if imp < 1 || imp > 10 {
				imp = 7
			}
			var convPtr any
			if parseUUID(fallbackConv) == nil {
				convPtr = fallbackConv
			}
			_, err = tx.Exec(ctx, `INSERT INTO user_memories(bot_id,conversation_id,subject_id,platform_user_id,display_name,kind,content,confidence,importance,source,event_at,embedding,embedding_profile_id)
				SELECT $1,$2,$3,$4,$5,$6,$7,$8,$9,'reflection',now(),$10::vector,$11
				WHERE NOT EXISTS(SELECT 1 FROM user_memories WHERE subject_id=$3 AND status='active' AND content=$7)`,
				fallbackBot, convPtr, subjectID, fallbackUser, rec.DisplayName, kind, content, m.Confidence, imp, nullString(vectors[i]), embedProfile)
			if err != nil {
				return err
			}
		}
	}

	return tx.Commit(ctx)
}

func (a *App) triggerSubjectReflectionAPI(c *gin.Context) {
	subjectID := c.Param("id")
	if parseUUID(subjectID) != nil {
		fail(c, 400, "invalid_id", "无效的人物 ID")
		return
	}
	var exists bool
	if err := a.db.QueryRow(c, "SELECT EXISTS(SELECT 1 FROM memory_subjects WHERE id=$1)", subjectID).Scan(&exists); err != nil || !exists {
		fail(c, 404, "not_found", "人物不存在")
		return
	}
	if err := a.jobReflectUser(c.Request.Context(), subjectID, ""); err != nil {
		fail(c, 502, "reflection_failed", "反思整理失败："+err.Error())
		return
	}
	a.audit(c, "subject.reflect", "memory_subject", subjectID, nil)
	ok(c, gin.H{"reflected": true})
}
