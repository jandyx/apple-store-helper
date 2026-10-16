package main

import (
	"apple-store-helper/common"
	"apple-store-helper/services"
	"apple-store-helper/theme"
	"apple-store-helper/view"
	"errors"
	"fmt"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/app"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/layout"
	"fyne.io/fyne/v2/widget"
	"github.com/faiface/beep"
	"github.com/faiface/beep/speaker"
)

// barkEnabled Bark 推送开关。官方服务 api.day.app 当前不可用，先隐藏相关界面，恢复后改为 true 即可
const barkEnabled = false

// main 主函数 (Main function)
func main() {
	initMP3Player()
	initFyneApp()

	// 默认地区 (Default Area)
	defaultArea := services.Listen.Area.Title

	// 门店选择器 (Store Selector)
	storeWidget := newStorePicker(services.Store.ByAreaTitleForOptions(defaultArea))

	// 型号选择器 (Product Selector)
	productWidget := widget.NewSelect(services.Product.ByAreaTitleForOptions(defaultArea), nil)
	productWidget.PlaceHolder = "请选择 iPhone 型号"

	// Bark 通知输入框
	barkWidget := widget.NewEntry()
	barkWidget.SetPlaceHolder("https://api.day.app/你的BarkKey")

	// 查询频率选择器 (Polling Interval Selector)
	// 可从下拉选择，也可直接输入秒数（如 1.5）或毫秒（如 800ms）
	intervalWidget := widget.NewSelectEntry(services.IntervalLabels())
	intervalWidget.SetPlaceHolder("选择或输入秒数，如 1.5")
	intervalWidget.OnChanged = func(value string) {
		ms, ok := services.ParseInterval(value)
		if !ok {
			return
		}
		services.Listen.IntervalMs.Set(ms)

		// 已有配置文件时立即落盘；没有时等“添加”一起保存，避免生成只含频率的配置导致地区为空
		if settings, err := services.LoadSettings(); err == nil && settings.ListenIntervalMs != ms {
			settings.ListenIntervalMs = ms
			settings.ListenInterval = 0
			services.SaveSettings(settings)
		}
	}

	// 地区选择器 (Area Selector)
	areaWidget := widget.NewRadioGroup(services.Area.ForOptions(), func(value string) {
		// 防止空值或无效值导致崩溃
		if value == "" {
			return
		}

		storeWidget.SetOptions(services.Store.ByAreaTitleForOptions(value))

		productWidget.Options = services.Product.ByAreaTitleForOptions(value)
		productWidget.ClearSelected()

		services.Listen.Area = services.Area.GetArea(value)
		services.Listen.Clean()
	})

	areaWidget.Horizontal = true

	help := `1. 在 Apple 官网将需要购买的型号加入购物车
2. 选择地区、门店和型号，点击“添加”按钮，将需要监听的型号添加到监听列表
3. 点击“开始”按钮开始监听，检测到有货时会自动打开购物车页面
`

	loadUserSettingsCache(areaWidget, storeWidget, productWidget, barkWidget, intervalWidget)

	// 表单放在同一个 FormLayout 里，标签列宽度一致
	formItems := []fyne.CanvasObject{
		widget.NewLabel("选择地区:"), areaWidget,
		widget.NewLabel("选择门店:"), storeWidget.Widget(),
		widget.NewLabel("选择型号:"), productWidget,
	}
	if barkEnabled {
		formItems = append(formItems, widget.NewLabel("Bark 通知地址:"), barkWidget)
	}
	formItems = append(formItems, widget.NewLabel("查询频率:"), intervalWidget)

	top := container.NewVBox(
		widget.NewLabel(help),
		container.New(layout.NewFormLayout(), formItems...),
		container.NewBorder(nil, nil,
			createActionButtons(areaWidget, storeWidget, productWidget, barkWidget, intervalWidget),
			createControlButtons(),
		),
		widget.NewSeparator(),
		widget.NewLabelWithStyle("监听列表", fyne.TextAlignLeading, fyne.TextStyle{Bold: true}),
	)

	// 初始化 GUI 窗口内容 (Initialize GUI)：监听列表占满剩余空间，四周留白
	content := container.NewBorder(top, createVersionLabel(), nil, nil, container.NewVScroll(services.Listen.Logs))
	view.Window.SetContent(container.New(layout.NewCustomPaddedLayout(12, 8, 16, 16), content))

	view.Window.Resize(fyne.NewSize(1000, 800))
	view.Window.CenterOnScreen()
	services.Listen.Run()
	view.Window.ShowAndRun()
}

