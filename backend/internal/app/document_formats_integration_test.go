package app

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"mime/multipart"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"ai-bot/backend/internal/config"
)

func TestKnowledgeUploadAndRetrieveFormats(t *testing.T) {
	dbURL := os.Getenv("TEST_DATABASE_URL")
	if dbURL == "" {
		t.Skip("TEST_DATABASE_URL is not configured")
	}
	ctx := context.Background()
	pool := newIntegrationSchema(t, ctx, dbURL)
	terms := []string{"COBALT", "AMBER", "退款"}
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req struct{ Input []string }
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, err.Error(), 400)
			return
		}
		var data []map[string]any
		for i, input := range req.Input {
			vec := make([]float32, len(terms))
			for j, term := range terms {
				if strings.Contains(input, term) {
					vec[j] = 1
				}
			}
			data = append(data, map[string]any{"embedding": vec, "index": i})
		}
		json.NewEncoder(w).Encode(map[string]any{"data": data})
	}))
	defer upstream.Close()
	cfg := config.Config{DataDir: t.TempDir(), MasterKey: []byte("0123456789abcdef0123456789abcdef"), AdminUsername: "admin", AdminPassword: "integration-password", AIRequestTimeout: 10 * time.Second}
	a, err := New(ctx, cfg, pool, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(a.Router())
	defer server.Close()
	jar, _ := cookiejar.New(nil)
	client := &http.Client{Jar: jar}
	api := server.URL + "/api"
	requestJSON(t, client, "POST", api+"/auth/login", map[string]string{"username": cfg.AdminUsername, "password": cfg.AdminPassword}, 200)
	profile := createdID(t, requestJSON(t, client, "POST", api+"/model-profiles", map[string]any{"name": "format test", "kind": "embedding", "baseUrl": upstream.URL + "/v1", "apiKey": "fixture", "model": "mock", "dimension": 3}, 200))
	kb := createdID(t, requestJSON(t, client, "POST", api+"/knowledge-bases", map[string]string{"name": "format test", "embeddingProfileId": profile}, 201))
	base := api + "/knowledge-bases/" + kb
	for _, test := range []struct {
		name, code  string
		description bool
	}{{"image.png", "image_model_required", false}, {"notes.txt", "invalid_description", true}} {
		var buf bytes.Buffer
		w := multipart.NewWriter(&buf)
		f, e := w.CreateFormFile("file", test.name)
		if e != nil {
			t.Fatal(e)
		}
		if _, e = f.Write([]byte("example")); e != nil {
			t.Fatal(e)
		}
		if test.description {
			if e = w.WriteField("description", ""); e != nil {
				t.Fatal(e)
			}
		}
		if e = w.Close(); e != nil {
			t.Fatal(e)
		}
		resp, e := client.Post(base+"/documents", w.FormDataContentType(), &buf)
		if e != nil {
			t.Fatal(e)
		}
		raw, e := io.ReadAll(resp.Body)
		resp.Body.Close()
		if e != nil || resp.StatusCode != 400 || !bytes.Contains(raw, []byte(test.code)) {
			t.Fatalf("upload %s: %d %s %v", test.name, resp.StatusCode, raw, e)
		}
	}
	ids := make([]string, 0, len(terms))
	for _, name := range []string{"cobalt.pdf", "amber.docx", "refund.txt"} {
		var raw []byte
		var err error
		if name == "refund.txt" {
			raw = []byte("退款规则")
		} else {
			raw, err = os.ReadFile("testdata/knowledge/" + name)
		}
		if err != nil {
			t.Fatal(err)
		}
		id := uploadDocument(t, client, base+"/documents", name, string(raw))
		ids = append(ids, id)
		if err := a.processDocument(ctx); err != nil {
			t.Fatal(err)
		}
		info := requestJSON(t, client, "GET", base, nil, 200)
		var view struct {
			Data struct {
				Documents []struct {
					ID, Status, LastError string
					ChunkCount            int
				}
			}
		}
		if err := json.Unmarshal(info, &view); err != nil {
			t.Fatal(err)
		}
		ready := false
		for _, doc := range view.Data.Documents {
			if doc.ID == id {
				ready = doc.Status == "ready" && doc.ChunkCount > 0
			}
		}
		if !ready {
			t.Fatalf("%s not indexed: %s", name, info)
		}
	}
	for i, term := range terms {
		hits := requestJSON(t, client, "POST", base+"/search", map[string]any{"query": term, "limit": 1}, 200)
		if !strings.Contains(string(hits), ids[i]) || !strings.Contains(string(hits), term) {
			t.Fatalf("%s search: %s", term, hits)
		}
	}
}
