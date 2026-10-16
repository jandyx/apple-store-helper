package services

// 日志同时写入文件。参考 https://github.com/dashug/apple-store-helper 的 services/logging.go。
// 双击 .app / .exe 启动时 stdout 不指向任何地方，查询失败、推送失败等诊断信息会被直接丢弃，
// 写到文件里，出问题时才能让用户把日志发过来。

import (
	"io"
	"log"
	"os"
	"path/filepath"
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

	if info, err := os.Stat(path); err == nil && info.Size() >= logRotateSize {
		if err := os.Rename(path, path+".1"); err != nil {
			log.Println("日志轮转失败:", err)
		}
	}

	file, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return "", err
	}

	if alsoStderr {
		log.SetOutput(io.MultiWriter(os.Stderr, file))
	} else {
		log.SetOutput(file)
	}
	log.SetFlags(log.LstdFlags)
	return path, nil
}