// initMP3Player 初始化 MP3 播放器 (Initialize MP3 player)
func initMP3Player() {
	SampleRate := beep.SampleRate(44100)
	speaker.Init(SampleRate, SampleRate.N(time.Second/10))
}

// initFyneApp 初始化 Fyne 应用 (Initialize Fyne App)
func initFyneApp() {
	view.App = app.NewWithID("apple-store-helper")
	view.App.Settings().SetTheme(&theme.MyTheme{})
	view.Window = view.App.NewWindow("Apple Store Helper")
}

// 加载用户设置缓存 (Load user settings cache)
func loadUserSettingsCache(areaWidget *widget.RadioGroup, storeWidget *storePicker, productWidget *widget.Select, barkNotifyWidget *widget.Entry, intervalWidget *widget.SelectEntry) {
	settings, err := services.LoadSettings()

	interval := services.DefaultIntervalMs
	if err == nil && settings.IntervalMs() > 0 {
		interval = settings.IntervalMs()
	}
	services.Listen.IntervalMs.Set(interval)
	intervalWidget.SetText(services.IntervalLabel(interval))

	if err == nil {
		areaWidget.SetSelected(settings.SelectedArea)
		storeWidget.SetSelected(settings.Stores())
		productWidget.SetSelected(settings.SelectedProduct)
		services.Listen.SetListenItems(settings.ListenItems)
		barkNotifyWidget.SetText(settings.BarkNotifyUrl)
	} else {
		areaWidget.SetSelected(services.Listen.Area.Title)
	}
}

// 创建动作按钮 (Create action buttons)
func createActionButtons(areaWidget *widget.RadioGroup, storeWidget *storePicker, productWidget *widget.Select, barkNotifyWidget *widget.Entry, intervalWidget *widget.SelectEntry) *fyne.Container {
	barkTestButton := widget.NewButton("测试 Bark 通知", func() {
		services.Listen.BarkNotifyUrl = barkNotifyWidget.Text
		go func() {
			err := services.Listen.SendPushNotificationByBark("有货提醒（测试）", "此为测试提醒，点击通知将跳转到相关链接", services.Listen.BagURL())
			if err != nil {
				dialog.ShowError(fmt.Errorf("Bark 通知发送失败: %w", err), view.Window)
				return
			}
			dialog.ShowInformation("Bark", "测试通知已发送，请查看手机", view.Window)
		}()
	})
	if !barkEnabled {
		barkTestButton.Hide()
	}

	return container.NewHBox(
		widget.NewButton("添加", func() {
			if len(storeWidget.Selected()) == 0 || productWidget.Selected == "" {
				dialog.ShowError(errors.New("请选择门店和型号"), view.Window)
			} else {
				barkNotifyUrl := ""
				if barkEnabled {
					barkNotifyUrl = barkNotifyWidget.Text
				}
				for _, store := range storeWidget.Selected() {
					services.Listen.Add(areaWidget.Selected, store, productWidget.Selected, barkNotifyUrl)
				}
				services.SaveSettings(services.UserSettings{
					SelectedArea:     areaWidget.Selected,
					SelectedStores:   storeWidget.Selected(),
					SelectedProduct:  productWidget.Selected,
					BarkNotifyUrl:    barkNotifyWidget.Text,
					ListenIntervalMs: services.ParseIntervalLabel(intervalWidget.Text),
					ListenItems:      services.Listen.GetListenItems(),
				})
			}
		}),
		widget.NewButton("清空", func() {
			services.Listen.Clean()
			services.ClearSettings()
		}),
		widget.NewButton("试听(有货提示音)", func() {
			go services.Listen.AlertMp3()
		}),
		widget.NewButton("测试打开购物车", func() {
			services.Listen.OpenBag()
		}),
		barkTestButton,
	)
}

// 创建控制按钮 (Create control buttons)
func createControlButtons() *fyne.Container {
	return container.NewHBox(
		widget.NewButton("开始", func() {
			services.Listen.Status.Set(services.Running)
		}),
		widget.NewButton("暂停", func() {
			services.Listen.Status.Set(services.Pause)
		}),
		container.NewCenter(widget.NewLabel("状态:")),
		container.NewCenter(widget.NewLabelWithData(services.Listen.Status)),
	)
}

// createVersionLabel 创建版本标签 (Create version label)
func createVersionLabel() *fyne.Container {
	return container.NewHBox(
		layout.NewSpacer(),
		widget.NewLabel("version: "+common.VERSION),
	)
}
