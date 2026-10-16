package power

import (
	"os"
	"os/exec"
	"strconv"
	"sync"
)

func acquire() (func(), error) {
	// -w also releases the assertion if this process exits without cleanup.
	cmd := exec.Command("/usr/bin/caffeinate", "-i", "-w", strconv.Itoa(os.Getpid()))
	if err := cmd.Start(); err != nil {
		return nil, err
	}
	var once sync.Once
	return func() {
		once.Do(func() {
			_ = cmd.Process.Kill()
			_ = cmd.Wait()
		})
	}, nil
}
