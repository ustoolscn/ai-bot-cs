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
	type msg struct {
		Role    string `json:"role"`
		Content string `json:"content"`
	}
	req := struct {
		Model       string  `json:"model"`
		Messages    []msg   `json:"messages"`
		Temperature float64 `json:"temperature"`
	}{Model: c.Model, Temperature: 0.3}
	for _, m := range messages {
		req.Messages = append(req.Messages, msg{m.Role, m.Content})
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
