package app

import (
	"ai-bot/backend/internal/storage"
	"bytes"
	"encoding/json"
	"github.com/gin-gonic/gin"
	"mime/multipart"
	"net/http/httptest"
	"os"
	"testing"
)

func TestKnowledgeArchiveRoundTrip(t *testing.T) {
	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("TEST_DATABASE_URL not set")
	}
	ctx := t.Context()
	db := newIntegrationSchema(t, ctx, dsn)
	files, e := storage.NewLocal(t.TempDir())
	if e != nil {
		t.Fatal(e)
	}
	a := App{files: files, db: db}
	id := func(q string, args ...any) string {
		t.Helper()
		var v string
		if e := db.QueryRow(ctx, q, args...).Scan(&v); e != nil {
			t.Fatal(e)
		}
		return v
	}
	profile := id("INSERT INTO model_profiles(name,kind,base_url,api_key_enc,model) VALUES('embed','embedding','','','e') RETURNING id")
	kb := id("INSERT INTO knowledge_bases(name,description,embedding_profile_id) VALUES('kb','说明',$1) RETURNING id", profile)
	entry := id("INSERT INTO knowledge_entries(knowledge_base_id,question,answer) VALUES($1,'q','a') RETURNING id", kb)
	cand := id("INSERT INTO knowledge_candidates(knowledge_base_id,question,answer,source,entry_id) VALUES($1,'q','a','manual',$2) RETURNING id", kb, entry)
	pic := demoPNG(t)
	for table, key := range map[string]string{"knowledge_entries": entry, "knowledge_candidates": cand} {
		id("UPDATE "+table+" SET image_mime='image/png',image_data=$2 WHERE id=$1 RETURNING id", key, pic)
	}
	id("INSERT INTO knowledge_gaps(question,knowledge_base_id,candidate_id) VALUES('q',$1,$2) RETURNING id", kb, cand)
	id("INSERT INTO eval_cases(question,entry_id) VALUES('q',$1) RETURNING id", entry)
	key, e := files.Save(ctx, "guide.txt", []byte("abc"))
	if e != nil {
		t.Fatal(e)
	}
	id("INSERT INTO knowledge_documents(knowledge_base_id,name,storage_key,content_type,size_bytes) VALUES($1,'guide.txt',$2,'text/plain',3) RETURNING id", kb, key)
	r := gin.New()
	r.GET("/kb/:id", a.exportKnowledge)
	r.POST("/import", a.importKnowledge)
	out := httptest.NewRecorder()
	r.ServeHTTP(out, httptest.NewRequest("GET", "/kb/"+kb, nil))
	if out.Code != 200 {
		t.Fatal(out.Body.String())
	}
	var body bytes.Buffer
	w := multipart.NewWriter(&body)
	part, e := w.CreateFormFile("file", "kb.zip")
	if e != nil {
		t.Fatal(e)
	}
	_, _ = part.Write(out.Body.Bytes())
	_ = w.WriteField("embeddingProfileId", profile)
	_ = w.Close()
	req := httptest.NewRequest("POST", "/import", &body)
	req.Header.Set("Content-Type", w.FormDataContentType())
	out = httptest.NewRecorder()
	r.ServeHTTP(out, req)
	if out.Code != 201 {
		t.Fatal(out.Body.String())
	}
	var res struct{ Data struct{ ID string } }
	if e = json.Unmarshal(out.Body.Bytes(), &res); e != nil {
		t.Fatal(e)
	}
	newKey := id("SELECT storage_key FROM knowledge_documents WHERE knowledge_base_id=$1", res.Data.ID)
	b, e := files.Read(ctx, newKey)
	if newKey == key || string(b) != "abc" || e != nil {
		t.Fatal("original file did not round trip")
	}
	id("SELECT e.id FROM knowledge_entries e JOIN knowledge_candidates c ON c.entry_id=e.id JOIN knowledge_gaps g ON g.candidate_id=c.id JOIN eval_cases v ON v.entry_id=e.id JOIN agent_jobs j ON j.payload->>'entryId'=e.id::text WHERE e.knowledge_base_id=$1 AND e.image_data=$2 AND c.image_data=$2 AND e.image_mime=c.image_mime", res.Data.ID, pic)
}
