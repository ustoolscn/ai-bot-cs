package app

import (
	"context"
	"encoding/json"
	"errors"
	"math"
	"slices"
	"strings"
	"time"

	"ai-bot/backend/internal/agent"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// subjectRecord holds the cross-bot unified person state loaded for a turn or admin view.
type subjectRecord struct {
	ID                   string         `json:"id"`
	DisplayName          string         `json:"displayName"`
	Nicknames            []string       `json:"nicknames"`
	ProfileContent       string         `json:"profileContent"`
	Persona              map[string]any `json:"persona,omitempty"`
	Familiarity          float64        `json:"familiarity"`
	Affinity             float64        `json:"affinity"`
	Trust                float64        `json:"trust"`
	RelationshipStage    string         `json:"relationshipStage"`
	InteractionTurns     int            `json:"interactionTurns"`
	InteractionDays      int            `json:"interactionDays"`
	TurnsSinceReflection int            `json:"turnsSinceReflection"`
	LastReflectedAt      *time.Time     `json:"lastReflectedAt,omitempty"`
	LastInteractionAt    *time.Time     `json:"lastInteractionAt,omitempty"`
	MoodState            userMoodState  `json:"moodState"`
}

// userMoodState tracks a person's emotional inertia across turns.
type userMoodState struct {
	CurrentMood    string         `json:"currentMood,omitempty"`
	Intensity      float64        `json:"intensity,omitempty"`
	Valence        float64        `json:"valence,omitempty"`
	NegativeStreak int            `json:"negativeStreak,omitempty"`
	UpdatedAt      *time.Time     `json:"updatedAt,omitempty"`
	History        []moodSnapshot `json:"history,omitempty"`
}

type moodSnapshot struct {
	Mood      string    `json:"mood"`
	Valence   float64   `json:"valence"`
	Intensity float64   `json:"intensity"`
	At        time.Time `json:"at"`
}

func identityTypeForConversation(convType string) string {
	if convType == "private" {
		return "user_openid"
	}
	return "member_openid"
}

// resolveSubject resolves or creates the global memory_subjects row for a QQ user,
// automatically linking and merging across bots whenever qq_union_id is available.
func (a *App) resolveSubject(ctx context.Context, botID, convID, convType, platformUserID, displayName, unionID string) (string, error) {
	tx, err := a.db.Begin(ctx)
	if err != nil {
		return "", err
	}
	defer tx.Rollback(ctx)
	id, err := a.resolveSubjectTx(ctx, tx, botID, convID, convType, platformUserID, displayName, unionID)
	if err != nil {
		return "", err
	}
	if err = tx.Commit(ctx); err != nil {
		return "", err
	}
	return id, nil
}

func (a *App) resolveSubjectTx(ctx context.Context, tx pgx.Tx, botID, convID, convType, platformUserID, displayName, unionID string) (string, error) {
	platformUserID = strings.TrimSpace(platformUserID)
	unionID = strings.TrimSpace(unionID)
	displayName = strings.TrimSpace(displayName)
	if platformUserID == "" && unionID == "" {
		return "", nil
	}
	if convType == "" && convID != "" {
		_ = tx.QueryRow(ctx, "SELECT type FROM conversations WHERE id=$1", convID).Scan(&convType)
	}
	if unionID == "" && convID != "" && platformUserID != "" {
		_ = tx.QueryRow(ctx, "SELECT COALESCE(qq_union_id,'') FROM conversation_members WHERE conversation_id=$1 AND platform_user_id=$2", convID, platformUserID).Scan(&unionID)
		unionID = strings.TrimSpace(unionID)
	}
	if botID == "" && convID != "" {
		_ = tx.QueryRow(ctx, "SELECT bot_id::text FROM conversations WHERE id=$1", convID).Scan(&botID)
	}
	idType := identityTypeForConversation(convType)

	// Serialize concurrent resolution for the same union or bot-scoped user.
	if unionID != "" {
		if _, err := tx.Exec(ctx, "SELECT pg_advisory_xact_lock(hashtextextended($1,0))", "union:"+unionID); err != nil {
			return "", err
		}
	}
	if botID != "" && platformUserID != "" {
		if _, err := tx.Exec(ctx, "SELECT pg_advisory_xact_lock(hashtextextended($1,0))", "botuser:"+botID+":"+idType+":"+platformUserID); err != nil {
			return "", err
		}
	}

	var unionSubject, botSubject string
	if unionID != "" {
		_ = tx.QueryRow(ctx, "SELECT subject_id::text FROM user_identity_links WHERE identity_type='qq_union_id' AND identity_value=$1", unionID).Scan(&unionSubject)
	}
	if botID != "" && platformUserID != "" {
		_ = tx.QueryRow(ctx, "SELECT subject_id::text FROM user_identity_links WHERE bot_id=$1 AND identity_type=$2 AND identity_value=$3", botID, idType, platformUserID).Scan(&botSubject)
	}

	subjectID := ""
	switch {
	case unionSubject != "" && botSubject != "" && unionSubject != botSubject:
		primary, secondary := choosePrimarySubject(ctx, tx, unionSubject, botSubject)
		if err := a.mergeSubjectsTx(ctx, tx, primary, secondary); err != nil {
			return "", err
		}
		subjectID = primary
	case unionSubject != "":
		subjectID = unionSubject
	case botSubject != "":
		subjectID = botSubject
	default:
		var profileContent string
		var personaRaw []byte
		if convID != "" && platformUserID != "" {
			_ = tx.QueryRow(ctx, "SELECT coalesce(content,''),persona FROM user_profiles WHERE conversation_id=$1 AND platform_user_id=$2", convID, platformUserID).Scan(&profileContent, &personaRaw)
		}
		if err := tx.QueryRow(ctx, `INSERT INTO memory_subjects(display_name,profile_content,persona) VALUES($1,$2,$3) RETURNING id::text`, displayName, profileContent, personaRaw).Scan(&subjectID); err != nil {
			return "", err
		}
	}

	if displayName != "" {
		_, _ = tx.Exec(ctx, `UPDATE memory_subjects SET display_name=CASE WHEN display_name='' THEN $2 ELSE display_name END,updated_at=now() WHERE id=$1`, subjectID, displayName)
	}
	var convPtr any
	if uuid.Validate(convID) == nil {
		convPtr = convID
	}
	if unionID != "" {
		_, err := tx.Exec(ctx, `INSERT INTO user_identity_links(subject_id,bot_id,identity_type,identity_value,last_conversation_id,display_name)
			VALUES($1,NULL,'qq_union_id',$2,$3,$4)
			ON CONFLICT(identity_value) WHERE identity_type='qq_union_id' DO UPDATE SET
			last_conversation_id=COALESCE(EXCLUDED.last_conversation_id,user_identity_links.last_conversation_id),
			display_name=CASE WHEN EXCLUDED.display_name<>'' THEN EXCLUDED.display_name ELSE user_identity_links.display_name END,
			updated_at=now()`, subjectID, unionID, convPtr, displayName)
		if err != nil {
			return "", err
		}
	}
	if botID != "" && platformUserID != "" {
		_, err := tx.Exec(ctx, `INSERT INTO user_identity_links(subject_id,bot_id,identity_type,identity_value,last_conversation_id,display_name)
			VALUES($1,$2,$3,$4,$5,$6)
			ON CONFLICT(bot_id,identity_type,identity_value) WHERE bot_id IS NOT NULL DO UPDATE SET
			last_conversation_id=COALESCE(EXCLUDED.last_conversation_id,user_identity_links.last_conversation_id),
			display_name=CASE WHEN EXCLUDED.display_name<>'' THEN EXCLUDED.display_name ELSE user_identity_links.display_name END,
			updated_at=now()`, subjectID, botID, idType, platformUserID, convPtr, displayName)
		if err != nil {
			return "", err
		}
		// Attach any legacy unlinked memories for this bot+user.
		_, _ = tx.Exec(ctx, `UPDATE user_memories SET subject_id=$1 WHERE bot_id=$2 AND platform_user_id=$3 AND subject_id IS NULL`, subjectID, botID, platformUserID)
	}
	return subjectID, nil
}

func choosePrimarySubject(ctx context.Context, tx pgx.Tx, aID, bID string) (string, string) {
	var aTurns, bTurns int
	var aAt, bAt time.Time
	_ = tx.QueryRow(ctx, "SELECT interaction_turns,created_at FROM memory_subjects WHERE id=$1", aID).Scan(&aTurns, &aAt)
	_ = tx.QueryRow(ctx, "SELECT interaction_turns,created_at FROM memory_subjects WHERE id=$1", bID).Scan(&bTurns, &bAt)
	if aTurns > bTurns || (aTurns == bTurns && !aAt.After(bAt)) {
		return aID, bID
	}
	return bID, aID
}

// mergeSubjectsTx losslessly merges secondaryID into primaryID inside tx and deletes secondaryID.
func (a *App) mergeSubjectsTx(ctx context.Context, tx pgx.Tx, primaryID, secondaryID string) error {
	if primaryID == "" || secondaryID == "" || primaryID == secondaryID {
		return nil
	}
	// Lock both subjects in deterministic UUID order to prevent deadlocks.
	first, second := primaryID, secondaryID
	if first > second {
		first, second = second, first
	}
	if _, err := tx.Exec(ctx, "SELECT id FROM memory_subjects WHERE id = ANY($1::uuid[]) ORDER BY id FOR UPDATE", []string{first, second}); err != nil {
		return err
	}

	var p, sec subjectRecord
	var pNick, secNick, pPersona, secPersona, pMood, secMood []byte
	err := tx.QueryRow(ctx, `SELECT display_name,nicknames,profile_content,persona,familiarity,affinity,trust,relationship_stage,interaction_turns,interaction_days,mood_state,last_interaction_at FROM memory_subjects WHERE id=$1`, primaryID).
		Scan(&p.DisplayName, &pNick, &p.ProfileContent, &pPersona, &p.Familiarity, &p.Affinity, &p.Trust, &p.RelationshipStage, &p.InteractionTurns, &p.InteractionDays, &pMood, &p.LastInteractionAt)
	if err != nil {
		return err
	}
	err = tx.QueryRow(ctx, `SELECT display_name,nicknames,profile_content,persona,familiarity,affinity,trust,relationship_stage,interaction_turns,interaction_days,mood_state,last_interaction_at FROM memory_subjects WHERE id=$1`, secondaryID).
		Scan(&sec.DisplayName, &secNick, &sec.ProfileContent, &secPersona, &sec.Familiarity, &sec.Affinity, &sec.Trust, &sec.RelationshipStage, &sec.InteractionTurns, &sec.InteractionDays, &secMood, &sec.LastInteractionAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}

	_ = json.Unmarshal(pNick, &p.Nicknames)
	_ = json.Unmarshal(secNick, &sec.Nicknames)
	_ = json.Unmarshal(pPersona, &p.Persona)
	_ = json.Unmarshal(secPersona, &sec.Persona)
	_ = json.Unmarshal(pMood, &p.MoodState)
	_ = json.Unmarshal(secMood, &sec.MoodState)

	if p.DisplayName == "" {
		p.DisplayName = sec.DisplayName
	}
	for _, n := range sec.Nicknames {
		if n != "" && !slices.Contains(p.Nicknames, n) && len(p.Nicknames) < 8 {
			p.Nicknames = append(p.Nicknames, n)
		}
	}
	if p.ProfileContent == "" {
		p.ProfileContent = sec.ProfileContent
	} else if sec.ProfileContent != "" && sec.ProfileContent != p.ProfileContent && !strings.Contains(p.ProfileContent, sec.ProfileContent) {
		merged := p.ProfileContent + "\n" + sec.ProfileContent
		runes := []rune(merged)
		for len(runes) > 0 && agent.EstimateTokens(string(runes)) > 2000 {
			runes = runes[:len(runes)-20]
		}
		p.ProfileContent = string(runes)
	}
	if len(sec.Persona) > 0 {
		if len(p.Persona) == 0 {
			p.Persona = sec.Persona
		} else if merged := mergePersona(p.Persona, sec.Persona, time.Now()); merged != nil {
			p.Persona = merged
		}
	}

	p.InteractionTurns += sec.InteractionTurns
	if sec.InteractionDays > p.InteractionDays {
		p.InteractionDays = sec.InteractionDays
	}
	p.Familiarity = math.Max(p.Familiarity, sec.Familiarity)
	p.Affinity = (p.Affinity + sec.Affinity) / 2
	p.Trust = math.Max(p.Trust, sec.Trust)
	p.RelationshipStage = maxStage(p.RelationshipStage, sec.RelationshipStage)
	if p.LastInteractionAt == nil || (sec.LastInteractionAt != nil && sec.LastInteractionAt.After(*p.LastInteractionAt)) {
		p.LastInteractionAt = sec.LastInteractionAt
		if sec.MoodState.CurrentMood != "" {
			p.MoodState = sec.MoodState
		}
	}

	// Reassign identity links; ignore duplicate union links that already exist on primary.
	if _, err = tx.Exec(ctx, `DELETE FROM user_identity_links s WHERE s.subject_id=$2 AND EXISTS (
		SELECT 1 FROM user_identity_links p WHERE p.subject_id=$1 AND p.identity_type=s.identity_type AND p.identity_value=s.identity_value AND p.bot_id IS NOT DISTINCT FROM s.bot_id
	)`, primaryID, secondaryID); err != nil {
		return err
	}
	if _, err = tx.Exec(ctx, `UPDATE user_identity_links SET subject_id=$1,updated_at=now() WHERE subject_id=$2`, primaryID, secondaryID); err != nil {
		return err
	}

	// Deduplicate active memories with identical content before reassigning.
	if _, err = tx.Exec(ctx, `UPDATE user_memories p SET
		confidence = GREATEST(p.confidence, s.confidence),
		importance = GREATEST(p.importance, s.importance),
		access_count = p.access_count + s.access_count,
		pinned = p.pinned OR s.pinned
		FROM user_memories s
		WHERE p.subject_id=$1 AND s.subject_id=$2 AND p.status='active' AND s.status='active' AND p.content=s.content`, primaryID, secondaryID); err != nil {
		return err
	}
	if _, err = tx.Exec(ctx, `DELETE FROM user_memories s WHERE s.subject_id=$2 AND s.status='active' AND EXISTS (
		SELECT 1 FROM user_memories p WHERE p.subject_id=$1 AND p.status='active' AND p.content=s.content
	)`, primaryID, secondaryID); err != nil {
		return err
	}
	if _, err = tx.Exec(ctx, `UPDATE user_memories SET subject_id=$1 WHERE subject_id=$2`, primaryID, secondaryID); err != nil {
		return err
	}
	if _, err = tx.Exec(ctx, `UPDATE proactive_triggers SET subject_id=$1 WHERE subject_id=$2`, primaryID, secondaryID); err != nil {
		return err
	}

	nickJSON, _ := json.Marshal(nonNilStrings(p.Nicknames))
	moodJSON, _ := json.Marshal(p.MoodState)
	var personaJSON any
	if len(p.Persona) > 0 {
		b, _ := json.Marshal(p.Persona)
		personaJSON = string(b)
	}
	if _, err = tx.Exec(ctx, `UPDATE memory_subjects SET display_name=$2,nicknames=$3::jsonb,profile_content=$4,persona=$5::jsonb,familiarity=$6,affinity=$7,trust=$8,relationship_stage=$9,interaction_turns=$10,interaction_days=$11,mood_state=$12::jsonb,last_interaction_at=$13,updated_at=now() WHERE id=$1`,
		primaryID, p.DisplayName, string(nickJSON), p.ProfileContent, personaJSON, p.Familiarity, p.Affinity, p.Trust, p.RelationshipStage, p.InteractionTurns, p.InteractionDays, string(moodJSON), p.LastInteractionAt); err != nil {
		return err
	}
	_, err = tx.Exec(ctx, `DELETE FROM memory_subjects WHERE id=$1`, secondaryID)
	return err
}

func stageRank(s string) int {
	switch s {
	case "acquaintance":
		return 1
	case "familiar":
		return 2
	case "close_friend":
		return 3
	default:
		return 0
	}
}

func maxStage(a, b string) string {
	if stageRank(b) > stageRank(a) {
		return b
	}
	if a == "" {
		return "stranger"
	}
	return a
}

func computeRelationshipStage(turns int, fam, aff, trust float64) string {
	switch {
	case turns >= 30 && fam >= 75 && aff >= 70 && trust >= 70:
		return "close_friend"
	case turns >= 12 && fam >= 45 && trust >= 50:
		return "familiar"
	case turns >= 3 || fam >= 15:
		return "acquaintance"
	default:
		return "stranger"
	}
}

func nonNilStrings(in []string) []string {
	if in == nil {
		return []string{}
	}
	return in
}

func (a *App) loadSubjectRecord(ctx context.Context, subjectID string) (subjectRecord, error) {
	var rec subjectRecord
	var nickRaw, personaRaw, moodRaw []byte
	err := a.db.QueryRow(ctx, `SELECT id::text,display_name,nicknames,profile_content,persona,familiarity,affinity,trust,relationship_stage,interaction_turns,interaction_days,turns_since_reflection,last_reflected_at,last_interaction_at,mood_state FROM memory_subjects WHERE id=$1`, subjectID).
		Scan(&rec.ID, &rec.DisplayName, &nickRaw, &rec.ProfileContent, &personaRaw, &rec.Familiarity, &rec.Affinity, &rec.Trust, &rec.RelationshipStage, &rec.InteractionTurns, &rec.InteractionDays, &rec.TurnsSinceReflection, &rec.LastReflectedAt, &rec.LastInteractionAt, &moodRaw)
	if err != nil {
		return rec, err
	}
	_ = json.Unmarshal(nickRaw, &rec.Nicknames)
	_ = json.Unmarshal(personaRaw, &rec.Persona)
	_ = json.Unmarshal(moodRaw, &rec.MoodState)
	return rec, nil
}

// --- admin APIs for cross-bot unified subjects ---

func (a *App) listSubjects(c *gin.Context) {
	args := []any{}
	where := []string{"1=1"}
	if q := strings.TrimSpace(c.Query("q")); q != "" {
		args = append(args, "%"+q+"%")
		n := itoa(len(args))
		where = append(where, "(s.display_name ILIKE $"+n+" OR s.profile_content ILIKE $"+n+" OR EXISTS(SELECT 1 FROM user_identity_links l WHERE l.subject_id=s.id AND (l.display_name ILIKE $"+n+" OR l.identity_value ILIKE $"+n+")))")
	}
	if stage := strings.TrimSpace(c.Query("stage")); stage != "" && stage != "all" {
		args = append(args, stage)
		where = append(where, "s.relationship_stage=$"+itoa(len(args)))
	}
	if botID := strings.TrimSpace(c.Query("botId")); botID != "" {
		args = append(args, botID)
		where = append(where, "EXISTS(SELECT 1 FROM user_identity_links l WHERE l.subject_id=s.id AND l.bot_id=$"+itoa(len(args))+")")
	}
	queryList(c, a, `SELECT s.id::text,s.display_name AS "displayName",s.nicknames,s.profile_content AS "profileContent",s.persona,
		s.familiarity,s.affinity,s.trust,s.relationship_stage AS "relationshipStage",
		s.interaction_turns AS "interactionTurns",s.interaction_days AS "interactionDays",
		s.turns_since_reflection AS "turnsSinceReflection",s.last_reflected_at AS "lastReflectedAt",
		s.last_interaction_at AS "lastInteractionAt",s.mood_state AS "moodState",s.updated_at AS "updatedAt",
		(SELECT count(*) FROM user_memories m WHERE m.subject_id=s.id AND m.status='active') AS "memoryCount",
		COALESCE((SELECT json_agg(json_build_object(
			'id',l.id::text,'botId',l.bot_id::text,'botName',COALESCE(b.name,'全局跨机器人'),'identityType',l.identity_type,
			'identityValue',l.identity_value,'displayName',l.display_name,'conversationId',l.last_conversation_id::text,
			'conversationName',COALESCE(NULLIF(cv.name,''),cv.platform_id,'')
		) ORDER BY l.created_at) FROM user_identity_links l LEFT JOIN bots b ON b.id=l.bot_id LEFT JOIN conversations cv ON cv.id=l.last_conversation_id WHERE l.subject_id=s.id),'[]') AS links
		FROM memory_subjects s WHERE `+strings.Join(where, " AND ")+` ORDER BY COALESCE(s.last_interaction_at,s.updated_at) DESC LIMIT 200`, args...)
}

func (a *App) getSubject(c *gin.Context) {
	id := c.Param("id")
	if parseUUID(id) != nil {
		fail(c, 400, "invalid_id", "无效的人物 ID")
		return
	}
	rows, err := a.db.Query(c, `SELECT s.id::text,s.display_name AS "displayName",s.nicknames,s.profile_content AS "profileContent",s.persona,
		s.familiarity,s.affinity,s.trust,s.relationship_stage AS "relationshipStage",
		s.interaction_turns AS "interactionTurns",s.interaction_days AS "interactionDays",
		s.turns_since_reflection AS "turnsSinceReflection",s.last_reflected_at AS "lastReflectedAt",
		s.last_interaction_at AS "lastInteractionAt",s.mood_state AS "moodState",s.updated_at AS "updatedAt",
		(SELECT count(*) FROM user_memories m WHERE m.subject_id=s.id AND m.status='active') AS "memoryCount",
		COALESCE((SELECT json_agg(json_build_object(
			'id',l.id::text,'botId',l.bot_id::text,'botName',COALESCE(b.name,'全局跨机器人'),'identityType',l.identity_type,
			'identityValue',l.identity_value,'displayName',l.display_name,'conversationId',l.last_conversation_id::text,
			'conversationName',COALESCE(NULLIF(cv.name,''),cv.platform_id,'')
		) ORDER BY l.created_at) FROM user_identity_links l LEFT JOIN bots b ON b.id=l.bot_id LEFT JOIN conversations cv ON cv.id=l.last_conversation_id WHERE l.subject_id=s.id),'[]') AS links
		FROM memory_subjects s WHERE s.id=$1`, id)
	if err != nil {
		fail(c, 500, "database_error", "读取人物信息失败")
		return
	}
	items, err := rowsJSON(rows)
	if err != nil || len(items) == 0 {
		fail(c, 404, "not_found", "人物不存在")
		return
	}
	ok(c, items[0])
}

func (a *App) updateSubject(c *gin.Context) {
	id := c.Param("id")
	if parseUUID(id) != nil {
		fail(c, 400, "invalid_id", "无效的人物 ID")
		return
	}
	var in struct {
		DisplayName    *string   `json:"displayName"`
		ProfileContent *string   `json:"profileContent"`
		Nicknames      *[]string `json:"nicknames"`
	}
	if c.ShouldBindJSON(&in) != nil {
		fail(c, 400, "invalid_request", "请求参数无效")
		return
	}
	if in.ProfileContent != nil {
		if privateMemory(*in.ProfileContent) || agent.EstimateTokens(*in.ProfileContent) > 2000 {
			fail(c, 400, "invalid_profile", "档案须在约 2000 token 以内，且不得含敏感信息")
			return
		}
	}
	var cleanNicks []string
	var nickJSON any
	if in.Nicknames != nil {
		for _, n := range *in.Nicknames {
			n = strings.TrimSpace(n)
			if n != "" && !privateMemory(n) && len([]rune(n)) <= 24 && !slices.Contains(cleanNicks, n) && len(cleanNicks) < 8 {
				cleanNicks = append(cleanNicks, n)
			}
		}
		b, _ := json.Marshal(nonNilStrings(cleanNicks))
		nickJSON = string(b)
	}
	var dn, pc any
	if in.DisplayName != nil {
		dn = strings.TrimSpace(*in.DisplayName)
	}
	if in.ProfileContent != nil {
		pc = strings.TrimSpace(*in.ProfileContent)
	}
	tag, err := a.db.Exec(c, `UPDATE memory_subjects SET
		display_name=COALESCE($2,display_name),
		profile_content=COALESCE($3,profile_content),
		nicknames=COALESCE($4::jsonb,nicknames),
		updated_at=now() WHERE id=$1`, id, dn, pc, nickJSON)
	if err != nil || tag.RowsAffected() == 0 {
		fail(c, 404, "not_found", "人物不存在")
		return
	}
	ok(c, gin.H{"updated": true})
}

func (a *App) deleteSubject(c *gin.Context) {
	id := c.Param("id")
	if parseUUID(id) != nil {
		fail(c, 400, "invalid_id", "无效的人物 ID")
		return
	}
	if c.Query("mode") == "clear_profile" {
		tag, err := a.db.Exec(c, `UPDATE memory_subjects SET profile_content='',persona=NULL,updated_at=now() WHERE id=$1`, id)
		if err != nil || tag.RowsAffected() == 0 {
			fail(c, 404, "not_found", "人物不存在")
			return
		}
		ok(c, gin.H{"cleared": true})
		return
	}
	tag, err := a.db.Exec(c, `DELETE FROM memory_subjects WHERE id=$1`, id)
	if err != nil || tag.RowsAffected() == 0 {
		fail(c, 404, "not_found", "人物不存在")
		return
	}
	ok(c, gin.H{"deleted": true})
}

func (a *App) mergeSubjectsAPI(c *gin.Context) {
	var in struct {
		PrimaryID   string `json:"primaryId"`
		SecondaryID string `json:"secondaryId"`
	}
	if c.ShouldBindJSON(&in) != nil || parseUUID(in.PrimaryID) != nil || parseUUID(in.SecondaryID) != nil || in.PrimaryID == in.SecondaryID {
		fail(c, 400, "invalid_request", "请选择两个不同的人物合并")
		return
	}
	tx, err := a.db.Begin(c)
	if err != nil {
		fail(c, 500, "database_error", "合并失败")
		return
	}
	defer tx.Rollback(c)
	if err = a.mergeSubjectsTx(c, tx, in.PrimaryID, in.SecondaryID); err != nil {
		fail(c, 500, "database_error", "合并人物失败")
		return
	}
	if err = tx.Commit(c); err != nil {
		fail(c, 500, "database_error", "提交合并失败")
		return
	}
	a.audit(c, "subject.merge", "memory_subject", in.PrimaryID, map[string]any{"merged": in.SecondaryID})
	ok(c, gin.H{"merged": true, "subjectId": in.PrimaryID})
}

func (a *App) unlinkSubjectAPI(c *gin.Context) {
	subjectID := c.Param("id")
	var in struct {
		LinkID string `json:"linkId"`
	}
	if c.ShouldBindJSON(&in) != nil || parseUUID(subjectID) != nil || parseUUID(in.LinkID) != nil {
		fail(c, 400, "invalid_request", "无效的身份关联参数")
		return
	}
	tx, err := a.db.Begin(c)
	if err != nil {
		fail(c, 500, "database_error", "拆分失败")
		return
	}
	defer tx.Rollback(c)
	var count int
	if err = tx.QueryRow(c, "SELECT count(*) FROM user_identity_links WHERE subject_id=$1", subjectID).Scan(&count); err != nil {
		fail(c, 500, "database_error", "拆分失败")
		return
	}
	if count <= 1 {
		fail(c, 400, "cannot_unlink_last", "该人物仅剩一个身份，无需拆分")
		return
	}
	var botID, idType, idValue, displayName string
	err = tx.QueryRow(c, `SELECT COALESCE(bot_id::text,''),identity_type,identity_value,display_name FROM user_identity_links WHERE id=$1 AND subject_id=$2 FOR UPDATE`, in.LinkID, subjectID).
		Scan(&botID, &idType, &idValue, &displayName)
	if errors.Is(err, pgx.ErrNoRows) {
		fail(c, 404, "not_found", "身份关联不存在")
		return
	}
	if err != nil {
		fail(c, 500, "database_error", "拆分失败")
		return
	}
	var newSubjectID string
	if err = tx.QueryRow(ctxOrGin(c), `INSERT INTO memory_subjects(display_name) VALUES($1) RETURNING id::text`, displayName).Scan(&newSubjectID); err != nil {
		fail(c, 500, "database_error", "创建新人物失败")
		return
	}
	if _, err = tx.Exec(c, `UPDATE user_identity_links SET subject_id=$1,updated_at=now() WHERE id=$2`, newSubjectID, in.LinkID); err != nil {
		fail(c, 500, "database_error", "转移身份失败")
		return
	}
	if botID != "" && idType != "qq_union_id" {
		_, _ = tx.Exec(c, `UPDATE user_memories SET subject_id=$1 WHERE subject_id=$2 AND bot_id=$3 AND platform_user_id=$4`, newSubjectID, subjectID, botID, idValue)
	}
	if err = tx.Commit(c); err != nil {
		fail(c, 500, "database_error", "拆分提交失败")
		return
	}
	a.audit(c, "subject.unlink", "memory_subject", subjectID, map[string]any{"newSubjectId": newSubjectID, "linkId": in.LinkID})
	ok(c, gin.H{"unlinked": true, "newSubjectId": newSubjectID})
}

func ctxOrGin(c *gin.Context) context.Context { return c.Request.Context() }
