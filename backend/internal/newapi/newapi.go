// Package newapi reads public live data (announcements, group/monitor status,
// model pricing) from a new-api (QuantumNous/new-api) site.
package newapi

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strings"
	"time"
)

// Client fetches data from a new-api site.
type Client struct {
	BaseURL     string
	AccessToken string // optional system access token
	UserID      string // optional, sent as New-Api-User
	HTTP        *http.Client
}

func NewClient(baseURL, token, userID string) *Client {
	return &Client{BaseURL: strings.TrimRight(baseURL, "/"), AccessToken: token, UserID: userID, HTTP: &http.Client{Timeout: 15 * time.Second}}
}

type envelope struct {
	Success bool            `json:"success"`
	Message string          `json:"message"`
	Data    json.RawMessage `json:"data"`
}

func (c *Client) get(ctx context.Context, path string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.BaseURL+path, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/json")
	if c.AccessToken != "" {
		req.Header.Set("Authorization", c.AccessToken)
	}
	if c.UserID != "" {
		req.Header.Set("New-Api-User", c.UserID)
	}
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	if err != nil {
		return nil, err
	}
	if resp.StatusCode/100 != 2 {
		return nil, fmt.Errorf("%s 返回 HTTP %d", path, resp.StatusCode)
	}
	return body, nil
}

// Announcement is one site announcement.
type Announcement struct {
	Content     string `json:"content"`
	PublishDate string `json:"publishDate"`
	Type        string `json:"type"`
	Extra       string `json:"extra,omitempty"`
}

// Monitor is one uptime monitor (new-api exposes Uptime Kuma groups).
type Monitor struct {
	Category string  `json:"category"`
	Name     string  `json:"name"`
	Group    string  `json:"group,omitempty"`
	Status   int     `json:"status"` // 1 up, 0 down, 2 pending, 3 maintenance
	Uptime   float64 `json:"uptime"`
}

type VideoPrice struct {
	BaseFPS        int                `json:"base_fps,omitempty"`
	Prices         map[string]float64 `json:"prices"`
	InputPerSecond *float64           `json:"input_video_price_per_second,omitempty"`
}

// ModelPrice is one model entry from /api/pricing.
type ModelPrice struct {
	BillingMode      string      `json:"billing_mode,omitempty"`
	BillingExpr      string      `json:"billing_expr,omitempty"`
	VideoPrice       *VideoPrice `json:"video_price,omitempty"`
	CacheRatio       *float64    `json:"cache_ratio,omitempty"`
	CreateCacheRatio *float64    `json:"create_cache_ratio,omitempty"`
	ModelName        string      `json:"model_name"`
	QuotaType        int         `json:"quota_type"` // 0 per-token ratio, 1 per-call price
	ModelRatio       float64     `json:"model_ratio"`
	ModelPrice       float64     `json:"model_price"`
	CompletionRatio  float64     `json:"completion_ratio"`
	OwnerBy          string      `json:"owner_by,omitempty"`
	EnableGroups     []string    `json:"enable_groups,omitempty"`
	Description      string      `json:"description,omitempty"`
}

// Snapshot is all live data at one point in time.
type Snapshot struct {
	ServiceStatus map[string]*ServiceWindow `json:"serviceStatus,omitempty"`
	SystemName    string                    `json:"systemName,omitempty"`
	Notice        string                    `json:"notice,omitempty"`
	Announcements []Announcement            `json:"announcements,omitempty"`
	Monitors      []Monitor                 `json:"monitors,omitempty"`
	Models        []ModelPrice              `json:"models,omitempty"`
	GroupRatio    map[string]float64        `json:"groupRatio,omitempty"`
	UsableGroups  map[string]string         `json:"usableGroups,omitempty"`
	FetchedAt     time.Time                 `json:"fetchedAt"`
	Errors        map[string]string         `json:"errors,omitempty"`
}

