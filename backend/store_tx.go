package main

import (
	"database/sql"
	"fmt"
	"sync"
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
// Server vitals (ADR 0290) split a write's cost in two, keyed by its op name:
// "tx.lock_wait.<op>" is the time spent queued on s.mu behind other writers,
// and "tx.time.<op>" is the transaction itself, from begin to the commit or
// rollback. A slow write that shows a long wait is waiting on another write,
// not running slowly.
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
// calling s.now() again. op names the write in a begin or commit failure
// ("insert document: begin: ..."); errors fn returns pass through as they
// are. fn must not call another documentStore method: they take the same
// mutex. A method that already holds s.mu (because it reads something before
// deciding to write) calls runTxLocked instead.
func (s *documentStore) RunTx(op string, fn func(tx *sql.Tx, now time.Time) error) error {
	s.lockForTx(op)
	defer s.mu.Unlock()
	return s.runTxLocked(op, fn)
}

// lockForTx takes s.mu for a transaction. lockWaitHook, nil outside tests,
// runs just before the lock is requested so a test can tell that a writer
// has reached the lock without inspecting goroutine stacks. The time spent
// waiting is recorded as tx.lock_wait.<op>, so every caller that locks
// through here is measured; a writer that locks s.mu directly is not.
func (s *documentStore) lockForTx(op string) {
	if s.lockWaitHook != nil {
		s.lockWaitHook()
	}
	lockTimed(&s.mu, op, time.Now, serverVitals)
}

// runTxLocked is RunTx for a caller that already holds s.mu. It never takes
// the mutex itself, and panics when the mutex is free: calling it without
// s.mu held is a bug that would let two writers share the connection.
func (s *documentStore) runTxLocked(op string, fn func(tx *sql.Tx, now time.Time) error) error {
	return runTxTimed(&s.mu, s.db, op, time.Now, serverVitals, func(tx *sql.Tx) error {
		return fn(tx, s.now())
	})
}

// lockTimed takes mu and records the time spent waiting for it as
// tx.lock_wait.<op>, through the registry's key budget. The stores that run
// transactions (documentStore, coastlineStore, outsideAssistantStore) all
// lock through it, so they are measured the same way.
func lockTimed(mu *sync.Mutex, op string, clock func() time.Time, reg *vitalsRegistry) {
	waitStart := clock()
	mu.Lock()
	reg.record(reg.keys.admit("tx.lock_wait."+op, "tx.lock_wait.<overflow>"), clock().Sub(waitStart))
}

// lockOpLimit caps how many lockOps the code may declare. Lock keys are
// fixed when the program starts, one wait key and one hold key per op, so they
// do not draw on the registry's key budget (sqlKeyLimit) and cannot crowd SQL
// statement shapes out of it. newLockOp panics past the cap, so a runaway set
// stops the process at start-up rather than growing silently.
const lockOpLimit = 512

// lockOp names one store method that takes a store mutex for a read or a
// single statement, with its two vitals keys built once. Declare each as a
// package-level var in store_lock_ops.go; the name is a literal like
// "documents.Get", never an id or any value from a request.
type lockOp struct {
	name, waitKey, holdKey string
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
	o := &lockOp{name: name, waitKey: "tx.lock_wait." + name, holdKey: "tx.lock_hold." + name}
	lockOpRegistry.ops = append(lockOpRegistry.ops, o)
	return o
}

// heldLock is a store mutex taken through a lockOp, until release.
type heldLock struct {
	op    *lockOp
	mu    *sync.Mutex
	clock func() time.Time
	reg   *vitalsRegistry
	since time.Time
}

// held takes mu and records the time spent waiting for it, which includes
// time queued behind readers as well as writers. A method writes
// `defer lockDocumentsGet.held(&s.mu).release()`.
func (o *lockOp) held(mu *sync.Mutex) heldLock {
	return o.heldTimed(mu, time.Now, serverVitals)
}

// heldTimed is held with its clock and registry passed in.
func (o *lockOp) heldTimed(mu *sync.Mutex, clock func() time.Time, reg *vitalsRegistry) heldLock {
	waitStart := clock()
	mu.Lock()
	since := clock()
	reg.record(o.waitKey, since.Sub(waitStart))
	return heldLock{op: o, mu: mu, clock: clock, reg: reg, since: since}
}

// release unlocks the mutex and records how long it was held.
func (h heldLock) release() {
	d := h.clock().Sub(h.since)
	h.mu.Unlock()
	h.reg.record(h.op.holdKey, d)
}

// runTxUnderLock takes mu, runs fn in one transaction on db and releases mu,
// recording tx.lock_wait.<op> and tx.time.<op>. It is the whole of RunTx for a
// store with no test seam on its lock; fn's error and begin or commit failures
// are returned as runTxTimed returns them.
func runTxUnderLock(mu *sync.Mutex, db *sql.DB, op string, clock func() time.Time, reg *vitalsRegistry, fn func(tx *sql.Tx) error) error {
	lockTimed(mu, op, clock, reg)
	defer mu.Unlock()
	return runTxTimed(mu, db, op, clock, reg, fn)
}

// runTxTimed runs fn in one transaction on db, for a caller that holds mu. It
// records begin to commit or rollback as tx.time.<op>, commits only when fn
// returns nil, and wraps a begin or commit failure with op; fn's own error
// passes through as it is. It panics when mu is free: running without the
// mutex would let two writers share the connection.
func runTxTimed(mu *sync.Mutex, db *sql.DB, op string, clock func() time.Time, reg *vitalsRegistry, fn func(tx *sql.Tx) error) error {
	if mu.TryLock() {
		mu.Unlock()
		panic("store transaction run without the store mutex held (" + op + ")")
	}
	txStart := clock()
	defer func() {
		reg.record(reg.keys.admit("tx.time."+op, "tx.time.<overflow>"), clock().Sub(txStart))
	}()
	tx, err := db.Begin()
	if err != nil {
		return fmt.Errorf("%s: begin: %w", op, err)
	}
	defer tx.Rollback()

	if err := fn(tx); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("%s: commit: %w", op, err)
	}
	return nil
}
