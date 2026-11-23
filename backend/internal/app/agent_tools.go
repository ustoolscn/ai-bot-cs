package app

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"

	"ai-bot/backend/internal/agent"
	"ai-bot/backend/internal/domain"
	"ai-bot/backend/internal/newapi"
)

// turnRuntime is the per-turn state tools operate on.
type turnRuntime struct {
	settings         agent.Settings
	botID            string
	conversationID   string
	conversationType string
	senderID         string
	senderName       string
	messageID        string
	runID            string
	knowledgeBases   []string
	snapshot         newapi.Snapshot
	snapshotOK       bool
	snapshotStale    bool
	dryRun           bool // evaluation: no side effects
	handoff          bool
	hits             []map[string]any
	toolsUsed        []string
	screenshot       *domain.ContentPart
}

type builtinTool struct {
	spec      domain.ToolSpec
	needsLive bool
	needsKB   bool
}

var builtinTools = []builtinTool{
	{spec: domain.ToolSpec{Name: "render_html_image", Description: "把你编写的静态 HTML/CSS 排版成 PNG，直接作为本次 QQ 回复发送。适用于用户要求图片、海报、表格或网页截图。使用内联样式或 style，按 1000px 宽排版，不使用脚本和外部资源。", Parameters: objSchema(map[string]any{"html": strProp("完整 HTML 网页或片段")}, "html")}},
	{spec: domain.ToolSpec{Name: "search_knowledge", Description: "在知识库中检索资料。当预先提供的知识库结果不足以回答、或用户换了话题时使用。", Parameters: objSchema(map[string]any{"query": strProp("要检索的完整问题")}, "query")}, needsKB: true},
	{spec: domain.ToolSpec{Name: "get_model_price", Description: "查询网站上模型的实时价格（美元）。支持模糊模型名，如 4o、claude sonnet、deepseek。", Parameters: objSchema(map[string]any{"model": strProp("模型名称或关键词"), "group": strProp("分组名，可选，默认 default")}, "model")}, needsLive: true},
	{spec: domain.ToolSpec{Name: "get_service_status", Description: "按全站、模型或计费分组查询成功率（%）、请求数、首个 Token 延迟中位数（毫秒）和趋势。无数据时不要推断故障。", Parameters: objSchema(map[string]any{
		"scope":       map[string]any{"type": "string", "enum": []string{"overall", "model", "group"}},
		"name":        strProp("模型或分组名称，可模糊匹配"),
		"granularity": map[string]any{"type": "string", "enum": []string{"hour", "day"}, "description": "hour 为最近24小时（默认），day 为最近90天"},
		"recent":      map[string]any{"type": "integer", "minimum": 1, "maximum": 24, "description": "附带最近几个小时或天的逐段统计，可选"},
	}, "scope")}, needsLive: true},
	{spec: domain.ToolSpec{Name: "get_group_status", Description: "查询网站分组列表（倍率）和各分组/服务的实时可用状态。", Parameters: objSchema(map[string]any{"group": strProp("分组或服务名称，可选；为空返回全部")})}, needsLive: true},
	{spec: domain.ToolSpec{Name: "get_announcements", Description: "获取网站最新公告和通知全文。", Parameters: objSchema(map[string]any{"limit": map[string]any{"type": "integer", "description": "返回条数，默认 3"}})}, needsLive: true},
	{spec: domain.ToolSpec{Name: "remember_user_fact", Description: "记住关于当前用户的一条长期有用的信息（如常用分组、接入方式、偏好）。不要记录密钥、密码等敏感信息。", Parameters: objSchema(map[string]any{"content": strProp("一句话描述的事实"), "kind": map[string]any{"type": "string", "enum": []string{"fact", "preference", "issue"}}}, "content")}},
	{spec: domain.ToolSpec{Name: "handoff_to_human", Description: "把当前用户转给人工客服处理。用户要求人工、涉及退款/到账/封号等需人工核实、或你无法解决时使用。", Parameters: objSchema(map[string]any{"reason": strProp("转人工原因，一句话")}, "reason")}},
}

func objSchema(props map[string]any, required ...string) map[string]any {
	s := map[string]any{"type": "object", "properties": props}
	if len(required) > 0 {
		s["required"] = required
	}
	return s
}

func strProp(desc string) map[string]any {
	return map[string]any{"type": "string", "description": desc}
}

func isBuiltinTool(name string) bool {
	for _, t := range builtinTools {
		if t.spec.Name == name {
			return true
		}
	}
	return false
}

type customTool struct {
	ID, Name, Description string
	Parameters            map[string]any
	Config                httpToolConfig
}

type httpToolConfig struct {
	Method         string            `json:"method"`
	URL            string            `json:"url"`
	Headers        map[string]string `json:"headers"`
	TimeoutSeconds int               `json:"timeoutSeconds"`
}

var toolNamePattern = regexp.MustCompile(`^[a-zA-Z][a-zA-Z0-9_-]{0,63}$`)

