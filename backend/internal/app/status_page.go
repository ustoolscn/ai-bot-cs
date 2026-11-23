package app

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"regexp"
	"runtime"
	"time"

	"ai-bot/backend/internal/domain"
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
	return d.DialContext(ctx, network, net.JoinHostPort(ips[0].IP.String(), port))
}

func (a *App) statusPageImage(ctx context.Context, target string) (domain.ContentPart, error) {
	return a.renderStatusPage(ctx, target, publicDial)
}

func (a *App) renderStatusPage(ctx context.Context, target string, dial func(context.Context, string, string) (net.Conn, error)) (domain.ContentPart, error) {
	var empty domain.ContentPart
	if runtime.GOOS == "linux" && os.Getuid() == 0 {
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
	var png []byte
	response, err := chromedp.RunResponse(tab, chromedp.EmulateViewport(1200, 1400), chromedp.Navigate(target))
	if err != nil {
		return empty, err
	}
	if response == nil || response.Status >= 400 {
		return empty, fmt.Errorf("状态页未正常响应")
	}
	if err = chromedp.Run(tab, chromedp.WaitVisible("body"), chromedp.CaptureScreenshot(&png)); err != nil {
		return empty, err
	}
	key, err := a.images.Save(ctx, "status.png", png)
	if err != nil {
		return empty, err
	}
	return domain.ContentPart{Type: "image", Filename: "status.png", ContentType: "image/png", StorageKey: key, Width: 1200, Height: 1400, SizeBytes: int64(len(png))}, nil
}

var directStatusRequest = regexp.MustCompile(`(?i)(分组|服务|全站|网站).{0,10}(状态|可用吗|正常吗|挂了|成功率)|(?:group|service|site|system) status|status (?:of|for) (?:the )?(?:group|service|site|system)`)

func explicitStatusQuestion(question string) bool { return directStatusRequest.MatchString(question) }
func (a *App) attachStatusPage(ctx context.Context, rt *turnRuntime) error {
	if rt.statusImage != nil {
		return nil
	}
	part, err := a.statusPageImage(ctx, rt.settings.LiveData.StatusPageURL)
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
