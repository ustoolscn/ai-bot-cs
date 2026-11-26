package app

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"ai-bot/backend/internal/config"
	"github.com/gin-gonic/gin"
)

func TestHumanReplyDelivery(t *testing.T) {
	databaseURL := os.Getenv("TEST_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("TEST_DATABASE_URL is not set")
	}
	ctx := context.Background()
	pool := newIntegrationSchema(t, ctx, databaseURL)
	var bodies []map[string]any
	reject := false
	sentCount := 0
	mock := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/token" {
			json.NewEncoder(w).Encode(map[string]string{"access_token": "token"})
			return
		}
		var body map[string]any
		json.NewDecoder(r.Body).Decode(&body)
		if strings.HasSuffix(r.URL.Path, "/files") {
			expected := demoPNG(t)
			if body["file_type"] == float64(2) {
				var err error
				expected, err = os.ReadFile("testdata/knowledge/demo.mp4")
				if err != nil {
					t.Error(err)
				}
			}
			if body["file_data"] != base64.StdEncoding.EncodeToString(expected) {
				t.Error("uploaded image differs")
			}
			json.NewEncoder(w).Encode(map[string]string{"file_info": "fixture"})
			return
		}
		bodies = append(bodies, body)
		if reject {
			http.Error(w, `{"code":40034102,"message":"主动消息失败, 无权限"}`, 403)
			return
		}
		sentCount++
		json.NewEncoder(w).Encode(map[string]string{"id": fmt.Sprintf("sent-%d", sentCount)})
	}))
	defer mock.Close()
	a, err := New(ctx, config.Config{DataDir: t.TempDir(), MasterKey: []byte("0123456789abcdef0123456789abcdef"), AdminUsername: "admin", AdminPassword: "admin123456", QQAPIBaseURL: mock.URL, QQTokenURL: mock.URL + "/token"}, pool, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatal(err)
	}
	secret, _ := a.cipher.Encrypt("secret")
	var botID string
	if err := pool.QueryRow(ctx, `INSERT INTO bots(name,app_id,app_secret_enc) VALUES('human-test','human-app',$1) RETURNING id::text`, secret).Scan(&botID); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name, kind          string
		age                 time.Duration
		exhausted, rejected bool
	}{
		{"passive group", "group", time.Minute, false, false},
		{"group after six minutes", "group", 6 * time.Minute, false, false},
		{"expired private", "private", 61 * time.Minute, false, false},
		{"passive private", "private", 30 * time.Minute, false, false},
		{"ordered private", "private", time.Minute, false, false},
		{"exhausted", "group", time.Minute, true, false},
		{"active rejected", "group", time.Hour, false, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			bodies = nil
			reject = tc.rejected
			var convID, inboundID string
			if err := pool.QueryRow(ctx, `INSERT INTO conversations(channel,bot_id,platform_id,type,name) VALUES('qq',$1,$2,$3,$2) RETURNING id::text`, botID, tc.name, tc.kind).Scan(&convID); err != nil {
				t.Fatal(err)
			}
			if err := pool.QueryRow(ctx, `INSERT INTO messages(channel,bot_id,conversation_id,direction,platform_message_id,content,event_at) VALUES('qq',$1,$2,'inbound',$3,'question',$4) RETURNING id::text`, botID, convID, tc.name, time.Now().Add(-tc.age)).Scan(&inboundID); err != nil {
				t.Fatal(err)
			}
			if tc.exhausted {
				var previousID string
				if err := pool.QueryRow(ctx, `INSERT INTO messages(channel,bot_id,conversation_id,direction,reply_to_message_id,content) VALUES('qq',$1,$2,'outbound',$3,'previous') RETURNING id::text`, botID, convID, tc.name).Scan(&previousID); err != nil {
					t.Fatal(err)
				}
				if _, err := pool.Exec(ctx, `INSERT INTO outbox_tasks(message_id,msg_seq,status) VALUES($1,5,'sent')`, previousID); err != nil {
					t.Fatal(err)
				}
			}
			var adminID string
			if err := pool.QueryRow(ctx, "SELECT id::text FROM admin_users WHERE username='admin'").Scan(&adminID); err != nil {
				t.Fatal(err)
			}
			_, err := pool.Exec(ctx, `WITH h AS(INSERT INTO handoffs(bot_id,conversation_id,message_id,status,claimed_via,claimed_by_id,claimed_by_name,paused_until) VALUES($1,$2,$3,'processing','web',$4,'admin',now()+interval '30 minutes') RETURNING id)
			UPDATE messages SET handoff_id=h.id FROM h WHERE messages.id=$3`, botID, convID, inboundID, adminID)
			if err != nil {
				t.Fatal(err)
			}
			w := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(w)
			c.Request = httptest.NewRequest("POST", "/reply", bytes.NewBufferString(`{"text":"人工回复"}`))
			c.Request.Header.Set("Content-Type", "application/json")
			if tc.name == "passive group" || tc.name == "expired private" {
				c.Request = pictureRequest(t, "/reply", map[string]string{"text": "人工回复"}, demoPNG(t))
			}
			ordered := tc.name == "ordered private"
			if ordered {
				pic := demoPNG(t)
				clip, err := os.ReadFile("testdata/knowledge/demo.mp4")
				if err != nil {
					t.Fatal(err)
				}
				c.Request = mediaRequest(t, "/reply", map[string]string{"text": "人工回复"}, "files", pic, clip, pic, clip, pic)
			}
			c.Params = gin.Params{{Key: "id", Value: inboundID}}
			c.Set("userId", adminID)
			c.Set("username", "admin")
			a.humanReply(c)
			if w.Code != 200 {
				t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
			}

			assertHandoffReplyDelivery(t, a, convID, "pending", "")
			attempts := 1
			if ordered {
				attempts = 5
			}
			if reject {
				attempts = 3
			}
			for i := 0; i < attempts; i++ {
				if _, err := pool.Exec(ctx, `UPDATE outbox_tasks SET next_attempt_at=now() WHERE status='pending'`); err != nil {
					t.Fatal(err)
				}
				if err := a.processOutbox(ctx); err != nil {
					t.Fatal(err)
				}
			}
			active := tc.exhausted || tc.age >= time.Hour
			if len(bodies) != attempts {
				t.Fatalf("calls=%d want=%d", len(bodies), attempts)
			}
			for idx, body := range bodies {
				if ordered {
					if body["msg_type"] != float64(7) || (idx == 0) != (body["content"] != nil) {
						t.Fatal(body)
					}
					if idx < 4 {
						if body["msg_seq"] != float64(idx+1) || body["msg_id"] != tc.name {
							t.Fatal(body)
						}
					} else if body["msg_seq"] != nil || body["msg_id"] != nil {
						t.Fatal(body)
					}
					continue
				}
				if tc.name == "passive group" || tc.name == "expired private" {
					if body["msg_type"] != float64(7) || !strings.Contains(fmt.Sprint(body["content"]), "人工回复") {
						t.Fatal(body)
					}
				}
				_, hasID := body["msg_id"]
				_, hasSeq := body["msg_seq"]
				if active && (hasID || hasSeq) {
					t.Fatalf("active contains passive fields: %#v", body)
				}
				if !active && (body["msg_id"] != tc.name || body["msg_seq"] != float64(2)) {
					t.Fatalf("unexpected passive: %#v", body)
				}
			}
			var status, deliveryError, association string
			if err := pool.QueryRow(ctx, `SELECT t.status,COALESCE(t.last_error,''),m.reply_to_message_id FROM outbox_tasks t JOIN messages m ON m.id=t.message_id WHERE m.conversation_id=$1 AND m.event_type='HUMAN_REPLY'`, convID).Scan(&status, &deliveryError, &association); err != nil {
				t.Fatal(err)
			}
			want := "sent"
			if reject {
				want = "failed"
			}
			if status != want || association != tc.name {
				t.Fatalf("status=%s association=%s", status, association)
			}
			assertHandoffReplyDelivery(t, a, convID, want, deliveryError)
			if reject && !strings.Contains(deliveryError, "qq_active_send_failed: QQ 主动消息发送失败") {
				t.Fatalf("error=%s", deliveryError)
			}
		})
	}
}

func assertHandoffReplyDelivery(t *testing.T, a *App, conversationID, status, deliveryError string) {
	t.Helper()
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodGet, "/api/handoffs", nil)
	a.listHandoffs(c)
	var response struct {
		Data []struct {
			ConversationID string `json:"conversationId"`
			Recent         []struct {
				EventType     string `json:"eventType"`
				Status        string `json:"status"`
				DeliveryError string `json:"deliveryError"`
			} `json:"recent"`
		} `json:"data"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &response); err != nil || w.Code != http.StatusOK {
		t.Fatalf("handoffs status=%d body=%s err=%v", w.Code, w.Body.String(), err)
	}
	for _, h := range response.Data {
		if h.ConversationID != conversationID {
			continue
		}
		for _, m := range h.Recent {
			if m.EventType != "HUMAN_REPLY" {
				continue
			}
			if m.Status != status || m.DeliveryError != deliveryError {
				t.Fatalf("recent status=%q error=%q want status=%q error=%q", m.Status, m.DeliveryError, status, deliveryError)
			}
			return
		}
	}
	t.Fatal("human reply missing from handoff recent")
}
