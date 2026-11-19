# ADR 0290: Server Vitals

## Status

Accepted (2026-11-18). Builds on [ADR 0141](0141-one-business-database.md)
(one business file, one pool per store) and
[ADR 0184](0184-one-transaction-convention-for-the-record-store.md)
(`RunTx` and the store mutex).

## Context

Helmcentral runs on a small box, and the wall display, the phones and Mate
all share its one process. When the operator says "it felt slow around
midnight", nothing on the box can say whether a SQLite statement stalled, a
write queued behind another write, the heap grew, or the Go runtime paused.
The log buffer holds messages, and the pprof routes (off by default) need
someone watching at the moment it happens. Neither keeps a record of an
ordinary hour.

Two facts about the code made the first step cheap. Every store reaches
SQLite through `database/sql`, about 780 call sites that all end in one
driver. And the opening code existed in five copies: `openHelmcentralDB` for
eleven stores, and three bare `sql.Open` calls for sessions, secrets and the
tile cache.

## Decision

Helmcentral records **server vitals**: how long its own work takes, and how
the Go runtime is doing. Recording is always on, held in memory, and written
to a file once a minute. The word "telemetry" stays with boat data; these
numbers describe the server process.

### One opener

`openSQLite(store, path, profile)` replaces `openHelmcentralDB` and the three
`sql.Open` calls. Every caller passes a short store name (`documents`,
`assistant`, `alarm_log`, `sessions`, `secrets`, `tiles`). Four profiles keep
what each file had before, byte for byte:

- **business**: foreign keys, 30 second busy timeout, WAL, synchronous NORMAL,
  `BEGIN IMMEDIATE`, one connection. Foreign keys and WAL are read back.
- **sessions** and **secrets**: driver defaults, one connection.
- **tiles**: 5 second busy timeout, WAL, synchronous NORMAL, four connections.

The opener also creates the file's directory, so no store does it itself.
The `migrate-db` command opens the business file through the same function.
The stores still call `db.Exec`, `db.Query` and `db.Begin` as before.

### The connector seam

The opener builds its pool from a wrapping `driver.Connector` around
modernc's driver, so the stores change nothing. The wrapper times every
statement, a prepared statement's `Exec` and `Query`, `Begin`, `Commit` and
`Rollback`. It forwards every optional interface modernc implements that
`database/sql` relies on: `ExecerContext`, `QueryerContext`,
`ConnPrepareContext`, `ConnBeginTx`, `Pinger`, `SessionResetter`,
`Validator`, the statement context methods and the five column-type
interfaces on rows. modernc has no `NamedValueChecker`, so none is forwarded.
Opening a connection checks once that modernc's connection and statements
carry the context methods and fails if an upgrade drops one, so the wrapper
has no fallback path that would leave statements untimed.

- **Key**: store name plus the statement's text with whitespace collapsed,
  cut to 80 characters. Bound arguments never appear. A value pasted into the
  SQL text would appear, up to the cut, which is one reason the stores bind
  arguments.
- **Query clock**: a query's clock starts before the driver call and stops at
  `rows.Close()`, so the time a caller spends reading rows counts. Rows read
  are counted beside it.
- **Cap**: 2,000 distinct keys across the whole process, held by the
  recorder. Only SQL statement shapes draw on this budget. The production code
  holds about 470 distinct literal statement shapes, so the cap leaves room
  for SQL built at run time without letting it grow without bound. Past that, one warning
  goes to the log and further statements record under `<store>:<overflow>`.
  The overflow key is visible in the output; nothing is dropped quietly.
  The lock and transaction keys of the store ops (below) sit outside this
  budget and have a cap of their own.
- **Slow statements**: any statement over 250 ms writes one log line naming
  the store, the shape and the duration.
- **Cost**: about 0.4 µs a statement on an M1 (`BenchmarkSQLExec`), against
  about 12 µs for the statement itself.

### More database figures

Every store call that takes a store mutex names itself once as a `lockOp`
(`var lockDocumentsGet = newLockOp("documents.Get")`, all in
`store_lock_ops.go`) and records three numbers under that one name:
`tx.lock_wait.<op>` (the time spent getting the lock), `tx.lock_hold.<op>`
(the time the lock was held) and, for a call that runs a transaction,
`tx.time.<op>` (begin to commit or rollback). The wait includes time queued
behind readers as well as writers, because every call on a store shares one
mutex. A slow write that shows a long wait was waiting on another call.

A read or a single statement locks with
`defer lockDocumentsGet.held(&s.mu).release()`. A write goes through
`RunTx(op, fn)`, or, when it reads before it decides to write, takes the lock
as `h := op.held(&s.mu)` and runs `s.runTxLocked(h, fn)`. The coastline, MCP
and assistant stores run theirs through `runTxUnderLock(op, ...)`. In every
case the lock and the transaction of one write carry the same op, so a
transaction never appears under free text apart from its lock.

