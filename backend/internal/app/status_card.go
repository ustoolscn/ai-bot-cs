package app

import (
	"bytes"
	"context"
	"encoding/base64"
	"fmt"
	"image/png"
	"os"
	"runtime"
	"slices"
	"strings"
	"time"
	"unicode/utf8"

	"ai-bot/backend/internal/domain"
	"ai-bot/backend/internal/statuscard"
	"github.com/chromedp/cdproto/emulation"
	"github.com/chromedp/cdproto/network"
	"github.com/chromedp/chromedp"
)

func (a *App) statusCardImage(ctx context.Context, rt *turnRuntime, c statuscard.Card) (domain.ContentPart, error) {
	facts := c.Facts()
	c.Summary = facts[0] + "。"
	var summary struct {
		Summary string `json:"summary"`
	}
	summaryCtx, cancel := context.WithTimeout(ctx, 8*time.Second)
	defer cancel()
	err := a.auxJSON(summaryCtx, rt.settings, rt.chatProfileID, rt.runID, rt.messageID, "status_card_summary", "根据给定事实写一句不超过50字的中文状态简报。原样选用一至两条事实，以中文逗号连接，句号结尾。优先用户关注的分组。只输出 JSON 对象，字段 summary。", jsonString(facts), &summary)
	if err == nil && utf8.RuneCountInString(summary.Summary) <= 50 {
		parts := strings.Split(strings.TrimSuffix(summary.Summary, "。"), "，")
		if len(parts) <= 2 && slices.Contains(facts, parts[0]) && slices.Contains(facts, parts[len(parts)-1]) && utf8.RuneCountInString(strings.Join(parts, "，")) < 50 {
			c.Summary = strings.Join(parts, "，") + "。"
		}
	}
	img, err := renderStatusCard(ctx, rt.settings.LiveData.StatusImageStyle, c)
	if err != nil {
		return domain.ContentPart{}, err
	}
	cfg, err := png.DecodeConfig(bytes.NewReader(img))
	if err != nil {
		return domain.ContentPart{}, err
	}
	key, err := a.images.Save(ctx, "status.png", img)
	return domain.ContentPart{Type: "image", Filename: "status.png", ContentType: "image/png", StorageKey: key, Width: cfg.Width, Height: cfg.Height, SizeBytes: int64(len(img))}, err
}

func renderStatusCard(ctx context.Context, style string, c statuscard.Card) ([]byte, error) {
	doc, err := statuscard.Document(style, c)
	if err != nil {
		return nil, err
	}
	if runtime.GOOS == "linux" && os.Getuid() == 0 {
		return nil, fmt.Errorf("请使用普通用户运行截图服务")
	}
	path := browserPath()
	if path == "" {
		return nil, fmt.Errorf("未找到 Chromium 或 Edge")
	}
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	select {
	case renderSlots <- struct{}{}:
		defer func() { <-renderSlots }()
	case <-ctx.Done():
		return nil, ctx.Err()
	}
	opts := append([]chromedp.ExecAllocatorOption{}, chromedp.DefaultExecAllocatorOptions[:]...)
	opts = append(opts, chromedp.ExecPath(path), chromedp.Flag("host-resolver-rules", "MAP * ~NOTFOUND"))
	alloc, stop := chromedp.NewExecAllocator(ctx, opts...)
	defer stop()
	tab, closeTab := chromedp.NewContext(alloc)
	defer closeTab()
	var img []byte
	var height int
	err = chromedp.Run(tab, network.Enable(), network.EmulateNetworkConditions(true, 0, 0, 0), emulation.SetTimezoneOverride("Asia/Shanghai"),
		chromedp.EmulateViewport(800, 600, chromedp.EmulateScale(2)), chromedp.Navigate("data:text/html;charset=utf-8;base64,"+base64.StdEncoding.EncodeToString([]byte(doc))),
		chromedp.WaitVisible("body[data-ready='1'] #card", chromedp.ByQuery), chromedp.Evaluate("document.getElementById('card').offsetHeight", &height))
	if err != nil {
		return nil, err
	}
	if height > 10000 {
		return nil, fmt.Errorf("状态卡片过长")
	}
	err = chromedp.Run(tab, chromedp.Screenshot("#card", &img, chromedp.ByQuery))
	if len(img) > 8<<20 {
		return nil, fmt.Errorf("状态卡片超过 8 MB")
	}
	return img, err
}
