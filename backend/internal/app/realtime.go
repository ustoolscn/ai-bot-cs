package app

import (
	"context"
	"crypto/sha256"
	"errors"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/jackc/pgx/v5"
	"golang.org/x/net/websocket"
)

// A slow browser gets a catch-up notification instead of blocking other clients.
type adminEvents struct {
	mu      sync.Mutex
	clients map[chan string]struct{}
}

func (h *adminEvents) subscribe() (chan string, func()) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.clients == nil {
		h.clients = make(map[chan string]struct{})
	}
	ch := make(chan string, 32)
	h.clients[ch] = struct{}{}
	ch <- "*"
	return ch, func() { h.mu.Lock(); delete(h.clients, ch); h.mu.Unlock() }
}

func (h *adminEvents) publish(resource string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	for ch := range h.clients {
		select {
		case ch <- resource:
		default:
			select {
			case <-ch:
			default:
			}
			ch <- "*"
		}
	}
}

func (a *App) listenAdminEvents(ctx context.Context) error {
	conn, err := pgx.ConnectConfig(ctx, a.db.Config().ConnConfig.Copy())
	if err != nil {
		return err
	}
	defer conn.Close(context.Background())
	var channel string
	if err = conn.QueryRow(ctx, "SELECT 'admin_change_' || md5(current_schema())").Scan(&channel); err != nil {
		return err
	}
	if _, err = conn.Exec(ctx, "LISTEN "+pgx.Identifier{channel}.Sanitize()); err != nil {
		return err
	}
	a.events.publish("*")
	for {
		event, err := conn.WaitForNotification(ctx)
		if err != nil {
			return err
		}
		a.events.publish(event.Payload)
	}
}

func (a *App) runAdminEvents(ctx context.Context) {
	for ctx.Err() == nil {
		err := a.listenAdminEvents(ctx)
		if ctx.Err() != nil {
			return
		}
		a.log.Warn("admin event listener interrupted", "error", err)
		select {
		case <-ctx.Done():
			return
		case <-time.After(time.Second):
		}
	}
}

func (a *App) adminSocket(c *gin.Context) {
	token, _ := c.Cookie("aibot_session")
	hash := sha256.Sum256([]byte(token))
	server := websocket.Server{
		Handshake: func(_ *websocket.Config, r *http.Request) error {
			origin, err := url.Parse(r.Header.Get("Origin"))
			if err != nil || (origin.Scheme != "http" && origin.Scheme != "https") || !strings.EqualFold(origin.Host, r.Host) {
				return errors.New("invalid websocket origin")
			}
			return nil
		},
		Handler: func(ws *websocket.Conn) {
			changes, unsubscribe := a.events.subscribe()
			defer unsubscribe()
			ws.SetDeadline(time.Time{})
			ws.MaxPayloadBytes = 1024
			closed := make(chan struct{})
			go func() { defer close(closed); var frame string; _ = websocket.Message.Receive(ws, &frame) }()
			heartbeat := time.NewTicker(20 * time.Second)
			defer heartbeat.Stop()
			for {
				var resource string
				select {
				case <-closed:
					return
				case resource = <-changes:
				case <-heartbeat.C:
					var valid bool
					if err := a.db.QueryRow(c.Request.Context(), "SELECT EXISTS(SELECT FROM admin_sessions WHERE token_hash=$1 AND expires_at>now())", hash[:]).Scan(&valid); err != nil || !valid {
						return
					}
				}
				ws.SetWriteDeadline(time.Now().Add(5 * time.Second))
				if err := websocket.JSON.Send(ws, map[string]string{"resource": resource}); err != nil {
					return
				}
			}
		}}
	server.ServeHTTP(c.Writer, c.Request)
}
