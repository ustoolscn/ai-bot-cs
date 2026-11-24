package qq

import (
	"crypto/ed25519"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/url"
	"path"
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
		ID             string `json:"id"`
		MemberOpenID   string `json:"member_openid"`
		UserOpenID     string `json:"user_openid"`
		Username       string `json:"username"`
		MemberNickname string `json:"member_nickname"`
		Nickname       string `json:"nickname"`
		Bot            bool   `json:"bot"`
	} `json:"author"`
	Mentions []struct {
		IsYou bool `json:"is_you"`
	} `json:"mentions"`
	MessageElement
}

type MessageAttachment struct {
	Transcript  string `json:"asr_refer_text"`
	Wave        string `json:"voice_wav_url"`
	URL         string `json:"url"`
	ContentType string `json:"content_type"`
	Filename    string `json:"filename"`
	Size        int64  `json:"size"`
	Width       int    `json:"width"`
	Height      int    `json:"height"`
}
type MessageElement struct {
	Content     string              `json:"content"`
	Index       string              `json:"msg_idx"`
	Kind        int                 `json:"message_type"`
	Attachments []MessageAttachment `json:"attachments"`
	Elements    []MessageElement    `json:"msg_elements"`
	Card        struct {
		Type   string `json:"ark_type"`
		Prompt string `json:"prompt"`
		Fields struct {
			Preview     string `json:"preview"`
			Title       string `json:"title"`
			Description string `json:"desc"`
			Link        string `json:"jump_url"`
		} `json:"fields"`
	} `json:"ark_data"`
}

func (e MessageElement) collectAttachments(out *[]MessageAttachment, seen map[string]bool) {
	add := func(item MessageAttachment) {
		item.URL = strings.TrimSpace(item.URL)
		if strings.HasPrefix(item.URL, "//") {
			item.URL = "https:" + item.URL
		}
		// Older QQ media links can omit the scheme or use HTTP. Fetch QQ CDN media over HTTPS.
		candidate := item.URL
		if !strings.Contains(candidate, "://") {
			candidate = "https://" + candidate
		}
		if u, err := url.Parse(candidate); err == nil && u.User == nil && (u.Scheme == "http" || u.Scheme == "https") {
			host := strings.ToLower(u.Hostname())
			if strings.HasSuffix(host, ".qq.com") || strings.HasSuffix(host, ".qq.com.cn") || strings.HasSuffix(host, ".qpic.cn") {
				u.Scheme = "https"
				item.URL = u.String()
			}
		}
		if item.URL != "" && !seen[item.URL] {
			*out = append(*out, item)
			seen[item.URL] = true
		}
	}
	for _, item := range e.Attachments {
		add(item)
	}
	if e.Card.Type == "picture" {
		add(MessageAttachment{URL: e.Card.Fields.Preview, ContentType: "image/unknown"})
	}
	for _, child := range e.Elements {
		child.collectAttachments(out, seen)
	}
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
	if sender == "" && ct == "private" {
		sender = cid
	}
	senderName := strings.TrimSpace(d.Author.Username)
	if senderName == "" {
		senderName = strings.TrimSpace(d.Author.Nickname)
	}
	if ct == "group" && strings.TrimSpace(d.Author.MemberNickname) != "" {
		senderName = strings.TrimSpace(d.Author.MemberNickname)
	}
	conversationName := strings.TrimSpace(d.GroupName)
	if ct == "private" {
		conversationName = senderName
	}
	parts := make([]domain.ContentPart, 0, len(d.Attachments)+1)
	if text := strings.TrimSpace(d.Content); text != "" {
		parts = append(parts, domain.ContentPart{Type: "text", Text: text})
	}
	var attachments []MessageAttachment
	d.collectAttachments(&attachments, map[string]bool{})
	for _, a := range attachments {
		partType := "attachment"
		mime := strings.ToLower(strings.TrimSpace(a.ContentType))
		generic := mime == "" || mime == "file" || mime == "application/octet-stream"
		ext := strings.ToLower(path.Ext(a.Filename))
		knownExt := ext == ".png" || ext == ".jpeg" || ext == ".jpg" || ext == ".webp" || ext == ".gif"
		if strings.HasPrefix(mime, "image/") || mime == "image" || generic && (knownExt || a.Width > 0 && a.Height > 0) {
			partType = "image"
		}
		parts = append(parts, domain.ContentPart{Type: partType, URL: a.URL, WaveURL: a.Wave, Transcript: a.Transcript, Text: a.Filename, Filename: a.Filename, ContentType: a.ContentType, SizeBytes: a.Size, Width: a.Width, Height: a.Height})
	}
	eventTime := time.Now().UTC()
	if d.Timestamp != "" {
		if t, err := time.Parse(time.RFC3339, d.Timestamp); err == nil {
			eventTime = t
		}
	}
	mentionsBot := false
	for _, mention := range d.Mentions {
		mentionsBot = mentionsBot || mention.IsYou
	}
	deadline := eventTime.Add(time.Hour)
	return e, &domain.InboundMessage{MentionsBot: mentionsBot, EventID: e.ID, Channel: "qq", BotID: botID, ConversationType: ct, ConversationID: cid,
		ConversationName: conversationName, SenderID: sender, SenderName: senderName, PlatformMessageID: d.ID, EventType: e.T, Text: strings.TrimSpace(d.Content),
		Parts: parts, EventTime: eventTime, ReplyDeadline: &deadline, Raw: body}, nil
}

func isMessageEvent(t string) bool {
	return t == "GROUP_AT_MESSAGE_CREATE" || t == "GROUP_MESSAGE_CREATE" || t == "C2C_MESSAGE_CREATE"
}
func ShouldReply(t string, mentionsBot bool) bool {
	return t == "GROUP_AT_MESSAGE_CREATE" || t == "C2C_MESSAGE_CREATE" || (t == "GROUP_MESSAGE_CREATE" && mentionsBot)
}
func ShouldQueue(t, triggerMode string, mentionsBot bool) bool {
	return ShouldReply(t, mentionsBot) || (t == "GROUP_MESSAGE_CREATE" && (triggerMode == "always" || triggerMode == "smart"))
}
