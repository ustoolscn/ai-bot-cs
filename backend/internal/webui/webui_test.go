package webui

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestHandlerServesSPA(t *testing.T) {
	h := Handler()
	for _, route := range []string{"/", "/knowledge-bases/example"} {
		r := httptest.NewRequest(http.MethodGet, route, nil)
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), "AI Bot") {
			t.Fatalf("route %s returned %d: %s", route, w.Code, w.Body.String())
		}
	}
}

func TestHandlerRejectsUnknownPost(t *testing.T) {
	w := httptest.NewRecorder()
	Handler().ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/unknown", nil))
	if w.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", w.Code)
	}
}

func TestHandlerDoesNotMaskMissingAPIOrAsset(t *testing.T) {
	for _, route := range []string{"/api/missing", "/callbacks/missing", "/assets/missing.js"} {
		w := httptest.NewRecorder()
		Handler().ServeHTTP(w, httptest.NewRequest(http.MethodGet, route, nil))
		if w.Code != http.StatusNotFound {
			t.Fatalf("route %s expected 404, got %d", route, w.Code)
		}
	}
}
