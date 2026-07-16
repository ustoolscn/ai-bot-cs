package qq

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"ai-bot/backend/internal/domain"
)

func TestClientSendGroupMessage(t *testing.T) {
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/token":
			_ = json.NewEncoder(w).Encode(map[string]string{"access_token": "token"})
		case "/v2/groups/group/messages":
			if r.Header.Get("Authorization") != "QQBot token" {
				t.Errorf("bad auth")
			}
			_ = json.NewEncoder(w).Encode(map[string]string{"id": "sent-id"})
		default:
			http.NotFound(w, r)
		}
	}))
	defer s.Close()
	c := NewClient("app", "secret", s.URL, s.URL+"/token")
	id, err := c.Send(context.Background(), domain.OutboundMessage{ConversationType: "group", ConversationID: "group", ReplyToMessageID: "m1", Text: "hello", Sequence: 1})
	if err != nil || id != "sent-id" {
		t.Fatalf("id=%q err=%v", id, err)
	}
}

func TestClientSendGroupImage(t *testing.T) {
	var uploadBody map[string]any
	var messageBody map[string]any
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/token":
			_ = json.NewEncoder(w).Encode(map[string]string{"access_token": "token"})
		case "/v2/groups/group/files":
			if r.Header.Get("Authorization") != "QQBot token" {
				t.Errorf("bad upload auth")
			}
			_ = json.NewDecoder(r.Body).Decode(&uploadBody)
			_ = json.NewEncoder(w).Encode(map[string]string{"file_info": "uploaded-file-info"})
		case "/v2/groups/group/messages":
			_ = json.NewDecoder(r.Body).Decode(&messageBody)
			_ = json.NewEncoder(w).Encode(map[string]string{"id": "image-message-id"})
		default:
			http.NotFound(w, r)
		}
	}))
	defer s.Close()

	c := NewClient("app", "secret", s.URL, s.URL+"/token")
	id, err := c.Send(context.Background(), domain.OutboundMessage{
		ConversationType: "group", ConversationID: "group", ReplyToMessageID: "source-message", Sequence: 3,
		Text: "图片说明", Parts: []domain.ContentPart{{Type: "image", URL: "https://example.com/result.png"}},
	})
	if err != nil || id != "image-message-id" {
		t.Fatalf("id=%q err=%v", id, err)
	}
	if uploadBody["file_type"] != float64(1) || uploadBody["url"] != "https://example.com/result.png" {
		t.Fatalf("unexpected upload body: %#v", uploadBody)
	}
	if messageBody["msg_type"] != float64(7) || messageBody["msg_id"] != "source-message" || messageBody["msg_seq"] != float64(3) {
		t.Fatalf("unexpected message body: %#v", messageBody)
	}
	media := messageBody["media"].(map[string]any)
	if media["file_info"] != "uploaded-file-info" {
		t.Fatalf("unexpected media body: %#v", media)
	}
}

func TestClientSendLocalImageData(t *testing.T) {
	var uploadBody map[string]any
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/token":
			_ = json.NewEncoder(w).Encode(map[string]string{"access_token": "token"})
		case strings.HasSuffix(r.URL.Path, "/files"):
			_ = json.NewDecoder(r.Body).Decode(&uploadBody)
			_ = json.NewEncoder(w).Encode(map[string]string{"file_info": "file-info"})
		case strings.HasSuffix(r.URL.Path, "/messages"):
			_ = json.NewEncoder(w).Encode(map[string]string{"id": "sent"})
		default:
			http.NotFound(w, r)
		}
	}))
	defer s.Close()

	c := NewClient("app", "secret", s.URL, s.URL+"/token")
	_, err := c.Send(context.Background(), domain.OutboundMessage{
		ConversationType: "private", ConversationID: "user",
		Parts: []domain.ContentPart{{Type: "image", Data: []byte("png-data")}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if uploadBody["file_data"] != "cG5nLWRhdGE=" {
		t.Fatalf("unexpected file_data: %#v", uploadBody)
	}
}
