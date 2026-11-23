package app

import (
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/jackc/pgx/v5"
)

func scheduledPageBase(raw string) (string, error) {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err == nil && (u.Scheme == "https" || u.Scheme == "http") && u.Hostname() != "" && u.User == nil && u.RawQuery == "" && u.Fragment == "" {
		host := strings.TrimSuffix(strings.ToLower(u.Hostname()), ".")
		ip := net.ParseIP(host)
		if host != "localhost" && !strings.HasSuffix(host, ".localhost") && (ip == nil || (!ip.IsLoopback() && !ip.IsUnspecified())) {
			return strings.TrimRight(u.String(), "/"), nil
		}
	}
	return "", fmt.Errorf("网页链接需要在服务器配置群成员可访问的 PUBLIC_BASE_URL")
}

func (a *App) scheduledWebPage(c *gin.Context) {
	id := c.Param("id")
	c.Header("Cache-Control", "no-store")
	c.Header("Referrer-Policy", "no-referrer")
	c.Header("X-Robots-Tag", "noindex, nofollow")
	c.Header("X-Content-Type-Options", "nosniff")
	c.Header("Content-Security-Policy", "sandbox allow-top-navigation-by-user-activation; default-src 'none'; style-src 'unsafe-inline'; img-src data:; base-uri 'none'; form-action 'none'; frame-ancestors 'none'")
	if parseUUID(id) != nil {
		c.String(http.StatusNotFound, "网页不存在或已过期")
		return
	}
	var doc string
	err := a.db.QueryRow(c, "SELECT document FROM scheduled_web_pages WHERE id=$1 AND expires_at>now()", id).Scan(&doc)
	if errors.Is(err, pgx.ErrNoRows) {
		c.String(http.StatusNotFound, "网页不存在或已过期")
		return
	}
	if err != nil {
		c.String(http.StatusInternalServerError, "暂时无法读取网页")
		return
	}
	c.Data(http.StatusOK, "text/html; charset=utf-8", []byte(doc))
}
