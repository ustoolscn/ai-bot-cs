package qq

import (
	"crypto/ed25519"
	"encoding/hex"
	"testing"
)

func TestSignatureAndParse(t *testing.T) {
	body := []byte(`{"id":"evt1","op":0,"t":"GROUP_AT_MESSAGE_CREATE","d":{"id":"m1","group_openid":"g1","group_name":"测试群","content":" hi ","author":{"member_openid":"u1","nickname":"小明"}}}`)
	ts := "123"
	sig := ed25519.Sign(privateKey("secret"), append([]byte(ts), body...))
	if !VerifySignature("secret", ts, hex.EncodeToString(sig), body) {
		t.Fatal("signature rejected")
	}
	_, m, err := Parse(body, "b1")
	if err != nil {
		t.Fatal(err)
	}
	if m.ConversationID != "g1" || m.ConversationName != "测试群" || m.SenderName != "小明" || m.Text != "hi" || !ShouldReply(m.EventType) {
		t.Fatalf("unexpected: %#v", m)
	}
}

func TestValidationResponse(t *testing.T) {
	r := ValidationResponse("secret", Validation{PlainToken: "plain", EventTS: "123"})
	if r["plain_token"] != "plain" || r["signature"] == "" {
		t.Fatal(r)
	}
}

func TestShouldQueue(t *testing.T) {
	if !ShouldQueue("GROUP_AT_MESSAGE_CREATE", "mention_only") {
		t.Fatal("mention should queue")
	}
	if ShouldQueue("GROUP_MESSAGE_CREATE", "mention_only") {
		t.Fatal("ordinary message should not queue")
	}
	if !ShouldQueue("GROUP_MESSAGE_CREATE", "always") {
		t.Fatal("always mode should queue")
	}
}
