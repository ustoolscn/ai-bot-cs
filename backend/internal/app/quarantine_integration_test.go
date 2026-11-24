package app

import (
	"context"
	"os"
	"testing"
)

func TestModerationStreakIsolation(t *testing.T) {
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
	var bot string
	check(pool.QueryRow(ctx, `INSERT INTO bots(name,app_id,app_secret_enc) VALUES('bot','bot','secret') RETURNING id::text`).Scan(&bot))
	conversation := func(name string) string {
		t.Helper()
		var id string
		check(pool.QueryRow(ctx, `INSERT INTO conversations(channel,bot_id,platform_id,type,moderation_cutoff) VALUES('qq',$1,$2,'group','2020-01-01') RETURNING id::text`, bot, name).Scan(&id))
		_, err := pool.Exec(ctx, `INSERT INTO conversation_summaries(conversation_id,summary) VALUES($1,'原有摘要')`, id)
		check(err)
		return id
	}
	cid, other := conversation("first"), conversation("other")
	message := func(cid, pid string) string {
		t.Helper()
		var id string
		check(pool.QueryRow(ctx, `INSERT INTO messages(channel,bot_id,conversation_id,direction,platform_message_id,content) VALUES('qq',$1,$2,'inbound',$3,$3) RETURNING id::text`, bot, cid, pid).Scan(&id))
		return id
	}
	history, first, second := message(cid, "older"), message(cid, "first"), message(cid, "second")
	foreign := message(other, "foreign")
	_, err := pool.Exec(ctx, `INSERT INTO messages(channel,bot_id,conversation_id,direction,event_type,reply_to_message_id)
 SELECT 'qq',$1,$2,'outbound',kind,ref FROM (VALUES ('AI_RESPONSE','first'),('AI_RESPONSE','first'),('HUMAN_REPLY','first'),('AI_RESPONSE','older')) f(kind,ref)`, bot, cid)
	check(err)
	intact := func(cid string) bool {
		t.Helper()
		var ok bool
		check(pool.QueryRow(ctx, `SELECT moderation_cutoff='2020-01-01' AND EXISTS(SELECT FROM conversation_summaries WHERE conversation_id=c.id AND summary='原有摘要') FROM conversations c WHERE id=$1`, cid).Scan(&ok))
		return ok
	}
	// Two workers can report the same message at once.
	result := make(chan error, 2)
	for i := 0; i < 2; i++ {
		go func() { result <- a.quarantineBackground(ctx, first) }()
	}
	check(<-result)
	check(<-result)
	check(a.quarantineBackground(ctx, foreign))
	var excluded, visibleHistory int
	check(pool.QueryRow(ctx, `SELECT count(*) FILTER(WHERE context_excluded),count(*) FILTER(WHERE id=$2 AND `+visibleContextSQL+`) FROM messages m WHERE conversation_id=$1`, cid, history).Scan(&excluded, &visibleHistory))
	if excluded != 3 || visibleHistory != 1 || !intact(cid) || !intact(other) {
		t.Fatalf("first failure affected unrelated context: excluded=%d history=%d", excluded, visibleHistory)
	}
	// Expired strikes do not combine. An old job may finish after a newer one.
	_, err = pool.Exec(ctx, `UPDATE conversations SET moderation_pending_at=now()-interval '11 minutes' WHERE id=$1`, cid)
	check(err)
	check(a.quarantineBackground(ctx, second))
	if !intact(cid) {
		t.Fatal("expired strike advanced legacy cutoff")
	}
	check(a.quarantineBackground(ctx, history))
	var reset bool
	check(pool.QueryRow(ctx, `SELECT moderation_cutoff=(SELECT event_at FROM messages WHERE id=$2) AND moderation_pending_at IS NULL AND NOT EXISTS(SELECT FROM conversation_summaries WHERE conversation_id=c.id) FROM conversations c WHERE id=$1`, cid, second).Scan(&reset))
	if !reset || !intact(other) {
		t.Fatal("repeated failure did not reset just the affected conversation")
	}
}
