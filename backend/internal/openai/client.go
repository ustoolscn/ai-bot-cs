package openai

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"ai-bot/backend/internal/domain"
)

type Client struct {
	BaseURL, APIKey, Model string
	Dimensions             int
	ExtraBody              map[string]any
	UseResponses           bool
	ReasoningEffort        string
	HTTP                   *http.Client
}

type HTTPError struct {
	StatusCode int
	Body       string
}

func (e *HTTPError) Error() string {
	return fmt.Sprintf("model API status %d: %s", e.StatusCode, e.Body)
}

func IsHTTPStatus(err error, status int) bool {
	var apiErr *HTTPError
	return errors.As(err, &apiErr) && apiErr.StatusCode == status
}

func New(baseURL, apiKey, model string, timeout time.Duration) *Client {
	return &Client{BaseURL: strings.TrimRight(baseURL, "/"), APIKey: apiKey, Model: model, HTTP: &http.Client{Timeout: timeout}}
}

func (c *Client) do(ctx context.Context, path string, payload any, out any) error {
	b, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.BaseURL+path, bytes.NewReader(b))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	if c.APIKey != "" {
		req.Header.Set("Authorization", "Bearer "+c.APIKey)
	}
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	data, readErr := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if readErr != nil {
		return fmt.Errorf("读取模型接口响应失败: %w", readErr)
	}
	if resp.StatusCode/100 != 2 {
		body := strings.TrimSpace(string(data))
		if len([]rune(body)) > 2048 {
			body = string([]rune(body)[:2048]) + "…"
		}
		return &HTTPError{StatusCode: resp.StatusCode, Body: body}
	}
	if err := json.Unmarshal(data, out); err != nil {
		snippet := strings.TrimSpace(string(data))
		if len([]rune(snippet)) > 512 {
			snippet = string([]rune(snippet)[:512]) + "…"
		}
		return fmt.Errorf("模型接口返回非 JSON（Content-Type: %s，解析错误: %v）；响应片段: %q；请检查 Base URL 是否正确，OpenAI 兼容接口通常应包含 /v1", resp.Header.Get("Content-Type"), err, snippet)
	}
	return nil
}

func (c *Client) Chat(ctx context.Context, messages []domain.ChatMessage) (domain.ChatResult, error) {
	return c.ChatWith(ctx, messages, domain.ChatOptions{})
}

// ChatWith sends a chat request with optional function tools and limits.
func (c *Client) ChatWith(ctx context.Context, messages []domain.ChatMessage, opts domain.ChatOptions) (domain.ChatResult, error) {
	if c.UseResponses {
		return c.responsesChat(ctx, messages, opts)
	}
	return c.completionsChat(ctx, messages, opts)
}

