package agent

import (
	"strings"
	"testing"
	"time"
)

func TestBuildDirectorInstructions(t *testing.T) {
	s := DefaultSettings()
	s.Companion.Backstory = "一个写 Go 和折腾各种 AI 模型的老群友"
	s.Companion.PersonalityTraits = []string{"毒舌但热心", "直爽"}
	s.Companion.Catchphrases = []string{"绝了"}
	s.Companion.EmojiPolicy = "natural"

	t.Run("negative emotion overrides humor and disables emoji", func(t *testing.T) {
		out := BuildDirectorInstructions(DirectorInput{
			Settings:         s,
			ConversationType: "group",
			Now:              time.Date(2026, 11, 28, 14, 0, 0, 0, time.UTC),
			Subject: SubjectContext{
				RelationshipStage: "close_friend",
				Nicknames:         []string{"老张"},
				Persona: map[string]any{
					"expertise": map[string]any{"score": 20.0, "confidence": 0.8},
					"communication": map[string]any{
						"patience": map[string]any{"score": 25.0, "confidence": 0.9},
						"humor":    map[string]any{"score": 85.0, "confidence": 0.9},
					},
					"avoid": []any{"长篇大论"},
				},
			},
			Perception: PerceptionContext{
				UserMood:         "愤怒",
				NeedEmpathyFirst: true,
				Sarcasm:          true,
			},
			AllowSemanticSplit: true,
		})
		for _, want := range []string{
			"情绪优先",
			"禁止使用 emoji",
			"反话",
			"偏新手",
			"简短直接",
			"长篇大论",
			"|||",
		} {
			if !strings.Contains(out, want) {
				t.Fatalf("expected %q in director instructions:\n%s", want, out)
			}
		}
		if strings.Contains(out, "接梗强") {
			t.Fatalf("negative mood should suppress humor prompt:\n%s", out)
		}
	})

	t.Run("chime in banter uses close friend and catchphrase", func(t *testing.T) {
		out := BuildDirectorInstructions(DirectorInput{
			Settings:         s,
			ConversationType: "group",
			Now:              time.Date(2026, 11, 28, 1, 30, 0, 0, time.UTC),
			Subject: SubjectContext{
				RelationshipStage: "close_friend",
				Nicknames:         []string{"阿伟"},
			},
			BotMood: BotMoodContext{Mood: "愉快"},
			Perception: PerceptionContext{
				ReplyMode: "banter",
				IsChimeIn: true,
				UserMood:  "调侃",
			},
		})
		for _, want := range []string{"群聊搭茬模式", "老铁", "阿伟", "深夜", "绝了"} {
			if !strings.Contains(out, want) {
				t.Fatalf("expected %q in director instructions:\n%s", want, out)
			}
		}
	})

	t.Run("disabled companion returns empty", func(t *testing.T) {
		off := s
		off.Companion.Enabled = false
		if got := BuildDirectorInstructions(DirectorInput{Settings: off}); got != "" {
			t.Fatalf("expected empty when disabled, got %q", got)
		}
	})
}

func TestSplitSemanticReplyAndDelays(t *testing.T) {
	parts := SplitSemanticReply("哈哈笑死 ||| 对了你那个问题 ||| 这样改就行", 100, 3)
	if len(parts) != 3 || parts[0] != "哈哈笑死" || parts[2] != "这样改就行" {
		t.Fatalf("unexpected semantic split: %#v", parts)
	}
	capped := SplitSemanticReply("a ||| b ||| c ||| d", 100, 2)
	if len(capped) != 2 || capped[1] != "b\nc\nd" {
		t.Fatalf("unexpected capped split: %#v", capped)
	}

	delays := ReplyDelays(parts, true, 15*time.Second)
	if len(delays) != 3 || delays[0] != 0 || delays[1] < 700*time.Millisecond || delays[2] <= delays[1] {
		t.Fatalf("unexpected dynamic delays: %#v", delays)
	}

	// Deadline clamp keeps cumulative delay within remaining - 10s
	tight := ReplyDelays(parts, true, 11*time.Second)
	if tight[len(tight)-1] > time.Second+10*time.Millisecond {
		t.Fatalf("expected tight deadline scaling, got %#v", tight)
	}
}
