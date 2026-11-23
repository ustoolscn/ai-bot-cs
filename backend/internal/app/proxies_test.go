package app

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"ai-bot/backend/internal/openai"
)

func TestModelProxySelection(t *testing.T) {
	proxy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Host != "model.invalid" || r.URL.Path != "/embeddings" {
			t.Errorf("unexpected proxy request %s", r.URL)
		}
		w.Write([]byte(`{"data":[{"embedding":[1,2,3]}]}`))
	}))
	defer proxy.Close()
	tr, err := modelTransport(proxy.URL)
	if err != nil {
		t.Fatal(err)
	}
	defer tr.CloseIdleConnections()
	client := openai.New("http://model.invalid", "key", "model", time.Second)
	client.HTTP.Transport = tr
	_, err = client.Embed(context.Background(), []string{"test"})
	if err != nil {
		t.Fatal(err)
	}
	tr, err = modelTransport("")
	if err != nil || tr.Proxy != nil {
		t.Fatal("direct mode must bypass environment proxies")
	}
}
