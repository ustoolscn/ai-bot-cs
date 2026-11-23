package app

import (
	"bytes"
	"context"
	"encoding/json"
	"image/png"
	"os"
	"path/filepath"
	"testing"

	"ai-bot/backend/internal/agent"
	"ai-bot/backend/internal/newapi"
	"ai-bot/backend/internal/statuscard"
)

func TestSampleStatusCards(t *testing.T) {
	if browserPath() == "" {
		t.Skip("Chromium unavailable")
	}
	raw, err := os.ReadFile("../statuscard/templates/sample.json")
	if err != nil {
		t.Fatal(err)
	}
	var c statuscard.Card
	if err = json.Unmarshal(raw, &c); err != nil {
		t.Fatal(err)
	}
	for _, style := range []string{"brief", "table", "heatmap", "night"} {
		t.Run(style, func(t *testing.T) {
			img, err := renderStatusCard(context.Background(), style, c)
			if err != nil {
				t.Fatal(err)
			}
			cfg, err := png.DecodeConfig(bytes.NewReader(img))
			if err != nil || cfg.Width < 1120 || cfg.Height < 300 {
				t.Fatal("unexpected rendered size", cfg, err)
			}
			if dir := os.Getenv("STATUS_CARD_PREVIEW_DIR"); dir != "" {
				if err = os.WriteFile(filepath.Join(dir, style+".png"), img, 0644); err != nil {
					t.Fatal(err)
				}
			}
			t.Logf("%dx%d", cfg.Width, cfg.Height)
		})
	}
}

func TestStatusImageSelection(t *testing.T) {
	s := agent.DefaultSettings()
	if s.LiveData.StatusImageStyle != "brief" {
		t.Fatal("unexpected default")
	}
	rt := turnRuntime{settings: s, snapshotOK: true, snapshot: newapi.Snapshot{Monitors: []newapi.Monitor{{Name: "API", Status: 1}}}}
	for _, style := range []string{"brief", "table", "heatmap", "night", "off", "page"} {
		rt.settings.LiveData.StatusImageStyle = style
		if err := rt.settings.Validate(); err != nil {
			t.Fatal(err)
		}
		if statusPageTool(&rt, "get_group_status") != (style != "page" && style != "off") {
			t.Fatalf("incorrect image selection: %s", style)
		}
	}
	rt.settings.LiveData.StatusPageURL = "https://status.example/"
	rt.snapshotOK = false
	if !statusPageTool(&rt, "get_service_status") {
		t.Fatal("legacy page mode requires a snapshot")
	}
	rt.settings.LiveData.StatusImageStyle = "off"
	if statusPageTool(&rt, "get_service_status") {
		t.Fatal("off mode uses external page")
	}
	rt.settings.LiveData.StatusImageStyle = "other"
	if rt.settings.Validate() == nil {
		t.Fatal("unknown style accepted")
	}
}
