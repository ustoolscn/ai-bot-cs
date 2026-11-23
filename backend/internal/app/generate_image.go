package app

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"time"

	"ai-bot/backend/internal/agent"
	"ai-bot/backend/internal/domain"
)

func (a *App) generateImage(ctx context.Context, cfg agent.ImageGenerationSettings, prompt string) (domain.ContentPart, error) {
	var empty domain.ContentPart
	if prompt == "" {
		return empty, errors.New("请提供生图描述")
	}
	key, err := a.cipher.Decrypt(cfg.APIKeyEnc)
	if err != nil {
		return empty, errors.New("无法读取生图密钥")
	}
	body, err := json.Marshal(map[string]any{"model": cfg.Model, "prompt": prompt, "n": 1})
	if err != nil {
		return empty, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, cfg.BaseURL+"/images/generations", bytes.NewReader(body))
	if err != nil {
		return empty, err
	}
	req.Header.Set("Authorization", "Bearer "+key)
	req.Header.Set("Content-Type", "application/json")
	client := http.Client{Transport: directModelTransport, Timeout: 2 * time.Minute, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	res, err := client.Do(req)
	if err != nil {
		return empty, fmt.Errorf("生图请求失败: %w", err)
	}
	defer res.Body.Close()
	if res.StatusCode/100 != 2 {
		return empty, fmt.Errorf("生图接口返回 HTTP %d", res.StatusCode)
	}
	const limit = (maxInboundImageSize+2)/3*4 + 65536
	raw, err := io.ReadAll(io.LimitReader(res.Body, limit+1))
	if err != nil {
		return empty, err
	}
	if len(raw) > limit {
		return empty, errors.New("生图响应过大")
	}
	var result struct {
		Data []struct {
			Base64 string `json:"b64_json"`
			URL    string `json:"url"`
		} `json:"data"`
	}
	if json.Unmarshal(raw, &result) != nil || len(result.Data) == 0 {
		return empty, errors.New("生图接口未返回图片")
	}
	item := result.Data[0]
	var data []byte
	if item.Base64 != "" {
		data, err = base64.StdEncoding.DecodeString(item.Base64)
	} else if item.URL != "" {
		data, _, _, err = downloadImage(ctx, a.imageHTTP, item.URL)
	} else {
		return empty, errors.New("生图响应缺少 b64_json 或 url")
	}
	if err != nil {
		return empty, errors.New("无法读取生成的图片")
	}
	contentType := http.DetectContentType(data)
	ext := imageExtension(contentType)
	if ext == "" || len(data) > maxInboundImageSize {
		return empty, errors.New("生成的图片格式无效或超过 10 MB")
	}
	name := "generated" + ext
	stored, err := a.images.Save(ctx, name, data)
	if err != nil {
		return empty, err
	}
	return domain.ContentPart{Type: "image", Filename: name, ContentType: contentType, StorageKey: stored, SizeBytes: int64(len(data))}, nil
}
