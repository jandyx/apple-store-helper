package services

// 日志同时写入文件。参考 https://github.com/dashug/apple-store-helper 的 services/logging.go。
// 双击 .app / .exe 启动时 stdout 不指向任何地方，查询失败、推送失败等诊断信息会被直接丢弃，
// 写到文件里，出问题时才能让用户把日志发过来。

import (
	"io"
	"log"
	"os"
	"path/filepath"
	"sync"
)

const (
	logFileName = "app.log"
	// logRotateSize 单个日志文件上限，超过后轮转一次，只保留一份历史
	logRotateSize = 2 << 20
)

// LogPath 日志文件路径，与配置文件同目录
func LogPath() (string, error) {
	dir, err := DataDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, logFileName), nil
}

// SetupLogging 让日志写入文件，返回日志文件路径。
// alsoStderr 为 true 时同时输出到 stderr；命令行版默认关闭，避免诊断日志淹没监听状态输出
func SetupLogging(alsoStderr bool) (string, error) {
	path, err := LogPath()
	if err != nil {
		return "", err
	}

	writer := &rotatingLogWriter{path: path, limit: logRotateSize}
	if err := writer.open(); err != nil {
		return "", err
	}

	if alsoStderr {
		log.SetOutput(io.MultiWriter(os.Stderr, writer))
	} else {
		log.SetOutput(writer)
	}
	log.SetFlags(log.LstdFlags)
	return path, nil
}

// rotatingLogWriter bounds both the current file and its one retained backup
// during long-running monitoring, not only on application startup.
type rotatingLogWriter struct {
	mu    sync.Mutex
	path  string
	limit int64
	file  *os.File
	size  int64
}

func (w *rotatingLogWriter) open() error {
	file, err := os.OpenFile(w.path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	info, err := file.Stat()
	if err != nil {
		file.Close()
		return err
	}
	w.file, w.size = file, info.Size()
	return nil
}
func (w *rotatingLogWriter) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	written := 0
	for len(p) > 0 {
		if w.file == nil {
			if err := w.open(); err != nil {
				return written, err
			}
		}
		if w.size >= w.limit || (w.size > 0 && int64(len(p)) <= w.limit && w.size+int64(len(p)) > w.limit) {
			if err := w.file.Close(); err != nil {
				return written, err
			}
			w.file = nil
			// Windows cannot rename over an existing destination.
			if err := os.Remove(w.path + ".1"); err != nil && !os.IsNotExist(err) {
				return written, err
			}
			if err := os.Rename(w.path, w.path+".1"); err != nil {
				return written, err
			}
			if err := w.open(); err != nil {
				return written, err
			}
		}
		size := min(int64(len(p)), w.limit-w.size)
		n, err := w.file.Write(p[:size])
		written += n
		w.size += int64(n)
		p = p[n:]
		if err != nil {
			return written, err
		}
		if n == 0 {
			return written, io.ErrShortWrite
		}
	}
	return written, nil
}
