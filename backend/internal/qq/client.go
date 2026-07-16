package qq

import (
	"bytes"
	"context"
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
	var path string
	if m.ConversationType == "private" {
		path = "/v2/users/" + url.PathEscape(m.ConversationID) + "/messages"
	} else {
		path = "/v2/groups/" + url.PathEscape(m.ConversationID) + "/messages"
	}
	b, _ := json.Marshal(map[string]any{"content": m.Text, "msg_type": 0, "msg_id": m.ReplyToMessageID, "msg_seq": m.Sequence})
	req, _ := http.NewRequestWithContext(ctx, http.MethodPost, c.APIBase+path, bytes.NewReader(b))
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
