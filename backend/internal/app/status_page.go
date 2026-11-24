package app

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"image/png"
	"io"
	"math"
	"net"
	"net/http"
	"net/url"
	"os"
	"regexp"
	goruntime "runtime"
	"time"

	"ai-bot/backend/internal/domain"
	"ai-bot/backend/internal/statuscard"
	"github.com/chromedp/cdproto/page"
	"github.com/chromedp/cdproto/runtime"
	"github.com/chromedp/chromedp"
)

// Pin connections to resolved public addresses, including subresources and redirects.
func publicDial(ctx context.Context, network, addr string) (net.Conn, error) {
	host, port, err := net.SplitHostPort(addr)
	if err != nil {
		return nil, err
	}
	ips, err := net.DefaultResolver.LookupIPAddr(ctx, host)
	if err != nil {
		return nil, err
	}
	for _, ip := range ips {
		if !ip.IP.IsGlobalUnicast() || ip.IP.IsPrivate() || ip.IP.IsLoopback() || ip.IP.IsLinkLocalUnicast() || ip.IP.IsInterfaceLocalMulticast() {
			return nil, errors.New("状态页不能访问内网地址")
		}
	}
	if len(ips) == 0 {
		return nil, errors.New("状态页地址无解析结果")
	}
	d := net.Dialer{Timeout: 5 * time.Second}
	for _, ip := range ips {
		var conn net.Conn
		conn, err = d.DialContext(ctx, network, net.JoinHostPort(ip.String(), port))
		if err == nil {
			return conn, nil
		}
		if ctx.Err() != nil {
			break
		}
	}
	return nil, err
}

func (a *App) statusPageImage(ctx context.Context, target string) (domain.ContentPart, error) {
	return a.renderStatusPage(ctx, target, publicDial)
}

func (a *App) renderStatusPage(ctx context.Context, target string, dial func(context.Context, string, string) (net.Conn, error)) (domain.ContentPart, error) {
	var empty domain.ContentPart
	if goruntime.GOOS == "linux" && os.Getuid() == 0 {
		return empty, errors.New("请使用普通用户运行截图服务")
	}
	if err := validateStatusURL(target); err != nil {
		return empty, err
	}
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	select {
	case renderSlots <- struct{}{}:
		defer func() { <-renderSlots }()
	case <-ctx.Done():
		return empty, ctx.Err()
	}
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return empty, err
	}
	transport := &http.Transport{DialContext: dial, DisableKeepAlives: true}
	proxy := http.Server{ReadHeaderTimeout: 5 * time.Second, Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodConnect {
			upstream, err := dial(ctx, "tcp", r.Host)
			if err != nil {
				http.Error(w, "unavailable", 502)
				return
			}
			defer upstream.Close()
			client, buf, err := w.(http.Hijacker).Hijack()
			if err != nil {
				return
			}
			defer client.Close()
			deadline, _ := ctx.Deadline()
			client.SetDeadline(deadline)
			upstream.SetDeadline(deadline)
			buf.WriteString("HTTP/1.1 200 Connection Established\r\n\r\n")
			if buf.Flush() != nil {
				return
			}
			go io.Copy(upstream, buf)
			io.Copy(client, upstream)
			return
		}
		if r.URL.Scheme != "http" || r.Method != "GET" {
			http.Error(w, "unsupported", 400)
			return
		}
		r = r.Clone(ctx)
		r.RequestURI = ""
		r.Header.Del("Proxy-Connection")
		res, err := transport.RoundTrip(r)
		if err != nil {
			http.Error(w, "unavailable", 502)
			return
		}
		defer res.Body.Close()
		for k, v := range res.Header {
			w.Header()[k] = v
		}
		w.WriteHeader(res.StatusCode)
		io.Copy(w, res.Body)
	})}
	go proxy.Serve(listener)
	defer proxy.Close()
	opts := append([]chromedp.ExecAllocatorOption{}, chromedp.DefaultExecAllocatorOptions[:]...)
	if path := browserPath(); path != "" {
		opts = append(opts, chromedp.ExecPath(path))
	}
	opts = append(opts, chromedp.ProxyServer("http://"+listener.Addr().String()), chromedp.Flag("proxy-bypass-list", "<-loopback>"), chromedp.Flag("disable-quic", true), chromedp.Flag("force-webrtc-ip-handling-policy", "disable_non_proxied_udp"))
	alloc, stop := chromedp.NewExecAllocator(ctx, opts...)
	defer stop()
	tab, closeTab := chromedp.NewContext(alloc)
	defer closeTab()
	var screenshot []byte
	response, err := chromedp.RunResponse(tab, chromedp.EmulateViewport(1200, 1400), chromedp.Navigate(target))
	if err != nil {
		return empty, err
	}
	if response == nil {
		return empty, fmt.Errorf("状态页未正常响应")
	}
	if response.Status >= 400 {
		return empty, fmt.Errorf("状态页返回 HTTP %d", response.Status)
	}
	var size struct{ Width, Height int }
	// Give client-rendered status tables a bounded window to settle. Measure
	// the document without resizing the viewport (which changes vh layouts).
	err = chromedp.Run(tab, chromedp.WaitReady("body"), chromedp.Evaluate(`new Promise(resolve => {
		let timer;
		const finish = () => { observer.disconnect(); clearTimeout(timer); clearTimeout(limit);
			const root = document.documentElement;
			resolve({Width: root.scrollWidth, Height: root.scrollHeight}); };
		const quiet = () => { clearTimeout(timer); timer = setTimeout(finish, 750); };
		const observer = new MutationObserver(quiet);
		observer.observe(document.body, {childList:true, subtree:true, attributes:true, characterData:true});
		const limit = setTimeout(finish, 4000); quiet();
	})`, &size, func(p *runtime.EvaluateParams) *runtime.EvaluateParams { return p.WithAwaitPromise(true) }))
	if err != nil {
		return empty, err
	}
	if size.Width <= 0 || size.Height <= 0 {
		return empty, fmt.Errorf("状态页尺寸 %d × %d 超出截图范围", size.Width, size.Height)
	}
	// Bound output pixels while retaining the bottom of unusually long pages.
	scale := math.Min(1, math.Min(1600/float64(size.Width), 10000/float64(size.Height)))
	err = chromedp.Run(tab, chromedp.ActionFunc(func(ctx context.Context) error {
		var err error
		screenshot, err = page.CaptureScreenshot().WithCaptureBeyondViewport(true).WithClip(&page.Viewport{Width: float64(size.Width), Height: float64(size.Height), Scale: scale}).Do(ctx)
		return err
	}))
	if err != nil {
		return empty, err
	}
	if len(screenshot) > 8<<20 {
		return empty, errors.New("状态页截图超过 8 MB")
	}
	cfg, err := png.DecodeConfig(bytes.NewReader(screenshot))
	if err != nil {
		return empty, err
	}
	key, err := a.images.Save(ctx, "status.png", screenshot)
	if err != nil {
		return empty, err
	}
	return domain.ContentPart{Type: "image", Filename: "status.png", ContentType: "image/png", StorageKey: key, Width: cfg.Width, Height: cfg.Height, SizeBytes: int64(len(screenshot))}, nil
}

