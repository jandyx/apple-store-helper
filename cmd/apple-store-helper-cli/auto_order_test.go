package main

import (
	"context"
	"os"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"apple-store-helper/services"
)

func TestOnceCannotTriggerAutoOrder(t *testing.T) {
	if err := validateAutoOrderInvocation(true, services.AutoOrderOptions{}); err != nil {
		t.Fatal(err)
	}
	for _, mode := range []string{"review", "submit"} {
		err := validateAutoOrderInvocation(true, services.AutoOrderOptions{Mode: mode})
		if err == nil || !strings.Contains(err.Error(), "-once") {
			t.Fatal("inventory one-shot must not silently trigger/kill order", mode, err)
		}
	}
}

type pendingOrderWaiter struct {
	started   chan struct{}
	cancelled atomic.Int32
}

func (w *pendingOrderWaiter) WaitAutoOrder(ctx context.Context) error {
	close(w.started)
	<-ctx.Done()
	return ctx.Err()
}
func (w *pendingOrderWaiter) CancelAutoOrder() { w.cancelled.Add(1) }

func TestOrderWaitDoesNotExitEarlyAndRemainsInterruptible(t *testing.T) {
	waiter := &pendingOrderWaiter{started: make(chan struct{})}
	stop := make(chan os.Signal, 1)
	done := make(chan error, 1)
	go func() { done <- waitForOrderAndPush(waiter, stop) }()
	<-waiter.started
	select {
	case <-done:
		t.Fatal("CLI exited before the order attempt finished")
	default:
	}
	stop <- os.Interrupt
	select {
	case err := <-done:
		if err != nil || waiter.cancelled.Load() != 1 {
			t.Fatal("interrupt must cancel one active attempt", err)
		}
	case <-time.After(time.Second):
		t.Fatal("order wait ignored process interrupt")
	}
}
