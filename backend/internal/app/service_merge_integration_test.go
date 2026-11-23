package app

import (
	"ai-bot/backend/internal/storage"
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"ai-bot/backend/internal/agent"
	"ai-bot/backend/internal/config"
	"ai-bot/backend/internal/db"
	"ai-bot/backend/internal/domain"
	"ai-bot/backend/internal/newapi"
)

func TestServiceMergeAndMemory(t *testing.T) {
	url := os.Getenv("TEST_DATABASE_URL")
	if url == "" {
		t.Skip("TEST_DATABASE_URL not set")
	}
	ctx := context.Background()
	pool := newIntegrationSchema(t, ctx, url)
	check := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}
	cfg := config.Config{DataDir: t.TempDir(), MasterKey: []byte("0123456789abcdef0123456789abcdef"), AdminUsername: "admin", AdminPassword: "test-password", PublicBaseURL: "https://seed.example"}
	a, err := New(ctx, cfg, pool, slog.New(slog.NewTextHandler(io.Discard, nil)))
	check(err)
	server := httptest.NewServer(a.Router())
	defer server.Close()
	jar, _ := cookiejar.New(nil)
	client := &http.Client{Jar: jar}
	req := func(method, path string, in any, status int) []byte {
		t.Helper()
		return requestJSON(t, client, method, server.URL+"/api/"+path, in, status)
	}
	req("POST", "auth/login", map[string]string{"username": "admin", "password": "test-password"}, 200)
	bot := createdID(t, req("POST", "bots", map[string]any{"name": "robot", "appId": "qq", "appSecret": "secret", "enabled": true}, 201))
	services := "bots/" + bot + "/services"
	req("PUT", "agent/settings", map[string]any{"liveData": map[string]any{"baseUrl": "https://old.example"}, "liveDataAccessToken": "old-token", "stylePrompt": "global style"}, 200)
	// Reconstruct the pre-023 service schema with a bound online group.
	_, err = pool.Exec(ctx, "ALTER TABLE bot_services DROP COLUMN settings,DROP COLUMN instructions,ALTER COLUMN agent_id SET NOT NULL")
	check(err)
	var legacy, cid, mid string
	check(pool.QueryRow(ctx, `WITH agent AS (INSERT INTO support_agents(name,instructions,personality_id) SELECT 'template','old role',id FROM personality_presets LIMIT 1 RETURNING id) INSERT INTO bot_services(bot_id,name,agent_id) SELECT $1,'old service',id FROM agent RETURNING id::text`, bot).Scan(&legacy))
	check(pool.QueryRow(ctx, `INSERT INTO conversations(channel,bot_id,service_id,platform_id,type) VALUES('qq',$1,$2,'group','group') RETURNING id::text`, bot, legacy).Scan(&cid))
	check(db.Migrate(ctx, pool))
	s, role, _, err := a.conversationAgent(ctx, cid, a.agentSettings(ctx))
	check(err)
	if role != "old role" || !strings.Contains(s.StylePrompt, "global style") || s.LiveData.AccessTokenEnc == "" {
		t.Fatal("legacy configuration lost")
	}
	check(pool.QueryRow(ctx, `INSERT INTO messages(channel,bot_id,conversation_id,direction,content) VALUES('qq',$1,$2,'inbound','hello') RETURNING id::text`, bot, cid).Scan(&mid))
	newID := createdID(t, req("POST", services, map[string]any{"name": "independent", "instructions": "new role", "settings": map[string]any{"stylePrompt": "short", "toolNames": []string{}, "liveData": map[string]any{"enabled": true, "baseUrl": "https://new.example"}}}, 200))
	_, err = pool.Exec(ctx, "UPDATE conversations SET service_id=$2 WHERE id=$1", cid, newID)
	check(err)
	effective := func() agent.Settings {
		t.Helper()
		s, role, _, err := a.conversationAgent(ctx, cid, a.agentSettings(ctx))
		check(err)
		if role != "new role" {
			t.Fatal(role)
		}
		return s
	}
	s = effective()
	if s.LiveData.AccessTokenEnc != "" || s.StylePrompt != "short" || toolSelected(&turnRuntime{settings: s}, "publish_html_page") {
		t.Fatal("service isolation failed")
	}
	req("PUT", services+"/"+newID, map[string]any{"name": "independent", "liveDataAccessToken": "new-token"}, 200)
	check(db.Migrate(ctx, pool))
	s = effective()
	token, err := a.cipher.Decrypt(s.LiveData.AccessTokenEnc)
	check(err)
	if token != "new-token" {
		t.Fatal("lost saved token")
	}
	raw := string(req("GET", services, nil, 200))
	if strings.Contains(raw, "accessTokenEnc") || strings.Contains(raw, "new-token") {
		t.Fatal("service exposed credential")
	}
	req("PUT", "system/settings", map[string]string{"publicBaseUrl": "https://public.example"}, 200)
	if !strings.Contains(string(req("GET", "bots", nil, 200)), "https://public.example/callbacks/qq/"+bot) {
		t.Fatal("callback base")
	}
	result, err := a.executeTool(ctx, &turnRuntime{settings: agent.DefaultSettings()}, domain.ToolCall{Name: "publish_html_page", Arguments: `{"html":"<p>hello</p><script>unsafe()</script>"}`}, nil)
	check(err)
	var published struct{ URL string }
	check(json.Unmarshal([]byte(result), &published))
	resp, err := client.Get(server.URL + strings.TrimPrefix(published.URL, "https://public.example"))
	check(err)
	body, err := io.ReadAll(resp.Body)
	resp.Body.Close()
	check(err)
	if resp.StatusCode != 200 || strings.Contains(string(body), "unsafe()") || !strings.Contains(string(body), "hello") {
		t.Fatal("published page")
	}
	profile := "conversations/" + cid + "/user-profiles/alice"
	req("PUT", profile, map[string]string{"content": "喜欢简短答复"}, 200)
	req("PUT", profile, map[string]string{"content": "手机号 13800138000"}, 400)
	var ev evaluation
	check(json.Unmarshal([]byte(`{"profile":"使用中文，喜欢简短答复","memories":[{"action":"add","content":"已迁移项目","confidence":1,"eventAt":"2025-03-01T00:00:00Z"}]}`), &ev))
	check(a.applyExtractedMemory(ctx, ev, "喜欢简短答复", "[]", cid, "alice", "Alice", bot, mid, time.Now(), s))
	var id string
	var day time.Time
	check(pool.QueryRow(ctx, "SELECT id::text,event_at FROM user_memories WHERE conversation_id=$1", cid).Scan(&id, &day))
	if day.Year() != 2025 {
		t.Fatal(day)
	}
	known := jsonString([]map[string]string{{"id": id, "content": "已迁移项目"}})
	ev.Profile = nil
	ev.Memories[0].ID = id
	ev.Memories[0].Action = "update"
	ev.Memories[0].Content = "完成迁移"
	check(a.applyExtractedMemory(ctx, ev, "", known, cid, "alice", "Alice", bot, mid, time.Now(), s))
	if !strings.Contains(string(req("GET", "memories?conversationId="+cid, nil, 200)), "完成迁移") {
		t.Fatal("event update failed")
	}
	ev.Memories[0].Action = "delete"
	check(a.applyExtractedMemory(ctx, ev, "", strings.Replace(known, "已迁移项目", "完成迁移", 1), cid, "alice", "Alice", bot, mid, time.Now(), s))
	if !strings.Contains(string(req("GET", "memories/directory", nil, 200)), "alice") {
		t.Fatal("missing profile-only user")
	}
	memories := a.loadMemories(ctx, s, bot, cid, "alice", "")
	if len(memories) != 1 || memories[0].Kind != "profile" {
		t.Fatal(memories)
	}
	req("DELETE", profile, nil, 200)
	_, err = pool.Exec(ctx, "INSERT INTO inbox_tasks(message_id,status,locked_at) VALUES($1,'processing',now())", mid)
	check(err)
	finish := a.startStage(ctx, mid, "测试环节")
	finish("", nil)
	overview := dataMap(t, req("GET", "system/overview", nil, 200))
	if len(overview["activeTasks"].([]any)) != 1 || len(overview["stageAverages"].([]any)) == 0 {
		t.Fatal(overview)
	}
}

