package app

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"math"
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
	case r.URL.Path == "/v1/responses":
		var req map[string]any
		_ = json.NewDecoder(r.Body).Decode(&req)
		content := "查证后的测试资讯"
		output := []any{}
		if req["tool_choice"] == "required" {
			output = append(output, map[string]any{"type": "web_search_call", "status": "completed"})
		} else {
			content = "<html><body><h1>AI 资讯</h1><p>查证后的测试资讯</p></body></html>"
		}
		output = append(output, map[string]any{"type": "message", "content": []any{map[string]any{"type": "output_text", "text": content}}})
		_ = json.NewEncoder(w).Encode(map[string]any{"output": output})
	case strings.HasSuffix(r.URL.Path, "/files"):
		var payload struct {
			Data []byte `json:"file_data"`
		}
		_ = json.NewDecoder(r.Body).Decode(&payload)
		if !bytes.HasPrefix(payload.Data, []byte{137, 80, 78, 71}) {
			http.Error(w, "expected PNG", 400)
			return
		}
		w.Write([]byte(`{"file_info":"screenshot"}`))
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
		_ = json.NewEncoder(w).Encode(map[string]any{"choices": []any{map[string]any{"message": msg}}, "usage": map[string]any{"prompt_tokens": 10, "completion_tokens": 5, "prompt_tokens_details": map[string]int{"cached_tokens": 2, "cache_write_tokens": 1}}})
	}
	aux := func(kind string) {
		m.mu.Lock()
		m.auxKinds = append(m.auxKinds, kind)
		m.mu.Unlock()
	}
	switch {
	case bytes.Contains(body, []byte("判断这条群消息")):
		aux("reply_decision")
		var input struct {
			Current string         `json:"当前消息"`
			History []decisionTurn `json:"近期群聊"`
		}
		raw, _ := req.Messages[len(req.Messages)-1].Content.(string)
		_ = json.Unmarshal([]byte(raw), &input)
		var aliceAskedAboutVIP bool
		for _, prior := range input.History {
			if prior.SameSender && prior.Content == "我平时用的是 vip 分组" {
				aliceAskedAboutVIP = true
			}
		}
		if (strings.Contains(input.Current, "那价格多少") && aliceAskedAboutVIP) || strings.Contains(input.Current, "发成网页截图") || strings.Contains(input.Current, "我要人工") {
			reply(`{"reply":true}`, nil)
		} else {
			reply(`{"reply":false}`, nil)
		}
		return
	case bytes.Contains(body, []byte("分析客服样本的表达风格")):
		aux("staff_style")
		reply("语气自然亲切，先说结论，再补充一步操作。", nil)
		return
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
	case strings.Contains(lastUser, "发成网页截图"):
		reply("", []map[string]any{{"id": "shot", "type": "function", "function": map[string]any{"name": "render_html_image", "arguments": `{"html":"<h1>说明图</h1><p>第一步</p>"}`}}})
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
			"timestamp":     time.Now().UTC().Format(time.RFC3339),
			"message_scene": map[string]any{"ext": []string{"msg_idx=ref-" + messageID}},
			"author":        map[string]any{"member_openid": memberID, "username": name},
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
		"name": "chat", "kind": "chat", "baseUrl": mock.URL + "/v1", "apiKey": "k", "model": "mock-chat", "enabled": true, "isDefault": true, "inputPricePerMillion": 2, "outputPricePerMillion": 4, "cacheReadPricePerMillion": 1, "cacheWritePricePerMillion": 3,
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
	settings["replyQuietSeconds"] = 1
	settings["learningKnowledgeBaseId"] = kbID
	live := settings["liveData"].(map[string]any)
	live["enabled"] = true
	live["baseUrl"] = mock.URL
	// Obsolete client fields cannot re-enable announcement indexing.
	live["announcementKnowledgeBaseId"] = kbID
	live["syncAnnouncements"] = true
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
	var entries int
	if err := pool.QueryRow(ctx, "SELECT count(*) FROM knowledge_entries WHERE source='announcement'").Scan(&entries); err != nil || entries != 0 {
		t.Fatal("manual sync created announcement entries", entries, err)
	}

	hook := server.URL + "/callbacks/qq/" + botID
	postQQWebhook(t, client, hook, botSecret, groupEventBody(t, "e1", "GROUP_MESSAGE_CREATE", "m1", "alice", "Alice", "我平时用的是 vip 分组"))
	postQQWebhook(t, client, hook, botSecret, groupEventBody(t, "e2", "GROUP_MESSAGE_CREATE", "m2", "bob", "Bob", "BOB_SECRET 无关闲聊"))
	var convID string
	if err := pool.QueryRow(ctx, `SELECT id::text FROM conversations WHERE platform_id='agent-group'`).Scan(&convID); err != nil {
		t.Fatal(err)
	}
	create := func(path string, fields map[string]any) string {
		return createdID(t, requestJSON(t, client, http.MethodPost, server.URL+"/api/"+path, fields, http.StatusOK))
	}
	presetID := create("personalities", map[string]any{"name": "测试性格", "prompt": "初始表达习惯"})
	agentID := create("support-agents", map[string]any{"name": "接入顾问", "personalityId": presetID, "instructions": "解答接入流程问题"})
	serviceProfileID := create("model-profiles", map[string]any{"name": "客服专用", "kind": "chat", "baseUrl": mock.URL + "/v1", "apiKey": "k", "model": "service-chat", "enabled": true, "inputPricePerMillion": 2, "outputPricePerMillion": 4, "cacheReadPricePerMillion": 1, "cacheWritePricePerMillion": 3})
	requestJSON(t, client, http.MethodPost, server.URL+"/api/bots/"+botID+"/services", map[string]any{"name": "接入小助手", "agentId": agentID, "chatProfileId": embID}, http.StatusBadRequest)
	requestJSON(t, client, http.MethodPost, server.URL+"/api/bots/"+botID+"/services", map[string]any{"name": "无效等待", "settings": map[string]int{"replyQuietSeconds": 9, "replyMaxWaitSeconds": 4}}, http.StatusBadRequest)
	serviceID := create("bots/"+botID+"/services", map[string]any{"name": "接入小助手", "settings": map[string]int{"replyQuietSeconds": 1, "replyMaxWaitSeconds": 4}, "agentId": agentID, "chatProfileId": serviceProfileID, "decisionProfileId": chatID, "simpleProfileId": chatID})
	otherBotID := createdID(t, requestJSON(t, client, http.MethodPost, server.URL+"/api/bots", map[string]any{"name": "另一个机器人", "appId": "another", "appSecret": botSecret, "enabled": true}, http.StatusCreated))
	otherServiceID := create("bots/"+otherBotID+"/services", map[string]any{"name": "其他客服", "agentId": agentID})
	requestJSON(t, client, http.MethodPut, server.URL+"/api/conversations/"+convID, map[string]any{"triggerMode": "mention_only", "contextLimit": 20, "serviceId": otherServiceID}, http.StatusBadRequest)
	requestJSON(t, client, http.MethodPut, server.URL+"/api/conversations/"+convID, map[string]any{
		"name": "群", "serviceId": serviceID, "triggerMode": "smart", "systemPrompt": "你是客服。", "chatProfileId": chatID, "enabled": true, "contextLimit": 20, "knowledgeBaseIds": []string{kbID},
	}, http.StatusOK)

	requestJSON(t, client, http.MethodPut, server.URL+"/api/personalities/"+presetID, map[string]any{"name": "测试性格", "prompt": "使用更新后的表达习惯"}, http.StatusOK)
	// This scenario exercises a follow-up to an earlier, already judged turn.
	eventually(t, 5*time.Second, "first turn judged", func() (bool, error) {
		var done bool
		err := pool.QueryRow(ctx, `SELECT t.status='skipped' FROM inbox_tasks t JOIN messages m ON m.id=t.message_id WHERE m.platform_message_id='m1'`).Scan(&done)
		return done, err
	})
	postQQWebhook(t, client, hook, botSecret, groupEventBody(t, "e3", "GROUP_MESSAGE_CREATE", "m3", "alice", "Alice", "那价格多少"))
	eventually(t, 10*time.Second, "split price reply delivered", func() (bool, error) {
		var n int
		err := pool.QueryRow(ctx, `SELECT count(*) FROM outbox_tasks t JOIN messages m ON m.id=t.message_id WHERE m.reply_to_message_id='m3' AND m.event_type='AI_RESPONSE' AND t.status='sent'`).Scan(&n)
		return n >= 2, err
	})
	var timing struct {
		Stages []struct {
			Label, Status string
			Ms, ElapsedMs *int64
		}
		TotalMs *int64
	}
	var expectedTotal int64
	err = pool.QueryRow(ctx, `SELECT message_timeline(m),(SELECT round(extract(epoch FROM max(t.updated_at)-m.created_at)*1000)::bigint
 FROM outbox_tasks t JOIN messages r ON r.id=t.message_id WHERE r.conversation_id=m.conversation_id AND r.event_type='AI_RESPONSE' AND r.reply_to_message_id=m.platform_message_id)
 FROM messages m WHERE m.platform_message_id='m3'`).Scan(&timing, &expectedTotal)
	if err != nil || timing.TotalMs == nil || *timing.TotalMs != expectedTotal {
		t.Fatalf("split reply total: got %+v expected %d err %v", timing.TotalMs, expectedTotal, err)
	}
	found := map[string]bool{}
	var previous int64
	for _, stage := range timing.Stages {
		found[stage.Label] = true
		if stage.ElapsedMs != nil {
			if *stage.ElapsedMs < previous || stage.Ms == nil || *stage.Ms > *stage.ElapsedMs {
				t.Fatalf("invalid timing for %+v", stage)
			}
			previous = *stage.ElapsedMs
		}
	}
	for _, label := range []string{"判断是否回复", "模型生成 · 第 1 轮", "模型生成 · 第 2 轮", "回复整理与分段", "投递第 1 段", "投递第 2 段"} {
		if !found[label] {
			t.Errorf("missing stage %q", label)
		}
	}
	checkedSegments := 0
	for _, payload := range mock.Sends() {
		if payload["msg_id"] != "m3" || payload["msg_type"] != float64(2) {
			continue
		}
		firstSegment := payload["msg_seq"] == float64(1)
		markdown := payload["markdown"].(map[string]any)["content"].(string)
		ref, hasRef := payload["message_reference"].(map[string]any)
		if strings.Contains(markdown, "qqbot-at-user") != firstSegment || hasRef != firstSegment || (hasRef && ref["message_id"] != "ref-m3") || strings.Contains(markdown, "回复「") {
			t.Fatalf("unexpected mention or quote on segment %v: %#v", payload["msg_seq"], payload)
		}
		checkedSegments++
	}
	if checkedSegments < 2 {
		t.Fatalf("expected multiple passive QQ replies, got %d", checkedSegments)
	}
	calls := mock.MainCalls()
	if len(calls) != 2 {
		t.Fatalf("expected tool round trip (2 main calls), got %d", len(calls))
	}
	first := string(calls[0])
	for _, want := range []string{`"model":"service-chat"`, "接入小助手", "解答接入流程问题", "使用更新后的表达习惯"} {
		if !strings.Contains(first, want) {
			t.Fatalf("missing selected agent prompt %q", want)
		}
	}
	if !strings.Contains(first, "vip 分组") || !strings.Contains(first, "BOB_SECRET") || !strings.Contains(first, "get_model_price") || !strings.Contains(first, "vip：异常") {
		t.Fatalf("thread context / tools / live snapshot wrong: %s", first)
	}
	if !strings.Contains(string(calls[1]), `inputPerMillionTokens\":14`) {
		t.Fatalf("tool result not passed back: %s", calls[1])
	}
	rows, _ := pool.Query(ctx, `SELECT m.content,t.msg_seq FROM outbox_tasks t JOIN messages m ON m.id=t.message_id WHERE m.reply_to_message_id='m3' AND m.event_type='AI_RESPONSE' ORDER BY t.msg_seq`)
	var parts []string
	expectSeq := 1
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

	// A matching user identifier in a second group must not recall the first group's memory.
	var otherGroup string
	if err := pool.QueryRow(ctx, `INSERT INTO conversations(channel,bot_id,platform_id,type) VALUES('qq',$1,'memory-other','group') RETURNING id::text`, botID).Scan(&otherGroup); err != nil {
		t.Fatal(err)
	}
	memorySettings := application.agentSettings(ctx)
	if got := application.loadMemories(ctx, memorySettings, botID, otherGroup, "alice", "", &queryEmbeddings{}); len(got) != 0 {
		t.Fatalf("cross-group recall: %v", got)
	}
	requestJSON(t, client, http.MethodPost, server.URL+"/api/memories", map[string]any{"botId": botID, "conversationId": otherGroup, "userId": "alice", "content": "用户使用 vip 分组", "pinned": true}, http.StatusCreated)
	var scopes int
	if err := pool.QueryRow(ctx, `SELECT count(DISTINCT conversation_id) FROM user_memories WHERE platform_user_id='alice' AND content='用户使用 vip 分组'`).Scan(&scopes); err != nil || scopes != 2 {
		t.Fatalf("scoped deduplication: %d %v", scopes, err)
	}
	directory := requestJSON(t, client, http.MethodGet, server.URL+"/api/memories/directory", nil, http.StatusOK)
	if !bytes.Contains(directory, []byte(otherGroup)) || !bytes.Contains(directory, []byte(convID)) {
		t.Fatalf("missing groups in memory directory: %s", directory)
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
	requestJSON(t, client, http.MethodPost, server.URL+"/api/conversations/"+convID+"/staff-learning", map[string]any{}, http.StatusOK)
	eventually(t, 8*time.Second, "staff history style learned", func() (bool, error) {
		var n int
		err := pool.QueryRow(ctx, `SELECT sample_count FROM conversation_staff_styles WHERE conversation_id=$1`, convID).Scan(&n)
		return n == 2, nilIfNoRows(err)
	})
	learned := dataMap(t, requestJSON(t, client, http.MethodGet, server.URL+"/api/conversations/"+convID+"/staff-learning", nil, http.StatusOK))
	if learned["sampleCount"] != float64(2) || !strings.Contains(fmt.Sprint(learned["style"]), "先说结论") || !strings.Contains(application.staffStylePrompt(ctx, convID), "先说结论") {
		t.Fatalf("staff learning API: %#v", learned)
	}
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
		err := pool.QueryRow(ctx, `SELECT count(*) FROM handoffs WHERE platform_user_id='alice' AND status='open' AND EXISTS (SELECT 1 FROM messages WHERE reply_to_message_id='m6' AND event_type='AI_RESPONSE')`).Scan(&n)
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
	var ticketID string
	if err := pool.QueryRow(ctx, "SELECT handoff_id::text FROM messages WHERE id=$1", m7).Scan(&ticketID); err != nil {
		t.Fatal(err)
	}
	ticketURL := server.URL + "/api/handoffs/" + ticketID
	requestJSON(t, client, "POST", server.URL+"/api/messages/"+m7+"/reply", map[string]string{"text": "未接单回复"}, 409)
	requestJSON(t, client, "POST", ticketURL+"/claim", nil, 200)
	requestJSON(t, client, http.MethodPost, server.URL+"/api/messages/"+m7+"/reply", map[string]any{"text": "您好，我是人工客服", "learn": false}, http.StatusOK)
	eventually(t, 8*time.Second, "human reply sent", func() (bool, error) {
		for _, s := range mock.Sends() {
			if s["content"] == "【人工客服】您好，我是人工客服" && s["msg_id"] == "m7" && s["msg_type"] == float64(0) {
				return true, nil
			}
		}
		return false, nil
	})
	eventually(t, 3*time.Second, "delivered human reply keeps ticket processing", func() (bool, error) {
		detail := dataMap(t, requestJSON(t, client, http.MethodGet, server.URL+"/api/messages/"+m7, nil, http.StatusOK))
		return detail["status"] == "human_processing" && detail["humanReplyContent"] == "您好，我是人工客服" && detail["humanRepliedAt"] != nil, nil
	})
	requestJSON(t, client, "POST", server.URL+"/api/messages/"+m7+"/reply", map[string]string{"text": "这是同一工单的第二句回复"}, 200)
	requestJSON(t, client, "POST", ticketURL+"/resolve", nil, 200)
	eventually(t, 8*time.Second, "both web replies and closure delivered", func() (bool, error) {
		var valid bool
		err := pool.QueryRow(ctx, `SELECT status='resolved' AND close_method='web' AND resolved_at IS NOT NULL
		AND (SELECT count(*) FROM messages WHERE handoff_id=h.id AND event_type='HUMAN_REPLY' AND status='sent')=2
		AND EXISTS(SELECT FROM messages WHERE handoff_id=h.id AND event_type='AI_RESPONSE' AND reply_to_message_id='m6')
		AND EXISTS(SELECT FROM messages WHERE handoff_id=h.id AND event_type='HANDOFF_UPDATE' AND content LIKE '%已结束%' AND status='sent')
		FROM handoffs h WHERE id=$1`, ticketID).Scan(&valid)
		return valid, err
	})
	requestJSON(t, client, "POST", server.URL+"/api/messages/"+m7+"/reply", map[string]string{"text": "结单后回复"}, 409)
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
	if stats["publishedEntries"].(float64) != 2 {
		t.Fatalf("stats: %v", stats)
	}

	msg := dataMap(t, detail)
	count := msg["inputTokens"].(float64) / 10
	cost, priced := msg["estimatedCost"].(float64)
	if count < 4 || msg["cacheHitRate"] != 0.2 || msg["cachedInputTokens"] != 2*count || msg["cacheWriteTokens"] != count || !priced || math.Abs(cost-count*39/1e6) > 1e-12 {
		t.Fatalf("incorrect message usage: inputs=%v cache=%v writes=%v cost=%v", msg["inputTokens"], msg["cachedInputTokens"], msg["cacheWriteTokens"], cost)
	}
	if !bytes.Contains(detail, []byte(`"name":"Alice"`)) || !bytes.Contains(detail, []byte(`"name":"Bob"`)) {
		t.Fatal("missing context speaker")
	}
	feedback := dataSlice(t, requestJSON(t, client, http.MethodGet, server.URL+"/api/feedback?rating=wrong", nil, http.StatusOK))
	id := object(t, feedback[0])["id"].(string)
	if object(t, feedback[0])["read"] != false || stats["unreadNegativeFeedback"].(float64) < 1 {
		t.Fatal("missing unread feedback")
	}
	for _, action := range []struct {
		path string
		body any
	}{{"/feedback/" + id + "/read", nil}, {"/feedback/read", map[string]any{"ids": []string{id}}}, {"/feedback/read", map[string]any{"all": true}}} {
		requestJSON(t, client, http.MethodPost, server.URL+"/api"+action.path, action.body, http.StatusOK)
	}
	stats = dataMap(t, requestJSON(t, client, http.MethodGet, server.URL+"/api/learning/stats", nil, http.StatusOK))
	feedback = dataSlice(t, requestJSON(t, client, http.MethodGet, server.URL+"/api/feedback?rating=wrong", nil, http.StatusOK))
	if object(t, feedback[0])["read"] != true || stats["unreadNegativeFeedback"] != float64(0) {
		t.Fatal("read feedback or badge incorrect")
	}
	kb := dataMap(t, requestJSON(t, client, http.MethodGet, server.URL+"/api/knowledge-bases/"+kbID, nil, http.StatusOK))
	if kb["entryCount"] != stats["publishedEntries"] {
		t.Fatalf("KB count %v, stats %v", kb["entryCount"], stats["publishedEntries"])
	}

	// Evaluation run in dry-run mode.
	requestJSON(t, client, http.MethodPost, server.URL+"/api/eval/cases", map[string]any{"question": "gpt-4o 价格", "expected": "2 美元", "enabled": true}, http.StatusOK)
	runID := createdID(t, requestJSON(t, client, http.MethodPost, server.URL+"/api/eval/runs", map[string]any{"chatProfileId": chatID, "knowledgeBaseIds": []string{kbID}}, http.StatusCreated))
	eventually(t, 10*time.Second, "eval run completed", func() (bool, error) {
		var status string
		err := pool.QueryRow(ctx, `SELECT status FROM eval_runs WHERE id=$1`, runID).Scan(&status)
		return status == "completed", err
	})
	var skipped int
	if err := pool.QueryRow(ctx, "SELECT count(*) FROM inbox_tasks t JOIN messages m ON m.id=t.message_id WHERE m.platform_message_id IN ('m1','m2') AND t.status='skipped'").Scan(&skipped); err != nil || skipped != 2 {
		t.Fatalf("skipped casual messages=%d err=%v", skipped, err)
	}
	profile := create("model-profiles", map[string]any{"name": "联网推送", "kind": "chat", "baseUrl": mock.URL + "/v1", "apiKey": "k", "model": "search-model", "enabled": true, "webSearchMode": "responses"})
	scheduledService := create("bots/"+botID+"/services", map[string]any{"name": "资讯客服", "agentId": agentID, "chatProfileId": profile})
	url := "conversations/" + convID + "/schedules"
	format := "text"
	if browserPath() != "" {
		format = "image"
	}
	create(url, map[string]any{"serviceId": scheduledService, "sendTime": "09:17", "instruction": "整理最新 AI 资讯", "source": "web", "format": format, "enabled": true})
	tomorrow := time.Now().In(chinaLocation).AddDate(0, 0, 1)
	at := time.Date(tomorrow.Year(), tomorrow.Month(), tomorrow.Day(), 9, 17, 0, 0, chinaLocation)
	for _, when := range []time.Time{at.Add(-time.Minute), at, at} {
		if err := application.queueScheduledPosts(ctx, when); err != nil {
			t.Fatal(err)
		}
	}
	eventually(t, 35*time.Second, "one generated scheduled post delivered", func() (bool, error) {
		var n, sent int
		err := pool.QueryRow(ctx, "SELECT count(*),count(*) FILTER(WHERE status='sent' AND reply_to_message_id IS NULL AND (content LIKE '【%】' || chr(10) || chr(10) || '查证后的测试资讯' OR (content='' AND parts->0->>'contentType'='image/png'))) FROM messages WHERE event_type='SCHEDULED_POST' AND conversation_id=$1", convID).Scan(&n, &sent)
		if sent == 0 {
			var reason string
			_ = pool.QueryRow(ctx, "SELECT COALESCE(last_error,'') FROM agent_jobs WHERE kind='scheduled_post' LIMIT 1").Scan(&reason)
			if reason != "" {
				return false, fmt.Errorf("%s", reason)
			}
		}
		return n == 1 && sent == 1, err
	})
	if format == "image" {
		found := false
		for _, payload := range mock.Sends() {
			if payload["msg_type"] == float64(7) && payload["msg_id"] == nil {
				if _, ok := payload["content"]; ok {
					t.Fatalf("scheduled image carried a caption: %v", payload)
				}
				found = true
			}
		}
		if !found {
			t.Fatal("missing active scheduled image")
		}
		postQQWebhook(t, client, hook, botSecret, groupEventBody(t, "image-skill", "GROUP_AT_MESSAGE_CREATE", "m8", "visitor", "用户", "把说明发成网页截图"))
		eventually(t, 30*time.Second, "HTML tool replied with QQ image", func() (bool, error) {
			var count int
			err := pool.QueryRow(ctx, "SELECT count(*) FROM messages WHERE reply_to_message_id='m8' AND status='sent' AND parts->0->>'contentType'='image/png'").Scan(&count)
			return count == 1, err
		})
		mock.mu.Lock()
		richReplies := 0
		for _, payload := range mock.qqSends {
			if payload["msg_type"] == float64(7) {
				richReplies++
			}
		}
		mock.mu.Unlock()
		if richReplies < 2 {
			t.Fatalf("expected scheduled and interactive QQ image payloads, got %d", richReplies)
		}
	}
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