var directStatusRequest = regexp.MustCompile(`(?i)状态图|请求成功率|状态页.{0,8}(截图|截屏)|(?:截图|截屏).{0,8}状态页|(分组|服务|全站|网站|网页).{0,10}(状态|可用吗|正常吗|挂了|成功率)|(?:group|service|site|system) status|status (?:of|for) (?:the )?(?:group|service|site|system)`)

func explicitStatusQuestion(question string) bool { return directStatusRequest.MatchString(question) }
func (a *App) attachStatusPage(ctx context.Context, rt *turnRuntime, group string) error {
	var part domain.ContentPart
	var err error
	if rt.settings.LiveData.StatusImageStyle == "page" {
		if rt.statusImage != nil {
			return nil
		}
		part, err = a.statusPageImage(ctx, rt.settings.LiveData.StatusPageURL)
	} else {
		status, fetchErr := a.turnStatus(ctx, rt)
		if fetchErr != nil {
			return fetchErr
		}
		if status.ServiceStatus["hour"] == nil && len(status.Monitors) == 0 {
			return errors.New("实时服务状态不可用")
		}
		card := statuscard.FromSnapshot(*status, rt.question, group)
		focus := ""
		if card.Focus != nil {
			focus = *card.Focus
		}
		if rt.statusImage != nil && (focus == "" || focus == rt.statusFocus) {
			return nil
		}
		part, err = a.statusCardImage(ctx, rt, card)
		if err == nil {
			rt.statusFocus = focus
		}
	}
	if err != nil {
		return err
	}
	rt.statusImage = &part
	return nil
}

func validateStatusURL(target string) error {
	u, err := url.Parse(target)
	if err != nil {
		return err
	}
	u.RawQuery = ""
	u.Fragment = ""
	_, err = scheduledPageBase(u.String())
	return err
}
