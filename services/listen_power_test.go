package services

import (
	"errors"
	"sync"
	"testing"

	"apple-store-helper/model"
)

func fakePower(s *listenService) (acquired, released *int) {
	a, r := 0, 0
	s.acquirePower = func() (func(), error) {
		a++
		return func() { r++ }, nil
	}
	return &a, &r
}

func TestPowerFollowsStatus(t *testing.T) {
	s := newListenService()
	a, r := fakePower(s)
	for i := 0; i < 2; i++ {
		s.SetStatus(Running)
		s.SetStatus(Running)
		if *a != i+1 || *r != i {
			t.Fatalf("unexpected acquire/release: %d/%d", *a, *r)
		}
		s.SetStatus(Pause)
		s.SetStatus(Pause)
		if *r != i+1 || s.releasePower != nil {
			t.Fatal("pause must release exactly once")
		}
	}
}

func TestPowerFailureDoesNotStopMonitoring(t *testing.T) {
	s := newListenService()
	attempts := 0
	s.acquirePower = func() (func(), error) {
		attempts++
		return nil, errors.New("unsupported")
	}
	s.SetStatus(Running)
	if s.GetStatus() != Running {
		t.Fatal("sleep assertion failure must not prevent monitoring")
	}
	s.SetStatus(Pause)
	s.SetStatus(Running)
	s.SetStatus(Pause)
	if attempts != 2 {
		t.Fatalf("restart must retry: attempts = %d", attempts)
	}
}

func TestPowerReleasedOnStockHit(t *testing.T) {
	for _, stop := range []bool{true, false} {
		t.Run(map[bool]string{true: "pause", false: "continuous"}[stop], func(t *testing.T) {
			fake, s := setupFakeApple(t)
			a, r := fakePower(s)
			s.SetStopOnHit(stop)
			addItem(s, model.Areas[0], "R639", "A1")
			fake.inStock["R639.A1"] = true
			s.SetStatus(Running)
			t.Cleanup(func() { s.SetStatus(Pause) })
			s.tick()
			wantReleased := 0
			if stop {
				wantReleased = 1
			}
			if *a != 1 || *r != wantReleased {
				t.Fatalf("acquired/released = %d/%d, stop = %v", *a, *r, stop)
			}
		})
	}
}

func TestPowerConcurrentStatusChanges(t *testing.T) {
	s := newListenService()
	a, r := fakePower(s)
	var wg sync.WaitGroup
	for i := 0; i < 30; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			s.SetStatus(Running)
			s.SetStatus(Pause)
		}()
	}
	wg.Wait()
	s.SetStatus(Pause)
	if *a != *r || s.releasePower != nil {
		t.Fatalf("leaked assertion: acquired/released = %d/%d", *a, *r)
	}
}