func (c *Client) completionsChat(ctx context.Context, messages []domain.ChatMessage, opts domain.ChatOptions) (domain.ChatResult, error) {
	type fnCall struct {
		Name      string `json:"name"`
		Arguments string `json:"arguments"`
	}
	type toolCall struct {
		ID       string `json:"id"`
		Type     string `json:"type"`
		Function fnCall `json:"function"`
	}
	type requestMsg struct {
		Role       string     `json:"role"`
		Content    any        `json:"content"`
		ToolCalls  []toolCall `json:"tool_calls,omitempty"`
		ToolCallID string     `json:"tool_call_id,omitempty"`
	}
	type responseMsg struct {
		Annotations []struct {
			Type string `json:"type"`
		} `json:"annotations"`
		Role      string     `json:"role"`
		Content   string     `json:"content"`
		ToolCalls []toolCall `json:"tool_calls"`
	}
	chatMessages := make([]requestMsg, 0, len(messages))
	for _, m := range messages {
		content := any(m.Content)
		if len(m.Parts) > 0 {
			content = completionContent(m)
		}
		rm := requestMsg{Role: m.Role, Content: content, ToolCallID: m.ToolCallID}
		for _, tc := range m.ToolCalls {
			rm.ToolCalls = append(rm.ToolCalls, toolCall{ID: tc.ID, Type: "function", Function: fnCall{Name: tc.Name, Arguments: tc.Arguments}})
		}
		chatMessages = append(chatMessages, rm)
	}
	req := map[string]any{"model": c.Model, "messages": chatMessages, "temperature": 0.3}
	for key, value := range c.ExtraBody {
		if key == "model" || key == "messages" || key == "reasoning_effort" || key == "tools" {
			continue
		}
		req[key] = value
	}
	if c.ReasoningEffort != "" && c.ReasoningEffort != "default" {
		req["reasoning_effort"] = c.ReasoningEffort
	}
	if len(opts.Tools) > 0 {
		tools := make([]map[string]any, 0, len(opts.Tools))
		for _, t := range opts.Tools {
			tools = append(tools, map[string]any{"type": "function", "function": map[string]any{"name": t.Name, "description": t.Description, "parameters": t.Parameters}})
		}
		req["tools"] = tools
	}
	if opts.DisableWebSearch {
		delete(req, "enable_search")
		delete(req, "web_search_options")
		delete(req, "search_options")
	}
	if opts.MaxOutputTokens > 0 {
		req["max_tokens"] = opts.MaxOutputTokens
	}
	if opts.RequireWebSearch {
		if req["enable_search"] == true {
			req["search_options"] = map[string]any{"forced_search": true, "enable_source": true}
		} else if req["web_search_options"] == nil {
			return domain.ChatResult{}, fmt.Errorf("请为此客服配置支持联网检索的对话模型")
		}
	}
	var resp struct {
		SearchInfo struct {
			Results []json.RawMessage `json:"search_results"`
		} `json:"search_info"`
		Choices []struct {
			Message responseMsg `json:"message"`
		} `json:"choices"`
		Usage tokenUsage `json:"usage"`
	}
	err := c.do(ctx, "/chat/completions", req, &resp)
	if err != nil && opts.MaxOutputTokens > 0 && IsHTTPStatus(err, 400) && strings.Contains(err.Error(), "max_tokens") {
		delete(req, "max_tokens")
		req["max_completion_tokens"] = opts.MaxOutputTokens
		err = c.do(ctx, "/chat/completions", req, &resp)
	}
	if err != nil {
		return domain.ChatResult{}, err
	}
	if len(resp.Choices) == 0 {
		return domain.ChatResult{}, fmt.Errorf("model returned no choices")
	}
	msg := resp.Choices[0].Message
	if opts.RequireWebSearch && len(resp.SearchInfo.Results) == 0 {
		cited := false
		for _, a := range msg.Annotations {
			if a.Type == "url_citation" {
				cited = true
				break
			}
		}
		if !cited {
			return domain.ChatResult{}, fmt.Errorf("联网模型未返回检索来源")
		}
	}
	result := resp.Usage.result()
	result.Content = msg.Content
	for _, tc := range msg.ToolCalls {
		if tc.Function.Name == "" {
			continue
		}
		result.ToolCalls = append(result.ToolCalls, domain.ToolCall{ID: tc.ID, Name: tc.Function.Name, Arguments: tc.Function.Arguments})
	}
	return result, nil
}

