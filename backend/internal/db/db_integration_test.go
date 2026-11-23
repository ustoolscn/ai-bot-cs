package db

import (
	"context"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
)

func TestPostgresPgvectorAndSkipLocked(t *testing.T) {
	url := os.Getenv("TEST_DATABASE_URL")
	if url == "" {
		t.Skip("TEST_DATABASE_URL is not configured")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	pool, err := Open(ctx, url)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	if err = Migrate(ctx, pool); err != nil {
		t.Fatal(err)
	}

	var columnExists bool
	if err = pool.QueryRow(ctx, `SELECT EXISTS(
		SELECT 1 FROM information_schema.columns
		WHERE table_name='bots' AND column_name='default_chat_profile_id'
	)`).Scan(&columnExists); err != nil || !columnExists {
		t.Fatalf("bot default model migration missing: exists=%v err=%v", columnExists, err)
	}

	table := "integration_jobs_" + strings.ReplaceAll(uuid.NewString(), "-", "")
	if _, err = pool.Exec(ctx, fmt.Sprintf(`CREATE TABLE %s (id serial PRIMARY KEY, status text NOT NULL, embedding vector NOT NULL)`, table)); err != nil {
		t.Fatal(err)
	}
	defer pool.Exec(context.Background(), "DROP TABLE IF EXISTS "+table) //nolint:errcheck
	if _, err = pool.Exec(ctx, fmt.Sprintf(`INSERT INTO %s(status,embedding) VALUES ('pending','[1,0]'::vector),('pending','[0,1]'::vector)`, table)); err != nil {
		t.Fatal(err)
	}
	var nearest int
	if err = pool.QueryRow(ctx, fmt.Sprintf(`SELECT id FROM %s ORDER BY embedding <=> '[0.9,0.1]'::vector LIMIT 1`, table)).Scan(&nearest); err != nil || nearest != 1 {
		t.Fatalf("unexpected nearest vector: id=%d err=%v", nearest, err)
	}

	conn1, err := pool.Acquire(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer conn1.Release()
	conn2, err := pool.Acquire(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer conn2.Release()
	tx1, err := conn1.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer tx1.Rollback(context.Background()) //nolint:errcheck
	tx2, err := conn2.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer tx2.Rollback(context.Background()) //nolint:errcheck
	var first, second int
	claimSQL := fmt.Sprintf(`SELECT id FROM %s WHERE status='pending' ORDER BY id FOR UPDATE SKIP LOCKED LIMIT 1`, table)
	if err = tx1.QueryRow(ctx, claimSQL).Scan(&first); err != nil {
		t.Fatal(err)
	}
	if err = tx2.QueryRow(ctx, claimSQL).Scan(&second); err != nil {
		t.Fatal(err)
	}
	if first == second {
		t.Fatalf("SKIP LOCKED returned the same row twice: %d", first)
	}
}

func TestLegacyHandoffMigration(t *testing.T) {
	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("TEST_DATABASE_URL is not configured")
	}
	ctx := context.Background()
	pool, err := Open(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback(ctx)
	run := func(query string) {
		t.Helper()
		if _, err := tx.Exec(ctx, query); err != nil {
			t.Fatal(err)
		}
	}
	schema := "handoff_migration_" + strings.ReplaceAll(uuid.NewString(), "-", "")
	run("CREATE SCHEMA " + schema)
	run("SET LOCAL search_path TO " + schema + ",public")
	files, err := migrations.ReadDir("migrations")
	if err != nil {
		t.Fatal(err)
	}
	apply := func(name string) {
		t.Helper()
		sql, err := migrations.ReadFile("migrations/" + name)
		if err != nil {
			t.Fatal(err)
		}
		run(string(sql))
	}
	for _, f := range files {
		if f.Name() < "026" {
			apply(f.Name())
		}
	}
	run(`WITH b AS(INSERT INTO bots(name,app_id,app_secret_enc) VALUES('test','app','secret') RETURNING id),
	c AS(INSERT INTO conversations(channel,bot_id,platform_id,type) SELECT 'qq',id,'group','group' FROM b RETURNING id,bot_id)
	INSERT INTO handoffs(bot_id,conversation_id,platform_user_id,status,resolved_at) SELECT bot_id,id,'alice','replied',now() FROM c`)
	apply("026_handoff_lifecycle.sql")
	apply("025_proxy_handoff.sql")
	apply("026_handoff_lifecycle.sql")
	var valid bool
	if err := tx.QueryRow(ctx, "SELECT status='resolved' AND close_method IS NULL AND resolved_at IS NOT NULL FROM handoffs WHERE platform_user_id='alice'").Scan(&valid); err != nil || !valid {
		t.Fatal("historical replied ticket was not migrated", err)
	}
}
