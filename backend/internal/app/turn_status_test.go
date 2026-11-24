package app

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"ai-bot/backend/internal/agent"
	"ai-bot/backend/internal/domain"
	"ai-bot/backend/internal/newapi"
	"ai-bot/backend/internal/statuscard"
)

func TestStatusToolsAndCardShareOnlyThisTurnsReading(t *testing.T) {
	var count, calls atomic.Int32
	site := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		n := count.Load()
		if n == 0 {
			http.Error(w, "unavailable", 503)
			return
		}
		switch r.URL.Path {
		case "/api/service-status":
			fmt.Fprintf(w, `{"success":true,"data":{"generated_at":123,"granularity":%q,"overall":{"request_count":%d,"success_count":%d}}}`, r.URL.Query().Get("granularity"), n, n)
		case "/api/uptime/status":
			fmt.Fprintf(w, `{"success":true,"data":[{"categoryName":"分组","monitors":[{"name":"monitor-%d","status":1}]}]}`, n)
		default:
			t.Errorf("unrelated endpoint fetched: %s", r.URL.Path)
		}
	}))
	defer site.Close()
	s := agent.DefaultSettings()
	s.LiveData.Enabled, s.LiveData.BaseURL, s.LiveData.StatusImageStyle = true, site.URL, "off"
	old := newapi.Snapshot{Monitors: []newapi.Monitor{{Name: "obsolete"}}, ServiceStatus: map[string]*newapi.ServiceWindow{"hour": {Overall: &newapi.ServiceRow{}}}}
	a := App{}
	ctx := context.Background()
	for i, n := range []int32{11, 22, 0} {
		count.Store(n)
		rt := turnRuntime{settings: s, snapshot: old, snapshotOK: i != 0}
		answer, err := a.executeTool(ctx, &rt, domain.ToolCall{Name: "get_service_status", Arguments: `{"scope":"overall"}`}, nil)
		groups, groupErr := a.executeTool(ctx, &rt, domain.ToolCall{Name: "get_group_status"}, nil)
		if calls.Load() != int32(3*(i+1)) {
			t.Fatalf("unexpected upstream calls: %d", calls.Load())
		}
		if n == 0 {
			if err == nil || groupErr == nil || rt.liveStatus.ServiceStatus["hour"] != nil || len(rt.liveStatus.Monitors) != 0 {
				t.Fatal("failed request used old statistics", answer, groups)
			}
			break
		}
		if err != nil || groupErr != nil || !strings.Contains(answer, fmt.Sprintf(`"request_count":%d`, n)) || !strings.Contains(groups, fmt.Sprintf("monitor-%d", n)) {
			t.Fatal(answer, err, groups, groupErr)
		}
		card := statuscard.FromSnapshot(*rt.liveStatus, "", "")
		if card.Overall.Requests == nil || *card.Overall.Requests != int64(n) || len(card.Monitors) != 1 || card.Monitors[0].Name != fmt.Sprintf("monitor-%d", n) {
			t.Fatal(card)
		}
	}
}
