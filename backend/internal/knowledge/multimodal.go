package knowledge

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/base64"
	"encoding/xml"
	"errors"
	"fmt"
	"image"
	"image/jpeg"
	"io"
	"math"
	"net/url"
	"path"
	"path/filepath"
	"regexp"
	"strings"

	"golang.org/x/image/draw"
)

type Section struct {
	Text, MIME string
	Image      []byte
}
type paragraph struct {
	text   string
	images []string
}

// Index and deliver the same normalized pixels. The limits apply before decoding.
func picture(raw []byte) (Section, error) {
	cfg, format, e := image.DecodeConfig(bytes.NewReader(raw))
	if e != nil || cfg.Width < 1 || cfg.Height < 1 || cfg.Width > 16000000/cfg.Height {
		return Section{}, errors.New("截图格式无效或超过 1600 万像素")
	}
	if format != "png" && format != "jpeg" && format != "webp" {
		return Section{}, errors.New("仅支持 PNG、JPEG、WebP 截图")
	}
	if format != "webp" && max(cfg.Width, cfg.Height) <= 2000 && len(raw) <= 2<<20 {
		return Section{MIME: "image/" + format, Image: raw}, nil
	}
	img, _, e := image.Decode(bytes.NewReader(raw))
	if e != nil {
		return Section{}, e
	}
	scale := math.Min(1, 2000/float64(max(cfg.Width, cfg.Height)))
	dst := image.NewRGBA(image.Rect(0, 0, max(1, int(float64(cfg.Width)*scale)), max(1, int(float64(cfg.Height)*scale))))
	draw.Draw(dst, dst.Bounds(), image.White, image.Point{}, draw.Src)
	draw.CatmullRom.Scale(dst, dst.Bounds(), img, img.Bounds(), draw.Over, nil)
	var b bytes.Buffer
	if e = jpeg.Encode(&b, dst, &jpeg.Options{Quality: 85}); e != nil {
		return Section{}, e
	}
	return Section{MIME: "image/jpeg", Image: b.Bytes()}, nil
}

type sections struct {
	items         []Section
	bytes, images int
	textBytes     int
}

func (s *sections) add(paras []paragraph, load func(string) ([]byte, error)) error {
	for _, p := range paras {
		s.textBytes += len(p.text)
		if s.textBytes > textLimit {
			return errors.New("提取的文本超过 2 MiB，请拆分文件")
		}
	}
	// Office often puts a screenshot in its own paragraph immediately after its instruction.
	for i, p := range paras {
		if len(p.images) == 0 || strings.TrimSpace(p.text) != "" {
			continue
		}
		if i > 0 && len(paras[i-1].images) == 0 && strings.TrimSpace(paras[i-1].text) != "" {
			paras[i].text = paras[i-1].text
			paras[i-1].text = ""
		} else if i+1 < len(paras) && len(paras[i+1].images) == 0 {
			paras[i].text = paras[i+1].text
			paras[i+1].text = ""
		}
	}
	var plain strings.Builder
	flush := func() {
		for _, c := range Chunk(strings.TrimSpace(plain.String()), 1000, 150) {
			s.items = append(s.items, Section{Text: c})
		}
		plain.Reset()
	}
	for _, p := range paras {
		if len(p.images) == 0 {
			plain.WriteString(p.text)
			plain.WriteByte('\n')
			continue
		}
		flush()
		chunks := Chunk(strings.TrimSpace(p.text), 1000, 150)
		var caption string
		if len(chunks) > 0 {
			caption = chunks[len(chunks)-1]
			for _, c := range chunks[:len(chunks)-1] {
				s.items = append(s.items, Section{Text: c})
			}
		}
		for _, ref := range p.images {
			if s.images >= 64 {
				return errors.New("每份文档最多支持 64 张截图，请拆分文件")
			}
			raw, e := load(ref)
			if e != nil {
				return e
			}
			pic, e := picture(raw)
			if e != nil {
				return e
			}
			s.bytes += len(pic.Image)
			s.images++
			if s.bytes > 32<<20 {
				return errors.New("提取的截图超过 32 MiB，请拆分文件")
			}
			pic.Text = caption
			s.items = append(s.items, pic)
		}
	}
	flush()
	return nil
}

