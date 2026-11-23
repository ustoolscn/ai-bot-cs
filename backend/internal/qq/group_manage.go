package qq

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"time"
)

func (c *Client) GroupManagement(ctx context.Context, group, path string, query url.Values) (json.RawMessage, error) {
	token, err := c.token(ctx)
	if err != nil {
		return nil, err
	}
	address := c.APIBase + "/v2/groups/" + url.PathEscape(group) + "/" + path
	if len(query) > 0 {
		address += "?" + query.Encode()
	}
	r, err := http.NewRequestWithContext(ctx, "GET", address, nil)
	if err != nil {
		return nil, err
	}
	r.Header.Set("Authorization", "QQBot "+token)
	resp, err := c.HTTP.Do(r)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(io.LimitReader(resp.Body, 2<<20))
	if err != nil {
		return nil, err
	}
	var status struct {
		Code    int
		Message string
	}
	if e := json.Unmarshal(data, &status); e != nil {
		return nil, e
	}
	if resp.StatusCode/100 != 2 || status.Code != 0 {
		return nil, fmt.Errorf("QQ HTTP %d (%d): %s", resp.StatusCode, status.Code, status.Message)
	}
	return data, nil
}
func (c *Client) ReviewApplication(ctx context.Context, group, member, id, op, reason string, block bool) error {
	p := map[string]any{"op": op, "join_request_id": id}
	if op == "decline" {
		p["reject_reason"] = reason
		p["add_to_member_blacklist"] = block
	}
	return c.groupPost(ctx, group, "approval_join_request/"+url.PathEscape(member), p)
}
func (c *Client) SetMute(ctx context.Context, group, member, op string, until time.Time) error {
	m := map[string]string{"op": op, "member_openid": member}
	if op != "del" {
		m["mute_expire_at"] = until.Format(time.RFC3339)
	}
	return c.groupPost(ctx, group, "restrict_chat_setting", map[string]any{"members": []any{m}})
}
