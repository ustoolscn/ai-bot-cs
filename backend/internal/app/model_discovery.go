package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

func (a *App) discoverModels(c *gin.Context) {
	var in struct {
		Provider  string  `json:"provider"`
		BaseURL   string  `json:"baseUrl"`
		APIKey    string  `json:"apiKey"`
		ProfileID string  `json:"profileId"`
		ProxyID   *string `json:"proxyId"`
	}
	if c.ShouldBindJSON(&in) != nil || (in.ProfileID != "" && uuid.Validate(in.ProfileID) != nil) {
		fail(c, 400, "invalid_request", "模型查询参数无效")
		return
	}
	var savedBase, encryptedKey, savedProxy string
	if in.ProfileID != "" {
		var provider string
		err := a.db.QueryRow(c, "SELECT provider,base_url,api_key_enc,COALESCE(proxy_id::text,'') FROM model_profiles WHERE id=$1", in.ProfileID).Scan(&provider, &savedBase, &encryptedKey, &savedProxy)
		if errors.Is(err, pgx.ErrNoRows) {
			fail(c, 404, "not_found", "模型配置不存在")
			return
		}
		if err != nil {
			fail(c, 500, "database_error", "读取模型配置失败")
			return
		}
		if in.Provider == "" {
			in.Provider = provider
		}
		if in.BaseURL == "" && in.Provider == provider {
			in.BaseURL = savedBase
		}
	}
	target := modelInput{Provider: in.Provider, BaseURL: strings.TrimRight(strings.TrimSpace(in.BaseURL), "/")}
	if err := target.applyProvider(); err != nil {
		fail(c, 400, "invalid_request", err.Error())
		return
	}
	u, err := url.Parse(target.BaseURL)
	if err != nil || (u.Scheme != "https" && u.Scheme != "http") || u.Hostname() == "" || u.User != nil || u.RawQuery != "" || u.ForceQuery || u.Fragment != "" {
		fail(c, 400, "invalid_request", "请输入有效的 Base URL")
		return
	}
	proxyID := savedProxy
	if in.ProxyID != nil {
		proxyID = *in.ProxyID
	}
	key := strings.TrimSpace(in.APIKey)
	if key == "" {
		if encryptedKey == "" || target.BaseURL != strings.TrimRight(savedBase, "/") || proxyID != savedProxy {
			fail(c, 400, "invalid_request", "请为当前服务地址和代理填写 API Key")
			return
		}
		key, err = a.cipher.Decrypt(encryptedKey)
		if err != nil {
			fail(c, 500, "credential_error", "读取已保存的 API Key 失败")
			return
		}
	}
	var proxy string
	if proxyID != "" {
		if uuid.Validate(proxyID) != nil || a.db.QueryRow(c, "SELECT address FROM model_proxies WHERE id=$1", proxyID).Scan(&proxy) != nil {
			fail(c, 400, "invalid_proxy", "代理无效或不存在")
			return
		}
	}
	tr, err := modelTransport(proxy)
	if err != nil {
		fail(c, 400, "invalid_proxy", "代理配置无效")
		return
	}
	if proxy != "" {
		defer tr.CloseIdleConnections()
	}
	client := &http.Client{Transport: tr, Timeout: 20 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	models, err := fetchModelIDs(c.Request.Context(), client, target.BaseURL, key)
	if err != nil {
		fail(c, 502, "model_discovery_failed", err.Error())
		return
	}
	ok(c, models)
}

func fetchModelIDs(ctx context.Context, client *http.Client, base, key string) ([]string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, base+"/models", nil)
	if err != nil {
		return nil, errors.New("服务地址无效")
	}
	req.Header.Set("Authorization", "Bearer "+key)
	req.Header.Set("Accept", "application/json")
	resp, err := client.Do(req)
	if err != nil {
		return nil, errors.New("无法连接模型服务，请检查服务地址和代理")
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("模型服务返回 HTTP %d", resp.StatusCode)
	}
	const maxBytes = 4 << 20
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxBytes+1))
	if err != nil || len(body) > maxBytes {
		return nil, errors.New("读取模型列表失败或响应过大")
	}
	var list struct {
		Data []struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	if json.Unmarshal(body, &list) != nil || list.Data == nil {
		return nil, errors.New("服务返回的模型列表格式无效")
	}
	ids := make([]string, 0, len(list.Data))
	seen := make(map[string]bool)
	for _, m := range list.Data {
		id := strings.TrimSpace(m.ID)
		if id != "" && !seen[id] {
			ids = append(ids, id)
			seen[id] = true
		}
	}
	return ids, nil
}
