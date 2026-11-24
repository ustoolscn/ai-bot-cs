package app

import (
	"context"
	"log/slog"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
)

func TestLiveCostsAndDismissal(t *testing.T) {
	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("TEST_DATABASE_URL not configured")
	}
	ctx := context.Background()
	pool := newIntegrationSchema(t, ctx, dsn)
	a := &App{db: pool, log: slog.Default()}
	r := gin.New()
	r.GET("/costs", a.costs)
	r.GET("/messages/:id", a.getMessage)
	r.POST("/dismiss/:id", a.dismissFailed)
	r.POST("/dismiss", a.dismissFailed)
	r.POST("/retry", a.retryFailedTasks)
	r.GET("/overview", a.overview)
	srv := httptest.NewServer(r)
	defer srv.Close()
	call := func(method, path string) map[string]any {
		t.Helper()
		return dataMap(t, requestJSON(t, srv.Client(), method, srv.URL+path, nil, 200))
	}
	id := func(sql string, args ...any) string {
		t.Helper()
		var v string
		if err := pool.QueryRow(ctx, sql, args...).Scan(&v); err != nil {
			t.Fatal(err)
		}
		return v
	}
	exec := func(sql string, args ...any) {
		t.Helper()
		if _, err := pool.Exec(ctx, sql, args...); err != nil {
			t.Fatal(err)
		}
	}
	bot := id("INSERT INTO bots(name,app_id,app_secret_enc) VALUES('bot','app','secret') RETURNING id::text")
	conv := id("INSERT INTO conversations(channel,bot_id,type,platform_id,name) VALUES('qq',$1,'group','group','群') RETURNING id::text", bot)
	mid, task := insertOutboxFixture(t, pool, bot, conv, time.Now())
	profile := func(kind string) string {
		return id("INSERT INTO model_profiles(name,kind,base_url,api_key_enc,model) VALUES($1,$1,'url','key',$1) RETURNING id::text", kind)
	}
	chat, image, embed := profile("chat"), profile("image"), profile("embedding")
	exec("UPDATE model_profiles SET input_price=2 WHERE id=$1", chat)
	exec("UPDATE model_profiles SET price_per_image=3 WHERE id=$1", image)
	run := id("INSERT INTO agent_runs(message_id) VALUES($1) RETURNING id::text", mid)
	exec("INSERT INTO model_calls(profile_id,message_id,agent_run_id,kind,input_tokens,estimated_cost,created_at) VALUES($1,$2,$3,'rewrite',1000000,99,now()-interval '10 days')", chat, mid, run)
	exec("INSERT INTO model_calls(profile_id,conversation_id,kind,images) VALUES($1,$2,'image',2)", image, conv)
	exec("INSERT INTO model_calls(profile_id,kind,input_tokens,output_tokens) VALUES($1,'embedding_index',2000000,1000000)", embed)
	first := call("GET", "/costs?range=7d")
	if first["today"] != 6.0 || first["total"] != 8.0 || first["unpricedModels"].([]any)[0] != "embedding" {
		t.Fatal(first)
	}
	exec("UPDATE model_profiles SET input_price=7,output_price=4 WHERE id=$1", embed)
	exec("UPDATE model_profiles SET input_price=4 WHERE id=$1", chat)
	detail := call("GET", "/messages/"+mid)
	runDetail := object(t, detail["agentRuns"].([]any)[0])
	if detail["estimatedCost"] != 4.0 || object(t, runDetail["calls"].([]any)[0])["estimatedCost"] != 4.0 {
		t.Fatal(detail)
	}
	updated := call("GET", "/costs")
	if updated["today"] != 24.0 || updated["total"] != 28.0 || len(updated["unpricedModels"].([]any)) != 0 {
		t.Fatal(updated)
	}
	for dim, want := range map[string]string{"byBot": bot, "byConversation": conv} {
		rows := updated[dim].([]any)
		if len(rows) != 2 || object(t, rows[0])["id"] != "knowledge-index" || object(t, rows[1])["id"] != want || object(t, rows[1])["cost"] != 10.0 {
			t.Fatal(rows)
		}
	}
	kinds := first["byKind"].([]any)
	if len(kinds) != 3 || object(t, kinds[0])["cost"] != 0.0 || object(t, kinds[2])["images"] != 2.0 {
		t.Fatal(kinds)
	}
	exec("UPDATE outbox_tasks SET status='failed',last_error='403' WHERE id=$1", task)
	exec("INSERT INTO inbox_tasks(message_id,status) VALUES($1,'failed')", mid)
	if call("GET", "/overview")["failedTasks"] != 2.0 || call("POST", "/dismiss/"+task)["dismissed"] != 1.0 || call("GET", "/overview")["failedTasks"] != 1.0 {
		t.Fatal("single dismissal")
	}
	if call("POST", "/dismiss")["dismissed"] != 1.0 || call("POST", "/dismiss")["dismissed"] != 0.0 || call("POST", "/retry")["queued"] != 0.0 || call("GET", "/overview")["failedTasks"] != 0.0 {
		t.Fatal("bulk dismissal/retry")
	}
	if id("SELECT last_error FROM outbox_tasks WHERE id=$1 AND status='failed' AND dismissed_at IS NOT NULL", task) != "403" {
		t.Fatal("lost failure details")
	}
}
