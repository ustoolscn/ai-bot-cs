package qq

import (
	"ai-bot/backend/internal/domain"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestMediaReceiptAndTokenReuse(t *testing.T) {
	var tokenCalls, uploaded, verified int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/token":
			tokenCalls++
			fmt.Fprint(w, `{"access_token":"T","expires_in":"7200"}`)
		case "/v2/groups/g/files":
			var p map[string]any
			json.NewDecoder(r.Body).Decode(&p)
			if p["file_name"] != "screen.png" || p["file_data"] != "cG5n" {
				t.Errorf("upload: %v", p)
			}
			uploaded++
			fmt.Fprint(w, `{"file_info":"file","file_uuid":"uuid","ttl":60}`)
		case "/v2/groups/g/messages":
			if r.Header.Get("Authorization") != "QQBot T" {
				t.Error("missing token")
			}
			verified++
			fmt.Fprint(w, `{"id":"message","timestamp":"2026-11-24T03:41:00+08:00","ext_info":{"ref_idx":"ref"}}`)
		default:
			t.Error(r.URL.Path)
		}
	}))
	defer srv.Close()
	client := NewClient("app", "secret", srv.URL, srv.URL+"/token")
	for range 2 {
		receipt, err := client.SendWithReceipt(context.Background(), domain.OutboundMessage{ConversationType: "group", ConversationID: "g", Parts: []domain.ContentPart{{Type: "image", Filename: "tmp/screen.png", Data: []byte("png")}}})
		if err != nil || receipt.Ext.Index != "ref" || receipt.Timestamp == "" {
			t.Fatal(receipt, err)
		}
	}
	if tokenCalls != 1 || uploaded != 2 || verified != 2 {
		t.Fatal(tokenCalls, uploaded, verified)
	}
}
func TestMultipartMedia(t *testing.T) {
	data := bytes.Repeat([]byte{7}, 5<<20)
	var base string
	var put, finish bool
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/token":
			fmt.Fprint(w, `{"access_token":"t"}`)
		case "/v2/users/u/upload_prepare":
			fmt.Fprintf(w, `{"upload_id":"i","parts":[{"index":0,"block_size":"5242880","presigned_url":"%s/put"}]}`, base)
		case "/put":
			b, _ := io.ReadAll(r.Body)
			put = r.Method == "PUT" && r.Header.Get("Authorization") == "" && bytes.Equal(b, data)
		case "/v2/users/u/upload_part_finish":
			finish = put
			fmt.Fprint(w, "{}")
		case "/v2/users/u/files":
			var p map[string]any
			json.NewDecoder(r.Body).Decode(&p)
			if !finish || p["file_name"] != "clip.mp4" || p["upload_id"] != "i" || p["srv_send_msg"] != true {
				t.Errorf("merge %v", p)
			}
			fmt.Fprint(w, `{"id":"sent","file_info":"f","raw_url":"https://example.org/clip","ttl":20}`)
		default:
			t.Error(r.URL.Path)
		}
	}))
	defer srv.Close()
	base = srv.URL
	c := NewClient("app", "secret", base, base+"/token")
	c.HTTP = srv.Client()
	m, err := c.Upload(context.Background(), Prefix("private", "u"), "clip.mp4", "", 2, bytes.NewReader(data), int64(len(data)), true)
	if err != nil || m.ID != "sent" || m.RawURL == "" {
		t.Fatal(m, err)
	}
}
func TestReferenceFallbackAndVoice(t *testing.T) {
	e := DescribeEvent([]byte(`{"d":{"message_type":103,"message_scene":{"ext":["ref_msg_idx=target","auth_token=private"]},"msg_elements":[{"content":"old text","attachments":[{"asr_refer_text":"voice"}]}],"mentions":[{"scope":"all"}]}}`))
	if e.Reference != "target" || !strings.Contains(e.Elements, "old text") || !strings.Contains(e.Elements, "voice") || e.Extra != "@全体成员" || strings.Contains(e.Elements, "auth_token") {
		t.Fatal(e)
	}
}
