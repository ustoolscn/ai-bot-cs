package app

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"ai-bot/backend/internal/config"
	"ai-bot/backend/internal/qq"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

func TestDatabaseWorkerReliability(t *testing.T) {
	databaseURL := os.Getenv("TEST_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("TEST_DATABASE_URL is not set")
	}
	ctx := context.Background()
	pool := newIntegrationSchema(t, ctx, databaseURL)

	var tokenHits, sendHits atomic.Int32
	mock := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/token":
			tokenHits.Add(1)
			_ = json.NewEncoder(w).Encode(map[string]string{"access_token": "test-token"})
		default:
			sendHits.Add(1)
			http.Error(w, "temporary", http.StatusServiceUnavailable)
		}
	}))
	defer mock.Close()
	cfg := config.Config{DataDir: t.TempDir(), MasterKey: []byte("0123456789abcdef0123456789abcdef"), AdminUsername: "admin", AdminPassword: "admin123456", QQAPIBaseURL: mock.URL, QQTokenURL: mock.URL + "/token", WorkerPoll: time.Millisecond, AIRequestTimeout: 10 * time.Second}
	a, err := New(ctx, cfg, pool, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatal(err)
	}

	t.Run("model test endpoint returns diagnostics and validates dimensions", func(t *testing.T) {
		modelMock := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			var request map[string]any
			_ = json.NewDecoder(r.Body).Decode(&request)
			model, _ := request["model"].(string)
			if model == "upstream-error" {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusTooManyRequests)
				_, _ = w.Write([]byte(`{"error":{"message":"quota exceeded"}}`))
				return
			}
			if r.URL.Path == "/v1/chat/completions" {
				_ = json.NewEncoder(w).Encode(map[string]any{"choices": []any{map[string]any{"message": map[string]any{"role": "assistant", "content": "chat-ok"}}}, "usage": map[string]int{"prompt_tokens": 7, "completion_tokens": 3}})
				return
			}
			if r.URL.Path == "/v1/responses" {
				tools, _ := request["tools"].([]any)
				if len(tools) != 1 || tools[0].(map[string]any)["type"] != "web_search" {
					http.Error(w, "missing web_search tool", http.StatusBadRequest)
					return
				}
				_ = json.NewEncoder(w).Encode(map[string]any{"output": []any{map[string]any{"type": "message", "content": []any{map[string]any{"type": "output_text", "text": "responses-search-ok"}}}}, "usage": map[string]int{"input_tokens": 9, "output_tokens": 4}})
				return
			}
			if r.URL.Path == "/v1/embeddings" {
				_ = json.NewEncoder(w).Encode(map[string]any{"data": []any{map[string]any{"index": 0, "embedding": []float32{0.1, 0.2, 0.3}}}})
				return
			}
			http.NotFound(w, r)
		}))
		defer modelMock.Close()
		key, _ := a.cipher.Encrypt("model-key")
		insertProfile := func(kind, model string, dimension int) string {
			var id string
			var dim any = nil
			if dimension > 0 {
				dim = dimension
			}
			if err := pool.QueryRow(ctx, `INSERT INTO model_profiles(name,kind,base_url,api_key_enc,model,dimension) VALUES($1,$2,$3,$4,$5,$6) RETURNING id::text`, kind+model, kind, modelMock.URL+"/v1", key, model, dim).Scan(&id); err != nil {
				t.Fatal(err)
			}
			return id
		}
		chatID := insertProfile("chat", "chat-good", 0)
		status, body := callModelTest(t, a, chatID, map[string]string{"input": "hello", "systemPrompt": "be concise"})
		if status != 200 {
			t.Fatalf("chat status=%d body=%v", status, body)
		}
		data := body["data"].(map[string]any)
		if data["content"] != "chat-ok" || data["inputTokens"] != float64(7) || data["model"] != "chat-good" {
			t.Fatalf("chat data=%v", data)
		}
		responsesID := insertProfile("chat", "responses-good", 0)
		_, _ = pool.Exec(ctx, "UPDATE model_profiles SET web_search_mode='responses' WHERE id=$1", responsesID)
		status, body = callModelTest(t, a, responsesID, map[string]string{"input": "latest news", "systemPrompt": "search first"})
		if status != 200 {
			t.Fatalf("responses status=%d body=%v", status, body)
		}
		data = body["data"].(map[string]any)
		if data["content"] != "responses-search-ok" || data["inputTokens"] != float64(9) {
			t.Fatalf("responses data=%v", data)
		}
		embedID := insertProfile("embedding", "embed-good", 3)
		status, body = callModelTest(t, a, embedID, map[string]string{"input": "hello"})
		if status != 200 {
			t.Fatalf("embedding status=%d body=%v", status, body)
		}
		data = body["data"].(map[string]any)
		if data["dimensions"] != float64(3) || len(data["vectorPreview"].([]any)) != 3 {
			t.Fatalf("embedding data=%v", data)
		}
		mismatchID := insertProfile("embedding", "embed-good", 2)
		status, body = callModelTest(t, a, mismatchID, map[string]string{"input": "hello"})
		if status != 502 || !strings.Contains(modelErrorMessage(body), "配置为 2") || !strings.Contains(modelErrorMessage(body), "实际返回 3") {
			t.Fatalf("mismatch status=%d body=%v", status, body)
		}
		errorID := insertProfile("chat", "upstream-error", 0)
		status, body = callModelTest(t, a, errorID, map[string]string{"input": "hello"})
		if status != 502 || !strings.Contains(modelErrorMessage(body), "status 429") || !strings.Contains(modelErrorMessage(body), "quota exceeded") {
			t.Fatalf("upstream status=%d body=%v", status, body)
		}

		storageKey, err := a.files.Save(ctx, "failure.md", []byte("document embedding failure"))
		if err != nil {
			t.Fatal(err)
		}
		var kbID, docID string
		_ = pool.QueryRow(ctx, `INSERT INTO knowledge_bases(name,embedding_profile_id,embedding_model) VALUES('failure-kb',$1,'upstream-error') RETURNING id::text`, insertProfile("embedding", "upstream-error", 3)).Scan(&kbID)
		_ = pool.QueryRow(ctx, `INSERT INTO knowledge_documents(knowledge_base_id,name,storage_key,content_type,size_bytes) VALUES($1,'failure.md',$2,'text/markdown',26) RETURNING id::text`, kbID, storageKey).Scan(&docID)
		if err := a.processDocument(ctx); err != nil {
			t.Fatal(err)
		}
		var lastError string
		_ = pool.QueryRow(ctx, "SELECT last_error FROM knowledge_documents WHERE id=$1", docID).Scan(&lastError)
		if !strings.Contains(lastError, "生成 Embedding 失败") || !strings.Contains(lastError, "status 429") || !strings.Contains(lastError, "quota exceeded") {
			t.Fatalf("last_error=%q", lastError)
		}
		_, _ = pool.Exec(ctx, "UPDATE knowledge_documents SET status='failed' WHERE id=$1", docID)
	})
	secret, _ := a.cipher.Encrypt("secret")
	var botID, conversationID string
	err = pool.QueryRow(ctx, `INSERT INTO bots(name,app_id,app_secret_enc) VALUES('bot','app',$1) RETURNING id::text`, secret).Scan(&botID)
	if err != nil {
		t.Fatal(err)
	}
	err = pool.QueryRow(ctx, `INSERT INTO conversations(channel,bot_id,platform_id,type,name) VALUES('qq',$1,'group','group','group') RETURNING id::text`, botID).Scan(&conversationID)
	if err != nil {
		t.Fatal(err)
	}

	t.Run("expired reply is terminal without QQ call", func(t *testing.T) {
		messageID, taskID := insertOutboxFixture(t, pool, botID, conversationID, time.Now().Add(-time.Minute))
		if err := a.processOutbox(ctx); err != nil {
			t.Fatal(err)
		}
		var taskStatus, messageStatus string
		_ = pool.QueryRow(ctx, "SELECT status FROM outbox_tasks WHERE id=$1", taskID).Scan(&taskStatus)
		_ = pool.QueryRow(ctx, "SELECT status FROM messages WHERE id=$1", messageID).Scan(&messageStatus)
		if taskStatus != "expired" || messageStatus != "expired" {
			t.Fatalf("task=%s message=%s", taskStatus, messageStatus)
		}
		if tokenHits.Load() != 0 || sendHits.Load() != 0 {
			t.Fatalf("QQ called for expired reply: token=%d send=%d", tokenHits.Load(), sendHits.Load())
		}
	})

	t.Run("temporary QQ errors retry at most three times", func(t *testing.T) {
		messageID, taskID := insertOutboxFixture(t, pool, botID, conversationID, time.Now().Add(5*time.Minute))
		for i := 0; i < 3; i++ {
			if err := a.processOutbox(ctx); err != nil {
				t.Fatal(err)
			}
			if i < 2 {
				_, _ = pool.Exec(ctx, "UPDATE outbox_tasks SET next_attempt_at=now() WHERE id=$1", taskID)
			}
		}
		var status, messageStatus string
		var attempts int
		_ = pool.QueryRow(ctx, "SELECT status,attempts FROM outbox_tasks WHERE id=$1", taskID).Scan(&status, &attempts)
		_ = pool.QueryRow(ctx, "SELECT status FROM messages WHERE id=$1", messageID).Scan(&messageStatus)
		if status != "failed" || messageStatus != "failed" || attempts != 3 {
			t.Fatalf("status=%s message=%s attempts=%d", status, messageStatus, attempts)
		}
		if tokenHits.Load() != 3 || sendHits.Load() != 3 {
			t.Fatalf("expected 3 attempts: token=%d send=%d", tokenHits.Load(), sendHits.Load())
		}
		if err := a.processOutbox(ctx); err == nil {
			t.Fatal("expected no pending job after third failure")
		}
	})

	t.Run("startup recovery resets processing tasks", func(t *testing.T) {
		inMessage := uuid.NewString()
		_, err := pool.Exec(ctx, `INSERT INTO messages(id,channel,bot_id,conversation_id,direction,content) VALUES($1,'qq',$2,$3,'inbound','hi')`, inMessage, botID, conversationID)
		if err != nil {
			t.Fatal(err)
		}
		_, _ = pool.Exec(ctx, "INSERT INTO inbox_tasks(message_id,status,locked_at) VALUES($1,'processing',now())", inMessage)
		outMessage, outTask := insertOutboxFixture(t, pool, botID, conversationID, time.Now().Add(time.Minute))
		_, _ = pool.Exec(ctx, "UPDATE outbox_tasks SET status='processing',locked_at=now() WHERE id=$1", outTask)
		_, _ = pool.Exec(ctx, "INSERT INTO message_retractions(message_id,status,locked_at) VALUES($1,'processing',now())", outMessage)
		var profileID, kbID, docID string
		_ = pool.QueryRow(ctx, `INSERT INTO model_profiles(name,kind,base_url,api_key_enc,model,dimension) VALUES('embed','embedding','http://invalid','x','embed',2) RETURNING id::text`).Scan(&profileID)
		_ = pool.QueryRow(ctx, `INSERT INTO knowledge_bases(name,embedding_profile_id,embedding_model) VALUES('kb',$1,'embed') RETURNING id::text`, profileID).Scan(&kbID)
		_ = pool.QueryRow(ctx, `INSERT INTO knowledge_documents(knowledge_base_id,name,storage_key,content_type,size_bytes,status) VALUES($1,'a.md','x.md','text/markdown',1,'processing') RETURNING id::text`, kbID).Scan(&docID)
		if err := a.recoverTasks(ctx); err != nil {
			t.Fatal(err)
		}
		for table, id := range map[string]string{"inbox_tasks": inMessage, "outbox_tasks": outMessage, "message_retractions": outMessage, "knowledge_documents": docID} {
			var status string
			column := "message_id"
			if table == "knowledge_documents" {
				column = "id"
			}
			if err := pool.QueryRow(ctx, "SELECT status FROM "+table+" WHERE "+column+"=$1", id).Scan(&status); err != nil {
				t.Fatal(err)
			}
			if status != "pending" {
				t.Fatalf("%s status=%s", table, status)
			}
		}
	})

	t.Run("blocked user is stored but never queued and webhook is idempotent", func(t *testing.T) {
		_, err := pool.Exec(ctx, `INSERT INTO blocked_users(bot_id,platform_user_id,reason) VALUES($1,'blocked-user','test')`, botID)
		if err != nil {
			t.Fatal(err)
		}
		raw := []byte(`{"id":"blocked-event","op":0,"t":"GROUP_AT_MESSAGE_CREATE","d":{"id":"blocked-message","group_openid":"group","content":"hello","author":{"member_openid":"blocked-user"}}}`)
		env, msg, err := qq.Parse(raw, botID)
		if err != nil {
			t.Fatal(err)
		}
		if err = a.persistInbound(ctx, botID, env, msg, raw); err != nil {
			t.Fatal(err)
		}
		if err = a.persistInbound(ctx, botID, env, msg, raw); err != nil {
			t.Fatal(err)
		}
		var events, messages, tasks int
		_ = pool.QueryRow(ctx, "SELECT count(*) FROM webhook_events WHERE platform_event_id='blocked-event'").Scan(&events)
		_ = pool.QueryRow(ctx, "SELECT count(*) FROM messages WHERE platform_message_id='blocked-message'").Scan(&messages)
		_ = pool.QueryRow(ctx, `SELECT count(*) FROM inbox_tasks t JOIN messages m ON m.id=t.message_id WHERE m.platform_message_id='blocked-message'`).Scan(&tasks)
		if events != 1 || messages != 1 || tasks != 0 {
			t.Fatalf("events=%d messages=%d tasks=%d", events, messages, tasks)
		}
	})

	t.Run("deleting bot cascades messages and tasks", func(t *testing.T) {
		_, err := pool.Exec(ctx, "DELETE FROM bots WHERE id=$1", botID)
		if err != nil {
			t.Fatal(err)
		}
		var conversations, messages, tasks int
		_ = pool.QueryRow(ctx, "SELECT count(*) FROM conversations WHERE bot_id=$1", botID).Scan(&conversations)
		_ = pool.QueryRow(ctx, "SELECT count(*) FROM messages WHERE bot_id=$1", botID).Scan(&messages)
		_ = pool.QueryRow(ctx, "SELECT count(*) FROM outbox_tasks").Scan(&tasks)
		if conversations != 0 || messages != 0 || tasks != 0 {
			t.Fatalf("conversations=%d messages=%d outbox=%d", conversations, messages, tasks)
		}
	})
}

