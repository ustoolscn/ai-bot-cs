package openai

import (
	"context"
	"fmt"

	"ai-bot/backend/internal/domain"
)

type googlePart struct {
	Text   string      `json:"text,omitempty"`
	Inline *googleBlob `json:"inlineData,omitempty"`
}
type googleBlob struct {
	MIME string `json:"mimeType"`
	Data []byte `json:"data"`
}
type googleContent struct {
	Parts []googlePart `json:"parts"`
}
type googleRequest struct {
	Model      string        `json:"model"`
	Content    googleContent `json:"content"`
	Dimensions int           `json:"outputDimensionality,omitempty"`
}
type googleUsage struct{ PromptTokenCount int }
type googleVector struct {
	Values []float32 `json:"values"`
}

func (c *Client) googleRequest(parts []googlePart) googleRequest {
	return googleRequest{"models/" + c.Model, googleContent{parts}, c.Dimensions}
}

func (c *Client) GoogleTexts(ctx context.Context, texts []string, document bool) ([][]float32, domain.ChatResult, error) {
	requests := make([]googleRequest, 0, len(texts))
	for _, s := range texts {
		prefix := "task: search result | query: "
		if document {
			prefix = "title: none | text: "
		}
		requests = append(requests, c.googleRequest([]googlePart{{Text: prefix + s}}))
	}
	var resp struct {
		Embeddings    []googleVector
		UsageMetadata googleUsage
	}
	err := c.do(ctx, "/models/"+c.Model+":batchEmbedContents", map[string]any{"requests": requests}, &resp)
	usage := domain.ChatResult{InputTokens: resp.UsageMetadata.PromptTokenCount}
	if err != nil {
		return nil, usage, err
	}
	if len(resp.Embeddings) != len(texts) {
		return nil, usage, fmt.Errorf("Google 返回的向量数量异常")
	}
	out := make([][]float32, len(texts))
	for i, v := range resp.Embeddings {
		if len(v.Values) == 0 || (c.Dimensions > 0 && len(v.Values) != c.Dimensions) {
			return nil, usage, fmt.Errorf("Google 返回的向量维度异常")
		}
		out[i] = v.Values
	}
	return out, usage, nil
}

func (c *Client) GoogleImage(ctx context.Context, text, mime string, data []byte) ([]float32, domain.ChatResult, error) {
	parts := []googlePart{}
	if text != "" {
		parts = append(parts, googlePart{Text: text})
	}
	parts = append(parts, googlePart{Inline: &googleBlob{mime, data}})
	var resp struct {
		Embedding     googleVector
		UsageMetadata googleUsage
	}
	err := c.do(ctx, "/models/"+c.Model+":embedContent", c.googleRequest(parts), &resp)
	if err == nil && len(resp.Embedding.Values) == 0 {
		err = fmt.Errorf("Google 返回空图片向量")
	}
	return resp.Embedding.Values, domain.ChatResult{InputTokens: resp.UsageMetadata.PromptTokenCount}, err
}
