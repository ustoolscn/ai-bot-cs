package app

import (
	"bytes"
	"image"
	"image/jpeg"
	"image/png"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	"github.com/gin-gonic/gin"
)

func pictureRequest(t *testing.T, url string, fields map[string]string, images ...[]byte) *http.Request {
	return mediaRequest(t, url, fields, "image", images...)
}
func mediaRequest(t *testing.T, url string, fields map[string]string, field string, images ...[]byte) *http.Request {
	t.Helper()
	var body bytes.Buffer
	form := multipart.NewWriter(&body)
	for k, v := range fields {
		if err := form.WriteField(k, v); err != nil {
			t.Fatal(err)
		}
	}
	for _, data := range images {
		name := "demo.png"
		if http.DetectContentType(data) == "video/mp4" {
			name = "recording.mp4"
		}
		f, err := form.CreateFormFile(field, name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err = f.Write(data); err != nil {
			t.Fatal(err)
		}
	}
	if err := form.Close(); err != nil {
		t.Fatal(err)
	}
	req, err := http.NewRequest("POST", url, &body)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", form.FormDataContentType())
	return req
}
func demoPNG(t *testing.T) []byte {
	t.Helper()
	var b bytes.Buffer
	if err := png.Encode(&b, image.NewRGBA(image.Rect(0, 0, 2, 2))); err != nil {
		t.Fatal(err)
	}
	return b.Bytes()
}
func TestRejectInvalidReplyImages(t *testing.T) {
	pic := demoPNG(t)
	for _, files := range [][][]byte{{pic, pic}, {pic[:len(pic)/2]}, {[]byte("not an image")}, {make([]byte, maxReplyImage+1)}} {
		c, _ := gin.CreateTestContext(httptest.NewRecorder())
		c.Request = pictureRequest(t, "/reply", nil, files...)
		if _, err := bindReplyImage(c, &struct{}{}); err == nil {
			t.Fatal("invalid upload accepted")
		}
	}
}

func TestKnowledgeImageEditing(t *testing.T) {
	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("TEST_DATABASE_URL not set")
	}
	pool := newIntegrationSchema(t, t.Context(), dsn)
	a := App{db: pool}
	var kb, gap string
	if err := pool.QueryRow(t.Context(), "INSERT INTO knowledge_bases(name) VALUES('fixture') RETURNING id").Scan(&kb); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(t.Context(), "INSERT INTO knowledge_gaps(question) VALUES('where?') RETURNING id").Scan(&gap); err != nil {
		t.Fatal(err)
	}
	r := gin.New()
	r.POST("/entries", a.createEntryAPI)
	r.PUT("/entries/:id", a.updateEntryAPI)
	r.POST("/gap/:id", a.answerGap)
	r.POST("/approve/:id", a.approveCandidateAPI)
	fields := map[string]string{"knowledgeBaseId": kb, "answer": "see demo", "question": "where?"}
	call := func(method, url string, status int, files ...[]byte) map[string]any {
		t.Helper()
		req := pictureRequest(t, url, fields, files...)
		req.Method = method
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		if w.Code != status {
			t.Fatalf("%s %s: %d %s", method, url, w.Code, w.Body.String())
		}
		return dataMap(t, w.Body.Bytes())
	}
	check := func(id string, want []byte) {
		t.Helper()
		var got []byte
		if err := pool.QueryRow(t.Context(), "SELECT image_data FROM knowledge_entries WHERE id=$1", id).Scan(&got); err != nil || !bytes.Equal(got, want) {
			t.Fatalf("image differs: %v", err)
		}
	}
	pic := demoPNG(t)
	var jpegPic bytes.Buffer
	if err := jpeg.Encode(&jpegPic, image.NewRGBA(image.Rect(0, 0, 1, 1)), nil); err != nil {
		t.Fatal(err)
	}
	entry := call("POST", "/entries", 201, pic)["id"].(string)
	check(entry, pic)
	call("PUT", "/entries/"+entry, 200)
	check(entry, pic)
	call("PUT", "/entries/"+entry, 200, jpegPic.Bytes())
	check(entry, jpegPic.Bytes())
	fields["removeImage"] = "true"
	call("PUT", "/entries/"+entry, 200)
	check(entry, nil)
	delete(fields, "removeImage")
	check(call("POST", "/gap/"+gap, 200, pic)["entryId"].(string), pic)
	for _, action := range []string{"keep", "replace", "remove"} {
		candidate, err := insertCandidate(t.Context(), pool, kb, "q", nil, "a", "manual", nil, 1, replyImage{mime: "image/png", data: pic})
		if err != nil {
			t.Fatal(err)
		}
		var files [][]byte
		want := pic
		if action == "replace" {
			files = [][]byte{jpegPic.Bytes()}
			want = jpegPic.Bytes()
		}
		if action == "remove" {
			fields["removeImage"] = "true"
			want = nil
		}
		check(call("POST", "/approve/"+candidate, 200, files...)["entryId"].(string), want)
	}
}

func TestVerbatimFAQImage(t *testing.T) {
	hits := []map[string]any{{"entry_id": "entry", "id": "chunk", "content": "问：在哪里？\n答：见图中红框。", "hasImage": true}}
	for answer, want := range map[string]string{"见图中红框。": "chunk", "我来帮你。": ""} {
		_, id := selectedKnowledgeImage(answer, hits)
		if id != want {
			t.Fatalf("%s: %s", answer, id)
		}
	}
}
