package app

import (
	"context"
	"log/slog"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"

	"ai-bot/backend/internal/config"
	"golang.org/x/net/websocket"
)

func TestAdminEventsDeliverCommittedChanges(t *testing.T) {
	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("TEST_DATABASE_URL not configured")
	}
	ctx := context.Background()
	pool := newIntegrationSchema(t, ctx, dsn)
	a, err := New(ctx, config.Config{DataDir: t.TempDir(), MasterKey: []byte("0123456789abcdef0123456789abcdef"), AdminUsername: "admin", AdminPassword: "test-password"}, pool, slog.Default())
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(a.Router())
	defer server.Close()
	addr, _ := url.Parse(server.URL)
	jar, _ := cookiejar.New(nil)
	dial := func(origin string) (*websocket.Conn, error) {
		cfg, err := websocket.NewConfig("ws"+strings.TrimPrefix(server.URL, "http")+"/api/events", origin)
		if err != nil {
			return nil, err
		}
		for _, cookie := range jar.Cookies(addr) {
			cfg.Header.Add("Cookie", cookie.String())
		}
		return websocket.DialConfig(cfg)
	}
	if ws, err := dial(server.URL); err == nil {
		ws.Close()
		t.Fatal("anonymous connection accepted")
	}
	requestJSON(t, &http.Client{Jar: jar}, "POST", server.URL+"/api/auth/login", map[string]string{"username": "admin", "password": "test-password"}, 200)
	if ws, err := dial("https://other.example"); err == nil {
		ws.Close()
		t.Fatal("cross-origin connection accepted")
	}
	ws, err := dial(server.URL)
	if err != nil {
		t.Fatal(err)
	}
	defer ws.Close()
	receive := func(conn *websocket.Conn, want string) {
		t.Helper()
		conn.SetReadDeadline(time.Now().Add(3 * time.Second))
		var msg struct{ Resource string }
		if err := websocket.JSON.Receive(conn, &msg); err != nil || msg.Resource != want {
			t.Fatalf("want %q got %+v err %v", want, msg, err)
		}
	}
	receive(ws, "*")
	listenCtx, cancel := context.WithCancel(ctx)
	done := make(chan struct{})
	go func() { defer close(done); a.runAdminEvents(listenCtx) }()
	defer func() { cancel(); <-done }()
	receive(ws, "*")
	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = tx.Exec(ctx, "INSERT INTO system_alerts(kind,title) VALUES('test','rollback')"); err != nil {
		t.Fatal(err)
	}
	if err = tx.Rollback(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err = pool.Exec(ctx, "INSERT INTO bots(name,app_id,app_secret_enc) VALUES('test','id','enc')"); err != nil {
		t.Fatal(err)
	}
	receive(ws, "bots")
	ws.Close()
	if _, err = pool.Exec(ctx, "INSERT INTO system_alerts(kind,title) VALUES('test','offline')"); err != nil {
		t.Fatal(err)
	}
	again, err := dial(server.URL)
	if err != nil {
		t.Fatal(err)
	}
	defer again.Close()
	receive(again, "*")
}
