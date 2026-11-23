package statuscard

import (
	"strconv"
	"strings"
	"testing"
	"time"

	"ai-bot/backend/internal/newapi"
)

func TestSnapshotCard(t *testing.T) {
	rate := 91.25
	group := newapi.ServiceRow{Name: "VIP", ServiceMetrics: newapi.ServiceMetrics{SuccessRate: &rate, RequestCount: 8}, Series: []newapi.ServiceMetrics{{SuccessRate: &rate}}}
	w := &newapi.ServiceWindow{GeneratedAt: 86400, Periods: []newapi.ServicePeriod{{Start: 86400}, {Start: 90000}}, Groups: []newapi.ServiceRow{group}, Overall: &group}
	for i := 0; i < 10; i++ {
		w.Models = append(w.Models, newapi.ServiceRow{Name: strconv.Itoa(i), ServiceMetrics: newapi.ServiceMetrics{RequestCount: int64(i)}})
	}
	s := newapi.Snapshot{SystemName: "</script><script>unexpected()</script>", FetchedAt: time.Now(), ServiceStatus: map[string]*newapi.ServiceWindow{"hour": w}, Monitors: []newapi.Monitor{{Status: 3, Uptime: 0.985}}}
	c := FromSnapshot(s, "vip分组正常吗？", "")
	if c.Focus == nil || *c.Focus != "VIP" || c.Window.Periods[0] != "08:00" || *c.Overall.Success != 91.25 || c.Groups[0].Series[1] != nil || len(c.Models) != 8 || c.Models[0].Name != "9" || c.Monitors[0].Status != "maintenance" || c.Monitors[0].Uptime != 98.5 {
		t.Fatalf("mismatched metrics: %+v", c)
	}
	doc, err := Document("brief", c)
	if err != nil || strings.Contains(doc, s.SystemName) {
		t.Fatal("unsafe document", err)
	}
}
