package app

import "testing"

func TestContextHistoryLimitIncludesCurrentMessage(t *testing.T) {
	tests := map[int]int{0: 0, 1: 0, 2: 1, 20: 19, 100: 99}
	for total, expectedHistory := range tests {
		if actual := contextHistoryLimit(total); actual != expectedHistory {
			t.Fatalf("total=%d history=%d want=%d", total, actual, expectedHistory)
		}
	}
}
