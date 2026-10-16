package services

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

// IntervalOptions 可选的查询间隔（毫秒）。2 秒以内较激进，容易触发 Apple 风控（被拦截后会停 5 秒）；30/60 秒适合长期挂机
var IntervalOptions = []int{500, 1000, 2000, 3000, 5000, 10000, 30000, 60000}

// IntervalLabel 毫秒数转下拉框文案
func IntervalLabel(ms int) string {
	label := fmt.Sprintf("%g 秒", float64(ms)/1000)
	switch {
	case ms == DefaultIntervalMs:
		label += " (默认)"
	case ms < 2000:
		label += " (易被风控)"
	}
	return label
}

// IntervalLabels 下拉框选项
func IntervalLabels() []string {
	labels := make([]string, 0, len(IntervalOptions))
	for _, ms := range IntervalOptions {
		labels = append(labels, IntervalLabel(ms))
	}
	return labels
}

var intervalNumberRe = regexp.MustCompile(`\d+(\.\d+)?`)

// ParseInterval 解析下拉框文案或自定义输入，如 "1.5"、"1.5 秒"、"800ms"、"800 毫秒"；
// 默认单位为秒，低于 MinIntervalMs 时取下限
func ParseInterval(text string) (int, bool) {
	text = strings.TrimSpace(text)
	for _, ms := range IntervalOptions {
		if IntervalLabel(ms) == text {
			return ms, true
		}
	}

	number := intervalNumberRe.FindString(text)
	if number == "" {
		return 0, false
	}
	value, err := strconv.ParseFloat(number, 64)
	if err != nil {
		return 0, false
	}

	lower := strings.ToLower(text)
	ms := int(value * 1000)
	if strings.Contains(lower, "ms") || strings.Contains(text, "毫秒") {
		ms = int(value)
	}
	if ms < MinIntervalMs {
		ms = MinIntervalMs
	}
	return ms, true
}

// ParseIntervalLabel 同 ParseInterval，无法识别时回退默认值
func ParseIntervalLabel(label string) int {
	if ms, ok := ParseInterval(label); ok {
		return ms
	}
	return DefaultIntervalMs
}
