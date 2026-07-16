package openai

import (
	"bytes"
	"context"
	"encoding/json"
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
		return fmt.Errorf("model API status %d: %s", resp.StatusCode, strings.TrimSpace(string(data)))
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
	type msg struct {
		Role    string `json:"role"`
		Content string `json:"content"`
	}
	chatMessages := make([]msg, 0, len(messages))
	for _, m := range messages {
		chatMessages = append(chatMessages, msg{m.Role, m.Content})
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
			Message msg `json:"message"`
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
		Content string `json:"content"`
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
		input = append(input, inputMessage{Role: role, Content: message.Content})
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
