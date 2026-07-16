package qq

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
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