func Multimodal(ctx context.Context, name string, data []byte, resolve func(string) ([]byte, error)) ([]Section, error) {
	var s sections
	switch strings.ToLower(filepath.Ext(name)) {
	case ".docx":
		return wordSections(data)
	case ".pdf":
		return readPDF(ctx, data, true)
	case ".md":
		if markdownImage.Match(data) {
			var paragraphs []paragraph
			for _, line := range strings.Split(string(data), "\n") {
				p := paragraph{}
				p.text = markdownImage.ReplaceAllStringFunc(line, func(tag string) string {
					m := markdownImage.FindStringSubmatch(tag)
					ref := strings.Trim(m[2], "<>")
					if inlineMarkdown.MatchString(ref) {
						p.images = append(p.images, ref)
						return m[1]
					}
					u, e := url.Parse(ref)
					if e != nil || u.Scheme != "" || u.Host != "" || strings.HasPrefix(u.Path, "/") || !IsImage(u.Path) {
						return tag
					}
					p.images = append(p.images, path.Base(u.Path))
					return m[1]
				})
				if len(p.images) > 0 && strings.TrimSpace(markdownImage.ReplaceAllString(line, "")) == "" && len(paragraphs) > 0 {
					prev := &paragraphs[len(paragraphs)-1]
					if len(prev.images) == 0 && prev.text != "" {
						p.text = strings.TrimSpace(prev.text + "\n" + p.text)
						prev.text = ""
					}
				}
				if strings.TrimSpace(p.text) != "" || len(p.images) > 0 {
					paragraphs = append(paragraphs, p)
				}
			}
			load := func(ref string) ([]byte, error) {
				if m := inlineMarkdown.FindStringSubmatch(ref); m != nil {
					return base64.StdEncoding.DecodeString(m[1])
				}
				return resolve(ref)
			}
			err := s.add(paragraphs, load)
			return s.items, err
		}
		fallthrough
	case ".txt":
		for _, c := range Chunk(string(data), 1000, 150) {
			s.items = append(s.items, Section{Text: c})
		}
	default:
		pic, e := picture(data)
		if e != nil {
			return nil, e
		}
		s.items = []Section{pic}
	}
	return s.items, nil
}

var markdownImage = regexp.MustCompile(`!\[([^\]\n]*)\]\((<[^>\n]+>|[^()\s]+)\)`)
var inlineMarkdown = regexp.MustCompile(`^data:image/(?:png|jpeg|webp);base64,([a-zA-Z0-9+/=]+)$`)

func wordSections(data []byte) ([]Section, error) {
	zr, e := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if e != nil {
		return nil, e
	}
	files := map[string]*zip.File{}
	for _, f := range zr.File {
		files[f.Name] = f
	}
	read := func(name string) ([]byte, error) {
		f := files[name]
		if f == nil || f.UncompressedSize64 > 16<<20 {
			return nil, fmt.Errorf("DOCX 部件缺失或过大：%s", name)
		}
		r, e := f.Open()
		if e != nil {
			return nil, e
		}
		defer r.Close()
		return io.ReadAll(io.LimitReader(r, (16<<20)+1))
	}
	rels := map[string]string{}
	if files["word/_rels/document.xml.rels"] != nil {
		b, e := read("word/_rels/document.xml.rels")
		if e != nil {
			return nil, e
		}
		var r struct {
			Items []struct {
				ID     string `xml:"Id,attr"`
				Target string `xml:"Target,attr"`
				Mode   string `xml:"TargetMode,attr"`
				Type   string `xml:"Type,attr"`
			} `xml:"Relationship"`
		}
		if e = xml.Unmarshal(b, &r); e != nil {
			return nil, e
		}
		for _, v := range r.Items {
			if v.Mode != "External" && strings.HasSuffix(v.Type, "/image") {
				target := path.Join("word", v.Target)
				if strings.HasPrefix(v.Target, "/") {
					target = strings.TrimPrefix(path.Clean(v.Target), "/")
				}
				rels[v.ID] = target
			}
		}
	}
	b, e := read("word/document.xml")
	if e != nil {
		return nil, e
	}
	d := xml.NewDecoder(bytes.NewReader(b))
	var ps []paragraph
	var line strings.Builder
	var imgs []string
	for {
		t, e := d.Token()
		if e == io.EOF {
			break
		}
		if e != nil {
			return nil, e
		}
		switch el := t.(type) {
		case xml.StartElement:
			if wordNamespace(el.Name.Space) {
				switch el.Name.Local {
				case "t":
					var v string
					if e = d.DecodeElement(&v, &el); e != nil {
						return nil, e
					}
					line.WriteString(v)
				case "tab", "br", "cr":
					line.WriteByte(' ')
				}
			}
			if el.Name.Local == "blip" || el.Name.Local == "imagedata" {
				for _, a := range el.Attr {
					if (a.Name.Local == "embed" || a.Name.Local == "id") && rels[a.Value] != "" {
						imgs = append(imgs, rels[a.Value])
					}
				}
			}
		case xml.EndElement:
			if el.Name.Local == "p" && wordNamespace(el.Name.Space) {
				if line.Len() > 0 || len(imgs) > 0 {
					ps = append(ps, paragraph{line.String(), imgs})
				}
				line.Reset()
				imgs = nil
			}
		}
	}
	var s sections
	e = s.add(ps, read)
	return s.items, e
}
