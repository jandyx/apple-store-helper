package power

import (
	"fmt"
	"runtime"
	"sync"
	"syscall"
)

func acquire() (func(), error) {
	setState := syscall.NewLazyDLL("kernel32.dll").NewProc("SetThreadExecutionState")
	if err := setState.Find(); err != nil {
		return nil, err
	}
	ready := make(chan error, 1)
	stop := make(chan struct{})
	done := make(chan struct{})
	go func() {
		// Execution state belongs to an OS thread, not a Go goroutine.
		runtime.LockOSThread()
		// Deliberately exit with the thread locked so it is destroyed, even if
		// clearing its execution state fails; never return it to Go's pool.
		defer close(done)
		const continuous = 0x80000000
		const systemRequired = 0x00000001
		result, _, err := setState.Call(continuous | systemRequired)
		if result == 0 {
			ready <- fmt.Errorf("SetThreadExecutionState: %v", err)
			return
		}
		ready <- nil
		<-stop
		setState.Call(continuous)
	}()
	if err := <-ready; err != nil {
		<-done
		return nil, err
	}
	var once sync.Once
	return func() {
		once.Do(func() { close(stop) })
		<-done
	}, nil
}
