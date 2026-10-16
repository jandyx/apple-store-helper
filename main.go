package main

import (
	"apple-store-helper/common"
	"apple-store-helper/model"
	"apple-store-helper/notify"
	"apple-store-helper/services"
	"apple-store-helper/theme"
	"apple-store-helper/view"
	"errors"
	"fmt"
	"log"
	"os/exec"
	"runtime"
	"strings"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/app"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/data/binding"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/layout"
	"fyne.io/fyne/v2/widget"
	"github.com/faiface/beep"
	"github.com/faiface/beep/speaker"
)

// 主界面上需要在多处读写的控件
var (
	areaWidget     *radioRow
	storeWidget    *multiPicker
	productWidget  *multiPicker
	pushWidget     *widget.Entry
	intervalWidget *widget.SelectEntry
	keepGoingCheck *widget.Check
	// settingsLoaded 配置恢复完成前不落盘，避免恢复过程中的 OnChanged 把半成品写回文件
	settingsLoaded bool
)

// main 主函数 (Main function)
func main() {
	if path, err := services.SetupLogging(true); err != nil {
		log.Println("日志文件初始化失败:", err)
	} else {
		log.Println("启动", common.VERSION, "日志:", path)
	}
	initMP3Player()
	initFyneApp()
	notify.Setup()
	setupSystemTray()

	// 默认地区 (Default Area)
	defaultArea := model.Areas[0].Title

	// 门店选择器 (Store Selector)
	storeWidget = newMultiPicker("选择门店", "请选择自提门店（可多选）", "家", services.Store.ByAreaTitleForOptions(defaultArea))

	// 型号选择器 (Product Selector)
	productWidget = newMultiPicker("选择型号", "请选择 iPhone 型号（可多选）", "个", services.Product.ByAreaTitleForOptions(defaultArea))
	productWidget.display = services.TitleName

	// 手机推送地址，每行一个，按域名自动识别渠道
	pushWidget = widget.NewMultiLineEntry()
	pushWidget.SetMinRowsVisible(2)
	pushWidget.SetPlaceHolder("选填，每行一个：Bark / Server酱 / 企业微信机器人 / Telegram / Webhook 地址")
	pushWidget.OnChanged = func(value string) {
		services.Listen.SetPushUrls(value)
		saveSettings()
	}

	// 查询频率选择器 (Polling Interval Selector)
	// 可从下拉选择，也可直接输入秒数（如 1.5）或毫秒（如 800ms）
	intervalWidget = widget.NewSelectEntry(services.IntervalLabels())
	intervalWidget.SetPlaceHolder("选择或输入秒数，如 1.5")
	intervalWidget.OnChanged = func(value string) {
		ms, ok := services.ParseInterval(value)
		if !ok {
			return
		}
		services.Listen.SetIntervalMs(ms)
		saveSettings()
	}

	// 地区选择器 (Area Selector)。切换地区只换门店和型号选项，监听列表保留，多个地区可同时监听；
	// 各地区的勾选分别记住，切回来时恢复
	areaSelections := newSelectionMemory()
	currentArea := ""
	areaWidget = newRadioRow(services.Area.ForOptions(), func(value string) {
		// 防止空值或无效值导致崩溃
		if value == "" {
			return
		}
		areaSelections.save(currentArea, storeWidget.Selected(), productWidget.Selected())
		currentArea = value
		storeWidget.SetOptions(services.Store.ByAreaTitleForOptions(value))
		productWidget.SetOptions(services.Product.ByAreaTitleForOptions(value))
		stores, products := areaSelections.load(value)
		storeWidget.SetSelected(stores)
		productWidget.SetSelected(products)
	})

	help := `1. 在 Apple 官网将需要购买的型号加入购物车
2. 选择地区、门店和型号，点击“添加”按钮加入监听列表；切换地区后可继续添加，多个地区同时监听
3. 点击“开始”按钮开始监听，检测到有货时会自动打开购物车页面
`

	statusBinding := binding.NewString()
	statusBinding.Set(services.Pause)
	services.Listen.OnStatusChange(func(status string) {
		statusBinding.Set(status)
	})
	services.Listen.OnInStock(alertInStock)
	services.Listen.OnWarning(func(title, msg string) {
		dialog.ShowInformation(title, msg, view.Window)
		notify.Send(title, msg)
	})

	// 有货后继续监听：打开后有货不暂停，只在某项变成有货时提醒
	keepGoingCheck = widget.NewCheck("有货后继续监听", func(checked bool) {
		services.Listen.SetStopOnHit(!checked)
		saveSettings()
	})

	list := newListenList()
	banner := newWarningBanner()
	statusBar := newStatusBar()
	services.Listen.OnChange(func() {
		list.Refresh()
		banner.Update(services.Listen.Stats())
		statusBar.refresh()
	})

	loadUserSettingsCache()

	// 表单放在同一个 FormLayout 里，标签列宽度一致
	form := container.New(layout.NewFormLayout(),
		widget.NewLabel("选择地区:"), areaWidget.Widget(),
		widget.NewLabel("选择门店:"), storeWidget.Widget(),
		widget.NewLabel("选择型号:"), productWidget.Widget(),
		widget.NewLabel("查询频率:"), intervalWidget,
		widget.NewLabel("手机推送:"), pushWidget,
	)

	top := container.NewVBox(
		widget.NewLabel(help),
		form,
		container.NewBorder(nil, nil, createActionButtons(), createControlButtons(statusBinding)),
		container.NewBorder(nil, nil, createCheckButtons(), createToolButtons()),
		widget.NewSeparator(),
		list.Header(),
		banner.box,
	)

	// 初始化 GUI 窗口内容 (Initialize GUI)：监听列表占满剩余空间，四周留白
	bottom := container.NewBorder(nil, nil, nil, widget.NewLabel("version: "+common.VERSION), statusBar.label)
	content := container.NewBorder(top, bottom, nil, nil, list.Widget())
	view.Window.SetContent(container.New(layout.NewCustomPaddedLayout(12, 8, 16, 16), content))

	view.Window.Resize(fyne.NewSize(1000, 820))
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
func loadUserSettingsCache() {
	defer func() { settingsLoaded = true }()

	settings, err := services.LoadSettings()

	interval := services.DefaultIntervalMs
	if err == nil && settings.IntervalMs() > 0 {
		interval = settings.IntervalMs()
	}
	services.Listen.SetIntervalMs(interval)
	intervalWidget.SetText(services.IntervalLabel(interval))

	if err != nil {
		areaWidget.SetSelected(model.Areas[0].Title)
		return
	}

	areaWidget.SetSelected(settings.SelectedArea)
	storeWidget.SetSelected(settings.Stores())
	productWidget.SetSelected(settings.Products())
	services.Listen.SetListenItems(settings.ListenItems, services.Area.GetArea(settings.SelectedArea))
	pushWidget.SetText(settings.Pushes())
	services.Listen.SetPushUrls(settings.Pushes())
	keepGoingCheck.SetChecked(settings.KeepGoingOnHit)
}

// saveSettings 保存推送、频率与监听列表；门店和型号的勾选沿用上次「添加」时保存的值。
// 切换地区会清空勾选，若在这里一并保存，会把上次的勾选覆盖成空
func saveSettings() {
	saveSettingsWith(false)
}

// saveSettingsWith selections 为 true 时同时保存当前地区与门店、型号勾选
func saveSettingsWith(selections bool) {
	if !settingsLoaded {
		return
	}
	previous, _ := services.LoadSettings()
	area, stores, products := previous.SelectedArea, previous.Stores(), previous.Products()
	if selections {
		area, stores, products = areaWidget.Selected, storeWidget.Selected(), productWidget.Selected()
	}
	err := services.SaveSettings(services.UserSettings{
		SelectedArea:     area,
		SelectedStores:   stores,
		SelectedProducts: products,
		PushUrls:         pushWidget.Text,
		ListenIntervalMs: services.ParseIntervalLabel(intervalWidget.Text),
		ListenItems:      services.Listen.GetListenItems(),
		KeepGoingOnHit:   keepGoingCheck.Checked,
	})
	if err != nil {
		log.Println("保存配置失败:", err)
	}
}

// createActionButtons 添加、清空
func createActionButtons() *fyne.Container {
	return container.NewHBox(
		widget.NewButton("添加", func() {
			if len(storeWidget.Selected()) == 0 || len(productWidget.Selected()) == 0 {
				dialog.ShowError(errors.New("请选择门店和型号"), view.Window)
				return
			}
			// 门店 × 型号的所有组合都加入监听
			for _, store := range storeWidget.Selected() {
				for _, product := range productWidget.Selected() {
					services.Listen.Add(areaWidget.Selected, store, product)
				}
			}
			saveSettingsWith(true)
		}),
		widget.NewButton("清空列表", func() {
			total, _ := services.Listen.Counts()
			if total == 0 {
				return
			}
			dialog.ShowConfirm("清空监听列表", fmt.Sprintf("将删除全部 %d 项监听，其他设置保留。确定吗？", total), func(ok bool) {
				if ok {
					services.Listen.Clean()
					saveSettings()
				}
			}, view.Window)
		}),
	)
}

// createCheckButtons 开售前自检
func createCheckButtons() *fyne.Container {
	return container.NewHBox(
		widget.NewButton("试听(有货提示音)", func() {
			go alertMp3()
		}),
		widget.NewButton("测试通知", func() {
			// 首次发送时 macOS 会申请通知权限，开售前先点一次完成授权
			notify.Send("有货提醒（测试）", "能看到这条横幅，说明有货时会收到系统通知")
		}),
		widget.NewButton("测试打开购物车", func() {
			openBrowser(services.BagURL(services.Area.GetArea(areaWidget.Selected)))
		}),
		widget.NewButton("测试手机推送", testPush),
	)
}

// createToolButtons 有货记录、日志
func createToolButtons() *fyne.Container {
	return container.NewHBox(
		widget.NewButton("有货记录", showHistory),
		widget.NewButton("打开日志", openLogDir),
	)
}

func testPush() {
	services.Listen.SetPushUrls(pushWidget.Text)
	if len(services.SplitPushTargets(pushWidget.Text)) == 0 {
		dialog.ShowInformation("手机推送", "还没有填写推送地址。\n支持 Bark、Server酱、企业微信机器人、Telegram 和通用 Webhook，每行一个。", view.Window)
		return
	}
	go func() {
		bag := services.BagURL(services.Area.GetArea(areaWidget.Selected))
		results := services.Listen.SendPush("有货提醒（测试）", "这是一条测试推送，有货时会以同样方式通知你", bag)
		var lines []string
		for _, r := range results {
			status := "成功"
			if r.Err != nil {
				status = "失败：" + r.Err.Error()
			}
			lines = append(lines, fmt.Sprintf("[%s] %s\n%s", r.Channel, services.MaskTarget(r.Target), status))
		}
		dialog.ShowInformation("手机推送测试结果", strings.Join(lines, "\n\n"), view.Window)
	}()
}

// openLogDir 在访达 / 资源管理器中打开日志所在目录，出问题时把 app.log 发给开发者
func openLogDir() {
	dir, err := services.DataDir()
	if err != nil {
		dialog.ShowError(err, view.Window)
		return
	}
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		cmd = exec.Command("open", dir)
	case "windows":
		cmd = exec.Command("explorer", dir)
	default:
		cmd = exec.Command("xdg-open", dir)
	}
	if err := cmd.Start(); err != nil {
		dialog.ShowError(fmt.Errorf("打开日志目录失败: %w\n目录: %s", err, dir), view.Window)
	}
}

// 创建控制按钮 (Create control buttons)
func createControlButtons(status binding.String) *fyne.Container {
	return container.NewHBox(
		keepGoingCheck,
		widget.NewButton("开始", func() {
			services.Listen.SetStatus(services.Running)
		}),
		widget.NewButton("暂停", func() {
			services.Listen.SetStatus(services.Pause)
		}),
		container.NewCenter(widget.NewLabel("状态:")),
		container.NewCenter(widget.NewLabelWithData(status)),
	)
}