func (c *Client) responsesChat(ctx context.Context, messages []domain.ChatMessage, opts domain.ChatOptions) (domain.ChatResult, error) {
	input := make([]map[string]any, 0, len(messages))
	instructions := make([]string, 0, 2)
	for _, message := range messages {
		switch message.Role {
		case "system", "developer":
			if strings.TrimSpace(message.Content) != "" {
				instructions = append(instructions, message.Content)
			}
			continue
		case "tool":
			input = append(input, map[string]any{"type": "function_call_output", "call_id": message.ToolCallID, "output": message.Content})
			continue
		}
		role := message.Role
		if role != "assistant" {
			role = "user"
		}
		content := any(message.Content)
		if len(message.Parts) > 0 {
			content = responsesContent(message)
		}
		if role != "assistant" || strings.TrimSpace(message.Content) != "" || len(message.Parts) > 0 {
			input = append(input, map[string]any{"role": role, "content": content})
		}
		for _, tc := range message.ToolCalls {
			input = append(input, map[string]any{"type": "function_call", "call_id": tc.ID, "name": tc.Name, "arguments": tc.Arguments})
		}
	}
	tools := []map[string]any{}
	if !opts.DisableWebSearch {
		tools = append(tools, map[string]any{"type": "web_search"})
	}
	for _, t := range opts.Tools {
		tools = append(tools, map[string]any{"type": "function", "name": t.Name, "description": t.Description, "parameters": t.Parameters})
	}
	payload := map[string]any{
		"model": c.Model,
		"input": input,
	}
	if len(tools) > 0 {
		payload["tools"] = tools
	}
	if len(instructions) > 0 {
		payload["instructions"] = strings.Join(instructions, "\n\n")
	}
	for key, value := range c.ExtraBody {
		if key == "model" || key == "input" || key == "instructions" || key == "tools" || key == "reasoning" {
			continue
		}
		payload[key] = value
	}
	if opts.RequireWebSearch {
		payload["tool_choice"] = "required"
	}
	if c.ReasoningEffort != "" && c.ReasoningEffort != "default" {
		payload["reasoning"] = map[string]any{"effort": c.ReasoningEffort}
	}
	if opts.MaxOutputTokens > 0 {
		payload["max_output_tokens"] = opts.MaxOutputTokens
	}
	var resp struct {
		OutputText string `json:"output_text"`
		Output     []struct {
			Status    string `json:"status"`
			Type      string `json:"type"`
			CallID    string `json:"call_id"`
			Name      string `json:"name"`
			Arguments string `json:"arguments"`
			Content   []struct {
				Type string `json:"type"`
				Text string `json:"text"`
			} `json:"content"`
		} `json:"output"`
		Usage tokenUsage `json:"usage"`
	}
	if err := c.do(ctx, "/responses", payload, &resp); err != nil {
		return domain.ChatResult{}, err
	}
	parts := make([]string, 0, 2)
	var calls []domain.ToolCall
	searched := false
	for _, output := range resp.Output {
		if output.Type == "web_search_call" && output.Status == "completed" {
			searched = true
		}
		if output.Type == "function_call" && output.Name != "" {
			calls = append(calls, domain.ToolCall{ID: output.CallID, Name: output.Name, Arguments: output.Arguments})
			continue
		}
		if output.Type != "message" {
			continue
		}
		for _, content := range output.Content {
			if content.Type == "output_text" && strings.TrimSpace(content.Text) != "" {
				parts = append(parts, content.Text)
			}
		}
	}
	if opts.RequireWebSearch && !searched {
		return domain.ChatResult{}, fmt.Errorf("联网模型未完成检索")
	}
	content := strings.TrimSpace(strings.Join(parts, "\n"))
	if content == "" {
		content = strings.TrimSpace(resp.OutputText)
	}
	if content == "" && len(calls) == 0 {
		return domain.ChatResult{}, fmt.Errorf("responses API returned no output_text")
	}
	result := resp.Usage.result()
	result.Content, result.ToolCalls = content, calls
	return result, nil
}

func completionContent(message domain.ChatMessage) []map[string]any {
	parts := make([]map[string]any, 0, len(message.Parts)+1)
	if strings.TrimSpace(message.Content) != "" {
		parts = append(parts, map[string]any{"type": "text", "text": message.Content})
	}
	for _, part := range message.Parts {
		if part.Type == "text" && strings.TrimSpace(part.Text) != "" {
			parts = append(parts, map[string]any{"type": "text", "text": part.Text})
			continue
		}
		if part.Type != "image" {
			continue
		}
		imageURL := part.DataURL
		if imageURL == "" {
			imageURL = part.URL
		}
		if imageURL != "" {
			detail := part.Detail
			if detail == "" {
				detail = "auto"
			}
			parts = append(parts, map[string]any{"type": "image_url", "image_url": map[string]any{"url": imageURL, "detail": detail}})
		}
	}
	return parts
}

