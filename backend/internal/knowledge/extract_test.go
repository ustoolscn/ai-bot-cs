package knowledge

import (
	"bytes"
	"context"
	"image"
	"image/png"
	"testing"
)

func TestImageDescriptionAndFormat(t *testing.T) {
	var buf bytes.Buffer
	if err := png.Encode(&buf, image.NewGray(image.Rect(0, 0, 8, 8))); err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	got, err := Extract(ctx, "产品.PNG", buf.Bytes(), "蓝色双肩包")
	if got != "蓝色双肩包" || err != nil {
		t.Fatalf("description = %q, err = %v", got, err)
	}
	if _, err := Extract(ctx, "产品.jpg", buf.Bytes(), "蓝色双肩包"); err == nil {
		t.Fatal("mismatched extension accepted")
	}
	for _, name := range []string{"a.md", "a.txt"} {
		original := "  原文\n保留换行\n"
		got, err := Extract(ctx, name, []byte(original), "")
		if got != original || err != nil {
			t.Fatalf("%s changed original text", name)
		}
	}
}
