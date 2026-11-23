package app

import (
	"testing"
	"time"
)

func TestContextHistoryLimitIncludesCurrentMessage(t *testing.T) {
	tests := map[int]int{0: 0, 1: 0, 2: 1, 20: 19, 100: 99}
	for total, expectedHistory := range tests {
		if actual := contextHistoryLimit(total); actual != expectedHistory {
			t.Fatalf("total=%d history=%d want=%d", total, actual, expectedHistory)
		}
	}
}

func TestHumanReplyActiveSelection(t *testing.T) {
	now := time.Now()
	past, future := now.Add(-time.Minute), now.Add(time.Minute)
	for _, tc := range []struct {
		name, event string
		seq         int
		deadline    *time.Time
		want        bool
	}{
		{"within window", "HUMAN_REPLY", 5, &future, false},
		{"sequence exhausted", "HUMAN_REPLY", 6, &future, true},
		{"expired while queued", "HUMAN_REPLY", 2, &past, true},
		{"at deadline", "HUMAN_REPLY", 2, &now, true},
		{"automatic remains passive", "AI_RESPONSE", 6, &past, false},
		{"ack remains passive", "PROCESSING_ACK", 6, &past, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := humanReplyNeedsActive(outboxJob{EventType: tc.event, ReplyTo: "source", Sequence: tc.seq, Deadline: tc.deadline}, now); got != tc.want {
				t.Fatalf("active=%v want=%v", got, tc.want)
			}
		})
	}
}
