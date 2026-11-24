package app

import "testing"

func TestScheduledGroupText(t *testing.T) {
	draft := "# 周末活动\r\n\r\n## 报名提醒\n- 「周五」截止\n* 每人可报名一次\n\n\n来源：活动页 https://example.com/signup"
	want := "【周末活动】\n\n◆ 报名提醒\n• 「周五」截止\n• 每人可报名一次\n\n来源：活动页 https://example.com/signup"
	if got := scheduledGroupText(draft, "群公告"); got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
	if got := scheduledGroupText("周五截止", "群公告"); got != "【群公告】\n\n周五截止" {
		t.Fatalf("missing fallback title or content: %q", got)
	}
}
