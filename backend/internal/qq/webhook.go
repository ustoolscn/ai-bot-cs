package qq

import (
	"crypto/ed25519"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"ai-bot/backend/internal/domain"
)

type Envelope struct {
	ID string          `json:"id"`
	Op int             `json:"op"`
	S  int64           `json:"s"`
	T  string          `json:"t"`
	D  json.RawMessage `json:"d"`
}
type Validation struct {
	PlainToken string `json:"plain_token"`
	EventTS    string `json:"event_ts"`
}
type MessageData struct {
	ID          string `json:"id"`
	Content     string `json:"content"`
	Timestamp   string `json:"timestamp"`
	GroupOpenID string `json:"group_openid"`
	GroupName   string `json:"group_name"`
	OpenID      string `json:"openid"`
	MsgType     int    `json:"msg_type"`
	Author      struct {
		ID           string `json:"id"`
		MemberOpenID string `json:"member_openid"`
		UserOpenID   string `json:"user_openid"`
		Username     string `json:"username"`
		Nickname     string `json:"nickname"`
		Bot          bool   `json:"bot"`
	} `json:"author"`
	Attachments []struct {
		URL         string `json:"url"`
		ContentType string `json:"content_type"`
		Filename    string `json:"filename"`
	} `json:"attachments"`
}

func privateKey(secret string) ed25519.PrivateKey {
	seed := make([]byte, ed25519.SeedSize)
	b := []byte(secret)
	for i := range seed {
		if len(b) > 0 {
			seed[i] = b[i%len(b)]
		}
	}
	return ed25519.NewKeyFromSeed(seed)
}

func VerifySignature(secret, timestamp, signature string, body []byte) bool {
	if secret == "" || signature == "" {
		return false
	}
	sig, err := hex.DecodeString(signature)
	if err != nil {
		sig, err = base64.StdEncoding.DecodeString(signature)
	}
	if err != nil {
		return false
	}
	pub := privateKey(secret).Public().(ed25519.PublicKey)
	return ed25519.Verify(pub, append([]byte(timestamp), body...), sig)
}

func ValidationResponse(secret string, d Validation) map[string]string {
	sig := ed25519.Sign(privateKey(secret), []byte(d.EventTS+d.PlainToken))
	return map[string]string{"plain_token": d.PlainToken, "signature": hex.EncodeToString(sig)}
}

func Parse(body []byte, botID string) (Envelope, *domain.InboundMessage, error) {
	var e Envelope
	if err := json.Unmarshal(body, &e); err != nil {
		return e, nil, err
	}
	if e.ID == "" && e.Op != 13 {
		return e, nil, errors.New("missing event id")
	}
	if e.Op == 13 {
		return e, nil, nil
	}
	var d MessageData
	if len(e.D) > 0 {
		_ = json.Unmarshal(e.D, &d)
	}
	if !isMessageEvent(e.T) {
		return e, nil, nil
	}
	if d.Author.Bot {
		return e, nil, nil
	}
	ct, cid := "group", d.GroupOpenID
	if e.T == "C2C_MESSAGE_CREATE" {
		ct, cid = "private", d.OpenID
		if cid == "" {
			cid = d.Author.UserOpenID
		}
	}
	if cid == "" {
		return e, nil, errors.New("missing conversation id")
	}
	sender := d.Author.MemberOpenID
	if sender == "" {
		sender = d.Author.UserOpenID
	}
	if sender == "" {
		sender = d.Author.ID
	}
	senderName := strings.TrimSpace(d.Author.Username)
	if senderName == "" {
		senderName = strings.TrimSpace(d.Author.Nickname)
	}
	conversationName := strings.TrimSpace(d.GroupName)
	if ct == "private" {
		conversationName = senderName
	}
	parts := []domain.ContentPart{{Type: "text", Text: strings.TrimSpace(d.Content)}}
	for _, a := range d.Attachments {
		parts = append(parts, domain.ContentPart{Type: "attachment", URL: a.URL, Text: a.Filename})
	}
	eventTime := time.Now().UTC()
	if d.Timestamp != "" {
		if t, err := time.Parse(time.RFC3339, d.Timestamp); err == nil {
			eventTime = t
		}
	}
	deadline := eventTime.Add(5 * time.Minute)
	return e, &domain.InboundMessage{EventID: e.ID, Channel: "qq", BotID: botID, ConversationType: ct, ConversationID: cid,
		ConversationName: conversationName, SenderID: sender, SenderName: senderName, PlatformMessageID: d.ID, EventType: e.T, Text: strings.TrimSpace(d.Content),
		Parts: parts, EventTime: eventTime, ReplyDeadline: &deadline, Raw: body}, nil
}

func isMessageEvent(t string) bool {
	return t == "GROUP_AT_MESSAGE_CREATE" || t == "GROUP_MESSAGE_CREATE" || t == "C2C_MESSAGE_CREATE"
}
func ShouldReply(t string) bool { return t == "GROUP_AT_MESSAGE_CREATE" || t == "C2C_MESSAGE_CREATE" }
func ShouldQueue(t, triggerMode string) bool {
	return ShouldReply(t) || (t == "GROUP_MESSAGE_CREATE" && triggerMode == "always")
}
