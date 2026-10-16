//go:build !darwin

package notify

import (
	"fyne.io/fyne/v2"

	"apple-store-helper/view"
)

func Setup() {}

// Send 非 macOS 平台使用 fyne 自带通知
func Send(title, content string) {
	view.App.SendNotification(&fyne.Notification{Title: title, Content: content})
}
