package app

import (
	"context"
	"encoding/json"
	"fmt"
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

func TestBlacklistIsolationAndOfficialKick(t *testing.T) {
	databaseURL := os.Getenv("TEST_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("TEST_DATABASE_URL not configured")
	}
	ctx := context.Background()
	pool := newIntegrationSchema(t, ctx, databaseURL)
	kicks := 0
	reject := false
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/token" {
			w.Write([]byte(`{"access_token":"token"}`))
			return
		}
		if !strings.HasSuffix(r.URL.Path, "/batch_remove_members") {
			t.Errorf("unexpected QQ call %s", r.URL.Path)
		}
		kicks++
		if reject {
			w.WriteHeader(403)
			w.Write([]byte(`{"code":40103012,"message":"owner disabled"}`))
			return
		}
		w.Write([]byte(`{"remove_members_result":"success"}`))
	}))
	defer upstream.Close()
	a, err := New(ctx, config.Config{DataDir: t.TempDir(), MasterKey: []byte("0123456789abcdef0123456789abcdef"), AdminUsername: "admin", AdminPassword: "test-password", QQAPIBaseURL: upstream.URL, QQTokenURL: upstream.URL + "/token"}, pool, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatal(err)
	}
	secret, _ := a.cipher.Encrypt("secret")
	var botID string
	if err = pool.QueryRow(ctx, `INSERT INTO bots(name,app_id,app_secret_enc) VALUES('blacklist','app',$1) RETURNING id::text`, secret).Scan(&botID); err != nil {
		t.Fatal(err)
	}
	persist := func(eventID, eventType, pid, group, user string) {
		t.Helper()
		raw, _ := json.Marshal(map[string]any{"id": eventID, "op": 0, "t": eventType, "d": map[string]any{"id": pid, "group_openid": group, "member_openid": user, "author": map[string]string{"member_openid": user}, "content": "黑名单测试内容"}})
		env, msg, e := qq.Parse(raw, botID)
		if e != nil {
			t.Fatal(e)
		}
		if e = a.persistInbound(ctx, botID, env, msg, raw); e != nil {
			t.Fatal(e)
		}
	}
	persist("e1", "GROUP_AT_MESSAGE_CREATE", "m1", "g1", "member")
	persist("e2", "GROUP_MESSAGE_CREATE", "m2", "g2", "member")
	var convID, otherConv, mid string
	pool.QueryRow(ctx, `SELECT id::text FROM conversations WHERE platform_id='g1'`).Scan(&convID)
	pool.QueryRow(ctx, `SELECT id::text FROM conversations WHERE platform_id='g2'`).Scan(&otherConv)
	pool.QueryRow(ctx, `SELECT id::text FROM messages WHERE platform_message_id='m1'`).Scan(&mid)
	server := httptest.NewServer(a.Router())
	defer server.Close()
	jar, _ := cookiejar.New(nil)
	client := &http.Client{Jar: jar}
	requestJSON(t, client, http.MethodPost, server.URL+"/api/auth/login", map[string]any{"username": "admin", "password": "test-password"}, http.StatusOK)
	requestJSON(t, client, http.MethodPost, server.URL+"/api/blocked-users", map[string]any{"botId": botID, "userId": "123456789"}, http.StatusBadRequest)
	input := map[string]any{"botId": botID, "conversationId": convID, "userId": "member", "reason": "test"}
	response := dataMap(t, requestJSON(t, client, http.MethodPost, server.URL+"/api/blocked-users", input, http.StatusOK))
	id := fmt.Sprint(response["id"])
	again := dataMap(t, requestJSON(t, client, http.MethodPost, server.URL+"/api/blocked-users", input, http.StatusOK))
	if again["id"] != id {
		t.Fatal("duplicate block created")
	}
	excluded, e := a.messageExcluded(ctx, mid)
	if e != nil || !excluded {
		t.Fatalf("not excluded: %v %v", excluded, e)
	}
	var otherExcluded bool
	pool.QueryRow(ctx, `SELECT context_excluded FROM messages WHERE platform_message_id='m2'`).Scan(&otherExcluded)
	if otherExcluded {
		t.Fatal("group-scoped block leaked to another group")
	}
	if e = a.processInbox(ctx); e != nil {
		t.Fatal(e)
	}
	var status string
	pool.QueryRow(ctx, `SELECT status FROM inbox_tasks WHERE message_id=$1`, mid).Scan(&status)
	if status != "skipped" {
		t.Fatalf("queued inbox not skipped: %s", status)
	}
	if e = a.saveMemory(ctx, a.agentSettings(ctx), botID, "member", "", "fact", "should never save", 0.9, "extracted", mid); e != nil {
		t.Fatal(e)
	}
	var memories int
	pool.QueryRow(ctx, `SELECT count(*) FROM user_memories`).Scan(&memories)
	if memories != 0 {
		t.Fatal("blocked memory saved")
	}
	if e = a.mineStaffAnswer(ctx, mid, true); e != nil {
		t.Fatal(e)
	}
	turns, _, _ := a.loadHistory(ctx, convID, "00000000-0000-0000-0000-000000000000", time.Time{}, "", a.agentSettings(ctx), 20)
	if len(turns) != 0 {
		t.Fatalf("blocked context leaked: %#v", turns)
	}
	memberAPI := requestJSON(t, client, http.MethodGet, server.URL+"/api/conversations/"+convID+"/members", nil, http.StatusOK)
	if !strings.Contains(string(memberAPI), `"isBlocked":true`) {
		t.Fatalf("member blacklist status missing: %s", memberAPI)
	}
	var actionID string
	pool.QueryRow(ctx, `SELECT id::text FROM blacklist_actions WHERE blocked_user_id=$1`, id).Scan(&actionID)
	if e = a.jobBlacklistKick(ctx, actionID); e != nil {
		t.Fatal(e)
	}
	pool.QueryRow(ctx, `SELECT status FROM blacklist_actions WHERE id=$1`, actionID).Scan(&status)
	if status != "kicked" || kicks != 1 {
		t.Fatalf("kick status=%s calls=%d", status, kicks)
	}
	persist("join", "GROUP_MEMBER_ADD", "", "g1", "member")
	persist("join", "GROUP_MEMBER_ADD", "", "g1", "member")
	reject = true
	if e = a.jobBlacklistKick(ctx, actionID); e != nil {
		t.Fatal(e)
	}
	pool.QueryRow(ctx, `SELECT status FROM blacklist_actions WHERE id=$1`, actionID).Scan(&status)
	var alerts int
	pool.QueryRow(ctx, `SELECT count(*) FROM system_alerts WHERE kind='blacklist_kick_failed'`).Scan(&alerts)
	if status != "failed" || alerts != 1 {
		t.Fatalf("failed kick status=%s alerts=%d", status, alerts)
	}
	persist("e3", "GROUP_AT_MESSAGE_CREATE", "m3", "g1", "member")
	if e = a.jobBlacklistKick(ctx, actionID); e != nil {
		t.Fatal(e)
	}
	if kicks != 2 {
		t.Fatal("failed kick was not throttled")
	}
	var tasks int
	pool.QueryRow(ctx, `SELECT count(*) FROM inbox_tasks t JOIN messages m ON m.id=t.message_id WHERE m.platform_message_id='m3'`).Scan(&tasks)
	if tasks != 0 {
		t.Fatal("blocked message queued")
	}
	list := requestJSON(t, client, http.MethodGet, server.URL+"/api/blocked-users?conversationId="+convID, nil, http.StatusOK)
	if !strings.Contains(string(list), "40103012") || !strings.Contains(string(list), actionID) {
		t.Fatalf("missing moderation results: %s", list)
	}
	requestJSON(t, client, http.MethodDelete, server.URL+"/api/blocked-users/"+id, nil, http.StatusOK)
	if e = a.jobBlacklistKick(ctx, actionID); e != nil {
		t.Fatal(e)
	}
	if kicks != 2 {
		t.Fatal("deleted block still kicked")
	}
	globalInput := map[string]any{"botId": botID, "userId": "member", "autoKick": true}
	global := dataMap(t, requestJSON(t, client, http.MethodPost, server.URL+"/api/blocked-users", globalInput, http.StatusOK))
	globalID := fmt.Sprint(global["id"])
	pool.QueryRow(ctx, `SELECT id::text FROM blacklist_actions WHERE blocked_user_id=$1 AND conversation_id=$2`, globalID, otherConv).Scan(&actionID)
	if actionID == "" {
		t.Fatal("bot-wide block did not queue another group")
	}
	globalInput["autoKick"] = false
	requestJSON(t, client, http.MethodPost, server.URL+"/api/blocked-users", globalInput, http.StatusOK)
	if e = a.jobBlacklistKick(ctx, actionID); e != nil {
		t.Fatal(e)
	}
	if kicks != 2 {
		t.Fatal("disabled auto kick executed")
	}
	globalInput["autoKick"] = true
	requestJSON(t, client, http.MethodPost, server.URL+"/api/blocked-users", globalInput, http.StatusOK)
	reject = false
	if e = a.jobBlacklistKick(ctx, actionID); e != nil {
		t.Fatal(e)
	}
	if kicks != 3 {
		t.Fatal("re-enabled auto kick not scheduled")
	}
	persist("e4", "GROUP_AT_MESSAGE_CREATE", "m4", "g2", "member")
	pool.QueryRow(ctx, `SELECT context_excluded FROM messages WHERE platform_message_id='m4'`).Scan(&otherExcluded)
	if !otherExcluded {
		t.Fatal("bot-wide block did not exclude second group")
	}
}
