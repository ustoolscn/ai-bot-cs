package app

import (
	"math"
	"slices"
	"strings"
	"time"
	"unicode/utf8"
)

var personaGroups = map[string][]string{
	"bigFive":       {"openness", "conscientiousness", "extraversion", "agreeableness", "stability"},
	"communication": {"verbosity", "formality", "directness", "patience", "emotion", "humor", "decision"},
	"relationship":  {"satisfaction", "trust"},
}

func personaText(v any, limit int) string {
	s, _ := v.(string)
	s = strings.TrimSpace(s)
	if utf8.RuneCountInString(s) > limit || privateMemory(s) {
		return ""
	}
	for _, word := range strings.Fields("健康 疾病 抑郁 诊断 政治 党派 宗教 信仰 性取向 同性恋 民族 族裔 住址 地址 证件 财务 账号 银行 残疾 病史") {
		if strings.Contains(s, word) {
			return ""
		}
	}
	return s
}
func personaMetric(prior, v any) map[string]any {
	m, _ := v.(map[string]any)
	score, sok := m["score"].(float64)
	confidence, cok := m["confidence"].(float64)
	evidence := personaText(m["evidence"], 40)
	if !sok || !cok || evidence == "" || score < 0 || score > 100 || confidence <= 0 || confidence > 1 {
		return nil
	}
	old, _ := prior.(map[string]any)
	if prev, ok := old["score"].(float64); ok {
		score = prev + (score-prev)*0.25*confidence
	}
	return map[string]any{"score": math.Round(score), "confidence": confidence, "evidence": evidence}
}
func personaTags(old, v any) []any {
	tags, _ := old.([]any)
	input, _ := v.([]any)
	for _, item := range input {
		if s := personaText(item, 40); s != "" && !slices.Contains(tags, any(s)) {
			tags = append(tags, s)
		}
	}
	if len(tags) > 12 {
		tags = tags[len(tags)-12:]
	}
	return tags
}
func mergePersona(old, patch map[string]any, at time.Time) map[string]any {
	if old == nil {
		old = map[string]any{}
	}
	changed := false
	for group, keys := range personaGroups {
		src, _ := patch[group].(map[string]any)
		dst, _ := old[group].(map[string]any)
		if dst == nil {
			dst = map[string]any{}
		}
		for _, k := range keys {
			if m := personaMetric(dst[k], src[k]); m != nil {
				dst[k] = m
				changed = true
			}
		}
		if group == "relationship" {
			for key, allowed := range map[string][]string{"mood": {"平静", "满意", "着急", "不满", "愤怒"}, "trend": {"up", "flat", "down"}} {
				s, _ := src[key].(string)
				if slices.Contains(allowed, s) {
					dst[key] = s
					changed = true
				}
			}
		}
		if len(dst) > 0 {
			old[group] = dst
		}
	}
	if m := personaMetric(old["expertise"], patch["expertise"]); m != nil {
		prev, _ := old["expertise"].(map[string]any)
		m["domains"] = personaTags(prev["domains"], patch["expertise"].(map[string]any)["domains"])
		old["expertise"] = m
		changed = true
	}
	for _, key := range strings.Fields("roles interests habits needs avoid tips") {
		if tags := personaTags(old[key], patch[key]); len(tags) > 0 && patch[key] != nil {
			old[key] = tags
			changed = true
		}
	}
	if s := personaText(patch["summary"], 120); s != "" {
		old["summary"] = s
		changed = true
	}
	if !changed {
		return nil
	}
	turns, _ := old["observedTurns"].(float64)
	old["observedTurns"] = turns + 1
	old["updatedAt"] = at.UTC()
	return old
}

const personaPrompt = `
启用记忆时可输出 persona，仅基于本轮明确表现提出增量。不要推断或记录健康、政治、宗教、性取向、民族、地址、证件、财务或其他敏感信息。未知项省略。大五与沟通风格不是诊断，证据少时降低置信度。每个量表项都用 {score:0至100,confidence:0至1,evidence:来自对话的40字以内依据}。bigFive 可含 openness、conscientiousness、extraversion、agreeableness、stability（情绪稳定）；communication 可含 verbosity（简短到详细）、formality（随意到正式）、directness（委婉到直接）、patience（急躁到耐心）、emotion（冷静到情绪化）、humor（严肃到幽默）、decision（直觉到数据）。expertise 是量表项，另含 domains 字符串数组。relationship 含 satisfaction、trust 量表项，mood 为平静/满意/着急/不满/愤怒，trend 为 up/flat/down。summary 为一句话；roles、interests、habits、needs、avoid、tips 为短文本数组。不得根据昵称或单条消息臆测身份。`
