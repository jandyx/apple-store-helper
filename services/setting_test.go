package services

import (
	"os"
	"path/filepath"
	"testing"
)

func useTempConfigDir(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("HOME", dir)
	t.Setenv("XDG_CONFIG_HOME", dir)
	t.Setenv("AppData", dir)
	return dir
}

func TestSettingsRoundTrip(t *testing.T) {
	useTempConfigDir(t)
	want := UserSettings{
		SelectedArea:     "中国大陆",
		SelectedStores:   []string{"广东-天环广场"},
		SelectedProducts: []string{"iphoneduo - 夜空色 - 256gb"},
		PushUrls:         "https://sctapi.ftqq.com/X.send",
		ListenIntervalMs: 1500,
		KeepGoingOnHit:   true,
		ListenItems:      map[string]ListenItem{"R577.A": {Status: StatusWait}},
	}
	if err := SaveSettings(want); err != nil {
		t.Fatal(err)
	}
	got, err := LoadSettings()
	if err != nil {
		t.Fatal(err)
	}
	if got.SelectedArea != want.SelectedArea || got.Pushes() != want.PushUrls || got.IntervalMs() != 1500 ||
		!got.KeepGoingOnHit || len(got.ListenItems) != 1 || got.Stores()[0] != "广东-天环广场" {
		t.Fatalf("round trip mismatch: %+v", got)
	}
}

// 旧版配置：单选门店 / 型号、按秒保存的间隔、只有 Bark 地址
func TestSettingsLegacyFields(t *testing.T) {
	dir := useTempConfigDir(t)
	path := settingsPath()
	if filepath.Dir(path) == "." || !filepath.IsAbs(path) || !filepath.HasPrefix(path, dir) {
		t.Fatalf("settings should live under the config dir, got %s", path)
	}
	legacy := `{"selected_area":"中国大陆","selected_store":"  广东-天环广场 ","selected_product":"iphoneduo - 夜空色 - 256gb","bark_notify_url":"https://api.day.app/KEY","listen_interval":5}`
	if err := os.WriteFile(path, []byte(legacy), 0644); err != nil {
		t.Fatal(err)
	}
	s, err := LoadSettings()
	if err != nil {
		t.Fatal(err)
	}
	if s.IntervalMs() != 5000 {
		t.Errorf("legacy seconds should convert to ms, got %d", s.IntervalMs())
	}
	if s.Pushes() != "https://api.day.app/KEY" {
		t.Errorf("legacy bark url should be used as push url, got %q", s.Pushes())
	}
	if len(s.Stores()) != 1 || len(s.Products()) != 1 {
		t.Errorf("legacy single selections should be kept: %q %q", s.Stores(), s.Products())
	}
}

func TestClearSettings(t *testing.T) {
	useTempConfigDir(t)
	SaveSettings(UserSettings{SelectedArea: "中国大陆"})
	ClearSettings()
	if _, err := LoadSettings(); err == nil {
		t.Fatal("settings should be gone after clear")
	}
}
