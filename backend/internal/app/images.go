package app

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"ai-bot/backend/internal/domain"
	"github.com/gin-gonic/gin"
)

var (
	markdownImagePattern = regexp.MustCompile(`!\[[^\]]*\]\((https://[^\s)]+)\)`)
	imageMarkerPattern   = regexp.MustCompile(`(?i)\[IMAGE\](https://[^\s\[]+)\[/IMAGE\]`)
)

const (
	maxInboundImages    = 4
	maxInboundImageSize = 10 << 20
)

func newSafeImageHTTPClient() *http.Client {
	dialer := &net.Dialer{Timeout: 10 * time.Second, KeepAlive: 30 * time.Second}
	transport := &http.Transport{
		Proxy: http.ProxyFromEnvironment,
		DialContext: func(ctx context.Context, network, address string) (net.Conn, error) {
			host, port, err := net.SplitHostPort(address)
			if err != nil {
				return nil, err
			}
			ips, err := net.DefaultResolver.LookupIP(ctx, "ip", host)
			if err != nil {
				return nil, err
			}
			for _, ip := range ips {
				if isPublicIP(ip) {
					return dialer.DialContext(ctx, network, net.JoinHostPort(ip.String(), port))
				}
			}
			return nil, fmt.Errorf("图片地址解析到非公网 IP")
		},
		TLSHandshakeTimeout: 10 * time.Second,
	}
	return &http.Client{
		Timeout:   20 * time.Second,
		Transport: transport,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) >= 3 {
				return fmt.Errorf("图片下载重定向次数过多")
			}
			return validateImageURL(req.URL)
		},
	}
}

func isPublicIP(ip net.IP) bool {
	return ip != nil && !ip.IsPrivate() && !ip.IsLoopback() && !ip.IsLinkLocalUnicast() && !ip.IsLinkLocalMulticast() && !ip.IsUnspecified() && !ip.IsMulticast()
}

func validateImageURL(parsed *url.URL) error {
	if parsed == nil || parsed.Scheme != "https" || parsed.Hostname() == "" {
		return fmt.Errorf("图片地址必须是 HTTPS 公网地址")
	}
	return nil
}

func downloadImage(ctx context.Context, client *http.Client, rawURL string) ([]byte, string, string, error) {
	parsed, err := url.Parse(rawURL)
	if err != nil || validateImageURL(parsed) != nil {
		return nil, "", "", fmt.Errorf("无效图片地址")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, parsed.String(), nil)
	if err != nil {
		return nil, "", "", err
	}
	req.Header.Set("User-Agent", "ai-bot-image-fetcher/1.0")
	resp, err := client.Do(req)
	if err != nil {
		return nil, "", "", fmt.Errorf("下载图片失败: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode/100 != 2 {
		return nil, "", "", fmt.Errorf("下载图片返回 HTTP %d", resp.StatusCode)
	}
	if resp.ContentLength > maxInboundImageSize {
		return nil, "", "", fmt.Errorf("图片超过 10 MB")
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, maxInboundImageSize+1))
	if err != nil {
		return nil, "", "", fmt.Errorf("读取图片失败: %w", err)
	}
	if len(data) > maxInboundImageSize {
		return nil, "", "", fmt.Errorf("图片超过 10 MB")
	}
	contentType := http.DetectContentType(data)
	ext := imageExtension(contentType)
	if ext == "" {
		return nil, "", "", fmt.Errorf("不支持的图片格式 %s", contentType)
	}
	return data, contentType, ext, nil
}

func imageExtension(contentType string) string {
	switch strings.ToLower(strings.TrimSpace(strings.Split(contentType, ";")[0])) {
	case "image/jpeg":
		return ".jpg"
	case "image/png":
		return ".png"
	case "image/gif":
		return ".gif"
	case "image/webp":
		return ".webp"
	default:
		return ""
	}
}

