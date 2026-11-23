package qq

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
)

var ErrGroupInfoPermission = errors.New("当前机器人没有获取群基本信息的权限（11253），请向 QQ 平台运营申请")

type GroupInfo struct {
	Code    int
	Name    string   `json:"group_name"`
	Group   string   `json:"group_openid"`
	Memo    string   `json:"group_finger_memo"`
	Class   string   `json:"group_class_text"`
	Tags    []string `json:"group_tags"`
	Members int      `json:"group_member_num"`
	Union   string   `json:"union_group_openid"`
}

func (c *Client) GroupInfo(ctx context.Context, group string) (GroupInfo, error) {
	token, err := c.token(ctx)
	if err != nil {
		return GroupInfo{}, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.APIBase+"/v2/groups/"+url.PathEscape(group)+"/info", nil)
	if err != nil {
		return GroupInfo{}, err
	}
	req.Header.Set("Authorization", "QQBot "+token)
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return GroupInfo{}, err
	}
	defer resp.Body.Close()
	var info GroupInfo
	decodeErr := json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&info)
	if info.Code == 11253 {
		return GroupInfo{}, ErrGroupInfoPermission
	}
	if resp.StatusCode/100 != 2 || info.Code != 0 {
		return GroupInfo{}, fmt.Errorf("查询 QQ 群名失败（HTTP %d，错误码 %d）", resp.StatusCode, info.Code)
	}
	name := strings.TrimSpace(info.Name)
	if decodeErr != nil || name == "" || (info.Group != "" && info.Group != group) {
		return GroupInfo{}, errors.New("QQ 返回的群资料无效或群名为空")
	}
	info.Name = name
	return info, nil
}
