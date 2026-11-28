package app

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"strconv"
	"strings"
	"time"

	"ai-bot/backend/internal/agent"
	"ai-bot/backend/internal/domain"
)

// TurnPerception captures pre-reply intent, emotional valence, sarcasm, and memory focus.
type TurnPerception struct {
	Reply            *bool   `json:"reply,omitempty"`
	ReplyMode        string  `json:"replyMode,omitempty"` // help | empathy | banter | brief_ack
	SocialOnly       bool    `json:"socialOnly,omitempty"`
	NeedsWebSearch   bool    `json:"needsWebSearch,omitempty"`
	IsChimeIn        bool    `json:"isChimeIn,omitempty"`
	UserMood         string  `json:"userMood,omitempty"`
	MoodIntensity    float64 `json:"moodIntensity,omitempty"`
	Valence          float64 `json:"valence,omitempty"`
	Sarcasm          bool    `json:"sarcasm,omitempty"`
	NeedEmpathyFirst bool    `json:"needEmpathyFirst,omitempty"`
	MemoryFocus      string  `json:"memoryFocus,omitempty"` // facts | episodes | inside_jokes | issues | all | none
	TopicHint        string  `json:"topicHint,omitempty"`
	SuggestHandoff   bool    `json:"suggestHandoff,omitempty"`
}

func (p *TurnPerception) normalize(fallbackSocial bool) {
	switch p.ReplyMode {
	case "help", "empathy", "banter", "brief_ack":
	default:
		if p.SocialOnly || fallbackSocial {
			p.ReplyMode = "banter"
		} else {
			p.ReplyMode = "help"
		}
	}
	if p.UserMood == "" {
		p.UserMood = "平静"
	}
	if p.MoodIntensity < 0 {
		p.MoodIntensity = 0
	} else if p.MoodIntensity > 1 {
		p.MoodIntensity = 1
	}
	if p.Valence < -1 {
		p.Valence = -1
	} else if p.Valence > 1 {
		p.Valence = 1
	}
	switch p.MemoryFocus {
	case "facts", "episodes", "inside_jokes", "issues", "all", "none":
	default:
		if p.ReplyMode == "banter" || p.ReplyMode == "empathy" {
			p.MemoryFocus = "episodes"
		} else {
			p.MemoryFocus = "all"
		}
	}
	p.TopicHint = agent.TruncateRunes(strings.TrimSpace(p.TopicHint), 30)
}

func inQuietHours(startStr, endStr string, now time.Time) bool {
	if startStr == "" || endStr == "" || startStr == endStr {
		return false
	}
	parseMin := func(s string) (int, bool) {
		parts := strings.Split(strings.TrimSpace(s), ":")
		if len(parts) != 2 {
			return 0, false
		}
		h, err1 := strconv.Atoi(parts[0])
		m, err2 := strconv.Atoi(parts[1])
		if err1 != nil || err2 != nil || h < 0 || h > 23 || m < 0 || m > 59 {
			return 0, false
		}
		return h*60 + m, true
	}
	sMin, ok1 := parseMin(startStr)
	eMin, ok2 := parseMin(endStr)
	if !ok1 || !ok2 {
		return false
	}
	cur := now.In(chinaLocation).Hour()*60 + now.In(chinaLocation).Minute()
	if sMin < eMin {
		return cur >= sMin && cur < eMin
	}
	// Overnight window such as 01:00 -> 08:00 or 23:00 -> 07:30
	return cur >= sMin || cur < eMin
}

func isChimeInBlocked(s agent.Settings, lastChimeAt *time.Time, lastSenderIsBot bool, now time.Time) (bool, string) {
	if !s.Companion.Enabled || s.Companion.GroupChimeIn == "" || s.Companion.GroupChimeIn == "off" {
		return true, "chime_in_disabled"
	}
	if lastSenderIsBot {
		return true, "last_message_from_bot"
	}
	if inQuietHours(s.Companion.QuietHoursStart, s.Companion.QuietHoursEnd, now) {
		return true, "quiet_hours"
	}
	cooldown := time.Duration(s.Companion.ChimeInCooldownMinutes) * time.Minute
	switch s.Companion.GroupChimeIn {
	case "restrained":
		cooldown = max(cooldown, 15*time.Minute)
	case "active":
		cooldown = min(cooldown, 5*time.Minute)
	}
	if lastChimeAt != nil && now.Sub(*lastChimeAt) < cooldown {
		return true, "cooldown_active"
	}
	return false, ""
}

