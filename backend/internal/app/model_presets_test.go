package app

import "testing"

func TestProviderDefaults(t *testing.T) {
	for _, p := range modelProviderPresets {
		in := modelInput{Provider: p.ID, APIKey: "key", BaseURL: "https://wrong.example"}
		if err := in.applyProvider(); err != nil || in.BaseURL != p.BaseURL || in.Model != p.Models[0].Model || in.Name == "" || in.Kind != "chat" || in.WebSearchMode != p.WebSearchMode {
			t.Fatalf("preset %s: %+v, %v", p.ID, in, err)
		}
		in.Model = "custom-model"
		in.WebSearchMode = "disabled"
		if err := in.applyProvider(); err != nil || in.Model != "custom-model" || in.WebSearchMode != "disabled" {
			t.Fatalf("customization failed: %+v", in)
		}
	}
	for _, mode := range []string{"disabled", "responses", "qwen", "openai", "custom"} {
		in := modelInput{WebSearchMode: mode, BaseURL: "https://third-party.example"}
		if err := in.applyProvider(); err != nil || in.WebSearchMode != mode || in.Provider != "compatible" || in.BaseURL != "https://third-party.example" {
			t.Fatalf("legacy: %+v, %v", in, err)
		}
	}
}
