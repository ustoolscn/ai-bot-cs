package app

import (
	"ai-bot/backend/internal/secure"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"ai-bot/backend/internal/storage"
	"github.com/gin-gonic/gin"
)

func TestDeleteKnowledgeBase(t *testing.T) {
	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("TEST_DATABASE_URL not set")
	}
	ctx := t.Context()
	db := newIntegrationSchema(t, ctx, dsn)
	dir := t.TempDir()
	files, err := storage.NewLocal(dir)
	if err != nil {
		t.Fatal(err)
	}
	a := App{db: db, files: files}
	if err = a.ensureRuntimeSettings(ctx); err != nil {
		t.Fatal(err)
	}
	scalar := func(q string, args ...any) string {
		t.Helper()
		var v string
		if err := db.QueryRow(ctx, q, args...).Scan(&v); err != nil {
			t.Fatal(err)
		}
		return v
	}
	exec := func(q string, args ...any) {
		t.Helper()
		if _, err := db.Exec(ctx, q, args...); err != nil {
			t.Fatal(err)
		}
	}
	started, release := make(chan struct{}), make(chan struct{})
	model := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		close(started)
		<-release
		io.WriteString(w, `{"data":[{"index":0,"embedding":[1]}]}`)
	}))
	defer model.Close()
	resume := sync.OnceFunc(func() { close(release) })
	defer resume()
	a.cipher, err = secure.NewCipher(make([]byte, 32))
	if err != nil {
		t.Fatal(err)
	}
	secret, err := a.cipher.Encrypt("test")
	if err != nil {
		t.Fatal(err)
	}
	profile := scalar("INSERT INTO model_profiles(name,kind,base_url,api_key_enc,model) VALUES('emb','embedding',$1,$2,'emb') RETURNING id", model.URL, secret)
	kb := scalar("INSERT INTO knowledge_bases(name,embedding_profile_id) VALUES('target',$1) RETURNING id", profile)
	other := scalar("INSERT INTO knowledge_bases(name) VALUES('other') RETURNING id")
	bot := scalar("INSERT INTO bots(name,app_id,app_secret_enc) VALUES('bot','b','s') RETURNING id")
	svc := scalar("INSERT INTO bot_services(bot_id,name,settings) VALUES($1,'客服',jsonb_build_object('knowledgeBaseIds',jsonb_build_array($2::text,$3::text),'learningKnowledgeBaseId',$2::text)) RETURNING id", bot, kb, other)
	conv := scalar("INSERT INTO conversations(bot_id,channel,platform_id,type,name,service_id) VALUES($1,'qq','group','group','测试群',$2) RETURNING id", bot, svc)
	exec("INSERT INTO conversation_knowledge_bases VALUES($1,$2)", conv, kb)
	msg := scalar("INSERT INTO messages(channel,bot_id,conversation_id,direction) VALUES('qq',$1,$2,'inbound') RETURNING id", bot, conv)
	exec("UPDATE messages SET knowledge_base_id=$1 WHERE id=$2", kb, msg)
	feedback := scalar("INSERT INTO message_feedback(message_id,rating) VALUES($1,'bad') RETURNING id", msg)
	key, err := files.Save(ctx, "doc.txt", []byte("text"))
	if err != nil {
		t.Fatal(err)
	}
	doc := scalar("INSERT INTO knowledge_documents(knowledge_base_id,name,storage_key,content_type,size_bytes,status) VALUES($1,'doc.txt',$2,'text/plain',4,'pending') RETURNING id", kb, key)
	chunk := scalar("INSERT INTO knowledge_chunks(document_id,knowledge_base_id,chunk_index,content,embedding) VALUES($1,$2,0,'text','[1]') RETURNING id", doc, kb)
	exec("INSERT INTO knowledge_chunk_images VALUES($1,'image/png','data')", chunk)
	entry := scalar("INSERT INTO knowledge_entries(knowledge_base_id,question,answer) VALUES($1,'q','a') RETURNING id", kb)
	exec("INSERT INTO knowledge_candidates(knowledge_base_id,question,answer,source) VALUES($1,'q','a','manual')", kb)
	exec("INSERT INTO knowledge_gaps(knowledge_base_id,question) VALUES($1,'owned'),(NULL,'shared')", kb)
	exec("INSERT INTO eval_cases(entry_id,question) VALUES($1,'owned'),(NULL,'manual')", entry)
	exec("INSERT INTO agent_jobs(kind,payload) VALUES('index_entry',jsonb_build_object('entryId',$1::text))", entry)
	r := gin.New()
	r.GET("/:id", a.getKB)
	r.DELETE("/:id", a.deleteKB)
	request := func(method string, status int) []byte {
		t.Helper()
		w := httptest.NewRecorder()
		r.ServeHTTP(w, httptest.NewRequest(method, "/"+kb, nil))
		if w.Code != status {
			t.Fatalf("%s: %d %s", method, w.Code, w.Body.String())
		}
		return w.Body.Bytes()
	}
	var detail struct {
		Data struct{ BoundAgents, BoundConversations []struct{ ID, Name string } }
	}
	if err = json.Unmarshal(request("GET", 200), &detail); err != nil {
		t.Fatal(err)
	}
	if len(detail.Data.BoundAgents) != 1 || detail.Data.BoundAgents[0].ID != svc || len(detail.Data.BoundConversations) != 1 || detail.Data.BoundConversations[0].ID != conv {
		t.Fatal(detail)
	}
	done := make(chan error, 1)
	go func() { done <- a.processDocument(ctx) }()
	select {
	case <-started:
	case err := <-done:
		t.Fatalf("worker exited before embedding: %v %s", err, scalar("SELECT last_error FROM knowledge_documents WHERE id=$1", doc))
	case <-time.After(5 * time.Second):
		t.Fatal("embedding request timed out")
	}
	var result struct{ Data struct{ Deleted bool } }
	if err = json.Unmarshal(request("DELETE", 200), &result); err != nil || !result.Data.Deleted {
		t.Fatal(result, err)
	}
	resume()
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	request("DELETE", 404)
	if _, err = files.Read(ctx, key); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("file still present", err)
	}
	for _, table := range []string{"knowledge_documents", "knowledge_entries", "knowledge_candidates", "knowledge_chunks", "knowledge_chunk_images", "conversation_knowledge_bases", "deleted_knowledge_files"} {
		if scalar("SELECT count(*)::text FROM "+table) != "0" {
			t.Error(table)
		}
	}
	if scalar("SELECT count(*)::text FROM agent_jobs WHERE payload->>'entryId'=$1", entry) != "0" || scalar("SELECT question FROM knowledge_gaps") != "shared" || scalar("SELECT count(*)::text FROM eval_cases WHERE question='owned'") != "0" {
		t.Fatal("owned learning data retained")
	}
	if scalar("SELECT (settings->'knowledgeBaseIds'=jsonb_build_array($2::text) AND settings->>'learningKnowledgeBaseId'='')::text FROM bot_services WHERE id=$1", svc, other) != "true" {
		t.Fatal("agent settings")
	}
	if scalar("SELECT (m.knowledge_base_id IS NULL AND f.knowledge_base_id IS NULL)::text FROM messages m JOIN message_feedback f ON f.message_id=m.id WHERE f.id=$1", feedback) != "true" {
		t.Fatal("history attribution")
	}
	t.Run("磁盘清理失败后重试", func(t *testing.T) {
		kb = other
		const key = "blocked.txt"
		blocked := filepath.Join(dir, key)
		if err := os.Mkdir(blocked, 0700); err != nil {
			t.Fatal(err)
		}
		child := filepath.Join(blocked, "child")
		if err := os.WriteFile(child, nil, 0600); err != nil {
			t.Fatal(err)
		}
		exec("INSERT INTO knowledge_documents(knowledge_base_id,name,storage_key,content_type,size_bytes) VALUES($1,'blocked.txt',$2,'text/plain',0)", kb, key)
		request("DELETE", 500)
		if scalar("SELECT storage_key FROM deleted_knowledge_files WHERE knowledge_base_id=$1", kb) != key {
			t.Fatal("missing retry record")
		}
		if err := os.Remove(child); err != nil {
			t.Fatal(err)
		}
		if err := a.cleanDeletedKnowledgeFiles(ctx, nil); err != nil {
			t.Fatal(err)
		}
		if _, err := os.Stat(blocked); !errors.Is(err, os.ErrNotExist) {
			t.Fatal("retry did not remove file", err)
		}
		if scalar("SELECT count(*)::text FROM deleted_knowledge_files") != "0" {
			t.Fatal("retry record retained")
		}
	})
}
