// Package agent holds the database-free parts of the customer-service agent:
// token budgeting, reply styling and prompt assembly.
package agent

import (
	"hash/fnv"
	"regexp"
	"strings"
	"time"
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
	reBoldOrURL  = regexp.MustCompile(`(?i)(?:https?://|www\.)[^\s<>]+|\*\*([^*\n]+)\*\*|__([^_\n]+)__`)
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
		t = reBoldOrURL.ReplaceAllStringFunc(t, func(m string) string {
			if strings.HasPrefix(m, "**") || strings.HasPrefix(m, "__") {
				return m[2 : len(m)-2]
			}
			return m
		})
		t = reFiller.ReplaceAllString(t, "")
		segments[i].text = reBlankLines.ReplaceAllString(t, "\n\n")
	}
	var b strings.Builder
	for _, seg := range segments {
		b.WriteString(seg.text)
	}
	return strings.TrimSpace(b.String())
}

type segment struct {
	text string
	code bool
}

func splitFences(s string) []segment {
	var out []segment
	start := 0
	for pos := 0; pos < len(s); {
		line, end := replyLine(s, pos)
		if fence := fenceMarker(line); fence != "" {
			end = fenceEnd(s, end, fence)
			out = append(out, segment{text: s[start:pos]}, segment{text: s[pos:end], code: true})
			start = end
		}
		pos = end
	}
	return append(out, segment{text: s[start:]})
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
	var parts []string
	start, length := 0, 0
	for offset, unit := range replyUnits(s) {
		n := utf8.RuneCountInString(unit)
		if length > 0 && length+n > target && len(parts) < maxParts-1 {
			if part := strings.TrimSpace(s[start:offset]); part != "" {
				parts = append(parts, part)
			}
			start, length = offset, 0
		}
		length += n
	}
	if last := strings.TrimSpace(s[start:]); last != "" {
		parts = append(parts, last)
	}
	return parts
}

var listStart = regexp.MustCompile(`^[ \t]*(?:[-+*]|[0-9]+[.)])[ \t]+`)

// Each block boundary is a safe delivery boundary. Inline links can span a
// soft line break, so scan the whole prose block when finding its sentences.
func replyUnits(s string) func(func(int, string) bool) {
	return func(yield func(int, string) bool) {
		for pos := 0; pos < len(s); {
			line, end := replyLine(s, pos)
			if fence := fenceMarker(line); fence != "" {
				end = fenceEnd(s, end, fence)
			} else if listStart.MatchString(line) {
				for end < len(s) {
					next, after := replyLine(s, end)
					if listStart.MatchString(next) {
						break
					}
					// An unindented line after a blank line starts a new paragraph.
					if strings.TrimSpace(next) == "" {
						look := after
						for look < len(s) {
							l, n := replyLine(s, look)
							if strings.TrimSpace(l) != "" {
								break
							}
							look = n
						}
						if look == len(s) || (s[look] != ' ' && s[look] != '\t') {
							break
						}
					}
					if f := fenceMarker(next); f != "" {
						after = fenceEnd(s, after, f)
					}
					end = after
				}
			} else {
				for end < len(s) {
					next, after := replyLine(s, end)
					if fenceMarker(next) != "" || listStart.MatchString(next) {
						break
					}
					end = after
				}
				for _, unit := range splitSentences(s[pos:end]) {
					if !yield(pos, unit) {
						return
					}
					pos += len(unit)
				}
				continue
			}
			if !yield(pos, s[pos:end]) {
				return
			}
			pos = end
		}
	}
}

func replyLine(s string, pos int) (string, int) {
	line, _, newline := strings.Cut(s[pos:], "\n")
	end := pos + len(line)
	if newline {
		end++
	}
	return line, end
}

func fenceMarker(line string) string {
	line = strings.TrimSpace(line)
	if len(line) < 3 || (line[0] != '~' && line[0] != '`') {
		return ""
	}
	n := 1
	for n < len(line) && line[n] == line[0] {
		n++
	}
	if n < 3 {
		return ""
	}
	return line[:n]
}

