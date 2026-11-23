package newapi

import (
	"fmt"
	"math"
	"sort"
	"strings"
)

// aliases maps colloquial names users type to model-name fragments.
var aliases = map[string]string{
	"克劳德": "claude", "claud": "claude", "cluade": "claude",
	"双子座": "gemini", "杰米尼": "gemini",
	"深度求索": "deepseek", "ds": "deepseek",
	"千问": "qwen", "通义": "qwen",
	"豆包": "doubao", "智谱": "glm", "kimi": "moonshot",
	"四o": "4o", "4欧": "4o",
}

func normalizeName(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	for k, v := range aliases {
		s = strings.ReplaceAll(s, k, v)
	}
	repl := strings.NewReplacer(" ", "-", "_", "-", ".", "-", "—", "-")
	return repl.Replace(s)
}

// FindModels returns models matching a user query, best matches first.
func (s Snapshot) FindModels(query string, limit int) []ModelPrice {
	q := normalizeName(query)
	if q == "" {
		return nil
	}
	tokens := strings.FieldsFunc(q, func(r rune) bool { return r == '-' || r == '/' })
	type scored struct {
		m     ModelPrice
		score int
	}
	var hits []scored
	for _, m := range s.Models {
		name := normalizeName(m.ModelName)
		score := 0
		switch {
		case name == q:
			score = 1000
		case strings.HasPrefix(name, q):
			score = 500 - len(name)
		case strings.Contains(name, q):
			score = 300 - len(name)
		default:
			matched := 0
			for _, t := range tokens {
				if strings.Contains(name, t) {
					matched++
				}
			}
			if len(tokens) > 0 && matched == len(tokens) {
				score = 100 + matched*10 - len(name)
			}
		}
		if score > 0 {
			hits = append(hits, scored{m, score})
		}
	}
	sort.SliceStable(hits, func(i, j int) bool { return hits[i].score > hits[j].score })
	if limit <= 0 {
		limit = 5
	}
	var out []ModelPrice
	for i, h := range hits {
		if i >= limit {
			break
		}
		out = append(out, h.m)
	}
	return out
}

// PriceQuote is a computed price for one model in one group.
type PriceQuote struct {
	Model            string   `json:"model"`
	Group            string   `json:"group"`
	GroupName        string   `json:"groupName,omitempty"`
	GroupRatio       float64  `json:"groupRatio"`
	Billing          string   `json:"billing"` // per_token | per_call
	InputPerMillion  *float64 `json:"inputUsdPerMillionTokens,omitempty"`
	OutputPerMillion *float64 `json:"outputUsdPerMillionTokens,omitempty"`
	PerCall          *float64 `json:"usdPerCall,omitempty"`
	AvailableGroups  []string `json:"availableGroups,omitempty"`
	Note             string   `json:"note,omitempty"`
}

// Quote computes a model's price in a group using new-api conventions:
// per-token: input $/1M = model_ratio*2, output = input*completion_ratio;
// per-call: model_price; both multiplied by the group ratio.
func (s Snapshot) Quote(m ModelPrice, group string) PriceQuote {
	if group == "" {
		group = "default"
	}
	ratio, ok := s.GroupRatio[group]
	if !ok {
		ratio = 1
	}
	q := PriceQuote{Model: m.ModelName, Group: group, GroupName: s.UsableGroups[group], GroupRatio: ratio, AvailableGroups: m.EnableGroups}
	if len(m.EnableGroups) > 0 && !contains(m.EnableGroups, group) && !contains(m.EnableGroups, "all") {
		q.Note = fmt.Sprintf("该模型不在 %s 分组中可用", group)
	}
	if m.QuotaType == 1 {
		q.Billing = "per_call"
		v := round(m.ModelPrice * ratio)
		q.PerCall = &v
		return q
	}
	q.Billing = "per_token"
	in := round(m.ModelRatio * 2 * ratio)
	out := round(m.ModelRatio * 2 * m.CompletionRatio * ratio)
	q.InputPerMillion, q.OutputPerMillion = &in, &out
	return q
}

// GroupList returns groups with their ratios, sorted by name.
func (s Snapshot) GroupList() []map[string]any {
	names := make([]string, 0, len(s.GroupRatio))
	for g := range s.GroupRatio {
		names = append(names, g)
	}
	for g := range s.UsableGroups {
		if _, ok := s.GroupRatio[g]; !ok {
			names = append(names, g)
		}
	}
	sort.Strings(names)
	var out []map[string]any
	for _, g := range names {
		item := map[string]any{"group": g}
		if r, ok := s.GroupRatio[g]; ok {
			item["ratio"] = r
		}
		if d := s.UsableGroups[g]; d != "" {
			item["description"] = d
		}
		out = append(out, item)
	}
	return out
}

func contains(list []string, v string) bool {
	for _, x := range list {
		if x == v {
			return true
		}
	}
	return false
}

func round(v float64) float64 { return math.Round(v*10000) / 10000 }