const privatePerceptionPrompt = `你是对话情绪与社交意图前置感知器。只分析“当前消息”并结合近期对话判断用户此刻情绪与对话需求。
输出 JSON（不要输出其它内容）：
{
  "replyMode": "help|empathy|banter|brief_ack",
  "socialOnly": true/false,
  "userMood": "开心|兴奋|平静|调侃|困惑|委屈|焦虑|不满|愤怒|疲惫",
  "moodIntensity": 0.0-1.0,
  "valence": -1.0到1.0,
  "sarcasm": true/false,
  "needEmpathyFirst": true/false,
  "memoryFocus": "facts|episodes|inside_jokes|issues|all|none",
  "topicHint": "15字以内话题摘要",
  "suggestHandoff": true/false
}
说明：纯寒暄/闲聊/吐槽时 socialOnly 为 true；涉及业务、报价、故障或追问时为 false。发现用户说反话/阴阳怪气时 sarcasm 为 true。`

// perceivePrivateTurn performs a fast, non-blocking pre-reply emotion and intent prediction for DMs.
func (a *App) perceivePrivateTurn(ctx context.Context, s agent.Settings, profileID, messageID, content string, recent []agent.Turn, images ...domain.ChatContentPart) TurnPerception {
	var p TurnPerception
	p.SocialOnly = socialOnly(content)
	p.normalize(p.SocialOnly)
	if !s.Companion.Enabled || !s.Companion.RealTimeEmotion || (strings.TrimSpace(content) == "" && len(images) == 0) {
		return p
	}
	cl, pid, err := a.auxModel(ctx, s, profileID)
	if err != nil {
		return p
	}
	var recentText []string
	start := max(0, len(recent)-4)
	for _, t := range recent[start:] {
		who := "用户"
		if t.Role == "assistant" {
			who = "你"
		}
		recentText = append(recentText, who+"："+agent.TruncateRunes(t.Content, 160))
	}
	payload, _ := json.Marshal(map[string]any{
		"近期私聊": recentText,
		"当前消息": agent.TruncateRunes(content, 1000),
		"含图片":  len(images) > 0,
	})
	pctx, cancel := context.WithTimeout(ctx, 4*time.Second)
	defer cancel()
	started := time.Now()
	res, err := cl.ChatWith(pctx, []domain.ChatMessage{
		{Role: "system", Content: privatePerceptionPrompt},
		{Role: "user", Content: string(payload), Parts: images},
	}, domain.ChatOptions{DisableWebSearch: true})
	a.recordModelCall(ctx, "", messageID, pid, "emotion_perception", res, time.Since(started).Milliseconds(), err)
	if err != nil {
		return p
	}
	raw := agent.ExtractJSON(res.Content)
	if raw == "" {
		return p
	}
	var parsed TurnPerception
	if json.Unmarshal([]byte(raw), &parsed) != nil {
		return p
	}
	parsed.normalize(p.SocialOnly)
	return parsed
}

// updateMoodInertia smooths emotional state across turns so sudden neutral replies
// right after strong frustration do not reset the affective context immediately.
func updateMoodInertia(prev userMoodState, p TurnPerception, now time.Time) userMoodState {
	next := prev
	valence := p.Valence
	intensity := p.MoodIntensity
	mood := p.UserMood
	if mood == "" {
		mood = "平静"
	}

	if prev.UpdatedAt != nil && now.Sub(*prev.UpdatedAt) <= 2*time.Hour {
		if prev.Valence <= -0.4 && math.Abs(valence) < 0.15 && intensity < 0.35 {
			valence = prev.Valence * 0.6
			intensity = math.Max(intensity, prev.Intensity*0.6)
			if prev.CurrentMood != "" {
				mood = prev.CurrentMood
			}
		} else {
			valence = 0.35*prev.Valence + 0.65*valence
		}
	}

	if valence <= -0.25 || isNegativeMoodString(mood) {
		next.NegativeStreak = prev.NegativeStreak + 1
	} else if valence >= 0.2 {
		next.NegativeStreak = 0
	}

	next.CurrentMood = mood
	next.Intensity = math.Round(intensity*100) / 100
	next.Valence = math.Round(valence*100) / 100
	t := now.UTC()
	next.UpdatedAt = &t
	next.History = append(next.History, moodSnapshot{
		Mood:      mood,
		Valence:   next.Valence,
		Intensity: next.Intensity,
		At:        t,
	})
	if len(next.History) > 8 {
		next.History = next.History[len(next.History)-8:]
	}
	return next
}

