package app

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"os"
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
