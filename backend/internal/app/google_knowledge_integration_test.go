package app

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"ai-bot/backend/internal/config"
)

func TestGoogleKnowledgeScreenshotReply(t *testing.T) {
	url := os.Getenv("TEST_DATABASE_URL")
	if url == "" {
		t.Skip("TEST_DATABASE_URL is not configured")
	}
	ctx := context.Background()
	pool := newIntegrationSchema(t, ctx, url)
	setTestReplyQuietSecond(t, ctx, pool)
	mock := newUpstreamMock(t)
	defer mock.Close()
	const model = "gemini-embedding-2"
	type request struct {
		Model                string
		OutputDimensionality int
		Content              struct {
			Parts []struct {
				Text       string
				InlineData *struct {
					MimeType string
					Data     []byte
				}
			}
		}
	}
	vector := func(r request) []float32 {
		if r.Model != "models/"+model || r.OutputDimensionality != 128 {
			t.Errorf("unexpected model or dimension")
		}
		v := make([]float32, 128)
		var term string
		var img bool
		for _, p := range r.Content.Parts {
			term += p.Text
			if p.InlineData != nil {
				img = len(p.InlineData.Data) > 0 && (p.InlineData.MimeType == "image/png" || p.InlineData.MimeType == "image/jpeg")
			}
		}
		switch {
		case strings.Contains(term, "TOPAZ"):
			v[4] = 1
		case strings.Contains(term, "COBALT"):
			v[0] = 1
		case strings.Contains(term, "AMBER"):
			v[1] = 1
		case img || strings.Contains(term, "ORCHID"):
			v[2] = 1
		default:
			v[3] = 1
		}
		return v
	}
	gemini := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("x-goog-api-key") != "fixture" || r.Header.Get("Authorization") != "" {
			t.Error("incorrect Gemini authentication")
		}
		switch r.URL.Path {
		case "/models/" + model + ":embedContent":
			var req request
			if e := json.NewDecoder(r.Body).Decode(&req); e != nil {
				t.Error(e)
			}
			if len(req.Content.Parts) == 0 || req.Content.Parts[len(req.Content.Parts)-1].InlineData == nil {
				t.Error("expected actual image input")
			}
			json.NewEncoder(w).Encode(map[string]any{"embedding": map[string]any{"values": vector(req)}})
		case "/models/" + model + ":batchEmbedContents":
			var req struct{ Requests []request }
			json.NewDecoder(r.Body).Decode(&req)
			var out []map[string]any
			for _, item := range req.Requests {
				out = append(out, map[string]any{"values": vector(item)})
			}
			json.NewEncoder(w).Encode(map[string]any{"embeddings": out})
		default:
			http.NotFound(w, r)
		}
	}))
	defer gemini.Close()
	cfg := config.Config{DataDir: t.TempDir(), MasterKey: []byte("0123456789abcdef0123456789abcdef"), AdminUsername: "admin", AdminPassword: "integration-password", AIRequestTimeout: 10 * time.Second, QQAPIBaseURL: mock.URL, QQTokenURL: mock.URL + "/token", WorkerPoll: 10 * time.Millisecond}
	a, e := New(ctx, cfg, pool, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if e != nil {
		t.Fatal(e)
	}
	server := httptest.NewServer(a.Router())
	defer server.Close()
	jar, _ := cookiejar.New(nil)
	client := &http.Client{Jar: jar}
	api := server.URL + "/api"
	requestJSON(t, client, "POST", api+"/auth/login", map[string]string{"username": cfg.AdminUsername, "password": cfg.AdminPassword}, 200)
	profile := createdID(t, requestJSON(t, client, "POST", api+"/model-profiles", map[string]any{"provider": "google", "apiKey": "fixture", "dimension": 128}, 200))
	// Only this isolated schema points the fixed official preset at a mock.
	if _, e = pool.Exec(ctx, "UPDATE model_profiles SET base_url=$1 WHERE id=$2", gemini.URL, profile); e != nil {
		t.Fatal(e)
	}
	old := createdID(t, requestJSON(t, client, "POST", api+"/model-profiles", map[string]any{"name": "old", "kind": "embedding", "model": "old", "apiKey": "fixture", "baseUrl": mock.URL + "/v1", "dimension": 3}, 200))
	kb := createdID(t, requestJSON(t, client, "POST", api+"/knowledge-bases", map[string]string{"name": "steps", "embeddingProfileId": old}, 201))
	base := api + "/knowledge-bases/" + kb
	uploadDocument(t, client, base+"/documents", "old.txt", "legacy content")
	if e = a.processDocument(ctx); e != nil {
		t.Fatal(e)
	}
	requestJSON(t, client, "PUT", base+"/embedding-profile", map[string]string{"embeddingProfileId": profile}, 200)
	if e = a.processDocument(ctx); e != nil {
		t.Fatal(e)
	}
	var dims int
	if e = pool.QueryRow(ctx, "SELECT vector_dims(embedding) FROM knowledge_chunks WHERE knowledge_base_id=$1", kb).Scan(&dims); e != nil || dims != 128 {
		t.Fatalf("reindex: %d %v", dims, e)
	}
	ids := map[string]string{}
	chunks := map[string]string{}
	for _, f := range []struct{ name, term string }{{"cobalt-steps.pdf", "COBALT"}, {"amber-steps.docx", "AMBER"}, {"orchid.png", "ORCHID"}} {
		b, e := os.ReadFile("testdata/knowledge/" + f.name)
		if e != nil {
			t.Fatal(e)
		}
		ids[f.term] = uploadDocument(t, client, base+"/documents", f.name, string(b))
		if e = a.processDocument(ctx); e != nil {
			t.Fatal(e)
		}
	}
	for _, term := range []string{"COBALT", "AMBER", "ORCHID"} {
		raw := requestJSON(t, client, "POST", base+"/search", map[string]any{"query": term, "limit": 1}, 200)
		h := object(t, dataSlice(t, raw)[0])
		if h["document_id"] != ids[term] || h["hasImage"] != true {
			t.Fatalf("%s search: %s", term, raw)
		}
		chunks[term] = h["id"].(string)
	}
	res, e := client.Get(base + "/chunks/" + chunks["AMBER"] + "/image")
	if e != nil {
		t.Fatal(e)
	}
	pic, _ := io.ReadAll(res.Body)
	res.Body.Close()
	if res.StatusCode != 200 || len(pic) == 0 {
		t.Fatal("missing preview")
	}
	chat := createdID(t, requestJSON(t, client, "POST", api+"/model-profiles", map[string]any{"name": "chat", "kind": "chat", "model": "mock", "baseUrl": mock.URL + "/v1", "apiKey": "fixture", "isDefault": true}, 200))
	const secret = "qq-test-secret"
	bot := createdID(t, requestJSON(t, client, "POST", api+"/bots", map[string]any{"name": "test", "appId": "kb-screenshot-test", "appSecret": secret, "modelProfileId": chat, "enabled": true}, 201))
	work, stop := context.WithCancel(ctx)
	done := make(chan struct{})
	go func() { defer close(done); a.RunWorkers(work) }()
	defer func() { stop(); <-done }()
	for i, kind := range []string{"group", "private"} {
		platform, event, prefix := "group-open-id", "GROUP_AT_MESSAGE_CREATE", "/v2/groups/"
		if kind == "private" {
			platform, event, prefix = "user-open-id", "C2C_MESSAGE_CREATE", "/v2/users/"
		}
		var cid string
		if e = pool.QueryRow(ctx, "INSERT INTO conversations(channel,bot_id,platform_id,type,trigger_mode) VALUES('qq',$1,$2,$3,'always') RETURNING id::text", bot, platform, kind).Scan(&cid); e != nil {
			t.Fatal(e)
		}
		requestJSON(t, client, "PUT", api+"/conversations/"+cid, map[string]any{"name": kind, "triggerMode": "always", "chatProfileId": chat, "enabled": true, "contextLimit": 20, "knowledgeBaseIds": []string{kb}}, 200)
		mock.SetNextChatContent("请点击设置。 [[kb-image:" + chunks["AMBER"] + "]]")
		eventBody := qqEventBody(t, "evt-"+kind, event, "msg-"+kind, "AMBER 设置在哪里？")
		if kind == "private" {
			eventBody = bytes.ReplaceAll(eventBody, []byte(`"member_openid":"member-open-id"`), []byte(`"user_openid":"user-open-id"`))
		}
		postQQWebhook(t, client, server.URL+"/callbacks/qq/"+bot, secret, eventBody)
		eventually(t, 10*time.Second, "QQ screenshot "+kind, func() (bool, error) { return len(mock.QQSends()) >= i+1, nil })
		uploads := mock.QQUploads()
		sends := mock.QQSends()
		if len(uploads) != i+1 || uploads[i].Path != prefix+platform+"/files" || uploads[i].Body["file_data"] != base64.StdEncoding.EncodeToString(pic) || sends[i].Body["msg_type"] != float64(7) || !strings.Contains(sends[i].Body["content"].(string), "请点击设置") || strings.Contains(sends[i].Body["content"].(string), "kb-image") {
			t.Fatalf("incorrect %s delivery", kind)
		}
		var found bool
		for _, raw := range mock.ChatRequests() {
			if bytes.Contains(raw, []byte(chunks["AMBER"])) {
				found = true
			}
		}
		if !found {
			t.Fatal("missing screenshot citation in chat context")
		}
	}
	// A published FAQ has its own demonstration image. A verbatim answer also
	// attaches it when the model omits the optional citation syntax.
	faqPic := demoPNG(t)
	req := pictureRequest(t, api+"/knowledge/entries", map[string]string{"knowledgeBaseId": kb, "question": "TOPAZ 如何操作？", "answer": "请打开 TOPAZ 面板。"}, faqPic)
	response, err := client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	raw, err := io.ReadAll(response.Body)
	response.Body.Close()
	if err != nil || response.StatusCode != 201 {
		t.Fatalf("create FAQ: %s %v", raw, err)
	}
	id := createdID(t, raw)
	eventually(t, 8*time.Second, "FAQ index", func() (bool, error) {
		var ready bool
		e := pool.QueryRow(ctx, "SELECT index_status='ready' FROM knowledge_entries WHERE id=$1", id).Scan(&ready)
		return ready, e
	})
	mock.SetNextChatContent("请打开 TOPAZ 面板。")
	postQQWebhook(t, client, server.URL+"/callbacks/qq/"+bot, secret, qqEventBody(t, "evt-faq", "GROUP_AT_MESSAGE_CREATE", "msg-faq", "TOPAZ 如何操作？"))
	eventually(t, 10*time.Second, "QQ FAQ image", func() (bool, error) { return len(mock.QQSends()) >= 3, nil })
	upload := mock.QQUploads()
	send := mock.QQSends()[2].Body
	if len(upload) != 3 || upload[2].Body["file_data"] != base64.StdEncoding.EncodeToString(faqPic) || send["msg_type"] != float64(7) || !strings.Contains(fmt.Sprint(send["content"]), "请打开 TOPAZ 面板。") {
		t.Fatalf("FAQ delivery: %v", send)
	}

}

func TestScreenshotRequiresRetrievedCitation(t *testing.T) {
	hits := []map[string]any{{"id": "first", "hasImage": true}, {"id": "second", "hasImage": true}}
	for _, test := range []struct{ reply, want string }{{"答复", ""}, {"答复 [[kb-image:unknown]]", ""}, {"答复 [[kb-image:first]][[kb-image:second]]", "first"}} {
		clean, got := selectedKnowledgeImage(test.reply, hits)
		if got != test.want || clean != "答复" {
			t.Fatalf("%q -> %q %q", test.reply, clean, got)
		}
	}
}
