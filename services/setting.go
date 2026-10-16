package services

import (
	"encoding/json"
	"os"
)

type UserSettings struct {
	SelectedArea     string                `json:"selected_area"`
	SelectedStore    string                `json:"selected_store"`
	SelectedProduct  string                `json:"selected_product"`
	BarkNotifyUrl    string                `json:"bark_notify_url"`
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
	return os.WriteFile("user_settings.json", data, 0644)
}

// 加载缓存配置 LoadSettings loads settings from a file
func LoadSettings() (UserSettings, error) {
	var settings UserSettings
	data, err := os.ReadFile("user_settings.json")
	if err != nil {
		return settings, err
	}
	err = json.Unmarshal(data, &settings)
	return settings, err
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
	return os.Remove("user_settings.json")
}
