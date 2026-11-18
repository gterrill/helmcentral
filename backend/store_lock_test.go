package main

import (
	"database/sql"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// handoffClock is a clock the test moves by hand. Its first two reads (the
// start of the wait and the moment the lock is won) signal on first and
// second, so the test knows where the goroutine under test has got to without
// sleeping.
type handoffClock struct {
	ns            atomic.Int64
	reads         atomic.Int32
	first, second chan struct{}
}

func newHandoffClock() *handoffClock {
	return &handoffClock{first: make(chan struct{}, 1), second: make(chan struct{}, 1)}
}

func (c *handoffClock) advance(d time.Duration) { c.ns.Add(int64(d)) }

func (c *handoffClock) now() time.Time {
	t := time.Unix(0, c.ns.Load())
	switch c.reads.Add(1) {
	case 1:
		c.first <- struct{}{}
	case 2:
		c.second <- struct{}{}
	}
	return t
}

func TestLockOpHeldTimed_RecordsWaitAndHold(t *testing.T) {
	v := newVitalsRegistry(time.Now)
	clock := newHandoffClock()
	op := &lockOp{name: "t.op", waitKey: "tx.lock_wait.t.op", holdKey: "tx.lock_hold.t.op"}
	var mu sync.Mutex
	mu.Lock()
	release := make(chan struct{})
	done := make(chan struct{})
	go func() {
		h := op.heldTimed(&mu, clock.now, v)
		<-release
		h.release()
		close(done)
	}()

	<-clock.first // the goroutine has noted when its wait began
	clock.advance(50 * time.Millisecond)
	mu.Unlock()
	<-clock.second // it won the lock, at 50 ms
	clock.advance(25 * time.Millisecond)
	close(release)
	<-done

	wait, ok := v.timing("tx.lock_wait.t.op")
	if !ok || wait.MaxMs != 50 {
		t.Errorf("lock wait = %+v (found %v), want 50ms", wait, ok)
	}
	hold, ok := v.timing("tx.lock_hold.t.op")
	if !ok || hold.MaxMs != 25 {
		t.Errorf("lock hold = %+v (found %v), want 25ms and not the wait", hold, ok)
	}
}

// Lock keys are fixed at start-up and sit outside the registry's key budget:
// every declared op fits its own cap, and a registry that has recorded all of
// them still admits a full budget of SQL shapes.
func TestLockOps_FitTheirOwnBudgetAndLeaveSQLShapesTheirs(t *testing.T) {
	lockOpRegistry.mu.Lock()
	ops := append([]*lockOp(nil), lockOpRegistry.ops...)
	lockOpRegistry.mu.Unlock()
	if len(ops) == 0 || len(ops) > lockOpLimit {
		t.Fatalf("%d lock ops declared, want 1..%d", len(ops), lockOpLimit)
	}
	v := newVitalsRegistry(time.Now)
	logged := 0
	v.keys.logf = func(string, ...any) { logged++ }
	for _, o := range ops {
		unlock := o.heldTimed(&sync.Mutex{}, time.Now, v)
		unlock.release()
	}
	for i := 0; i < sqlKeyLimit; i++ {
		key := fmt.Sprintf("test: SELECT %d", i)
		if got := v.keys.admit(key, "test:<overflow>"); got != key {
			t.Fatalf("SQL shape %d folded into %q after the lock ops recorded; lock keys must not use the SQL budget", i, got)
		}
	}
	if logged != 0 {
		t.Errorf("key budget warned %d times; lock ops must not touch it", logged)
	}
}

func TestLockOpHeldTimed_UnlocksWhenTheCallerPanics(t *testing.T) {
	v := newVitalsRegistry(time.Now)
	op := &lockOp{name: "t.panic", waitKey: "tx.lock_wait.t.panic", holdKey: "tx.lock_hold.t.panic"}
	var mu sync.Mutex
	func() {
		defer func() { _ = recover() }()
		defer op.heldTimed(&mu, time.Now, v).release()
		panic("boom")
	}()
	if !mu.TryLock() {
		t.Fatal("mutex still held after a panic")
	}
	mu.Unlock()
	if _, ok := v.timing("tx.lock_hold.t.panic"); !ok {
		t.Error("hold not recorded for a call that panicked")
	}
}

func TestRunTxUnderLock_ReturnsTheInnerErrorUnlocksAndRecords(t *testing.T) {
	v := newVitalsRegistry(time.Now)
	db := openOneConnPool(t)
	var mu sync.Mutex
	want := errors.New("inner failure")
	err := runTxUnderLock(&mu, db, "t.tx", time.Now, v, func(tx *sql.Tx) error { return want })
	if err != want {
		t.Errorf("err = %v, want the inner error as it is", err)
	}
	if !mu.TryLock() {
		t.Fatal("mutex still held after an error")
	}
	mu.Unlock()
	for _, k := range []string{"tx.lock_wait.t.tx", "tx.time.t.tx"} {
		if _, ok := v.timing(k); !ok {
			t.Errorf("%s not recorded", k)
		}
	}
	if err := runTxUnderLock(&mu, db, "t.tx", time.Now, v, func(tx *sql.Tx) error { return nil }); err != nil {
		t.Errorf("clean run: %v", err)
	}
}

func TestRunTxUnderLock_UnlocksWhenFnPanics(t *testing.T) {
	v := newVitalsRegistry(time.Now)
	db := openOneConnPool(t)
	var mu sync.Mutex
	func() {
		defer func() { _ = recover() }()
		_ = runTxUnderLock(&mu, db, "t.panic", time.Now, v, func(tx *sql.Tx) error { panic("boom") })
	}()
	if !mu.TryLock() {
		t.Fatal("mutex still held after a panic")
	}
	mu.Unlock()
}
