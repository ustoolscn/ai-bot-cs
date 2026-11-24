package app

import (
	"context"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"sync/atomic"
	"testing"

	"ai-bot/backend/internal/agent"
	"ai-bot/backend/internal/config"
	"ai-bot/backend/internal/domain"
)

func TestGroupStatusCooldown(t *testing.T) {
	url := os.Getenv("TEST_DATABASE_URL")
	if url == "" {
		t.Skip("TEST_DATABASE_URL not configured")
	}
	ctx := context.Background()
	pool := newIntegrationSchema(t, ctx, url)
	check := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}
	var statusHits atomic.Int32
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/service-status":
			statusHits.Add(1)
			w.Write([]byte(`{"success":true,"data":{"granularity":"hour","generated_at":123,"overall":{"request_count":20}}}`))
		case "/v1/chat/completions":
			w.Write([]byte(`{"choices":[{"message":{"role":"assistant","content":"当前有 20 个请求。"}}]}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer upstream.Close()
	a, err := New(ctx, config.Config{DataDir: t.TempDir(), MasterKey: []byte("0123456789abcdef0123456789abcdef"), AdminUsername: "admin", AdminPassword: "integration-password"}, pool, slog.Default())
	check(err)
	secret, err := a.cipher.Encrypt("key")
	check(err)
	_, err = pool.Exec(ctx, `INSERT INTO model_profiles(name,kind,base_url,api_key_enc,model,enabled,is_default) VALUES('test','chat',$1,$2,'test',true,true)`, upstream.URL+"/v1", secret)
	check(err)
	_, err = pool.Exec(ctx, `INSERT INTO system_settings(key,value) VALUES('agent_settings',jsonb_build_object('toolsEnabled',true,'liveData',jsonb_build_object('enabled',true,'baseUrl',$1::text,'statusImageStyle','off'))) ON CONFLICT(key) DO UPDATE SET value=excluded.value`, upstream.URL)
	check(err)
	var bot, cid, other, mid string
	check(pool.QueryRow(ctx, "INSERT INTO bots(name,app_id,app_secret_enc) VALUES('bot','app','secret') RETURNING id::text").Scan(&bot))
	conversation := func(platformID string) string {
		t.Helper()
		var id string
		check(pool.QueryRow(ctx, "INSERT INTO conversations(channel,bot_id,platform_id,type) VALUES('qq',$1,$2,'group') RETURNING id::text", bot, platformID).Scan(&id))
		return id
	}
	cid, other = conversation("one"), conversation("two")
	ask := func(pid, sender string) {
		t.Helper()
		_, err := pool.Exec(ctx, `WITH m AS (INSERT INTO messages(channel,bot_id,conversation_id,platform_message_id,direction,event_type,sender_id,content)
			VALUES('qq',$1,$2,$3,'inbound','GROUP_AT_MESSAGE_CREATE',$4,'网站状态怎么样？') RETURNING id)
			INSERT INTO inbox_tasks(message_id) SELECT id FROM m`, bot, cid, pid, sender)
		check(err)
		check(a.processInbox(ctx))
	}
	ask("first", "first-user")
	check(pool.QueryRow(ctx, "SELECT id::text FROM messages WHERE reply_to_message_id='first' AND contains_site_status").Scan(&mid))
	firstHits := statusHits.Load()
	if firstHits == 0 {
		t.Fatal("first query never reached status endpoint")
	}
	exec := func(sql string) {
		t.Helper()
		_, err := pool.Exec(ctx, sql, mid)
		check(err)
	}
	recent := func(id string, want bool) {
		t.Helper()
		var got bool
		check(pool.QueryRow(ctx, recentStatusSQL, id).Scan(&got))
		if got != want {
			t.Fatalf("recent status = %v, want %v", got, want)
		}
	}
	recent(cid, true)
	recent(other, false)
	exec("UPDATE outbox_tasks SET status='expired' WHERE message_id=$1")
	recent(cid, false)
	exec("UPDATE messages SET status='sent',event_at=now()-interval '8 minutes' WHERE id=$1")
	exec("UPDATE outbox_tasks SET status='sent',updated_at=now()-interval '4 minutes' WHERE message_id=$1")
	recent(cid, true)
	rt := turnRuntime{settings: agent.DefaultSettings(), recentStatus: true}
	for _, name := range []string{"get_service_status", "get_group_status"} {
		if toolSelected(&rt, name) {
			t.Fatal("offered blocked tool", name)
		}
		if _, err := a.executeTool(ctx, &rt, domain.ToolCall{Name: name}, nil); err == nil || rt.liveStatus != nil || rt.statusImage != nil {
			t.Fatal("executed blocked tool", name)
		}
	}
	ask("question", "second-user")
	if statusHits.Load() != firstHits {
		t.Fatal("duplicate status request reached endpoint")
	}
	var skipped bool
	check(pool.QueryRow(ctx, `SELECT t.status='skipped' AND NOT EXISTS(SELECT FROM agent_runs WHERE message_id=m.id)
		AND NOT EXISTS(SELECT FROM messages WHERE reply_to_message_id='question') FROM inbox_tasks t JOIN messages m ON m.id=t.message_id WHERE m.platform_message_id='question'`).Scan(&skipped))
	if !skipped {
		t.Fatal("repeated explicit request generated a reply")
	}
	exec("UPDATE messages SET context_excluded=true WHERE id=$1")
	recent(cid, false)
	exec("UPDATE messages SET context_excluded=false,status='retracted' WHERE id=$1")
	recent(cid, false)
	exec("UPDATE messages SET status='sent' WHERE id=$1")
	exec("UPDATE outbox_tasks SET updated_at=now()-interval '5 minutes' WHERE message_id=$1")
	recent(cid, false)
	ask("after-expiry", "third-user")
	if statusHits.Load() == firstHits {
		t.Fatal("expired report still blocked endpoint")
	}
}
