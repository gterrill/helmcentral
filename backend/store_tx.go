package main

import (
	"database/sql"
	"fmt"
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
	s.lockForTx()
	defer s.mu.Unlock()
	return s.runTxLocked(op, fn)
}

// lockForTx takes s.mu for a transaction. lockWaitHook, nil outside tests,
// runs just before the lock is requested so a test can tell that a writer
// has reached the lock without inspecting goroutine stacks.
func (s *documentStore) lockForTx() {
	if s.lockWaitHook != nil {
		s.lockWaitHook()
	}
	s.mu.Lock()
}

// runTxLocked is RunTx for a caller that already holds s.mu. It never takes
// the mutex itself, and panics when the mutex is free: calling it without
// s.mu held is a bug that would let two writers share the connection.
func (s *documentStore) runTxLocked(op string, fn func(tx *sql.Tx, now time.Time) error) error {
	if s.mu.TryLock() {
		s.mu.Unlock()
		panic("documentStore.runTxLocked called without s.mu held (" + op + ")")
	}
	tx, err := s.db.Begin()
	if err != nil {
		return fmt.Errorf("%s: begin: %w", op, err)
	}
	defer tx.Rollback()

	if err := fn(tx, s.now()); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("%s: commit: %w", op, err)
	}
	return nil
}
