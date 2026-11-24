package app

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

func proxyAddress(address string) (*url.URL, error) {
	u, err := url.Parse(address)
	if err != nil || u.Hostname() == "" || u.ForceQuery || u.RawQuery != "" || u.Fragment != "" || (u.Path != "" && u.Path != "/") {
		return nil, errors.New("请输入有效的代理地址")
	}
	switch u.Scheme {
	case "http", "https", "socks5":
		return u, nil
	}
	return nil, errors.New("支持 http、https、socks5 代理")
}

var directModelTransport = func() *http.Transport {
	tr := http.DefaultTransport.(*http.Transport).Clone()
	tr.Proxy = nil
	return tr
}()

// Suppress proxy-supplied diagnostics, which can echo credentials on auth failure.
type authenticatedProxyTransport struct {
	*http.Transport
	redactor *strings.Replacer
}

func (tr authenticatedProxyTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	resp, err := tr.Transport.RoundTrip(req)
	if err != nil {
		if req.Context().Err() != nil {
			return nil, req.Context().Err()
		}
		return nil, errors.New("代理连接失败，请检查地址和认证信息")
	}
	if resp.StatusCode == http.StatusProxyAuthRequired {
		resp.Body.Close()
		return nil, errors.New("代理认证失败 (HTTP 407)")
	}
	return resp, nil
}

func (a *App) proxyTransport(ctx context.Context, id string) (http.RoundTripper, error) {
	if id == "" {
		return directModelTransport, nil
	}
	var address, username, encrypted string
	err := a.db.QueryRow(ctx, "SELECT address,username,password_enc FROM model_proxies WHERE id=$1", id).Scan(&address, &username, &encrypted)
	if err != nil {
		return nil, err
	}
	u, err := proxyAddress(address)
	if err != nil {
		return nil, err
	}
	password, err := a.cipher.Decrypt(encrypted)
	if err != nil {
		return nil, errors.New("读取代理密码失败")
	}
	if username != "" || password != "" {
		u.User = url.UserPassword(username, password)
	}
	return modelTransport(u), nil
}

func modelTransport(u *url.URL) http.RoundTripper {
	tr := directModelTransport.Clone()
	tr.Proxy = http.ProxyURL(u)
	if u.User != nil {
		password, _ := u.User.Password()
		basic := base64.StdEncoding.EncodeToString([]byte(u.User.Username() + ":" + password))
		replacements := []string{basic, "[REDACTED]"}
		if password != "" {
			encoded := strings.TrimPrefix(url.UserPassword("", password).String(), ":")
			replacements = append(replacements, encoded, "[REDACTED]", password, "[REDACTED]")
		}
		return authenticatedProxyTransport{tr, strings.NewReplacer(replacements...)}
	}
	return tr
}

type proxyInput struct {
	Name, Address, Password string
	Username                *string
	ClearPassword           bool
}

func (in *proxyInput) splitAddress() error {
	u, err := proxyAddress(strings.TrimSpace(in.Address))
	if err != nil {
		return err
	}
	if u.User != nil {
		if in.Username == nil || *in.Username == "" {
			user := u.User.Username()
			in.Username = &user
		}
		if in.Password == "" {
			in.Password, _ = u.User.Password()
		}
		u.User = nil
	}
	in.Address = u.String()
	return nil
}

func (a *App) proxyRoutes(r *gin.RouterGroup) {
	g := r.Group("/system/proxies")
	g.GET("", func(c *gin.Context) {
		queryList(c, a, `SELECT id::text,name,address,username,password_enc<>'' AS "hasPassword" FROM model_proxies ORDER BY name,id`)
	})
	save := func(c *gin.Context) {
		var in proxyInput
		if c.ShouldBindJSON(&in) != nil || strings.TrimSpace(in.Name) == "" {
			fail(c, 400, "invalid_request", "请填写名称和地址")
			return
		}
		if err := in.splitAddress(); err != nil {
			fail(c, 400, "invalid_address", err.Error())
			return
		}
		var passwordEnc *string
		if in.ClearPassword || in.Password != "" {
			if in.ClearPassword {
				in.Password = ""
			}
			encrypted, err := a.cipher.Encrypt(in.Password)
			if err != nil {
				fail(c, 500, "credential_error", "保存代理密码失败")
				return
			}
			passwordEnc = &encrypted
		}
		saveSupportRecord(c, a,
			`INSERT INTO model_proxies(name,address,username,password_enc) VALUES($1,$2,COALESCE($3,''),COALESCE($4,'')) RETURNING id::text`,
			`UPDATE model_proxies SET name=$1,address=$2,username=COALESCE($3,username),password_enc=COALESCE($4,password_enc) WHERE id=$5 RETURNING id::text`,
			strings.TrimSpace(in.Name), in.Address, in.Username, passwordEnc)
	}
	g.POST("", save)
	g.PUT("/:item", save)
	g.DELETE("/:item", func(c *gin.Context) {
		tag, err := a.db.Exec(c, "DELETE FROM model_proxies WHERE id=$1", c.Param("item"))
		var pg *pgconn.PgError
		if errors.As(err, &pg) && pg.Code == "23503" {
			fail(c, 409, "proxy_in_use", "请先解除模型关联")
			return
		}
		if err != nil {
			fail(c, 500, "database_error", "删除代理失败")
			return
		}
		if tag.RowsAffected() == 0 {
			fail(c, 404, "not_found", "代理不存在")
			return
		}
		ok(c, gin.H{"deleted": true})
	})
	g.POST("/:item/test", func(c *gin.Context) {
		start := time.Now()
		tr, err := a.proxyTransport(c, c.Param("item"))
		if errors.Is(err, pgx.ErrNoRows) {
			fail(c, 404, "not_found", "代理不存在")
			return
		}
		reason := ""
		if err != nil {
			reason = "无法读取代理配置"
		} else {
			client := http.Client{Transport: tr, Timeout: 8 * time.Second}
			defer client.CloseIdleConnections()
			req, _ := http.NewRequestWithContext(c.Request.Context(), http.MethodHead, "https://example.com/", nil)
			resp, requestErr := client.Do(req)
			if requestErr != nil {
				reason = "代理连接失败，请检查地址和认证信息"
			} else {
				resp.Body.Close()
				if resp.StatusCode >= 400 {
					reason = fmt.Sprintf("HTTP %d", resp.StatusCode)
				}
			}
		}
		ok(c, gin.H{"reachable": reason == "", "latencyMs": time.Since(start).Milliseconds(), "error": reason})
	})
}
