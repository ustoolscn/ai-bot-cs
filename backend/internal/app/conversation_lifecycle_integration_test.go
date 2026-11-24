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
	"ai-bot/backend/internal/db"
	"ai-bot/backend/internal/qq"
	"github.com/google/uuid"
)

func TestConversationLifecycle(t *testing.T) {
	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("TEST_DATABASE_URL not set")
	}
	ctx := context.Background()
	pool := newIntegrationSchema(t, ctx, dsn)
	check := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}
	exec := func(sql string, args ...any) {
		t.Helper()
		_, err := pool.Exec(ctx, sql, args...)
		check(err)
	}
	value := func(sql string, args ...any) string {
		t.Helper()
		var v string
		check(pool.QueryRow(ctx, sql, args...).Scan(&v))
		return v
	}
	cfg := config.Config{DataDir: t.TempDir(), MasterKey: []byte("0123456789abcdef0123456789abcdef"), AdminUsername: "admin", AdminPassword: "test-password"}
	a, err := New(ctx, cfg, pool, slog.New(slog.NewTextHandler(io.Discard, nil)))
	check(err)
	server := httptest.NewServer(a.Router())
	defer server.Close()
	jar, _ := cookiejar.New(nil)
	client := &http.Client{Jar: jar}
	req := func(method, path string, in any, status int) string {
		t.Helper()
		return string(requestJSON(t, client, method, server.URL+"/api/"+path, in, status))
	}
	req("POST", "auth/login", map[string]string{"username": "admin", "password": "test-password"}, 200)
	bot := value("INSERT INTO bots(name,app_id,app_secret_enc) VALUES('bot','bot','x') RETURNING id")
	base := time.Now().Add(-time.Hour).Truncate(time.Second)
	event := func(kind string, offset int) {
		t.Helper()
		at := base.Add(time.Duration(offset) * time.Second)
		data := map[string]any{"group_openid": "group", "timestamp": at.Unix()}
		if kind == "GROUP_MESSAGE_CREATE" {
			data["timestamp"] = at.Format(time.RFC3339)
			data["id"] = uuid.NewString()
			data["author"] = map[string]string{"member_openid": "member"}
		}
		raw, err := json.Marshal(map[string]any{"id": uuid.NewString(), "t": kind, "d": data})
		check(err)
		env, msg, err := qq.Parse(raw, bot)
		check(err)
		check(a.persistInbound(ctx, bot, env, msg, raw))
	}
	event("GROUP_ADD_ROBOT", 0)
	cid := value("SELECT id FROM conversations WHERE bot_id=$1", bot)
	listed := func(want bool) {
		t.Helper()
		body := req("GET", "conversations", nil, 200)
		if strings.Contains(body, cid) != want {
			t.Fatalf("listed=%t, response %s", want, body)
		}
	}
	exec("UPDATE conversations SET enabled=false WHERE id=$1", cid)
	listed(true)
	event("GROUP_DEL_ROBOT", 2)
	listed(false)
	event("GROUP_MESSAGE_CREATE", 1)
	listed(false)
	event("GROUP_ADD_ROBOT", 3)
	listed(true)
	event("GROUP_DEL_ROBOT", 4)
	event("GROUP_MESSAGE_CREATE", 5)
	listed(true)
	if value("SELECT enabled::text FROM conversations WHERE id=$1", cid) != "false" {
		t.Fatal("manual disable lost")
	}
	event("GROUP_DEL_ROBOT", 6)
	// Replay an upgrade with a delayed old message, then run the migrations twice.
	exec("DROP VIEW schedule_recipients; ALTER TABLE conversations DROP COLUMN bot_removed,DROP COLUMN membership_event_at")
	exec("UPDATE webhook_events SET created_at=now() WHERE event_type='GROUP_MESSAGE_CREATE'")
	check(db.Migrate(ctx, pool))
	listed(false)
	event("GROUP_ADD_ROBOT", 7)
	check(db.Migrate(ctx, pool))
	listed(true)

	svc := value("INSERT INTO bot_services(bot_id,name) VALUES($1,'service') RETURNING id", bot)
	exec("UPDATE conversations SET service_id=$1 WHERE id=$2", svc, cid)
	schedule := value("INSERT INTO scheduled_posts(name,service_id,send_time,instruction,source) VALUES('global',$1,'08:00','test','group') RETURNING id", svc)
	target := func(id string) {
		exec("INSERT INTO schedule_targets(schedule_id,conversation_id,service_id) VALUES($1,$2,$3)", schedule, id, svc)
	}
	target(cid)
	exec("UPDATE conversations SET enabled=true WHERE id=$1", cid)
	recipients := func(want string) {
		t.Helper()
		if got := value("SELECT count(*)::text FROM schedule_recipients WHERE schedule_id=$1", schedule); got != want {
			t.Fatalf("got %s schedule recipients, want %s", got, want)
		}
	}
	recipients("1")
	event("GROUP_DEL_ROBOT", 8)
	recipients("0")
	event("GROUP_MESSAGE_CREATE", 9)
	recipients("1")
	exec("INSERT INTO user_memories(bot_id,conversation_id,platform_user_id,content) VALUES($1,$2,'member','fact')", bot, cid)
	body := req("DELETE", "conversations/"+cid, nil, 200)
	if !strings.Contains(body, `"deleted":true`) {
		t.Fatal(body)
	}
	req("DELETE", "conversations/"+cid, nil, 404)
	for _, table := range []string{"messages", "conversation_members", "user_memories", "schedule_targets"} {
		if value(fmt.Sprintf("SELECT count(*)::text FROM %s WHERE conversation_id=$1", table), cid) != "0" {
			t.Fatal(table)
		}
	}
	event("GROUP_MESSAGE_CREATE", 10)
	newID := value("SELECT id FROM conversations WHERE bot_id=$1", bot)
	if cid == newID {
		t.Fatal("expected a fresh conversation")
	}
	exec("UPDATE conversations SET service_id=$1 WHERE id=$2", svc, newID)
	target(newID)
	privateID := value("INSERT INTO conversations(channel,bot_id,type,platform_id) VALUES('qq',$1,'private','user') RETURNING id", bot)
	req("DELETE", "conversations/"+privateID, nil, 200)
	req("DELETE", "bots/"+bot, nil, 200)
	for _, table := range []string{"bots", "bot_services", "conversations", "messages", "webhook_events", "schedule_targets"} {
		if value("SELECT count(*)::text FROM "+table) != "0" {
			t.Fatal(table)
		}
	}
	if value("SELECT (service_id IS NULL)::text FROM scheduled_posts WHERE id=$1", schedule) != "true" {
		t.Fatal("global schedule still bound")
	}
}
