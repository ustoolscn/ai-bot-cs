package app

import (
	"ai-bot/backend/internal/db"
	"ai-bot/backend/internal/domain"
	"bytes"
	"context"
	"encoding/json"
	"image"
	"image/png"
	"io"
	"log/slog"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"ai-bot/backend/internal/config"
	"ai-bot/backend/internal/qq"
)

func TestRuntimeSettingsDynamicBehavior(t *testing.T) {
	databaseURL := os.Getenv("TEST_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("TEST_DATABASE_URL is not configured")
	}
	ctx := context.Background()
	pool := newIntegrationSchema(t, ctx, databaseURL)
	cfg := config.Config{DataDir: t.TempDir(), MasterKey: []byte("0123456789abcdef0123456789abcdef"), AdminUsername: "settings-admin", AdminPassword: "settings-password", DefaultContextLimit: 33, AIRequestTimeout: 77 * time.Second, MessageRetentionDays: 123, WorkerPoll: time.Second}
	a, err := New(ctx, cfg, pool, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(a.Router())
	defer server.Close()
	jar, _ := cookiejar.New(nil)
	client := &http.Client{Jar: jar}
	requestJSON(t, client, http.MethodPost, server.URL+"/api/auth/login", map[string]string{"username": "settings-admin", "password": "settings-password"}, http.StatusOK)
	seed := dataMap(t, requestJSON(t, client, http.MethodGet, server.URL+"/api/system/settings", nil, http.StatusOK))
	if seed["defaultContextLimit"] != float64(33) || seed["aiRequestTimeoutSeconds"] != float64(77) || seed["messageRetentionDays"] != float64(123) {
		t.Fatalf("seed=%v", seed)
	}
	updated := dataMap(t, requestJSON(t, client, http.MethodPut, server.URL+"/api/system/settings", map[string]int{"defaultContextLimit": 44, "aiRequestTimeoutSeconds": 66, "messageRetentionDays": 7}, http.StatusOK))
	if updated["defaultContextLimit"] != float64(44) {
		t.Fatalf("updated=%v", updated)
	}
	var audits int
	if err := pool.QueryRow(ctx, "SELECT count(*) FROM audit_logs WHERE action='system.settings.update' AND target_id=$1", runtimeSettingsKey).Scan(&audits); err != nil || audits != 1 {
		t.Fatalf("audits=%d err=%v", audits, err)
	}

	otherCfg := cfg
	otherCfg.DefaultContextLimit = 99
	otherCfg.AIRequestTimeout = 500 * time.Second
	otherCfg.MessageRetentionDays = 1000
	otherCfg.DataDir = t.TempDir()
	restarted, err := New(ctx, otherCfg, pool, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatal(err)
	}
	persisted, err := restarted.runtimeSettings(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if persisted.DefaultContextLimit != 44 || persisted.AIRequestTimeoutSeconds != 66 || persisted.MessageRetentionDays != 7 {
		t.Fatalf("restart overwrote settings: %+v", persisted)
	}

	apiKey, _ := restarted.cipher.Encrypt("key")
	var profileID string
	if err = pool.QueryRow(ctx, `INSERT INTO model_profiles(name,kind,base_url,api_key_enc,model) VALUES('runtime-chat','chat','http://127.0.0.1',$1,'chat') RETURNING id::text`, apiKey).Scan(&profileID); err != nil {
		t.Fatal(err)
	}
	model, _, _, _, err := restarted.loadModelDetails(ctx, profileID)
	if err != nil {
		t.Fatal(err)
	}
	if model.HTTP.Timeout != 66*time.Second {
		t.Fatalf("dynamic timeout=%s", model.HTTP.Timeout)
	}

	botSecret, _ := restarted.cipher.Encrypt("secret")
	var botID string
	if err = pool.QueryRow(ctx, `INSERT INTO bots(name,app_id,app_secret_enc) VALUES('settings-bot','app',$1) RETURNING id::text`, botSecret).Scan(&botID); err != nil {
		t.Fatal(err)
	}
	raw := []byte(`{"id":"settings-event","op":0,"t":"GROUP_AT_MESSAGE_CREATE","d":{"id":"settings-message","group_openid":"settings-group","content":"hello","author":{"member_openid":"user"}}}`)
	env, msg, err := qq.Parse(raw, botID)
	if err != nil {
		t.Fatal(err)
	}
	if err = restarted.persistInbound(ctx, botID, env, msg, raw); err != nil {
		t.Fatal(err)
	}
	var conversationID string
	var contextLimit int
	if err = pool.QueryRow(ctx, "SELECT id::text,context_limit FROM conversations WHERE bot_id=$1 AND platform_id='settings-group'", botID).Scan(&conversationID, &contextLimit); err != nil {
		t.Fatal(err)
	}
	if contextLimit != 44 {
		t.Fatalf("new conversation context_limit=%d", contextLimit)
	}

	old := time.Now().AddDate(0, 0, -8)
	if _, err = pool.Exec(ctx, `INSERT INTO messages(channel,bot_id,conversation_id,direction,content,created_at) VALUES('qq',$1,$2,'inbound','old',$3)`, botID, conversationID, old); err != nil {
		t.Fatal(err)
	}
	if _, err = pool.Exec(ctx, `INSERT INTO webhook_events(channel,bot_id,platform_event_id,event_type,raw_event,created_at) VALUES('qq',$1,'old-event','TEST','{}'::jsonb,$2)`, botID, old); err != nil {
		t.Fatal(err)
	}
	if _, err = pool.Exec(ctx, `INSERT INTO admin_sessions(user_id,token_hash,expires_at) SELECT id,$1,now()-interval '1 day' FROM admin_users LIMIT 1`, []byte("expired-session")); err != nil {
		t.Fatal(err)
	}
	if err = restarted.processMaintenance(ctx); err != nil {
		t.Fatal(err)
	}
	var oldMessages, oldEvents, newMessages, newEvents, expiredSessions int
	_ = pool.QueryRow(ctx, "SELECT count(*) FROM messages WHERE content='old'").Scan(&oldMessages)
	_ = pool.QueryRow(ctx, "SELECT count(*) FROM webhook_events WHERE platform_event_id='old-event'").Scan(&oldEvents)
	_ = pool.QueryRow(ctx, "SELECT count(*) FROM messages WHERE platform_message_id='settings-message'").Scan(&newMessages)
	_ = pool.QueryRow(ctx, "SELECT count(*) FROM webhook_events WHERE platform_event_id='settings-event'").Scan(&newEvents)
	_ = pool.QueryRow(ctx, "SELECT count(*) FROM admin_sessions WHERE expires_at<now()").Scan(&expiredSessions)
	if oldMessages != 0 || oldEvents != 0 || newMessages != 1 || newEvents != 1 || expiredSessions != 0 {
		t.Fatalf("retention oldMessages=%d oldEvents=%d newMessages=%d newEvents=%d expiredSessions=%d", oldMessages, oldEvents, newMessages, newEvents, expiredSessions)
	}
}

func TestImageGenerationConfigurationAndTool(t *testing.T) {
	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("TEST_DATABASE_URL not configured")
	}
	ctx := context.Background()
	pool := newIntegrationSchema(t, ctx, dsn)
	a, err := New(ctx, config.Config{DataDir: t.TempDir(), MasterKey: []byte("0123456789abcdef0123456789abcdef"), AdminUsername: "admin", AdminPassword: "test-password"}, pool, slog.Default())
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(a.Router())
	defer server.Close()
	jar, _ := cookiejar.New(nil)
	client := &http.Client{Jar: jar}
	requestJSON(t, client, "POST", server.URL+"/api/auth/login", map[string]string{"username": "admin", "password": "test-password"}, 200)
	var generated bytes.Buffer
	if err := png.Encode(&generated, image.NewRGBA(image.Rect(0, 0, 2, 3))); err != nil {
		t.Fatal(err)
	}
	calls := 0
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		var body struct {
			Model, Prompt string
			N             int
		}
		if r.Method != "POST" || r.URL.Path != "/v1/images/generations" || r.Header.Get("Authorization") != "Bearer image-secret" || json.NewDecoder(r.Body).Decode(&body) != nil || body.Model != "gpt-image-2.6-flare" || body.Prompt != "蓝色的山" || body.N != 1 {
			t.Error("unexpected generation request")
		}
		json.NewEncoder(w).Encode(map[string]any{"data": []any{map[string]any{"b64_json": generated.Bytes()}}})
	}))
	defer provider.Close()
	api := server.URL + "/api"
	var bot, cid string
	if err := pool.QueryRow(ctx, "INSERT INTO bots(name,app_id,app_secret_enc) VALUES('bot','image-test','enc') RETURNING id::text").Scan(&bot); err != nil {
		t.Fatal(err)
	}
	// A legacy encrypted key must survive startup migration, without selecting
	// the migrated model for any individual service.
	enc, _ := a.cipher.Encrypt("image-secret")
	_, err = pool.Exec(ctx, `INSERT INTO system_settings(key,value) VALUES('agent_settings',jsonb_build_object('imageGeneration',jsonb_build_object('model','gpt-image-2.6-flare','baseUrl',$1::text,'apiKeyEnc',$2::text))) ON CONFLICT(key) DO UPDATE SET value=excluded.value`, provider.URL+"/v1", enc)
	if err != nil {
		t.Fatal(err)
	}
	for range 2 {
		if err = db.Migrate(ctx, pool); err != nil {
			t.Fatal(err)
		}
	}
	var profile string
	if err = pool.QueryRow(ctx, "SELECT id::text FROM model_profiles WHERE kind='image' AND api_key_enc=$1", enc).Scan(&profile); err != nil {
		t.Fatal(err)
	}
	raw := requestJSON(t, client, "GET", api+"/agent/settings", nil, 200)
	if bytes.Contains(raw, []byte("imageGeneration")) {
		t.Fatal("obsolete settings exposed")
	}
	bots := api + "/bots/" + bot + "/services"
	svc := createdID(t, requestJSON(t, client, "POST", bots, map[string]string{"name": "image-service"}, 200))
	if err := pool.QueryRow(ctx, "INSERT INTO conversations(channel,bot_id,type,platform_id,service_id) VALUES('qq',$1,'private','user',$2) RETURNING id::text", bot, svc).Scan(&cid); err != nil {
		t.Fatal(err)
	}
	settings, _, _, err := a.conversationAgent(ctx, cid, a.agentSettings(ctx))
	if err != nil || settings.ImageProfileID != "" {
		t.Fatal("implicit image model", err)
	}
	proxy := createdID(t, requestJSON(t, client, "POST", api+"/system/proxies", map[string]string{"name": "image-test", "address": provider.URL}, 200))
	profileBody := map[string]any{"name": "image-test", "kind": "image", "model": "gpt-image-2.6-flare", "baseUrl": "http://images.invalid/v1", "apiKey": "image-secret", "proxyId": proxy, "pricePerImage": 0.25}
	viaProxy := createdID(t, requestJSON(t, client, "POST", api+"/model-profiles", profileBody, 200))
	delete(profileBody, "apiKey")
	requestJSON(t, client, "PUT", api+"/model-profiles/"+viaProxy, profileBody, 200)
	requestJSON(t, client, "PUT", bots+"/"+svc, map[string]string{"name": "image-service", "imageProfileId": profile}, 200)
	settings, _, _, err = a.conversationAgent(ctx, cid, a.agentSettings(ctx))
	if err != nil || settings.ImageProfileID != profile {
		t.Fatal("selected profile missing", err)
	}
	rt := turnRuntime{conversationID: cid, senderID: "user", settings: settings, dryRun: true}
	call := domain.ToolCall{Name: "generate_image", Arguments: `{"prompt":"蓝色的山"}`}
	if result, err := a.executeTool(ctx, &rt, call, nil); err != nil || result != `{"preview":true}` {
		t.Fatal(result, err)
	}
	rt.dryRun = false
	if result, err := a.executeTool(ctx, &rt, call, nil); err != nil || result != `{"ready":true}` || rt.screenshot == nil {
		t.Fatal(result, err)
	}
	data, err := a.images.Read(ctx, rt.screenshot.StorageKey)
	if err != nil || !bytes.Equal(data, generated.Bytes()) || calls != 1 {
		t.Fatal("generated image was not saved once", err)
	}
	requestJSON(t, client, "PUT", api+"/system/settings", map[string]int{"imageDailyLimitDefault": 1}, 200)
	if _, err := a.executeTool(ctx, &rt, call, nil); err == nil || !strings.Contains(err.Error(), "次数已用完") || calls != 1 {
		t.Fatalf("quota: %v calls=%d", err, calls)
	}
	rt.settings.ToolNames = []string{}
	if _, err := a.executeTool(ctx, &rt, call, nil); err == nil {
		t.Fatal("disabled tool executed")
	}
	raw = requestJSON(t, client, "POST", api+"/model-profiles/"+viaProxy+"/test", map[string]string{"input": "蓝色的山"}, 200)
	if !bytes.Contains(raw, []byte(`"contentType":"image/png"`)) || !bytes.Contains(raw, []byte("data:image/png;base64,")) || calls != 2 {
		t.Fatal("image profile test failed", string(raw))
	}
	costs := dataMap(t, requestJSON(t, client, "GET", api+"/system/costs", nil, 200))
	if costs["total"] != 0.25 {
		t.Fatalf("image test usage %v", costs)
	}
	profileBody["pricePerImage"] = 0.5
	requestJSON(t, client, "PUT", api+"/model-profiles/"+viaProxy, profileBody, 200)
	costs = dataMap(t, requestJSON(t, client, "GET", api+"/system/costs", nil, 200))
	if costs["total"] != 0.5 {
		t.Fatalf("repriced image usage %v", costs)
	}
	requestJSON(t, client, "DELETE", api+"/model-profiles/"+viaProxy, nil, 200)
	requestJSON(t, client, "PUT", bots+"/"+svc, map[string]string{"name": "image-service", "imageProfileId": ""}, 200)
	settings, _, _, err = a.conversationAgent(ctx, cid, a.agentSettings(ctx))
	if err != nil || toolSelected(&turnRuntime{settings: settings}, "generate_image") {
		t.Fatal("cleared profile still available", err)
	}
}
