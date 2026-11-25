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

	"ai-bot/backend/internal/domain"
	"ai-bot/backend/internal/openai"
)

func (a *App) generateImage(ctx context.Context, cl *openai.Client, prompt string) (domain.ContentPart, error) {
	var empty domain.ContentPart
	data, contentType, err := a.requestImage(ctx, cl, prompt)
	if err != nil {
		return empty, err
	}
	name := "generated" + imageExtension(contentType)
	stored, err := a.images.Save(ctx, name, data)
	if err != nil {
		return empty, err
	}
	return domain.ContentPart{Type: "image", Filename: name, ContentType: contentType, StorageKey: stored, SizeBytes: int64(len(data))}, nil
}

func (a *App) requestImage(ctx context.Context, cl *openai.Client, prompt string) ([]byte, string, error) {
	if prompt == "" {
		return nil, "", errors.New("请提供生图描述")
	}
	body, err := json.Marshal(map[string]any{"model": cl.Model, "prompt": prompt, "n": 1})
	if err != nil {
		return nil, "", err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, cl.BaseURL+"/images/generations", bytes.NewReader(body))
	if err != nil {
		return nil, "", err
	}
	req.Header.Set("Authorization", "Bearer "+cl.APIKey)
	req.Header.Set("Content-Type", "application/json")
	client := *cl.HTTP
	client.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	res, err := client.Do(req)
	if err != nil {
		return nil, "", fmt.Errorf("生图请求失败: %w", err)
	}
	defer res.Body.Close()
	if res.StatusCode/100 != 2 {
		return nil, "", fmt.Errorf("生图接口返回 HTTP %d", res.StatusCode)
	}
	const limit = (maxInboundImageSize+2)/3*4 + 65536
	raw, err := io.ReadAll(io.LimitReader(res.Body, limit+1))
	if err != nil {
		return nil, "", err
	}
	if len(raw) > limit {
		return nil, "", errors.New("生图响应过大")
	}
	var result struct {
		Data []struct {
			Base64 string `json:"b64_json"`
			URL    string `json:"url"`
		} `json:"data"`
	}
	if json.Unmarshal(raw, &result) != nil || len(result.Data) == 0 {
		return nil, "", errors.New("生图接口未返回图片")
	}
	item := result.Data[0]
	var data []byte
	if item.Base64 != "" {
		data, err = base64.StdEncoding.DecodeString(item.Base64)
	} else if item.URL != "" {
		data, _, _, err = downloadImage(ctx, a.imageHTTP, item.URL)
	} else {
		return nil, "", errors.New("生图响应缺少 b64_json 或 url")
	}
	if err != nil {
		return nil, "", errors.New("无法读取生成的图片")
	}
	contentType := http.DetectContentType(data)
	ext := imageExtension(contentType)
	if ext == "" || len(data) > maxInboundImageSize {
		return nil, "", errors.New("生成的图片格式无效或超过 10 MB")
	}
	return data, contentType, nil
}

type imageLimitError int

func (e imageLimitError) Error() string {
	return fmt.Sprintf("你今天在本会话的生图次数已用完（每日限额 %d 张），明天再来吧。", e)
}

func (a *App) generateLimitedImage(ctx context.Context, cid, uid, profile, prompt string) (domain.ContentPart, error) {
	var part domain.ContentPart
	cl, kind, err := a.loadModel(ctx, profile)
	if err != nil || kind != "image" {
		return part, errors.New("客服未选择可用的生图模型")
	}
	tx, err := a.db.Begin(ctx)
	if err != nil {
		return part, err
	}
	defer tx.Rollback(ctx)
	const day = "(now() AT TIME ZONE 'Asia/Shanghai')::date"
	_, err = tx.Exec(ctx, `INSERT INTO image_daily_usage(conversation_id,user_id,day) VALUES($1,$2,`+day+`) ON CONFLICT DO NOTHING`, cid, uid)
	if err != nil {
		return part, err
	}
	var used, limit int
	err = tx.QueryRow(ctx, `SELECT u.used,coalesce(c.image_daily_limit,(s.value->>'imageDailyLimitDefault')::int,0) FROM image_daily_usage u JOIN conversations c ON c.id=u.conversation_id LEFT JOIN system_settings s ON s.key='runtime_defaults' WHERE u.conversation_id=$1 AND u.user_id=$2 AND u.day=`+day+` FOR UPDATE OF u`, cid, uid).Scan(&used, &limit)
	if err != nil {
		return part, err
	}
	if limit > 0 && used >= limit {
		return part, imageLimitError(limit)
	}
	started := time.Now()
	part, err = a.generateImage(ctx, cl, prompt)
	if err != nil {
		return part, err
	}
	_, err = tx.Exec(ctx, `UPDATE image_daily_usage SET used=used+1 WHERE conversation_id=$1 AND user_id=$2 AND day=`+day, cid, uid)
	if err != nil {
		return part, err
	}
	err = tx.Commit(ctx)
	a.recordModelCall(ctx, "", "", profile, "image", domain.ChatResult{}, time.Since(started).Milliseconds(), nil)
	return part, err
}
