package agent

import (
	"strings"
	"testing"
)

func TestEstimateTokens(t *testing.T) {
	if got := EstimateTokens("你好世界"); got != 4 {
		t.Fatalf("cjk tokens = %d", got)
	}
	if got := EstimateTokens("abcdefgh"); got != 2 {
		t.Fatalf("ascii tokens = %d", got)
	}
}

func TestFitHistoryKeepsNewest(t *testing.T) {
	turns := []Turn{{Role: "user", Content: strings.Repeat("长", 100)}, {Role: "assistant", Content: "短"}, {Role: "user", Content: "最新"}}
	kept, dropped := FitHistory(turns, 20, 10)
	if dropped != 1 || len(kept) != 2 || kept[1].Content != "最新" {
		t.Fatalf("kept=%v dropped=%d", kept, dropped)
	}
	kept, dropped = FitHistory(turns, 1000, 1)
	if len(kept) != 1 || dropped != 2 {
		t.Fatalf("maxTurns not applied: %v", kept)
	}
}

func TestSanitizeReply(t *testing.T) {
	in := "## 结论\n这是 **重点** 内容。\n\n```bash\n# keep comment\ncurl **x**\n```\n希望以上信息对您有帮助！"
	out := SanitizeReply(in)
	if strings.Contains(out, "##") || strings.Contains(out, "**重点**") || strings.Contains(out, "希望") {
		t.Fatalf("not sanitized: %q", out)
	}
	if !strings.Contains(out, "# keep comment") || !strings.Contains(out, "curl **x**") {
		t.Fatalf("code block modified: %q", out)
	}
	if !strings.Contains(out, "![a](https://x/y.png)") && SanitizeReply("看图 ![a](https://x/y.png)") != "看图 ![a](https://x/y.png)" {
		t.Fatal("image syntax modified")
	}
}

func TestSplitReply(t *testing.T) {
	short := "好的，已经帮你查了。"
	if parts := SplitReply(short, 50, 3); len(parts) != 1 {
		t.Fatalf("short split: %v", parts)
	}
	long := strings.Repeat("第一段内容很长。", 10) + "\n\n" + strings.Repeat("第二段。", 10) + "\n\n" + strings.Repeat("第三段！", 10) + "\n\n最后一句。"
	parts := SplitReply(long, 60, 3)
	if len(parts) < 2 || len(parts) > 3 {
		t.Fatalf("parts=%d %v", len(parts), parts)
	}
	if strings.Join(parts, "") == "" || !strings.Contains(parts[len(parts)-1], "最后一句") {
		t.Fatalf("content lost: %v", parts)
	}
}

func TestExtractJSON(t *testing.T) {
	if got := ExtractJSON("```json\n{\"a\":1}\n```"); got != `{"a":1}` {
		t.Fatalf("got %q", got)
	}
}

func TestBuildSystemPrompt(t *testing.T) {
	s := DefaultSettings()
	s.Examples = []Example{{Question: "怎么充值", Answer: "后台钱包页就能充"}}
	msgs := BuildSystemPrompt(PromptInput{Settings: s, ConversationType: "group", Memories: []Memory{{Content: "用的是 vip 分组"}}, Knowledge: "[1] x", ToolsAvailable: true})
	joined := strings.Join(msgs, "\n")
	for _, want := range []string{"120 字", "后台钱包页就能充", "vip 分组", "知识库检索结果", "调用工具"} {
		if !strings.Contains(joined, want) {
			t.Fatalf("missing %q in prompt", want)
		}
	}
}
