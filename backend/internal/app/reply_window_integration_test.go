package app

import (
	"ai-bot/backend/internal/config"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"sync/atomic"
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
	// Moderation may outlast the quiet window. Keep its decision as a
	// prerequisite and include the old text only once it is allowed.
	review := add(other, "R", true)
	if _, err = pool.Exec(ctx, "DELETE FROM inbox_tasks WHERE message_id=$1", review.id); err != nil {
		t.Fatal(err)
	}
	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if err = a.queueReview(ctx, tx, review.id, true); err != nil {
		t.Fatal(err)
	}
	if err = tx.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err = pool.Exec(ctx, "UPDATE messages SET reply_due_at=now()-interval '1 second' WHERE id=$1", review.id); err != nil {
		t.Fatal(err)
	}
	follow := add(other, "R", false)
	if follow.batch == "" {
		t.Fatal("pending review did not carry trigger")
	}
	if _, err = pool.Exec(ctx, "UPDATE messages SET reply_due_at=now()-interval '1 second' WHERE id=$1", follow.id); err != nil {
		t.Fatal(err)
	}
	if _, err = a.claimInbox(ctx); !errors.Is(err, pgx.ErrNoRows) {
		t.Fatalf("claimed during review: %v", err)
	}
	if _, err = pool.Exec(ctx, "UPDATE group_reviews SET status='allowed' WHERE message_id=$1", review.id); err != nil {
		t.Fatal(err)
	}
	if _, err = pool.Exec(ctx, "INSERT INTO inbox_tasks(message_id) VALUES($1)", review.id); err != nil {
		t.Fatal(err)
	}
	job, err = a.claimInbox(ctx)
	if err != nil || job.MessageID != follow.id {
		t.Fatalf("review follow-up claim: %+v, %v", job, err)
	}
	parts, err = a.replySegments(ctx, job.MessageID)
	if err != nil || len(parts) != 2 || parts[0].ID != review.id {
		t.Fatalf("review segments: %+v %v", parts, err)
	}

}

