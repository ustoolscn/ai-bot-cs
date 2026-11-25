package app

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"image"
	"image/color"
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

func TestGroupRosterAndGuard(t *testing.T) {
	databaseURL := os.Getenv("TEST_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("TEST_DATABASE_URL not configured")
	}
	ctx := context.Background()
	pool := newIntegrationSchema(t, ctx, databaseURL)
	picture := func(red uint8) []byte {
		img := image.NewRGBA(image.Rect(0, 0, 1, 1))
		img.Set(0, 0, color.RGBA{R: red, A: 255})
		var buf bytes.Buffer
		if err := png.Encode(&buf, img); err != nil {
			t.Fatal(err)
		}
		return buf.Bytes()
	}
	ad, normal := picture(255), picture(0)
	media := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/ad" {
			w.Write(ad)
		} else {
			w.Write(normal)
		}
	}))
	defer media.Close()
	deny := false
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/token":
			fmt.Fprint(w, `{"access_token":"token"}`)
		case "/v2/groups/g/info":
			if r.Method != "GET" || r.Header.Get("Authorization") != "QQBot token" {
				t.Error("invalid group info request")
			}
			if deny {
				fmt.Fprint(w, `{"code":11253}`)
				return
			}
			fmt.Fprint(w, `{"group_openid":"g","group_name":"QQ 测试群","group_avatar":"https://example.org/g.png"}`)
		case "/v2/groups/g/join_request_list":
			if r.URL.Query().Get("limit") != "50" {
				t.Error("missing application page size")
			}
			fmt.Fprint(w, `{"list":[{"member_openid":"user","username":"Alice","verify_info":{"review_qa_list":[{"question":"q","answer":"a"}]}}],"next_cursor":""}`)
		case "/v2/groups/g/approval_join_request/review":
			var p map[string]any
			json.NewDecoder(r.Body).Decode(&p)
			if p["op"] != "decline" || p["reject_reason"] != "reason" || p["add_to_member_blacklist"] != true {
				t.Errorf("bad decline: %#v", p)
			}
			fmt.Fprint(w, "{}")
		case "/v2/groups/g/messages":
			var p map[string]any
			json.NewDecoder(r.Body).Decode(&p)
			if ref, ok := p["message_reference"].(map[string]any); !ok || ref["message_id"] != "own-index" {
				t.Errorf("bad sent reference: %v", p)
			}
			fmt.Fprint(w, `{"id":"out-test","ext_info":{"ref_idx":"out-ref"},"timestamp":"2026-11-24T03:40:00+08:00"}`)
		case "/v2/groups/g/members":
			if deny {
				w.WriteHeader(403)
				fmt.Fprint(w, `{"code":11253}`)
				return
			}
			if r.URL.Query().Get("cursor") == "" {
				fmt.Fprint(w, `{"members":[],"next_cursor":"next"}`)
			} else {
				fmt.Fprint(w, `{"members":[{"member_openid":"silent","username":"未发言成员","member_role":"member"}]}`)
			}
		case "/v1/chat/completions":
			body, _ := io.ReadAll(r.Body)
			if bytes.Contains(body, []byte(`"type":"image_url"`)) {
				if !strings.Contains(string(body), "招揽购买") {
					t.Error("missing group intent")
				}
				if strings.Contains(string(body), "data:image/png;base64,"+base64.StdEncoding.EncodeToString(normal)) {
					fmt.Fprint(w, `{"choices":[{"message":{"content":"{\"violation\":false}"}}]}`)
					return
				}
				if !strings.Contains(string(body), "data:image/png;base64,"+base64.StdEncoding.EncodeToString(ad)) {
					t.Error("missing original ad image")
				}
			}
			fmt.Fprint(w, `{"choices":[{"message":{"content":"{\"violation\":true,\"reason\":\"主动招揽群友购买服务\"}"}}]}`)
		case "/v2/groups/g/messages/keyword", "/v2/groups/g/messages/semantic", "/v2/groups/g/messages/ad":
			if r.Method != "DELETE" {
				t.Errorf("unexpected method %s", r.Method)
			}
		case "/v2/groups/g/restrict_chat_setting":
			if r.Method == "GET" {
				fmt.Fprint(w, `{"members":[],"global_rule":{"mode":"none"}}`)
				return
			}
			var p struct{ Members []struct{ Op string } }
			json.NewDecoder(r.Body).Decode(&p)
			if len(p.Members) != 1 || (p.Members[0].Op != "add" && p.Members[0].Op != "update" && p.Members[0].Op != "del") {
				t.Errorf("invalid mute %v", p)
			}
			fmt.Fprint(w, "{}")
		case "/v2/groups/g/approval_join_request/new":
			if r.Method != "POST" {
				t.Errorf("unexpected method %s", r.Method)
			}
		default:
			t.Errorf("unexpected request %s", r.URL.Path)
			w.WriteHeader(404)
		}
	}))
	defer upstream.Close()
	a, err := New(ctx, config.Config{DataDir: t.TempDir(), MasterKey: []byte("0123456789abcdef0123456789abcdef"), AdminUsername: "admin", AdminPassword: "test-password", QQAPIBaseURL: upstream.URL, QQTokenURL: upstream.URL + "/token", AIRequestTimeout: 10 * time.Second}, pool, slog.New(slog.NewTextHandler(io.Discard, nil)))
	check := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}
	check(err)
	a.imageHTTP = media.Client()
	secret, err := a.cipher.Encrypt("secret")
	check(err)
	var bot, cid string
	check(pool.QueryRow(ctx, `INSERT INTO bots(name,app_id,app_secret_enc) VALUES('guard','app',$1) RETURNING id::text`, secret).Scan(&bot))
	check(pool.QueryRow(ctx, `INSERT INTO conversations(channel,type,bot_id,platform_id,name) VALUES('qq','group',$1,'g','test') RETURNING id::text`, bot).Scan(&cid))
	check(a.refreshMissingGroupInfo(ctx, cid))
	var name, avatar string
	check(pool.QueryRow(ctx, `SELECT name,qq_group_info->>'group_avatar' FROM conversations WHERE id=$1`, cid).Scan(&name, &avatar))
	if name != "test" || avatar != "https://example.org/g.png" {
		t.Fatalf("unexpected group display name %q and avatar %q", name, avatar)
	}
	_, err = pool.Exec(ctx, `INSERT INTO model_profiles(name,kind,base_url,api_key_enc,model,enabled,is_default) VALUES('decision','chat',$1,$2,'mock',true,true)`, upstream.URL+"/v1", secret)
	check(err)
	server := httptest.NewServer(a.Router())
	defer server.Close()
	jar, _ := cookiejar.New(nil)
	client := &http.Client{Jar: jar}
	requestJSON(t, client, "POST", server.URL+"/api/auth/login", map[string]string{"username": "admin", "password": "test-password"}, 200)
	path := server.URL + "/api/conversations/" + cid
	requestJSON(t, client, "GET", path+"/qq/applications", nil, 200)
	requestJSON(t, client, "GET", path+"/qq/mutes", nil, 200)
	requestJSON(t, client, "POST", path+"/qq/applications", map[string]any{"member": "review", "id": "request", "op": "decline", "block": true, "reason": "reason"}, 200)
	for _, op := range []string{"add", "update", "del"} {
		requestJSON(t, client, "POST", path+"/qq/mutes", map[string]any{"member": "test", "op": op, "minutes": 20}, 200)
	}
	requestJSON(t, client, "POST", path+"/qq/mutes", map[string]any{"member": "test", "op": "update", "minutes": 50000}, 400)
	var oldID, inID string
	check(pool.QueryRow(ctx, `INSERT INTO messages(channel,bot_id,conversation_id,direction,event_type,platform_message_id,content,status,qq_ref_idx) VALUES('qq',$1,$2,'outbound','AI_RESPONSE','own','原来的回复','sent','own-index') RETURNING id::text`, bot, cid).Scan(&oldID))
	check(pool.QueryRow(ctx, `INSERT INTO messages(channel,bot_id,conversation_id,direction,content,raw_event) VALUES('qq',$1,$2,'inbound','继续','{"d":{"message_scene":{"ext":["ref_msg_idx=own-index"]},"msg_elements":[{"content":"备用原文"}]}}') RETURNING id::text`, bot, cid).Scan(&inID))
	quoted, e := a.qqContext(ctx, inboxJob{MessageID: inID, ConversationID: cid, Content: "继续"})
	check(e)
	if !strings.Contains(quoted, "机器人：原来的回复") {
		t.Fatal(quoted)
	}
	requestJSON(t, client, "GET", path+"/delivery/options", nil, 200)
	requestJSON(t, client, "POST", path+"/delivery/send", map[string]any{"kind": "text", "text": "test", "reference": oldID}, 200)
	check(a.processOutbox(ctx))
	var sent bool
	check(pool.QueryRow(ctx, `SELECT EXISTS(SELECT FROM messages WHERE platform_message_id='out-test' AND qq_ref_idx='out-ref' AND qq_sent_at IS NOT NULL)`).Scan(&sent))
	if !sent {
		t.Fatal("missing QQ receipt")
	}
	requestJSON(t, client, "POST", path+"/name/sync", nil, 200)
	requestJSON(t, client, "POST", path+"/members/sync", nil, 200)
	deny = true
	requestJSON(t, client, "POST", path+"/members/sync?refresh=true", nil, 403)
	requestJSON(t, client, "POST", path+"/name/sync", nil, 403)
	var savedName string
	check(pool.QueryRow(ctx, "SELECT name FROM conversations WHERE id=$1", cid).Scan(&savedName))
	if savedName != "QQ 测试群" {
		t.Fatalf("unexpected saved name %q", savedName)
	}
	count := func(want int, sql string) {
		t.Helper()
		var n int
		check(pool.QueryRow(ctx, sql).Scan(&n))
		if n != want {
			t.Fatalf("%s: got %d want %d", sql, n, want)
		}
	}
	count(1, "SELECT count(*) FROM conversation_members WHERE active AND platform_user_id='silent'")
	policy := guardPolicy{Enabled: true, Keywords: "返佣", Intent: "招揽购买", Retract: true, MuteMinutes: 5, Block: true, JoinAnswer: "报名"}
	requestJSON(t, client, "PUT", path+"/guard", policy, 200)
	event := func(id, kind string, d map[string]any) {
		t.Helper()
		raw, _ := json.Marshal(map[string]any{"id": id, "op": 0, "t": kind, "d": d})
		env, msg, e := qq.Parse(raw, bot)
		check(e)
		check(a.persistInbound(ctx, bot, env, msg, raw))
	}
	message := func(id, content string) {
		d := map[string]any{"id": id, "group_openid": "g", "author": map[string]string{"member_openid": id}, "content": content}
		event("e"+id, "GROUP_MESSAGE_CREATE", d)
		event("e"+id, "GROUP_MESSAGE_CREATE", d)
		check(a.processGuardReview(ctx))
	}
	message("keyword", "私聊返佣")
	message("semantic", "来我这里买，有优惠")
	count(2, "SELECT count(*) FROM group_reviews WHERE status='violation'")
	count(2, "SELECT count(*) FROM blocked_users")
	for i := 0; i < 4; i++ {
		check(a.processGuardAction(ctx))
	}
	count(6, "SELECT count(*) FROM group_guard_actions WHERE status='done'")
	message("old", "返佣")
	requestJSON(t, client, "PUT", path+"/guard", policy, 200)
	for i := 0; i < 2; i++ {
		check(a.processGuardAction(ctx))
	}
	count(2, "SELECT count(*) FROM group_guard_actions WHERE status='skipped'")
	for _, answer := range []string{"报名", "无效"} {
		event("join"+answer, "GROUP_JOIN_REQUEST", map[string]any{"group_openid": "g", "member_openid": "new", "join_request_id": "request" + answer, "verify_info": map[string]string{"verify_message": answer}})
	}
	check(a.processGuardAction(ctx))
	count(1, "SELECT count(*) FROM group_guard_actions WHERE kind='approve' AND status='done'")
	for _, id := range []string{"ad", "normal"} {
		event("event-"+id, "GROUP_MESSAGE_CREATE", map[string]any{"group_openid": "g", "id": id, "author": map[string]string{"member_openid": id}, "attachments": []map[string]string{{"url": media.URL + "/" + id, "content_type": "image/png"}}})
		check(a.processGuardReview(ctx))
	}
	count(1, "SELECT count(*) FROM group_reviews r JOIN messages m ON m.id=r.message_id WHERE m.platform_message_id='normal' AND r.status='allowed' AND NOT m.context_excluded")
	count(0, "SELECT count(*) FROM group_guard_actions WHERE event_key='normal'")
	for i := 0; i < 2; i++ {
		check(a.processGuardAction(ctx))
	}
	count(3, "SELECT count(*) FROM group_guard_actions WHERE event_key='ad' AND status='done'")
	requestJSON(t, client, "GET", path+"/guard/actions", nil, 200)
}
