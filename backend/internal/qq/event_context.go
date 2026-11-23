package qq

import (
	"encoding/json"
	"strings"
)

type EventDescription struct {
	Reference, Elements, Extra string
	IsQuote                    bool
}

func DescribeEvent(raw []byte) EventDescription {
	var event struct {
		D struct {
			MessageElement
			Scene    struct{ Ext []string } `json:"message_scene"`
			Mentions []struct {
				ID       string `json:"member_openid"`
				Name     string `json:"member_nickname"`
				Username string
				Scope    string
				Role     string `json:"member_role"`
				Bot      bool
			}
		}
	}
	if json.Unmarshal(raw, &event) != nil {
		return EventDescription{}
	}
	d := event.D
	r := EventDescription{IsQuote: d.Kind == 103}
	for _, v := range d.Scene.Ext {
		if s, ok := strings.CutPrefix(v, "ref_msg_idx="); ok {
			r.Reference = s
			break
		}
	}
	var extra, elements []string
	var visit func(MessageElement, bool)
	visit = func(e MessageElement, nested bool) {
		if nested && strings.TrimSpace(e.Content) != "" {
			elements = append(elements, e.Content)
		}
		if e.Card.Type != "" && e.Card.Type != "picture" {
			f := e.Card.Fields
			elements = append(elements, strings.TrimSpace(strings.Join([]string{e.Card.Prompt, f.Title, f.Description, f.Link}, " ")))
		}
		for _, a := range e.Attachments {
			if a.Transcript != "" {
				elements = append(elements, "语音识别："+a.Transcript)
			}
		}
		for _, child := range e.Elements {
			if r.IsQuote && r.Reference == "" && child.Index != "" {
				r.Reference = child.Index
			}
			visit(child, true)
		}
	}
	visit(d.MessageElement, false)
	for _, m := range d.Mentions {
		if m.Scope == "all" {
			extra = append(extra, "@全体成员")
			continue
		}
		name := m.Name
		if name == "" {
			name = m.Username
		}
		if name == "" {
			name = m.ID
		}
		if m.Bot {
			name += "（机器人）"
		}
		if name != "" {
			extra = append(extra, "@"+name+" "+m.Role)
		}
	}
	r.Elements = strings.Join(elements, "\n")
	r.Extra = strings.Join(extra, " ")
	return r
}
