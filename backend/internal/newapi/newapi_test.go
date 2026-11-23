package newapi

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

const pricingJSON = `{"success":true,"data":[
 {"model_name":"gpt-4o","quota_type":0,"model_ratio":1.25,"model_price":0,"completion_ratio":4,"enable_groups":["default","vip"]},
 {"model_name":"gpt-4o-mini","quota_type":0,"model_ratio":0.075,"completion_ratio":4,"enable_groups":["default"]},
 {"model_name":"claude-sonnet-4-5-20250929","quota_type":0,"model_ratio":1.5,"completion_ratio":5,"enable_groups":["default"]},
 {"model_name":"mj_imagine","quota_type":1,"model_price":0.1,"enable_groups":["default"]}
],"group_ratio":{"default":1,"vip":0.8},"usable_group":{"default":"默认分组","vip":"VIP 分组"}}`

func TestParseAndQuote(t *testing.T) {
	models, ratio, usable, err := ParsePricing([]byte(pricingJSON))
	if err != nil || len(models) != 4 || ratio["vip"] != 0.8 || usable["vip"] == "" {
		t.Fatalf("parse: %v %v", err, models)
	}
	s := Snapshot{Models: models, GroupRatio: ratio, UsableGroups: usable}
	found := s.FindModels("4o", 5)
	if len(found) == 0 || found[0].ModelName != "gpt-4o" {
		t.Fatalf("find 4o: %+v", found)
	}
	q := s.Quote(found[0], "vip")
	if *q.InputPerMillion != 2 || *q.OutputPerMillion != 8 {
		t.Fatalf("quote: %+v", q)
	}
	if got := s.FindModels("克劳德 sonnet 4.5", 3); len(got) == 0 || !strings.HasPrefix(got[0].ModelName, "claude-sonnet-4-5") {
		t.Fatalf("alias match: %+v", got)
	}
	call := s.Quote(s.FindModels("mj_imagine", 1)[0], "")
	if call.Billing != "per_call" || *call.PerCall != 0.1 {
		t.Fatalf("per call: %+v", call)
	}
	if mini := s.Quote(models[1], "vip"); mini.Note == "" {
		t.Fatal("expected unavailable-group note")
	}
}

func TestFetchToleratesPartialFailure(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/notice":
			w.Write([]byte(`{"success":true,"data":"# 维护通知\n今晚 23 点维护"}`))
		case "/api/status":
			w.Write([]byte(`{"success":true,"data":{"system_name":"Cooper","announcements_enabled":true,"announcements":[{"content":"旧公告","publishDate":"2026-01-01T00:00:00Z"},{"content":"新公告：上线 xx 模型","publishDate":"2026-11-01T00:00:00Z","type":"success"}]}}`))
		case "/api/uptime/status":
			w.Write([]byte(`{"success":true,"data":[{"categoryName":"分组","monitors":[{"name":"default","status":1,"uptime":0.99},{"name":"vip","status":0,"uptime":0.8}]}]}`))
		case "/api/pricing":
			w.Write([]byte(pricingJSON))
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()
	s, err := NewClient(srv.URL, "", "").Fetch(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if s.SystemName != "Cooper" || s.Announcements[0].Content != "新公告：上线 xx 模型" || len(s.Monitors) != 2 || len(s.Models) != 4 {
		t.Fatalf("snapshot: %+v", s)
	}
	summary := s.PromptSummary(false)
	for _, want := range []string{"新公告", "vip：异常", "get_model_price"} {
		if !strings.Contains(summary, want) {
			t.Fatalf("summary missing %q: %s", want, summary)
		}
	}
	srv2 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/pricing" {
			w.Write([]byte(pricingJSON))
			return
		}
		http.Error(w, "x", 500)
	}))
	defer srv2.Close()
	s2, err := NewClient(srv2.URL, "", "").Fetch(context.Background())
	if err != nil || len(s2.Models) != 4 || s2.Errors["notice"] == "" {
		t.Fatalf("partial: %v %+v", err, s2.Errors)
	}
}
