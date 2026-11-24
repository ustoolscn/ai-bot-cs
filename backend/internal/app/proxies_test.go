package app

import (
	"bufio"
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"

	"ai-bot/backend/internal/config"
	"ai-bot/backend/internal/openai"
)

func TestModelProxySelection(t *testing.T) {
	direct, err := (&App{}).proxyTransport(context.Background(), "")
	if err != nil || direct.(*http.Transport).Proxy != nil {
		t.Fatal("direct mode must bypass environment proxies")
	}
	for _, scheme := range []string{"http", "https"} {
		t.Run(scheme, func(t *testing.T) {
			handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Host != "model.invalid" || r.URL.Path != "/embeddings" {
					t.Error("unexpected proxy target")
				}
				if r.Header.Get("Proxy-Authorization") != "Basic "+base64.StdEncoding.EncodeToString([]byte("user:p@ss:!")) {
					t.Error("missing proxy credentials")
				}
				io.WriteString(w, `{"data":[{"embedding":[1,2,3]}]}`)
			})
			proxy := httptest.NewUnstartedServer(handler)
			if scheme == "https" {
				proxy.StartTLS()
			} else {
				proxy.Start()
			}
			defer proxy.Close()
			u, err := proxyAddress(proxy.URL)
			if err != nil {
				t.Fatal(err)
			}
			u.User = url.UserPassword("user", "p@ss:!")
			tr := modelTransport(u).(authenticatedProxyTransport)
			if scheme == "https" {
				tr.TLSClientConfig = proxy.Client().Transport.(*http.Transport).TLSClientConfig.Clone()
			}
			defer tr.CloseIdleConnections()
			client := openai.New("http://model.invalid", "key", "model", time.Second)
			client.HTTP.Transport = tr
			if _, err = client.Embed(context.Background(), []string{"test"}); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestSOCKS5ProxyCredentials(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	u, _ := proxyAddress("socks5://user:p%40ss@" + ln.Addr().String())
	client := http.Client{Transport: modelTransport(u), Timeout: 3 * time.Second}
	defer client.CloseIdleConnections()
	done := make(chan error, 1)
	go func() {
		resp, err := client.Get("http://model.invalid")
		if err == nil {
			resp.Body.Close()
		}
		done <- err
	}()
	ln.(*net.TCPListener).SetDeadline(time.Now().Add(3 * time.Second))
	conn, err := ln.Accept()
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	conn.SetDeadline(time.Now().Add(3 * time.Second))
	r := bufio.NewReader(conn)
	read := func(n int) []byte {
		b := make([]byte, n)
		if _, err := io.ReadFull(r, b); err != nil {
			t.Fatal(err)
		}
		return b
	}
	hello := read(2)
	if hello[0] != 5 || !bytes.Contains(read(int(hello[1])), []byte{2}) {
		t.Fatal("SOCKS5 password authentication not offered")
	}
	conn.Write([]byte{5, 2})
	auth := read(2)
	if auth[0] != 1 || string(read(int(auth[1]))) != "user" || string(read(int(read(1)[0]))) != "p@ss" {
		t.Fatal("wrong SOCKS credentials")
	}
	conn.Write([]byte{1, 0})
	if !bytes.Equal(read(4), []byte{5, 1, 0, 3}) || string(read(int(read(1)[0]))) != "model.invalid" {
		t.Fatal("wrong SOCKS CONNECT target")
	}
	read(2)
	conn.Write([]byte{5, 0, 0, 1, 127, 0, 0, 1, 0, 80})
	req, err := http.ReadRequest(r)
	if err != nil || req.Header.Get("Proxy-Authorization") != "" {
		t.Fatal("invalid tunneled request")
	}
	io.WriteString(conn, "HTTP/1.1 204 No Content\r\nConnection: close\r\n\r\n")
	if err := <-done; err != nil {
		t.Fatal(err)
	}
}

func TestSavedProxyCredentials(t *testing.T) {
	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("TEST_DATABASE_URL not configured")
	}
	ctx := context.Background()
	pool := newIntegrationSchema(t, ctx, dsn)
	a, err := New(ctx, config.Config{DataDir: t.TempDir(), MasterKey: []byte("0123456789abcdef0123456789abcdef"), AdminUsername: "admin", AdminPassword: "test-password"}, pool, slog.Default())
	if err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(a.Router())
	defer srv.Close()
	jar, _ := cookiejar.New(nil)
	client := &http.Client{Jar: jar}
	api := srv.URL + "/api"
	requestJSON(t, client, "POST", api+"/auth/login", map[string]string{"username": "admin", "password": "test-password"}, 200)
	const password = "p@ss:word!"
	connected := make(chan bool, 1)
	proxy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/anonymous" {
			if r.Header.Get("Proxy-Authorization") != "" {
				t.Error("unexpected credentials after removal")
			}
			w.WriteHeader(http.StatusNoContent)
			return
		}
		if r.Header.Get("Proxy-Authorization") != "Basic "+base64.StdEncoding.EncodeToString([]byte("user:"+password)) {
			t.Error("proxy credential mismatch")
		}
		if r.Method == "CONNECT" {
			connected <- true
			http.Error(w, password, 407)
			return
		}
		switch r.URL.Path {
		case "/models":
			io.WriteString(w, `{"data":[{"id":"model-test"}]}`)
		case "/embeddings":
			if r.Header.Get("Authorization") == "Bearer error-case" {
				http.Error(w, password, http.StatusForbidden)
				return
			}
			io.WriteString(w, `{"data":[{"embedding":[1]}]}`)
		default:
			t.Error("unexpected proxy request")
		}
	}))
	defer proxy.Close()
	u, _ := url.Parse(proxy.URL)
	u.User = url.UserPassword("user", password)
	endpoint := api + "/system/proxies"
	var created struct{ Data struct{ ID string } }
	result := requestJSON(t, client, "POST", endpoint, map[string]string{"name": "test", "address": u.String()}, 200)
	if json.Unmarshal(result, &created) != nil || created.Data.ID == "" {
		t.Fatal("missing proxy id")
	}
	id := created.Data.ID
	item := endpoint + "/" + id
	requestJSON(t, client, "PUT", item, map[string]string{"name": "edited", "address": proxy.URL, "password": ""}, 200)
	var encrypted string
	if err := pool.QueryRow(ctx, "SELECT password_enc FROM model_proxies WHERE id=$1", id).Scan(&encrypted); err != nil || encrypted == "" || encrypted == password {
		t.Fatal("expected encrypted password")
	}
	list := requestJSON(t, client, "GET", endpoint, nil, 200)
	var rows struct{ Data []map[string]any }
	if json.Unmarshal(list, &rows) != nil || len(rows.Data) != 1 {
		t.Fatal("invalid proxy list")
	}
	row := rows.Data[0]
	if len(row) != 5 || row["address"] != proxy.URL || row["username"] != "user" || row["hasPassword"] != true {
		t.Fatal("unexpected proxy list fields")
	}
	var profileID string
	key, _ := a.cipher.Encrypt("model-key")
	err = pool.QueryRow(ctx, `INSERT INTO model_profiles(name,kind,base_url,api_key_enc,model,proxy_id) VALUES('test','embedding','http://model.invalid',$1,'test',$2) RETURNING id::text`, key, id).Scan(&profileID)
	if err != nil {
		t.Fatal(err)
	}
	model, _, err := a.loadModel(ctx, profileID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = model.Embed(ctx, []string{"test"}); err != nil {
		t.Fatal(err)
	}
	model.APIKey = "error-case"
	if _, err = model.Embed(ctx, []string{"test"}); !openai.IsHTTPStatus(err, http.StatusForbidden) || strings.Contains(err.Error(), password) {
		t.Fatal("model error must preserve HTTP status and hide echoed credentials")
	}
	list = requestJSON(t, client, "POST", api+"/model-profiles/discover", map[string]string{"profileId": profileID}, 200)
	if !bytes.Contains(list, []byte("model-test")) {
		t.Fatal("missing discovered model")
	}
	result = requestJSON(t, client, "POST", item+"/test", nil, 200)
	if len(connected) != 1 || bytes.Contains(result, []byte(password)) || !bytes.Contains(result, []byte(`"reachable":false`)) {
		t.Fatal("connectivity test must authenticate and hide proxy diagnostics")
	}
	requestJSON(t, client, "PUT", item, map[string]any{"address": proxy.URL, "name": "test", "username": "", "clearPassword": true}, 200)
	list = requestJSON(t, client, "GET", endpoint, nil, 200)
	if !bytes.Contains(list, []byte(`"hasPassword":false`)) || !bytes.Contains(list, []byte(`"username":""`)) {
		t.Fatal("credentials were not cleared")
	}
	tr, err := a.proxyTransport(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	anonymous := http.Client{Transport: tr, Timeout: time.Second}
	defer anonymous.CloseIdleConnections()
	resp, err := anonymous.Get("http://model.invalid/anonymous")
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	requestJSON(t, client, "PUT", item, map[string]string{"address": proxy.URL, "name": "test", "username": "rotated-user", "password": "rotated-password"}, 200)
	if err := pool.QueryRow(ctx, "SELECT password_enc FROM model_proxies WHERE id=$1", id).Scan(&encrypted); err != nil {
		t.Fatal(err)
	}
	if plain, err := a.cipher.Decrypt(encrypted); err != nil || plain != "rotated-password" {
		t.Fatal("password update failed")
	}
}
