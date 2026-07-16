package app

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"mime/multipart"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"ai-bot/backend/internal/config"
	database "ai-bot/backend/internal/db"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

func TestMVPPostgresPgvectorEndToEnd(t *testing.T) {
	databaseURL := os.Getenv("TEST_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("TEST_DATABASE_URL is not configured")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	pool := newIntegrationSchema(t, ctx, databaseURL)

	mock := newUpstreamMock(t)
	defer mock.Close()

	cfg := config.Config{
		DataDir:          t.TempDir(),
		MasterKey:        []byte("0123456789abcdef0123456789abcdef"),
		AdminUsername:    "admin",
		AdminPassword:    "integration-password",
		QQAPIBaseURL:     mock.URL,
		QQTokenURL:       mock.URL + "/token",
		CookieSecure:     false,
		WorkerPoll:       10 * time.Millisecond,
		AIRequestTimeout: 10 * time.Second,
	}
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	application, err := New(ctx, cfg, pool, logger)
	if err != nil {
		t.Fatalf("create app: %v", err)
	}

	workerCtx, stopWorkers := context.WithCancel(context.Background())
	workersDone := make(chan struct{})
	go func() {
		defer close(workersDone)
		application.RunWorkers(workerCtx)
	}()
	t.Cleanup(func() {
		stopWorkers()
		select {
		case <-workersDone:
		case <-time.After(3 * time.Second):
			t.Error("workers did not stop")
		}
	})

	server := httptest.NewServer(application.Router())
	t.Cleanup(server.Close)
	jar, err := cookiejar.New(nil)
	if err != nil {
		t.Fatal(err)
	}
	client := &http.Client{Jar: jar, Timeout: 5 * time.Second}

	login := requestJSON(t, client, http.MethodPost, server.URL+"/api/auth/login", map[string]any{
		"username": "admin", "password": "integration-password",
	}, http.StatusOK)
	if dataMap(t, login)["username"] != "admin" {
		t.Fatalf("unexpected login response: %s", login)
	}
	requestJSON(t, client, http.MethodGet, server.URL+"/api/auth/me", nil, http.StatusOK)

	const chatAPIKey = "chat-secret-key-that-must-not-leak"
	chatID := createdID(t, requestJSON(t, client, http.MethodPost, server.URL+"/api/model-profiles", map[string]any{
		"name": "集成测试对话模型", "kind": "chat", "baseUrl": mock.URL + "/v1",
		"apiKey": chatAPIKey, "model": "mock-chat", "enabled": true, "isDefault": true,
	}, http.StatusOK))

	const embeddingAPIKey = "embedding-secret-key-that-must-not-leak"
	embeddingID := createdID(t, requestJSON(t, client, http.MethodPost, server.URL+"/api/model-profiles", map[string]any{
		"name": "集成测试向量模型", "kind": "embedding", "baseUrl": mock.URL + "/v1",
		"apiKey": embeddingAPIKey, "model": "mock-embedding", "dimension": 3,
		"enabled": true, "isDefault": true,
	}, http.StatusOK))

	const botSecret = "qq-bot-secret-that-must-not-leak"
	botID := createdID(t, requestJSON(t, client, http.MethodPost, server.URL+"/api/bots", map[string]any{
		"name": "集成测试机器人", "appId": "qq-app-id", "appSecret": botSecret,
		"modelProfileId": chatID, "enabled": true,
	}, http.StatusCreated))

	modelsJSON := requestJSON(t, client, http.MethodGet, server.URL+"/api/model-profiles", nil, http.StatusOK)
	assertSecretNotReturned(t, modelsJSON, chatAPIKey, embeddingAPIKey)
	for _, item := range dataSlice(t, modelsJSON) {
		m := object(t, item)
		if _, exists := m["apiKey"]; exists {
			t.Fatalf("model API returned apiKey: %s", modelsJSON)
		}
		if m["hasApiKey"] != true {
			t.Fatalf("model API did not expose the safe hasApiKey flag: %s", modelsJSON)
		}
	}
	botsJSON := requestJSON(t, client, http.MethodGet, server.URL+"/api/bots", nil, http.StatusOK)
	assertSecretNotReturned(t, botsJSON, botSecret)
	for _, item := range dataSlice(t, botsJSON) {
		m := object(t, item)
		if _, exists := m["appSecret"]; exists {
			t.Fatalf("bot API returned appSecret: %s", botsJSON)
		}
		if m["hasSecret"] != true {
			t.Fatalf("bot API did not expose the safe hasSecret flag: %s", botsJSON)
		}
	}

	kbID := createdID(t, requestJSON(t, client, http.MethodPost, server.URL+"/api/knowledge-bases", map[string]any{
		"name": "售后知识库", "description": "集成测试资料", "embeddingProfileId": embeddingID,
	}, http.StatusCreated))
	documentID := uploadDocument(t, client, server.URL+"/api/knowledge-bases/"+kbID+"/documents", "refund.md",
		"# 退款政策\n\n用户付款后七天内可以申请无理由退款。退款申请由售后团队审核。")
	eventually(t, 8*time.Second, "document indexed", func() (bool, error) {
		var status string
		var chunks int
		err := pool.QueryRow(ctx, `SELECT d.status,count(c.id) FROM knowledge_documents d LEFT JOIN knowledge_chunks c ON c.document_id=d.id WHERE d.id=$1 GROUP BY d.id`, documentID).Scan(&status, &chunks)
		return status == "ready" && chunks > 0, err
	})

	ordinaryBody := qqEventBody(t, "event-ordinary", "GROUP_MESSAGE_CREATE", "message-ordinary", "今天的普通群消息只应作为上下文")
	postQQWebhook(t, client, server.URL+"/callbacks/qq/"+botID, botSecret, ordinaryBody)

	var conversationID string
	if err := pool.QueryRow(ctx, `SELECT id::text FROM conversations WHERE bot_id=$1 AND platform_id='group-open-id'`, botID).Scan(&conversationID); err != nil {
		t.Fatalf("ordinary message did not create conversation: %v", err)
	}
	var ordinaryInbox int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM inbox_tasks t JOIN messages m ON m.id=t.message_id WHERE m.platform_message_id='message-ordinary'`).Scan(&ordinaryInbox); err != nil {
		t.Fatal(err)
	}
	if ordinaryInbox != 0 || mock.ChatCount() != 0 || mock.QQSendCount() != 0 {
		t.Fatalf("ordinary mention-only message triggered a reply: inbox=%d chat=%d sends=%d", ordinaryInbox, mock.ChatCount(), mock.QQSendCount())
	}

	requestJSON(t, client, http.MethodPut, server.URL+"/api/conversations/"+conversationID, map[string]any{
		"name": "测试群", "triggerMode": "mention_only", "systemPrompt": "你是售后助手。",
		"chatProfileId": chatID, "enabled": true, "contextLimit": 20,
		"knowledgeBaseIds": []string{kbID},
	}, http.StatusOK)

	mentionBody := qqEventBody(t, "event-mention", "GROUP_AT_MESSAGE_CREATE", "message-mention", "@机器人 退款期限是多久？")
	postQQWebhook(t, client, server.URL+"/callbacks/qq/"+botID, botSecret, mentionBody)
	eventually(t, 8*time.Second, "RAG answer delivered to QQ", func() (bool, error) {
		var sent int
		err := pool.QueryRow(ctx, `SELECT count(*) FROM outbox_tasks WHERE status='sent'`).Scan(&sent)
		return sent == 1 && mock.QQSendCount() == 1, err
	})

	chatRequests := mock.ChatRequests()
	if len(chatRequests) != 1 {
		t.Fatalf("expected one chat request, got %d", len(chatRequests))
	}
	chatPayload := string(chatRequests[0])
	for _, expected := range []string{"七天内可以申请无理由退款", "今天的普通群消息只应作为上下文", "退款期限是多久"} {
		if !strings.Contains(chatPayload, expected) {
			t.Fatalf("chat prompt is missing %q: %s", expected, chatPayload)
		}
	}

	sends := mock.QQSends()
	if len(sends) != 1 {
		t.Fatalf("expected one QQ send, got %d", len(sends))
	}
	if sends[0].Authorization != "QQBot mock-access-token" || sends[0].Path != "/v2/groups/group-open-id/messages" {
		t.Fatalf("unexpected QQ request: %+v", sends[0])
	}
	if sends[0].Body["content"] != "根据知识库，退款期限是七天。" || sends[0].Body["msg_id"] != "message-mention" {
		t.Fatalf("unexpected QQ message body: %#v", sends[0].Body)
	}

	postQQWebhook(t, client, server.URL+"/callbacks/qq/"+botID, botSecret, mentionBody)
	time.Sleep(250 * time.Millisecond)
	if mock.ChatCount() != 1 || mock.QQSendCount() != 1 {
		t.Fatalf("duplicate event was processed again: chats=%d sends=%d", mock.ChatCount(), mock.QQSendCount())
	}
	var webhookEvents, inboundMessages, agentRuns, outboundMessages int
	if err := pool.QueryRow(ctx, `SELECT
		(SELECT count(*) FROM webhook_events WHERE platform_event_id='event-mention'),
		(SELECT count(*) FROM messages WHERE platform_message_id='message-mention'),
		(SELECT count(*) FROM agent_runs r JOIN messages m ON m.id=r.message_id WHERE m.platform_message_id='message-mention'),
		(SELECT count(*) FROM messages WHERE direction='outbound' AND reply_to_message_id='message-mention')`).Scan(&webhookEvents, &inboundMessages, &agentRuns, &outboundMessages); err != nil {
		t.Fatal(err)
	}
	if webhookEvents != 1 || inboundMessages != 1 || agentRuns != 1 || outboundMessages != 1 {
		t.Fatalf("idempotency counts are wrong: events=%d inbound=%d runs=%d outbound=%d", webhookEvents, inboundMessages, agentRuns, outboundMessages)
	}

	var mentionMessageID string
	if err := pool.QueryRow(ctx, `SELECT id::text FROM messages WHERE platform_message_id='message-mention'`).Scan(&mentionMessageID); err != nil {
		t.Fatal(err)
	}
	messageDetail := requestJSON(t, client, http.MethodGet, server.URL+"/api/messages/"+mentionMessageID, nil, http.StatusOK)
	if !bytes.Contains(messageDetail, []byte("七天内可以申请无理由退款")) || !bytes.Contains(messageDetail, []byte(`"status":"completed"`)) {
		t.Fatalf("message detail does not expose the completed RAG trace: %s", messageDetail)
	}
	assertSecretNotReturned(t, messageDetail, chatAPIKey, embeddingAPIKey, botSecret)
}

func newIntegrationSchema(t *testing.T, ctx context.Context, databaseURL string) *pgxpool.Pool {
	t.Helper()
	admin, err := pgxpool.New(ctx, databaseURL)
	if err != nil {
		t.Fatalf("open integration database: %v", err)
	}
	if err = admin.Ping(ctx); err != nil {
		admin.Close()
		t.Fatalf("ping integration database: %v", err)
	}
	if _, err = admin.Exec(ctx, `CREATE EXTENSION IF NOT EXISTS vector; CREATE EXTENSION IF NOT EXISTS pgcrypto`); err != nil {
		admin.Close()
		t.Fatalf("enable PostgreSQL extensions: %v", err)
	}

	schema := "app_integration_" + strings.ReplaceAll(uuid.NewString(), "-", "")
	identifier := pgx.Identifier{schema}.Sanitize()
	if _, err = admin.Exec(ctx, "DROP SCHEMA IF EXISTS "+identifier+" CASCADE; CREATE SCHEMA "+identifier); err != nil {
		admin.Close()
		t.Fatalf("rebuild integration schema: %v", err)
	}

	poolConfig, err := pgxpool.ParseConfig(databaseURL)
	if err != nil {
		admin.Close()
		t.Fatalf("parse TEST_DATABASE_URL: %v", err)
	}
	poolConfig.ConnConfig.RuntimeParams["search_path"] = schema + ",public"
	pool, err := pgxpool.NewWithConfig(ctx, poolConfig)
	if err != nil {
		admin.Close()
		t.Fatalf("open schema pool: %v", err)
	}
	if err = pool.Ping(ctx); err != nil {
		pool.Close()
		admin.Close()
		t.Fatalf("ping schema pool: %v", err)
	}
	if err = database.Migrate(ctx, pool); err != nil {
		pool.Close()
		_, _ = admin.Exec(context.Background(), "DROP SCHEMA IF EXISTS "+identifier+" CASCADE")
		admin.Close()
		t.Fatalf("migrate integration schema: %v", err)
	}

	t.Cleanup(func() {
		pool.Close()
		cleanupCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if _, err := admin.Exec(cleanupCtx, "DROP SCHEMA IF EXISTS "+identifier+" CASCADE"); err != nil {
			t.Errorf("drop integration schema: %v", err)
		}
		admin.Close()
	})
	return pool
}

type upstreamMock struct {
	*httptest.Server
	t          *testing.T
	mu         sync.Mutex
	chatBodies [][]byte
	qqSends    []qqSendRequest
}

type qqSendRequest struct {
	Path          string
	Authorization string
	Body          map[string]any
}

func newUpstreamMock(t *testing.T) *upstreamMock {
	t.Helper()
	m := &upstreamMock{t: t}
	m.Server = httptest.NewServer(http.HandlerFunc(m.serveHTTP))
	return m
}

func (m *upstreamMock) serveHTTP(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	switch {
	case r.URL.Path == "/v1/embeddings":
		var request struct {
			Input []string `json:"input"`
		}
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		data := make([]map[string]any, len(request.Input))
		for i := range request.Input {
			data[i] = map[string]any{"index": i, "embedding": []float32{1, 0, 0}}
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"data": data})
	case r.URL.Path == "/v1/chat/completions":
		body, _ := io.ReadAll(r.Body)
		m.mu.Lock()
		m.chatBodies = append(m.chatBodies, append([]byte(nil), body...))
		m.mu.Unlock()
		_ = json.NewEncoder(w).Encode(map[string]any{
			"choices": []any{map[string]any{"message": map[string]any{"role": "assistant", "content": "根据知识库，退款期限是七天。"}}},
			"usage":   map[string]int{"prompt_tokens": 42, "completion_tokens": 9},
		})
	case r.URL.Path == "/token":
		_ = json.NewEncoder(w).Encode(map[string]any{"access_token": "mock-access-token", "expires_in": 7200})
	case strings.HasPrefix(r.URL.Path, "/v2/groups/") && strings.HasSuffix(r.URL.Path, "/messages"):
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		m.mu.Lock()
		m.qqSends = append(m.qqSends, qqSendRequest{Path: r.URL.Path, Authorization: r.Header.Get("Authorization"), Body: body})
		m.mu.Unlock()
		_ = json.NewEncoder(w).Encode(map[string]any{"id": "qq-outbound-message-id"})
	default:
		http.NotFound(w, r)
	}
}

func (m *upstreamMock) ChatCount() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.chatBodies)
}

func (m *upstreamMock) ChatRequests() [][]byte {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([][]byte, len(m.chatBodies))
	for i := range m.chatBodies {
		out[i] = append([]byte(nil), m.chatBodies[i]...)
	}
	return out
}

func (m *upstreamMock) QQSendCount() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.qqSends)
}

func (m *upstreamMock) QQSends() []qqSendRequest {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]qqSendRequest, len(m.qqSends))
	copy(out, m.qqSends)
	return out
}

func requestJSON(t *testing.T, client *http.Client, method, endpoint string, payload any, status int) []byte {
	t.Helper()
	var body io.Reader
	if payload != nil {
		encoded, err := json.Marshal(payload)
		if err != nil {
			t.Fatal(err)
		}
		body = bytes.NewReader(encoded)
	}
	req, err := http.NewRequest(method, endpoint, body)
	if err != nil {
		t.Fatal(err)
	}
	if payload != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("%s %s: %v", method, endpoint, err)
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != status {
		t.Fatalf("%s %s: status=%d want=%d body=%s", method, endpoint, resp.StatusCode, status, data)
	}
	return data
}

func dataMap(t *testing.T, raw []byte) map[string]any {
	t.Helper()
	var envelope struct {
		Data map[string]any `json:"data"`
	}
	if err := json.Unmarshal(raw, &envelope); err != nil {
		t.Fatalf("decode object response: %v: %s", err, raw)
	}
	return envelope.Data
}

func dataSlice(t *testing.T, raw []byte) []any {
	t.Helper()
	var envelope struct {
		Data []any `json:"data"`
	}
	if err := json.Unmarshal(raw, &envelope); err != nil {
		t.Fatalf("decode array response: %v: %s", err, raw)
	}
	return envelope.Data
}

func object(t *testing.T, value any) map[string]any {
	t.Helper()
	m, ok := value.(map[string]any)
	if !ok {
		t.Fatalf("value is not an object: %#v", value)
	}
	return m
}

func createdID(t *testing.T, raw []byte) string {
	t.Helper()
	id, _ := dataMap(t, raw)["id"].(string)
	if id == "" {
		t.Fatalf("response does not contain an id: %s", raw)
	}
	return id
}

func uploadDocument(t *testing.T, client *http.Client, endpoint, filename, content string) string {
	t.Helper()
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	part, err := writer.CreateFormFile("file", filename)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = io.WriteString(part, content); err != nil {
		t.Fatal(err)
	}
	if err = writer.Close(); err != nil {
		t.Fatal(err)
	}
	req, err := http.NewRequest(http.MethodPost, endpoint, &body)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", writer.FormDataContentType())
	resp, err := client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("upload document: status=%d body=%s", resp.StatusCode, raw)
	}
	return createdID(t, raw)
}

func qqEventBody(t *testing.T, eventID, eventType, messageID, content string) []byte {
	t.Helper()
	body, err := json.Marshal(map[string]any{
		"id": eventID, "op": 0, "t": eventType,
		"d": map[string]any{
			"id": messageID, "group_openid": "group-open-id", "content": content,
			"timestamp": time.Now().UTC().Format(time.RFC3339),
			"author":    map[string]any{"member_openid": "member-open-id", "username": "群成员"},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	return body
}

func postQQWebhook(t *testing.T, client *http.Client, endpoint, secret string, body []byte) {
	t.Helper()
	timestamp := fmt.Sprintf("%d", time.Now().Unix())
	seed := make([]byte, ed25519.SeedSize)
	secretBytes := []byte(secret)
	for i := range seed {
		seed[i] = secretBytes[i%len(secretBytes)]
	}
	privateKey := ed25519.NewKeyFromSeed(seed)
	signature := ed25519.Sign(privateKey, append([]byte(timestamp), body...))
	req, err := http.NewRequest(http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Signature-Timestamp", timestamp)
	req.Header.Set("X-Signature-Ed25519", hex.EncodeToString(signature))
	resp, err := client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	responseBody, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("QQ webhook: status=%d body=%s", resp.StatusCode, responseBody)
	}
	if !bytes.Contains(responseBody, []byte(`"op":12`)) {
		t.Fatalf("QQ webhook did not acknowledge event: %s", responseBody)
	}
}

func assertSecretNotReturned(t *testing.T, raw []byte, secrets ...string) {
	t.Helper()
	for _, secret := range secrets {
		if bytes.Contains(raw, []byte(secret)) {
			t.Fatalf("secret was returned by API: %s", raw)
		}
	}
}

func eventually(t *testing.T, timeout time.Duration, description string, check func() (bool, error)) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	var lastErr error
	for time.Now().Before(deadline) {
		ok, err := check()
		if err == nil && ok {
			return
		}
		lastErr = err
		time.Sleep(20 * time.Millisecond)
	}
	if lastErr != nil {
		t.Fatalf("timed out waiting for %s: %v", description, lastErr)
	}
	t.Fatalf("timed out waiting for %s", description)
}