func callModelTest(t *testing.T, a *App, id string, input any) (int, map[string]any) {
	t.Helper()
	payload, _ := json.Marshal(input)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodPost, "/api/model-profiles/"+id+"/test", bytes.NewReader(payload))
	c.Request.Header.Set("Content-Type", "application/json")
	c.Params = gin.Params{{Key: "id", Value: id}}
	a.testModel(c)
	var body map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode response: %v body=%s", err, w.Body.String())
	}
	return w.Code, body
}
func modelErrorMessage(body map[string]any) string {
	e, _ := body["error"].(map[string]any)
	message, _ := e["message"].(string)
	return message
}

func insertOutboxFixture(t *testing.T, pool *pgxpool.Pool, botID, conversationID string, deadline time.Time) (string, string) {
	t.Helper()
	var messageID, taskID string
	if err := pool.QueryRow(context.Background(), `INSERT INTO messages(channel,bot_id,conversation_id,direction,content,reply_to_message_id,status,reply_deadline) VALUES('qq',$1,$2,'outbound','answer','source','pending',$3) RETURNING id::text`, botID, conversationID, deadline).Scan(&messageID); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(context.Background(), `INSERT INTO outbox_tasks(message_id) VALUES($1) RETURNING id::text`, messageID).Scan(&taskID); err != nil {
		t.Fatal(err)
	}
	return messageID, taskID
}
