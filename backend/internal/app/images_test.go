package app

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

var testPNG = []byte{
	0x89, 0x50, 0x4e, 0x47, 0x0d, 0x0a, 0x1a, 0x0a,
	0x00, 0x00, 0x00, 0x0d, 0x49, 0x48, 0x44, 0x52,
}

func TestDownloadImage(t *testing.T) {
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "image/png")
		_, _ = w.Write(testPNG)
	}))
	defer server.Close()

	data, contentType, ext, err := downloadImage(context.Background(), server.Client(), server.URL+"/image.png")
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != string(testPNG) || contentType != "image/png" || ext != ".png" {
		t.Fatalf("data=%x contentType=%q ext=%q", data, contentType, ext)
	}
}

func TestDownloadImageRejectsUnsupportedFormat(t *testing.T) {
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("not an image"))
	}))
	defer server.Close()

	_, _, _, err := downloadImage(context.Background(), server.Client(), server.URL+"/file.txt")
	if err == nil || !strings.Contains(err.Error(), "不支持的图片格式") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestDownloadImageRejectsOversizedContentLength(t *testing.T) {
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Length", "10485761")
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	_, _, _, err := downloadImage(context.Background(), server.Client(), server.URL+"/large.png")
	if err == nil || !strings.Contains(err.Error(), "超过 10 MB") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestExtractOutboundImage(t *testing.T) {
	text, parts := extractOutboundImage("给你一张图：![结果](https://example.com/result.png)\n请查收。")
	if text != "给你一张图：\n请查收。" || len(parts) != 1 || parts[0].URL != "https://example.com/result.png" {
		t.Fatalf("text=%q parts=%#v", text, parts)
	}

	text, parts = extractOutboundImage("[IMAGE]http://127.0.0.1/private.png[/IMAGE]")
	if len(parts) != 0 || !strings.Contains(text, "127.0.0.1") {
		t.Fatalf("unsafe image URL should remain text: text=%q parts=%#v", text, parts)
	}
}
