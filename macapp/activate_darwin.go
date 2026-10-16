// Package macapp macOS 应用级事件
package macapp

/*
#cgo CFLAGS: -x objective-c
#cgo LDFLAGS: -framework AppKit
void observeActivation(void);
*/
import "C"

var onActivate func()

// OnActivate 应用被激活时回调（点 Dock 图标、再次打开 App 等）。
// 回调在主线程触发，fn 内如需操作窗口请另起 goroutine，避免与 fyne 主循环互等
func OnActivate(fn func()) {
	onActivate = fn
	C.observeActivation()
}

//export goAppDidBecomeActive
func goAppDidBecomeActive() {
	if onActivate != nil {
		onActivate()
	}
}
