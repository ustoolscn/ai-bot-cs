package qq

import (
	"encoding/json"
	"strings"
)

// ReferenceIndex is distinct from d.id (the passive-send token).
func ReferenceIndex(raw []byte) string {
	var event struct {
		D     json.RawMessage `json:"d"`
		Scene struct {
			Ext []string `json:"ext"`
		} `json:"message_scene"`
	}
	if json.Unmarshal(raw, &event) != nil {
		return ""
	}
	if len(event.D) > 0 {
		return ReferenceIndex(event.D)
	}
	for _, ext := range event.Scene.Ext {
		if v, ok := strings.CutPrefix(ext, "msg_idx="); ok {
			return v
		}
	}
	return ""
}
