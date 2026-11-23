package openai

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"ai-bot/backend/internal/domain"
)

func TestResponsesFunctionCalling(t *testing.T) {
	var got map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(b, &got)
		w.Write([]byte(`{"output":[{"type":"function_call","call_id":"c1","name":"get_model_price","arguments":"{\"model\":\"4o\"}"}],"usage":{"input_tokens":3,"output_tokens":1}}`))
	}))
	defer srv.Close()
	c := New(srv.URL, "k", "m", 5*time.Second)
	c.UseResponses = true
	msgs := []domain.ChatMessage{
		{Role: "system", Content: "sys"},
		{Role: "user", Content: "价格"},
		{Role: "assistant", ToolCalls: []domain.ToolCall{{ID: "c0", Name: "x", Arguments: "{}"}}},
		{Role: "tool", ToolCallID: "c0", Content: `{"ok":true}`},
	}
	res, err := c.ChatWith(context.Background(), msgs, domain.ChatOptions{Tools: []domain.ToolSpec{{Name: "get_model_price", Parameters: map[string]any{"type": "object"}}}, DisableWebSearch: true, MaxOutputTokens: 100})
	if err != nil || len(res.ToolCalls) != 1 || res.ToolCalls[0].ID != "c1" {
		t.Fatalf("res=%+v err=%v", res, err)
	}
	raw, _ := json.Marshal(got)
	s := string(raw)
	for _, want := range []string{`"type":"function_call"`, `"type":"function_call_output"`, `"call_id":"c0"`, `"name":"get_model_price"`, `"max_output_tokens":100`} {
		if !strings.Contains(s, want) {
			t.Fatalf("payload missing %s: %s", want, s)
		}
	}
	if strings.Contains(s, "web_search") {
		t.Fatalf("web search should be disabled: %s", s)
	}
}

func TestCompletionsToolCallsAndMaxTokensFallback(t *testing.T) {
	calls := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		b, _ := io.ReadAll(r.Body)
		if strings.Contains(string(b), `"max_tokens"`) {
			w.WriteHeader(400)
			w.Write([]byte(`{"error":"Unsupported parameter: 'max_tokens'"}`))
			return
		}
		if !strings.Contains(string(b), `"max_completion_tokens":50`) || !strings.Contains(string(b), `"tool_call_id":"t1"`) {
			t.Errorf("unexpected body %s", b)
		}
		w.Write([]byte(`{"choices":[{"message":{"content":null,"tool_calls":[{"id":"t2","type":"function","function":{"name":"f","arguments":"{}"}}]}}]}`))
	}))
	defer srv.Close()
	c := New(srv.URL, "k", "m", 5*time.Second)
	res, err := c.ChatWith(context.Background(), []domain.ChatMessage{{Role: "user", Content: "hi"}, {Role: "assistant", ToolCalls: []domain.ToolCall{{ID: "t1", Name: "f", Arguments: "{}"}}}, {Role: "tool", ToolCallID: "t1", Content: "ok"}}, domain.ChatOptions{MaxOutputTokens: 50, Tools: []domain.ToolSpec{{Name: "f", Parameters: map[string]any{"type": "object"}}}})
	if err != nil || calls != 2 || len(res.ToolCalls) != 1 || res.ToolCalls[0].Name != "f" {
		t.Fatalf("res=%+v err=%v calls=%d", res, err, calls)
	}
}
