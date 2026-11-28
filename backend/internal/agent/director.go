package agent

import (
	"fmt"
	"strings"
	"time"
)

// SubjectContext carries the cross-bot unified person profile and relationship state.
type SubjectContext struct {
	DisplayName       string
	Nicknames         []string
	RelationshipStage string // stranger | acquaintance | familiar | close_friend
	Familiarity       float64
	Affinity          float64
	Trust             float64
	InteractionTurns  int
	InteractionDays   int
	Persona           map[string]any
	NegativeStreak    int
	LastMood          string
}

// BotMoodContext represents the bot's own mild mood fluctuation.
type BotMoodContext struct {
	Mood    string
	Energy  float64
	Valence float64
}

// PerceptionContext carries the real-time pre-reply mood and intent prediction.
type PerceptionContext struct {
	ReplyMode        string // help | empathy | banter | brief_ack
	IsChimeIn        bool
	UserMood         string
	MoodIntensity    float64
	Valence          float64
	Sarcasm          bool
	NeedEmpathyFirst bool
	TopicHint        string
}

// DirectorInput is everything the deterministic style director needs to craft
// per-turn persona, emotional empathy, and tone guidance.
type DirectorInput struct {
	Settings           Settings
	ConversationType   string
	Now                time.Time
	Subject            SubjectContext
	BotMood            BotMoodContext
	Perception         PerceptionContext
	AllowSemanticSplit bool
}

