package main

import (
	"context"
	"errors"
	"fmt"
	"time"

	"apple-store-helper/checkout"
	"apple-store-helper/notify"
	"apple-store-helper/services"
	"apple-store-helper/view"
	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/widget"
)

var autoOrderMode *widget.Select
var autoOrderConfig *widget.Entry

func currentAutoOrderOptions() services.AutoOrderOptions {
	options := services.AutoOrderOptions{Mode: services.AutoOrderOff}
	if autoOrderConfig != nil {
		options.ConfigPath = autoOrderConfig.Text
	}
	if autoOrderMode != nil {
		switch autoOrderMode.SelectedIndex() {
		case 1:
			options.Mode = checkout.ModeReview
		case 2:
			options.Mode = checkout.ModeSubmit
		}
	}
	return options
}

func createAutoOrderControls() fyne.CanvasObject {
	autoOrderConfig = widget.NewEntry()
	autoOrderConfig.SetPlaceHolder("私有 API 下单配置 JSON 的完整路径；不是 Cookie 内容")
	autoOrderConfig.OnChanged = func(string) {
		autoOrderDraftChanged()
		refreshPickupContactStatus()
		saveSettings()
	}
	autoOrderConfig.Hide()
	autoOrderMode = widget.NewSelect([]string{"仅提醒（默认）", "API 演练到核对页", "API 自动提交订单（不付款）"}, func(string) {
		autoOrderDraftChanged()
		if currentAutoOrderOptions().Mode == services.AutoOrderOff {
			autoOrderConfig.Hide()
		} else {
			autoOrderConfig.Show()
		}
		saveSettings()
	})
	autoOrderMode.SetSelectedIndex(0)
	cancel := widget.NewButton("停止本次 API 尝试", func() { services.Listen.CancelAutoOrder() })
	status := widget.NewButton("查看本次 API 结果", func() {
		if event, ok := services.Listen.AutoOrderLastResult(); ok {
			alertAutoOrder(event)
		} else {
			dialog.ShowInformation("API 下单状态", "本次尚无结束结果；提交结果仍以 Apple 订单记录为准。", view.Window)
		}
	})
	pickupContactStatus = widget.NewLabel("提货资料：尚未保存")
	contact := widget.NewButton("填写／编辑提货资料", showPickupContactEditor)
	refreshPickupContactStatus()
	heading := widget.NewLabel("API 自提下单（实验性，macOS/Linux，中国大陆 Duo 256/512GB）")
	heading.Wrapping = fyne.TextWrapWord
	return container.NewVBox(heading,
		autoOrderMode, autoOrderConfig, contact, pickupContactStatus, container.NewHBox(cancel, status))
}

func autoOrderDraftChanged() {
	// A displayed reminder-only/new draft must never leave an older submit arm
	// active. Editing policy also pauses inventory until a new explicit Start.
	services.Listen.CancelAutoOrder()
	services.Listen.SetStatus(services.Pause)
}

func loadAutoOrderOptions(options services.AutoOrderOptions) {
	if autoOrderConfig != nil {
		autoOrderConfig.SetText(options.ConfigPath)
	}
	// A saved path is not permission to resume submitting orders after restart.
	if autoOrderMode != nil {
		autoOrderMode.SetSelectedIndex(0)
	}
}

func beginListening(options services.AutoOrderOptions) {
	if err := services.Listen.ConfigureAutoOrder(options); err != nil {
		dialog.ShowError(err, view.Window)
		return
	}
	services.Listen.SetStatus(services.Running)
}

func confirmAutoOrderStart(options services.AutoOrderOptions) {
	config, err := checkout.LoadConfig(options.ConfigPath)
	if err != nil {
		dialog.ShowError(errors.New("API 下单配置无效，请检查私有配置文件"), view.Window)
		return
	}
	message := fmt.Sprintf("开始监听后，指定商品有货时会通过 API 创建真实订单。\n\n型号：%s\n门店：%v\n数量：1\n总价上限：¥%d.%02d\n支付类型：%s（程序不付款）\n\n每次开始最多尝试一次；提交结果未知时必须先核对 Apple 订单记录。是否武装本次自动提交？",
		config.SKU, config.StoreIDs, config.MaxTotalCents/100, config.MaxTotalCents%100, config.Payment)
	dialog.ShowConfirm("确认武装 API 自动提交", message, func(confirmed bool) {
		if confirmed {
			beginListening(options)
		}
	}, view.Window)
}

func alertAutoOrder(event services.AutoOrderEvent) {
	message := fmt.Sprintf("%s\n型号：%s\n门店：%s\n阶段：%s", event.Message, event.SKU, event.StoreID, event.Stage)
	showMainWindow()
	dialog.ShowInformation("API 下单状态", message, view.Window)
	notify.Send("API 下单状态", message)
}

func stopOrderAndListening() {
	services.Listen.CancelAutoOrder()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_ = services.Listen.WaitAutoOrder(ctx)
	services.Listen.SetStatus(services.Pause)
}
