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
			if request["reasoning_effort"] != "low" {
				t.Errorf("reasoning_effort=%v", request["reasoning_effort"])
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
	c.ReasoningEffort = "low"
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

func TestHTTPStatusErrorCanBeClassified(t *testing.T) {
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, `{"error":"content moderation"}`, http.StatusForbidden)
	}))
	defer s.Close()

	c := New(s.URL, "", "test", time.Second)
	_, err := c.Chat(context.Background(), []domain.ChatMessage{{Role: "user", Content: "blocked"}})
	if err == nil || !IsHTTPStatus(err, http.StatusForbidden) {
		t.Fatalf("expected classifiable 403 error, got %v", err)
	}
	if IsHTTPStatus(err, http.StatusTooManyRequests) {
		t.Fatalf("403 error must not be classified as 429: %v", err)
	}
}

func TestResponsesWebSearch(t *testing.T) {
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/responses" {
			http.NotFound(w, r)
			return
		}
		var request map[string]any
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Fatal(err)
		}
		tools, _ := request["tools"].([]any)
		if len(tools) != 1 || tools[0].(map[string]any)["type"] != "web_search" {
			t.Fatalf("unexpected tools: %#v", request["tools"])
		}
		if request["instructions"] != "使用最新信息回答。" {
			t.Fatalf("unexpected instructions: %#v", request["instructions"])
		}
		reasoning, _ := request["reasoning"].(map[string]any)
		if reasoning["effort"] != "high" {
			t.Fatalf("unexpected reasoning: %#v", request["reasoning"])
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"output": []any{map[string]any{
				"type":    "message",
				"content": []any{map[string]any{"type": "output_text", "text": "联网搜索结果"}},
			}},
			"usage": map[string]any{"input_tokens": 8, "output_tokens": 4, "total_tokens": 12},
		})
	}))
	defer s.Close()

	c := New(s.URL+"/v1", "key", "gpt-test", time.Second)
	c.UseResponses = true
	c.ReasoningEffort = "high"
	result, err := c.Chat(context.Background(), []domain.ChatMessage{
		{Role: "system", Content: "使用最新信息回答。"},
		{Role: "user", Content: "今天有什么新闻？"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.Content != "联网搜索结果" || result.InputTokens != 8 || result.OutputTokens != 4 {
		t.Fatalf("unexpected result: %#v", result)
	}
}

func TestChatCompletionsImageInput(t *testing.T) {
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var request map[string]any
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Fatal(err)
		}
		messages := request["messages"].([]any)
		content := messages[0].(map[string]any)["content"].([]any)
		if content[0].(map[string]any)["text"] != "这是什么？" {
			t.Fatalf("unexpected text part: %#v", content)
		}
		image := content[1].(map[string]any)
		if image["type"] != "image_url" {
			t.Fatalf("unexpected image part: %#v", image)
		}
		imageURL := image["image_url"].(map[string]any)
		if imageURL["url"] != "data:image/png;base64,aW1hZ2U=" || imageURL["detail"] != "high" {
			t.Fatalf("unexpected image_url: %#v", imageURL)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"choices": []any{map[string]any{"message": map[string]any{"content": "图片内容"}}}})
	}))
	defer s.Close()

	c := New(s.URL, "", "vision-model", time.Second)
	result, err := c.Chat(context.Background(), []domain.ChatMessage{{
		Role: "user", Content: "这是什么？",
		Parts: []domain.ChatContentPart{{Type: "image", DataURL: "data:image/png;base64,aW1hZ2U=", Detail: "high"}},
	}})
	if err != nil || result.Content != "图片内容" {
		t.Fatalf("result=%#v err=%v", result, err)
	}
}

func TestResponsesImageInput(t *testing.T) {
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var request map[string]any
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Fatal(err)
		}
		input := request["input"].([]any)
		content := input[0].(map[string]any)["content"].([]any)
		image := content[1].(map[string]any)
		if image["type"] != "input_image" || image["image_url"] != "data:image/jpeg;base64,aW1hZ2U=" || image["detail"] != "auto" {
			t.Fatalf("unexpected responses image: %#v", image)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"output_text": "识别完成"})
	}))
	defer s.Close()

	c := New(s.URL, "", "vision-model", time.Second)
	c.UseResponses = true
	result, err := c.Chat(context.Background(), []domain.ChatMessage{{
		Role: "user", Content: "描述图片",
		Parts: []domain.ChatContentPart{{Type: "image", DataURL: "data:image/jpeg;base64,aW1hZ2U="}},
	}})
	if err != nil || result.Content != "识别完成" {
		t.Fatalf("result=%#v err=%v", result, err)
	}
}
