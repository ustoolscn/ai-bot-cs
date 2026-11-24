package app

import (
	"context"
	"errors"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
)

func TestReplyWindowDeadlinesAndIsolation(t *testing.T) {
	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("TEST_DATABASE_URL not set")
	}
	ctx := context.Background()
	pool := newIntegrationSchema(t, ctx, dsn)
	a := App{db: pool}
	var bot string
	if err := pool.QueryRow(ctx, "INSERT INTO bots(name,app_id,app_secret_enc) VALUES('win','win','x') RETURNING id::text").Scan(&bot); err != nil {
		t.Fatal(err)
	}
	conversation := func(platform string) string {
		t.Helper()
		var id string
		if err := pool.QueryRow(ctx, "INSERT INTO conversations(channel,bot_id,type,platform_id) VALUES('qq',$1,'private',$2) RETURNING id::text", bot, platform).Scan(&id); err != nil {
			t.Fatal(err)
		}
		return id
	}
	cid, other := conversation("one"), conversation("two")
	type arrival struct {
		id, batch string
		due, hard time.Time
	}
	add := func(cid, sender string, trigger bool) arrival {
		t.Helper()
		tx, err := pool.Begin(ctx)
		if err != nil {
			t.Fatal(err)
		}
		defer tx.Rollback(ctx)
		var m arrival
		if err = tx.QueryRow(ctx, "INSERT INTO messages(channel,bot_id,conversation_id,direction,sender_id,content) VALUES('qq',$1,$2,'inbound',$3,'text') RETURNING id::text", bot, cid, sender).Scan(&m.id); err != nil {
			t.Fatal(err)
		}
		queued, err := joinReplyWindow(ctx, tx, cid, sender, m.id, trigger)
		if err != nil {
			t.Fatal(err)
		}
		if queued {
			if err = tx.QueryRow(ctx, "SELECT reply_batch::text,reply_due_at,reply_max_at FROM messages WHERE id=$1", m.id).Scan(&m.batch, &m.due, &m.hard); err != nil {
				t.Fatal(err)
			}
			if _, err = tx.Exec(ctx, "INSERT INTO inbox_tasks(message_id) VALUES($1)", m.id); err != nil {
				t.Fatal(err)
			}
		}
		if err = tx.Commit(ctx); err != nil {
			t.Fatal(err)
		}
		return m
	}
	first := add(cid, "A", true)
	if first.hard.Sub(first.due) != 14*time.Second {
		t.Fatalf("default deadlines %+v", first)
	}
	if add(cid, "B", false).batch != "" || add(other, "A", false).batch != "" {
		t.Fatal("unrelated message joined")
	}
	second := add(cid, "A", false)
	if second.batch != first.batch || !second.hard.Equal(first.hard) || !second.due.After(first.due) {
		t.Fatalf("window did not slide: %+v %+v", first, second)
	}
	if _, err := a.claimInbox(ctx); !errors.Is(err, pgx.ErrNoRows) {
		t.Fatalf("claimed before silence: %v", err)
	}
	// Move the fixed deadline close, simulating a speaker already near the cap.
	if _, err := pool.Exec(ctx, "UPDATE messages SET reply_max_at=now()+interval '2 seconds',reply_due_at=now()+interval '1 second' WHERE reply_batch=$1", first.batch); err != nil {
		t.Fatal(err)
	}
	last := add(cid, "A", false)
	if last.batch != first.batch || !last.due.Equal(last.hard) {
		t.Fatal("continuous arrivals exceeded maximum")
	}
	if _, err := pool.Exec(ctx, "UPDATE messages SET reply_due_at=now()-interval '1 second' WHERE reply_batch=$1", first.batch); err != nil {
		t.Fatal(err)
	}
	next := add(cid, "A", true)
	if next.batch == last.batch {
		t.Fatal("late worker extended closed batch")
	}
	job, err := a.claimInbox(ctx)
	if err != nil || job.MessageID != last.id {
		t.Fatalf("wrong anchor: %+v %v", job, err)
	}
	parts, err := a.replySegments(ctx, job.MessageID)
	if err != nil || len(parts) != 3 || parts[0].ID != first.id || parts[2].ID != last.id {
		t.Fatalf("wrong segments: %+v %v", parts, err)
	}
	if _, err = a.claimInbox(ctx); !errors.Is(err, pgx.ErrNoRows) {
		t.Fatalf("duplicate claim: %v", err)
	}
}
