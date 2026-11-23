package app

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	"ai-bot/backend/internal/config"
	"ai-bot/backend/internal/db"
	"github.com/google/uuid"
)

func TestScheduleDistributionAndReviewDeletion(t *testing.T) {
	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("TEST_DATABASE_URL not configured")
	}
	ctx := context.Background()
	pool := newIntegrationSchema(t, ctx, dsn)
	mock := &agentMock{}
	mock.Server = httptest.NewServer(http.HandlerFunc(mock.serve))
	defer mock.Close()
	a, err := New(ctx, config.Config{MasterKey: []byte("0123456789abcdef0123456789abcdef"), AdminUsername: "admin", AdminPassword: "test-password", DataDir: t.TempDir(), QQTokenURL: mock.URL + "/token", QQAPIBaseURL: mock.URL}, pool, slog.New(slog.NewTextHandler(io.Discard, nil)))
	check := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}
	check(err)
	server := httptest.NewServer(a.Router())
	defer server.Close()
	jar, _ := cookiejar.New(nil)
	client := &http.Client{Jar: jar}
	api := server.URL + "/api/"
	requestJSON(t, client, "POST", api+"auth/login", map[string]string{"username": "admin", "password": "test-password"}, 200)
	insert := func(query string, args ...any) string {
		t.Helper()
		var id string
		check(pool.QueryRow(ctx, query+" RETURNING id::text", args...).Scan(&id))
		return id
	}
	count := func(want int, query string, args ...any) {
		t.Helper()
		var n int
		check(pool.QueryRow(ctx, query, args...).Scan(&n))
		if want != n {
			t.Fatalf("%s: want %d got %d", query, want, n)
		}
	}
	secret, err := a.cipher.Encrypt("secret")
	check(err)
	makeGroup := func(i int) (string, string) {
		bot := insert("INSERT INTO bots(name,app_id,app_secret_enc) VALUES('bot',$1,$2)", fmt.Sprint(i), secret)
		return bot, insert("INSERT INTO conversations(channel,bot_id,platform_id,type,name) VALUES('qq',$1,$2,'group',$2)", bot, fmt.Sprint(i))
	}
	bot, one := makeGroup(1)
	_, two := makeGroup(2)
	insert("INSERT INTO model_profiles(name,kind,base_url,api_key_enc,model,enabled,is_default,web_search_mode) VALUES('search','chat',$1,$2,'search',true,true,'responses')", mock.URL+"/v1", secret)
	input := map[string]any{"name": "天气", "sendTime": "08:00", "instruction": "查询天气", "source": "web", "format": "text", "enabled": true, "groupIds": []string{one, two}}
	id := createdID(t, requestJSON(t, client, "POST", api+"schedules", input, 200))
	path := api + "schedules/" + id
	_, three := makeGroup(3)
	testBody := map[string]any{"requestId": uuid.NewString(), "groupIds": []string{three}}
	requestJSON(t, client, "POST", path+"/test", testBody, 400)
	testBody["groupIds"] = []string{one, two}
	for i := 0; i < 2; i++ {
		requestJSON(t, client, "POST", path+"/test", testBody, 200)
	}
	count(1, "SELECT count(*) FROM schedule_runs WHERE is_test")
	count(2, "SELECT count(*) FROM schedule_deliveries")
	check(a.processAgentJob(ctx))
	check(a.processOutbox(ctx))
	check(a.processOutbox(ctx))
	count(2, "SELECT count(DISTINCT m.bot_id) FROM messages m JOIN schedule_deliveries d ON m.id=d.message_id WHERE m.status='sent'")
	count(1, "SELECT count(*) FROM model_calls WHERE kind='scheduled_post'")
	requestJSON(t, client, "GET", path+"/runs", nil, 200)
	input["allGroups"] = true
	requestJSON(t, client, "PUT", path, input, 200)
	tomorrow := time.Now().In(chinaLocation).AddDate(0, 0, 1)
	at := time.Date(tomorrow.Year(), tomorrow.Month(), tomorrow.Day(), 8, 0, 1, 0, chinaLocation)
	check(a.queueScheduledPosts(ctx, at))
	check(a.queueScheduledPosts(ctx, at))
	count(1, "SELECT count(*) FROM schedule_runs WHERE NOT is_test")
	count(3, "SELECT count(*) FROM schedule_deliveries d JOIN schedule_runs r ON r.id=d.run_id WHERE NOT r.is_test")
	check(a.processAgentJob(ctx))
	for i := 0; i < 3; i++ {
		check(a.processOutbox(ctx))
	}
	count(3, "SELECT count(DISTINCT m.conversation_id) FROM messages m JOIN schedule_deliveries d ON d.message_id=m.id JOIN schedule_runs r ON r.id=d.run_id WHERE NOT is_test AND m.status='sent'")
	requestJSON(t, client, "GET", api+"schedules", nil, 200)
	kb := insert("INSERT INTO knowledge_bases(name) VALUES('test')")
	msg := insert("INSERT INTO messages(channel,bot_id,conversation_id,direction) VALUES('qq',$1,$2,'inbound')", bot, one)
	for _, tc := range []struct {
		collection, table, create string
		args                      []any
	}{
		{"entries", "knowledge_entries", "INSERT INTO knowledge_entries(knowledge_base_id,question,answer) VALUES($1,'q','a')", []any{kb}},
		{"candidates", "knowledge_candidates", "INSERT INTO knowledge_candidates(question,answer,source) VALUES('q','a','manual')", nil},
		{"gaps", "knowledge_gaps", "INSERT INTO knowledge_gaps(question) VALUES('q')", nil},
		{"feedback", "message_feedback", "INSERT INTO message_feedback(message_id,rating) VALUES($1,'good')", []any{msg}},
		{"memories", "user_memories", "INSERT INTO user_memories(bot_id,platform_user_id,content) VALUES($1,'user','记忆')", []any{bot}},
	} {
		ids := []string{insert(tc.create, tc.args...), insert(tc.create, tc.args...)}
		keep := insert(tc.create, tc.args...)
		endpoint := api + "learning/" + tc.collection + "/bulk-delete"
		requestJSON(t, client, "POST", endpoint, map[string]any{"ids": []string{ids[0], "bad-id"}}, 400)
		count(2, "SELECT count(*) FROM "+tc.table+" WHERE id=ANY($1::uuid[])", ids)
		res := dataMap(t, requestJSON(t, client, "POST", endpoint, map[string]any{"ids": ids}, 200))
		if res["deleted"] != float64(2) {
			t.Fatalf("%s: %v", tc.collection, res)
		}
		count(1, "SELECT count(*) FROM "+tc.table+" WHERE id=$1", keep)
	}

	// Replaying migrations also upgrades an old single-group task with its existing delivery job.
	oldJob := uuid.NewString()
	legacy := insert("INSERT INTO scheduled_posts(conversation_id,send_time,instruction,source,last_run_date,last_job_id) VALUES($1,'08:00','旧任务','group',current_date,$2)", one, oldJob)
	check(db.Migrate(ctx, pool))
	check(db.Migrate(ctx, pool))
	count(1, "SELECT count(*) FROM schedule_targets WHERE schedule_id=$1 AND conversation_id=$2", legacy, one)
	count(1, "SELECT count(*) FROM schedule_runs r JOIN schedule_deliveries d ON d.run_id=r.id WHERE r.schedule_id=$1 AND d.job_id=$2", legacy, oldJob)
	count(1, "SELECT count(*) FROM scheduled_posts WHERE id=$1 AND conversation_id IS NULL AND last_run_date=current_date", legacy)
}
