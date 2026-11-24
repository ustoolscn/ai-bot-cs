package app

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"os"
	"slices"
	"strings"
	"testing"
	"time"

	"ai-bot/backend/internal/agent"
	"ai-bot/backend/internal/config"
	"ai-bot/backend/internal/openai"
)

func TestTypeSafeReplyDecision(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "POST" || r.URL.Path != "/v1/systemone" || r.Header.Get("Authorization") != "Bearer test-key" || r.Header.Get("Content-Type") != "application/json" {
			t.Errorf("incorrect TypeSafe request to %s %s", r.Method, r.URL.Path)
		}
		var body struct {
			Model     string
			State     map[string]string
			Questions map[string]struct{ Type, Instructions string }
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Error(err)
			return
		}
		if body.Model != "jev-latest" || body.State["当前消息"] != "你好" || len(body.Questions) != 3 {
			t.Errorf("unexpected request: %+v", body)
		}
		for _, field := range []string{"reply", "socialOnly", "needsWebSearch"} {
			q := body.Questions[field]
			if q.Type != "noul" || q.Instructions == "" {
				t.Errorf("invalid question %s: %+v", field, q)
			}
		}
		io.WriteString(w, `{"answers":{"reply":{"type":"noul","noul":0.5},"socialOnly":{"type":"noul","noul":1},"needsWebSearch":{"type":"noul","noul":0.499}},"usage":{"input_tokens":42,"output_tokens":3}}`)
	}))
	defer server.Close()
	cl := openai.New(server.URL+"/v1", "test-key", "jev-latest", time.Second)
	res, err := cl.TypeSafeDecision(context.Background(), json.RawMessage(`{"当前消息":"你好"}`), replyQuestions)
	if err != nil || res.Content != `{"needsWebSearch":false,"reply":true,"socialOnly":true}` || res.InputTokens != 42 || res.OutputTokens != 3 {
		t.Fatalf("unexpected decision: %+v, %v", res, err)
	}
}

func TestTypeSafeInvalidDecision(t *testing.T) {
	for _, reply := range []string{`null`, `{"type":"noul"}`, `{"type":"choice","noul":0.8}`, `{"type":"noul","noul":-0.1}`, `{"type":"noul","noul":1.01}`} {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			io.WriteString(w, `{"answers":{"reply":`+reply+`}}`)
		}))
		cl := openai.New(server.URL, "key", "jev-latest", time.Second)
		_, err := cl.TypeSafeDecision(context.Background(), "text", map[string]string{"reply": "需要回复吗？"})
		server.Close()
		if err == nil {
			t.Errorf("accepted invalid answer %s", reply)
		}
	}
	for _, status := range []int{403, 429, 529} {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(status) }))
		cl := openai.New(server.URL, "key", "jev-latest", time.Second)
		_, err := cl.TypeSafeDecision(context.Background(), "text", replyQuestions)
		server.Close()
		if !openai.IsHTTPStatus(err, status) {
			t.Errorf("lost status %d: %v", status, err)
		}
	}
}

func TestTypeSafeModelDiscovery(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "GET" || r.URL.Path != "/v1/models" || r.Header.Get("Authorization") != "Bearer key" {
			t.Error("invalid discovery request")
		}
		io.WriteString(w, `{"models":[{"name":"jev-latest"},{"name":"jev-preview"},{"name":"jev-latest"}]}`)
	}))
	defer server.Close()
	models, err := fetchModelIDs(context.Background(), server.Client(), server.URL+"/v1", "key", "typesafe")
	if err != nil || !slices.Equal(models, []string{"jev-latest", "jev-preview"}) {
		t.Fatalf("models %v, error %v", models, err)
	}
}

