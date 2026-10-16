package services

// 有货历史：记录每次检测到有货的时间、地区、门店和型号，并按门店统计命中次数。
// 参考 https://github.com/dashug/apple-store-helper 的 services/history.go。
// 回头看哪家店、哪个时段更容易放货，比单次提醒更有参考价值。

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"time"
)

const (
	historyFileName = "history.json"
	// maxHistoryEntries 只保留最近的记录，避免文件无限增长
	maxHistoryEntries = 1000
	// historyDedupWindow 同一门店同一型号在这段时间内重复命中只记一次（命令行版持续监听时每轮都会命中）
	historyDedupWindow = 10 * time.Minute
)

// HistoryEntry 一次有货记录
type HistoryEntry struct {
	Time        time.Time `json:"time"`
	Area        string    `json:"area"`
	StoreNumber string    `json:"store_number"`
	Store       string    `json:"store"`
	ProductCode string    `json:"product_code"`
	Product     string    `json:"product"`
}

// StoreRank 门店命中排行的一行
type StoreRank struct {
	Area  string
	Store string
	Count int
	Last  time.Time
}

var historyMu sync.Mutex

func historyPath() (string, error) {
	dir, err := DataDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, historyFileName), nil
}

// LoadHistory 读取有货记录，按时间倒序
func LoadHistory() ([]HistoryEntry, error) {
	historyMu.Lock()
	defer historyMu.Unlock()
	return loadHistoryLocked()
}

func loadHistoryLocked() ([]HistoryEntry, error) {
	path, err := historyPath()
	if err != nil {
		return nil, err
	}
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}

	var entries []HistoryEntry
	if err := json.Unmarshal(data, &entries); err != nil {
		return nil, err
	}
	sort.SliceStable(entries, func(i, j int) bool { return entries[i].Time.After(entries[j].Time) })
	return entries, nil
}

// RecordHistory 追加有货记录，返回实际写入的条数
func RecordHistory(items []ListenItem, at time.Time) (int, error) {
	historyMu.Lock()
	defer historyMu.Unlock()

	entries, err := loadHistoryLocked()
	if err != nil {
		// 文件损坏时从头开始，不能因为历史记录影响提醒
		entries = nil
	}

	added := 0
	for _, item := range items {
		if recentlyRecorded(entries, item, at) {
			continue
		}
		entries = append([]HistoryEntry{{
			Time:        at,
			Area:        item.Area.Title,
			StoreNumber: item.Store.StoreNumber,
			Store:       item.Store.CityStoreName,
			ProductCode: item.Product.Code,
			Product:     ProductName(item.Product),
		}}, entries...)
		added++
	}
	if added == 0 {
		return 0, nil
	}

	if len(entries) > maxHistoryEntries {
		entries = entries[:maxHistoryEntries]
	}

	path, err := historyPath()
	if err != nil {
		return 0, err
	}
	data, err := json.MarshalIndent(entries, "", "  ")
	if err != nil {
		return 0, err
	}
	return added, os.WriteFile(path, data, 0644)
}

func recentlyRecorded(entries []HistoryEntry, item ListenItem, at time.Time) bool {
	for _, e := range entries {
		if at.Sub(e.Time) > historyDedupWindow {
			// entries 按时间倒序，后面的更早
			return false
		}
		if e.StoreNumber == item.Store.StoreNumber && e.ProductCode == item.Product.Code {
			return true
		}
	}
	return false
}

// ClearHistory 删除全部有货记录
func ClearHistory() error {
	historyMu.Lock()
	defer historyMu.Unlock()
	path, err := historyPath()
	if err != nil {
		return err
	}
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}

// RankStores 按门店统计命中次数，次数多的在前，相同次数时最近命中的在前
func RankStores(entries []HistoryEntry) []StoreRank {
	byStore := map[string]*StoreRank{}
	for _, e := range entries {
		rank, ok := byStore[e.StoreNumber]
		if !ok {
			rank = &StoreRank{Area: e.Area, Store: e.Store}
			byStore[e.StoreNumber] = rank
		}
		rank.Count++
		if e.Time.After(rank.Last) {
			rank.Last = e.Time
		}
	}

	ranks := make([]StoreRank, 0, len(byStore))
	for _, r := range byStore {
		ranks = append(ranks, *r)
	}
	sort.Slice(ranks, func(i, j int) bool {
		if ranks[i].Count != ranks[j].Count {
			return ranks[i].Count > ranks[j].Count
		}
		return ranks[i].Last.After(ranks[j].Last)
	})
	return ranks
}