func responsesContent(message domain.ChatMessage) []map[string]any {
	parts := make([]map[string]any, 0, len(message.Parts)+1)
	if strings.TrimSpace(message.Content) != "" {
		parts = append(parts, map[string]any{"type": "input_text", "text": message.Content})
	}
	for _, part := range message.Parts {
		if part.Type == "text" && strings.TrimSpace(part.Text) != "" {
			parts = append(parts, map[string]any{"type": "input_text", "text": part.Text})
			continue
		}
		if part.Type != "image" {
			continue
		}
		imageURL := part.DataURL
		if imageURL == "" {
			imageURL = part.URL
		}
		if imageURL != "" {
			detail := part.Detail
			if detail == "" {
				detail = "auto"
			}
			parts = append(parts, map[string]any{"type": "input_image", "image_url": imageURL, "detail": detail})
		}
	}
	return parts
}

func (c *Client) Embed(ctx context.Context, texts []string) ([][]float32, error) {
	var resp struct {
		Data []struct {
			Index     int       `json:"index"`
			Embedding []float32 `json:"embedding"`
		} `json:"data"`
	}
	payload := map[string]any{"model": c.Model, "input": texts}
	if c.Dimensions > 0 {
		payload["dimensions"] = c.Dimensions
	}
	if err := c.do(ctx, "/embeddings", payload, &resp); err != nil {
		return nil, err
	}
	out := make([][]float32, len(texts))
	for _, d := range resp.Data {
		if d.Index >= 0 && d.Index < len(out) {
			out[d.Index] = d.Embedding
		}
	}
	for i := range out {
		if len(out[i]) == 0 {
			return nil, fmt.Errorf("missing embedding %d", i)
		}
	}
	return out, nil
}

type cacheUsage struct {
	Cached        int `json:"cached_tokens"`
	Write         int `json:"cache_write_tokens"`
	Creation      int `json:"cache_creation_tokens"`
	CreationInput int `json:"cache_creation_input_tokens"`
}

type tokenUsage struct {
	Prompt        *int       `json:"prompt_tokens"`
	Completion    int        `json:"completion_tokens"`
	Input         int        `json:"input_tokens"`
	Output        int        `json:"output_tokens"`
	PromptDetails cacheUsage `json:"prompt_tokens_details"`
	InputDetails  cacheUsage `json:"input_tokens_details"`
	CacheHit      int        `json:"prompt_cache_hit_tokens"`
	CacheRead     int        `json:"cache_read_input_tokens"`
	CacheWrite    int        `json:"cache_creation_input_tokens"`
	WriteTokens   int        `json:"cache_write_tokens"`
}

func (u tokenUsage) result() domain.ChatResult {
	input, output := u.Input, u.Output
	if u.Prompt != nil {
		input, output = *u.Prompt, u.Completion
	} else if u.CacheRead+u.CacheWrite > 0 {
		// Native Anthropic usage reports ordinary input separately from cache usage.
		input += u.CacheRead + u.CacheWrite
	}
	read := min(input, max(u.CacheHit, u.CacheRead, u.PromptDetails.Cached, u.InputDetails.Cached))
	write := min(input-read, max(u.CacheWrite, u.WriteTokens, u.InputDetails.CreationInput, u.PromptDetails.CreationInput, u.PromptDetails.Write, u.PromptDetails.Creation, u.InputDetails.Write, u.InputDetails.Creation))
	return domain.ChatResult{InputTokens: input, OutputTokens: output, CachedInputTokens: read, CacheWriteTokens: write}
}