// FetchStatus reads only the two statistics windows and the uptime monitors.
func (c *Client) FetchStatus(ctx context.Context) Snapshot {
	s := Snapshot{Errors: map[string]string{}}
	type serviceResult struct {
		granularity string
		window      *ServiceWindow
		err         error
	}
	results := make(chan serviceResult, 2)
	for _, g := range []string{"hour", "day"} {
		go func(g string) { w, err := c.fetchServiceWindow(ctx, g); results <- serviceResult{g, w, err} }(g)
	}
	if body, err := c.get(ctx, "/api/uptime/status"); err != nil {
		s.Errors["uptime"] = err.Error()
	} else if monitors, err := ParseUptime(body); err != nil {
		s.Errors["uptime"] = err.Error()
	} else {
		s.Monitors = monitors
	}
	for i := 0; i < 2; i++ {
		r := <-results
		if r.err != nil {
			s.Errors["service-status/"+r.granularity] = r.err.Error()
			continue
		}
		if s.ServiceStatus == nil {
			s.ServiceStatus = map[string]*ServiceWindow{}
		}
		s.ServiceStatus[r.granularity] = r.window
	}
	s.FetchedAt = time.Now().UTC()
	return s
}

// Fetch loads every source; a failing source is recorded in Errors and does
// not prevent the others from loading. It errors only if all sources fail.
func (c *Client) Fetch(ctx context.Context) (Snapshot, error) {
	statusResult := make(chan Snapshot, 1)
	go func() { statusResult <- c.FetchStatus(ctx) }()
	s := Snapshot{Errors: map[string]string{}}
	ok := 0
	if body, err := c.get(ctx, "/api/notice"); err != nil {
		s.Errors["notice"] = err.Error()
	} else if notice, err := ParseNotice(body); err != nil {
		s.Errors["notice"] = err.Error()
	} else {
		s.Notice = notice
		ok++
	}
	if body, err := c.get(ctx, "/api/status"); err != nil {
		s.Errors["status"] = err.Error()
	} else if name, anns, err := ParseStatus(body); err != nil {
		s.Errors["status"] = err.Error()
	} else {
		s.SystemName, s.Announcements = name, anns
		ok++
	}
	if body, err := c.get(ctx, "/api/pricing"); err != nil {
		s.Errors["pricing"] = err.Error()
	} else if models, ratio, usable, err := ParsePricing(body); err != nil {
		s.Errors["pricing"] = err.Error()
	} else {
		s.Models, s.GroupRatio, s.UsableGroups = models, ratio, usable
		ok++
	}
	status := <-statusResult
	s.ServiceStatus, s.Monitors = status.ServiceStatus, status.Monitors
	for source, message := range status.Errors {
		s.Errors[source] = message
	}
	ok += len(status.ServiceStatus)
	if status.Errors["uptime"] == "" {
		ok++
	}
	s.FetchedAt = time.Now().UTC()
	if len(s.Errors) == 0 {
		s.Errors = nil
	}
	if ok == 0 {
		return s, fmt.Errorf("所有数据源均获取失败")
	}
	return s, nil
}

func unwrap(body []byte) (envelope, error) {
	var e envelope
	if err := json.Unmarshal(body, &e); err != nil {
		return e, fmt.Errorf("返回不是 JSON: %w", err)
	}
	if !e.Success && len(e.Data) == 0 {
		return e, fmt.Errorf("接口返回失败: %s", e.Message)
	}
	return e, nil
}

// ParseNotice parses GET /api/notice.
func ParseNotice(body []byte) (string, error) {
	e, err := unwrap(body)
	if err != nil {
		return "", err
	}
	var s string
	if len(e.Data) > 0 && string(e.Data) != "null" {
		if err := json.Unmarshal(e.Data, &s); err != nil {
			return "", err
		}
	}
	return strings.TrimSpace(s), nil
}

// ParseStatus parses GET /api/status for the system name and announcements.
func ParseStatus(body []byte) (string, []Announcement, error) {
	e, err := unwrap(body)
	if err != nil {
		return "", nil, err
	}
	var d struct {
		SystemName           string         `json:"system_name"`
		AnnouncementsEnabled *bool          `json:"announcements_enabled"`
		Announcements        []Announcement `json:"announcements"`
	}
	if err := json.Unmarshal(e.Data, &d); err != nil {
		return "", nil, err
	}
	if d.AnnouncementsEnabled != nil && !*d.AnnouncementsEnabled {
		d.Announcements = nil
	}
	sort.SliceStable(d.Announcements, func(i, j int) bool { return d.Announcements[i].PublishDate > d.Announcements[j].PublishDate })
	return d.SystemName, d.Announcements, nil
}