func (a *App) loadCustomTools(ctx context.Context, onlyEnabled bool) ([]customTool, error) {
	q := "SELECT id::text,name,description,config_enc,parameters FROM tool_definitions WHERE kind='http'"
	if onlyEnabled {
		q += " AND enabled"
	}
	rows, err := a.db.Query(ctx, q+" ORDER BY name")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []customTool
	for rows.Next() {
		var t customTool
		var enc string
		var params []byte
		if err := rows.Scan(&t.ID, &t.Name, &t.Description, &enc, &params); err != nil {
			return nil, err
		}
		_ = json.Unmarshal(params, &t.Parameters)
		if enc != "" {
			if plain, err := a.cipher.Decrypt(enc); err == nil {
				_ = json.Unmarshal([]byte(plain), &t.Config)
			}
		}
		out = append(out, t)
	}
	return out, rows.Err()
}

// availableTools lists tools the model may call this turn.
func (a *App) availableTools(ctx context.Context, rt *turnRuntime) ([]domain.ToolSpec, map[string]customTool) {
	var specs []domain.ToolSpec
	for _, t := range builtinTools {
		if t.needsLive && !rt.snapshotOK {
			continue
		}
		if t.needsKB && len(rt.knowledgeBases) == 0 {
			continue
		}
		specs = append(specs, t.spec)
	}
	custom := map[string]customTool{}
	tools, err := a.loadCustomTools(ctx, true)
	if err == nil {
		for _, t := range tools {
			params := t.Parameters
			if params == nil {
				params = objSchema(map[string]any{})
			}
			specs = append(specs, domain.ToolSpec{Name: t.Name, Description: t.Description, Parameters: params})
			custom[t.Name] = t
		}
	}
	return specs, custom
}

// executeTool runs one tool call and returns a JSON string for the model.
func (a *App) executeTool(ctx context.Context, rt *turnRuntime, call domain.ToolCall, custom map[string]customTool) (string, error) {
	var args map[string]any
	if strings.TrimSpace(call.Arguments) != "" {
		if err := json.Unmarshal([]byte(call.Arguments), &args); err != nil {
			return "", fmt.Errorf("工具参数不是合法 JSON")
		}
	}
	str := func(k string) string { v, _ := args[k].(string); return strings.TrimSpace(v) }
	rt.toolsUsed = append(rt.toolsUsed, call.Name)
	switch call.Name {
	case "render_html_image":
		if rt.dryRun {
			return jsonString(map[string]any{"preview": true}), nil
		}
		part, err := a.htmlImage(ctx, str("html"))
		if err != nil {
			return "", err
		}
		rt.screenshot = &part
		return jsonString(map[string]any{"ready": true}), nil
	case "search_knowledge":
		q := str("query")
		if q == "" {
			return "", fmt.Errorf("query 必填")
		}
		text, hits, err := a.retrieve(ctx, rt.knowledgeBases, q)
		if err != nil {
			return "", err
		}
		rt.hits = append(rt.hits, hits...)
		if text == "" {
			return `{"result":"没有检索到相关资料"}`, nil
		}
		return jsonString(map[string]any{"results": text}), nil
	case "get_model_price":
		if !rt.snapshotOK {
			return "", fmt.Errorf("网站数据暂不可用")
		}
		models := rt.snapshot.FindModels(str("model"), 5)
		if len(models) == 0 {
			return jsonString(map[string]any{"found": false, "message": "没有找到匹配的模型，请让用户确认模型名称", "groups": rt.snapshot.GroupList()}), nil
		}
		var quotes []newapi.PriceQuote
		for _, m := range models {
			quotes = append(quotes, rt.snapshot.Quote(m, str("group")))
		}
		return jsonString(map[string]any{"found": true, "unit": "美元。Token 单价按百万计。阶梯公式 baseBillingExpr 中 p/c/cr/cc 分别为输入、输出、缓存读取、缓存写入，系数为分组倍率前单价；按原条件选档并应用服务或时段系数，结果再乘 groupRatio 一次。视频费用为对应分辨率生成秒数乘输出秒价，加输入视频秒数乘输入秒价。视频、普通 Token 和按次单价已含分组倍率。unknown 表示价格待同步或规则不明。", "quotes": quotes, "dataTime": rt.snapshot.FetchedAt, "possiblyStale": rt.snapshotStale, "note": rt.settings.LiveData.PriceNote}), nil
	case "get_service_status":
		g := str("granularity")
		if g == "" {
			g = "hour"
		}
		if g != "hour" && g != "day" {
			return "", fmt.Errorf("granularity 必须为 hour 或 day")
		}
		w := rt.snapshot.ServiceStatus[g]
		if !rt.snapshotOK || w == nil || w.Overall == nil {
			return "", fmt.Errorf("该时间范围的服务状态暂无可用数据")
		}
		recent := 0
		if v, ok := args["recent"]; ok {
			n, ok := v.(float64)
			if !ok || n < 1 || n > 24 || float64(int(n)) != n {
				return "", fmt.Errorf("recent 必须是 1 到 24 之间的整数")
			}
			recent = int(n)
		}
		result, err := w.Query(str("scope"), str("name"), recent)
		if err != nil {
			return "", err
		}
		result["possiblyStale"] = rt.snapshotStale
		return jsonString(result), nil
	case "get_group_status":
		if !rt.snapshotOK {
			return "", fmt.Errorf("网站数据暂不可用")
		}
		g := strings.ToLower(str("group"))
		var monitors []map[string]any
		for _, m := range rt.snapshot.Monitors {
			if g != "" && !strings.Contains(strings.ToLower(m.Name+" "+m.Category+" "+m.Group), g) {
				continue
			}
			monitors = append(monitors, map[string]any{"category": m.Category, "name": m.Name, "status": newapi.MonitorStatusText(m.Status), "uptime": m.Uptime})
		}
		return jsonString(map[string]any{"groups": rt.snapshot.GroupList(), "status": monitors, "dataTime": rt.snapshot.FetchedAt, "possiblyStale": rt.snapshotStale}), nil
	case "get_announcements":
		if !rt.snapshotOK {
			return "", fmt.Errorf("网站数据暂不可用")
		}
		limit := 3
		if v, ok := args["limit"].(float64); ok && v >= 1 && v <= 10 {
			limit = int(v)
		}
		anns := rt.snapshot.Announcements
		if len(anns) > limit {
			anns = anns[:limit]
		}
		return jsonString(map[string]any{"notice": rt.snapshot.Notice, "announcements": anns}), nil
	case "remember_user_fact":
		content := str("content")
		if content == "" {
			return "", fmt.Errorf("content 必填")
		}
		if rt.dryRun || rt.senderID == "" {
			return `{"saved":true}`, nil
		}
		kind := str("kind")
		if kind == "" {
			kind = "fact"
		}
		if err := a.saveMemory(ctx, rt.settings, rt.botID, rt.conversationID, rt.senderID, rt.senderName, kind, content, 0.9, "tool", rt.messageID); err != nil {
			return "", err
		}
		return `{"saved":true}`, nil
	case "handoff_to_human":
		rt.handoff = true
		if rt.dryRun {
			return `{"ok":true,"message":"已通知人工客服"}`, nil
		}
		pause := time.Now().Add(time.Duration(rt.settings.HandoffPauseMinutes) * time.Minute)
		_, err := a.db.Exec(ctx, `INSERT INTO handoffs(bot_id,conversation_id,platform_user_id,display_name,message_id,reason,paused_until) VALUES($1,$2,$3,$4,$5,$6,$7)`,
			rt.botID, rt.conversationID, rt.senderID, rt.senderName, nullString(rt.messageID), str("reason"), pause)
		if err != nil {
			return "", err
		}
		_, _ = a.db.Exec(ctx, "INSERT INTO system_alerts(kind,title,detail) VALUES('handoff',$1,$2::jsonb)", "用户请求人工："+firstNonEmpty(rt.senderName, rt.senderID), jsonString(map[string]any{"conversationId": rt.conversationID, "reason": str("reason")}))
		return `{"ok":true,"message":"已通知人工客服，告诉用户稍等即可"}`, nil
	}
	if t, ok := custom[call.Name]; ok {
		if rt.dryRun && strings.ToUpper(t.Config.Method) != "GET" {
			return `{"dryRun":true}`, nil
		}
		return a.callHTTPTool(ctx, t, args)
	}
	return "", fmt.Errorf("未知工具 %s", call.Name)
}

