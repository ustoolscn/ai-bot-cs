// Package statuscard maps a saved live snapshot onto the fixed card templates.
package statuscard

import (
	"embed"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"
	"unicode/utf8"

	"ai-bot/backend/internal/newapi"
)

//go:embed templates/*.html
var templates embed.FS

type Row struct {
	Name     string     `json:"name,omitempty"`
	Success  *float64   `json:"successRate"`
	Requests *int64     `json:"requests"`
	TTFT     *float64   `json:"medianTtftMs"`
	Series   []*float64 `json:"series"`
}
type Monitor struct {
	Category string  `json:"category"`
	Name     string  `json:"name"`
	Status   string  `json:"status"`
	Uptime   float64 `json:"uptime"`
}
type Card struct {
	Site        string    `json:"site"`
	Title       string    `json:"title"`
	GeneratedAt time.Time `json:"generatedAt"`
	Window      struct {
		Label       string   `json:"label"`
		Granularity string   `json:"granularity"`
		Periods     []string `json:"periods"`
	} `json:"window"`
	Summary  string    `json:"summary"`
	Focus    *string   `json:"focus"`
	Overall  Row       `json:"overall"`
	Groups   []Row     `json:"groups"`
	Models   []Row     `json:"models"`
	Monitors []Monitor `json:"monitors"`
}

func row(src newapi.ServiceRow, n int) Row {
	r := Row{src.Name, src.SuccessRate, &src.RequestCount, src.MedianTTFT, make([]*float64, n)}
	for i := 0; i < min(n, len(src.Series)); i++ {
		r.Series[i] = src.Series[i].SuccessRate
	}
	return r
}

func FromSnapshot(s newapi.Snapshot, question, group string) Card {
	tz := time.FixedZone("CST", 8*3600)
	c := Card{Title: "服务状态", Site: s.SystemName, GeneratedAt: s.FetchedAt.In(tz), Groups: []Row{}, Models: []Row{}, Monitors: []Monitor{}}
	if c.Site == "" {
		c.Site = "服务监测"
	}
	c.Window.Label = "小时统计"
	c.Window.Granularity = "hour"
	c.Window.Periods = []string{}
	c.Overall.Series = []*float64{}
	if w := s.ServiceStatus["hour"]; w != nil {
		if w.GeneratedAt > 0 {
			c.GeneratedAt = time.Unix(w.GeneratedAt, 0).In(tz)
		}
		for _, period := range w.Periods {
			label := period.Label
			if period.Start > 0 {
				label = time.Unix(period.Start, 0).In(tz).Format("15:04")
			}
			c.Window.Periods = append(c.Window.Periods, label)
		}
		n := len(w.Periods)
		if n > 0 {
			c.Window.Label = fmt.Sprintf("近 %d 小时", n)
		}
		c.Overall.Series = make([]*float64, n)
		if w.Overall != nil {
			c.Overall = row(*w.Overall, n)
		}
		var matches []string
		for _, g := range w.Groups {
			c.Groups = append(c.Groups, row(g, n))
			if (group != "" && strings.EqualFold(group, g.Name)) || (group == "" && mentionsGroup(question, g.Name)) {
				matches = append(matches, g.Name)
			}
		}
		if len(matches) == 1 {
			c.Focus = &matches[0]
		}
		models := append([]newapi.ServiceRow(nil), w.Models...)
		sort.SliceStable(models, func(i, j int) bool { return models[i].RequestCount > models[j].RequestCount })
		for _, m := range models[:min(8, len(models))] {
			c.Models = append(c.Models, row(m, n))
		}
	}
	for _, m := range s.Monitors {
		states := [...]string{"down", "up", "pending", "maintenance"}
		status := "pending"
		if m.Status >= 0 && m.Status < len(states) {
			status = states[m.Status]
		}
		uptime := m.Uptime
		if uptime >= 0 && uptime <= 1 {
			uptime *= 100
		}
		c.Monitors = append(c.Monitors, Monitor{m.Category, m.Name, status, uptime})
	}
	return c
}

// ASCII model IDs such as gpt-5 should not select the group named gpt.
func mentionsGroup(question, name string) bool {
	if name == "" {
		return false
	}
	word := func(b byte) bool { return b >= 'a' && b <= 'z' || b >= '0' && b <= '9' || b == '_' || b == '-' }
	text, name := strings.ToLower(question), strings.ToLower(name)
	for start := 0; start < len(text); {
		i := strings.Index(text[start:], name)
		if i < 0 {
			return false
		}
		i += start
		end := i + len(name)
		if (i == 0 || !word(text[i-1])) && (end == len(text) || !word(text[end])) {
			return true
		}
		start = end
	}
	return false
}

// Supply verifiable clauses to the auxiliary model, so a fabricated rate or
// diagnosis cannot be printed on the image even if the model ignores its prompt.
func (c Card) Facts() []string {
	var facts []string
	for _, g := range c.Groups {
		if c.Focus != nil && g.Name == *c.Focus && g.Success != nil {
			facts = append(facts, fmt.Sprintf("%s 分组成功率为 %.2f%%", g.Name, *g.Success))
		}
	}
	if c.Overall.Success != nil {
		facts = append(facts, fmt.Sprintf("整体成功率为 %.2f%%", *c.Overall.Success))
	}
	var worst *Row
	for i := range c.Groups {
		group := &c.Groups[i]
		if group.Success != nil && (worst == nil || *group.Success < *worst.Success) {
			worst = group
		}
	}
	if worst != nil && len(c.Groups) > 1 {
		facts = append(facts, fmt.Sprintf("已列分组中 %s 成功率最低（%.2f%%）", worst.Name, *worst.Success))
	}
	if c.Overall.Requests != nil {
		facts = append(facts, fmt.Sprintf("共记录 %d 次请求", *c.Overall.Requests))
	}
	if c.Overall.TTFT != nil {
		facts = append(facts, fmt.Sprintf("首字延迟中位数为 %.0f 毫秒", *c.Overall.TTFT))
	}
	if len(c.Monitors) > 0 {
		up := 0
		for _, m := range c.Monitors {
			if m.Status == "up" {
				up++
			}
		}
		facts = append(facts, fmt.Sprintf("%d 个监测项中有 %d 个在线", len(c.Monitors), up))
	}
	out := facts[:0]
	for _, fact := range facts {
		if utf8.RuneCountInString(fact) < 50 {
			out = append(out, fact)
		}
	}
	if len(out) == 0 {
		return []string{"本次快照缺少可汇总的指标"}
	}
	return out
}

func Document(style string, c Card) (string, error) {
	switch style {
	case "brief", "table", "heatmap", "night":
	default:
		return "", fmt.Errorf("未知状态图样式")
	}
	tpl, err := templates.ReadFile("templates/" + style + ".html")
	if err != nil {
		return "", err
	}
	// encoding/json escapes HTML delimiters, including the start of </script>.
	data, err := json.Marshal(c)
	if err != nil {
		return "", err
	}
	head := `<head><meta http-equiv="Content-Security-Policy" content="default-src 'none'; script-src 'unsafe-inline'; style-src 'unsafe-inline'; base-uri 'none'; form-action 'none'"><script>window.STATUS_DATA=` + string(data) + ";</script>"
	return strings.Replace(string(tpl), "<head>", head, 1), nil
}
