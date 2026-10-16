package main

// 状态栏与警示条，参考 dashug/apple-store-helper。
// 状态栏显示上次检查时间、下次检查倒计时和本轮各状态数量，开售时能确认程序还在正常查询；
// 警示条在整轮查询全部失败时出现，避免把「查不到」误当成「没货」。

import (
	"fmt"
	"image/color"
	"sort"
	"strings"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"

	"apple-store-helper/services"
	"apple-store-helper/view"
)

// statusBar 底部状态栏，每 0.5 秒刷新一次倒计时
type statusBar struct {
	label *widget.Label
}

func newStatusBar() *statusBar {
	b := &statusBar{label: widget.NewLabel("")}
	b.label.Truncation = fyne.TextTruncateEllipsis
	b.refresh()
	go func() {
		for range time.Tick(500 * time.Millisecond) {
			b.refresh()
		}
	}()
	return b
}

func (b *statusBar) refresh() {
	b.label.SetText(statusText(services.Listen.GetStatus(), services.Listen.Stats(), time.Now()))
}

// statusText 状态栏文字
func statusText(status string, stats services.RoundStats, now time.Time) string {
	var parts []string

	if stats.LastCheck.IsZero() {
		parts = append(parts, "尚未查询")
	} else {
		parts = append(parts, "上次检查 "+stats.LastCheck.In(displayZone).Format("15:04:05"))
	}

	switch {
	case status != services.Running:
		parts = append(parts, "已暂停")
	case !stats.NextCheck.IsZero():
		wait := stats.NextCheck.Sub(now)
		if wait < 0 {
			wait = 0
		}
		parts = append(parts, fmt.Sprintf("下次 %.1f 秒后", wait.Seconds()))
	default:
		parts = append(parts, "查询中…")
	}

	if stats.Checked > 0 {
		parts = append(parts, fmt.Sprintf("本轮 %d 项：%s", stats.Checked, countsText(stats.Counts)))
	}
	return strings.Join(parts, " · ")
}

// countsText 按固定顺序列出各状态数量，如「有货 1，无货 14，查询失败 1」
func countsText(counts map[string]int) string {
	order := []string{services.StatusInStock, services.StatusOutStock, services.StatusFailed, services.StatusStoreClosed, services.StatusWait}
	rank := map[string]int{}
	for i, s := range order {
		rank[s] = i
	}
	keys := make([]string, 0, len(counts))
	for k := range counts {
		keys = append(keys, k)
	}
	sort.Slice(keys, func(i, j int) bool {
		ri, iok := rank[keys[i]]
		rj, jok := rank[keys[j]]
		if iok != jok {
			return iok
		}
		if ri != rj {
			return ri < rj
		}
		return keys[i] < keys[j]
	})
	var parts []string
	for _, k := range keys {
		parts = append(parts, fmt.Sprintf("%s %d", k, counts[k]))
	}
	return strings.Join(parts, "，")
}

// warningBanner 整轮查询全部失败时显示的警示条
type warningBanner struct {
	box   *fyne.Container
	bg    *canvas.Rectangle
	label *widget.Label
}

func newWarningBanner() *warningBanner {
	b := &warningBanner{
		bg:    canvas.NewRectangle(color.Transparent),
		label: widget.NewLabel(""),
	}
	b.label.Wrapping = fyne.TextWrapWord
	b.bg.CornerRadius = 6
	b.box = container.NewStack(b.bg, container.NewPadded(b.label))
	b.box.Hide()
	return b
}

// Update 根据最近一轮统计显示或隐藏
func (b *warningBanner) Update(stats services.RoundStats) {
	text, level := bannerText(stats)
	if text == "" {
		if b.box.Visible() {
			b.box.Hide()
			relayout()
		}
		return
	}
	b.label.SetText(text)
	switch level {
	case bannerError:
		b.bg.FillColor = withAlpha(theme.Color(theme.ColorNameError), 0x40)
	default:
		b.bg.FillColor = withAlpha(theme.Color(theme.ColorNamePrimary), 0x30)
	}
	b.bg.Refresh()
	if !b.box.Visible() {
		b.box.Show()
		relayout()
	}
}

// relayout 警示条显示或隐藏后高度变化，外层的 Border 布局不会自动重排，需要刷新整个窗口内容
func relayout() {
	if content := view.Window.Content(); content != nil {
		content.Refresh()
	}
}

type bannerLevel int

const (
	bannerInfo bannerLevel = iota
	bannerError
)

// bannerText 警示条文字。商店维护是预期内的，用提示色；其他原因的全部失败用错误色
func bannerText(stats services.RoundStats) (string, bannerLevel) {
	switch {
	case !stats.AllFailed:
		return "", bannerInfo
	case stats.AllClosed:
		return "Apple 商店维护中（开售前会切到预购封面页），商店开放后会按设定频率自动查到。", bannerInfo
	default:
		reason := stats.TopReason
		if reason == "" {
			reason = "原因未知"
		}
		text := fmt.Sprintf("本轮全部查询失败（%s），当前结果不可信。", reason)
		if stats.FailedRounds > 1 {
			text = fmt.Sprintf("已连续 %d 轮全部查询失败（%s），当前结果不可信。", stats.FailedRounds, reason)
		}
		return text + "可能被 Apple 限流，建议调低查询频率；也可能是网络问题。", bannerError
	}
}

func withAlpha(c color.Color, alpha uint8) color.Color {
	r, g, b, _ := c.RGBA()
	return color.NRGBA{R: uint8(r >> 8), G: uint8(g >> 8), B: uint8(b >> 8), A: alpha}
}
