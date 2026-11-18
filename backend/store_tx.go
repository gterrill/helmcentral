package main

import (
	"database/sql"
	"fmt"
	"sync"
	"sync/atomic"
	"time"
)

// The record store's one transaction convention (ADR 0184).
//
// documentStore owns every record table in helmcentral.sqlite - documents,
// folders, manuals, notes, imports, inventory, maintenance, vessel
// particulars - and RunTx is the only way its code begins a transaction.
// Every write through it holds the store mutex for the transaction's whole
// life, so a REST handler's command, a Mate changeset's dry run and its
// apply (assistant_proposals.go) all queue on the same Go mutex on the same
// connection, instead of meeting each other only at SQLite's file lock.
//
// Server vitals (ADR 0290) split a write's cost in three, all keyed by its
// lockOp (store_lock_ops.go): "tx.lock_wait.<op>" is the time spent queued on
// s.mu, "tx.lock_hold.<op>" is how long the mutex was held, and
// "tx.time.<op>" is the transaction itself, from begin to the commit or
// rollback. A slow write that shows a long wait is waiting on another write,
// not running slowly. All three keys are built once, when the op is declared.
//
// Handlers never see a *sql.Tx: they call a documentStore method, and the
// method calls RunTx around the shared cmd*/ *Tx commands that changesets
// also run (ADR 0158).
//
// Schema creation and migrations at open time (createDocumentsSchema,
// rebuildEquipmentForSpares) run before the store is shared and keep their
// own transactions.

// RunTx runs fn in one transaction on this store's connection, holding the
// store mutex for its duration, and commits only when fn returns nil. now is
// the store's own clock, read once per transaction; fn uses it rather than
// calling s.now() again. op names the write for server vitals and in a begin
// or commit failure ("documents.Insert: begin: ..."); errors fn returns pass
// through as they are. fn must not call another documentStore method: they
// take the same mutex. A method that already holds s.mu (because it reads
// something before deciding to write) takes it through the same op and calls
// runTxLocked instead.
func (s *documentStore) RunTx(op *lockOp, fn func(tx *sql.Tx, now time.Time) error) error {
	h := s.holdForTx(op)
	defer h.release()
	return s.runTxLocked(h, fn)
}

// holdForTx takes s.mu for a write through op. lockWaitHook, nil outside
// tests, runs just before the lock is requested so a test can tell that a
// writer has reached the lock without inspecting goroutine stacks.
func (s *documentStore) holdForTx(op *lockOp) *heldLock {
	if s.lockWaitHook != nil {
		s.lockWaitHook()
	}
	return op.held(&s.mu)
}

// runTxLocked is RunTx for a caller that already holds s.mu through h, the
// lock it took with the same op. It never takes the mutex itself, and panics
// when the mutex is free: calling it without s.mu held is a bug that would let
// two writers share the connection.
func (s *documentStore) runTxLocked(h *heldLock, fn func(tx *sql.Tx, now time.Time) error) error {
	return h.runTx(s.db, func(tx *sql.Tx) error {
		return fn(tx, s.now())
	})
}

// lockOpLimit caps how many lockOps the code may declare. Lock and transaction
// keys are fixed when the program starts, three per op, so they do not draw on
// the registry's key budget (sqlKeyLimit) and cannot crowd SQL statement shapes
// out of it. newLockOp panics past the cap, so a runaway set stops the process
// at start-up rather than growing silently.
const lockOpLimit = 400

// lockOp names one store method that takes a store mutex, whether for a read,
// a single statement or a transaction, with its three vitals keys built once.
// Declare each as a package-level var in store_lock_ops.go; the name is a
// literal like "documents.Get", never an id or any value from a request. One
// name per op: the lock and the transaction of a write record under the same
// one, and newLockOp refuses a name declared twice.
type lockOp struct {
	name, waitKey, holdKey, txKey string
}

var lockOpRegistry struct {
	mu  sync.Mutex
	ops []*lockOp
}

// newLockOp declares an op. It panics on a repeated name or past lockOpLimit.
func newLockOp(name string) *lockOp {
	lockOpRegistry.mu.Lock()
	defer lockOpRegistry.mu.Unlock()
	if len(lockOpRegistry.ops) >= lockOpLimit {
		panic(fmt.Sprintf("more than %d lock ops; raise lockOpLimit on purpose (%s)", lockOpLimit, name))
	}
	for _, o := range lockOpRegistry.ops {
		if o.name == name {
			panic("lock op declared twice: " + name)
		}
	}
	o := makeLockOp(name)
	lockOpRegistry.ops = append(lockOpRegistry.ops, o)
	return o
}

