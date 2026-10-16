//go:build darwin

// Package notify 发送系统横幅通知。
// macOS 下直接使用 UserNotifications，并设置 delegate，App 在前台时也会弹出横幅。
package notify

/*
#cgo CFLAGS: -x objective-c
#cgo LDFLAGS: -framework Foundation -framework UserNotifications

#include <stdbool.h>
#include <stdlib.h>

bool notifySetup(void);
void notifySend(char *title, char *body);
*/
import "C"

import (
	"fmt"
	"log"
	"os/exec"
	"strings"
	"unsafe"
)

// Setup 启动时调用：设置前台展示 delegate 并申请通知权限
func Setup() {
	C.notifySetup()
}

// Send 发送横幅通知（带系统提示音）
func Send(title, content string) {
	cTitle := C.CString(title)
	defer C.free(unsafe.Pointer(cTitle))
	cContent := C.CString(content)
	defer C.free(unsafe.Pointer(cContent))
	C.notifySend(cTitle, cContent)
}

//export notifyFallback
func notifyFallback(cTitle, cBody *C.char) {
	fallback(C.GoString(cTitle), C.GoString(cBody))
}

// fallback 未授权或未打包为 .app 时用 osascript 发送
func fallback(title, content string) {
	escape := func(s string) string {
		return strings.ReplaceAll(strings.ReplaceAll(s, `\`, `\\`), `"`, `\"`)
	}
	script := fmt.Sprintf(`display notification "%s" with title "%s" sound name "Glass"`, escape(content), escape(title))
	if err := exec.Command("osascript", "-e", script).Start(); err != nil {
		log.Println("osascript notification failed", err)
	}
}
