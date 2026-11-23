package app

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"sync/atomic"
	"testing"

	"ai-bot/backend/internal/config"
)

func TestEmbeddingRequestCounts(t *testing.T) {
	url := os.Getenv("TEST_DATABASE_URL")
	if url == "" {
		t.Skip("TEST_DATABASE_URL not configured")
	}
	ctx := context.Background()
	db := newIntegrationSchema(t, ctx, url)
	check := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}
	a, err := New(ctx, config.Config{DataDir: t.TempDir(), MasterKey: []byte("0123456789abcdef0123456789abcdef"), AdminUsername: "admin", AdminPassword: "test-password"}, db, slog.New(slog.NewTextHandler(io.Discard, nil)))
	check(err)
	var calls atomic.Int32
	model := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/embeddings" {
			t.Errorf("unexpected endpoint %s", r.URL.Path)
		}
		calls.Add(1)
		fmt.Fprint(w, `{"data":[{"index":0,"embedding":[1,0,0]}]}`)
	}))
	defer model.Close()
	count := func(stage string, want int32) {
		t.Helper()
		got := calls.Swap(0)
		t.Logf("%s: %d embedding requests", stage, got)
		if got != want {
			t.Fatalf("want %d", want)
		}
	}
	id := func(sql string, args ...any) string {
		t.Helper()
		var v string
		check(db.QueryRow(ctx, sql, args...).Scan(&v))
		return v
	}
	exec := func(sql string, args ...any) {
		t.Helper()
		_, err := db.Exec(ctx, sql, args...)
		check(err)
	}
	key, err := a.cipher.Encrypt("test")
	check(err)
	pid := id(`INSERT INTO model_profiles(name,kind,base_url,api_key_enc,model,dimension) VALUES('emb','embedding',$1,$2,'emb',3) RETURNING id::text`, model.URL, key)
	bot := id(`INSERT INTO bots(name,app_id,app_secret_enc) VALUES('b','a',$1) RETURNING id::text`, key)
	conv := id(`INSERT INTO conversations(channel,bot_id,type,platform_id) VALUES('qq',$1,'group','g') RETURNING id::text`, bot)
	kb := func() string {
		return id(`INSERT INTO knowledge_bases(name,embedding_profile_id) VALUES('kb',$1) RETURNING id::text`, pid)
	}
	libraries := []string{kb(), kb()}
	var cache queryEmbeddings
	s := a.agentSettings(ctx)
	s.EmbeddingProfileID = pid
	query := "相同的问题"
	memory := func() int { return len(a.loadMemories(ctx, s, bot, conv, "user", query, &cache)) }
	search := func(text string) int {
		_, hits, err := a.retrieve(ctx, libraries, text, &cache)
		check(err)
		return len(hits)
	}
	if memory() != 0 || search(query) != 0 {
		t.Fatal("unexpected candidates")
	}
	count("empty memory and two empty libraries", 0)
	exec(`INSERT INTO user_memories(bot_id,conversation_id,platform_user_id,content,embedding,embedding_profile_id) VALUES($1,$2,'user','记忆','[1,0,0]',$3)`, bot, conv, pid)
	for _, k := range libraries {
		entry := id(`INSERT INTO knowledge_entries(knowledge_base_id,question,answer) VALUES($1,'问题','回答') RETURNING id::text`, k)
		exec(`INSERT INTO knowledge_chunks(knowledge_base_id,entry_id,chunk_index,content,embedding) VALUES($1,$2,0,'知识','[1,0,0]')`, k, entry)
	}
	cache = nil
	if memory() != 1 || search(query) != 2 {
		t.Fatal("missing candidates")
	}
	count("memory and two populated libraries", 1)
	if search(query) != 2 {
		t.Fatal("missing repeat results")
	}
	count("tool repeating the search", 0)
	if search("改写的问题") != 2 {
		t.Fatal("missing rewritten results")
	}
	count("different query", 1)
	otherProfile := id(`INSERT INTO model_profiles(name,kind,base_url,api_key_enc,model,dimension) VALUES('other','embedding',$1,$2,'other',3) RETURNING id::text`, model.URL, key)
	exec(`UPDATE knowledge_bases SET embedding_profile_id=$2 WHERE id=$1`, libraries[1], otherProfile)
	if search(query) != 2 {
		t.Fatal("missing hits for second profile")
	}
	count("same query, one additional model profile", 1)
	cache = nil
	if search(query) != 2 {
		t.Fatal("missing hits in fresh turn")
	}
	count("fresh turn with two profiles", 2)

}
