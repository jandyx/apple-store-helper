package services

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"apple-store-helper/model"
)

// fakeApple 模拟 pickup-message 接口：按门店返回不同情况
type fakeApple struct {
	mu       sync.Mutex
	requests map[string]int // 门店号 -> 请求次数
	inStock  map[string]bool
}

func (f *fakeApple) handler(w http.ResponseWriter, r *http.Request) {
	if !strings.Contains(r.URL.Path, "pickup-message") {
		w.WriteHeader(http.StatusOK) // 商品页预热
		return
	}
	store := r.URL.Query().Get("store")
	f.mu.Lock()
	f.requests[store]++
	f.mu.Unlock()

	switch store {
	case "RCLOSED":
		w.WriteHeader(http.StatusServiceUnavailable)
		fmt.Fprint(w, `<html><script>var c = {"cv" : "preorder"}</script></html>`)
		return
	case "RBLOCK":
		w.WriteHeader(541)
		return
	case "RGONE":
		fmt.Fprint(w, `{"head":{"status":"200"},"body":{"errorMessage":"There is no store associated with this search."}}`)
		return
	}

	var parts []string
	for key, values := range r.URL.Query() {
		if strings.HasPrefix(key, "parts.") {
			code := values[0]
			display := "unavailable"
			f.mu.Lock()
			if f.inStock[store+"."+code] {
				display = "available"
			}
			f.mu.Unlock()
			parts = append(parts, fmt.Sprintf(`%q:{"pickupDisplay":%q}`, code, display))
		}
	}
	fmt.Fprintf(w, `{"body":{"content":{"pickupMessage":{"stores":[{"storeNumber":%q,"partsAvailability":{%s}}]}}}}`, store, strings.Join(parts, ","))
}

func setupFakeApple(t *testing.T) (*fakeApple, *listenService) {
	t.Helper()
	// 历史记录写到临时目录
	dir := t.TempDir()
	t.Setenv("HOME", dir)
	t.Setenv("XDG_CONFIG_HOME", dir)
	t.Setenv("AppData", dir)

	fake := &fakeApple{requests: map[string]int{}, inStock: map[string]bool{}}
	server := httptest.NewServer(http.HandlerFunc(fake.handler))
	t.Cleanup(server.Close)

	original := shopOrigin
	shopOrigin = func(area model.Area) string { return server.URL + "/" + area.ShortCode }
	t.Cleanup(func() { shopOrigin = original })
	resetShopSession()

	return fake, newListenService()
}

func addItem(s *listenService, area model.Area, store string, code string) string {
	st := model.Store{StoreNumber: store, CityStoreName: store}
	p := model.Product{Code: code, Title: code, Type: "iphoneduo"}
	s.AddItem(area, st, p)
	return ItemKey(st, p)
}

func TestTickStatusesAcrossAreas(t *testing.T) {
	fake, s := setupFakeApple(t)
	cn, hk := model.Areas[0], model.Areas[1]

	inStock := addItem(s, cn, "R639", "A1")
	outStock := addItem(s, cn, "R639", "A2")
	closed := addItem(s, hk, "RCLOSED", "H1")
	blocked := addItem(s, cn, "RBLOCK", "A1")
	gone := addItem(s, cn, "RGONE", "A1")
	disabled := addItem(s, hk, "R409", "H2")
	s.SetDisabled(disabled, true)
	fake.inStock["R639.A1"] = true

	var hits []ListenItem
	s.OnInStock(func(items []ListenItem, _ PurchaseOptions) { hits = items })
	s.SetStatus(Running)

	result := s.tick()

	items := s.GetListenItems()
	want := map[string]string{
		inStock:  StatusInStock,
		outStock: StatusOutStock,
		closed:   StatusStoreClosed,
		blocked:  StatusFailed + "：HTTP 541",
		gone:     StatusFailed + "：门店编号已失效，请换一家门店",
		disabled: StatusDisabled,
	}
	for key, status := range want {
		if items[key].Status != status {
			t.Errorf("%s: status = %q, want %q", key, items[key].Status, status)
		}
	}

	if fake.requests["R409"] != 0 {
		t.Errorf("disabled item was queried %d times", fake.requests["R409"])
	}
	if fake.requests["R639"] != 1 {
		t.Errorf("same store should be one request, got %d", fake.requests["R639"])
	}
	if !result.blocked || !result.storeClosed() {
		t.Errorf("expected blocked and storeClosed, got %+v", result)
	}
	if len(hits) != 1 || hits[0].Store.StoreNumber != "R639" {
		t.Fatalf("unexpected hits: %+v", hits)
	}
	if s.GetStatus() != Pause {
		t.Errorf("stopOnHit should pause, status = %s", s.GetStatus())
	}

	history, err := LoadHistory()
	if err != nil || len(history) != 1 || history[0].ProductCode != "A1" || history[0].Area != cn.Title {
		t.Fatalf("unexpected history: %+v, %v", history, err)
	}
}

