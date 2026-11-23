package app

import (
	"bytes"
	"context"
	"encoding/base64"
	"fmt"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"ai-bot/backend/internal/domain"
	"github.com/chromedp/chromedp"
	"golang.org/x/net/html"
)

func browserPath() string {
	if path := os.Getenv("HTML_RENDER_BROWSER"); path != "" {
		return path
	}
	for _, name := range []string{"chromium", "chromium-browser", "google-chrome", "msedge"} {
		if path, err := exec.LookPath(name); err == nil {
			return path
		}
	}
	if runtime.GOOS == "darwin" {
		path := "/Applications/Google Chrome.app/Contents/MacOS/Google Chrome"
		if _, err := os.Stat(path); err == nil {
			return path
		}
	}
	if runtime.GOOS == "windows" {
		for _, root := range []string{os.Getenv("ProgramFiles"), os.Getenv("ProgramFiles(x86)")} {
			for _, sub := range []string{"Microsoft/Edge/Application/msedge.exe", "Google/Chrome/Application/chrome.exe"} {
				path := filepath.Join(root, sub)
				if _, err := os.Stat(path); err == nil {
					return path
				}
			}
		}
	}
	return ""
}

// Rebuild a static document with presentation elements and attributes only.
func staticDocument(src string) (string, error) {
	src = strings.TrimSpace(src)
	if strings.HasPrefix(src, "```") {
		_, src, _ = strings.Cut(src, "\n")
		src = strings.TrimSuffix(strings.TrimSpace(src), "```")
	}
	if src == "" || len(src) > 256<<10 {
		return "", fmt.Errorf("HTML 为空或超过 256 KB")
	}
	doc, err := html.Parse(strings.NewReader(normalizeHTMLAttributes(src)))
	if err != nil {
		return "", err
	}
	allowed := " html head a body style title main article section header footer aside div span h1 h2 h3 h4 h5 h6 p ul ol li b strong em i small pre code br hr table thead tbody tr th td blockquote figure figcaption time img dl dt dd "
	var clean func(*html.Node)
	clean = func(n *html.Node) {
		for c := n.FirstChild; c != nil; {
			next := c.NextSibling
			if c.Type == html.ElementNode && c.Namespace == "svg" && strings.Contains(svgNodes, " "+c.Data+" ") {
				cleanSVG(c)
				clean(c)
			} else if c.Type == html.ElementNode && c.Namespace == "" && strings.Contains(allowed, " "+c.Data+" ") {
				attrs := c.Attr[:0]
				for _, attr := range c.Attr {
					switch attr.Key {
					case "id", "class", "style", "colspan", "rowspan", "alt", "width", "height", "lang", "dir":
						attrs = append(attrs, attr)
					case "href":
						u, err := url.Parse(attr.Val)
						if c.Data == "a" && err == nil && (u.Scheme == "https" || u.Scheme == "http") && u.Hostname() != "" && u.User == nil {
							attrs = append(attrs, attr)
						}
					case "src":
						if c.Data == "img" && (strings.HasPrefix(attr.Val, "data:image/png;base64,") || strings.HasPrefix(attr.Val, "data:image/jpeg;base64,")) {
							attrs = append(attrs, attr)
						}
					}
				}
				if c.Data == "head" {
					attrs = nil
				}
				c.Attr = attrs
				clean(c)
			} else if c.Type != html.TextNode {
				n.RemoveChild(c)
			}
			c = next
		}
	}
	clean(doc)
	var body bytes.Buffer
	if err := html.Render(&body, doc); err != nil {
		return "", err
	}
	const policy = "<meta charset=utf-8><meta name=viewport content=\"width=device-width, initial-scale=1\"><meta http-equiv=\"Content-Security-Policy\" content=\"default-src 'none'; style-src 'unsafe-inline'; img-src data:; base-uri 'none'; form-action 'none'\"><style>html{background:white;font:18px/1.55 sans-serif}*{box-sizing:border-box;animation:none!important;transition:none!important}body{overflow-wrap:anywhere}</style>"
	return "<!doctype html>" + strings.Replace(body.String(), "<head>", "<head>"+policy, 1), nil
}

var renderSlots = make(chan struct{}, 2)

func (a *App) htmlImage(ctx context.Context, src string) (domain.ContentPart, error) {
	var part domain.ContentPart
	doc, err := staticDocument(src)
	if err != nil {
		return part, err
	}
	path := browserPath()
	if path == "" {
		return part, fmt.Errorf("未找到 Chromium 或 Edge，请安装浏览器或设置 HTML_RENDER_BROWSER")
	}
	if runtime.GOOS == "linux" && os.Getuid() == 0 {
		return part, fmt.Errorf("请使用普通用户运行截图服务")
	}
	ctx, cancel := context.WithTimeout(ctx, 35*time.Second)
	defer cancel()
	select {
	case renderSlots <- struct{}{}:
		defer func() { <-renderSlots }()
	case <-ctx.Done():
		return part, ctx.Err()
	}
	opts := append([]chromedp.ExecAllocatorOption{}, chromedp.DefaultExecAllocatorOptions[:]...)
	opts = append(opts, chromedp.ExecPath(path), chromedp.Flag("host-resolver-rules", "MAP * ~NOTFOUND"), chromedp.Flag("disable-gpu", true))
	alloc, stop := chromedp.NewExecAllocator(ctx, opts...)
	defer stop()
	tab, closeTab := chromedp.NewContext(alloc)
	defer closeTab()
	var height, width int64
	var png []byte
	err = chromedp.Run(tab, chromedp.EmulateViewport(1000, 240), chromedp.Navigate("data:text/html;charset=utf-8;base64,"+base64.StdEncoding.EncodeToString([]byte(doc))),
		chromedp.Evaluate("document.documentElement.scrollHeight", &height), chromedp.Evaluate("document.documentElement.scrollWidth", &width))
	if err != nil {
		return part, fmt.Errorf("网页渲染失败: %w", err)
	}
	if height > 10000 || width > 1600 {
		return part, fmt.Errorf("页面超过 1600 × 10000，请压缩布局后重试")
	}
	if err = chromedp.Run(tab, chromedp.EmulateViewport(width, height), chromedp.CaptureScreenshot(&png)); err != nil {
		return part, err
	}
	if len(png) > 8<<20 {
		return part, fmt.Errorf("截图超过 8 MB，请简化页面")
	}
	key, err := a.images.Save(ctx, "page.png", png)
	if err != nil {
		return part, err
	}
	return domain.ContentPart{Type: "image", ContentType: "image/png", Filename: "page.png", StorageKey: key, SizeBytes: int64(len(png)), Width: int(width), Height: int(height)}, nil
}

func hasStoredImage(parts []domain.ContentPart) bool {
	for _, part := range parts {
		if part.Type == "image" && part.StorageKey != "" {
			return true
		}
	}
	return false
}
