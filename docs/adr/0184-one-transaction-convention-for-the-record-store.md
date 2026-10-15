# ADR 0184: One Transaction Convention for the Record Store

## Status

Accepted (2026-10-15). Amends [ADR 0141](0141-one-business-database.md)
(which handle writes the record tables) and
[ADR 0158](0158-mate-changes-records-through-changesets.md) (where a
changeset's apply runs).

## Context

`documentStore` owns the record tables in `helmcentral.sqlite`: documents,
folders, manuals, notes, imports, inventory, maintenance and vessel
particulars. `profileStore` already borrows its handle and mutex. By October
2026 the store began transactions three different ways:

- **`RunTx`**, added for maintenance (ADR 0146) and kept by changesets: take
  the store mutex, begin, run a function over the `*sql.Tx`, commit on nil.
  About eighteen methods used it.
- **Raw `s.db.Begin()`** in 28 methods across documents, imports, manuals,
  notes, inventory photos, the maintenance log and vessel particulars. Each
  took the mutex itself and repeated the begin, rollback and commit
  boilerplate, with its own error wording.
- **Handlers holding a `*sql.Tx`.** Seven maintenance handlers called
  `globalDocumentStore.RunTx` themselves and ran a `cmd*` command inside the
  closure, so HTTP code saw the transaction.

The changeset path had a sharper problem. `recordRegistry.prepare` dry-ran a
changeset inside `documentStore.RunTx`, and every REST command wrote through
the same handle. The operator's Apply, though, opened its transaction on the
assistant store's connection under the assistant store's mutex, wrote the
maintenance and inventory tables there, and marked the proposal applied in
the same transaction. That was atomic, because both stores' tables live in
one file (ADR 0141). But an apply and a REST write to the same table were
kept apart only by SQLite's file lock and the 30 second `busy_timeout`, not
by the mutex every other record write waits on, and a command that read
through `globalDocumentStore` during an apply would have seen a different
connection's view. A comment warned against that; nothing enforced it.

## Decision

1. **`RunTx` is the only way record-store code begins a transaction.** It
   moves to `backend/store_tx.go`, since it is no longer maintenance's, and
   takes an operation name (`RunTx("insert document", fn)`) that labels a
   begin or commit failure; errors from `fn` pass through unchanged. All 28
   raw `Begin` sites now go through it, each taking its timestamp from the
   `now` RunTx passes. A method that reads something under the mutex before
   deciding to write (a duplicate check, a name validation, a batch loop)
   calls `runTxLocked`, the same function without taking the lock, which
   panics if the mutex is free. `fn` must not call another `documentStore`
   method; they take the same mutex. `profileStore` holds the document store
   itself and writes through `RunTx` and reads through `Read`, rather than a
   copied handle and mutex pointer. The `convert-profile-rules` subcommand
   keeps its own transaction: it runs in a separate, operator-run process
   that shares the handle with nothing.
2. **Handlers never see a `*sql.Tx`.** Each of the seven maintenance
   handlers calls a store method named after the command it wraps
   (`CreateMaintenanceRuleCmd`, `UpdateMaintenanceRuleCmd`,
   `SetMaintenanceRuleOverridesCmd`, `ResetMaintenanceRuleOverrideCmd`,
   `AcknowledgeMaintenanceRuleCmd`, `SetMaintenanceRuleLastDoneCmd`,
   `CompleteMaintenanceRuleCmd`). Each is `RunTx`, through a small generic
   `runCmd`, around the same `cmd*` function the changeset types call, so
   REST and Mate still run identical code (ADR 0158).
3. **Proposals resolve on the record store's handle.**
   `applyAssistantProposal(assistant, records, id, today)` and
   `dismissAssistantProposal(assistant, records, id)` replace the assistant
   store's `ApplyProposal` and `DismissProposal`. Both take the two stores
   explicitly and refuse to run unless they were opened on the same
   database file, because the proposal row is the assistant store's table
   written through the record store's connection. An apply reads the
   proposal, applies the operations and writes the applied mark
   (`markProposalAppliedTx`) in one transaction, so the records and the mark
   commit together or not at all. When it fails as stale, the stale mark
   follows in a second transaction under the same hold of the mutex, so
   nothing can resolve the proposal in between. If the stale mark finds the
   proposal no longer pending, the apply answers with what it actually is.
   The post-commit hooks run after the mutex is released.
4. **Lock order.** Apply and Dismiss hold only the record store's mutex and
   never take the assistant store's, so there is no order between the two
   to get wrong. The assistant store still creates proposal rows with their
   message and reads them on its own connection.
5. **`equipmentLinkedToDocument`** becomes
   `documentStore.EquipmentLinkedToDocument`, so the summary-note code no
   longer reaches into the store's mutex and handle.

Schema creation and migrations at open time (`createDocumentsSchema`, the
spares rebuild on a dedicated connection with foreign keys off, the
assistant store's migrations) run before the store is shared and keep their
own transactions. The assistant store's own writes (messages, dismissals)
stay on its own handle.

## Alternatives rejected

- **Split `documentStore` into per-area repositories sharing one handle.**
  The files already separate the areas (`documents_store.go`,
  `inventory_store.go`, `maintenance_store.go`, `manuals_store.go` and so on).
  The split would rename about 186 call sites and change nothing about
  correctness, because every area would still share the one connection and
  mutex. Revisit only if one area needs a lifecycle of its own.
- **Keep the apply on the assistant connection and document the hazard.**
  The comment had been doing that. It left the one write path that touched
  the most tables at once as the one that did not queue on the record
  store's mutex.

## Consequences

- Every record write, from any caller, queues on one Go mutex on one
  connection. Two record writes never meet at SQLite's file lock, and a
  `database is locked` from the record tables would now mean another store or
  process, not a sibling write.
- Tests hold the record store's mutex and show an apply, and a Dismiss
  tapped during an apply, waiting on it rather than completing on another
  connection. A test-only hook on the store (`lockWaitHook`, nil in
  production) tells them a writer has reached the lock.
- Out of scope: the other six stores' handles, `profileStore` borrowing the
  record store's handle, and handler tests that swap the global stores.
