package main

import (
	"strings"
	"testing"
	"time"

	"apple-store-helper/services"
)

func TestStatusText(t *testing.T) {
	now := time.Now()
	stats := services.RoundStats{
		LastCheck: now.Add(-time.Second),
		NextCheck: now.Add(2500 * time.Millisecond),
		Checked:   16,
		Counts:    map[string]int{services.StatusFailed: 2, services.StatusOutStock: 13, services.StatusInStock: 1},
	}
	got := statusText(services.Running, stats, now)
	for _, want := range []string{"上次检查", "下次 2.5 秒后", "本轮 16 项：有货 1，无货 13，查询失败 2"} {
		if !strings.Contains(got, want) {
			t.Errorf("status text %q missing %q", got, want)
		}
	}
	if got := statusText(services.Pause, services.RoundStats{}, now); !strings.Contains(got, "尚未查询") || !strings.Contains(got, "已暂停") {
		t.Errorf("paused text: %q", got)
	}
}

func TestBannerText(t *testing.T) {
	if text, _ := bannerText(services.RoundStats{AllFailed: false}); text != "" {
		t.Errorf("no banner expected, got %q", text)
	}
	if text, level := bannerText(services.RoundStats{AllFailed: true, AllClosed: true}); !strings.Contains(text, "维护") || level != bannerInfo {
		t.Errorf("closed banner: %q %v", text, level)
	}
	text, level := bannerText(services.RoundStats{AllFailed: true, FailedRounds: 3, TopReason: "HTTP 541"})
	if !strings.Contains(text, "连续 3 轮") || !strings.Contains(text, "HTTP 541") || level != bannerError {
		t.Errorf("blocked banner: %q %v", text, level)
	}
}

func TestFilterRows(t *testing.T) {
	rows := []services.Row{
		{Key: "a", Item: services.ListenItem{Status: services.StatusFailed + "：HTTP 541"}},
		{Key: "b", Item: services.ListenItem{Status: services.StatusOutStock}},
	}
	if got := filterRows(rows, services.StatusFailed); len(got) != 1 || got[0].Key != "a" {
		t.Errorf("failed filter should ignore reason, got %+v", got)
	}
	if got := filterRows(rows, filterAll); len(got) != 2 {
		t.Errorf("all filter got %d", len(got))
	}
}