// BuildDirectorInstructions translates companion persona settings, real-time
// emotion, relationship stage, and user persona metrics into concise, actionable
// system prompt guidance without exposing raw JSON or breaking factual support rules.
func BuildDirectorInstructions(in DirectorInput) string {
	c := in.Settings.Companion
	if !c.Enabled {
		return ""
	}

	var lines []string
	lines = append(lines, "【本轮表达与人设导演指令】")

	// 1. Persona core
	var personaParts []string
	if len(c.PersonalityTraits) > 0 {
		personaParts = append(personaParts, "性格底色："+strings.Join(c.PersonalityTraits, "、"))
	}
	if bs := strings.TrimSpace(c.Backstory); bs != "" {
		personaParts = append(personaParts, "背景设定："+bs)
	}
	if c.HumorLevel >= 70 {
		personaParts = append(personaParts, "平时很爱接梗开玩笑")
	} else if c.HumorLevel <= 20 {
		personaParts = append(personaParts, "整体偏稳重务实，少开玩笑")
	}
	if c.WarmthLevel >= 75 {
		personaParts = append(personaParts, "对群友热心温暖、有生活感")
	} else if c.WarmthLevel <= 25 {
		personaParts = append(personaParts, "表达偏克制酷一点，点到为止")
	}
	if len(c.Catchphrases) > 0 {
		personaParts = append(personaParts, "常用口癖参考（仅在自然时偶尔带出一两个，绝不每句硬塞）："+strings.Join(c.Catchphrases, "、"))
	}
	if len(personaParts) > 0 {
		lines = append(lines, "- "+strings.Join(personaParts, "；")+"。")
	}

	// 2. Circadian rhythm & bot's own mild mood
	isNegative := isNegativeMood(in.Perception.UserMood) || in.Perception.NeedEmpathyFirst || in.Subject.NegativeStreak >= 2
	if c.CircadianAware && !in.Now.IsZero() {
		hour := in.Now.Hour()
		switch {
		case hour >= 0 && hour < 5:
			lines = append(lines, "- 时段氛围：现在是深夜/凌晨，若对方在闲聊可自然带一点熬夜同频的放松感；问正事则保持清晰利落。")
		case hour >= 5 && hour < 9:
			lines = append(lines, "- 时段氛围：早晨时段，语气清爽有精神。")
		case hour >= 22:
			lines = append(lines, "- 时段氛围：夜深了，口吻可以稍微柔和一点。")
		}
	}
	if !isNegative && in.BotMood.Mood != "" && in.BotMood.Mood != "平静" {
		lines = append(lines, fmt.Sprintf("- 你自己当前的状态偏“%s”，保持活人感但不抢对方风头。", in.BotMood.Mood))
	}

	// 3. Relationship stage & nicknames
	switch in.Subject.RelationshipStage {
	case "close_friend":
		nickHint := ""
		if len(c.Catchphrases) >= 0 && len(in.Subject.Nicknames) > 0 && !isNegative {
			nickHint = "（轻松时可自然用称呼如：" + strings.Join(in.Subject.Nicknames, "、") + "）"
		}
		lines = append(lines, "- 关系阶段【老铁】：你们非常熟了，像认识很久的群友搭子，可自然接老梗和呼应过往经历，保持随和默契"+nickHint+"。")
	case "familiar":
		lines = append(lines, "- 关系阶段【熟人】：你们经常聊天，语气自然亲近、像熟络群友一样直接聊，不必客套见外。")
	case "acquaintance":
		lines = append(lines, "- 关系阶段【脸熟】：之前聊过几轮，态度友好放松，记得对方的习惯但不过度自来熟。")
	default:
		lines = append(lines, "- 关系阶段【初识】：第一次或刚认识不久，友好自然、不刻意套近乎。")
	}

	// 4. Real-time emotion & reply mode
	if isNegative {
		lines = append(lines, "- 情绪优先（重要）：对方当前情绪偏负面（"+firstNonEmptyStr(in.Perception.UserMood, "烦躁/不满")+"），必须先用一两句真诚共情接住情绪，不敷衍、不说教、严禁抖机灵或发表情包；若涉及故障/业务，共情后立刻给清晰结论。")
	} else if in.Perception.UserMood == "开心" || in.Perception.UserMood == "兴奋" {
		lines = append(lines, "- 情绪共鸣：对方心情很好，顺着他的开心捧场互动，氛围轻快。")
	} else if in.Perception.UserMood == "疲惫" {
		lines = append(lines, "- 情绪体贴：对方显得有点累，语气温和简短，少给对方增加阅读压力。")
	} else if in.Perception.UserMood == "调侃" {
		lines = append(lines, "- 互动气氛：对方在开玩笑/调侃，放松接住他的梗即可。")
	}
	if in.Perception.Sarcasm {
		lines = append(lines, "- 语义提示：对方疑似在说反话或阴阳怪气，别把反话当字面夸奖来接，先体察他真正不满或吐槽的点。")
	}
	if in.Perception.IsChimeIn {
		lines = append(lines, "- 群聊搭茬模式：你不是被提问才回答，而是像群友路过顺嘴接一句茬。务必简短自然（1～2 句以内），切忌长篇科普或客服腔。")
	} else if in.Perception.ReplyMode == "empathy" && !isNegative {
		lines = append(lines, "- 对话重心：对方当前重在倾诉/吐槽，优先倾听陪伴与情绪回应，不要急着给一堆建议。")
	} else if in.Perception.ReplyMode == "banter" {
		lines = append(lines, "- 对话重心：当前是纯日常闲聊接梗，像真人朋友聊天即可，不要强行往产品/业务上扯。")
	}

	// 5. Activate structured user_profiles.persona
	if hints := personaDirectorHints(in.Subject.Persona, isNegative); len(hints) > 0 {
		lines = append(lines, "- 对方个人画像适配："+strings.Join(hints, "；")+"。")
	}

	// 6. Emoji gating
	switch {
	case isNegative || c.EmojiPolicy == "off":
		lines = append(lines, "- 表情约束：本轮禁止使用 emoji 或 QQ 表情标签。")
	case c.EmojiPolicy == "minimal":
		lines = append(lines, "- 表情约束：尽量少用表情，仅在极自然时最多点缀 1 个。")
	case c.EmojiPolicy == "expressive" && (in.Perception.ReplyMode == "banter" || in.Subject.RelationshipStage == "familiar" || in.Subject.RelationshipStage == "close_friend"):
		lines = append(lines, "- 表情约束：可自然穿插 1～2 个贴切表情增强活人感。")
	default:
		lines = append(lines, "- 表情约束：日常可适度用 0～1 个表情，不滥用。")
	}

	// 7. Grounding & anti-robotic leak rules
	lines = append(lines, "- 真实性与记忆规范：涉及价格、状态、故障、文档等业务事实，必须严格以工具和知识库结果为准，不瞎编；引用记住的用户信息时要像自己想起来的，绝不说“根据记录/画像显示/记忆库”。")

	// 8. Semantic split hint
	if in.AllowSemanticSplit {
		lines = append(lines, "- 分段节奏：如果像微信/QQ 真人聊天那样先说一句反应、再发重点会更自然，可用 `|||` 隔开（最多 2～3 段，不需要每条都拆）。")
	}

	return strings.Join(lines, "\n")
}

