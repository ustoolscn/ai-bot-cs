package knowledge

import (
	"bytes"
	"context"
	"image"
	"image/png"
	"testing"
)

func TestTextModelRejectsImage(t *testing.T) {
	var buf bytes.Buffer
	if err := png.Encode(&buf, image.NewGray(image.Rect(0, 0, 8, 8))); err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	if _, err := Extract(ctx, "产品.PNG", buf.Bytes()); err == nil || err.Error() != ImageModelRequired {
		t.Fatalf("image accepted: %v", err)
	}
	for _, name := range []string{"a.md", "a.txt"} {
		original := "  原文\n保留换行\n"
		got, err := Extract(ctx, name, []byte(original))
		if got != original || err != nil {
			t.Fatalf("%s changed original text", name)
		}
	}
}
