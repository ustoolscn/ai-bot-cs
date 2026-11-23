package qq

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestRemoveGroupMember(t *testing.T) {
	for _, tc := range []struct {
		name, body string
		status     int
		wantErr    bool
	}{
		{"success", `{"remove_members_result":"success"}`, 200, false},
		{"owner disabled", `{"code":40103012,"message":"disabled"}`, 403, true},
		{"application disabled", `{"code":40103011}`, 200, true},
		{"no explicit success", `{}`, 200, true},
		{"invalid response", `invalid`, 200, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == "/token" {
					w.Write([]byte(`{"access_token":"test-token"}`))
					return
				}
				if r.Method != "POST" || r.URL.Path != "/v2/groups/group/batch_remove_members" || r.Header.Get("Authorization") != "QQBot test-token" {
					t.Errorf("unexpected request %s %s", r.Method, r.URL.Path)
				}
				var b struct {
					Members []string `json:"member_openids"`
				}
				if err := json.NewDecoder(r.Body).Decode(&b); err != nil || len(b.Members) != 1 || b.Members[0] != "member" {
					t.Errorf("invalid body %#v err=%v", b, err)
				}
				w.WriteHeader(tc.status)
				w.Write([]byte(tc.body))
			}))
			defer server.Close()
			err := NewClient("app", "secret", server.URL, server.URL+"/token").RemoveGroupMember(context.Background(), "group", "member")
			if (err != nil) != tc.wantErr {
				t.Fatalf("err=%v", err)
			}
		})
	}
}
