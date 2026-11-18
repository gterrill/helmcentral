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

// Scratch ops for tests that call RunTx, Read and inTx directly.
var (
	testTxOp   = newLockOp("test.Tx")
	testReadOp = newLockOp("test.Read")
)

// lockedForTest describes a mutex the test has locked by hand, for the helpers
// that take the hold their caller keeps. The test unlocks it itself.
func lockedForTest(mu *sync.Mutex) *heldLock {
	return &heldLock{op: testTxOp, mu: mu, clock: time.Now, reg: lockRecorder(), since: time.Now()}
}

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

// unlockProbe is a recorder that checks, on every record, that the store mutex
// is free: a registry whose record blocks must not be able to stall the store.
type unlockProbe struct {
	mu   *sync.Mutex
	held []string
	seen []string
}

func (p *unlockProbe) record(name string, _ time.Duration) {
	p.seen = append(p.seen, name)
	if !p.mu.TryLock() {
		p.held = append(p.held, name)
		return
	}
	p.mu.Unlock()
}
func (p *unlockProbe) count(string, int64)                     {}
func (p *unlockProbe) recordRows(string, time.Duration, int64) {}

func TestLockOpHeld_RecordsOnlyAfterTheMutexIsFree(t *testing.T) {
	var mu sync.Mutex
	probe := &unlockProbe{mu: &mu}
	op := makeLockOp("t.after")
	h := op.heldTimed(&mu, time.Now, probe)
	if len(probe.seen) != 0 {
		t.Fatalf("recorded %v while the lock was held", probe.seen)
	}
	h.release()
	if len(probe.held) != 0 {
		t.Errorf("recorded %v while the store mutex was held", probe.held)
	}
	if len(probe.seen) != 2 {
		t.Errorf("recorded %v, want the wait and the hold", probe.seen)
	}
}

func TestNewLockOp_PanicsOnARepeatedName(t *testing.T) {
	name := "t.repeated.once"
	newLockOp(name)
	defer func() {
		if recover() == nil {
			t.Error("a second op with the same name did not panic")
		}
	}()
	newLockOp(name)
}

func TestRunTxUnderLock_ReturnsTheInnerErrorUnlocksAndRecords(t *testing.T) {
	v := newVitalsRegistry(time.Now)
	db := openOneConnPool(t)
	var mu sync.Mutex
	want := errors.New("inner failure")
	op := makeLockOp("t.tx")
	err := runTxUnderLock(op, &mu, db, time.Now, v, func(tx *sql.Tx) error { return want })
	if err != want {
		t.Errorf("err = %v, want the inner error as it is", err)
	}
	if !mu.TryLock() {
		t.Fatal("mutex still held after an error")
	}
	mu.Unlock()
	for _, k := range []string{"tx.lock_wait.t.tx", "tx.time.t.tx", "tx.lock_hold.t.tx"} {
		if _, ok := v.timing(k); !ok {
			t.Errorf("%s not recorded", k)
		}
	}
	if err := runTxUnderLock(op, &mu, db, time.Now, v, func(tx *sql.Tx) error { return nil }); err != nil {
		t.Errorf("clean run: %v", err)
	}
}

func TestRunTxUnderLock_RecordsOnlyAfterTheMutexIsFree(t *testing.T) {
	var mu sync.Mutex
	probe := &unlockProbe{mu: &mu}
	db := openOneConnPool(t)
	op := makeLockOp("t.txafter")
	if err := runTxUnderLock(op, &mu, db, time.Now, probe, func(tx *sql.Tx) error { return nil }); err != nil {
		t.Fatal(err)
	}
	if len(probe.held) != 0 {
		t.Errorf("recorded %v while the store mutex was held", probe.held)
	}
	if len(probe.seen) != 3 {
		t.Errorf("recorded %v, want wait, transaction and hold", probe.seen)
	}
}

func TestRunTxUnderLock_UnlocksWhenFnPanics(t *testing.T) {
	v := newVitalsRegistry(time.Now)
	db := openOneConnPool(t)
	var mu sync.Mutex
	func() {
		defer func() { _ = recover() }()
		_ = runTxUnderLock(makeLockOp("t.panic"), &mu, db, time.Now, v, func(tx *sql.Tx) error { panic("boom") })
	}()
	if !mu.TryLock() {
		t.Fatal("mutex still held after a panic")
	}
	mu.Unlock()
}
