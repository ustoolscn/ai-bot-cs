package openai

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"ai-bot/backend/internal/domain"
)

func TestProviderSearch(t *testing.T) {
	for _, tc := range []struct {
		mode, tool, body, query, site string
	}{
		{"deepseek", "deepseek_web_search", `{"choices":[{"message":{"content":"news","server_tool_calls":[{"calls":[{"type":"deepseek_web_search","status":"completed","action":{"queries":["official release"]},"sources":[{"url":"https://example.org/news","site_name":"Example News"}]}]}],"annotations":[{"type":"url_citation","url_citation":{"title":"Latest","url":"https://example.org/news"}}]}}]}`, "official release", "Example News"},
		{"zhipu", "web_search", `{"choices":[{"message":{"content":"news"}}],"web_search":[{"link":"https://example.org/news","title":"Latest","media":"Example News"}]}`, "", "Example News"},
	} {
		t.Run(tc.mode, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				var p struct {
					Tools []struct {
						Type      string         `json:"type"`
						WebSearch map[string]any `json:"web_search"`
					} `json:"tools"`
					ToolChoice       any `json:"tool_choice"`
					WebSearchOptions any `json:"web_search_options"`
				}
				if err := json.NewDecoder(r.Body).Decode(&p); err != nil {
					t.Error(err)
					return
				}
				if r.URL.Path != "/chat/completions" || p.ToolChoice != nil || p.WebSearchOptions != nil || len(p.Tools) != 2 || p.Tools[0].Type != tc.tool || p.Tools[1].Type != "function" {
					t.Errorf("unexpected request: %+v", p)
				}
				if tc.mode == "zhipu" && (p.Tools[0].WebSearch["enable"] != true || p.Tools[0].WebSearch["search_result"] != true) {
					t.Errorf("missing GLM search parameters")
				}
				io.WriteString(w, tc.body)
			}))
			defer server.Close()
			c := New(server.URL, "", "test", time.Second)
			c.WebSearchMode = tc.mode
			c.ExtraBody = map[string]any{"tool_choice": "required", "web_search_options": map[string]any{}}
			got, err := c.ChatWith(context.Background(), nil, domain.ChatOptions{Tools: []domain.ToolSpec{{Name: "lookup"}}, RequireWebSearch: true})
			if err != nil || !got.Searched || len(got.Sources) != 1 || got.Sources[0].SiteName != tc.site || got.Sources[0].Title != "Latest" {
				t.Fatalf("result=%+v err=%v", got, err)
			}
			if tc.query != "" && (len(got.SearchQueries) != 1 || got.SearchQueries[0] != tc.query) || tc.query == "" && len(got.SearchQueries) != 0 {
				t.Errorf("queries=%v", got.SearchQueries)
			}
		})
	}
}

func TestProviderSearchWithoutEvidence(t *testing.T) {
	for _, disable := range []bool{false, true} {
		s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			var p map[string]any
			json.NewDecoder(r.Body).Decode(&p)
			if (p["tools"] == nil) != disable {
				t.Errorf("disable=%v tools=%v", disable, p["tools"])
			}
			io.WriteString(w, `{"choices":[{"message":{"content":"I searched https://example.org"}}]}`)
		}))
		c := New(s.URL, "", "test", time.Second)
		c.WebSearchMode = "deepseek"
		result, err := c.ChatWith(context.Background(), nil, domain.ChatOptions{DisableWebSearch: disable})
		s.Close()
		if err != nil || result.Searched || result.SearchQueries == nil || result.Sources == nil {
			t.Fatalf("result=%+v err=%v", result, err)
		}
	}
}

func TestResponsesSearchDiagnostics(t *testing.T) {
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		io.WriteString(w, `{"output":[{"type":"web_search_call","status":"completed","action":{"query":"release"}},{"type":"message","content":[{"type":"output_text","text":"News","annotations":[{"type":"url_citation","url":"https://example.org/news","title":"News"}]}]}]}`)
	}))
	defer s.Close()
	c := New(s.URL, "", "test", time.Second)
	c.UseResponses = true
	got, err := c.Chat(context.Background(), nil)
	if err != nil || !got.Searched || len(got.SearchQueries) != 1 || got.SearchQueries[0] != "release" || len(got.Sources) != 1 || got.Sources[0].Title != "News" {
		t.Fatalf("result=%+v err=%v", got, err)
	}
}
