package qq

import (
	"context"
	"net/url"
	"time"
)

func (c *Client) groupPost(ctx context.Context, group, path string, payload any) error {
	token, err := c.token(ctx)
	if err != nil {
		return err
	}
	return c.postJSON(ctx, token, "/v2/groups/"+url.PathEscape(group)+"/"+path, payload, nil)
}
func (c *Client) MuteMember(ctx context.Context, group, member string, until time.Time) error {
	return c.SetMute(ctx, group, member, "add", until)
}
func (c *Client) ApproveJoin(ctx context.Context, group, member, requestID string) error {
	return c.ReviewApplication(ctx, group, member, requestID, "approve", "", false)
}
