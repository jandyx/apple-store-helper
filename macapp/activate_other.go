//go:build !darwin

// Package macapp macOS 应用级事件，其他平台为空实现
package macapp

// OnActivate 非 macOS 平台不需要：窗口收起后可从任务栏托盘恢复
func OnActivate(fn func()) {}
