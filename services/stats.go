package services

// 每轮查询的统计与持续拦截告警。
// 状态栏、警示条参考 dashug/apple-store-helper；持续被拦截时提醒一次参考 Sunbelife/apple-store-helper-15。

import (
	"fmt"
	"log"
	"sort"
	"strings"
	"time"
)

// blockedWarnRounds 连续多少轮全部查询失败（非商店维护）后提醒一次
const blockedWarnRounds = 5

// RoundStats 最近一轮查询的统计
type RoundStats struct {
	// LastCheck 上一轮完成时间，零值表示还没查过
	LastCheck time.Time
	// NextCheck 下一轮开始时间，暂停时为零值
	NextCheck time.Time
	// Checked 本轮参与查询的项数（不含停用）
	Checked int
	// Counts 本轮各状态（去掉失败原因后）的项数
	Counts map[string]int
	// AllFailed 本轮全部查询失败，结果不可信
	AllFailed bool
	// AllClosed 本轮失败全部是因为商店维护中
	AllClosed bool
	// FailedRounds 连续全部失败（非商店维护）的轮数
	FailedRounds int
	// TopReason 本轮最常见的失败原因
	TopReason string
}

// StatusCategory 去掉失败原因，如「查询失败：HTTP 541」->「查询失败」
func StatusCategory(status string) string {
	return strings.SplitN(status, "：", 2)[0]
}

// Stats 最近一轮的统计
func (s *listenService) Stats() RoundStats {
	s.mu.Lock()
	defer s.mu.Unlock()
	stats := s.stats
	stats.Counts = make(map[string]int, len(s.stats.Counts))
	for k, v := range s.stats.Counts {
		stats.Counts[k] = v
	}
	return stats
}

// OnWarning 连续多轮全部查询失败时回调一次，恢复后可再次触发。手机推送由服务自身完成
func (s *listenService) OnWarning(fn func(title, msg string)) {
	s.mu.Lock()
	s.onWarning = fn
	s.mu.Unlock()
}

// recordRoundLocked 统计本轮结果，返回需要发出的告警（无则为空）
func (s *listenService) recordRoundLocked(checked []ListenItem, at time.Time) (title, msg string) {
	stats := RoundStats{LastCheck: at, NextCheck: s.stats.NextCheck, Checked: len(checked), Counts: map[string]int{}}
	reasons := map[string]int{}
	success := 0
	for _, item := range checked {
		category := StatusCategory(item.Status)
		stats.Counts[category]++
		switch category {
		case StatusInStock, StatusOutStock:
			success++
		case StatusFailed:
			reasons[strings.TrimPrefix(item.Status, StatusFailed+"：")]++
		}
	}

	stats.AllFailed = len(checked) > 0 && success == 0
	stats.AllClosed = stats.AllFailed && stats.Counts[StatusStoreClosed] == len(checked)
	stats.TopReason = topReason(reasons)

	switch {
	case !stats.AllFailed:
		stats.FailedRounds = 0
	case stats.AllClosed:
		// 商店维护是预期内的，不算被拦截
		stats.FailedRounds = 0
	default:
		stats.FailedRounds = s.stats.FailedRounds + 1
	}
	s.stats = stats

	if stats.FailedRounds == blockedWarnRounds {
		title = "监听可能被拦截"
		msg = fmt.Sprintf("已连续 %d 轮全部查询失败（%s），当前结果不可信。可能被 Apple 限流，建议调低查询频率，或稍后再试。", stats.FailedRounds, stats.TopReason)
	}
	return title, msg
}

func topReason(reasons map[string]int) string {
	if len(reasons) == 0 {
		return ""
	}
	keys := make([]string, 0, len(reasons))
	for k := range reasons {
		keys = append(keys, k)
	}
	sort.Slice(keys, func(i, j int) bool {
		if reasons[keys[i]] != reasons[keys[j]] {
			return reasons[keys[i]] > reasons[keys[j]]
		}
		return keys[i] < keys[j]
	})
	if keys[0] == "" {
		return "原因未知"
	}
	return keys[0]
}

// fireWarning 推送并回调告警
func (s *listenService) fireWarning(title, msg string, onWarning func(string, string)) {
	log.Println(title, msg)
	go s.SendPush(title, msg, "")
	if onWarning != nil {
		onWarning(title, msg)
	}
}

// setNextCheck 记录下一轮开始时间，暂停时传零值
func (s *listenService) setNextCheck(at time.Time) {
	s.mu.Lock()
	s.stats.NextCheck = at
	s.mu.Unlock()
}