func (a *App) callHTTPTool(ctx context.Context, t customTool, args map[string]any) (string, error) {
	method := strings.ToUpper(t.Config.Method)
	if method == "" {
		method = http.MethodGet
	}
	timeout := time.Duration(t.Config.TimeoutSeconds) * time.Second
	if timeout <= 0 || timeout > 30*time.Second {
		timeout = 10 * time.Second
	}
	target := t.Config.URL
	used := map[string]bool{}
	for k, v := range args {
		ph := "{" + k + "}"
		if strings.Contains(target, ph) {
			target = strings.ReplaceAll(target, ph, url.PathEscape(fmt.Sprint(v)))
			used[k] = true
		}
	}
	var body io.Reader
	if method == http.MethodGet {
		u, err := url.Parse(target)
		if err != nil {
			return "", fmt.Errorf("工具 URL 无效")
		}
		q := u.Query()
		for k, v := range args {
			if !used[k] {
				q.Set(k, fmt.Sprint(v))
			}
		}
		u.RawQuery = q.Encode()
		target = u.String()
	} else {
		b, _ := json.Marshal(args)
		body = bytes.NewReader(b)
	}
	cctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	req, err := http.NewRequestWithContext(cctx, method, target, body)
	if err != nil {
		return "", err
	}
	req.Header.Set("Accept", "application/json")
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	for k, v := range t.Config.Headers {
		req.Header.Set(k, v)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
	if resp.StatusCode/100 != 2 {
		return "", fmt.Errorf("接口返回 HTTP %d: %s", resp.StatusCode, agent.TruncateRunes(string(data), 300))
	}
	return string(data), nil
}

func jsonString(v any) string {
	b, _ := json.Marshal(v)
	return string(b)
}

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if strings.TrimSpace(v) != "" {
			return v
		}
	}
	return ""
}