One name per op. Each caller has its own op, and a helper that several
callers share (`documentStore.Read`, `RunTx`, the profile store's `inTx`, the
alarm log's `one` and `stampOpen`) takes the caller's op as an argument, so
the alarm review's read and the Mate's watch tools' read are not merged into
one key. An op is a store name and a method name written in the code, never an
id or a value from a request. `newLockOp` builds the three key strings once at
start-up and panics on a repeated name, so no two ops share a key and a call
allocates nothing for its keys. No `RunTx` caller passes a name built at run
time.

All three numbers are recorded after the mutex is released, from what the
call measured while it held it, so a slow recorder cannot stall a store.

The keys have a budget of their own. They are fixed when the program starts
and recorded directly, not through the 2,000-key budget above, so they cannot
crowd out SQL shapes. `lockOpLimit` (400) caps the ops: `newLockOp` panics past
it, or on a repeated name, so a runaway set stops the process at start-up
instead of growing quietly. A test declares every op against a fresh registry
and checks that a full 2,000 SQL shapes are still admitted.

Each pool's `sql.DBStats` `WaitCount` and `WaitDuration` are
sampled once a minute, as `db.pool_wait_count.<store>` and
`db.pool_wait_us.<store>`. The registry holds each pool through a weak
pointer, so watching never keeps a pool alive: a collected pool drops out at
the next roll-up, a closed pool that something still references adds nothing,
and two pools opened under one store name are both counted and summed.

### The recorder and the baseline

The recorder is three methods: `record(name, duration)`,
`count(name, n)` and `recordRows(name, duration, n)`, a timing that also
counts the rows it read. Timings go into fixed exponential
buckets, eight per decade from 0.1 ms to 10 s plus an overflow bucket, with
count, sum and max kept exactly. A percentile is the upper edge of its bucket
(never above the true max), so it can read up to 34 percent high and never
low.

A `runtime/metrics` sampler adds live heap bytes, allocation rate, GC cycles,
the GC pause p99, goroutines and the process's CPU in cores. The CPU figure
is the user plus system time from `getrusage` over the minute's wall time,
not the runtime's `/cpu/classes` metrics, which only move at the end of a GC
cycle and so mean nothing over a minute. A metric the
running Go version does not report is left out, and a test pins the list.

Once a minute the recorder closes the minute and appends one JSON line to
`server-vitals-YYYY-MM-DD.jsonl` (UTC days) in a `vitals` folder beside the
business database. Each line holds only that minute's numbers, with the
non-empty buckets so files merge (the percentiles are left out of the file
and worked out from the buckets when read), plus `started_at`, the process start, so a
restart shows as a new value. Every line also carries `"format": 1`, the
version of the bucket layout: the buckets are indexes into this build's edges,
so changing the bounds or the buckets per decade must bump the number. The
summary rejects a line with a missing or different format, naming file and
line. Files older than seven days are removed.

### Reading it

- `GET /api/debug/vitals`, on the same tier as `/api/logs`, returns the last
  minute (with its gauges) and everything since the process started (timings
  and counts only, since a gauge is a per-minute reading), plus the last
  file-write error.
- `helmcentral vitals-summary <files...>` (or `make vitals-summary FILE=...`)
  merges files into one table: count, p50, p95, p99, max and total per key,
  then heap, GC and process CPU. Each average covers only the minutes that
  recorded that gauge; with none, it says so. Files with no lines are an error.

### When it can't write

If the minute file cannot be written, the recorder logs the error once a
minute while it keeps failing (the next line counts the failures held back),
keeps recording in memory, and keeps serving.
The endpoint shows the last error until a write succeeds. Removing files past
seven days is reported apart from that: a failed clean-up has its own log line
("cannot prune old files") and its own `last_prune_error`, and it is tried once
a day, not every minute. There is no second
destination: a different folder would hide that the data folder has a
problem.

## What was left out

- **A background loop runner.** Several loops have no timing yet. They need
  one runner, which is a separate change.
- **An outbound transport.** Nothing leaves the boat. The files stay on the
  box.
- **Memory-holder gauges.** The caches and buffers that hold memory can
  report their size later. This change records the heap as a whole.
- **A replayed boat-hour benchmark.** The files give a baseline from a real
  hour. Replaying one against a build comes after there are real files.
- **A UI and a help page.** The endpoint and the file are for whoever is
  debugging, not the operator at the helm.

## Consequences

- A store opened any other way bypasses the timing. Reviewers should reject a
  new `sql.Open("sqlite", ...)`.
- 2,000 keys is an estimate. If the overflow warning shows up in real use,
  the cap or the shapes need another look.
- `tx.lock_wait.<op>` covers every store call that locks through the shared
  helpers. Mutexes that guard memory only (caches, registries, the telemetry
  buffers) are not timed: some are taken once per SignalK delta, and timing
  them would cost more than it shows. Two SQLite-backed locks are left out:
  `alarmIgnoreStore` uses a `sync.RWMutex`, which a `lockOp` does not take, and
  `nearbyContactStore` releases its lock by hand in the middle of a method.
- A clean stop keeps its last partial minute. SIGINT and SIGTERM make `main`
  drain the HTTP server (up to 5 s, then it cuts open streams), then end the vitals context and wait
  for the final roll-up to reach disk. A crash or SIGKILL still loses up to
  a minute.
- Slow-statement log lines are limited to one per key per minute, with a count
  of the lines held back. A query's time includes the caller reading its rows.
- Percentiles are bucket estimates. Compare them as trends, not to the
  millisecond.