// ParseUptime parses GET /api/uptime/status.
func ParseUptime(body []byte) ([]Monitor, error) {
	e, err := unwrap(body)
	if err != nil {
		return nil, err
	}
	if !e.Success {
		return nil, fmt.Errorf("监控接口返回失败")
	}
	var groups []struct {
		CategoryName string `json:"categoryName"`
		Monitors     []struct {
			Name   string  `json:"name"`
			Group  string  `json:"group"`
			Status int     `json:"status"`
			Uptime float64 `json:"uptime"`
		} `json:"monitors"`
	}
	if len(e.Data) == 0 || string(e.Data) == "null" {
		return nil, nil
	}
	if err := json.Unmarshal(e.Data, &groups); err != nil {
		return nil, err
	}
	var out []Monitor
	for _, g := range groups {
		for _, m := range g.Monitors {
			out = append(out, Monitor{Category: g.CategoryName, Name: m.Name, Group: m.Group, Status: m.Status, Uptime: m.Uptime})
		}
	}
	return out, nil
}

// ParsePricing parses GET /api/pricing.
func ParsePricing(body []byte) ([]ModelPrice, map[string]float64, map[string]string, error) {
	var d struct {
		Success     bool               `json:"success"`
		Message     string             `json:"message"`
		Data        []ModelPrice       `json:"data"`
		GroupRatio  map[string]float64 `json:"group_ratio"`
		UsableGroup map[string]string  `json:"usable_group"`
	}
	if err := json.Unmarshal(body, &d); err != nil {
		return nil, nil, nil, fmt.Errorf("价格接口返回无法解析: %w", err)
	}
	if !d.Success && len(d.Data) == 0 {
		return nil, nil, nil, fmt.Errorf("价格接口返回失败: %s", d.Message)
	}
	for i := range d.Data {
		if d.Data[i].BillingMode == "" {
			switch d.Data[i].QuotaType {
			case 0:
				d.Data[i].BillingMode = "per_token"
			case 1:
				d.Data[i].BillingMode = "per_call"
			default:
				d.Data[i].BillingMode = "unknown"
			}
		}
		if d.Data[i].CompletionRatio == 0 {
			d.Data[i].CompletionRatio = 1
		}
	}
	return d.Data, d.GroupRatio, d.UsableGroup, nil
}

// MonitorStatusText describes an uptime status code.
func MonitorStatusText(status int) string {
	switch status {
	case 1:
		return "正常"
	case 0:
		return "异常"
	case 2:
		return "检测中"
	case 3:
		return "维护中"
	default:
		return "未知"
	}
}

// PromptSummary is the short live-data block injected into every prompt.
func (s Snapshot) PromptSummary(stale bool) string {
	var b strings.Builder
	if !s.FetchedAt.IsZero() {
		fmt.Fprintf(&b, "数据时间：%s", s.FetchedAt.In(chinaTZ).Format("01-02 15:04"))
		if stale {
			b.WriteString("（缓存快照可能不是最新，请以标注的数据时间为准）")
		}
		b.WriteString("\n")
	}
	if len(s.Announcements) > 0 {
		b.WriteString("最新公告：")
		for i, a := range s.Announcements {
			if i >= 2 {
				break
			}
			fmt.Fprintf(&b, "\n- %s %s", shortDate(a.PublishDate), oneLine(a.Content, 120))
		}
		b.WriteString("\n")
	} else if s.Notice != "" {
		b.WriteString("站点通知：" + oneLine(s.Notice, 200) + "\n")
	}
	if !s.FetchedAt.IsZero() {
		b.WriteString("分组监控请调用 get_group_status；成功率、请求数和首个 Token 延迟请调用 get_service_status，以本次查询为准。\n")
	}
	if len(s.Models) > 0 {
		b.WriteString("模型价格请调用 get_model_price 查询，不要凭记忆回答。")
	}
	return strings.TrimSpace(b.String())
}

var chinaTZ = time.FixedZone("CST", 8*3600)

func shortDate(s string) string {
	if t, err := time.Parse(time.RFC3339, s); err == nil {
		return t.In(chinaTZ).Format("01-02")
	}
	if len(s) >= 10 {
		return s[:10]
	}
	return s
}

func oneLine(s string, n int) string {
	s = strings.Join(strings.Fields(stripMarkdown(s)), " ")
	r := []rune(s)
	if len(r) > n {
		return string(r[:n]) + "…"
	}
	return s
}

func stripMarkdown(s string) string {
	r := strings.NewReplacer("#", "", "**", "", "__", "", "`", "", ">", "")
	return r.Replace(s)
}
