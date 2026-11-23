package app

import (
	"bytes"
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
	"sync"
	"testing"
	"time"

	"ai-bot/backend/internal/config"
)

// agentMock fakes the model API, a new-api site and the QQ OpenAPI.
type agentMock struct {
	*httptest.Server
	mu        sync.Mutex
	mainCalls [][]byte
	auxKinds  []string
	qqSends   []map[string]any
}

func (m *agentMock) serve(w http.ResponseWriter, r *http.Request) {
	switch {
	case r.URL.Path == "/v1/embeddings":
		var req struct {
			Input []string `json:"input"`
		}
		_ = json.NewDecoder(r.Body).Decode(&req)
		data := make([]map[string]any, len(req.Input))
		for i := range req.Input {
			data[i] = map[string]any{"index": i, "embedding": []float32{1, 0, 0}}
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"data": data})
	case r.URL.Path == "/v1/chat/completions":
		body, _ := io.ReadAll(r.Body)
		m.handleChat(w, body)
	case r.URL.Path == "/api/notice":
		w.Write([]byte(`{"success":true,"data":"今晚 23:00 维护 10 分钟"}`))
	case r.URL.Path == "/api/status":
		w.Write([]byte(`{"success":true,"data":{"system_name":"Cooper","announcements_enabled":true,"announcements":[{"content":"新上线 gpt-4o 模型","publishDate":"2026-11-20T08:00:00Z"}]}}`))
	case r.URL.Path == "/api/uptime/status":
		w.Write([]byte(`{"success":true,"data":[{"categoryName":"分组","monitors":[{"name":"default","status":1,"uptime":1},{"name":"vip","status":0,"uptime":0.7}]}]}`))
	case r.URL.Path == "/api/pricing":
		w.Write([]byte(`{"success":true,"data":[{"model_name":"gpt-4o","quota_type":0,"model_ratio":1.25,"completion_ratio":4,"enable_groups":["default","vip"]}],"group_ratio":{"default":1,"vip":0.8},"usable_group":{"default":"默认","vip":"VIP"}}`))
	case r.URL.Path == "/token":
		_ = json.NewEncoder(w).Encode(map[string]any{"access_token": "tok", "expires_in": "7200"})
	case strings.HasSuffix(r.URL.Path, "/messages") && r.Method == http.MethodPost:
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		m.mu.Lock()
		m.qqSends = append(m.qqSends, body)
		id := fmt.Sprintf("qq-out-%d", len(m.qqSends))
		m.mu.Unlock()
		_ = json.NewEncoder(w).Encode(map[string]any{"id": id})
	case r.Method == http.MethodDelete:
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{}`))
	default:
		http.NotFound(w, r)
	}
}

func (m *agentMock) handleChat(w http.ResponseWriter, body []byte) {
	var req struct {
		Messages []struct {
			Role    string `json:"role"`
			Content any    `json:"content"`
		} `json:"messages"`
	}
	_ = json.Unmarshal(body, &req)
	reply := func(content string, calls []map[string]any) {
		msg := map[string]any{"role": "assistant", "content": content}
		if len(calls) > 0 {
			msg["tool_calls"] = calls
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"choices": []any{map[string]any{"message": msg}}, "usage": map[string]any{"prompt_tokens": 10, "completion_tokens": 5}})
	}
	aux := func(kind string) {
		m.mu.Lock()
		m.auxKinds = append(m.auxKinds, kind)
		m.mu.Unlock()
	}
	switch {
	case bytes.Contains(body, []byte("可以独立理解")):
		aux("rewrite")
		reply("gpt-4o 在 vip 分组的价格是多少", nil)
		return
	case bytes.Contains(body, []byte("客服质检")):
		aux("evaluate")
		reply("```json\n{\"answered\":true,\"knowledge_gap\":\"\",\"previous_feedback\":\"none\",\"too_long\":false,\"memories\":[{\"kind\":\"fact\",\"content\":\"用户使用 vip 分组\",\"confidence\":0.9}]}\n```", nil)
		return
	case bytes.Contains(body, []byte("最后一条是官方客服的发言")):
		aux("staff")
		reply(`{"reusable":true,"question":"充值多久到账","aliases":["充值没到账"],"answer":"一般 5 分钟内到账，超过请联系客服","confidence":0.9}`, nil)
		return
	case bytes.Contains(body, []byte("客服回答评审")):
		aux("judge")
		reply(`{"score":8,"verdict":"要点正确"}`, nil)
		return
	case bytes.Contains(body, []byte("维护客服对话的滚动摘要")):
		aux("summary")
		reply("用户此前咨询过价格", nil)
		return
	}
	m.mu.Lock()
	m.mainCalls = append(m.mainCalls, body)
	m.mu.Unlock()
	lastUser, hasTool := "", false
	for _, msg := range req.Messages {
		if msg.Role == "tool" {
			hasTool = true
		}
		if msg.Role == "user" {
			lastUser = fmt.Sprint(msg.Content)
		}
	}
	switch {
	case hasTool && strings.Contains(lastUser, "人工"):
		reply("好的，已经帮你叫人工客服了，稍等～", nil)
	case hasTool:
		reply("## 价格\ngpt-4o 在 vip 分组输入每百万 **2 美元**。\n\n输出每百万 8 美元，以官网为准。\n\n希望以上信息对您有帮助！", nil)
	case strings.Contains(lastUser, "价格"):
		reply("", []map[string]any{{"id": "call_price", "type": "function", "function": map[string]any{"name": "get_model_price", "arguments": `{"model":"4o","group":"vip"}`}}})
	case strings.Contains(lastUser, "人工"):
		reply("", []map[string]any{{"id": "call_handoff", "type": "function", "function": map[string]any{"name": "handoff_to_human", "arguments": `{"reason":"用户要求人工"}`}}})
	default:
		reply("在的～", nil)
	}
}

