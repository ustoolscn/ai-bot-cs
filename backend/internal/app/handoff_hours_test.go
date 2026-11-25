package app

import (
	"errors"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
)

func TestClosedHoursOnlyBlockNewTickets(t *testing.T) {
	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("TEST_DATABASE_URL not set")
	}
	ctx := t.Context()
	db := newIntegrationSchema(t, ctx, dsn)
	a := App{db: db}
	id := func(q string, args ...any) string {
		t.Helper()
		var v string
		if e := db.QueryRow(ctx, q, args...).Scan(&v); e != nil {
			t.Fatal(e)
		}
		return v
	}
	bot := id("INSERT INTO bots(name,app_id,app_secret_enc) VALUES('bot','test','secret') RETURNING id")
	svc := id("INSERT INTO bot_services(name,bot_id) VALUES('support',$1) RETURNING id", bot)
	conv := id("INSERT INTO conversations(channel,bot_id,platform_id,type,service_id) VALUES('qq',$1,'group','group',$2) RETURNING id", bot, svc)
	msg := func(user string) string {
		return id("INSERT INTO messages(bot_id,channel,conversation_id,direction,sender_id) VALUES($1,'qq',$2,'inbound',$3) RETURNING id", bot, conv, user)
	}
	open := func(user string) (bool, error) {
		return a.handoffTurn(ctx, bot, conv, user, user, msg(user), "请求人工", 30)
	}
	if ok, e := open("existing"); e != nil || !ok {
		t.Fatal(ok, e)
	}
	r := gin.New()
	r.PUT("/bots/:id/services/:item/handoff-hours", a.handoffHoursAPI)
	future := time.Now().In(chinaLocation).Add(time.Hour)
	req := httptest.NewRequest("PUT", "/bots/"+bot+"/services/"+svc+"/handoff-hours", strings.NewReader(jsonString(handoffHours{true, future.Format("15:04"), future.Add(time.Minute).Format("15:04")})))
	req.Header.Set("Content-Type", "application/json")
	out := httptest.NewRecorder()
	r.ServeHTTP(out, req)
	if out.Code != 200 {
		t.Fatal(out.Body.String())
	}
	if ok, e := open("existing"); e != nil || !ok {
		t.Fatal(ok, e)
	}
	var closed hoursClosed
	if ok, e := open("new"); ok || !errors.As(e, &closed) {
		t.Fatal(ok, e)
	}
	if n := id("SELECT count(*)::text FROM handoffs WHERE conversation_id=$1", conv); n != "1" {
		t.Fatal(n)
	}
}

func TestHandoffHours(t *testing.T) {
	for _, v := range []struct {
		start, end, utc string
		want            bool
	}{
		{"09:00", "18:00", "01:00", true}, {"09:00", "18:00", "10:00", false},
		{"22:00", "06:00", "15:00", true}, {"22:00", "06:00", "21:59", true}, {"22:00", "06:00", "22:00", false},
	} {
		now, e := time.Parse("15:04", v.utc)
		if e != nil {
			t.Fatal(e)
		}
		h := handoffHours{true, v.start, v.end}
		if !h.valid() || h.open(now) != v.want {
			t.Fatal(v)
		}
	}
}