func (a *App) prepareMessageImages(ctx context.Context, messageID string) ([]domain.ChatContentPart, error) {
	var raw []byte
	if err := a.db.QueryRow(ctx, "SELECT parts FROM messages WHERE id=$1", messageID).Scan(&raw); err != nil {
		return nil, err
	}
	var parts []domain.ContentPart
	if len(raw) > 0 {
		_ = json.Unmarshal(raw, &parts)
	}
	images := make([]domain.ChatContentPart, 0, maxInboundImages)
	changed := false
	for index := range parts {
		part := &parts[index]
		if part.Type != "image" && !strings.HasPrefix(strings.ToLower(part.ContentType), "image/") {
			continue
		}
		if len(images) >= maxInboundImages {
			break
		}
		var data []byte
		var err error
		if part.StorageKey != "" {
			data, err = a.images.Read(ctx, part.StorageKey)
			if err != nil {
				return nil, fmt.Errorf("读取缓存图片 %s 失败: %w", part.Filename, err)
			}
		} else {
			var contentType, ext string
			data, contentType, ext, err = downloadImage(ctx, a.imageHTTP, part.URL)
			if err != nil {
				return nil, fmt.Errorf("处理图片 %s 失败: %w", part.Filename, err)
			}
			name := strings.TrimSpace(part.Filename)
			name = strings.TrimSuffix(name, filepath.Ext(name)) + ext
			if name == ext {
				name = "image" + ext
			}
			part.StorageKey, err = a.images.Save(ctx, name, data)
			if err != nil {
				return nil, fmt.Errorf("保存图片缓存失败: %w", err)
			}
			part.Type = "image"
			part.ContentType = contentType
			part.SizeBytes = int64(len(data))
			changed = true
		}
		contentType := part.ContentType
		if imageExtension(contentType) == "" {
			contentType = http.DetectContentType(data)
		}
		images = append(images, domain.ChatContentPart{
			Type:        "image",
			URL:         fmt.Sprintf("/api/messages/%s/attachments/%d", messageID, index),
			ContentType: contentType,
			Detail:      "auto",
			DataURL:     "data:" + contentType + ";base64," + base64.StdEncoding.EncodeToString(data),
		})
	}
	if changed {
		encoded, _ := json.Marshal(parts)
		if _, err := a.db.Exec(ctx, "UPDATE messages SET parts=$1::jsonb WHERE id=$2", string(encoded), messageID); err != nil {
			return nil, err
		}
	}
	return images, nil
}

func extractOutboundImage(content string) (string, []domain.ContentPart) {
	match := markdownImagePattern.FindStringSubmatch(content)
	if len(match) < 2 {
		match = imageMarkerPattern.FindStringSubmatch(content)
	}
	if len(match) < 2 {
		return strings.TrimSpace(content), nil
	}
	imageURL := strings.TrimSpace(match[1])
	parsed, err := url.Parse(imageURL)
	if err != nil || validateImageURL(parsed) != nil {
		return strings.TrimSpace(content), nil
	}
	cleaned := strings.TrimSpace(strings.Replace(content, match[0], "", 1))
	return cleaned, []domain.ContentPart{{Type: "image", URL: imageURL, ContentType: "image/*"}}
}

func (a *App) getMessageAttachment(c *gin.Context) {
	index, err := strconv.Atoi(c.Param("index"))
	if err != nil || index < 0 {
		fail(c, 400, "invalid_index", "附件编号无效")
		return
	}
	var raw []byte
	if err := a.db.QueryRow(c, "SELECT parts FROM messages WHERE id=$1", c.Param("id")).Scan(&raw); err != nil {
		fail(c, 404, "not_found", "消息不存在")
		return
	}
	var parts []domain.ContentPart
	if json.Unmarshal(raw, &parts) != nil || index >= len(parts) || parts[index].StorageKey == "" {
		fail(c, 404, "not_found", "图片附件不存在或尚未缓存")
		return
	}
	data, err := a.images.Read(c, parts[index].StorageKey)
	if err != nil {
		fail(c, 404, "not_found", "图片文件不存在")
		return
	}
	contentType := parts[index].ContentType
	if imageExtension(contentType) == "" {
		contentType = http.DetectContentType(data)
	}
	c.Header("Cache-Control", "private, max-age=300")
	c.Header("X-Content-Type-Options", "nosniff")
	c.Data(http.StatusOK, contentType, data)
}

func decorateMessageParts(record map[string]any) {
	messageID, _ := record["id"].(string)
	record["parts"] = publicContentParts(messageID, record["parts"])
	answerID, _ := record["answerId"].(string)
	record["answerParts"] = publicContentParts(answerID, record["answerParts"])
	delete(record, "answerId")
}

func publicContentParts(messageID string, value any) []map[string]any {
	raw, _ := json.Marshal(value)
	var parts []domain.ContentPart
	_ = json.Unmarshal(raw, &parts)
	out := make([]map[string]any, 0, len(parts))
	for index, part := range parts {
		item := map[string]any{"type": part.Type}
		if part.Text != "" {
			item["text"] = part.Text
		}
		if part.Filename != "" {
			item["filename"] = part.Filename
		}
		if part.ContentType != "" {
			item["contentType"] = part.ContentType
		}
		if part.SizeBytes > 0 {
			item["sizeBytes"] = part.SizeBytes
		}
		if part.Width > 0 {
			item["width"] = part.Width
		}
		if part.Height > 0 {
			item["height"] = part.Height
		}
		if part.Type == "image" {
			if part.StorageKey != "" && messageID != "" {
				item["previewUrl"] = fmt.Sprintf("/api/messages/%s/attachments/%d", messageID, index)
			} else if part.URL != "" {
				item["previewUrl"] = part.URL
			}
		}
		out = append(out, item)
	}
	return out
}
