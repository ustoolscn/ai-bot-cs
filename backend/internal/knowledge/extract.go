package knowledge

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/xml"
	"errors"
	_ "image/jpeg"
	_ "image/png"
	"io"
	"path/filepath"
	"strings"

	_ "golang.org/x/image/webp"
)

const Formats = "TXT、MD、PDF、DOCX、PNG、JPG、JPEG、WEBP"
const textLimit = 2 << 20

func IsImage(name string) bool {
	switch strings.ToLower(filepath.Ext(name)) {
	case ".png", ".jpg", ".jpeg", ".webp":
		return true
	}
	return false
}

func Supported(name string) bool {
	switch strings.ToLower(filepath.Ext(name)) {
	case ".txt", ".md", ".pdf", ".docx":
		return true
	}
	return IsImage(name)
}

const ImageModelRequired = "当前向量模型不支持图片，请更换为图文向量模型"

func Extract(ctx context.Context, name string, data []byte) (string, error) {
	switch strings.ToLower(filepath.Ext(name)) {
	case ".txt", ".md":
		return string(data), nil
	case ".docx":
		return wordText(data)
	case ".pdf":
		pages, err := readPDF(ctx, data, false)
		if err != nil {
			return "", err
		}
		var all []string
		for _, p := range pages {
			all = append(all, p.Text)
		}
		if len(all) == 0 {
			return "", errors.New("PDF 没有可提取的文字，请使用图文向量模型处理扫描页")
		}
		return strings.Join(all, "\n"), nil
	}
	if IsImage(name) {
		return "", errors.New(ImageModelRequired)
	}
	return "", errors.New("不支持的文档格式")
}

func wordText(data []byte) (string, error) {
	r, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return "", errors.New("DOCX 格式无效")
	}
	for _, f := range r.File {
		if f.Name != "word/document.xml" {
			continue
		}
		if f.UncompressedSize64 > 16<<20 {
			return "", errors.New("DOCX 正文过大，请拆分文件")
		}
		rc, err := f.Open()
		if err != nil {
			return "", err
		}
		defer rc.Close()
		d := xml.NewDecoder(io.LimitReader(rc, (16<<20)+1))
		var out strings.Builder
		for {
			tok, err := d.Token()
			if err == io.EOF {
				break
			}
			if err != nil {
				return "", errors.New("DOCX 正文损坏")
			}
			switch e := tok.(type) {
			case xml.StartElement:
				if !wordNamespace(e.Name.Space) {
					continue
				}
				switch e.Name.Local {
				case "t":
					var s string
					if err := d.DecodeElement(&s, &e); err != nil {
						return "", err
					}
					out.WriteString(s)
				case "tab":
					out.WriteByte('\t')
				case "br", "cr":
					out.WriteByte('\n')
				}
			case xml.EndElement:
				if wordNamespace(e.Name.Space) && e.Name.Local == "p" {
					out.WriteByte('\n')
				}
			}
			if out.Len() > textLimit {
				return "", errors.New("提取的文本超过 2 MiB，请拆分文件")
			}
		}
		result := strings.TrimSpace(out.String())
		if result == "" {
			return "", errors.New("DOCX 没有可提取的正文，请将图片单独上传")
		}
		return result, nil
	}
	return "", errors.New("DOCX 缺少正文")
}

func wordNamespace(ns string) bool {
	return ns == "http://schemas.openxmlformats.org/wordprocessingml/2006/main" || ns == "http://purl.oclc.org/ooxml/wordprocessingml/main"
}
