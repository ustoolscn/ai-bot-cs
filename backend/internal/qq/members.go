package qq

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"time"
)

var ErrRosterPermission = errors.New("QQ 群成员列表接口尚未向此机器人开放（11253），需向 QQ 平台运营申请白名单")

type GroupMember struct {
	ID       string     `json:"member_openid"`
	Username string     `json:"username"`
	Joined   *time.Time `json:"joined_at"`
	Union    string     `json:"union_openid"`
	Role     string     `json:"member_role"`
	Bot      bool       `json:"bot"`
}

func (c *Client) GroupMembers(ctx context.Context, group string) ([]GroupMember, error) {
	token, err := c.token(ctx)
	if err != nil {
		return nil, err
	}
	endpoint := c.APIBase + "/v2/groups/" + url.PathEscape(group) + "/members"
	members := []GroupMember{}
	seenMembers := map[string]bool{}
	cursors := map[string]bool{}
	cursor := ""
	for {
		address := endpoint
		if cursor != "" {
			address += "?" + url.Values{"cursor": {cursor}}.Encode()
		}
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, address, nil)
		if err != nil {
			return nil, err
		}
		req.Header.Set("Authorization", "QQBot "+token)
		resp, err := c.HTTP.Do(req)
		if err != nil {
			return nil, err
		}
		var page struct {
			Members []GroupMember `json:"members"`
			Next    string        `json:"next_cursor"`
			Code    int           `json:"code"`
		}
		err = json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&page)
		resp.Body.Close()
		if page.Code == 11253 {
			return nil, ErrRosterPermission
		}
		if resp.StatusCode/100 != 2 || page.Code != 0 {
			return nil, fmt.Errorf("QQ 成员同步失败（HTTP %d，错误码 %d）", resp.StatusCode, page.Code)
		}
		if err != nil || page.Members == nil {
			return nil, errors.New("QQ 返回的成员列表格式无效")
		}
		for _, m := range page.Members {
			if m.ID == "" {
				return nil, errors.New("QQ 返回的成员缺少 OpenID")
			}
			if !seenMembers[m.ID] {
				members = append(members, m)
				seenMembers[m.ID] = true
			}
		}
		if page.Next == "" {
			return members, nil
		}
		if cursors[page.Next] {
			return nil, errors.New("QQ 返回了重复的分页游标")
		}
		cursor = page.Next
		cursors[cursor] = true
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(time.Second):
		}
	}
}
