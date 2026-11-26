package app

import (
	"bytes"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	"ai-bot/backend/internal/db"
	"github.com/gin-gonic/gin"
)

func TestMixedMaterialsEditing(t *testing.T) {
	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("TEST_DATABASE_URL not set")
	}
	pool := newIntegrationSchema(t, t.Context(), dsn)
	a := App{db: pool}
	var kb string
	if err := pool.QueryRow(t.Context(), "INSERT INTO knowledge_bases(name) VALUES('demo') RETURNING id").Scan(&kb); err != nil {
		t.Fatal(err)
	}
	router := gin.New()
	router.POST("/entries", a.createEntryAPI)
	router.PUT("/entries/:id", a.updateEntryAPI)
	router.GET("/list", a.listEntries)
	router.GET("/api/knowledge/entries/:id/materials/:materialId", a.materialPreview("knowledge_entries"))
	fields := map[string]string{"knowledgeBaseId": kb, "question": "步骤？", "answer": "如下"}
	send := func(req *http.Request, status int) *httptest.ResponseRecorder {
		t.Helper()
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)
		if w.Code != status {
			t.Fatalf("%d want %d: %s", w.Code, status, w.Body.String())
		}
		return w
	}
	video, err := os.ReadFile("testdata/knowledge/demo.mp4")
	if err != nil {
		t.Fatal(err)
	}
	id := createdID(t, send(mediaRequest(t, "/entries", fields, "files", demoPNG(t), video), 201).Body.Bytes())
	list := func() []any {
		raw := send(httptest.NewRequest("GET", "/list", nil), 200).Body.Bytes()
		return object(t, dataSlice(t, raw)[0])["materials"].([]any)
	}
	old := list()
	clip := object(t, old[1])
	if clip["kind"] != "video" || clip["sizeBytes"] != float64(len(video)) {
		t.Fatal(clip)
	}
	req := httptest.NewRequest("GET", clip["url"].(string), nil)
	req.Header.Set("Range", "bytes=0-7")
	if !bytes.Equal(send(req, 206).Body.Bytes(), video[:8]) {
		t.Fatal("bad range")
	}
	fields["materials"] = fmt.Sprintf(`["keep:%s","new:0"]`, clip["id"])
	req = mediaRequest(t, "/entries/"+id, fields, "files", demoPNG(t))
	req.Method = "PUT"
	send(req, 200)
	edited := list()
	if len(edited) != 2 || object(t, edited[0])["id"] != clip["id"] || object(t, edited[1])["id"] == object(t, old[0])["id"] {
		t.Fatal(edited)
	}
	fields["materials"] = fmt.Sprintf(`["keep:%s"]`, object(t, old[0])["id"])
	req = mediaRequest(t, "/entries/"+id, fields, "files")
	req.Method = "PUT"
	send(req, 400)
	fields["materials"] = "[]"
	req = mediaRequest(t, "/entries/"+id, fields, "files")
	req.Method = "PUT"
	send(req, 200)
	if len(list()) != 0 {
		t.Fatal("clear failed")
	}
}

func TestMaterialMigrationPreservesLegacyMedia(t *testing.T) {
	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("TEST_DATABASE_URL not set")
	}
	ctx := t.Context()
	pool := newIntegrationSchema(t, ctx, dsn)
	_, err := pool.Exec(ctx, "DROP TRIGGER sync_materials ON knowledge_entries; DROP TRIGGER sync_materials ON knowledge_candidates")
	if err != nil {
		t.Fatal(err)
	}
	pic := demoPNG(t)
	var kb string
	if err := pool.QueryRow(ctx, "INSERT INTO knowledge_bases(name) VALUES('legacy') RETURNING id").Scan(&kb); err != nil {
		t.Fatal(err)
	}
	owners := map[string]string{}
	for _, table := range []string{"knowledge_candidates", "knowledge_entries"} {
		var id string
		if err := pool.QueryRow(ctx, "INSERT INTO "+table+"(question,answer,source,knowledge_base_id,image_data,image_mime) VALUES('q','a','manual',$1,$2,'image/png') RETURNING id", kb, pic).Scan(&id); err != nil {
			t.Fatal(err)
		}
		owners[table] = id
	}
	ids := map[string]string{}
	for range 2 {
		if err := db.Migrate(ctx, pool); err != nil {
			t.Fatal(err)
		}
		for table, id := range owners {
			items, err := loadMaterials(ctx, pool, table, id, false)
			if err != nil || len(items) != 1 || !bytes.Equal(items[0].Data, pic) || items[0].Kind != "image" || items[0].ID == "" {
				t.Fatalf("migration %s: %v", table, err)
			}
			if prev := ids[table]; prev != "" && prev != items[0].ID {
				t.Fatal("unstable migrated ID")
			}
			ids[table] = items[0].ID
		}
	}
}
