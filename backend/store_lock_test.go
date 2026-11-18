package main

import (
	"database/sql"
	"errors"
	"sync"
	"testing"
	"time"
)

func TestLockHeldTimed_RecordsWaitAndHold(t *testing.T) {
	v := newVitalsRegistry(time.Now)
	var mu sync.Mutex
	mu.Lock()
	done := make(chan struct{})
	go func() {
		unlock := lockHeldTimed(&mu, "t.op", time.Now, v)
		time.Sleep(25 * time.Millisecond)
		unlock()
		close(done)
	}()
	time.Sleep(50 * time.Millisecond)
	mu.Unlock()
	<-done

	wait, ok := v.timing("tx.lock_wait.t.op")
	if !ok || wait.MaxMs < 40 {
		t.Errorf("lock wait = %+v (found %v), want at least 40ms", wait, ok)
	}
	hold, ok := v.timing("tx.lock_hold.t.op")
	if !ok || hold.MaxMs < 20 || hold.MaxMs >= 45 {
		t.Errorf("lock hold = %+v (found %v), want about 25ms and not the wait", hold, ok)
	}
}

func TestLockHeldTimed_UnlocksWhenTheCallerPanics(t *testing.T) {
	v := newVitalsRegistry(time.Now)
	var mu sync.Mutex
	func() {
		defer func() { _ = recover() }()
		defer lockHeldTimed(&mu, "t.panic", time.Now, v)()
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
