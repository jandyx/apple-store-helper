package main

// 系统托盘：关闭窗口只收起，监听在后台继续。
// 参考 https://github.com/dashug/apple-store-helper 的 ui_tray.go。

import (
	_ "embed"
	"sync"
	"sync/atomic"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/driver/desktop"

	"apple-store-helper/macapp"
	"apple-store-helper/notify"
	"apple-store-helper/services"
	"apple-store-helper/view"
)

//go:embed Icon.png
var iconPNG []byte

var appIcon = fyne.NewStaticResource("Icon.png", iconPNG)

// windowHidden 主窗口是否收在托盘里。
// fyne 对居中过的窗口每次 Show 都会重新居中，窗口可见时不能再调 Show，否则每次切回 App 窗口都会跳回屏幕中间
var windowHidden atomic.Bool

// showMainWindow 显示收起的主窗口并置前；窗口本来可见时只置前
func showMainWindow() {
	if windowHidden.Swap(false) {
		view.Window.Show()
	}
	view.Window.RequestFocus()
}

// setupSystemTray 启用托盘并拦截关闭。托盘不可用时保留关闭即退出，避免用户失去退出入口
func setupSystemTray() {
	view.App.SetIcon(appIcon)

	desk, ok := view.App.(desktop.App)
	if !ok {
		return
	}

	desk.SetSystemTrayIcon(appIcon)
	// fyne 会自动补上「退出」项
	desk.SetSystemTrayMenu(fyne.NewMenu("Apple Store Helper",
		fyne.NewMenuItem("显示主窗口", showMainWindow),
		fyne.NewMenuItemSeparator(),
		fyne.NewMenuItem("开始监听", func() {
			services.Listen.SetStatus(services.Running)
		}),
		fyne.NewMenuItem("暂停监听", func() {
			services.Listen.SetStatus(services.Pause)
		}),
	))

	// 点 Dock 图标或再次打开 App 时把收起的窗口找回来。
	// 菜单栏图标多时托盘图标可能被刘海挡住，不能只靠托盘菜单恢复
	macapp.OnActivate(func() {
		if windowHidden.Load() {
			go showMainWindow()
		}
	})

	var hintOnce sync.Once
	view.Window.SetCloseIntercept(func() {
		windowHidden.Store(true)
		view.Window.Hide()
		// 只提示一次，否则用户以为已经退出，程序却还在后台请求 Apple
		hintOnce.Do(func() {
			notify.Send("仍在后台监听", "窗口已收进托盘（菜单栏图标），完全退出请用托盘菜单中的「退出」")
		})
	})
}
