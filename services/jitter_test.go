package services

import (
	"testing"
	"time"
)

func TestWithJitterStaysInRange(t *testing.T) {
	base := 2 * time.Second
	low, high := time.Duration(float64(base)*(1-jitterRatio)), time.Duration(float64(base)*(1+jitterRatio))
	seen := map[time.Duration]bool{}
	for i := 0; i < 200; i++ {
		d := withJitter(base)
		if d < low || d > high {
			t.Fatalf("jitter out of range: %s", d)
		}
		seen[d] = true
	}
	if len(seen) < 10 {
		t.Fatalf("jitter not random enough: %d distinct values", len(seen))
	}
	if withJitter(0) != 0 {
		t.Fatal("zero interval must stay zero")
	}
}
