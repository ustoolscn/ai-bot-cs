package knowledge

import (
	"bytes"
	"context"
	"encoding/base64"
	"image"
	"image/png"
	"strings"
	"testing"
)

func TestMarkdownScreenshotScope(t *testing.T) {
	var buf bytes.Buffer
	if err := png.Encode(&buf, image.NewRGBA(image.Rect(0, 0, 10, 10))); err != nil {
		t.Fatal(err)
	}
	text := "点击账户设置\n![账户页](images/account.png)\n点击退款\n![](data:image/png;base64," + base64.StdEncoding.EncodeToString(buf.Bytes()) + ")"
	calls := 0
	sections, err := Multimodal(context.Background(), "guide.md", []byte(text), func(name string) ([]byte, error) {
		calls++
		if name != "account.png" {
			t.Fatalf("unexpected reference %q", name)
		}
		return buf.Bytes(), nil
	})
	if err != nil || calls != 1 || len(sections) != 2 {
		t.Fatalf("sections=%d calls=%d error=%v", len(sections), calls, err)
	}
	for i, caption := range []string{"点击账户设置\n账户页", "点击退款"} {
		if sections[i].Text != caption || !bytes.Equal(sections[i].Image, buf.Bytes()) || strings.Contains(sections[i].Text, "base64") {
			t.Fatalf("wrong section %d", i)
		}
	}
}
