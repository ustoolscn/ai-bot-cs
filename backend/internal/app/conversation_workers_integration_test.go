package app

import (
	"context"
	"errors"
	"log/slog"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
)

func TestConversationWorkerIsolation(t *testing.T) {
	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("TEST_DATABASE_URL not set")
	}
	for _, queue := range []string{"inbox", "outbox", "review"} {
		t.Run(queue, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			pool := newIntegrationSchema(t, ctx, dsn)
			a := App{db: pool, log: slog.Default()}
			a.cfg.WorkerPoll = time.Millisecond * 10
			id := func(query string, args ...any) string {
				t.Helper()
				var v string
				if err := pool.QueryRow(ctx, query, args...).Scan(&v); err != nil {
					t.Fatal(err)
				}
				return v
			}
			bot := id("INSERT INTO bots(name,app_id,app_secret_enc) VALUES('worker','worker','x') RETURNING id")
			group := func(name string) string {
				return id("INSERT INTO conversations(channel,bot_id,type,platform_id) VALUES('qq',$1,'group',$2) RETURNING id", bot, name)
			}
			g1, g2 := group("A"), group("B")
			table := queue + "_tasks"
			if queue == "review" {
				table = "agent_jobs"
			}
			enqueue := func(cid string) string {
				t.Helper()
				mid := id("INSERT INTO messages(channel,bot_id,conversation_id,direction) VALUES('qq',$1,$2,'inbound') RETURNING id", bot, cid)
				if queue == "review" {
					return id("INSERT INTO agent_jobs(kind,payload,dedupe_key) VALUES('group_review',jsonb_build_object('messageId',$1::text),'review:'||$1) RETURNING id", mid)
				}
				return id("INSERT INTO "+table+"(message_id) VALUES($1) RETURNING id", mid)
			}
			first, second, other := enqueue(g1), enqueue(g1), enqueue(g2)
			claim := func(ctx context.Context) (string, error) {
				switch queue {
				case "inbox":
					j, e := a.claimInbox(ctx)
					return j.TaskID, e
				case "outbox":
					j, e := a.claimOutbox(ctx)
					return j.TaskID, e
				default:
					j, e := a.claimGuardReview(ctx)
					return j.ID, e
				}
			}
			started := make(chan string, 3)
			release := make(chan struct{})
			done := make(chan struct{})
			go func() {
				defer close(done)
				dispatchJobs(ctx, &a, queue, claim, func(ctx context.Context, task string) error {
					started <- task
					select {
					case <-ctx.Done():
						return ctx.Err()
					case <-release:
					}
					_, err := pool.Exec(ctx, "UPDATE "+table+" SET status='completed' WHERE id=$1", task)
					return err
				})
			}()
			defer func() { cancel(); <-done }()
			next := func() string {
				t.Helper()
				select {
				case v := <-started:
					return v
				case <-ctx.Done():
					t.Fatal("timed out waiting for job")
					return ""
				}
			}
			seen := map[string]bool{next(): true, next(): true}
			if !seen[first] || !seen[other] {
				t.Fatalf("first jobs = %v, expected both group heads", seen)
			}
			if _, err := claim(ctx); !errors.Is(err, pgx.ErrNoRows) {
				t.Fatalf("claimed with both heads blocked: %v", err)
			}
			close(release)
			if got := next(); got != second {
				t.Fatalf("next = %s, want %s", got, second)
			}
		})
	}
}
