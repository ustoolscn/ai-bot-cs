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
	var messageBody map[string]any
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/token":
			_ = json.NewEncoder(w).Encode(map[string]string{"access_token": "token"})
		case "/v2/groups/group/messages":
			if r.Header.Get("Authorization") != "QQBot token" {
				t.Errorf("bad auth")
			}
			_ = json.NewDecoder(r.Body).Decode(&messageBody)
			_ = json.NewEncoder(w).Encode(map[string]string{"id": "sent-id"})
		default:
			http.NotFound(w, r)
		}
	}))
	defer s.Close()
	c := NewClient("app", "secret", s.URL, s.URL+"/token")
	id, err := c.Send(context.Background(), domain.OutboundMessage{ConversationType: "group", ConversationID: "group", ReplyToMessageID: "m1", Text: "## 标题\n\n**加粗内容**", Format: "markdown", Sequence: 2})
	if err != nil || id != "sent-id" {
		t.Fatalf("id=%q err=%v", id, err)
	}
	if messageBody["msg_type"] != float64(2) || messageBody["msg_id"] != "m1" || messageBody["msg_seq"] != float64(2) {
		t.Fatalf("unexpected markdown message body: %#v", messageBody)
	}
	markdown := messageBody["markdown"].(map[string]any)
	if markdown["content"] != "## 标题\n\n**加粗内容**" {
		t.Fatalf("unexpected markdown content: %#v", markdown)
	}
}

func TestClientSendProcessingArk(t *testing.T) {
	var bodies []map[string]any
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/token":
			_ = json.NewEncoder(w).Encode(map[string]string{"access_token": "token"})
		case "/v2/users/user/messages":
			var body map[string]any
			_ = json.NewDecoder(r.Body).Decode(&body)
			bodies = append(bodies, body)
			_ = json.NewEncoder(w).Encode(map[string]string{"id": "ark-id"})
		default:
			http.NotFound(w, r)
		}
	}))
	defer s.Close()

	c := NewClient("app", "secret", s.URL, s.URL+"/token")
	id, err := c.Send(context.Background(), domain.OutboundMessage{
		ConversationType: "private", ConversationID: "user", ReplyToMessageID: "source", Text: "👀", Sequence: 1,
		Parts: []domain.ContentPart{{Type: "ark_ack"}},
	})
	if err != nil || id != "ark-id" || len(bodies) != 1 {
		t.Fatalf("id=%q bodies=%#v err=%v", id, bodies, err)
	}
	if bodies[0]["msg_type"] != float64(3) || bodies[0]["msg_seq"] != float64(1) {
		t.Fatalf("unexpected ark body: %#v", bodies[0])
	}
	ark := bodies[0]["ark"].(map[string]any)
	if ark["template_id"] != float64(23) {
		t.Fatalf("unexpected ark payload: %#v", ark)
	}
}

func TestClientFallsBackWhenPassiveArkIsRejected(t *testing.T) {
	var bodies []map[string]any
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/token":
			_ = json.NewEncoder(w).Encode(map[string]string{"access_token": "token"})
		case "/v2/groups/group/messages":
			var body map[string]any
			_ = json.NewDecoder(r.Body).Decode(&body)
			bodies = append(bodies, body)
			if len(bodies) == 1 {
				http.Error(w, "ark permission denied", http.StatusForbidden)
				return
			}
			_ = json.NewEncoder(w).Encode(map[string]string{"id": "fallback-id"})
		default:
			http.NotFound(w, r)
		}
	}))
	defer s.Close()

	c := NewClient("app", "secret", s.URL, s.URL+"/token")
	id, err := c.Send(context.Background(), domain.OutboundMessage{
		ConversationType: "group", ConversationID: "group", ReplyToMessageID: "source", Text: "👀", Sequence: 1,
		Parts: []domain.ContentPart{{Type: "ark_ack"}},
	})
	if err != nil || id != "fallback-id" || len(bodies) != 2 {
		t.Fatalf("id=%q bodies=%#v err=%v", id, bodies, err)
	}
	if bodies[0]["msg_type"] != float64(3) || bodies[1]["msg_type"] != float64(0) || bodies[1]["content"] != "👀" {
		t.Fatalf("unexpected fallback bodies: %#v", bodies)
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
