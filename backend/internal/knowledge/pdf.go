package knowledge

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"math"
	"strings"

	reader "github.com/ledongthuc/pdf"
	"github.com/pdfcpu/pdfcpu/pkg/api"
	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu"
	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu/model"
)

// pdfcpu otherwise creates configuration and fonts in the service user's home.
func init() { model.ConfigPath = "disable" }

func readPDF(ctx context.Context, data []byte, withImages bool) (result []Section, err error) {
	defer func() {
		if recover() != nil {
			result = nil
			err = errors.New("PDF 损坏或使用了不支持的编码")
		}
	}()
	doc, err := reader.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return nil, fmt.Errorf("无法读取 PDF：%w", err)
	}
	count := doc.NumPage()
	if count < 1 || count > 200 {
		return nil, errors.New("PDF 页数必须在 1 到 200 之间")
	}
	var resources *model.Context
	if withImages {
		conf := model.NewDefaultConfiguration()
		conf.Cmd = model.EXTRACTIMAGES
		resources, err = api.ReadValidateAndOptimize(bytes.NewReader(data), conf)
		if err != nil {
			return nil, fmt.Errorf("PDF 图片资源解析失败：%w", err)
		}
	}
	var s sections
	for n := 1; n <= count; n++ {
		if err = ctx.Err(); err != nil {
			return nil, err
		}
		page := doc.Page(n)
		if !withImages {
			txt, e := page.GetPlainText(nil)
			if e != nil {
				return nil, e
			}
			txt = strings.TrimSpace(txt)
			s.textBytes += len(txt)
			if s.textBytes > textLimit {
				return nil, errors.New("提取的文本超过 2 MiB，请拆分文件")
			}
			if txt != "" {
				s.items = append(s.items, Section{Text: txt})
			}
			continue
		}
		rows, e := page.GetTextByRow()
		if e != nil {
			return nil, e
		}
		ps := make([]paragraph, len(rows))
		for i, row := range rows {
			var b strings.Builder
			for _, part := range row.Content {
				b.WriteString(part.S)
			}
			ps[i].text = b.String()
		}
		imgs, e := pdfcpu.ExtractPageImages(resources, n, true)
		if e != nil {
			return nil, e
		}
		refs := map[string]model.Image{}
		for _, img := range imgs {
			if !img.Thumb && !img.IsImgMask {
				refs[img.Name] = img
			}
		}
		if len(refs) > 0 {
			places := pdfPlacements(page)
			for _, loc := range places {
				if _, ok := refs[loc.name]; !ok {
					continue
				}
				if len(ps) == 0 {
					ps = append(ps, paragraph{})
				}
				best := 0
				distance := math.Inf(1)
				for i, row := range rows {
					score := math.Abs(float64(row.Position) - loc.y)
					if len(row.Content) > 0 {
						score += 0.15 * math.Abs(row.Content[0].X-loc.x)
					}
					if score < distance {
						distance = score
						best = i
					}
				}
				ps[best].images = append(ps[best].images, loc.name)
			}
		}
		e = s.add(ps, func(name string) ([]byte, error) {
			img := refs[name]
			if img.Width < 1 || img.Height < 1 || img.Width > 16000000/img.Height {
				return nil, errors.New("PDF 图片超过 1600 万像素，请缩小后上传")
			}
			obj := resources.Optimize.ImageObjects[img.ObjNr]
			pic, e := pdfcpu.ExtractImage(resources, obj.ImageDict, false, name, img.ObjNr, false)
			if e != nil {
				return nil, e
			}
			if pic == nil || pic.Reader == nil {
				return nil, errors.New("PDF 截图编码不受支持")
			}
			raw, e := io.ReadAll(io.LimitReader(pic.Reader, (16<<20)+1))
			if len(raw) > 16<<20 {
				return nil, errors.New("PDF 截图过大")
			}
			return raw, e
		})
		if e != nil {
			return nil, e
		}
	}
	return s.items, nil
}

type placement struct {
	name string
	x, y float64
}
type transform [6]float64

func (m transform) compose(n transform) transform {
	return transform{m[0]*n[0] + m[2]*n[1], m[1]*n[0] + m[3]*n[1], m[0]*n[2] + m[2]*n[3], m[1]*n[2] + m[3]*n[3], m[0]*n[4] + m[2]*n[5] + m[4], m[1]*n[4] + m[3]*n[5] + m[5]}
}

// Track each painted image, including repeated uses of the same resource.
func pdfPlacements(page reader.Page) []placement {
	m := transform{1, 0, 0, 1, 0, 0}
	var stack []transform
	var out []placement
	reader.Interpret(page.V.Key("Contents"), func(st *reader.Stack, op string) {
		switch op {
		case "q":
			stack = append(stack, m)
		case "Q":
			if len(stack) > 0 {
				m = stack[len(stack)-1]
				stack = stack[:len(stack)-1]
			}
		case "cm":
			if st.Len() >= 6 {
				var n transform
				for i := 5; i >= 0; i-- {
					n[i] = st.Pop().Float64()
				}
				m = m.compose(n)
			}
		case "Do":
			name := st.Pop().Name()
			out = append(out, placement{name, m[4], m[5] + math.Max(0, m[3])})
		}
		for st.Len() > 0 {
			st.Pop()
		}
	})
	return out
}
