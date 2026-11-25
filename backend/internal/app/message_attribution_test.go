package app

import (
	"context"
	"github.com/gin-gonic/gin"
	"net/http/httptest"
	"os"
	"testing"

	"ai-bot/backend/internal/agent"
	"ai-bot/backend/internal/db"
)

func TestMessageLearningAttribution(t *testing.T) {
	url := os.Getenv("TEST_DATABASE_URL")
	if url == "" {
		t.Skip("TEST_DATABASE_URL not configured")
	}
	ctx := context.Background()
	pool := newIntegrationSchema(t, ctx, url)
	scalar := func(query string, args ...any) string {
		t.Helper()
		var value string
		if err := pool.QueryRow(ctx, query, args...).Scan(&value); err != nil {
			t.Fatal(err)
		}
		return value
	}
	exec := func(query string, args ...any) {
		t.Helper()
		if _, err := pool.Exec(ctx, query, args...); err != nil {
			t.Fatal(err)
		}
	}
	bot := scalar("INSERT INTO bots(name,app_id,app_secret_enc) VALUES('qq','a','test') RETURNING id::text")
	cid := scalar("INSERT INTO conversations(channel,bot_id,platform_id,type) VALUES('qq',$1,'room','group') RETURNING id::text", bot)
	kb := scalar("INSERT INTO knowledge_bases(name) VALUES('A') RETURNING id::text")
	exec("INSERT INTO conversation_knowledge_bases VALUES($1,$2)", cid, kb)
	incoming := func(platform string) string {
		return scalar(`INSERT INTO messages(channel,bot_id,conversation_id,direction,platform_message_id,raw_event)
   VALUES('qq',$1,$2,'inbound',$3,'{"d":{"mentions":[{"id":"raw","username":"R"}]}}') RETURNING id::text`, bot, cid, platform)
	}
	first := incoming("first")
	exec("INSERT INTO conversation_members(conversation_id,platform_user_id,display_name) VALUES($1,'member','M')", cid)
	if got := scalar(`SELECT (message_mention_names(m,'<@raw> <@!member> <@missing>')='{"raw":"R","member":"M"}'::jsonb)::text FROM messages m WHERE id=$1`, first); got != "true" {
		t.Fatal("mentions", got)
	}
	exec("WITH k AS (INSERT INTO knowledge_bases(name) VALUES('B') RETURNING id) INSERT INTO conversation_knowledge_bases SELECT $1,id FROM k", cid)
	second := incoming("second")
	app := &App{db: pool}
	for _, id := range []string{first, second, first} {
		app.recordGap(ctx, agent.DefaultSettings(), "same question", id)
	}
	if got := scalar("SELECT occurrences::text FROM knowledge_gaps WHERE knowledge_base_id=$1", kb); got != "2" {
		t.Fatal("owned occurrences", got)
	}
	if got := scalar("SELECT occurrences::text FROM knowledge_gaps WHERE knowledge_base_id IS NULL"); got != "1" {
		t.Fatal("unassigned occurrences", got)
	}
	reply := scalar("INSERT INTO messages(channel,bot_id,conversation_id,direction,reply_to_message_id) VALUES('qq',$1,$2,'outbound','first') RETURNING id::text", bot, cid)
	exec("INSERT INTO message_feedback(message_id,rating) VALUES($1,'bad')", reply)
	if err := db.Migrate(ctx, pool); err != nil {
		t.Fatal(err)
	}
	if got := scalar("SELECT knowledge_base_id::text FROM message_feedback WHERE message_id=$1", reply); got != kb {
		t.Fatal("feedback moved on rebind", got)
	}

	// Simulate a deep link after the original question leaves the list.
	exec(`INSERT INTO inbox_tasks(message_id,status,last_error) VALUES($1,'failed','context deadline exceeded')`, first)
	exec(`INSERT INTO messages(channel,bot_id,conversation_id,direction,event_at)
 SELECT 'qq',$1,$2,'inbound',now()+interval '1 minute' FROM generate_series(1,55)`, bot, cid)
	exec(`INSERT INTO inbox_tasks(message_id) SELECT id FROM messages WHERE event_at>now()`)
	get := func(path string, handler gin.HandlerFunc) []byte {
		t.Helper()
		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request = httptest.NewRequest("GET", path, nil)
		c.Params = gin.Params{{Key: "id", Value: first}}
		handler(c)
		if w.Code != 200 {
			t.Fatalf("%s: %d %s", path, w.Code, w.Body.String())
		}
		return w.Body.Bytes()
	}
	if got := dataMap(t, get("/messages/"+first, app.getMessage))["id"]; got != first {
		t.Fatal("deep link", got)
	}
	for _, row := range dataSlice(t, get("/messages", app.listMessages)) {
		if object(t, row)["id"] == first {
			t.Fatal("old message unexpectedly on first page")
		}
	}
	for _, handler := range []gin.HandlerFunc{app.listFeedback, app.listGaps} {
		rows := dataSlice(t, get("/?knowledgeBaseId="+kb, handler))
		if len(rows) != 1 || object(t, rows[0])["knowledgeBaseName"] != "A" {
			t.Fatal("filtered attribution", rows)
		}
	}
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	alerts := app.overviewAlerts(c)
	if len(alerts) != 1 || alerts[0]["messageId"] != first || alerts[0]["kind"] != "inbox" {
		t.Fatal("alert destination", alerts)
	}
}
