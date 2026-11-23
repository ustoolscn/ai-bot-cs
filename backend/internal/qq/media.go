package qq

import (
	"bytes"
	"context"
	"crypto/md5"
	"crypto/sha1"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
)

const MaxMediaSize int64 = 200 << 20

type Media struct {
	FileUUID string `json:"file_uuid"`
	FileInfo string `json:"file_info"`
	TTL      int    `json:"ttl"`
	ID       string `json:"id"`
	RawURL   string `json:"raw_url"`
}

func Prefix(kind, id string) string {
	if kind == "private" {
		return "/v2/users/" + url.PathEscape(id)
	}
	return "/v2/groups/" + url.PathEscape(id)
}
func (c *Client) postJSON(ctx context.Context, token, path string, payload any, result any) error {
	return c.jsonRequest(ctx, http.MethodPost, token, path, payload, result)
}
func (c *Client) Acknowledge(ctx context.Context, id string) error {
	token, err := c.token(ctx)
	if err != nil {
		return err
	}
	return c.jsonRequest(ctx, http.MethodPut, token, "/interactions/"+url.PathEscape(id), map[string]int{"code": 0}, nil)
}
func (c *Client) jsonRequest(ctx context.Context, method, token, path string, payload, result any) error {
	body, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	r, err := http.NewRequestWithContext(ctx, method, c.APIBase+path, bytes.NewReader(body))
	if err != nil {
		return err
	}
	r.Header.Set("Authorization", "QQBot "+token)
	r.Header.Set("Content-Type", "application/json")
	resp, err := c.HTTP.Do(r)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(io.LimitReader(resp.Body, 2<<20))
	if err != nil {
		return err
	}
	var status struct {
		Code    int
		Message string
	}
	if len(bytes.TrimSpace(data)) != 0 {
		if err = json.Unmarshal(data, &status); err != nil {
			return fmt.Errorf("QQ HTTP %d 返回无效响应", resp.StatusCode)
		}
	}
	if resp.StatusCode/100 != 2 || status.Code != 0 {
		return fmt.Errorf("QQ HTTP %d (%d): %s", resp.StatusCode, status.Code, status.Message)
	}
	if result == nil {
		return nil
	}
	return json.Unmarshal(data, result)
}
func (c *Client) Upload(ctx context.Context, prefix, name, remote string, kind int, source io.ReaderAt, size int64, direct bool) (Media, error) {
	token, err := c.token(ctx)
	if err != nil {
		return Media{}, err
	}
	return c.upload(ctx, token, prefix, name, remote, kind, source, size, direct)
}
func (c *Client) upload(ctx context.Context, token, prefix, name, remote string, kind int, source io.ReaderAt, size int64, direct bool) (Media, error) {
	var out Media
	p := map[string]any{"file_type": kind, "srv_send_msg": direct}
	if name = pathBase(name); name != "" {
		p["file_name"] = name
	}
	switch {
	case kind < 1 || kind > 4:
		return out, errors.New("媒体类型无效")
	case source == nil:
		u, err := url.Parse(remote)
		if err != nil || u.Scheme != "https" || u.Host == "" || u.User != nil {
			return out, errors.New("请输入 HTTPS 素材地址")
		}
		p["url"] = remote
	case size <= 0 || size > MaxMediaSize || name == "":
		return out, errors.New("文件名为空或文件大小超出 QQ 限制")
	case size < 5<<20:
		data, err := io.ReadAll(io.NewSectionReader(source, 0, size))
		if err != nil {
			return out, err
		}
		p["file_data"] = base64.StdEncoding.EncodeToString(data)
	default:
		id, err := c.uploadParts(ctx, token, prefix, name, kind, source, size)
		if err != nil {
			return out, err
		}
		p["upload_id"] = id
	}
	if err := c.postJSON(ctx, token, prefix+"/files", p, &out); err != nil {
		return out, err
	}
	if (!direct && out.FileInfo == "") || (direct && out.ID == "") {
		return out, errors.New("QQ 未返回文件凭据或发送回执")
	}
	return out, nil
}
func (c *Client) uploadParts(ctx context.Context, token, prefix, name string, kind int, source io.ReaderAt, size int64) (string, error) {
	md, sha, head := md5.New(), sha1.New(), md5.New()
	if _, err := io.Copy(io.MultiWriter(md, sha), io.NewSectionReader(source, 0, size)); err != nil {
		return "", err
	}
	if _, err := io.Copy(head, io.NewSectionReader(source, 0, min(size, 10002432))); err != nil {
		return "", err
	}
	var plan struct {
		ID    string `json:"upload_id"`
		Parts []struct {
			Index int
			URL   string `json:"presigned_url"`
			Size  string `json:"block_size"`
		}
	}
	payload := map[string]any{"file_type": kind, "file_name": name, "file_size": strconv.FormatInt(size, 10), "md5": fmt.Sprintf("%x", md.Sum(nil)), "sha1": fmt.Sprintf("%x", sha.Sum(nil)), "md5_10m": fmt.Sprintf("%x", head.Sum(nil))}
	if err := c.postJSON(ctx, token, prefix+"/upload_prepare", payload, &plan); err != nil {
		return "", err
	}
	if plan.ID == "" {
		return "", errors.New("QQ 分片任务无效")
	}
	sort.Slice(plan.Parts, func(i, j int) bool { return plan.Parts[i].Index < plan.Parts[j].Index })
	var offset int64
	for i, part := range plan.Parts {
		n, err := strconv.ParseInt(part.Size, 10, 64)
		if err != nil || part.Index != i || n <= 0 || n > size-offset || !strings.HasPrefix(part.URL, "https://") {
			return "", errors.New("QQ 分片参数无效")
		}
		hash := md5.New()
		r, err := http.NewRequestWithContext(ctx, http.MethodPut, part.URL, io.TeeReader(io.NewSectionReader(source, offset, n), hash))
		if err != nil {
			return "", errors.New("分片地址无效")
		}
		r.ContentLength = n
		resp, err := c.HTTP.Do(r)
		if err != nil {
			return "", errors.New("上传分片失败")
		}
		resp.Body.Close()
		if resp.StatusCode/100 != 2 {
			return "", fmt.Errorf("上传分片 HTTP %d", resp.StatusCode)
		}
		if err = c.postJSON(ctx, token, prefix+"/upload_part_finish", map[string]any{"upload_id": plan.ID, "part_index": i, "block_size": part.Size, "md5": fmt.Sprintf("%x", hash.Sum(nil))}, nil); err != nil {
			return "", err
		}
		offset += n
	}
	if offset != size {
		return "", errors.New("QQ 分片长度不匹配")
	}
	return plan.ID, nil
}
func (c *Client) SendConfigured(ctx context.Context, prefix string, payload map[string]any) (Receipt, error) {
	token, err := c.token(ctx)
	if err != nil {
		return Receipt{}, err
	}
	return c.sendPayload(ctx, token, prefix+"/messages", payload)
}
