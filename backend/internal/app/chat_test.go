package app

import (
	"context"
	"encoding/json"
	"github.com/gin-gonic/gin"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"
)

func TestChatCursorAndReplyFlags(t *testing.T) {
	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("TEST_DATABASE_URL not set")
	}
	ctx := context.Background()
	pool := newIntegrationSchema(t, ctx, dsn)
	a := App{db: pool}
	var cid, bot, mid string
	check := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}
	check(pool.QueryRow(ctx, "INSERT INTO bots(name,app_id,app_secret_enc) VALUES('bot','chat','enc') RETURNING id::text").Scan(&bot))
	check(pool.QueryRow(ctx, "INSERT INTO conversations(channel,bot_id,type,platform_id) VALUES('qq',$1,'group','chat') RETURNING id::text", bot).Scan(&cid))
	check(pool.QueryRow(ctx, `INSERT INTO messages(channel,bot_id,conversation_id,direction,platform_message_id,qq_ref_idx,reply_deadline,sender_id,sender_name,content) VALUES('qq',$1,$2,'inbound','platform','idx',now()+interval '1 hour','u','小明','<@u>') RETURNING id::text`, bot, cid).Scan(&mid))
	_, err := pool.Exec(ctx, `INSERT INTO messages(channel,bot_id,conversation_id,direction,event_type,reply_to_message_id,status) VALUES('qq',$1,$2,'outbound','QQ_ADMIN_SEND','platform','pending')`, bot, cid)
	check(err)
	_, err = pool.Exec(ctx, `INSERT INTO conversation_members(conversation_id,platform_user_id,display_name,qq_avatar) VALUES($1,'u','小明','https://example.org/avatar')`, cid)
	check(err)
	r := gin.New()
	r.GET("/conversations/:id/chat", a.chatHistory)
	r.POST("/mention-names", a.mentionNames)
	fetch := func(method, path, body string) map[string]any {
		t.Helper()
		w := httptest.NewRecorder()
		req := httptest.NewRequest(method, path, strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		r.ServeHTTP(w, req)
		if w.Code != 200 {
			t.Fatal(w.Body.String())
		}
		var result struct{ Data map[string]any }
		check(json.Unmarshal(w.Body.Bytes(), &result))
		return result.Data
	}
	path := "/conversations/" + cid + "/chat?limit=1"
	first := fetch("GET", path, "")
	row := first["items"].([]any)[0].(map[string]any)
	if row["kind"] != "manual" || row["role"] != "human" || row["status"] != "pending" || row["replyToId"] != mid || row["canReply"] != false {
		t.Fatal(row)
	}
	last := fetch("GET", path+"&before="+first["nextBefore"].(string), "")
	row = last["items"].([]any)[0].(map[string]any)
	if row["senderAvatar"] != "https://example.org/avatar" || row["senderName"] != "小明" || row["id"] != mid || row["canReply"] != true || row["quotable"] != true || last["nextBefore"] != nil {
		t.Fatal(last)
	}
	_, err = pool.Exec(ctx, `INSERT INTO messages(channel,bot_id,conversation_id,direction,event_type,content) VALUES('qq',$1,$2,'outbound','SCHEDULED_POST','公告')`, bot, cid)
	check(err)
	scheduled := fetch("GET", path, "")["items"].([]any)[0].(map[string]any)
	if scheduled["role"] != "bot" || scheduled["kind"] != "scheduled" || scheduled["senderAvatar"] != "" {
		t.Fatal(scheduled)
	}
	names := fetch("POST", "/mention-names", `{"ids":["u","unknown"]}`)
	if len(names) != 1 || names["u"] != "小明" {
		t.Fatal(names)
	}
}
func TestPersonaEvidenceAndSmoothing(t *testing.T) {
	metric := func(score float64) map[string]any {
		return map[string]any{"score": score, "confidence": 0.5, "evidence": "主动索要对比数据"}
	}
	at := time.Now()
	old := mergePersona(nil, map[string]any{"communication": map[string]any{"decision": metric(60)}, "roles": []any{"开发者", "身份证"}}, at)
	next := mergePersona(old, map[string]any{"communication": map[string]any{"decision": metric(100)}}, at)
	got := next["communication"].(map[string]any)["decision"].(map[string]any)
	if got["score"] != 65.0 || next["observedTurns"] != 2.0 || len(next["roles"].([]any)) != 1 {
		t.Fatal(next)
	}
}
