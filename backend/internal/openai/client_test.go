package openai

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"ai-bot/backend/internal/domain"
)

func TestChatAndEmbedding(t *testing.T) {
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer key" {
			t.Errorf("missing authorization")
		}
		switch r.URL.Path {
		case "/v1/chat/completions":
			var request map[string]any
			_ = json.NewDecoder(r.Body).Decode(&request)
			if request["enable_search"] != true {
				t.Errorf("enable_search=%v", request["enable_search"])
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"choices": []any{map[string]any{"message": map[string]any{"role": "assistant", "content": "OK"}}}, "usage": map[string]int{"prompt_tokens": 2, "completion_tokens": 1}})
		case "/v1/embeddings":
			var request map[string]any
			_ = json.NewDecoder(r.Body).Decode(&request)
			if request["dimensions"] != float64(2) {
				t.Errorf("dimensions=%v", request["dimensions"])
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"data": []any{map[string]any{"index": 0, "embedding": []float32{0.1, 0.2}}}})
		default:
			http.NotFound(w, r)
		}
	}))
	defer s.Close()
	c := New(s.URL+"/v1", "key", "test", time.Second)
	c.Dimensions = 2
	c.ExtraBody = map[string]any{"enable_search": true}
	chat, err := c.Chat(context.Background(), []domain.ChatMessage{{Role: "user", Content: "hi"}})
	if err != nil || chat.Content != "OK" || chat.InputTokens != 2 {
		t.Fatalf("chat=%+v err=%v", chat, err)
	}
	v, err := c.Embed(context.Background(), []string{"hi"})
	if err != nil || len(v) != 1 || len(v[0]) != 2 {
		t.Fatalf("embedding=%v err=%v", v, err)
	}
}

func TestNonJSONResponseExplainsBaseURL(t *testing.T) {
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write([]byte("<html>wrong route</html>"))
	}))
	defer s.Close()
	c := New(s.URL, "", "test", time.Second)
	_, err := c.Embed(context.Background(), []string{"test"})
	if err == nil {
		t.Fatal("expected decode error")
	}
	message := err.Error()
	for _, want := range []string{"模型接口返回非 JSON", "text/html", "wrong route", "/v1"} {
		if !strings.Contains(message, want) {
			t.Fatalf("error %q missing %q", message, want)
		}
	}
}
