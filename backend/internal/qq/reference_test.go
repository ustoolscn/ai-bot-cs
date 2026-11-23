package qq

import "testing"

func TestReferenceIndex(t *testing.T) {
	for _, tc := range []struct{ raw, want string }{
		{`{"d":{"id":"passive-token","message_scene":{"ext":["ref_msg_idx=other","msg_idx=REFIDX_original=="]}}}`, "REFIDX_original=="},
		{`{"message_scene":{"ext":["msg_idx=direct"]}}`, "direct"},
		{`{"id":"passive-token"}`, ""}, {`invalid`, ""},
	} {
		if got := ReferenceIndex([]byte(tc.raw)); got != tc.want {
			t.Errorf("got %q want %q", got, tc.want)
		}
	}
}
