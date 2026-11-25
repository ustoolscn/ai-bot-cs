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
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

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

func Multimodal(ctx context.Context, name string, data []byte, description string, resolve func(string) ([]byte, error)) ([]Section, error) {
	var s sections
	switch strings.ToLower(filepath.Ext(name)) {
	case ".docx":
		return wordSections(data)
	case ".pdf":
		return pdfSections(ctx, data)
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
		pic.Text = description
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

type pdfNode struct {
	Top, Left, Width, Height int
	Src, Text                string
}

func (n *pdfNode) UnmarshalXML(d *xml.Decoder, start xml.StartElement) error {
	for _, a := range start.Attr {
		v, _ := strconv.Atoi(a.Value)
		switch a.Name.Local {
		case "top":
			n.Top = v
		case "left":
			n.Left = v
		case "width":
			n.Width = v
		case "height":
			n.Height = v
		case "src":
			n.Src = a.Value
		}
	}
	depth := 1
	var b strings.Builder
	for depth > 0 {
		t, e := d.Token()
		if e != nil {
			return e
		}
		switch x := t.(type) {
		case xml.StartElement:
			depth++
		case xml.EndElement:
			depth--
		case xml.CharData:
			b.Write(x)
		}
	}
	n.Text = strings.TrimSpace(b.String())
	return nil
}

var pageCount = regexp.MustCompile(`(?m)^Pages:\s+(\d+)`)

func pdfSections(ctx context.Context, data []byte) ([]Section, error) {
	ctx, cancel := context.WithTimeout(ctx, 60*time.Second)
	defer cancel()
	dir, e := os.MkdirTemp("", "kb-pdf-")
	if e != nil {
		return nil, e
	}
	defer os.RemoveAll(dir)
	src := filepath.Join(dir, "input.pdf")
	if e = os.WriteFile(src, data, 0600); e != nil {
		return nil, e
	}
	info, e := exec.CommandContext(ctx, "pdfinfo", src).Output()
	if e != nil {
		return nil, errors.New("PDF 无法读取，请检查文件或移除密码")
	}
	m := pageCount.FindSubmatch(info)
	if m == nil {
		return nil, errors.New("无法读取 PDF 页数")
	}
	count, _ := strconv.Atoi(string(m[1]))
	if count > 200 {
		return nil, errors.New("每份 PDF 最多支持 200 页，请拆分文件")
	}
	// Poppler decodes embedded bitmaps to disk. Inspect dimensions before extraction.
	var listing limitedOutput
	probe := exec.CommandContext(ctx, "pdfimages", "-list", src)
	probe.Stdout = &listing
	if e = probe.Run(); e != nil {
		return nil, errors.New("无法读取 PDF 图片信息")
	}
	var pixels int64
	for _, line := range strings.Split(listing.buffer.String(), "\n") {
		fields := strings.Fields(line)
		if len(fields) < 5 {
			continue
		}
		if _, e := strconv.Atoi(fields[0]); e != nil {
			continue
		}
		w, ew := strconv.ParseInt(fields[3], 10, 32)
		h, eh := strconv.ParseInt(fields[4], 10, 32)
		if ew != nil || eh != nil || w <= 0 || h <= 0 || w*h > 16000000 {
			return nil, errors.New("PDF 图片超过 1600 万像素，请缩小或拆分文件")
		}
		pixels += w * h
		if pixels > 96000000 {
			return nil, errors.New("PDF 图片总像素过多，请拆分文件")
		}
	}
	var s sections
	for i := 1; i <= count; i++ {
		page := strconv.Itoa(i)
		cmd := exec.CommandContext(ctx, "pdftohtml", "-q", "-xml", "-zoom", "1", "-f", page, "-l", page, src, "page.xml")
		cmd.Dir = dir
		if e = cmd.Run(); e != nil {
			return nil, errors.New("PDF 页面解析失败")
		}
		var doc struct {
			Page struct {
				Lines  []pdfNode `xml:"text"`
				Images []pdfNode `xml:"image"`
			} `xml:"page"`
		}
		file := filepath.Join(dir, "page.xml")
		f, e := os.Open(file)
		if e != nil {
			return nil, e
		}
		e = xml.NewDecoder(io.LimitReader(f, textLimit)).Decode(&doc)
		f.Close()
		if e != nil {
			return nil, e
		}
		ps := make([]paragraph, len(doc.Page.Lines))
		for i, l := range doc.Page.Lines {
			ps[i].text = l.Text
		}
		for _, img := range doc.Page.Images {
			if len(ps) == 0 {
				ps = append(ps, paragraph{})
			}
			best := 0
			dist := math.MaxFloat64
			for j, l := range doc.Page.Lines {
				vertical := max(0, l.Top-img.Top-img.Height, img.Top-l.Top-l.Height)
				horizontal := max(0, l.Left-img.Left-img.Width, img.Left-l.Left-l.Width)
				score := math.Hypot(float64(horizontal), float64(vertical))
				if score < dist {
					best = j
					dist = score
				}
			}
			ps[best].images = append(ps[best].images, img.Src)
		}
		e = s.add(ps, func(ref string) ([]byte, error) {
			name := filepath.Join(dir, filepath.Base(ref))
			stat, e := os.Stat(name)
			if e != nil {
				return nil, e
			}
			if stat.Size() > 16<<20 {
				return nil, errors.New("PDF 截图过大")
			}
			return os.ReadFile(name)
		})
		if e != nil {
			return nil, e
		}
		for _, img := range doc.Page.Images {
			os.Remove(filepath.Join(dir, filepath.Base(img.Src)))
		}
	}
	return s.items, nil
}
