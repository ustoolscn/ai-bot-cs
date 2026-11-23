package qq

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"time"
)

func (c *Client) groupPost(ctx context.Context, group, path string, payload any) error {
	token, err := c.token(ctx)
	if err != nil {
		return err
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, "POST", c.APIBase+"/v2/groups/"+url.PathEscape(group)+"/"+path, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "QQBot "+token)
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 8192))
	if err != nil {
		return err
	}
	var result struct {
		Code    int
		Message string
	}
	if len(bytes.TrimSpace(raw)) > 0 {
		if err = json.Unmarshal(raw, &result); err != nil {
			return fmt.Errorf("QQ 返回无效响应（HTTP %d）", resp.StatusCode)
		}
	}
	if resp.StatusCode/100 != 2 || result.Code != 0 {
		return fmt.Errorf("QQ HTTP %d，错误码 %d：%s", resp.StatusCode, result.Code, result.Message)
	}
	return nil
}
func (c *Client) MuteMember(ctx context.Context, group, member string, until time.Time) error {
	return c.groupPost(ctx, group, "restrict_chat_setting", map[string]any{"members": []any{map[string]any{"op": "add", "member_openid": member, "mute_expire_at": until.Format(time.RFC3339)}}})
}
func (c *Client) ApproveJoin(ctx context.Context, group, member, requestID string) error {
	return c.groupPost(ctx, group, "approval_join_request/"+url.PathEscape(member), map[string]string{"op": "approve", "join_request_id": requestID})
}
