package qq

import (
	"encoding/base64"
	"encoding/json"
	"regexp"
	"strings"
)

var faceTag = regexp.MustCompile(`<faceType=[^<>]*>`)
var faceExt = regexp.MustCompile(`(?:,|\s)\s*ext="([^"]+)"`)

func displayFaces(body string) string {
	return faceTag.ReplaceAllStringFunc(body, func(tag string) string {
		match := faceExt.FindStringSubmatch(tag)
		if match != nil {
			data, err := base64.RawStdEncoding.DecodeString(strings.TrimRight(match[1], "="))
			var face struct{ Text string }
			if err == nil && json.Unmarshal(data, &face) == nil && strings.TrimSpace(face.Text) != "" {
				name := strings.TrimSpace(face.Text)
				if strings.HasPrefix(name, "[") && strings.HasSuffix(name, "]") {
					return name
				}
				return "[" + name + "]"
			}
		}
		return "[表情]"
	})
}
