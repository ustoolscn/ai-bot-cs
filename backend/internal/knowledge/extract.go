package knowledge

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/xml"
	"errors"
	"fmt"
	"image"
	_ "image/jpeg"
	_ "image/png"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

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

func Extract(ctx context.Context, name string, data []byte, description string) (string, error) {
	switch strings.ToLower(filepath.Ext(name)) {
	case ".txt", ".md":
		return string(data), nil
	case ".docx":
		return wordText(data)
	case ".pdf":
		result, err := convert(ctx, data, "pdftotext", "-enc", "UTF-8", "-", "-")
		if err == nil && strings.TrimSpace(result) == "" {
			return "", errors.New("PDF 没有可提取的文字，请将扫描页作为图片上传")
		}
		return result, err
	}
	if !IsImage(name) {
		return "", errors.New("不支持的文档格式")
	}
	cfg, format, err := image.DecodeConfig(bytes.NewReader(data))
	ext := strings.TrimPrefix(strings.ToLower(filepath.Ext(name)), ".")
	if ext == "jpg" {
		ext = "jpeg"
	}
	if err != nil || format != ext {
		return "", errors.New("图片格式无效或与扩展名不符")
	}
	if cfg.Width <= 0 || cfg.Height <= 0 || int64(cfg.Width) > 16000000/int64(cfg.Height) {
		return "", errors.New("图片超过 1600 万像素，请缩小后上传")
	}
	if description != "" {
		return description, nil
	}
	result, err := convert(ctx, data, "tesseract", "stdin", "stdout", "-l", "chi_sim+eng")
	if err == nil && strings.TrimSpace(result) == "" {
		return "", errors.New("图片未识别到文字，请添加图片说明后重新上传")
	}
	return result, err
}

type limitedOutput struct{ buffer bytes.Buffer }

func (b *limitedOutput) Write(p []byte) (int, error) {
	if len(p) > textLimit-b.buffer.Len() {
		return 0, errors.New("提取的文本超过 2 MiB，请拆分文件")
	}
	return b.buffer.Write(p)
}

func convert(ctx context.Context, data []byte, program string, args ...string) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, 45*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, program, args...)
	cmd.Stdin = bytes.NewReader(data)
	cmd.Env = append(os.Environ(), "OMP_THREAD_LIMIT=1")
	var out limitedOutput
	cmd.Stdout = &out
	if err := cmd.Run(); err != nil {
		if ctx.Err() != nil {
			return "", fmt.Errorf("解析超时或已取消：%w", ctx.Err())
		}
		return "", fmt.Errorf("解析失败（%s）：%w", program, err)
	}
	return strings.TrimSpace(out.buffer.String()), nil
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
