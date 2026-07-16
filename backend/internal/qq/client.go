package qq

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"ai-bot/backend/internal/domain"
)

type Client struct {
	AppID, AppSecret, APIBase, TokenURL string
	HTTP                                *http.Client
}

func NewClient(appID, secret, apiBase, tokenURL string) *Client {
	return &Client{AppID: appID, AppSecret: secret, APIBase: strings.TrimRight(apiBase, "/"), TokenURL: tokenURL, HTTP: &http.Client{Timeout: 20 * time.Second}}
}
func (c *Client) token(ctx context.Context) (string, error) {
	b, _ := json.Marshal(map[string]string{"appId": c.AppID, "clientSecret": c.AppSecret})
	req, _ := http.NewRequestWithContext(ctx, http.MethodPost, c.TokenURL, bytes.NewReader(b))
	req.Header.Set("Content-Type", "application/json")
	r, err := c.HTTP.Do(req)
	if err != nil {
		return "", err
	}
	defer r.Body.Close()
	data, _ := io.ReadAll(io.LimitReader(r.Body, 1<<20))
	if r.StatusCode/100 != 2 {
		return "", fmt.Errorf("QQ token status %d", r.StatusCode)
	}
	var out struct {
		AccessToken string `json:"access_token"`
	}
	if err := json.Unmarshal(data, &out); err != nil {
		return "", err
	}
	if out.AccessToken == "" {
		return "", fmt.Errorf("empty QQ access token")
	}
	return out.AccessToken, nil
}
func (c *Client) Send(ctx context.Context, m domain.OutboundMessage) (string, error) {
	token, err := c.token(ctx)
	if err != nil {
		return "", err
	}
	var prefix string
	if m.ConversationType == "private" {
		prefix = "/v2/users/" + url.PathEscape(m.ConversationID)
	} else {
		prefix = "/v2/groups/" + url.PathEscape(m.ConversationID)
	}
	payload := map[string]any{"content": m.Text, "msg_type": 0, "msg_id": m.ReplyToMessageID, "msg_seq": m.Sequence}
	if image := firstImage(m.Parts); image != nil {
		fileInfo, err := c.uploadImage(ctx, token, prefix+"/files", *image)
		if err != nil {
			return "", err
		}
		payload["msg_type"] = 7
		payload["media"] = map[string]any{"file_info": fileInfo}
		if strings.TrimSpace(m.Text) == "" {
			payload["content"] = "图片"
		}
	}
	b, _ := json.Marshal(payload)
	req, _ := http.NewRequestWithContext(ctx, http.MethodPost, c.APIBase+prefix+"/messages", bytes.NewReader(b))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "QQBot "+token)
	r, err := c.HTTP.Do(req)
	if err != nil {
		return "", err
	}
	defer r.Body.Close()
	data, _ := io.ReadAll(io.LimitReader(r.Body, 1<<20))
	if r.StatusCode/100 != 2 {
		return "", fmt.Errorf("QQ send status %d: %s", r.StatusCode, string(data))
	}
	var out struct {
		ID string `json:"id"`
	}
	_ = json.Unmarshal(data, &out)
	return out.ID, nil
}

func firstImage(parts []domain.ContentPart) *domain.ContentPart {
	for index := range parts {
		if parts[index].Type == "image" {
			return &parts[index]
		}
	}
	return nil
}

func (c *Client) uploadImage(ctx context.Context, token, path string, image domain.ContentPart) (string, error) {
	payload := map[string]any{"file_type": 1}
	if len(image.Data) > 0 {
		payload["file_data"] = base64.StdEncoding.EncodeToString(image.Data)
	} else if strings.HasPrefix(image.URL, "https://") {
		payload["url"] = image.URL
	} else {
		return "", fmt.Errorf("QQ 图片发送需要 HTTPS URL 或本地图片数据")
	}
	b, _ := json.Marshal(payload)
	req, _ := http.NewRequestWithContext(ctx, http.MethodPost, c.APIBase+path, bytes.NewReader(b))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "QQBot "+token)
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode/100 != 2 {
		return "", fmt.Errorf("QQ media upload status %d: %s", resp.StatusCode, string(data))
	}
	var out struct {
		FileInfo string `json:"file_info"`
	}
	if err := json.Unmarshal(data, &out); err != nil || out.FileInfo == "" {
		return "", fmt.Errorf("QQ media upload returned empty file_info")
	}
	return out.FileInfo, nil
}
