package main

import (
	"bytes"
	"io"
	"log"
	"net/url"
	"strings"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/widget"
	"github.com/faiface/beep"
	"github.com/faiface/beep/mp3"
	"github.com/faiface/beep/speaker"

	"apple-store-helper/notify"
	"apple-store-helper/services"
	"apple-store-helper/theme"
	"apple-store-helper/view"
)

// alertInStock 有货时打开配置的购买入口，并通过弹窗、系统通知、提示音提醒。手机推送与历史记录由监听服务完成
func alertInStock(hits []services.ListenItem, options services.PurchaseOptions) {
	if !services.Listen.AutoOrderHandles(hits) {
		for _, link := range services.PurchaseLinks(hits, options) {
			openBrowser(link)
		}
	}

	title, msg := services.HitMessage(hits)
	// 窗口可能收在托盘里，先显示出来
	showMainWindow()
	if services.Listen.AutoOrderHandles(hits) {
		showHitDialog(title, msg+"\n\n本次命中已交给一次性 API 下单流程；请勿同时操作该购物袋。结果会单独提示。")
	} else {
		showHitDialog(title, msg+"\n\n已尝试在浏览器打开所选购买入口，请核对型号、门店和取货时间后自行下单。")
	}
	notify.Send(title, msg)
	go alertMp3()
}

// showHitDialog 有货弹窗：左对齐，按门店分组；内容多时可滚动
func showHitDialog(title, msg string) {
	label := widget.NewLabel(msg)
	label.Wrapping = fyne.TextWrapWord
	content := fyne.CanvasObject(label)
	if strings.Count(msg, "\n") > 12 {
		scroll := container.NewVScroll(label)
		scroll.SetMinSize(fyne.NewSize(520, 360))
		content = scroll
	}
	d := dialog.NewCustom(title, "知道了", content, view.Window)
	d.Resize(fyne.NewSize(560, d.MinSize().Height))
	d.Show()
}

func openBrowser(link string) {
	parse, err := url.Parse(link)
	if err != nil {
		dialog.ShowError(err, view.Window)
		return
	}
	if err := view.App.OpenURL(parse); err != nil {
		dialog.ShowError(err, view.Window)
	}
}

// alertMp3 播放有货提示音
func alertMp3() {
	streamer, _, err := mp3.Decode(io.NopCloser(bytes.NewReader(theme.Mp3().Content())))
	if err != nil {
		log.Println("提示音解码失败:", err)
		return
	}
	defer streamer.Close()

	done := make(chan bool)
	speaker.Play(beep.Seq(streamer, beep.Callback(func() {
		done <- true
	})))
	<-done
}