func isNegativeMoodString(mood string) bool {
	switch mood {
	case "愤怒", "不满", "委屈", "焦虑", "着急", "沮丧", "难过", "烦躁":
		return true
	}
	return false
}

// applyTurnEmotionAndMood updates the subject's mood inertia and the bot's mild mood state.
func (a *App) applyTurnEmotionAndMood(ctx context.Context, subjectID, scopeKey string, p TurnPerception, now time.Time) {
	if subjectID != "" {
		var moodRaw []byte
		if err := a.db.QueryRow(ctx, "SELECT mood_state FROM memory_subjects WHERE id=$1", subjectID).Scan(&moodRaw); err == nil {
			var prev userMoodState
			_ = json.Unmarshal(moodRaw, &prev)
			next := updateMoodInertia(prev, p, now)
			b, _ := json.Marshal(next)
			_, _ = a.db.Exec(ctx, "UPDATE memory_subjects SET mood_state=$2::jsonb,updated_at=now() WHERE id=$1", subjectID, string(b))
		}
	}
	if scopeKey != "" {
		var curMood string
		var energy, val float64
		if err := a.db.QueryRow(ctx, "SELECT mood,energy,valence FROM bot_mood_states WHERE scope_key=$1", scopeKey).Scan(&curMood, &energy, &val); err != nil {
			curMood, energy, val = "轻松", 0.7, 0.3
		}
		// Gently drift bot mood toward steady positive baseline; stay calm if user was negative.
		delta := p.Valence * 0.12
		if delta < -0.15 {
			delta = -0.15
		}
		val = math.Max(-0.4, math.Min(0.8, 0.75*val+0.25*0.3+delta))
		newMood := "轻松"
		switch {
		case p.Valence <= -0.4:
			newMood = "关切"
		case val >= 0.55:
			newMood = "愉快"
		case val <= 0.05:
			newMood = "沉稳"
		}
		_, _ = a.db.Exec(ctx, `INSERT INTO bot_mood_states(scope_key,mood,energy,valence,updated_at) VALUES($1,$2,$3,$4,now())
			ON CONFLICT(scope_key) DO UPDATE SET mood=EXCLUDED.mood,energy=EXCLUDED.energy,valence=EXCLUDED.valence,updated_at=now()`,
			scopeKey, newMood, energy, val)
	}
}

func (a *App) loadBotMood(ctx context.Context, scopeKey string) agent.BotMoodContext {
	var out agent.BotMoodContext
	if scopeKey == "" {
		return agent.BotMoodContext{Mood: "轻松", Energy: 0.7, Valence: 0.3}
	}
	if err := a.db.QueryRow(ctx, "SELECT mood,energy,valence FROM bot_mood_states WHERE scope_key=$1", scopeKey).Scan(&out.Mood, &out.Energy, &out.Valence); err != nil {
		return agent.BotMoodContext{Mood: "轻松", Energy: 0.7, Valence: 0.3}
	}
	return out
}

func botMoodScope(convID string, botID string) string {
	if convID != "" {
		return "conv:" + convID
	}
	return "bot:" + botID
}

func formatPerceptionFinish(p TurnPerception, personaName string, styleLen, recentLen int) string {
	decisionText := "需要回复"
	if p.Reply != nil && !*p.Reply {
		decisionText = "无需回复"
	} else if p.IsChimeIn {
		decisionText = "群聊搭茬"
	}
	return fmt.Sprintf("%s（%s/%s），客服 %s，性格 %d 字，近期群聊 %d 条",
		decisionText, p.ReplyMode, p.UserMood, personaName, styleLen, recentLen)
}
