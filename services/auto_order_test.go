package services

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"apple-store-helper/checkout"
	"apple-store-helper/model"
)

type orderExecutorFunc func(context.Context, checkout.Target) (checkout.Result, error)

func (fn orderExecutorFunc) Run(ctx context.Context, target checkout.Target) (checkout.Result, error) {
	return fn(ctx, target)
}

func testOrderHit(store string) ListenItem {
	return ListenItem{Area: model.Areas[0], Store: model.Store{StoreNumber: store},
		Product: model.Product{Code: "MK2Q4CH/A", Type: "iphoneduo"}, Status: StatusInStock}
}

func testOrderCoordinator(fn orderExecutorFunc, notify func(AutoOrderEvent)) *autoOrderCoordinator {
	c := newAutoOrderCoordinator(AutoOrderOptions{Mode: checkout.ModeReview}, checkout.Config{
		SKU: "MK2Q4CH/A", StoreIDs: []string{"R639", "R577"}, Province: "广东", City: "广州", District: "天河区",
		MaxTotalCents: 1799900, Payment: "WECHAT",
	}, checkout.Contact{FirstName: "first", LastName: "last", Email: "private@example.test", Phone: "12345678900"}, fn, notify)
	c.acquirePower = func() (func(), error) { return func() {}, nil }
	return c
}

func waitOrder(t *testing.T, c *autoOrderCoordinator) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if err := c.wait(ctx); err != nil {
		t.Fatal(err)
	}
}

func TestAutoOrderArmConsumesExactlyOneConcurrentHit(t *testing.T) {
	var calls atomic.Int32
	var target checkout.Target
	c := testOrderCoordinator(func(_ context.Context, got checkout.Target) (checkout.Result, error) {
		calls.Add(1)
		target = got
		return checkout.Result{State: "review_ready"}, nil
	}, nil)
	hits := []ListenItem{testOrderHit("R577"), testOrderHit("R639")}
	var wg sync.WaitGroup
	for range 30 {
		wg.Add(1)
		go func() { defer wg.Done(); c.tryStart(hits) }()
	}
	wg.Wait()
	waitOrder(t, c)
	if calls.Load() != 1 || target.SKU != "MK2Q4CH/A" || target.StoreID != "R639" {
		t.Fatal("must choose one configured priority store once", calls.Load(), target.StoreID)
	}
	if c.tryStart(hits) {
		t.Fatal("completed arm must not automatically retry")
	}
}

func TestAutoOrderRejectsUnrelatedInventory(t *testing.T) {
	c := testOrderCoordinator(func(context.Context, checkout.Target) (checkout.Result, error) {
		t.Fatal("must not execute")
		return checkout.Result{}, nil
	}, nil)
	for _, change := range []func(*ListenItem){
		func(i *ListenItem) { i.Area = model.Areas[1] },
		func(i *ListenItem) { i.Product.Code = "OTHER" },
		func(i *ListenItem) { i.Product.Type = "iphone17" },
		func(i *ListenItem) { i.Store.StoreNumber = "OTHER" },
		func(i *ListenItem) { i.Disabled = true },
		func(i *ListenItem) { i.Status = StatusOutStock },
	} {
		i := testOrderHit("R639")
		change(&i)
		if c.tryStart([]ListenItem{i}) {
			t.Fatal("unrelated/unavailable hit started an order")
		}
	}
	if c.consumed {
		t.Fatal("unrelated hits consumed arm")
	}
}

func TestAutoOrderFailureIsRedactedAndNeverRetried(t *testing.T) {
	var event AutoOrderEvent
	c := testOrderCoordinator(func(context.Context, checkout.Target) (checkout.Result, error) {
		return checkout.Result{State: "submission_unknown", Stage: "submit"}, errors.New("secret-contact-cookie-value")
	}, func(got AutoOrderEvent) { event = got })
	if !c.tryStart([]ListenItem{testOrderHit("R639")}) {
		t.Fatal("not started")
	}
	waitOrder(t, c)
	if event.State != "submission_unknown" || strings.Contains(event.Message, "secret-contact") {
		t.Fatal("wrong terminal state or leaked error", event)
	}
	if c.tryStart([]ListenItem{testOrderHit("R639")}) {
		t.Fatal("ambiguous submission must not be retried")
	}
}

func TestAutoOrderWaitCancellationAndPowerOwnership(t *testing.T) {
	started := make(chan struct{})
	var released atomic.Int32
	c := testOrderCoordinator(func(ctx context.Context, _ checkout.Target) (checkout.Result, error) {
		close(started)
		<-ctx.Done()
		return checkout.Result{}, ctx.Err()
	}, nil)
	c.acquirePower = func() (func(), error) { return func() { released.Add(1) }, nil }
	if !c.tryStart([]ListenItem{testOrderHit("R639")}) {
		t.Fatal("not started")
	}
	<-started
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if !errors.Is(c.wait(ctx), context.Canceled) {
		t.Fatal("Wait ignored caller cancellation")
	}
	s := newListenService()
	s.autoOrder = c
	s.CancelAutoOrder()
	waitOrder(t, c)
	if released.Load() != 1 {
		t.Fatal("checkout must release its own sleep assertion exactly once")
	}
}

