package newapi

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
)

type ServiceMetrics struct {
	RequestCount int64    `json:"request_count"`
	SuccessCount int64    `json:"success_count"`
	SuccessRate  *float64 `json:"success_rate"`
	MedianTTFT   *float64 `json:"median_ttft_ms"`
}

type ServiceRow struct {
	ServiceMetrics
	Name   string           `json:"name"`
	Series []ServiceMetrics `json:"series,omitempty"`
}

type ServicePeriod struct {
	Start int64  `json:"bucket_start"`
	Label string `json:"bucket_label"`
}

type ServiceWindow struct {
	GeneratedAt     int64           `json:"generated_at"`
	Start           int64           `json:"start_timestamp"`
	End             int64           `json:"end_timestamp"`
	Granularity     string          `json:"granularity"`
	Periods         []ServicePeriod `json:"periods"`
	Overall         *ServiceRow     `json:"overall"`
	Models          []ServiceRow    `json:"models"`
	Groups          []ServiceRow    `json:"groups"`
	ModelsTruncated bool            `json:"models_truncated"`
	GroupsTruncated bool            `json:"groups_truncated"`
}

func (c *Client) fetchServiceWindow(ctx context.Context, granularity string) (*ServiceWindow, error) {
	body, err := c.get(ctx, "/api/service-status?granularity="+granularity)
	if err != nil {
		return nil, err
	}
	var result struct {
		Success bool           `json:"success"`
		Data    *ServiceWindow `json:"data"`
	}
	if err = json.Unmarshal(body, &result); err != nil {
		return nil, err
	}
	w := result.Data
	if !result.Success || w == nil || w.Overall == nil || w.GeneratedAt <= 0 || w.Granularity != granularity {
		return nil, fmt.Errorf("服务状态接口未返回有效的 %s 数据", granularity)
	}
	return w, nil
}

// Query returns a bounded selection so long histories do not fill the model context.
func (w *ServiceWindow) Query(scope, query string, recent int) (map[string]any, error) {
	var rows []ServiceRow
	var truncated bool
	switch scope {
	case "overall":
		rows = []ServiceRow{*w.Overall}
	case "model":
		rows = w.Models
		truncated = w.ModelsTruncated
	case "group":
		rows = w.Groups
		truncated = w.GroupsTruncated
	default:
		return nil, fmt.Errorf("scope 必须为 overall、model 或 group")
	}
	query = strings.ToLower(strings.TrimSpace(query))
	var matches []ServiceRow
	for _, row := range rows {
		name := strings.ToLower(row.Name)
		if query != "" && name == query {
			matches = []ServiceRow{row}
			break
		}
		if strings.Contains(name, query) {
			matches = append(matches, row)
		}
	}
	// Tool responses share a 6000-character context budget.
	limit := 8
	if recent > 0 {
		limit = min(limit, max(1, 24/recent))
	}
	count := len(matches)
	if count > limit {
		matches = matches[:limit]
	}
	result := make([]map[string]any, 0, len(matches))
	for _, row := range matches {
		item := map[string]any{"name": row.Name, "metrics": row.ServiceMetrics}
		if recent > 0 && len(row.Series) == len(w.Periods) {
			start := len(row.Series) - recent
			if start < 0 {
				start = 0
			}
			trend := make([]map[string]any, 0, len(row.Series)-start)
			for i := start; i < len(row.Series); i++ {
				trend = append(trend, map[string]any{"period": w.Periods[i], "metrics": row.Series[i]})
			}
			item["trend"] = trend
		}
		result = append(result, item)
	}
	return map[string]any{"scope": scope, "granularity": w.Granularity, "generated_at": w.GeneratedAt, "start_timestamp": w.Start, "end_timestamp": w.End, "matches": result, "matchCount": count, "sourceTruncated": truncated}, nil
}
