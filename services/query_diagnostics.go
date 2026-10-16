package services

import (
	"log"
	"sync"
	"time"
)

// Only allowlisted metadata reaches disk: never bodies, URLs, headers or errors.
type queryDiagnostic struct {
	Stage, Area, Store, Outcome, Schema                  string
	Status, Requested, Parsed, Available, Missing, Bytes int
}
type diagnosticState struct {
	value      queryDiagnostic
	at         time.Time
	suppressed int
}
type diagnosticLimiter struct {
	mu      sync.Mutex
	entries map[string]diagnosticState
}

var stockDiagnostics = diagnosticLimiter{entries: make(map[string]diagnosticState)}

func (l *diagnosticLimiter) record(key string, d queryDiagnostic, elapsed time.Duration, now time.Time) {
	l.mu.Lock()
	defer l.mu.Unlock()
	previous, exists := l.entries[key]
	// Body length varies naturally and is not a state transition.
	signature, prior := d, previous.value
	signature.Bytes, prior.Bytes = 0, 0
	if exists && signature == prior && now.Sub(previous.at) < time.Minute {
		previous.suppressed++
		l.entries[key] = previous
		return
	}
	// Bound memory even when users repeatedly change their selection.
	if len(l.entries) >= 256 && !exists {
		clear(l.entries)
	}
	l.entries[key] = diagnosticState{value: d, at: now}
	log.Printf("stock_http stage=%q area=%q store=%q status=%d elapsed_ms=%d requested=%d parsed=%d available=%d missing=%d bytes=%d outcome=%q schema=%q suppressed=%d", d.Stage, d.Area, d.Store, d.Status, elapsed.Milliseconds(), d.Requested, d.Parsed, d.Available, d.Missing, d.Bytes, d.Outcome, d.Schema, previous.suppressed)
}
