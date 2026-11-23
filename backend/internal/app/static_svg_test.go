package app

import (
	"bytes"
	"context"
	"encoding/json"
	"image/color"
	"image/png"
	"strings"
	"testing"

	"ai-bot/backend/internal/domain"
	"ai-bot/backend/internal/storage"
)

const portraitHTML = `<div style="background:#fff1d6;width:200px;height:180px"><svg width="120" height="150" viewBox="0 0 120 150"><defs><linearGradient id="shirt"><stop offset="0" stop-color="#24569a"/><stop offset="1" stop-color="#24569a"/></linearGradient></defs><g><rect x="25" y="68" width="60" height="76" fill="url(#shirt)"/><ellipse cx="55" cy="46" rx="22" ry="24" fill="#edaf86"/><path d="M31 42 Q52 -8 81 43 Z" fill="#392c26"/></g></svg></div>`

func TestStaticPortrait(t *testing.T) {
	for _, src := range []string{portraitHTML, strings.ReplaceAll(portraitHTML, `"`, `\"`)} {
		doc, err := staticDocument(src + `<svg onload="bad"><script>bad</script><use href="bad"/><foreignObject>bad</foreignObject><rect fill="url(https://bad)"/></svg>`)
		if err != nil || strings.Contains(doc, "bad") || !strings.Contains(doc, `fill="url(#shirt)"`) || !strings.Contains(doc, `viewBox="0 0 120 150"`) {
			t.Fatalf("unexpected document: %v %s", err, doc)
		}
	}
}
func TestRenderPortraitTool(t *testing.T) {
	if browserPath() == "" {
		t.Skip("HTML_RENDER_BROWSER not available")
	}
	store, err := storage.NewLocal(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	a := App{images: store}
	args, _ := json.Marshal(map[string]string{"html": portraitHTML})
	rt := turnRuntime{}
	_, err = a.executeTool(context.Background(), &rt, domain.ToolCall{Name: "render_html_image", Arguments: string(args)}, nil)
	if err != nil {
		t.Fatal(err)
	}
	data, err := store.Read(context.Background(), rt.screenshot.StorageKey)
	if err != nil {
		t.Fatal(err)
	}
	im, err := png.Decode(bytes.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}
	for _, p := range []struct {
		x, y int
		c    color.RGBA
	}{{190, 130, color.RGBA{255, 241, 214, 255}}, {62, 65, color.RGBA{237, 175, 134, 255}}, {60, 110, color.RGBA{36, 86, 154, 255}}, {60, 42, color.RGBA{57, 44, 38, 255}}} {
		if got := color.RGBAModel.Convert(im.At(p.x, p.y)); got != p.c {
			t.Errorf("pixel (%d,%d) = %v want %v", p.x, p.y, got, p.c)
		}
	}
}