// Each model call rejects the same content. Verify the failed item and its
// cached summary cannot poison the next turn, including post-reply extraction.
func TestAuxiliaryModerationRecovery(t *testing.T) {
	url := os.Getenv("TEST_DATABASE_URL")
	if url == "" {
		t.Skip("TEST_DATABASE_URL not set")
	}
	ctx := context.Background()
	pool := newIntegrationSchema(t, ctx, url)
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { http.Error(w, "moderated", 403) }))
	defer upstream.Close()
	a, err := New(ctx, config.Config{DataDir: t.TempDir(), MasterKey: []byte("0123456789abcdef0123456789abcdef"), AdminUsername: "admin", AdminPassword: "test-password"}, pool, slog.Default())
	if err != nil {
		t.Fatal(err)
	}
	secret, _ := a.cipher.Encrypt("test")
	var bot, pid string
	if err = pool.QueryRow(ctx, `INSERT INTO model_profiles(name,kind,base_url,api_key_enc,model,enabled,is_default) VALUES('test','chat',$1,$2,'test',true,true) RETURNING id::text`, upstream.URL, secret).Scan(&pid); err != nil {
		t.Fatal(err)
	}
	if err = pool.QueryRow(ctx, "INSERT INTO bots(name,app_id,app_secret_enc) VALUES('test','test',$1) RETURNING id::text", secret).Scan(&bot); err != nil {
		t.Fatal(err)
	}
	for _, stage := range []string{"decision", "rewrite", "summarize", "evaluate", "group_review"} {
		t.Run(stage, func(t *testing.T) {
			check := func(err error) {
				t.Helper()
				if err != nil {
					t.Fatal(err)
				}
			}
			var cid, mid, run string
			mode := "mention_only"
			if stage == "decision" {
				mode = "smart"
			}
			check(pool.QueryRow(ctx, `INSERT INTO conversations(channel,bot_id,platform_id,type,trigger_mode) VALUES('qq',$1,$2,'group',$3) RETURNING id::text`, bot, stage, mode).Scan(&cid))
			check(pool.QueryRow(ctx, `INSERT INTO messages(channel,bot_id,conversation_id,platform_message_id,direction,sender_id,content,event_at) VALUES('qq',$1,$2,'m'||$3,'inbound','user','sample',now()) RETURNING id::text`, bot, cid, stage).Scan(&mid))
			_, err := pool.Exec(ctx, `INSERT INTO conversation_summaries(conversation_id,scope_key,summary,covered_until) VALUES($1,'','old flagged summary',now()-interval '1 day')`, cid)
			check(err)
			switch stage {
			case "rewrite", "decision":
				if stage == "rewrite" {
					_, err = pool.Exec(ctx, `INSERT INTO messages(channel,bot_id,conversation_id,direction,content,event_at) VALUES('qq',$1,$2,'inbound','earlier question',now()-interval '1 minute')`, bot, cid)
					check(err)
				}
				_, err = pool.Exec(ctx, "INSERT INTO inbox_tasks(message_id) VALUES($1)", mid)
				check(err)
				check(a.processInbox(ctx))
			case "summarize":
				check(a.jobSummarize(ctx, cid, "", time.Now().Add(time.Hour).Format(time.RFC3339Nano), pid))
			case "group_review", "evaluate":
				if stage == "evaluate" {
					check(pool.QueryRow(ctx, "INSERT INTO agent_runs(message_id) VALUES($1) RETURNING id::text", mid).Scan(&run))
					_, err = pool.Exec(ctx, `INSERT INTO messages(channel,bot_id,conversation_id,event_type,direction,reply_to_message_id,content) VALUES('qq',$1,$2,'AI_RESPONSE','outbound','m'||$3,'reply')`, bot, cid, stage)
					check(err)
				} else {
					_, err = pool.Exec(ctx, `UPDATE conversations SET guard_policy='{"enabled":true,"intent":"test"}' WHERE id=$1`, cid)
					check(err)
					_, err = pool.Exec(ctx, "INSERT INTO group_reviews(message_id,queue_reply) VALUES($1,true)", mid)
					check(err)
				}
				a.enqueueJob(ctx, stage, map[string]any{"messageId": mid, "runId": run, "profileId": pid}, "")
				job, err := a.claimAgentJob(ctx, []string{stage}, false)
				check(err)
				check(a.runJob(ctx, job))
			}
			var excluded bool
			var summaries, calls int
			check(pool.QueryRow(ctx, `SELECT context_excluded,(SELECT count(*) FROM conversation_summaries WHERE conversation_id=$2) FROM messages WHERE id=$1`, mid, cid).Scan(&excluded, &summaries))
			kind := map[string]string{"decision": "reply_decision", "rewrite": "rewrite", "summarize": "summarize", "evaluate": "evaluate", "group_review": "group_moderation"}[stage]
			check(pool.QueryRow(ctx, "SELECT count(*) FROM model_calls WHERE kind=$1 AND error IS NOT NULL", kind).Scan(&calls))
			if !excluded || summaries != 0 || calls != 1 {
				t.Fatalf("excluded=%v summaries=%d calls=%d", excluded, summaries, calls)
			}
		})
	}
}

