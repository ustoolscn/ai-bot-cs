package app

import (
	"testing"
	"time"

	"ai-bot/backend/internal/agent"
)

func TestRankCognitiveMemories(t *testing.T) {
	now := time.Date(2026, 11, 28, 12, 0, 0, 0, time.UTC)
	cands := []memoryCandidate{
		{ID: "stale-low", Kind: "fact", Content: "旧琐事", Importance: 2, AccessCount: 0, Similarity: 0.35, LastTouched: now.Add(-60 * 24 * time.Hour)},
		{ID: "resolved-issue", Kind: "issue", Content: "已修好的问题", Importance: 8, AccessCount: 2, Similarity: 0.90, Resolved: true, LastTouched: now.Add(-2 * 24 * time.Hour)},
		{ID: "joke-match", Kind: "inside_joke", Content: "每次写正则必翻车的老梗", Importance: 7, AccessCount: 5, Similarity: 0.78, LastTouched: now.Add(-3 * 24 * time.Hour)},
		{ID: "pinned-core", Kind: "preference", Content: "极简回复", Importance: 5, Pinned: true, Similarity: 0.1, LastTouched: now.Add(-90 * 24 * time.Hour)},
	}
	got := rankCognitiveMemories(cands, 3, "inside_jokes", true, now)
	if len(got) != 3 {
		t.Fatalf("expected 3, got %d", len(got))
	}
	if got[0].ID != "pinned-core" {
		t.Fatalf("pinned should rank first, got %s", got[0].ID)
	}
	if got[1].ID != "joke-match" {
		t.Fatalf("focus inside_joke should outrank resolved issue & stale trivia, got %s", got[1].ID)
	}
	for _, m := range got {
		if m.ID == "stale-low" {
			t.Fatalf("stale low-importance memory should be ranked last and excluded")
		}
	}
}

func TestMoodInertiaAndChimeInGuard(t *testing.T) {
	now := time.Date(2026, 11, 28, 15, 0, 0, 0, time.UTC)
	prev := updateMoodInertia(userMoodState{}, TurnPerception{UserMood: "愤怒", Valence: -0.8, MoodIntensity: 0.9}, now)
	if prev.NegativeStreak != 1 || prev.CurrentMood != "愤怒" {
		t.Fatalf("unexpected initial mood state: %+v", prev)
	}
	// Neutral short acknowledgement 10 mins later preserves negative inertia.
	after := updateMoodInertia(prev, TurnPerception{UserMood: "平静", Valence: 0.0, MoodIntensity: 0.2}, now.Add(10*time.Minute))
	if after.Valence >= -0.2 || after.CurrentMood != "愤怒" {
		t.Fatalf("expected emotional inertia to preserve negative mood, got %+v", after)
	}

	s := agent.DefaultSettings()
	s.Companion.QuietHoursStart = "01:00"
	s.Companion.QuietHoursEnd = "08:00"
	s.Companion.ChimeInCooldownMinutes = 10

	// 02:30 CST (18:30 UTC previous day) -> quiet hours
	cst230 := time.Date(2026, 11, 28, 2, 30, 0, 0, chinaLocation)
	if blocked, reason := isChimeInBlocked(s, nil, false, cst230); !blocked || reason != "quiet_hours" {
		t.Fatalf("expected quiet_hours block, got %v %s", blocked, reason)
	}

	cst1500 := time.Date(2026, 11, 28, 15, 0, 0, 0, chinaLocation)
	recent := cst1500.Add(-4 * time.Minute)
	if blocked, reason := isChimeInBlocked(s, &recent, false, cst1500); !blocked || reason != "cooldown_active" {
		t.Fatalf("expected cooldown block, got %v %s", blocked, reason)
	}
	if blocked, reason := isChimeInBlocked(s, nil, true, cst1500); !blocked || reason != "last_message_from_bot" {
		t.Fatalf("expected bot-last block, got %v %s", blocked, reason)
	}
	older := cst1500.Add(-15 * time.Minute)
	if blocked, reason := isChimeInBlocked(s, &older, false, cst1500); blocked {
		t.Fatalf("expected unblocked chime-in, got %s", reason)
	}
}
