package services

import (
	"apple-store-helper/model"
	"bytes"
	"fmt"
	"log"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func captureDiagnosticLog(t *testing.T) *bytes.Buffer {
	t.Helper()
	old := log.Writer()
	var output bytes.Buffer
	log.SetOutput(&output)
	t.Cleanup(func() { log.SetOutput(old) })
	return &output
}

func TestDiagnosticLimiter(t *testing.T) {
	output := captureDiagnosticLog(t)
	limiter := diagnosticLimiter{entries: map[string]diagnosticState{}}
	now := time.Now()
	d := queryDiagnostic{Stage: "pickup", Area: "CN", Store: "R1", Outcome: "ok"}
	limiter.record("key", d, time.Second, now)
	d.Bytes++
	limiter.record("key", d, time.Second, now.Add(time.Second))
	if strings.Count(output.String(), "stock_http") != 1 {
		t.Fatal(output.String())
	}
	limiter.record("key", d, time.Second, now.Add(time.Minute))
	if !strings.Contains(output.String(), "suppressed=1") {
		t.Fatal(output.String())
	}
	d.Status = 503
	limiter.record("key", d, time.Second, now.Add(time.Minute+time.Second))
	if strings.Count(output.String(), "stock_http") != 3 {
		t.Fatal(output.String())
	}
	for i := 0; i < 300; i++ {
		limiter.record(fmt.Sprint(i), d, 0, now)
	}
	if len(limiter.entries) > 256 {
		t.Fatal("unbounded diagnostic state")
	}
}

func TestFetchStockDiagnostics(t *testing.T) {
	output := captureDiagnosticLog(t)
	for _, tc := range []struct {
		name, body, outcome string
		status              int
	}{
		{"invalid", `secret-token <html>`, "invalid_json", 200},
		{"missing", `{"body":{"secret":"secret-token"}}`, "missing_stores", 200},
		{"blocked", `secret-token`, "http_error", 541},
		{"closed", `<html>"cv":"preorder" secret-token`, "store_closed", 503},
		{"parts", `{"body":{"stores":[{"storeNumber":"R1","partsAvailability":{}}]}}`, "missing_requested_parts", 200},
		{"fields", `{"body":{"stores":[{"storeNumber":"R1","partsAvailability":{"A1":{}}}]}}`, "missing_availability_fields", 200},
		{"ok", `{"body":{"stores":[{"storeNumber":"R1","partsAvailability":{"A1":{"pickupDisplay":"available"}}}]}}`, "ok", 200},
	} {
		t.Run(tc.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if !strings.Contains(r.URL.Path, "pickup-message") {
					return
				}
				w.WriteHeader(tc.status)
				fmt.Fprint(w, tc.body)
			}))
			defer server.Close()
			original := shopOrigin
			shopOrigin = func(model.Area) string { return server.URL }
			defer func() { shopOrigin = original }()
			resetShopSession()
			stockDiagnostics.mu.Lock()
			clear(stockDiagnostics.entries)
			stockDiagnostics.mu.Unlock()
			output.Reset()
			fetchStock(model.Areas[0], "R1", []ListenItem{{Product: model.Product{Code: "A1", Type: "iphone17"}}})
			logs := output.String()
			for _, want := range []string{`store="R1"`, fmt.Sprintf("status=%d", tc.status), `outcome="` + tc.outcome + `"`, "requested=1", "elapsed_ms="} {
				if !strings.Contains(logs, want) {
					t.Errorf("missing %s in %s", want, logs)
				}
			}
			if strings.Contains(logs, "secret-token") || strings.Contains(logs, server.URL) {
				t.Fatal("sensitive data in logs", logs)
			}
		})
	}
}

func TestRotatingLogWriterBounds(t *testing.T) {
	path := filepath.Join(t.TempDir(), "app.log")
	w := &rotatingLogWriter{path: path, limit: 64}
	if err := w.open(); err != nil {
		t.Fatal(err)
	}
	defer func() {
		if w.file != nil {
			w.file.Close()
		}
	}()
	payload := bytes.Repeat([]byte("x"), 200)
	if n, err := w.Write(payload); err != nil || n != len(payload) {
		t.Fatalf("write %d %v", n, err)
	}
	for _, file := range []string{path, path + ".1"} {
		info, err := os.Stat(file)
		if err != nil {
			t.Fatal(err)
		}
		if info.Size() > 64 {
			t.Fatalf("oversized %s: %d", file, info.Size())
		}
	}
	entries, err := os.ReadDir(filepath.Dir(path))
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 2 {
		t.Fatalf("expected 2 files, got %d", len(entries))
	}
}
