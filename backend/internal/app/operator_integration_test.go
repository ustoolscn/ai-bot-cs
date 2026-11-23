package app

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"testing"
	"time"

	"ai-bot/backend/internal/domain"
	"ai-bot/backend/internal/qq"
)

func TestOperatorTicketRouting(t *testing.T) {
	url := os.Getenv("TEST_DATABASE_URL")
	if url == "" {
		t.Skip("TEST_DATABASE_URL not set")
	}
	ctx := context.Background()
	pool := newIntegrationSchema(t, ctx, url)
	a := App{db: pool}
	if err := a.ensureRuntimeSettings(ctx); err != nil {
		t.Fatal(err)
	}
	var bot string
	if err := pool.QueryRow(ctx, "INSERT INTO bots(name,app_id,app_secret_enc) VALUES('bot','bot','secret') RETURNING id::text").Scan(&bot); err != nil {
		t.Fatal(err)
	}
	conv := func(kind, name string, op bool) string {
		var id string
		if err := pool.QueryRow(ctx, "INSERT INTO conversations(channel,bot_id,type,name,platform_id,human_operator) VALUES('qq',$1,$2,$3,$3,$4) RETURNING id::text", bot, kind, name, op).Scan(&id); err != nil {
			t.Fatal(err)
		}
		return id
	}
	conv("private", "operator", true)
	conv("private", "second", true)
	var ticket int64
	for _, name := range []string{"groupA", "groupB"} {
		cid := conv("group", name, false)
		var mid string
		err := pool.QueryRow(ctx, "INSERT INTO messages(bot_id,channel,conversation_id,direction,platform_message_id,sender_id,content) VALUES($1,'qq',$2,'inbound',$3,'alice','问题') RETURNING id::text", bot, cid, name).Scan(&mid)
		if err != nil {
			t.Fatal(err)
		}
		for range 2 {
			if _, err = a.handoffTurn(ctx, bot, cid, "alice", "Alice", mid, "转人工", time.Now().Add(time.Hour), true); err != nil {
				t.Fatal(err)
			}
		}
		if err = pool.QueryRow(ctx, "SELECT ticket_number FROM handoffs WHERE message_id=$1", mid).Scan(&ticket); err != nil {
			t.Fatal(err)
		}
	}
	var n int
	if err := pool.QueryRow(ctx, "SELECT count(*) FROM outbox_tasks t JOIN messages m ON m.id=t.message_id WHERE m.event_type='HANDOFF_NOTICE'").Scan(&n); err != nil || n != 4 {
		t.Fatalf("notices %d %v", n, err)
	}
	send := func(operator, event string) {
		msg := domain.InboundMessage{BotID: bot, ConversationType: "private", ConversationID: operator, SenderID: operator, SenderName: operator, PlatformMessageID: event, EventType: "C2C_MESSAGE_CREATE", Text: fmt.Sprintf("#%d 处理完毕", ticket), EventTime: time.Now(), Raw: json.RawMessage(`{}`)}
		if err := a.persistInbound(ctx, bot, qq.Envelope{ID: event, T: msg.EventType}, &msg, msg.Raw); err != nil {
			t.Fatal(err)
		}
	}
	send("operator", "cmd1")
	send("operator", "cmd1")
	send("second", "cmd2")
	var target, actor string
	if err := pool.QueryRow(ctx, "SELECT count(*),min(reply_to_message_id),min(sender_name) FROM messages WHERE event_type='HUMAN_REPLY'").Scan(&n, &target, &actor); err != nil || n != 1 || target != "groupB" || actor != "operator" {
		t.Fatalf("replies=%d target=%s actor=%s err=%v", n, target, actor, err)
	}
}