func TestTypeSafeInboxRouting(t *testing.T) {
	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("TEST_DATABASE_URL not configured")
	}
	ctx := context.Background()
	pool := newIntegrationSchema(t, ctx, dsn)
	a, err := New(ctx, config.Config{DataDir: t.TempDir(), MasterKey: []byte("0123456789abcdef0123456789abcdef"), AdminUsername: "admin", AdminPassword: "test-password", AIRequestTimeout: 10 * time.Second}, pool, slog.Default())
	check := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}
	check(err)
	enc, err := a.cipher.Encrypt("test-key")
	check(err)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body struct{ State map[string]any }
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Error(err)
			return
		}
		switch body.State["当前消息"] {
		case "天气":
			io.WriteString(w, `{"answers":{"reply":{"type":"noul","noul":1},"socialOnly":{"type":"noul","noul":0},"needsWebSearch":{"type":"noul","noul":1}}}`)
		case "审核":
			w.WriteHeader(403)
		case "限流":
			w.WriteHeader(429)
		default:
			io.WriteString(w, `{"answers":{"reply":{"type":"noul","noul":0},"socialOnly":{"type":"noul","noul":1},"needsWebSearch":{"type":"noul","noul":0}}}`)
		}
	}))
	defer server.Close()
	var pid, bot, service string
	admin := httptest.NewServer(a.Router())
	defer admin.Close()
	jar, _ := cookiejar.New(nil)
	client := &http.Client{Jar: jar}
	requestJSON(t, client, "POST", admin.URL+"/api/auth/login", map[string]string{"username": "admin", "password": "test-password"}, 200)
	profiles := admin.URL + "/api/model-profiles"
	pid = createdID(t, requestJSON(t, client, "POST", profiles, map[string]string{"provider": "typesafe", "apiKey": "test-key"}, 200))
	requestJSON(t, client, "PUT", profiles+"/"+pid, map[string]string{"provider": "compatible", "kind": "chat", "name": "different endpoint", "baseUrl": server.URL, "model": "chat"}, 400)
	// Redirect the saved profile to the local upstream after exercising its official preset.
	_, err = pool.Exec(ctx, `UPDATE model_profiles SET base_url=$2 WHERE id=$1 AND base_url='https://api.typesafe.ai/v1'`, pid, server.URL)
	check(err)
	preview := dataMap(t, requestJSON(t, client, "POST", profiles+"/"+pid+"/test", map[string]string{"input": "群友闲聊"}, 200))
	if preview["kind"] != "decision" || preview["decision"].(map[string]any)["reply"] != false {
		t.Fatalf("preview: %v", preview)
	}
	check(pool.QueryRow(ctx, `INSERT INTO bots(name,app_id,app_secret_enc) VALUES('小吉','app',$1) RETURNING id::text`, enc).Scan(&bot))
	services := admin.URL + "/api/bots/" + bot + "/services"
	requestJSON(t, client, "POST", services, map[string]string{"name": "无效", "chatProfileId": pid}, 400)
	service = createdID(t, requestJSON(t, client, "POST", services, map[string]string{"name": "小吉", "decisionProfileId": pid}, 200))

	for _, tc := range []struct {
		mode, msg, status, detail string
		excluded                  bool
	}{
		{"smart", "群友闲聊", "skipped", "", false},
		{"always", "另一条闲聊", "skipped", "", false},
		{"smart", "小吉在吗", "pending", "no chat model configured", false}, // Name override proceeds to generation.
		{"always", "天气", "pending", "no chat model configured", false},
		{"smart", "限流", "pending", "429", false},
		{"always", "审核", "failed", "403", true},
	} {
		var cid, mid, status, detail string
		var excluded bool
		check(pool.QueryRow(ctx, `INSERT INTO conversations(channel,bot_id,platform_id,type,trigger_mode,service_id) VALUES('qq',$1,$2,'group',$3,$4) RETURNING id::text`, bot, tc.msg, tc.mode, service).Scan(&cid))
		check(pool.QueryRow(ctx, `INSERT INTO messages(channel,bot_id,conversation_id,direction,event_type,sender_id,platform_message_id,content,status) VALUES('qq',$1,$2,'inbound','GROUP_MESSAGE_CREATE','member',$3,$3,'received') RETURNING id::text`, bot, cid, tc.msg).Scan(&mid))
		_, err = pool.Exec(ctx, `INSERT INTO inbox_tasks(message_id) VALUES($1)`, mid)
		check(err)
		check(a.processInbox(ctx))
		check(pool.QueryRow(ctx, `SELECT t.status,coalesce(t.last_error,''),m.context_excluded FROM inbox_tasks t JOIN messages m ON m.id=t.message_id WHERE m.id=$1`, mid).Scan(&status, &detail, &excluded))
		if status != tc.status || !strings.Contains(detail, tc.detail) || excluded != tc.excluded {
			t.Errorf("%s: status=%s detail=%s excluded=%t", tc.msg, status, detail, excluded)
		}
		// Isolate cases even when a slower test machine reaches the retry interval.
		_, err = pool.Exec(ctx, `UPDATE inbox_tasks SET next_attempt_at='infinity' WHERE message_id=$1`, mid)
		check(err)
	}
	chatID := createdID(t, requestJSON(t, client, "POST", profiles, map[string]string{"kind": "chat", "name": "辅助对话", "baseUrl": server.URL, "apiKey": "test-key", "model": "chat"}, 200))
	settings := agent.Settings{DecisionProfileID: pid, AuxChatProfileID: chatID}
	for _, task := range []string{"reply_decision", "group_moderation"} {
		_, chosen, err := a.auxJSONModel(ctx, settings, "", task, task == "reply_decision")
		if err != nil || chosen != chatID {
			t.Fatalf("%s needs auxiliary chat, got %s: %v", task, chosen, err)
		}
	}
}
