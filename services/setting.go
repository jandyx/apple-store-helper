package services

import (
	"encoding/json"
	"log"
	"os"
	"path/filepath"
)

const settingsFileName = "user_settings.json"

// settingsPath 配置文件路径：~/Library/Application Support/apple-store-helper/user_settings.json（macOS）。
// 双击 .app 启动时工作目录是 /，不可写，所以不能用相对路径
func settingsPath() string {
	dir, err := DataDir()
	if err != nil {
		return settingsFileName
	}
	return filepath.Join(dir, settingsFileName)
}

// DataDir 配置与日志所在目录，不存在时创建
func DataDir() (string, error) {
	dir, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	dir = filepath.Join(dir, "apple-store-helper")
	if err := os.MkdirAll(dir, 0755); err != nil {
		log.Println("create settings dir failed", err)
		return "", err
	}
	return dir, nil
}

type UserSettings struct {
	SelectedArea     string                `json:"selected_area"`
	SelectedStore    string                `json:"selected_store"` // 旧版单选字段，仅用于读取兼容
	SelectedStores   []string              `json:"selected_stores"`
	SelectedProduct  string                `json:"selected_product"` // 旧版单选字段，仅用于读取兼容
	SelectedProducts []string              `json:"selected_products"`
	BarkNotifyUrl    string                `json:"bark_notify_url"` // 旧版字段，仅用于读取兼容
	PushUrls         string                `json:"push_urls"`
	ListenInterval   int                   `json:"listen_interval"` // 旧版字段（秒），仅用于读取兼容
	ListenIntervalMs int                   `json:"listen_interval_ms"`
	ListenItems      map[string]ListenItem `json:"listen_items"`
}

// 保存配置到本地文件 SaveSettings saves settings to a file
func SaveSettings(settings UserSettings) error {
	data, err := json.Marshal(settings)
	if err != nil {
		return err
	}
	return os.WriteFile(settingsPath(), data, 0644)
}

// 加载缓存配置 LoadSettings loads settings from a file
func LoadSettings() (UserSettings, error) {
	var settings UserSettings
	data, err := os.ReadFile(settingsPath())
	if os.IsNotExist(err) {
		// 兼容旧版：配置保存在当前工作目录
		data, err = os.ReadFile(settingsFileName)
	}
	if err != nil {
		return settings, err
	}
	err = json.Unmarshal(data, &settings)
	return settings, err
}

// Stores 已选门店，兼容旧版单选
func (s UserSettings) Stores() []string {
	if len(s.SelectedStores) > 0 {
		return s.SelectedStores
	}
	if s.SelectedStore != "" {
		return []string{s.SelectedStore}
	}
	return nil
}

// Products 已选型号，兼容旧版单选
func (s UserSettings) Products() []string {
	if len(s.SelectedProducts) > 0 {
		return s.SelectedProducts
	}
	if s.SelectedProduct != "" {
		return []string{s.SelectedProduct}
	}
	return nil
}

// Pushes 手机推送地址，兼容旧版只填 Bark 的配置
func (s UserSettings) Pushes() string {
	if s.PushUrls != "" {
		return s.PushUrls
	}
	return s.BarkNotifyUrl
}

// IntervalMs 查询间隔（毫秒），兼容旧版按秒保存的 listen_interval
func (s UserSettings) IntervalMs() int {
	if s.ListenIntervalMs > 0 {
		return s.ListenIntervalMs
	}
	return s.ListenInterval * 1000
}

// 清空缓存配置 ClearSettings removes the settings file
func ClearSettings() error {
	os.Remove(settingsFileName)
	return os.Remove(settingsPath())
}