func isNegativeMood(mood string) bool {
	switch mood {
	case "愤怒", "不满", "委屈", "焦虑", "着急", "沮丧", "难过", "烦躁":
		return true
	}
	return false
}

func personaDirectorHints(persona map[string]any, isNegative bool) []string {
	if len(persona) == 0 {
		return nil
	}
	var hints []string
	metric := func(group, key string) (float64, bool) {
		g, _ := persona[group].(map[string]any)
		m, _ := g[key].(map[string]any)
		score, sok := m["score"].(float64)
		conf, cok := m["confidence"].(float64)
		if !sok || !cok || conf < 0.45 {
			return 0, false
		}
		return score, true
	}

	if exp, ok := persona["expertise"].(map[string]any); ok {
		if score, sok := exp["score"].(float64); sok {
			if conf, cok := exp["confidence"].(float64); cok && conf >= 0.45 {
				if score < 30 {
					hints = append(hints, "对方偏新手，用大白话和清晰步骤解释，少堆术语")
				} else if score >= 70 {
					hints = append(hints, "对方技术很熟，直接给关键点与底层细节即可")
				}
			}
		}
	}

	verb, vok := metric("communication", "verbosity")
	pat, pok := metric("communication", "patience")
	if (vok && verb < 35) || (pok && pat < 40) {
		hints = append(hints, "对方偏好简短直接，先把结论说出来，别铺垫废话")
	} else if vok && verb > 72 && (!pok || pat >= 50) {
		hints = append(hints, "对方喜欢把背景细节说透，可以适当展开讲明白")
	}

	if humor, hok := metric("communication", "humor"); hok && !isNegative {
		if humor >= 65 {
			hints = append(hints, "对方爱开玩笑、接梗强")
		} else if humor <= 30 {
			hints = append(hints, "对方风格偏严肃认真，少抖机灵")
		}
	}

	if dir, dok := metric("communication", "directness"); dok && dir >= 70 {
		hints = append(hints, "说话直来直去即可，不用拐弯抹角")
	}

	if avoid := stringSliceFromAny(persona["avoid"], 4); len(avoid) > 0 {
		hints = append(hints, "沟通雷区（务必避开）："+strings.Join(avoid, "、"))
	}
	if tips := stringSliceFromAny(persona["tips"], 4); len(tips) > 0 {
		hints = append(hints, "实用建议："+strings.Join(tips, "、"))
	}
	return hints
}

func stringSliceFromAny(v any, limit int) []string {
	arr, ok := v.([]any)
	if !ok || len(arr) == 0 {
		return nil
	}
	out := make([]string, 0, min(len(arr), limit))
	for _, item := range arr {
		if s, ok := item.(string); ok {
			s = strings.TrimSpace(s)
			if s != "" {
				out = append(out, s)
				if len(out) >= limit {
					break
				}
			}
		}
	}
	return out
}

func firstNonEmptyStr(vals ...string) string {
	for _, v := range vals {
		if strings.TrimSpace(v) != "" {
			return v
		}
	}
	return ""
}
