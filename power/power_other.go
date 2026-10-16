//go:build !darwin && !windows

package power

import (
	"fmt"
	"runtime"
)

func acquire() (func(), error) {
	return nil, fmt.Errorf("%s 暂不支持自动阻止睡眠，请在系统设置中关闭自动睡眠", runtime.GOOS)
}