func TestContinuousModeAlertsOnlyOnTransition(t *testing.T) {
	fake, s := setupFakeApple(t)
	s.SetStopOnHit(false)
	addItem(s, model.Areas[0], "R639", "A1")
	fake.inStock["R639.A1"] = true

	alerts := 0
	s.OnInStock(func(items []ListenItem, _ PurchaseOptions) { alerts++ })

	s.tick()
	s.tick()
	if alerts != 1 {
		t.Fatalf("still in stock should alert once, got %d", alerts)
	}

	fake.inStock["R639.A1"] = false
	s.tick()
	fake.inStock["R639.A1"] = true
	s.tick()
	if alerts != 2 {
		t.Fatalf("back in stock should alert again, got %d", alerts)
	}
	if s.GetStatus() != Pause {
		// 持续模式不改状态，初始即为暂停
		t.Fatalf("status changed unexpectedly: %s", s.GetStatus())
	}
}

func TestNextDelayBackoff(t *testing.T) {
	s := newListenService()
	s.SetIntervalMs(1000)

	blocked := newSkuFetch()
	blocked.blocked = true
	if d := s.nextDelay(blocked); d < time.Duration(float64(blockedBackoff)*(1-jitterRatio)) {
		t.Errorf("blocked should back off to ~%s, got %s", blockedBackoff, d)
	}

	closed := newSkuFetch()
	closed.blocked = true
	closed.closed["R1"] = true
	if d := s.nextDelay(closed); d > time.Duration(float64(time.Second)*(1+jitterRatio)) {
		t.Errorf("store closed should keep user interval, got %s", d)
	}
}

func TestSetListenItemsFillsMissingArea(t *testing.T) {
	s := newListenService()
	s.SetListenItems(map[string]ListenItem{
		"R639.A1": {Store: model.Store{StoreNumber: "R639"}, Product: model.Product{Code: "A1"}},
	}, model.Areas[0])
	if got := s.GetListenItems()["R639.A1"].Area.Locale; got != model.Areas[0].Locale {
		t.Fatalf("area not filled: %q", got)
	}
}

func TestHistoryDedupAndRanking(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	item := ListenItem{Area: model.Areas[0], Store: model.Store{StoreNumber: "R1", CityStoreName: "天环"}, Product: model.Product{Code: "A1"}}
	other := ListenItem{Area: model.Areas[0], Store: model.Store{StoreNumber: "R2", CityStoreName: "珠江"}, Product: model.Product{Code: "A1"}}

	now := time.Now()
	if n, _ := RecordHistory([]ListenItem{item}, now); n != 1 {
		t.Fatalf("first record should be written, got %d", n)
	}
	if n, _ := RecordHistory([]ListenItem{item}, now.Add(time.Minute)); n != 0 {
		t.Fatalf("duplicate within window should be skipped, got %d", n)
	}
	RecordHistory([]ListenItem{item, other}, now.Add(time.Hour))

	entries, _ := LoadHistory()
	if len(entries) != 3 {
		t.Fatalf("want 3 entries, got %d", len(entries))
	}
	ranks := RankStores(entries)
	if ranks[0].Store != "天环" || ranks[0].Count != 2 {
		t.Fatalf("unexpected ranking: %+v", ranks)
	}
}

func TestBlockedWarningFiresOnceAndResets(t *testing.T) {
	_, s := setupFakeApple(t)
	addItem(s, model.Areas[0], "RBLOCK", "A1")

	warnings := 0
	s.OnWarning(func(title, msg string) {
		warnings++
		if !strings.Contains(msg, "HTTP 541") {
			t.Errorf("warning should name the reason: %s", msg)
		}
	})

	for i := 0; i < blockedWarnRounds+2; i++ {
		s.tick()
	}
	stats := s.Stats()
	if warnings != 1 {
		t.Fatalf("warning should fire exactly once, got %d", warnings)
	}
	if !stats.AllFailed || stats.AllClosed || stats.FailedRounds != blockedWarnRounds+2 || stats.TopReason != "HTTP 541" {
		t.Fatalf("unexpected stats: %+v", stats)
	}
	if stats.Counts[StatusFailed] != 1 || stats.LastCheck.IsZero() {
		t.Fatalf("unexpected counts: %+v", stats)
	}

	// 恢复后清零，之后再次连续失败会重新提醒
	addItem(s, model.Areas[0], "R639", "A2")
	s.tick()
	if got := s.Stats(); got.AllFailed || got.FailedRounds != 0 {
		t.Fatalf("success should reset: %+v", got)
	}
}

func TestStoreClosedDoesNotCountAsBlocked(t *testing.T) {
	_, s := setupFakeApple(t)
	addItem(s, model.Areas[1], "RCLOSED", "H1")

	warnings := 0
	s.OnWarning(func(string, string) { warnings++ })
	for i := 0; i < blockedWarnRounds+1; i++ {
		s.tick()
	}
	stats := s.Stats()
	if warnings != 0 || !stats.AllClosed || stats.FailedRounds != 0 {
		t.Fatalf("store closed should not warn: warnings=%d stats=%+v", warnings, stats)
	}
}
