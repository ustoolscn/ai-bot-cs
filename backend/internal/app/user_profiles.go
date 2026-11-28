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
	var persona json.RawMessage
	var content string
	var updated *time.Time
	var botID, convType string
	if err := a.db.QueryRow(c, "SELECT bot_id::text,type FROM conversations WHERE id=$1", cid).Scan(&botID, &convType); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			fail(c, 404, "not_found", "会话不存在")
			return
		}
		fail(c, 500, "database_error", "读取会话失败")
		return
	}
	var err error
	switch c.Request.Method {
	case "GET":
		err = a.db.QueryRow(c, `SELECT coalesce(p.content,''),p.updated_at,p.persona FROM conversations c LEFT JOIN user_profiles p ON p.conversation_id=c.id AND p.platform_user_id=$2 WHERE c.id=$1`, cid, uid).Scan(&content, &updated, &persona)
		if err == nil {
			if subjID, e := a.resolveSubject(c, botID, cid, convType, uid, "", ""); e == nil && subjID != "" {
				var sContent string
				var sUpdated *time.Time
				var sPersona json.RawMessage
				if a.db.QueryRow(c, "SELECT profile_content,updated_at,persona FROM memory_subjects WHERE id=$1", subjID).Scan(&sContent, &sUpdated, &sPersona) == nil {
					if sContent != "" || content == "" {
						content = sContent
					}
					if len(sPersona) > 0 {
						persona = sPersona
					}
					if sUpdated != nil {
						updated = sUpdated
					}
				}
			}
		}
	case "PUT":
		var in struct{ Content *string }
		if c.ShouldBindJSON(&in) != nil || in.Content == nil || privateMemory(*in.Content) || agent.EstimateTokens(*in.Content) > 2000 {
			fail(c, 400, "invalid_profile", "档案须在约 2000 token 以内，且不得含敏感信息")
			return
		}
		content = strings.TrimSpace(*in.Content)
		err = a.db.QueryRow(c, `INSERT INTO user_profiles(conversation_id,platform_user_id,content) SELECT id,$2,$3 FROM conversations WHERE id=$1 ON CONFLICT(conversation_id,platform_user_id) DO UPDATE SET content=excluded.content,updated_at=now() RETURNING updated_at,persona`, cid, uid, content).Scan(&updated, &persona)
		if err == nil {
			if subjID, e := a.resolveSubject(c, botID, cid, convType, uid, "", ""); e == nil && subjID != "" {
				_, _ = a.db.Exec(c, "UPDATE memory_subjects SET profile_content=$2,updated_at=now() WHERE id=$1", subjID, content)
			}
		}
	case "DELETE":
		_, err = a.db.Exec(c, "DELETE FROM user_profiles WHERE conversation_id=$1 AND platform_user_id=$2", cid, uid)
		if err == nil {
			if subjID, e := a.resolveSubject(c, botID, cid, convType, uid, "", ""); e == nil && subjID != "" {
				_, _ = a.db.Exec(c, "UPDATE memory_subjects SET profile_content='',persona=NULL,updated_at=now() WHERE id=$1", subjID)
			}
		}
	}
	if errors.Is(err, pgx.ErrNoRows) {
		fail(c, 404, "not_found", "会话不存在")
		return
	}
	if err != nil {
		fail(c, 500, "database_error", "读写档案失败")
		return
	}
	ok(c, gin.H{"persona": persona, "conversationId": cid, "userId": uid, "content": content, "updatedAt": updated})
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
					if v, err := a.embed(ctx, cl, pid, "embedding", []string{m.Content}); err == nil && len(v) == 1 {
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
	var convType, convName, botName string
	_ = tx.QueryRow(ctx, `SELECT c.type,COALESCE(NULLIF(c.name,''),c.platform_id,''),COALESCE(b.name,'') FROM conversations c JOIN bots b ON b.id=c.bot_id WHERE c.id=$1`, cid).Scan(&convType, &convName, &botName)
	subjectID, err := a.resolveSubjectTx(ctx, tx, bot, cid, convType, uid, name, "")
	if err != nil {
		return err
	}
	lockKey := cid + "/" + uid
	if subjectID != "" {
		lockKey = "subject:" + subjectID
	}
	if _, err = tx.Exec(ctx, "SELECT pg_advisory_xact_lock(hashtextextended($1,0))", lockKey); err != nil {
		return err
	}
	var excluded, personaApplied bool
	if err = tx.QueryRow(ctx, "SELECT context_excluded,persona_applied FROM messages WHERE id=$1 FOR SHARE", mid).Scan(&excluded, &personaApplied); err != nil {
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
		if subjectID != "" {
			_, _ = tx.Exec(ctx, "UPDATE memory_subjects SET profile_content=$2,updated_at=now() WHERE id=$1", subjectID, string(profile))
		}
	}
	if !personaApplied && len(ev.Persona) > 0 {
		var old map[string]any
		err = tx.QueryRow(ctx, `SELECT (SELECT persona FROM user_profiles WHERE conversation_id=$1 AND platform_user_id=$2)`, cid, uid).Scan(&old)
		if err != nil {
			return err
		}
		if next := mergePersona(old, ev.Persona, time.Now()); next != nil {
			if _, err = tx.Exec(ctx, "UPDATE messages SET persona_applied=true WHERE id=$1", mid); err != nil {
				return err
			}
			_, err = tx.Exec(ctx, `INSERT INTO user_profiles(conversation_id,platform_user_id,display_name,persona) VALUES($1,$2,$3,$4) ON CONFLICT(conversation_id,platform_user_id) DO UPDATE SET persona=excluded.persona,updated_at=now()`, cid, uid, name, next)
			if err != nil {
				return err
			}
			if subjectID != "" {
				_, _ = tx.Exec(ctx, "UPDATE memory_subjects SET persona=$2,updated_at=now() WHERE id=$1", subjectID, next)
			}
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
		switch m.Action {
		case "delete":
			if found {
				_, err = tx.Exec(ctx, "UPDATE user_memories SET status='archived',updated_at=now() WHERE id=$1 AND (subject_id=$2 OR (conversation_id=$3 AND platform_user_id=$4)) AND content=$5 AND NOT pinned", m.ID, subjectID, cid, uid, old)
			}
		case "resolve":
			if found {
				_, err = tx.Exec(ctx, "UPDATE user_memories SET resolved_at=COALESCE(resolved_at,now()),content=COALESCE(NULLIF($5,''),content),updated_at=now() WHERE id=$1 AND (subject_id=$2 OR (conversation_id=$3 AND platform_user_id=$4)) AND status='active'", m.ID, subjectID, cid, uid, content)
			}
		default:
			if content != "" && !privateMemory(content) && len([]rune(content)) <= 300 {
				event, _ := time.Parse(time.RFC3339, m.EventAt)
				imp := m.Importance
				if imp < 1 || imp > 10 {
					imp = 5
				}
				emo := agent.TruncateRunes(strings.TrimSpace(m.EmotionTag), 20)
				switch m.Action {
				case "", "add":
					if event.IsZero() {
						event = at
					}
					kind := normalizeMemoryKind(m.Kind)
					if kind == "profile" {
						kind = "fact"
					}
					_, err = tx.Exec(ctx, `INSERT INTO user_memories(bot_id,conversation_id,subject_id,platform_user_id,display_name,kind,content,confidence,importance,emotion_tag,source,source_message_id,source_conversation_name,source_bot_name,event_at,embedding,embedding_profile_id)
						SELECT $1,$2,$3,$4,$5,$6,$7,$8,$9,$10,'extracted',$11,$12,$13,$14,$15::vector,$16
						WHERE NOT EXISTS(SELECT FROM user_memories WHERE (subject_id=$3 OR (conversation_id=$2 AND platform_user_id=$4)) AND status='active' AND content=$7)`,
						bot, cid, subjectID, uid, name, kind, content, m.Confidence, imp, emo, mid, convName, botName, event, nullString(vectors[i]), embeddingProfile)
				case "update":
					if found {
						var when any
						if !event.IsZero() {
							when = event
						}
						_, err = tx.Exec(ctx, `UPDATE user_memories SET content=$4,updated_at=now(),event_at=coalesce($5,event_at),importance=GREATEST(importance,$9),emotion_tag=COALESCE(NULLIF($10,''),emotion_tag),embedding=$7::vector,embedding_profile_id=$8
							WHERE id=$1 AND (subject_id=$2 OR (conversation_id=$11 AND platform_user_id=$3)) AND content=$6 AND NOT pinned AND status='active'`,
							m.ID, subjectID, uid, content, when, old, nullString(vectors[i]), embeddingProfile, imp, emo, cid)
					}
				}
			}
		}
		if err != nil {
			return err
		}
	}
	if subjectID != "" {
		_, err = tx.Exec(ctx, `UPDATE user_memories SET status='archived' WHERE id IN (
			SELECT id FROM user_memories WHERE subject_id=$1 AND status='active' AND NOT pinned
			ORDER BY importance DESC, COALESCE(last_used_at,updated_at) DESC, id OFFSET 80)`, subjectID)
	} else {
		_, err = tx.Exec(ctx, `UPDATE user_memories SET status='archived' WHERE id IN (SELECT id FROM user_memories WHERE conversation_id=$1 AND platform_user_id=$2 AND status='active' AND NOT pinned ORDER BY updated_at DESC,id OFFSET 50)`, cid, uid)
	}
	if err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func (a *App) advanceSubjectRelationship(ctx context.Context, subjectID, convID, botID, profileID string, eventAt time.Time, ev evaluation, perception TurnPerception, s agent.Settings) {
	if subjectID == "" {
		return
	}
	dayCST := eventAt.In(chinaLocation).Format("2006-01-02")
	tx, err := a.db.Begin(ctx)
	if err != nil {
		return
	}
	defer tx.Rollback(ctx)
	var fam, aff, trust float64
	var turns, days, sinceRef int
	var lastDay *time.Time
	var lastRef *time.Time
	if err = tx.QueryRow(ctx, `SELECT familiarity,affinity,trust,interaction_turns,interaction_days,last_interaction_day,turns_since_reflection,last_reflected_at FROM memory_subjects WHERE id=$1 FOR UPDATE`, subjectID).
		Scan(&fam, &aff, &trust, &turns, &days, &lastDay, &sinceRef, &lastRef); err != nil {
		return
	}
	turns++
	if lastDay == nil || lastDay.Format("2006-01-02") != dayCST {
		days++
	}
	sinceRef++

	fam = minFloat(100, fam+2.2)
	switch ev.MoodDelta {
	case "positive":
		aff = clampFloat(aff+2.0, 0, 100)
		trust = clampFloat(trust+1.5, 0, 100)
	case "negative":
		aff = clampFloat(aff-1.5, 0, 100)
		trust = clampFloat(trust-1.0, 0, 100)
	default:
		if ev.Answered {
			aff = clampFloat(aff+0.5, 0, 100)
			trust = clampFloat(trust+0.6, 0, 100)
		}
	}
	if perception.Valence >= 0.4 {
		aff = clampFloat(aff+0.8, 0, 100)
	}
	stage := computeRelationshipStage(turns, fam, aff, trust)

	if _, err = tx.Exec(ctx, `UPDATE memory_subjects SET
		familiarity=$2,affinity=$3,trust=$4,relationship_stage=$5,
		interaction_turns=$6,interaction_days=$7,last_interaction_day=$8::date,
		last_interaction_at=$9,turns_since_reflection=$10,updated_at=now()
		WHERE id=$1`, subjectID, fam, aff, trust, stage, turns, days, dayCST, eventAt, sinceRef); err != nil {
		return
	}
	if err = tx.Commit(ctx); err != nil {
		return
	}

	interval := s.Companion.ReflectionIntervalTurns
	if interval <= 0 {
		interval = 6
	}
	if sinceRef >= interval || (lastRef == nil && turns >= 3) {
		a.enqueueJob(ctx, "user_reflection", map[string]any{"subjectId": subjectID, "profileId": profileID}, "reflect:"+subjectID)
	}
}

func (a *App) scheduleProactiveCandidate(ctx context.Context, subjectID, botID, convID, kind, topic string, delayHours float64) {
	topic = agent.TruncateRunes(strings.TrimSpace(topic), 120)
	if topic == "" || privateMemory(topic) {
		return
	}
	if kind != "issue_followup" && kind != "event_care" && kind != "milestone" {
		kind = "event_care"
	}
	if delayHours <= 0.5 {
		delayHours = 4
	} else if delayHours > 72 {
		delayHours = 72
	}
	scheduled := time.Now().Add(time.Duration(delayHours * float64(time.Hour)))
	var dup bool
	_ = a.db.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM proactive_triggers WHERE conversation_id=$1 AND ($2='' OR subject_id::text=$2) AND status='pending' AND topic=$3)`, convID, subjectID, topic).Scan(&dup)
	if dup {
		return
	}
	var subjPtr any
	if subjectID != "" {
		subjPtr = subjectID
	}
	_, _ = a.db.Exec(ctx, `INSERT INTO proactive_triggers(subject_id,bot_id,conversation_id,kind,topic,scheduled_at) VALUES($1,$2,$3,$4,$5,$6)`,
		subjPtr, botID, convID, kind, topic, scheduled)
}

func minFloat(a, b float64) float64 {
	if a < b {
		return a
	}
	return b
}

func clampFloat(v, lo, hi float64) float64 {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}
