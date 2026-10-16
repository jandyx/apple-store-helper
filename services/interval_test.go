package services

import "testing"

func TestParseInterval(t *testing.T) {
	cases := []struct {
		in string
		ms int
		ok bool
	}{
		{"3 秒 (默认)", 3000, true},
		{"0.5 秒 (易被风控)", 500, true},
		{"1.5", 1500, true},
		{"800ms", 800, true},
		{"800 毫秒", 800, true},
		{"2 秒", 2000, true},
		{"100ms", MinIntervalMs, true}, // 低于下限取下限
		{"0.1", MinIntervalMs, true},
		{"abc", 0, false},
		{"", 0, false},
	}
	for _, c := range cases {
		ms, ok := ParseInterval(c.in)
		if ok != c.ok || ms != c.ms {
			t.Errorf("ParseInterval(%q) = %d, %v; want %d, %v", c.in, ms, ok, c.ms, c.ok)
		}
	}
	if got := ParseIntervalLabel("乱写"); got != DefaultIntervalMs {
		t.Errorf("fallback = %d", got)
	}
}

func TestIntervalLabelsRoundTrip(t *testing.T) {
	for _, ms := range IntervalOptions {
		got, ok := ParseInterval(IntervalLabel(ms))
		if !ok || got != ms {
			t.Errorf("label %q parsed to %d", IntervalLabel(ms), got)
		}
	}
}
