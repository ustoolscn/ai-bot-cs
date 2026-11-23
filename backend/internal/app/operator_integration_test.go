package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"ai-bot/backend/internal/domain"
	"ai-bot/backend/internal/qq"
	"github.com/gin-gonic/gin"
)

func TestOperatorTicketRouting(t *testing.T) {
	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("TEST_DATABASE_URL not set")
	}
	ctx := context.Background()
	pool := newIntegrationSchema(t, ctx, dsn)
	a := App{db: pool}
	check := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}
	check(a.ensureRuntimeSettings(ctx))
	var bot string
	check(pool.QueryRow(ctx, "INSERT INTO bots(name,app_id,app_secret_enc) VALUES('bot','bot','secret') RETURNING id::text").Scan(&bot))
	conv := func(kind, name string, op bool) string {
		var id string
		check(pool.QueryRow(ctx, "INSERT INTO conversations(channel,bot_id,type,name,platform_id,human_operator,trigger_mode) VALUES('qq',$1,$2,$3,$3,$4,'mention_only') RETURNING id::text", bot, kind, name, op).Scan(&id))
		return id
	}
	op := conv("private", "operator", true)
	conv("private", "second", true)
	group := conv("group", "group", false)
	private := conv("private", "carol", false)
	type ticket struct {
		id     string
		number int64
	}
	open := func(cid, user, platform string) ticket {
		var mid string
		check(pool.QueryRow(ctx, "INSERT INTO messages(bot_id,channel,conversation_id,direction,platform_message_id,sender_id,content) VALUES($1,'qq',$2,'inbound',$3,$4,$3) RETURNING id::text", bot, cid, platform, user).Scan(&mid))
		for range 2 {
			_, err := a.handoffTurn(ctx, bot, cid, user, user, mid, "转人工", 7)
			check(err)
		}
		var h ticket
		check(pool.QueryRow(ctx, "SELECT id::text,ticket_number FROM handoffs WHERE message_id=$1", mid).Scan(&h.id, &h.number))
		return h
	}
	alice := open(group, "alice", "alice-question")
	bob := open(group, "bob", "bob-question")
	carol := open(private, "carol", "carol-question")
	count := func(want int, query string, args ...any) {
		t.Helper()
		var n int
		check(pool.QueryRow(ctx, query, args...).Scan(&n))
		if n != want {
			t.Fatalf("%s: got %d want %d", query, n, want)
		}
	}
	count(6, "SELECT count(*) FROM messages WHERE event_type='HANDOFF_NOTICE'")
	send := func(kind, target, sender, event, body string) {
		msg := domain.InboundMessage{BotID: bot, ConversationType: kind, ConversationID: target, SenderID: sender, SenderName: sender, PlatformMessageID: event, EventType: "C2C_MESSAGE_CREATE", Text: body, EventTime: time.Now(), Raw: json.RawMessage(`{}`)}
		if kind == "group" {
			msg.EventType = "GROUP_MESSAGE_CREATE"
		}
		check(a.persistInbound(ctx, bot, qq.Envelope{ID: event, T: msg.EventType}, &msg, msg.Raw))
	}
	cmd := func(h ticket, who, event, body string) {
		send("private", who, who, event, fmt.Sprintf("#%d %s", h.number, body))
	}
	cmd(alice, "operator", "early", "第一句")
	count(1, "SELECT count(*) FROM messages WHERE reply_to_message_id='early' AND content=$1", fmt.Sprintf("请先发送 #%d 接单", alice.number))
	cmd(alice, "operator", "claim", "接单")
	cmd(alice, "operator", "claim", "接单")
	cmd(alice, "second", "compete", "接单")
	cmd(alice, "second", "denied", "越权回复")
	cmd(alice, "second", "denied-close", "结单")
	count(3, "SELECT count(*) FROM messages WHERE reply_to_message_id IN ('compete','denied','denied-close') AND content=$1", fmt.Sprintf("#%d 已由 operator 接单", alice.number))
	count(1, "SELECT count(*) FROM messages WHERE handoff_id=$1 AND event_type='HANDOFF_UPDATE'", alice.id)
	count(1, "SELECT count(*) FROM messages WHERE handoff_id=$1 AND event_type='HANDOFF_NOTICE' AND content=$2", alice.id, fmt.Sprintf("#%d 已由 operator 接单", alice.number))
	send("group", "group", "alice", "alice-follow", "alice-only")
	send("group", "group", "bob", "bob-follow", "bob-only")
	count(1, "SELECT count(*) FROM messages WHERE event_type='HANDOFF_NOTICE' AND conversation_id=$1 AND content LIKE '%alice-only%'", op)
	count(1, "SELECT count(*) FROM messages WHERE event_type='HANDOFF_NOTICE' AND content LIKE '%alice-only%'")
	count(2, "SELECT count(*) FROM messages WHERE event_type='HANDOFF_NOTICE' AND content LIKE '%bob-only%'")
	count(2, "SELECT count(*) FROM inbox_tasks t JOIN messages m ON m.id=t.message_id WHERE t.status='handoff' AND m.platform_message_id IN ('alice-follow','bob-follow')")
	cmd(alice, "operator", "cmd1", "alice-answer-one")
	cmd(alice, "operator", "cmd1", "alice-answer-one")
	cmd(alice, "operator", "cmd2", "alice-answer-two")
	count(2, "SELECT count(*) FROM messages WHERE handoff_id=$1 AND event_type='HUMAN_REPLY' AND conversation_id=$2 AND reply_to_message_id='alice-follow'", alice.id, group)
	count(1, "SELECT count(*) FROM handoffs WHERE id=$1 AND status='processing' AND claimed_by_id=$2 AND paused_until>now()+interval '6 minutes'", alice.id, op)

	// Both tickets live in the same QQ group. Inspect the exact public list.
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest("GET", "/api/handoffs?status=all", nil)
	a.listHandoffs(c)
	var response struct {
		Data []struct {
			ID     string
			Recent []struct{ Content string }
		}
	}
	check(json.Unmarshal(w.Body.Bytes(), &response))
	seen := map[string]int{}
	for _, h := range response.Data {
		for _, m := range h.Recent {
			seen[h.ID]++
			if h.ID == alice.id && !strings.HasPrefix(m.Content, "alice-") || h.ID == bob.id && !strings.HasPrefix(m.Content, "bob-") {
				t.Fatalf("wrong ticket history: %s %q", h.ID, m.Content)
			}
		}
	}
	if seen[alice.id] != 4 || seen[bob.id] != 2 {
		t.Fatal("incomplete history", seen)
	}
	cmd(alice, "operator", "close", "结束")
	count(1, "SELECT count(*) FROM handoffs WHERE id=$1 AND status='resolved' AND close_method='qq' AND resolved_at IS NOT NULL", alice.id)
	cmd(carol, "operator", "private-claim", "接单")
	cmd(carol, "operator", "private-answer", "私聊回复")
	count(1, "SELECT count(*) FROM messages WHERE event_type='HUMAN_REPLY' AND conversation_id=$1 AND handoff_id=$2", private, carol.id)
	_, err := pool.Exec(ctx, "UPDATE handoffs SET paused_until=now()-interval '1 second' WHERE id IN ($1,$2)", carol.id, bob.id)
	check(err)
	check(a.expireHandoffs(ctx))
	check(a.expireHandoffs(ctx))
	count(2, "SELECT count(*) FROM handoffs WHERE close_method='timeout' AND status='resolved'")
	count(1, "SELECT count(*) FROM messages WHERE event_type='HANDOFF_UPDATE' AND handoff_id=$1 AND content LIKE '%暂时未能接单%'", bob.id)
	count(1, "SELECT count(*) FROM messages WHERE event_type='HANDOFF_NOTICE' AND handoff_id=$1 AND conversation_id=$2 AND content LIKE '%自动结单%'", carol.id, op)
	send("private", "carol", "carol", "after", "结单后的问题")
	count(1, "SELECT count(*) FROM inbox_tasks t JOIN messages m ON m.id=t.message_id WHERE m.platform_message_id='after' AND t.status='pending' AND m.handoff_id IS NULL")
	// Competing transactions may both see the ticket in the waiting list.
	race := open(group, "dora", "dora-question")
	start := make(chan struct{})
	results := make(chan error, 2)
	for _, who := range []string{"web-one", "web-two"} {
		go func(name string) {
			<-start
			results <- pgx.BeginFunc(ctx, pool, func(tx pgx.Tx) error {
				return ticketAction(ctx, tx, race.id, ticketActor{"web", uuid.NewString(), name}, true)
			})
		}(who)
	}
	close(start)
	wins, conflicts := 0, 0
	for range 2 {
		err := <-results
		var conflict ticketConflict
		if err == nil {
			wins++
		} else if errors.As(err, &conflict) {
			conflicts++
		} else {
			t.Fatal(err)
		}
	}
	if wins != 1 || conflicts != 1 {
		t.Fatalf("race wins=%d conflicts=%d", wins, conflicts)
	}
	_, err = pool.Exec(ctx, "UPDATE handoffs SET paused_until=now()+interval '5 seconds' WHERE id=$1", race.id)
	check(err)
	send("group", "group", "dora", "web-follow", "仅网页接单人处理")
	count(1, "SELECT count(*) FROM handoffs WHERE id=$1 AND paused_until>now()+interval '6 minutes'", race.id)
	count(0, "SELECT count(*) FROM messages WHERE event_type='HANDOFF_NOTICE' AND content LIKE '%仅网页接单人处理%'")
	_, err = pool.Exec(ctx, "UPDATE handoffs SET paused_until=now() WHERE id=$1", race.id)
	check(err)
	check(a.expireHandoffs(ctx))
	count(1, "SELECT count(*) FROM system_alerts a JOIN handoffs h ON h.id::text=a.detail->>'handoffId' WHERE h.id=$1 AND h.claimed_by_id::text=a.detail->>'userId' AND a.kind='handoff_timeout'", race.id)

}