func TestReplyInterruptedDuringGenerationAndDelivery(t *testing.T) {
	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("TEST_DATABASE_URL not set")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 25*time.Second)
	defer cancel()
	pool := newIntegrationSchema(t, ctx, dsn)
	entered, release := make(chan struct{}), make(chan struct{})
	var calls atomic.Int32
	model := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if calls.Add(1) == 1 {
			close(entered)
			select {
			case <-release:
			case <-ctx.Done():
				return
			}
		}
		fmt.Fprint(w, `{"choices":[{"message":{"role":"assistant","content":"合并回答"}}]}`)
	}))
	defer model.Close()
	check := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}
	tokenStarted, tokenReady := make(chan struct{}), make(chan struct{})
	var deliveries atomic.Int32
	qqServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/token" {
			close(tokenStarted)
			select {
			case <-tokenReady:
			case <-ctx.Done():
				return
			}

			fmt.Fprint(w, `{"access_token":"token","expires_in":"7200"}`)
			return
		}
		deliveries.Add(1)
		fmt.Fprint(w, `{"id":"qq-receipt"}`)
	}))
	defer qqServer.Close()
	a, err := New(ctx, config.Config{DataDir: t.TempDir(), MasterKey: []byte("0123456789abcdef0123456789abcdef"), AdminUsername: "admin", AdminPassword: "integration-password", QQAPIBaseURL: qqServer.URL, QQTokenURL: qqServer.URL + "/token", AIRequestTimeout: 20 * time.Second}, pool, slog.Default())
	check(err)
	secret, err := a.cipher.Encrypt("key")
	check(err)
	exec := func(query string, args ...any) { t.Helper(); _, err := pool.Exec(ctx, query, args...); check(err) }
	exec("INSERT INTO model_profiles(name,kind,base_url,api_key_enc,model,is_default) VALUES('test','chat',$1,$2,'test',true)", model.URL+"/v1", secret)
	var bot, cid string
	check(pool.QueryRow(ctx, "INSERT INTO bots(name,app_id,app_secret_enc) VALUES('bot','app',$1) RETURNING id", secret).Scan(&bot))
	check(pool.QueryRow(ctx, "INSERT INTO conversations(channel,bot_id,platform_id,type) VALUES('qq',$1,'chat','private') RETURNING id", bot).Scan(&cid))
	add := func(pid, sender string, trigger bool) bool {
		t.Helper()
		tx, err := pool.Begin(ctx)
		check(err)
		defer tx.Rollback(ctx)
		var id string
		check(tx.QueryRow(ctx, `INSERT INTO messages(channel,bot_id,conversation_id,direction,sender_id,platform_message_id,content)
		VALUES('qq',$1,$2,'inbound',$3,$4,$4) RETURNING id`, bot, cid, sender, pid).Scan(&id))
		joined, err := joinReplyWindow(ctx, tx, cid, sender, id, trigger)
		check(err)
		if joined {
			_, err = tx.Exec(ctx, "INSERT INTO inbox_tasks(message_id) VALUES($1)", id)
			check(err)
		}
		check(tx.Commit(ctx))
		return joined
	}
	due := func() {
		exec("UPDATE messages SET reply_due_at=now()-interval '1 second' WHERE conversation_id=$1", cid)
	}
	add("first", "A", true)
	due()
	done := make(chan error, 1)
	go func() { done <- a.processInbox(ctx) }()
	select {
	case <-entered:
	case err := <-done:
		t.Fatalf("worker exited before model: %v", err)
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	// Seed a checkpoint from the running turn. The next turn can only
	// recover this content from the current, conversation-scoped chunk.
	var kb, chunk string
	check(pool.QueryRow(ctx, "INSERT INTO knowledge_bases(name) VALUES('FAQ') RETURNING id").Scan(&kb))
	exec("INSERT INTO conversation_knowledge_bases VALUES($1,$2)", cid, kb)
	check(pool.QueryRow(ctx, `WITH d AS (INSERT INTO knowledge_documents(knowledge_base_id,name,storage_key,content_type,size_bytes)
	VALUES($1,'test','test','text/plain',1) RETURNING id)
	INSERT INTO knowledge_chunks(knowledge_base_id,document_id,chunk_index,content,embedding)
	SELECT $1,id,0,'checkpoint-evidence','[1]' FROM d RETURNING id`, kb).Scan(&chunk))
	exec("UPDATE agent_runs SET retrieved_chunks=jsonb_build_array(jsonb_build_object('id',$1::text))", chunk)
	if add("other", "B", false) || !add("second", "A", false) {
		t.Fatal("sender isolation failed")
	}
	close(release)
	check(<-done)
	var count int
	check(pool.QueryRow(ctx, "SELECT count(*) FROM outbox_tasks").Scan(&count))
	if count != 0 {
		t.Fatal("stale generation queued")
	}
	due()
	check(a.processInbox(ctx))
	var reused bool
	check(pool.QueryRow(ctx, `SELECT r.context_messages::text LIKE '%checkpoint-evidence%' FROM agent_runs r
	JOIN messages m ON m.id=r.message_id WHERE m.platform_message_id='second'`).Scan(&reused))
	if !reused {
		t.Fatal("combined turn omitted retrieval checkpoint")
	}
	old, err := a.claimOutbox(ctx)
	check(err)
	go func() { done <- a.handleOutbox(ctx, old) }()
	select {
	case <-tokenStarted:
	case err := <-done:
		t.Fatalf("delivery exited before QQ credentials: %v", err)
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	if old.ReplyTo != "second" || !add("third", "A", false) {
		t.Fatal("queued reply did not join")
	}
	close(tokenReady)
	check(<-done)
	var state string
	check(pool.QueryRow(ctx, "SELECT status FROM outbox_tasks WHERE id=$1", old.TaskID).Scan(&state))
	if state != "merged" {
		t.Fatalf("old delivery: %s", state)
	}
	due()
	job, err := a.claimInbox(ctx)
	check(err)
	segments, err := a.replySegments(ctx, job.MessageID)
	check(err)
	if segmentText(segments) != "first\nsecond\nthird" {
		t.Fatalf("combined input: %+v", segments)
	}
	check(a.handleInbox(ctx, job))
	check(pool.QueryRow(ctx, "SELECT count(*) FROM outbox_tasks WHERE status='pending'").Scan(&count))
	if count != 1 {
		t.Fatalf("pending replies: %d", count)
	}
	sent, err := a.claimOutbox(ctx)
	check(err)
	if sent.ReplyTo != "third" {
		t.Fatal(sent)
	}
	check(a.handleOutbox(ctx, sent))
	if deliveries.Load() != 1 {
		t.Fatalf("QQ received %d replies", deliveries.Load())
	}
	if add("after", "A", false) {
		t.Fatal("completed reply joined")
	}
	check(pool.QueryRow(ctx, "SELECT status FROM outbox_tasks WHERE id=$1", sent.TaskID).Scan(&state))
	if state != "sent" {
		t.Fatal(state)
	}
	// When QQ has accepted only the first part, keep that receipt and
	// answer the follow-up despite the earlier part in the same batch.
	exec(`WITH rest AS (INSERT INTO messages(channel,bot_id,conversation_id,direction,event_type,content,reply_to_message_id,status)
	VALUES('qq',$1,$2,'outbound','AI_RESPONSE','第二段','third','pending') RETURNING id)
	INSERT INTO outbox_tasks(message_id,msg_seq) SELECT id,2 FROM rest`, bot, cid)
	if !add("fourth", "A", false) {
		t.Fatal("remaining reply part did not merge")
	}
	due()
	check(a.processInbox(ctx))
	next, err := a.claimOutbox(ctx)
	check(err)
	if next.ReplyTo != "fourth" {
		t.Fatalf("partial delivery prevented new answer: %+v", next)
	}
	check(pool.QueryRow(ctx, "SELECT status FROM outbox_tasks WHERE id=$1", sent.TaskID).Scan(&state))
	if state != "sent" {
		t.Fatal("sent part was superseded")
	}

}
