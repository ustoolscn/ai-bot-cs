package qq

import (
	"bytes"
	"context"

	"encoding/json"
	"fmt"
	"html"
	"io"
	"net/http"
	"net/url"
	"path"
	"strconv"
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
	key := credentialKey(c)
	entry := cachedToken(key)
	entry.mu.Lock()
	defer entry.mu.Unlock()
	if time.Until(entry.until) > time.Minute {
		return entry.value, nil
	}
	b, _ := json.Marshal(map[string]string{"appId": c.AppID, "clientSecret": c.AppSecret})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.TokenURL, bytes.NewReader(b))
	if err != nil {
		return "", err
	}
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
		AccessToken string          `json:"access_token"`
		Expires     json.RawMessage `json:"expires_in"`
		Code        int
		Message     string
	}
	if err := json.Unmarshal(data, &out); err != nil {
		return "", err
	}
	if out.Code != 0 {
		return "", fmt.Errorf("QQ token error %d: %s", out.Code, out.Message)
	}
	if out.AccessToken == "" {
		return "", fmt.Errorf("empty QQ access token")
	}
	seconds, _ := strconv.Atoi(strings.Trim(string(out.Expires), `"`))
	if seconds > 0 && seconds <= 86400 {
		entry.value = out.AccessToken
		entry.until = time.Now().Add(time.Duration(seconds) * time.Second)
	}
	return out.AccessToken, nil
}

type Receipt struct {
	ID        string `json:"id"`
	Timestamp string `json:"timestamp"`
	Ext       struct {
		Index string `json:"ref_idx"`
	} `json:"ext_info"`
}

func (c *Client) Send(ctx context.Context, m domain.OutboundMessage) (string, error) {
	receipt, err := c.SendWithReceipt(ctx, m)
	return receipt.ID, err
}
func (c *Client) SendWithReceipt(ctx context.Context, m domain.OutboundMessage) (Receipt, error) {
	token, err := c.token(ctx)
	if err != nil {
		return Receipt{}, err
	}
	var prefix string
	if m.ConversationType == "private" {
		prefix = "/v2/users/" + url.PathEscape(m.ConversationID)
	} else {
		prefix = "/v2/groups/" + url.PathEscape(m.ConversationID)
	}
	payload := textPayload(m)
	if m.Format == "markdown" {
		payload["msg_type"] = 2
		content := m.Text
		if m.ConversationType == "group" && m.ReferenceMessageID == "" && m.QuotedText != "" {
			content = "回复「" + html.EscapeString(m.QuotedText) + "」\n" + content
		}
		if m.ConversationType == "group" && m.MentionUserID != "" {
			content = `<qqbot-at-user id="` + html.EscapeString(m.MentionUserID) + `" /> ` + content
		}
		payload["markdown"] = map[string]any{"content": content, "force_verify_image_resource": strings.Contains(content, "![")}
		delete(payload, "content")
	}
	isAck := hasPartType(m.Parts, "ark_ack")
	if isAck {
		payload["msg_type"] = 3
		payload["ark"] = processingArk()
		delete(payload, "markdown")
	} else if image := firstImage(m.Parts); image != nil && m.Format != "markdown" {
		fileInfo, err := c.uploadImage(ctx, token, prefix+"/files", *image)
		if err != nil {
			return Receipt{}, err
		}
		payload["msg_type"] = 7
		payload["media"] = map[string]any{"file_info": fileInfo}
		if m.Format == "image-only" {
			delete(payload, "content")
		} else if strings.TrimSpace(m.Text) == "" {
			payload["content"] = "图片"
		}
		delete(payload, "markdown")
	}
	id, err := c.sendPayload(ctx, token, prefix+"/messages", payload)
	if err != nil && isAck {
		fallback := textPayload(m)
		fallback["content"] = "👀"
		fallbackID, fallbackErr := c.sendPayload(ctx, token, prefix+"/messages", fallback)
		if fallbackErr != nil {
			return Receipt{}, fmt.Errorf("QQ ARK send failed (%v); text fallback failed: %w", err, fallbackErr)
		}
		return fallbackID, nil
	}
	if err != nil && m.Format == "markdown" {
		fallback := textPayload(m)
		fallbackID, fallbackErr := c.sendPayload(ctx, token, prefix+"/messages", fallback)
		if fallbackErr != nil {
			return Receipt{}, fmt.Errorf("QQ markdown send failed (%v); text fallback failed: %w", err, fallbackErr)
		}
		return fallbackID, nil
	}
	return id, err
}

