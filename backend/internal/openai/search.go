package openai

import (
	"net/url"
	"slices"
	"strings"

	"ai-bot/backend/internal/domain"
)

// Search metadata is provider-reported. Assistant prose is never evidence of a search.
type searchSource struct {
	URL      string `json:"url"`
	Title    string `json:"title"`
	SiteName string `json:"site_name"`
	Link     string `json:"link"`
	Media    string `json:"media"`
}

type citation struct {
	searchSource
	Type     string       `json:"type"`
	Citation searchSource `json:"url_citation"`
}

type searchAction struct {
	Query   string         `json:"query"`
	Queries []string       `json:"queries"`
	Sources []searchSource `json:"sources"`
}

type serverStep struct {
	Calls []struct {
		Type    string         `json:"type"`
		Status  string         `json:"status"`
		Action  searchAction   `json:"action"`
		Sources []searchSource `json:"sources"`
	} `json:"calls"`
}

func (a searchAction) addQueries(r *domain.ChatResult) {
	for _, q := range append(slices.Clone(a.Queries), a.Query) {
		if q = strings.TrimSpace(q); q != "" && !slices.Contains(r.SearchQueries, q) {
			r.SearchQueries = append(r.SearchQueries, q)
		}
	}
}

func addSource(r *domain.ChatResult, s searchSource) {
	address := s.URL
	if address == "" {
		address = s.Link
	}
	u, err := url.Parse(address)
	if err != nil || (u.Scheme != "https" && u.Scheme != "http") || u.Hostname() == "" {
		return
	}
	for _, existing := range r.Sources {
		if existing.URL == address {
			return
		}
	}
	site := s.SiteName
	if site == "" {
		site = s.Media
	}
	if site == "" {
		site = u.Hostname()
	}
	r.Sources = append(r.Sources, domain.SearchSource{URL: address, Title: s.Title, SiteName: site})
}

func addCitations(r *domain.ChatResult, annotations []citation, metadata map[string]searchSource) {
	for _, a := range annotations {
		if a.Type != "url_citation" {
			continue
		}
		s := a.Citation
		if s.URL == "" {
			s = a.searchSource
		}
		if m, ok := metadata[s.URL]; ok {
			s.SiteName = m.SiteName
			if s.Title == "" || s.Title == s.URL {
				s.Title = m.Title
			}
		}
		addSource(r, s)
	}
	if len(r.Sources) > 0 {
		r.Searched = true
	}
}

func (c *Client) searchTool() map[string]any {
	switch c.WebSearchMode {
	case "deepseek":
		return map[string]any{"type": "deepseek_web_search"}
	case "zhipu":
		return map[string]any{"type": "web_search", "web_search": map[string]any{"enable": true, "search_engine": "search_std", "search_result": true}}
	}
	return nil
}
