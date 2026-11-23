package qq

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
)

// RemoveGroupMember uses the official QQ group API, never the guild endpoint.
func (c *Client) RemoveGroupMember(ctx context.Context, groupID, memberID string) error {
	token, err := c.token(ctx)
	if err != nil {
		return err
	}
	data, _ := json.Marshal(map[string]any{"member_openids": []string{memberID}})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.APIBase+"/v2/groups/"+url.PathEscape(groupID)+"/batch_remove_members", bytes.NewReader(data))
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
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return err
	}
	if resp.StatusCode/100 != 2 {
		return fmt.Errorf("QQ remove member status %d: %s", resp.StatusCode, raw)
	}
	var result struct {
		Result  string `json:"remove_members_result"`
		Code    int    `json:"code"`
		Message string `json:"message"`
	}
	if err = json.Unmarshal(raw, &result); err != nil {
		return fmt.Errorf("QQ remove member invalid response: %w", err)
	}
	if result.Result != "success" || result.Code != 0 {
		return fmt.Errorf("QQ remove member failed (code=%d message=%s result=%s)", result.Code, result.Message, result.Result)
	}
	return nil
}