func fenceEnd(s string, pos int, fence string) int {
	for pos < len(s) {
		line, end := replyLine(s, pos)
		pos = end
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, fence) && strings.Trim(line, fence[:1]) == "" {
			break
		}
	}
	return pos
}

var urlStart = regexp.MustCompile(`(?i)^(?:https?://|www\.)[^\s<>]+`)

func splitSentences(p string) []string {
	var units []string
	start := 0
	for i := 0; i < len(p); {
		if p[i] == '`' {
			fence := 1
			for i+fence < len(p) && p[i+fence] == '`' {
				fence++
			}
			if end := strings.Index(p[i+fence:], p[i:i+fence]); end >= 0 {
				i += end + 2*fence
				continue
			}
			i += fence
			continue
		}
		if p[i] == '[' {
			if after := balancedEnd(p, i, '[', ']'); after > i && after < len(p) && p[after] == '(' {
				if end := balancedEnd(p, after, '(', ')'); end > after {
					i = end
					continue
				}
			}
		}
		if p[i] == 'h' || p[i] == 'H' || p[i] == 'w' || p[i] == 'W' {
			if m := urlStart.FindStringIndex(p[i:]); m != nil {
				i += m[1]
				continue
			}
		}
		r, n := utf8.DecodeRuneInString(p[i:])
		i += n
		// ASCII punctuation inside a token can belong to a bare domain,
		// path or query string. Split only after the complete token.
		asciiEnd := strings.ContainsRune("!?;.", r) && (i == len(p) || unicode.IsSpace(firstRune(p[i:])))
		if asciiEnd || strings.ContainsRune("。！？；\n", r) {
			units = append(units, p[start:i])
			start = i
		}
	}
	if start < len(p) {
		units = append(units, p[start:])
	}
	return units
}

// TruncateReply omits a whole token when a preview would end in the middle
// of an address. The full message remains available in the conversation.
func TruncateReply(s string, n int) string {
	r := []rune(s)
	if n <= 0 || len(r) <= n {
		return s
	}
	start, end := n, n
	for start > 0 && !unicode.IsSpace(r[start-1]) {
		start--
	}
	for end < len(r) && !unicode.IsSpace(r[end]) {
		end++
	}
	if end > n && strings.ContainsAny(string(r[start:end]), "./:") {
		n = start
	}
	return string(r[:n]) + "…"
}

func firstRune(s string) rune {
	r, _ := utf8.DecodeRuneInString(s)
	return r
}

// PlainLinks converts inline Markdown links, including balanced parentheses in
// destinations. A soft line break inside an explicit destination is removed.
func PlainLinks(s string) string {
	var out strings.Builder
	for len(s) > 0 {
		i := strings.IndexByte(s, '[')
		if i < 0 {
			out.WriteString(s)
			break
		}
		label, addr, end := parseInlineLink(s[i:])
		if end == 0 {
			out.WriteString(s[:i+1])
			s = s[i+1:]
			continue
		}
		end += i
		out.WriteString(strings.TrimSuffix(s[:i], "!"))
		if label != "" && label != addr {
			out.WriteString(label)
			if addr != "" {
				out.WriteByte(' ')
			}
		}
		out.WriteString(addr)
		s = s[end:]
	}
	return out.String()
}

// MarkdownImage finds a legacy model image attachment without truncating URLs
// that contain parentheses.
func MarkdownImage(s string) (markup, addr string) {
	for {
		i := strings.Index(s, "![")
		if i < 0 {
			return "", ""
		}
		_, addr, end := parseInlineLink(s[i+1:])
		if end > 0 {
			return s[i : i+1+end], addr
		}
		s = s[i+2:]
	}
}