// An absent reply ID explicitly selects active sending, including text fallbacks.
func textPayload(m domain.OutboundMessage) map[string]any {
	text := m.Text
	if m.ConversationType == "group" && m.MentionUserID != "" {
		name := m.MentionUserName
		if name == "" {
			name = m.MentionUserID
		}
		text = "@" + name + " " + text
	}
	if m.ConversationType == "group" && m.ReferenceMessageID == "" && m.QuotedText != "" {
		text = "回复「" + m.QuotedText + "」\n" + text
	}
	payload := map[string]any{"content": text, "msg_type": 0}
	if m.ReferenceMessageID != "" {
		payload["message_reference"] = map[string]any{"message_id": m.ReferenceMessageID}
	}
	if m.ReplyToMessageID != "" {
		payload["msg_id"] = m.ReplyToMessageID
		payload["msg_seq"] = m.Sequence
	}
	return payload
}

func (c *Client) Retract(ctx context.Context, conversationType, conversationID, messageID string) error {
	return c.retract(ctx, conversationType, conversationID, messageID, true)
}

func (c *Client) RetractMemberMessage(ctx context.Context, group, messageID string) error {
	return c.retract(ctx, "group", group, messageID, false)
}

func (c *Client) retract(ctx context.Context, conversationType, conversationID, messageID string, allowMissing bool) error {
	token, err := c.token(ctx)
	if err != nil {
		return err
	}
	prefix := "/v2/groups/" + url.PathEscape(conversationID)
	if conversationType == "private" {
		prefix = "/v2/users/" + url.PathEscape(conversationID)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodDelete, c.APIBase+prefix+"/messages/"+url.PathEscape(messageID), nil)
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "QQBot "+token)
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if allowMissing && resp.StatusCode == http.StatusNotFound {
		return nil
	}
	if resp.StatusCode/100 != 2 {
		return fmt.Errorf("QQ retract status %d: %s", resp.StatusCode, string(data))
	}
	return nil
}

func (c *Client) sendPayload(ctx context.Context, token, path string, payload map[string]any) (Receipt, error) {
	b, _ := json.Marshal(payload)
	req, _ := http.NewRequestWithContext(ctx, http.MethodPost, c.APIBase+path, bytes.NewReader(b))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "QQBot "+token)
	r, err := c.HTTP.Do(req)
	if err != nil {
		return Receipt{}, err
	}
	defer r.Body.Close()
	data, _ := io.ReadAll(io.LimitReader(r.Body, 1<<20))
	if r.StatusCode/100 != 2 {
		return Receipt{}, fmt.Errorf("QQ send status %d: %s", r.StatusCode, string(data))
	}
	var out struct {
		Receipt
		Code    int    `json:"code"`
		Message string `json:"message"`
	}
	if err := json.Unmarshal(data, &out); err != nil {
		return Receipt{}, fmt.Errorf("QQ send returned invalid JSON: %w", err)
	}
	if out.ID == "" || out.Code != 0 {
		return Receipt{}, fmt.Errorf("QQ send returned no message id (code=%d message=%s)", out.Code, out.Message)
	}
	return out.Receipt, nil
}

func hasPartType(parts []domain.ContentPart, partType string) bool {
	for _, part := range parts {
		if part.Type == partType {
			return true
		}
	}
	return false
}

func processingArk() map[string]any {
	return map[string]any{
		"template_id": 23,
		"kv": []map[string]any{
			{"key": "#DESC#", "value": "👀"},
			{"key": "#PROMPT#", "value": "👀 正在处理"},
			{
				"key": "#LIST#",
				"obj": []map[string]any{
					{"obj_kv": []map[string]string{{"key": "desc", "value": "已收到消息，正在思考…"}}},
				},
			},
		},
	}
}

func firstImage(parts []domain.ContentPart) *domain.ContentPart {
	for index := range parts {
		if parts[index].Type == "image" {
			return &parts[index]
		}
	}
	return nil
}

func pathBase(s string) string {
	s = path.Base(strings.ReplaceAll(strings.TrimSpace(s), "\\", "/"))
	if s == "." || s == ".." || s == "/" {
		return ""
	}
	return s
}
func (c *Client) uploadImage(ctx context.Context, token, path string, image domain.ContentPart) (string, error) {
	var reader io.ReaderAt
	name := pathBase(image.Filename)
	if len(image.Data) > 0 {
		reader = bytes.NewReader(image.Data)
		if name == "" {
			name = "image.png"
			if http.DetectContentType(image.Data) == "image/jpeg" {
				name = "image.jpg"
			}
		}
	}
	out, err := c.upload(ctx, token, strings.TrimSuffix(path, "/files"), name, image.URL, 1, reader, int64(len(image.Data)), false)
	return out.FileInfo, err
}