func (m *agentMock) MainCalls() [][]byte {
	m.mu.Lock()
	defer m.mu.Unlock()
	return append([][]byte(nil), m.mainCalls...)
}

func (m *agentMock) Sends() []map[string]any {
	m.mu.Lock()
	defer m.mu.Unlock()
	return append([]map[string]any(nil), m.qqSends...)
}

func groupEventBody(t *testing.T, eventID, eventType, messageID, memberID, name, content string) []byte {
	t.Helper()
	body, _ := json.Marshal(map[string]any{
		"id": eventID, "op": 0, "t": eventType,
		"d": map[string]any{
			"id": messageID, "group_openid": "agent-group", "content": content,
			"timestamp": time.Now().UTC().Format(time.RFC3339),
			"author":    map[string]any{"member_openid": memberID, "username": name},
		},
	})
	return body
}

func TestAgentToolsMemoryAndLearning(t *testing.T) {
	databaseURL := os.Getenv("TEST_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("TEST_DATABASE_URL is not configured")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	pool := newIntegrationSchema(t, ctx, databaseURL)
	mock := &agentMock{}
	mock.Server = httptest.NewServer(http.HandlerFunc(mock.serve))
	defer mock.Close()

	application, err := New(ctx, config.Config{
		DataDir: t.TempDir(), MasterKey: []byte("0123456789abcdef0123456789abcdef"),
		AdminUsername: "admin", AdminPassword: "integration-password", WorkerPoll: 10 * time.Millisecond,
		AIRequestTimeout: 10 * time.Second, QQAPIBaseURL: mock.URL, QQTokenURL: mock.URL + "/token",
	}, pool, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatal(err)
	}
	workerCtx, stopWorkers := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { defer close(done); application.RunWorkers(workerCtx) }()
	defer func() { stopWorkers(); <-done }()
	server := httptest.NewServer(application.Router())
	defer server.Close()
	jar, _ := cookiejar.New(nil)
	client := &http.Client{Jar: jar, Timeout: 10 * time.Second}
	requestJSON(t, client, http.MethodPost, server.URL+"/api/auth/login", map[string]any{"username": "admin", "password": "integration-password"}, http.StatusOK)

	chatID := createdID(t, requestJSON(t, client, http.MethodPost, server.URL+"/api/model-profiles", map[string]any{
		"name": "chat", "kind": "chat", "baseUrl": mock.URL + "/v1", "apiKey": "k", "model": "mock-chat", "enabled": true, "isDefault": true,
	}, http.StatusOK))
	embID := createdID(t, requestJSON(t, client, http.MethodPost, server.URL+"/api/model-profiles", map[string]any{
		"name": "emb", "kind": "embedding", "baseUrl": mock.URL + "/v1", "apiKey": "k", "model": "mock-emb", "dimension": 3, "enabled": true, "isDefault": true,
	}, http.StatusOK))
	const botSecret = "agent-bot-secret"
	botID := createdID(t, requestJSON(t, client, http.MethodPost, server.URL+"/api/bots", map[string]any{
		"name": "agent bot", "appId": "app", "appSecret": botSecret, "modelProfileId": chatID, "enabled": true,
	}, http.StatusCreated))
	kbID := createdID(t, requestJSON(t, client, http.MethodPost, server.URL+"/api/knowledge-bases", map[string]any{
		"name": "FAQ", "embeddingProfileId": embID,
	}, http.StatusCreated))

	settings := dataMap(t, requestJSON(t, client, http.MethodGet, server.URL+"/api/agent/settings", nil, http.StatusOK))
	settings["groupReplyChars"] = 30
	settings["learningKnowledgeBaseId"] = kbID
	live := settings["liveData"].(map[string]any)
	live["enabled"] = true
	live["baseUrl"] = mock.URL
	live["announcementKnowledgeBaseId"] = kbID
	settings["liveDataAccessToken"] = "site-token-secret"
	saved := requestJSON(t, client, http.MethodPut, server.URL+"/api/agent/settings", settings, http.StatusOK)
	assertSecretNotReturned(t, saved, "site-token-secret")
	if !bytes.Contains(saved, []byte(`"hasAccessToken":true`)) {
		t.Fatalf("token flag missing: %s", saved)
	}

	liveJSON := requestJSON(t, client, http.MethodPost, server.URL+"/api/agent/live-data/refresh", nil, http.StatusOK)
	if !bytes.Contains(liveJSON, []byte(`"modelCount":1`)) || !bytes.Contains(liveJSON, []byte("vip：异常")) {
		t.Fatalf("live data: %s", liveJSON)
	}
	eventually(t, 8*time.Second, "announcements synced and indexed", func() (bool, error) {
		var n int
		err := pool.QueryRow(ctx, `SELECT count(*) FROM knowledge_entries WHERE source='announcement' AND index_status='ready'`).Scan(&n)
		return n == 2, err
	})

	hook := server.URL + "/callbacks/qq/" + botID
	postQQWebhook(t, client, hook, botSecret, groupEventBody(t, "e1", "GROUP_MESSAGE_CREATE", "m1", "alice", "Alice", "我平时用的是 vip 分组"))
	postQQWebhook(t, client, hook, botSecret, groupEventBody(t, "e2", "GROUP_MESSAGE_CREATE", "m2", "bob", "Bob", "BOB_SECRET 无关闲聊"))
	var convID string
	if err := pool.QueryRow(ctx, `SELECT id::text FROM conversations WHERE platform_id='agent-group'`).Scan(&convID); err != nil {
		t.Fatal(err)
	}
	requestJSON(t, client, http.MethodPut, server.URL+"/api/conversations/"+convID, map[string]any{
		"name": "群", "triggerMode": "mention_only", "systemPrompt": "你是客服。", "chatProfileId": chatID, "enabled": true, "contextLimit": 20, "knowledgeBaseIds": []string{kbID},
	}, http.StatusOK)

	postQQWebhook(t, client, hook, botSecret, groupEventBody(t, "e3", "GROUP_AT_MESSAGE_CREATE", "m3", "alice", "Alice", "那价格多少"))
	eventually(t, 10*time.Second, "split price reply delivered", func() (bool, error) {
		var n int
		err := pool.QueryRow(ctx, `SELECT count(*) FROM outbox_tasks t JOIN messages m ON m.id=t.message_id WHERE m.reply_to_message_id='m3' AND m.event_type='AI_RESPONSE' AND t.status='sent'`).Scan(&n)
		return n >= 2, err
	})
	calls := mock.MainCalls()
	if len(calls) != 2 {
		t.Fatalf("expected tool round trip (2 main calls), got %d", len(calls))
	}
	first := string(calls[0])
	if !strings.Contains(first, "vip 分组") || strings.Contains(first, "BOB_SECRET") || !strings.Contains(first, "get_model_price") || !strings.Contains(first, "vip：异常") {
		t.Fatalf("thread context / tools / live snapshot wrong: %s", first)
	}
	if !strings.Contains(string(calls[1]), `inputUsdPerMillionTokens\":2`) {
		t.Fatalf("tool result not passed back: %s", calls[1])
	}
	rows, _ := pool.Query(ctx, `SELECT m.content,t.msg_seq FROM outbox_tasks t JOIN messages m ON m.id=t.message_id WHERE m.reply_to_message_id='m3' AND m.event_type='AI_RESPONSE' ORDER BY t.msg_seq`)
	var parts []string
	expectSeq := 2
	for rows.Next() {
		var content string
		var seq int
		_ = rows.Scan(&content, &seq)
		if seq != expectSeq {
			t.Fatalf("unexpected msg_seq %d", seq)
		}
		expectSeq++
		parts = append(parts, content)
	}
	rows.Close()
	joined := strings.Join(parts, "|")
	if strings.Contains(joined, "##") || strings.Contains(joined, "**") || strings.Contains(joined, "希望以上") || !strings.Contains(joined, "8 美元") {
		t.Fatalf("reply not styled: %q", joined)
	}
	var rewritten string
	var toolCalls int
	_ = pool.QueryRow(ctx, `SELECT r.rewritten_query,(SELECT count(*) FROM tool_calls WHERE agent_run_id=r.id) FROM agent_runs r JOIN messages m ON m.id=r.message_id WHERE m.platform_message_id='m3'`).Scan(&rewritten, &toolCalls)
	if rewritten == "" || toolCalls != 1 {
		t.Fatalf("rewrite=%q toolCalls=%d", rewritten, toolCalls)
	}
	eventually(t, 8*time.Second, "memory extracted", func() (bool, error) {
		var n int
		err := pool.QueryRow(ctx, `SELECT count(*) FROM user_memories WHERE platform_user_id='alice' AND content='用户使用 vip 分组'`).Scan(&n)
		return n == 1, err
	})
	memJSON := requestJSON(t, client, http.MethodGet, server.URL+"/api/memories?userId=alice", nil, http.StatusOK)
	if !bytes.Contains(memJSON, []byte("用户使用 vip 分组")) {
		t.Fatalf("memory API: %s", memJSON)
	}

	// Staff answers become FAQ candidates.
	requestJSON(t, client, http.MethodPut, server.URL+"/api/conversations/"+convID+"/members/bob", map[string]any{"isStaff": true}, http.StatusOK)
	postQQWebhook(t, client, hook, botSecret, groupEventBody(t, "e4", "GROUP_MESSAGE_CREATE", "m4", "carol", "Carol", "充值了怎么还没到账"))
	postQQWebhook(t, client, hook, botSecret, groupEventBody(t, "e5", "GROUP_MESSAGE_CREATE", "m5", "bob", "Bob", "充值一般 5 分钟内到账，超过了私聊我"))
	var candidateID string
	eventually(t, 8*time.Second, "staff candidate created", func() (bool, error) {
		err := pool.QueryRow(ctx, `SELECT id::text FROM knowledge_candidates WHERE source='staff' AND status='pending'`).Scan(&candidateID)
		return candidateID != "", nilIfNoRows(err)
	})
	approved := requestJSON(t, client, http.MethodPost, server.URL+"/api/knowledge/candidates/"+candidateID+"/approve", map[string]any{
		"knowledgeBaseId": kbID, "question": "充值多久到账", "aliases": []string{"充值没到账"}, "answer": "一般 5 分钟内到账",
	}, http.StatusOK)
	entryID, _ := dataMap(t, approved)["entryId"].(string)
	eventually(t, 8*time.Second, "approved entry indexed", func() (bool, error) {
		var status string
		err := pool.QueryRow(ctx, `SELECT index_status FROM knowledge_entries WHERE id=$1`, entryID).Scan(&status)
		return status == "ready", err
	})

	// Handoff pauses the bot for that user.
	postQQWebhook(t, client, hook, botSecret, groupEventBody(t, "e6", "GROUP_AT_MESSAGE_CREATE", "m6", "alice", "Alice", "我要人工"))
	eventually(t, 8*time.Second, "handoff created", func() (bool, error) {
		var n int
		err := pool.QueryRow(ctx, `SELECT count(*) FROM handoffs WHERE platform_user_id='alice' AND status='open'`).Scan(&n)
		return n == 1, err
	})
	before := len(mock.MainCalls())
	postQQWebhook(t, client, hook, botSecret, groupEventBody(t, "e7", "GROUP_AT_MESSAGE_CREATE", "m7", "alice", "Alice", "在吗"))
	eventually(t, 8*time.Second, "paused user skipped", func() (bool, error) {
		var status string
		err := pool.QueryRow(ctx, `SELECT t.status FROM inbox_tasks t JOIN messages m ON m.id=t.message_id WHERE m.platform_message_id='m7'`).Scan(&status)
		return status == "handoff", nilIfNoRows(err)
	})
	if len(mock.MainCalls()) != before {
		t.Fatal("paused user should not reach the model")
	}
	var m7 string
	_ = pool.QueryRow(ctx, `SELECT id::text FROM messages WHERE platform_message_id='m7'`).Scan(&m7)
	requestJSON(t, client, http.MethodPost, server.URL+"/api/messages/"+m7+"/reply", map[string]any{"text": "您好，我是人工客服", "learn": false}, http.StatusOK)
	eventually(t, 8*time.Second, "human reply sent", func() (bool, error) {
		for _, s := range mock.Sends() {
			if s["content"] == "您好，我是人工客服" && s["msg_id"] == "m7" && s["msg_type"] == float64(0) {
				return true, nil
			}
		}
		return false, nil
	})
	handoffs := dataSlice(t, requestJSON(t, client, http.MethodGet, server.URL+"/api/handoffs", nil, http.StatusOK))
	hid, _ := object(t, handoffs[0])["id"].(string)
	requestJSON(t, client, http.MethodPost, server.URL+"/api/handoffs/"+hid+"/resolve", nil, http.StatusOK)

	// Admin correction published directly.
	var m3 string
	_ = pool.QueryRow(ctx, `SELECT id::text FROM messages WHERE platform_message_id='m3'`).Scan(&m3)
	fb := dataMap(t, requestJSON(t, client, http.MethodPost, server.URL+"/api/messages/"+m3+"/feedback", map[string]any{
		"rating": "wrong", "correctedAnswer": "vip 分组 gpt-4o 输入 2 美元/百万 tokens", "publish": true, "knowledgeBaseId": kbID,
	}, http.StatusOK))
	if fb["entryId"] == nil {
		t.Fatalf("correction not published: %v", fb)
	}
	detail := requestJSON(t, client, http.MethodGet, server.URL+"/api/messages/"+m3, nil, http.StatusOK)
	if !bytes.Contains(detail, []byte(`"steps":[`)) || !bytes.Contains(detail, []byte(`get_model_price`)) || !bytes.Contains(detail, []byte(`"rating":"wrong"`)) {
		t.Fatalf("message detail missing agent info: %s", detail)
	}
	stats := dataMap(t, requestJSON(t, client, http.MethodGet, server.URL+"/api/learning/stats", nil, http.StatusOK))
	if stats["publishedEntries"].(float64) < 4 {
		t.Fatalf("stats: %v", stats)
	}

	// Evaluation run in dry-run mode.
	requestJSON(t, client, http.MethodPost, server.URL+"/api/eval/cases", map[string]any{"question": "gpt-4o 价格", "expected": "2 美元", "enabled": true}, http.StatusOK)
	runID := createdID(t, requestJSON(t, client, http.MethodPost, server.URL+"/api/eval/runs", map[string]any{"chatProfileId": chatID, "knowledgeBaseIds": []string{kbID}}, http.StatusCreated))
	eventually(t, 10*time.Second, "eval run completed", func() (bool, error) {
		var status string
		err := pool.QueryRow(ctx, `SELECT status FROM eval_runs WHERE id=$1`, runID).Scan(&status)
		return status == "completed", err
	})
	results := requestJSON(t, client, http.MethodGet, server.URL+"/api/eval/runs/"+runID+"/results", nil, http.StatusOK)
	if !bytes.Contains(results, []byte("get_model_price")) {
		t.Fatalf("eval results: %s", results)
	}
}

func nilIfNoRows(err error) error {
	if err != nil && strings.Contains(err.Error(), "no rows") {
		return nil
	}
	return err
}
