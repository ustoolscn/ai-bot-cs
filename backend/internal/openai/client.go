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
	if c.UseResponses {
		return c.responsesChat(ctx, messages)
	}
	return c.completionsChat(ctx, messages)
}

func (c *Client) completionsChat(ctx context.Context, messages []domain.ChatMessage) (domain.ChatResult, error) {
	type requestMsg struct {
		Role    string `json:"role"`
		Content any    `json:"content"`
	}
	type responseMsg struct {
		Role    string `json:"role"`
		Content string `json:"content"`
	}
	chatMessages := make([]requestMsg, 0, len(messages))
	for _, m := range messages {
		content := any(m.Content)
		if len(m.Parts) > 0 {
			content = completionContent(m)
		}
		chatMessages = append(chatMessages, requestMsg{Role: m.Role, Content: content})
	}
	req := map[string]any{"model": c.Model, "messages": chatMessages, "temperature": 0.3}
	for key, value := range c.ExtraBody {
		if key == "model" || key == "messages" || key == "reasoning_effort" {
			continue
		}
		req[key] = value
	}
	if c.ReasoningEffort != "" && c.ReasoningEffort != "default" {
		req["reasoning_effort"] = c.ReasoningEffort
	}
	var resp struct {
		Choices []struct {
			Message responseMsg `json:"message"`
		} `json:"choices"`
		Usage struct {
			Prompt     int `json:"prompt_tokens"`
			Completion int `json:"completion_tokens"`
		} `json:"usage"`
	}
	if err := c.do(ctx, "/chat/completions", req, &resp); err != nil {
		return domain.ChatResult{}, err
	}
	if len(resp.Choices) == 0 {
		return domain.ChatResult{}, fmt.Errorf("model returned no choices")
	}
	return domain.ChatResult{Content: resp.Choices[0].Message.Content, InputTokens: resp.Usage.Prompt, OutputTokens: resp.Usage.Completion}, nil
}

func (c *Client) responsesChat(ctx context.Context, messages []domain.ChatMessage) (domain.ChatResult, error) {
	type inputMessage struct {
		Role    string `json:"role"`
		Content any    `json:"content"`
	}
	input := make([]inputMessage, 0, len(messages))
	instructions := make([]string, 0, 2)
	for _, message := range messages {
		if message.Role == "system" || message.Role == "developer" {
			if strings.TrimSpace(message.Content) != "" {
				instructions = append(instructions, message.Content)
			}
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
		input = append(input, inputMessage{Role: role, Content: content})
	}
	payload := map[string]any{
		"model": c.Model,
		"input": input,
		"tools": []map[string]any{{"type": "web_search"}},
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
	if c.ReasoningEffort != "" && c.ReasoningEffort != "default" {
		payload["reasoning"] = map[string]any{"effort": c.ReasoningEffort}
	}
	var resp struct {
		OutputText string `json:"output_text"`
		Output     []struct {
			Type    string `json:"type"`
			Content []struct {
				Type string `json:"type"`
				Text string `json:"text"`
			} `json:"content"`
		} `json:"output"`
		Usage struct {
			InputTokens  int `json:"input_tokens"`
			OutputTokens int `json:"output_tokens"`
		} `json:"usage"`
	}
	if err := c.do(ctx, "/responses", payload, &resp); err != nil {
		return domain.ChatResult{}, err
	}
	parts := make([]string, 0, 2)
	for _, output := range resp.Output {
		if output.Type != "message" {
			continue
		}
		for _, content := range output.Content {
			if content.Type == "output_text" && strings.TrimSpace(content.Text) != "" {
				parts = append(parts, content.Text)
			}
		}
	}
	content := strings.TrimSpace(strings.Join(parts, "\n"))
	if content == "" {
		content = strings.TrimSpace(resp.OutputText)
	}
	if content == "" {
		return domain.ChatResult{}, fmt.Errorf("responses API returned no output_text")
	}
	return domain.ChatResult{Content: content, InputTokens: resp.Usage.InputTokens, OutputTokens: resp.Usage.OutputTokens}, nil
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
