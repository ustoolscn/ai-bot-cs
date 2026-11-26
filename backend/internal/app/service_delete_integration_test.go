package app

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
)

func TestDeleteAssignedLastService(t *testing.T) {
	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("TEST_DATABASE_URL not set")
	}
	ctx := t.Context()
	db := newIntegrationSchema(t, ctx, dsn)
	a := App{db: db}
	r := gin.New()
	a.supportRoutes(r.Group(""))
	value := func(query string, args ...any) string {
		t.Helper()
		var v string
		if err := db.QueryRow(ctx, query, args...).Scan(&v); err != nil {
			t.Fatal(err)
		}
		return v
	}
	bot := value("INSERT INTO bots(name,app_id,app_secret_enc) VALUES('bot','one','secret') RETURNING id")
	other := value("INSERT INTO bots(name,app_id,app_secret_enc) VALUES('bot','two','secret') RETURNING id")
	svc := value("INSERT INTO bot_services(bot_id,name) VALUES($1,'support') RETURNING id", bot)
	group := value("INSERT INTO conversations(channel,bot_id,platform_id,type,service_id) VALUES('qq',$1,'group','group',$2) RETURNING id", bot, svc)
	remove := func(owner string, want int) {
		t.Helper()
		w := httptest.NewRecorder()
		r.ServeHTTP(w, httptest.NewRequest("DELETE", "/bots/"+owner+"/services/"+svc, nil))
		if w.Code != want {
			t.Fatalf("delete service: got %d %s, want %d", w.Code, w.Body.String(), want)
		}
		if want == 200 {
			var result struct{ Data struct{ Deleted bool } }
			if err := json.Unmarshal(w.Body.Bytes(), &result); err != nil || !result.Data.Deleted {
				t.Fatalf("unexpected delete response: %s", w.Body.String())
			}
		}
	}
	remove(other, 404)
	conn, err := db.Acquire(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Release()
	channel := value("SELECT 'admin_change_' || md5(current_schema())")
	if _, err = conn.Exec(ctx, "LISTEN "+channel); err != nil {
		t.Fatal(err)
	}
	remove(bot, 200)
	remove(bot, 404)
	if got := value("SELECT bot_id FROM conversations WHERE id=$1 AND service_id IS NULL", group); got != bot {
		t.Fatalf("group moved to %s", got)
	}
	timeout, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	for {
		event, err := conn.Conn().WaitForNotification(timeout)
		if err != nil {
			t.Fatal(err)
		}
		if event.Payload == "bot_services" {
			break
		}
	}
}
