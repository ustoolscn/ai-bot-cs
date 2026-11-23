package app

import (
	"errors"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/jackc/pgx/v5/pgconn"
)

func proxyAddress(address string) (*url.URL, error) {
	u, err := url.Parse(address)
	if err != nil || u.Hostname() == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || (u.Path != "" && u.Path != "/") {
		return nil, errors.New("请输入无认证信息的代理地址")
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

func modelTransport(address string) (*http.Transport, error) {
	if address == "" {
		return directModelTransport, nil
	}
	u, err := proxyAddress(address)
	if err != nil {
		return nil, err
	}
	tr := directModelTransport.Clone()
	tr.Proxy = http.ProxyURL(u)
	return tr, nil
}

func (a *App) proxyRoutes(r *gin.RouterGroup) {
	g := r.Group("/system/proxies")
	g.GET("", func(c *gin.Context) {
		queryList(c, a, "SELECT id::text,name,address FROM model_proxies ORDER BY name,id")
	})
	save := func(c *gin.Context) {
		var in struct{ Name, Address string }
		if c.ShouldBindJSON(&in) != nil || strings.TrimSpace(in.Name) == "" {
			fail(c, 400, "invalid_request", "请填写名称和地址")
			return
		}
		u, err := proxyAddress(strings.TrimSpace(in.Address))
		if err != nil {
			fail(c, 400, "invalid_address", err.Error())
			return
		}
		saveSupportRecord(c, a, "INSERT INTO model_proxies(name,address) VALUES($1,$2) RETURNING id::text", "UPDATE model_proxies SET name=$1,address=$2 WHERE id=$3 RETURNING id::text", strings.TrimSpace(in.Name), u.String())
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
		var address string
		if err := a.db.QueryRow(c, "SELECT address FROM model_proxies WHERE id=$1", c.Param("item")).Scan(&address); err != nil {
			fail(c, 404, "not_found", "代理不存在")
			return
		}
		start := time.Now()
		tr, err := modelTransport(address)
		if err == nil {
			req, _ := http.NewRequestWithContext(c, http.MethodHead, "https://example.com/", nil)
			client := http.Client{Transport: tr, Timeout: 8 * time.Second}
			var resp *http.Response
			resp, err = client.Do(req)
			if err == nil {
				resp.Body.Close()
				if resp.StatusCode >= 400 {
					err = errors.New(resp.Status)
				}
			}
		}
		reason := ""
		if err != nil {
			reason = err.Error()
		}
		ok(c, gin.H{"reachable": err == nil, "latencyMs": time.Since(start).Milliseconds(), "error": reason})
	})
}
