// Package agent holds the database-free parts of the customer-service agent:
// token budgeting, reply styling and prompt assembly.
package agent

import (
	"regexp"
	"strings"
	"unicode"
	"unicode/utf8"
)

// EstimateTokens is a cheap, provider-independent token estimate:
// CJK characters count as ~1 token each, other text as ~1 token per 4 bytes.
func EstimateTokens(s string) int {
	cjk, other := 0, 0
	for _, r := range s {
		if unicode.Is(unicode.Han, r) || unicode.Is(unicode.Hiragana, r) || unicode.Is(unicode.Katakana, r) || unicode.Is(unicode.Hangul, r) {
			cjk++
		} else {
			other += utf8.RuneLen(r)
		}
	}
	return cjk + (other+3)/4
}

// Turn is one message in the working-memory window.
type Turn struct {
	Role    string // user | assistant
	Name    string
	Content string
	Staff   bool
}

// FitHistory keeps the newest turns (input is oldest→newest) whose estimated
// size fits within budget tokens and at most maxTurns turns. It returns the
// kept turns in chronological order and the number of dropped older turns.
func FitHistory(turns []Turn, budget, maxTurns int) ([]Turn, int) {
	if budget <= 0 || maxTurns <= 0 || len(turns) == 0 {
		return nil, len(turns)
	}
	used, start := 0, len(turns)
	for i := len(turns) - 1; i >= 0; i-- {
		cost := EstimateTokens(turns[i].Content) + EstimateTokens(turns[i].Name) + 4
		if used+cost > budget || len(turns)-i > maxTurns {
			break
		}
		used += cost
		start = i
	}
	return turns[start:], start
}

// TruncateRunes shortens s to at most n runes, appending an ellipsis when cut.
func TruncateRunes(s string, n int) string {
	r := []rune(s)
	if n <= 0 || len(r) <= n {
		return s
	}
	return string(r[:n]) + "…"
}

var (
	reHeading    = regexp.MustCompile(`(?m)^\s{0,3}#{1,6}\s+`)
	reBold       = regexp.MustCompile(`\*\*([^*\n]+)\*\*|__([^_\n]+)__`)
	reHRule      = regexp.MustCompile(`(?m)^\s*(-{3,}|\*{3,}|_{3,})\s*$`)
	reBlankLines = regexp.MustCompile(`\n{3,}`)
	reBlockquote = regexp.MustCompile(`(?m)^\s{0,3}>\s?`)
	reFiller     = regexp.MustCompile(`(?m)^.*(希望(以上|这些)?(信息|回答|内容)?(对你|对您)有(所)?帮助|如(果)?(你|您)?还有其他(任何)?问题|如有(其他|任何)(疑问|问题)|随时(联系|告诉|找)我|作为(一个)?\s*AI).*$`)
)

// SanitizeReply removes decorative Markdown that renders badly in QQ and strips
// boilerplate closing lines. Fenced code blocks and image syntax are kept.
func SanitizeReply(s string) string {
	segments := splitFences(s)
	for i := range segments {
		if segments[i].code {
			continue
		}
		t := segments[i].text
		t = reHeading.ReplaceAllString(t, "")
		t = reHRule.ReplaceAllString(t, "")
		t = reBlockquote.ReplaceAllString(t, "")
		t = reBold.ReplaceAllStringFunc(t, func(m string) string {
			return strings.Trim(m, "*_")
		})
		t = reFiller.ReplaceAllString(t, "")
		segments[i].text = t
	}
	var b strings.Builder
	for _, seg := range segments {
		b.WriteString(seg.text)
	}
	out := reBlankLines.ReplaceAllString(b.String(), "\n\n")
	return strings.TrimSpace(out)
}

type segment struct {
	text string
	code bool
}

func splitFences(s string) []segment {
	var out []segment
	for {
		i := strings.Index(s, "```")
		if i < 0 {
			out = append(out, segment{text: s})
			return out
		}
		j := strings.Index(s[i+3:], "```")
		if j < 0 {
			out = append(out, segment{text: s})
			return out
		}
		end := i + 3 + j + 3
		out = append(out, segment{text: s[:i]}, segment{text: s[i:end], code: true})
		s = s[end:]
	}
}

// SplitReply splits a long reply into at most maxParts messages at paragraph
// (then sentence) boundaries, aiming for parts of about target runes. Code
// blocks are never split. Short replies are returned unchanged.
func SplitReply(s string, target, maxParts int) []string {
	s = strings.TrimSpace(s)
	if s == "" {
		return nil
	}
	if target <= 0 || maxParts <= 1 || utf8.RuneCountInString(s) <= target {
		return []string{s}
	}
	type unit struct {
		text   string
		inline bool // sentence continuing the previous unit's paragraph
	}
	var units []unit
	for _, seg := range splitFences(s) {
		if seg.code {
			units = append(units, unit{text: strings.TrimSpace(seg.text)})
			continue
		}
		for _, p := range strings.Split(seg.text, "\n\n") {
			p = strings.TrimSpace(p)
			if p == "" {
				continue
			}
			if utf8.RuneCountInString(p) <= target {
				units = append(units, unit{text: p})
				continue
			}
			for i, sentence := range splitSentences(p) {
				units = append(units, unit{text: sentence, inline: i > 0})
			}
		}
	}
	var parts []string
	var cur strings.Builder
	curLen := 0
	for _, u := range units {
		n := utf8.RuneCountInString(u.text)
		if curLen > 0 && curLen+n > target && len(parts) < maxParts-1 {
			parts = append(parts, strings.TrimSpace(cur.String()))
			cur.Reset()
			curLen = 0
		}
		if curLen > 0 && !u.inline {
			cur.WriteString("\n\n")
		}
		cur.WriteString(u.text)
		curLen += n
	}
	if strings.TrimSpace(cur.String()) != "" {
		parts = append(parts, strings.TrimSpace(cur.String()))
	}
	return parts
}

var sentenceRe = regexp.MustCompile(`[^。！？!?；;\n]+[。！？!?；;]*\n?|\n`)

func splitSentences(p string) []string {
	var out []string
	for _, s := range sentenceRe.FindAllString(p, -1) {
		if strings.TrimSpace(s) != "" {
			out = append(out, s)
		}
	}
	return out
}

// ExtractJSON returns the outermost JSON object in s (models often wrap JSON
// in code fences or add prose around it).
func ExtractJSON(s string) string {
	i := strings.Index(s, "{")
	j := strings.LastIndex(s, "}")
	if i < 0 || j <= i {
		return ""
	}
	return s[i : j+1]
}
