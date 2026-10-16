package main

import (
	"fmt"
	"strings"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/widget"

	"apple-store-helper/services"
	"apple-store-helper/view"
)

// displayZone 与监听列表一致，按北京时间显示
var displayZone = func() *time.Location {
	if loc, err := time.LoadLocation("Asia/Shanghai"); err == nil {
		return loc
	}
	return time.FixedZone("CST", 8*3600)
}()

// showHistory 有货记录：门店命中排行与最近的记录
func showHistory() {
	entries, err := services.LoadHistory()
	if err != nil {
		dialog.ShowError(fmt.Errorf("读取有货记录失败: %w", err), view.Window)
		return
	}
	if len(entries) == 0 {
		dialog.ShowInformation("有货记录", "还没有记录。检测到有货后，时间、门店和型号会记在这里，方便回头看哪家店更容易放货。", view.Window)
		return
	}

	var rank strings.Builder
	for i, r := range services.RankStores(entries) {
		if i >= 10 {
			break
		}
		fmt.Fprintf(&rank, "%2d. %s %s  %d 次（最近 %s）\n", i+1, r.Area, r.Store, r.Count, r.Last.In(displayZone).Format("01-02 15:04"))
	}

	var recent strings.Builder
	for i, e := range entries {
		if i >= 200 {
			break
		}
		fmt.Fprintf(&recent, "%s  %s  %s  %s\n", e.Time.In(displayZone).Format("2006-01-02 15:04:05"), e.Area, e.Store, e.Product)
	}

	content := container.NewBorder(
		container.NewVBox(
			widget.NewLabelWithStyle(fmt.Sprintf("门店排行（共 %d 条记录）", len(entries)), fyne.TextAlignLeading, fyne.TextStyle{Bold: true}),
			widget.NewLabel(strings.TrimRight(rank.String(), "\n")),
			widget.NewSeparator(),
			widget.NewLabelWithStyle("最近记录", fyne.TextAlignLeading, fyne.TextStyle{Bold: true}),
		),
		nil, nil, nil,
		container.NewVScroll(widget.NewLabel(strings.TrimRight(recent.String(), "\n"))),
	)

	var d dialog.Dialog
	clear := widget.NewButton("清空记录", func() {
		dialog.ShowConfirm("清空有货记录", "确定删除全部有货记录吗？", func(ok bool) {
			if !ok {
				return
			}
			if err := services.ClearHistory(); err != nil {
				dialog.ShowError(err, view.Window)
				return
			}
			d.Hide()
		}, view.Window)
	})
	d = dialog.NewCustom("有货记录", "关闭", container.NewBorder(nil, container.NewHBox(clear), nil, nil, content), view.Window)
	d.Resize(fyne.NewSize(760, 560))
	d.Show()
}
