package app

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"ai-bot/backend/internal/agent"
	"ai-bot/backend/internal/config"
	"ai-bot/backend/internal/db"
)

func TestCognitiveMemoryCrossBotAndCompanionPipeline(t *testing.T) {
	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("TEST_DATABASE_URL is not set")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	pool := newIntegrationSchema(t, ctx, dsn)

	// 1. Migration idempotency check.
	if err := db.Migrate(ctx, pool); err != nil {
		t.Fatalf("second migrate failed: %v", err)
	}

	var mu sync.Mutex
	var chatPayloads [][]byte
	var typingCalls int
	mock := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/token":
			_ = json.NewEncoder(w).Encode(map[string]any{"access_token": "tok", "expires_in": "7200"})
		case strings.HasSuffix(r.URL.Path, "/messages") && r.Method == http.MethodPost:
			var body map[string]any
			_ = json.NewDecoder(r.Body).Decode(&body)
			if body["msg_type"] == float64(6) {
				mu.Lock()
				typingCalls++
				mu.Unlock()
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"id": "qq-msg-1"})
		case r.URL.Path == "/v1/chat/completions":
			body, _ := io.ReadAll(r.Body)
			reply := func(content string) {
				_ = json.NewEncoder(w).Encode(map[string]any{
					"choices": []any{map[string]any{"message": map[string]any{"role": "assistant", "content": content}}},
					"usage":   map[string]any{"prompt_tokens": 20, "completion_tokens": 10},
				})
			}
			switch {
			case bytes.Contains(body, []byte("判断这条群消息")):
				if bytes.Contains(body, []byte("今天好累啊")) {
					reply(`{"reply":true,"isChimeIn":true,"replyMode":"empathy","socialOnly":true,"userMood":"疲惫","moodIntensity":0.7,"valence":-0.3,"memoryFocus":"episodes","topicHint":"群友吐槽累"}`)
				} else if bytes.Contains(body, []byte("随便聊聊")) {
					reply(`{"reply":true,"isChimeIn":true,"replyMode":"banter","socialOnly":true,"userMood":"平静","moodIntensity":0.2,"valence":0.1,"memoryFocus":"episodes","topicHint":"闲聊"}`)
				} else {
					reply(`{"reply":true,"isChimeIn":false,"replyMode":"help","socialOnly":false,"userMood":"平静"}`)
				}
			case bytes.Contains(body, []byte("对话情绪与社交意图前置感知器")):
				reply(`{"replyMode":"empathy","socialOnly":true,"userMood":"愤怒","moodIntensity":0.9,"valence":-0.85,"sarcasm":true,"needEmpathyFirst":true,"memoryFocus":"episodes","topicHint":"非常生气","suggestHandoff":false}`)
			case bytes.Contains(body, []byte("客服质检")):
				reply(`{"answered":true,"knowledge_gap":"","previous_feedback":"none","too_long":false,"mood_delta":"positive","memories":[{"action":"add","kind":"episode","content":"一起吐槽过通宵写代码","importance":8,"emotionTag":"共鸣","confidence":0.95}],"proactive_candidate":{"kind":"event_care","topic":"明天要项目上线","delayHours":4}}`)
			case bytes.Contains(body, []byte("记忆巩固与画像反思")):
				reply(`{"profile":"常熬夜写 Go 的老群友，熟络爱开玩笑","nicknames":["老林"],"archive_ids":[],"merged_memories":[{"kind":"preference","content":"喜欢直来直去的回复","importance":7,"confidence":0.9}],"persona":{"communication":{"directness":{"score":85,"confidence":0.8,"evidence":"说话很直接"}}}}`)
			case bytes.Contains(body, []byte("社群群聊记忆整理")):
				reply(`{"group_vibe":"大家都在熬夜赶项目，互相打气","memories":[{"kind":"topic","content":"群里刚讨论完新版本发布","importance":7,"ttlDays":7}]}`)
			case bytes.Contains(body, []byte("防打扰自检器")):
				reply(`{"shouldSend":true,"reason":"时间合适且话题自然","draft":"昨天你说今天项目上线，顺利不？"}`)
			default:
				mu.Lock()
				chatPayloads = append(chatPayloads, body)
				mu.Unlock()
				reply("抱抱你，先歇口气 ||| 我懂这种烦躁，有事随时说")
			}
		default:
			http.NotFound(w, r)
		}
	}))
	defer mock.Close()

	cfg := config.Config{
		DataDir: t.TempDir(), MasterKey: []byte("0123456789abcdef0123456789abcdef"),
		AdminUsername: "admin", AdminPassword: "test-password",
		QQAPIBaseURL: mock.URL, QQTokenURL: mock.URL + "/token",
		WorkerPoll: 10 * time.Millisecond, AIRequestTimeout: 10 * time.Second,
	}
	a, err := New(ctx, cfg, pool, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatal(err)
	}

	server := httptest.NewServer(a.Router())
	defer server.Close()
	jar, _ := cookiejar.New(nil)
	client := &http.Client{Jar: jar}
	requestJSON(t, client, http.MethodPost, server.URL+"/api/auth/login", map[string]string{"username": "admin", "password": "test-password"}, 200)

	var botA, botB, modelID, convGroupA, convDmB string
	secret, _ := a.cipher.Encrypt("qq-secret")
	check := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}
	check(pool.QueryRow(ctx, `INSERT INTO model_profiles(name,kind,base_url,api_key_enc,model,enabled,is_default) VALUES('chat','chat',$1,$2,'gpt-mock',true,true) RETURNING id::text`, mock.URL+"/v1", secret).Scan(&modelID))
	check(pool.QueryRow(ctx, `INSERT INTO bots(name,app_id,app_secret_enc,enabled) VALUES('BotA','appA',$1,true) RETURNING id::text`, secret).Scan(&botA))
	check(pool.QueryRow(ctx, `INSERT INTO bots(name,app_id,app_secret_enc,enabled) VALUES('BotB','appB',$1,true) RETURNING id::text`, secret).Scan(&botB))
	check(pool.QueryRow(ctx, `INSERT INTO conversations(channel,bot_id,platform_id,type,trigger_mode,enabled) VALUES('qq',$1,'grp-a','group','smart',true) RETURNING id::text`, botA).Scan(&convGroupA))
	check(pool.QueryRow(ctx, `INSERT INTO conversations(channel,bot_id,platform_id,type,trigger_mode,enabled) VALUES('qq',$1,'usr-b','private','always',true) RETURNING id::text`, botB).Scan(&convDmB))

	// 2. Cross-bot sharing via qq_union_id and automatic merge when union_openid arrives later.
	s := agent.DefaultSettings()
	s.MaxMemories = 1
	s.Companion.TypingIndicator = true
	check(a.saveMemory(ctx, s, botA, convGroupA, "member-a", "林哥", "preference", "喜欢喝冰美式", 0.9, "admin", "", nil, saveMemoryOptions{Importance: 8}))
	check(a.saveMemory(ctx, s, botA, convGroupA, "member-a", "林哥", "fact", "平时用 Go 写后端", 0.8, "admin", "", nil, saveMemoryOptions{Importance: 4}))
	// Separate user in Bot B before union_openid arrives
	check(a.saveMemory(ctx, s, botB, convDmB, "user-b", "林哥", "episode", "上周半夜聊过架构重构", 0.9, "admin", "", nil, saveMemoryOptions{Importance: 9}))

	subA, err := a.resolveSubject(ctx, botA, convGroupA, "group", "member-a", "林哥", "")
	check(err)
	subB, err := a.resolveSubject(ctx, botB, convDmB, "private", "user-b", "林哥", "")
	check(err)
	if subA == "" || subB == "" || subA == subB {
		t.Fatalf("expected two distinct subjects before union link, got %s vs %s", subA, subB)
	}

	// Now Bot A and Bot B both observe the same union_openid -> automatic lossless merge.
	subMergedA, err := a.resolveSubject(ctx, botA, convGroupA, "group", "member-a", "林哥", "union-lin-001")
	check(err)
	subMergedB, err := a.resolveSubject(ctx, botB, convDmB, "private", "user-b", "林哥", "union-lin-001")
	check(err)
	if subMergedA != subMergedB {
		t.Fatalf("expected same subject after union_openid link, got %s vs %s", subMergedA, subMergedB)
	}

	// Verify exact recall access_count increment (only 1 recalled out of 3).
	bundle := a.loadSubjectContext(ctx, s, botB, convDmB, "private", "user-b", "林哥", "", "episodes", &queryEmbeddings{})
	if len(bundle.Memories) != 1 || !strings.Contains(bundle.Memories[0].Content, "架构重构") {
		t.Fatalf("expected top episode memory recalled across bots, got %+v", bundle.Memories)
	}
	var hitSum int
	check(pool.QueryRow(ctx, "SELECT coalesce(sum(access_count),0) FROM user_memories WHERE subject_id=$1 AND status='active'", subMergedA).Scan(&hitSum))
	if hitSum != 1 {
		t.Fatalf("expected exactly 1 memory access_count incremented, got %d", hitSum)
	}

	// 3. Admin manual unlink & merge APIs.
	subDetail := dataMap(t, requestJSON(t, client, http.MethodGet, server.URL+"/api/subjects/"+subMergedA, nil, 200))
	links := subDetail["links"].([]any)
	if len(links) < 3 {
		t.Fatalf("expected >= 3 links on merged subject, got %d", len(links))
	}
	var botBLinkID string
	for _, l := range links {
		lm := l.(map[string]any)
		if lm["identityValue"] == "user-b" {
			botBLinkID = lm["id"].(string)
		}
	}
	unlinked := dataMap(t, requestJSON(t, client, http.MethodPost, server.URL+"/api/subjects/"+subMergedA+"/unlink", map[string]string{"linkId": botBLinkID}, 200))
	splitID := unlinked["newSubjectId"].(string)
	requestJSON(t, client, http.MethodPost, server.URL+"/api/subjects/merge", map[string]string{"primaryId": subMergedA, "secondaryId": splitID}, 200)

	// 4. Persona activation + real-time emotion perception + semantic split + typing + director_notes in DM.
	personaPatch := map[string]any{
		"expertise":     map[string]any{"score": 20, "confidence": 0.9, "evidence": "刚接触接口"},
		"communication": map[string]any{"patience": map[string]any{"score": 25, "confidence": 0.9, "evidence": "回复很急"}},
		"avoid":         []string{"不要说教"},
	}
	pBytes, _ := json.Marshal(personaPatch)
	_, err = pool.Exec(ctx, "UPDATE memory_subjects SET persona=$2::jsonb WHERE id=$1", subMergedA, string(pBytes))
	check(err)

	var dmMsgID string
	check(pool.QueryRow(ctx, `INSERT INTO messages(channel,bot_id,conversation_id,direction,sender_id,sender_name,platform_message_id,event_type,content,event_at,reply_deadline)
		VALUES('qq',$1,$2,'inbound','user-b','林哥','dm-m1','C2C_MESSAGE_CREATE','真是谢谢你们了，又崩了',now(),now()+interval '5 minutes') RETURNING id::text`, botB, convDmB).Scan(&dmMsgID))
	_, err = pool.Exec(ctx, "INSERT INTO inbox_tasks(message_id) VALUES($1)", dmMsgID)
	check(err)
	// Enable TypingIndicator globally for this test to verify msg_type=6 typing notification.
	requestJSON(t, client, http.MethodPut, server.URL+"/api/agent/settings", map[string]any{"companion": map[string]any{"typingIndicator": true}}, 200)
	check(a.processInbox(ctx))

	// Check agent_runs persisted perception, recalled_memories, director_notes, and main prompt received persona hints.
	var perceptionRaw, recalledRaw []byte
	var directorNotes string
	check(pool.QueryRow(ctx, "SELECT perception,recalled_memories,director_notes FROM agent_runs WHERE message_id=$1", dmMsgID).Scan(&perceptionRaw, &recalledRaw, &directorNotes))
	if !strings.Contains(string(perceptionRaw), "愤怒") || !strings.Contains(directorNotes, "偏新手") || !strings.Contains(directorNotes, "不要说教") || !strings.Contains(directorNotes, "禁止使用 emoji") {
		t.Fatalf("unexpected director/perception persistence: perception=%s notes=%s", perceptionRaw, directorNotes)
	}
	mu.Lock()
	lastMain := string(chatPayloads[len(chatPayloads)-1])
	mu.Unlock()
	if !strings.Contains(lastMain, "情绪优先") || !strings.Contains(lastMain, "不要说教") {
		t.Fatalf("persona & emotion guidance missing from main chat prompt: %s", lastMain)
	}
	// Semantic split produced 2 outbound messages with dynamic delay.
	var outCount int
	check(pool.QueryRow(ctx, "SELECT count(*) FROM messages WHERE conversation_id=$1 AND direction='outbound' AND event_type='AI_RESPONSE'", convDmB).Scan(&outCount))
	if outCount != 2 {
		t.Fatalf("expected 2 semantically split replies, got %d", outCount)
	}

	// 5. Fast-path evaluate + slow-path reflection + group digest + proactive trigger.
	job, err := a.claimAgentJob(ctx, []string{"eval_run", "group_review"}, true)
	check(err)
	check(a.runJob(ctx, job))

	var turns int
	check(pool.QueryRow(ctx, "SELECT interaction_turns FROM memory_subjects WHERE id=$1", subMergedA).Scan(&turns))
	if turns < 1 {
		t.Fatalf("expected interaction_turns incremented, got %d", turns)
	}

	// Trigger reflection directly via API.
	requestJSON(t, client, http.MethodPost, server.URL+"/api/subjects/"+subMergedA+"/reflect", nil, 200)
	reflected := dataMap(t, requestJSON(t, client, http.MethodGet, server.URL+"/api/subjects/"+subMergedA, nil, 200))
	if !strings.Contains(reflected["profileContent"].(string), "老群友") {
		t.Fatalf("expected reflected profile updated, got %v", reflected["profileContent"])
	}

	// Group chime-in + cooldown guard.
	var gMsg1, gMsg2 string
	check(pool.QueryRow(ctx, `INSERT INTO messages(channel,bot_id,conversation_id,direction,sender_id,sender_name,platform_message_id,event_type,content,event_at,reply_deadline)
		VALUES('qq',$1,$2,'inbound','member-a','林哥','gm1','GROUP_MESSAGE_CREATE','今天好累啊',now(),now()+interval '5 minutes') RETURNING id::text`, botA, convGroupA).Scan(&gMsg1))
	_, err = pool.Exec(ctx, "INSERT INTO inbox_tasks(message_id) VALUES($1)", gMsg1)
	check(err)
	check(a.processInbox(ctx))

	var chimeAt *time.Time
	check(pool.QueryRow(ctx, "SELECT last_chime_in_at FROM conversations WHERE id=$1", convGroupA).Scan(&chimeAt))
	if chimeAt == nil {
		t.Fatal("expected last_chime_in_at recorded after chime-in reply")
	}

	// Second casual chime-in within cooldown should be blocked to skipped.
	check(pool.QueryRow(ctx, `INSERT INTO messages(channel,bot_id,conversation_id,direction,sender_id,sender_name,platform_message_id,event_type,content,event_at,reply_deadline)
		VALUES('qq',$1,$2,'inbound','member-a','林哥','gm2','GROUP_MESSAGE_CREATE','随便聊聊',now()+interval '1 second',now()+interval '5 minutes') RETURNING id::text`, botA, convGroupA).Scan(&gMsg2))
	_, err = pool.Exec(ctx, "INSERT INTO inbox_tasks(message_id) VALUES($1)", gMsg2)
	check(err)
	check(a.processInbox(ctx))
	var gm2Status string
	check(pool.QueryRow(ctx, "SELECT status FROM inbox_tasks WHERE message_id=$1", gMsg2).Scan(&gm2Status))
	if gm2Status != "skipped" {
		t.Fatalf("expected cooldown to skip second chime-in, got %s", gm2Status)
	}

	// Group digest API.
	requestJSON(t, client, http.MethodPost, server.URL+"/api/conversations/"+convGroupA+"/group-memories/digest", nil, 200)
	gmResp := dataMap(t, requestJSON(t, client, http.MethodGet, server.URL+"/api/conversations/"+convGroupA+"/group-memories", nil, 200))
	if len(gmResp["items"].([]any)) == 0 || gmResp["groupVibe"] == "" {
		t.Fatalf("expected group memories and vibe, got %v", gmResp)
	}

	// Proactive care trigger execution (make scheduled_at due now while keeping created_at after dmMsgID).
	var trigID string
	check(pool.QueryRow(ctx, "SELECT id::text FROM proactive_triggers WHERE subject_id=$1 AND status='pending' LIMIT 1", subMergedA).Scan(&trigID))
	_, err = pool.Exec(ctx, "UPDATE proactive_triggers SET scheduled_at=now()-interval '1 minute' WHERE id=$1", trigID)
	check(err)
	check(a.jobProactiveSend(ctx, trigID))
	var trigStatus string
	check(pool.QueryRow(ctx, "SELECT status FROM proactive_triggers WHERE id=$1", trigID).Scan(&trigStatus))
	if trigStatus != "sent" {
		t.Fatalf("expected proactive trigger sent, got %s", trigStatus)
	}

	// Wait briefly for best-effort async typing goroutine so we verify typingCalls >= 1.
	time.Sleep(80 * time.Millisecond)
	mu.Lock()
	tc := typingCalls
	mu.Unlock()
	if tc == 0 {
		t.Fatal("expected at least 1 private typing notification when TypingIndicator=true")
	}
}