func parseInlineLink(s string) (label, addr string, end int) {
	labelEnd := balancedEnd(s, 0, '[', ']')
	if labelEnd == 0 {
		return "", "", 0
	}
	dest := labelEnd
	for dest < len(s) && strings.ContainsRune(" \n\r\t", rune(s[dest])) {
		dest++
	}
	if dest == len(s) || s[dest] != '(' {
		return "", "", 0
	}
	end = balancedEnd(s, dest, '(', ')')
	if end == dest {
		return "", "", 0
	}
	label = strings.Join(strings.Fields(s[1:labelEnd-1]), " ")
	addr = strings.TrimSpace(s[dest+1 : end-1])
	if strings.HasPrefix(addr, "<") && strings.Contains(addr, ">") {
		addr, _, _ = strings.Cut(addr[1:], ">")
	} else {
		for _, sep := range []string{` "`, " '", " ("} {
			addr, _, _ = strings.Cut(addr, sep)
		}
	}
	addr = strings.NewReplacer(`\(`, "(", `\)`, ")").Replace(strings.Join(strings.Fields(addr), ""))
	return label, addr, end
}

func balancedEnd(s string, start int, open, close byte) int {
	depth := 0
	for i := start; i < len(s); i++ {
		switch s[i] {
		case '\\':
			i++
		case open:
			depth++
		case close:
			depth--
			if depth == 0 {
				return i + 1
			}
		}
	}
	return start
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

// SplitSemanticReply splits text first by explicit semantic break markers (`|||`)
// and then delegates overly long chunks to SplitReply, respecting maxParts.
func SplitSemanticReply(s string, target, maxParts int) []string {
	s = strings.TrimSpace(s)
	if s == "" {
		return nil
	}
	if maxParts <= 1 {
		return []string{strings.ReplaceAll(s, "|||", "\n")}
	}
	if !strings.Contains(s, "|||") {
		return SplitReply(s, target, maxParts)
	}
	rawSegments := strings.Split(s, "|||")
	var parts []string
	for _, seg := range rawSegments {
		seg = strings.TrimSpace(seg)
		if seg == "" {
			continue
		}
		if target > 0 && utf8.RuneCountInString(seg) > target {
			sub := SplitReply(seg, target, maxParts)
			parts = append(parts, sub...)
		} else {
			parts = append(parts, seg)
		}
	}
	if len(parts) == 0 {
		return []string{strings.TrimSpace(strings.ReplaceAll(s, "|||", ""))}
	}
	if len(parts) > maxParts {
		parts = append(parts[:maxParts-1], strings.Join(parts[maxParts-1:], "\n"))
	}
	return parts
}

// ReplyDelays computes cumulative delivery offsets for multi-part replies.
// When dynamic is false, it preserves the fixed 1500ms step; when dynamic is true,
// it simulates human typing duration based on each subsequent chunk's length while
// respecting remaining deadline headroom.
func ReplyDelays(parts []string, dynamic bool, remaining time.Duration) []time.Duration {
	if len(parts) == 0 {
		return nil
	}
	offsets := make([]time.Duration, len(parts))
	if !dynamic {
		for i := range parts {
			offsets[i] = time.Duration(i) * 1500 * time.Millisecond
		}
		return offsets
	}
	var cumulative time.Duration
	for i := 1; i < len(parts); i++ {
		runes := utf8.RuneCountInString(strings.TrimSpace(parts[i]))
		h := fnv.New32a()
		_, _ = h.Write([]byte(parts[i]))
		jitter := time.Duration(int(h.Sum32()%401)-150) * time.Millisecond // -150ms .. +250ms
		step := 700*time.Millisecond + time.Duration(runes)*45*time.Millisecond + jitter
		if step < 700*time.Millisecond {
			step = 700 * time.Millisecond
		}
		if step > 2800*time.Millisecond {
			step = 2800 * time.Millisecond
		}
		cumulative += step
		offsets[i] = cumulative
	}
	if remaining > 0 {
		budget := remaining - 10*time.Second
		if budget < 0 {
			budget = 0
		}
		if cumulative > budget && cumulative > 0 {
			scale := float64(budget) / float64(cumulative)
			for i := 1; i < len(offsets); i++ {
				offsets[i] = time.Duration(float64(offsets[i]) * scale)
			}
		}
	}
	return offsets
}