func TestCancelUntriggeredArmPreventsLaterExecution(t *testing.T) {
	c := testOrderCoordinator(func(context.Context, checkout.Target) (checkout.Result, error) {
		t.Fatal("cancelled arm executed")
		return checkout.Result{}, nil
	}, nil)
	s := newListenService()
	s.autoOrder = c
	s.CancelAutoOrder()
	if s.AutoOrderHandles([]ListenItem{testOrderHit("R639")}) || c.tryStart([]ListenItem{testOrderHit("R639")}) {
		t.Fatal("cancel did not disarm both service and a previously captured pointer")
	}
}

func TestReplacingArmRetiresPreviouslyCapturedTickPointer(t *testing.T) {
	old := testOrderCoordinator(func(context.Context, checkout.Target) (checkout.Result, error) {
		t.Fatal("retired arm executed after replacement")
		return checkout.Result{}, nil
	}, nil)
	s := newListenService()
	s.autoOrder = old
	captured := s.autoOrder // tick snapshot taken before the user changes policy
	next := testOrderCoordinator(func(context.Context, checkout.Target) (checkout.Result, error) {
		return checkout.Result{State: "review_ready"}, nil
	}, nil)
	if err := s.installAutoOrder(next); err != nil {
		t.Fatal(err)
	}
	if captured.tryStart([]ListenItem{testOrderHit("R639")}) {
		t.Fatal("old snapshot remained armed")
	}
	if !next.tryStart([]ListenItem{testOrderHit("R639")}) {
		t.Fatal("replacement was not installed")
	}
	waitOrder(t, next)
	if err := s.ConfigureAutoOrder(AutoOrderOptions{}); err != nil {
		t.Fatal(err)
	}
	if s.autoOrder != nil {
		t.Fatal("off did not remove arm")
	}
}

func TestCallbackFailureDoesNotEraseTerminalResult(t *testing.T) {
	c := testOrderCoordinator(func(context.Context, checkout.Target) (checkout.Result, error) {
		return checkout.Result{State: "review_ready", Stage: "review"}, nil
	}, func(AutoOrderEvent) { panic("private callback details must not be logged") })
	s := newListenService()
	s.autoOrder = c
	c.tryStart([]ListenItem{testOrderHit("R639")})
	waitOrder(t, c)
	event, ok := s.AutoOrderLastResult()
	if !ok || event.State != "review_ready" || event.Stage != "review" {
		t.Fatal("callback panic erased terminal outcome", event)
	}
}

func TestAutoOrderModeNeverPersistsOrAutoArms(t *testing.T) {
	data, err := json.Marshal(AutoOrderOptions{ConfigPath: "/private/order.json", Mode: checkout.ModeSubmit})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), "submit") || strings.Contains(string(data), "mode") {
		t.Fatal("order arming mode leaked into saved settings", string(data))
	}
	var loaded AutoOrderOptions
	if err := json.Unmarshal(data, &loaded); err != nil {
		t.Fatal(err)
	}
	if loaded.Normalized().Mode != AutoOrderOff {
		t.Fatal("saved settings rearmed order submission")
	}
	s := newListenService()
	if err := s.ConfigureAutoOrder(AutoOrderOptions{}); err != nil || s.autoOrder != nil {
		t.Fatal("default must remain reminder-only", err)
	}
}

func TestInventoryTickStartsOrderAndStillNotifies(t *testing.T) {
	fake, s := setupFakeApple(t)
	hit := testOrderHit("R639")
	s.AddItem(hit.Area, hit.Store, hit.Product)
	fake.inStock[ItemKey(hit.Store, hit.Product)] = true
	var calls atomic.Int32
	s.autoOrder = testOrderCoordinator(func(context.Context, checkout.Target) (checkout.Result, error) {
		calls.Add(1)
		return checkout.Result{State: "review_ready"}, nil
	}, nil)
	var notified bool
	s.OnInStock(func(hits []ListenItem, _ PurchaseOptions) {
		notified = len(hits) == 1
	})
	s.SetStatus(Running)
	s.tick()
	waitOrder(t, s.autoOrder)
	if !notified || calls.Load() != 1 || s.GetStatus() != Pause {
		t.Fatal("hit did not drive one order and independent reminder")
	}
}

func TestUnrelatedHitDoesNotStopArmedMonitor(t *testing.T) {
	fake, s := setupFakeApple(t)
	hit := testOrderHit("R639")
	hit.Product.Code = "OTHER"
	s.AddItem(hit.Area, hit.Store, hit.Product)
	fake.inStock[ItemKey(hit.Store, hit.Product)] = true
	s.autoOrder = testOrderCoordinator(func(context.Context, checkout.Target) (checkout.Result, error) {
		t.Fatal("unrelated SKU ordered")
		return checkout.Result{}, nil
	}, nil)
	s.SetStatus(Running)
	defer s.SetStatus(Pause)
	s.tick()
	if s.GetStatus() != Running || s.autoOrder.consumed {
		t.Fatal("non-target availability disabled the pending target")
	}
}