// makeLockOp builds an op without declaring it. Production code goes through
// newLockOp; tests that need a scratch op use this.
func makeLockOp(name string) *lockOp {
	return &lockOp{name: name, waitKey: "tx.lock_wait." + name, holdKey: "tx.lock_hold." + name, txKey: "tx.time." + name}
}

// heldLock is a store mutex taken through a lockOp, until release. It keeps
// what it measured and records it only after the mutex is free, so a slow
// registry can never stall the store.
type heldLock struct {
	op    *lockOp
	mu    *sync.Mutex
	clock func() time.Time
	reg   vitalsRecorder
	since time.Time
	wait  time.Duration

	// txTime is the time spent in transactions run under this hold; txRan says
	// there was one.
	txTime time.Duration
	txRan  bool
}

// lockRecorderOverride, nil outside tests, replaces serverVitals as the place
// store locks record into, so a test reads back only what its own calls wrote.
var lockRecorderOverride atomic.Pointer[vitalsRecorder]

func lockRecorder() vitalsRecorder {
	if r := lockRecorderOverride.Load(); r != nil {
		return *r
	}
	return serverVitals
}

// held takes mu and measures the time spent waiting for it, which includes
// time queued behind readers as well as writers. A method writes
// `defer lockDocumentsGet.held(&s.mu).release()`; one that goes on to run a
// transaction under the lock keeps the hold: `h := op.held(&s.mu)`, then
// `defer h.release()` and `s.runTxLocked(h, fn)`.
func (o *lockOp) held(mu *sync.Mutex) *heldLock {
	return o.heldTimed(mu, time.Now, lockRecorder())
}

// heldTimed is held with its clock and recorder passed in.
func (o *lockOp) heldTimed(mu *sync.Mutex, clock func() time.Time, reg vitalsRecorder) *heldLock {
	waitStart := clock()
	mu.Lock()
	since := clock()
	return &heldLock{op: o, mu: mu, clock: clock, reg: reg, since: since, wait: since.Sub(waitStart)}
}

// release unlocks the mutex, then records the wait, how long it was held and,
// when a transaction ran under it, the transaction's time.
func (h *heldLock) release() {
	d := h.clock().Sub(h.since)
	h.mu.Unlock()
	h.reg.record(h.op.waitKey, h.wait)
	if h.txRan {
		h.reg.record(h.op.txKey, h.txTime)
	}
	h.reg.record(h.op.holdKey, d)
}

// runTx runs fn in one transaction on db, under the mutex h holds. It times
// begin to commit or rollback into h, to be recorded as tx.time.<op> when h is
// released; commits only when fn returns nil; and wraps a begin or commit
// failure with the op's name, while fn's own error passes through as it is. It
// panics when the mutex is free: running without it would let two writers
// share the connection.
func (h *heldLock) runTx(db *sql.DB, fn func(tx *sql.Tx) error) error {
	if h.mu.TryLock() {
		h.mu.Unlock()
		panic("store transaction run without the store mutex held (" + h.op.name + ")")
	}
	txStart := h.clock()
	defer func() {
		h.txTime += h.clock().Sub(txStart)
		h.txRan = true
	}()
	tx, err := db.Begin()
	if err != nil {
		return fmt.Errorf("%s: begin: %w", h.op.name, err)
	}
	defer tx.Rollback()

	if err := fn(tx); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("%s: commit: %w", h.op.name, err)
	}
	return nil
}

// runTxUnderLock takes mu, runs fn in one transaction on db and releases mu,
// recording tx.lock_wait.<op>, tx.time.<op> and tx.lock_hold.<op> after the
// release. It is the whole of RunTx for a store with no test seam on its lock;
// fn's error and begin or commit failures are returned as heldLock.runTx
// returns them.
func runTxUnderLock(op *lockOp, mu *sync.Mutex, db *sql.DB, clock func() time.Time, reg vitalsRecorder, fn func(tx *sql.Tx) error) error {
	h := op.heldTimed(mu, clock, reg)
	defer h.release()
	return h.runTx(db, fn)
}