func TestStatusPageScreenshot(t *testing.T) {
	if browserPath() == "" {
		t.Skip("Chromium not configured")
	}
	ctx := context.Background()
	if c, err := publicDial(ctx, "tcp", "127.0.0.1:80"); err == nil {
		c.Close()
		t.Fatal("accepted loopback")
	}
	fixture := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		io.WriteString(w, "<body><h1>Group status</h1>All services available</body>")
	}))
	defer fixture.Close()
	images, err := storage.NewLocal(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	a := App{images: images}
	// The fixture stands in for the admin's as-yet unknown status site.
	dial := func(ctx context.Context, network, addr string) (net.Conn, error) {
		if addr != "status.example:80" {
			return nil, errors.New("unexpected host")
		}
		var d net.Dialer
		return d.DialContext(ctx, network, fixture.Listener.Addr().String())
	}
	part, err := a.renderStatusPage(ctx, "http://status.example/", dial)
	if err != nil {
		t.Fatal(err)
	}
	data, err := images.Read(ctx, part.StorageKey)
	if err != nil || len(data) < 100 || string(data[1:4]) != "PNG" {
		t.Fatal("missing status PNG", err)
	}
}

func TestCustomerPriceCurrency(t *testing.T) {
	snap := newapi.Snapshot{GroupRatio: map[string]float64{"vip": 1.5}, Models: []newapi.ModelPrice{{ModelName: "example", BillingMode: "per_call", ModelPrice: 2}}}
	call := domain.ToolCall{Name: "get_model_price", Arguments: `{"model":"example","group":"vip"}`}
	for _, tc := range []struct {
		question, currency string
		price              float64
	}{{"What is the price?", "USD", 3}, {"每次多少钱？", "CNY", 21}} {
		rt := turnRuntime{settings: agent.DefaultSettings(), snapshotOK: true, snapshot: snap, question: tc.question}
		raw, err := (&App{}).executeTool(context.Background(), &rt, call, nil)
		if err != nil {
			t.Fatal(err)
		}
		var out struct {
			DisplayCurrency string
			DisplayQuotes   []struct {
				Group   string
				PerCall float64
			}
		}
		if json.Unmarshal([]byte(raw), &out) != nil || out.DisplayCurrency != tc.currency || len(out.DisplayQuotes) != 1 || out.DisplayQuotes[0].Group != "vip" || out.DisplayQuotes[0].PerCall != tc.price {
			t.Fatal(raw)
		}
	}
}
