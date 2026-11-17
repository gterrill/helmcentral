package main

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	_ "modernc.org/sqlite"
)

// One opener for every SQLite file (ADR 0290). Each store names itself and
// picks a profile; the opener applies that profile's settings, wraps the
// driver so every statement is timed, and registers the pool so its waits
// are sampled. The stores keep calling db.Exec, db.Query and db.Begin
// exactly as before.

// sqliteProfile is one of the four ways Helmcentral opens a database file.
// Each keeps the DSN, checks and connection limit its files had before the
// opener existed.
type sqliteProfile struct {
	name string
	// dsn turns the file path into the driver's connection string.
	dsn func(path string) string
	// maxOpenConns caps the pool.
	maxOpenConns int
	// verify reads settings back after opening.
	verify func(db *sql.DB) error
}

// sqliteProfileBusiness (below) opens the one business database, configured
// identically no matter which store (documentStore, assistantStore, nearbyContactStore, alarmLogStore,
// webPushSubscriptionStore, pluginOverridesStore) is opening it: foreign
// keys enforced, a 30-second busy timeout so one store's write never
// surfaces to another as a bare "database is locked", and WAL with
// synchronous(NORMAL) so a long document write never blocks a concurrent
// read from Mate's conversation history or the AIS sighting log - readers
// under WAL don't wait on a writer the way the default rollback journal
// does (ADR 0141; nearby_contacts.go's newNearbyContactStore carried this
// exact WAL/synchronous(NORMAL) reasoning alone before the six stores
// shared a file).
//
// Every caller keeps its own *sql.DB from its own call to openSQLite, each
// capped at one connection (the profile's maxOpenConns): modernc/sqlite
// surfaces a second concurrent writer as "database is locked" rather than
// queuing it, so capping each store's own pool at one connection makes
// database/sql do that queuing for that store's own writes, and it is WAL
// that then lets a *different* store's one-connection pool read the same
// file without waiting on it at all.
//
// Both pragmas that matter for correctness are read back and verified
// rather than trusted - modernc's own _pragma DSN parameters are otherwise
// silently unverified, the same reasoning newDocumentStore has always
// applied to foreign_keys alone.
//
// _txlock=immediate (a modernc/sqlite DSN parameter, not a _pragma - see
// sqlite.go's own doc comment on Driver.Open) makes every db.Begin() issue
// "BEGIN IMMEDIATE" instead of the default "BEGIN" (deferred): a deferred
// transaction takes no lock and no read snapshot until its first statement
// runs, and a multi-statement transaction that reads before it writes (e.g.
// AppendMessage, assistant_store.go: SELECT the conversation and its max
// seq, then INSERT the message) takes that snapshot at the SELECT. If any
// other connection commits a change before this transaction's later write
// runs, the write's attempt to upgrade a now-stale snapshot into a writer
// fails immediately with SQLITE_BUSY_SNAPSHOT - not something busy_timeout
// retries help with, since the problem isn't a lock to wait out, it's a
// snapshot that can never become valid by waiting. BEGIN IMMEDIATE closes
// that window by taking the write lock at BEGIN, before any read in the
// transaction runs: either it acquires the lock right away, or - if another
// connection already holds it - it blocks and retries exactly the way
// busy_timeout is meant to cover, and only then does the transaction's own
// reads and writes proceed against a snapshot nothing else can invalidate
// out from under it.
var sqliteProfileBusiness = sqliteProfile{
	name: "business",
	dsn: func(path string) string {
		return path + "?_pragma=foreign_keys(1)&_pragma=busy_timeout(30000)&_pragma=journal_mode(WAL)&_pragma=synchronous(NORMAL)&_txlock=immediate"
	},
	maxOpenConns: 1,
	verify:       verifyBusinessPragmas,
}

// sqliteProfileSessions is the sign-in sessions file: driver defaults, one
// connection.
var sqliteProfileSessions = sqliteProfile{
	name:         "sessions",
	dsn:          func(path string) string { return path },
	maxOpenConns: 1,
}

// sqliteProfileSecrets is the encrypted secrets file: driver defaults, one
// connection.
var sqliteProfileSecrets = sqliteProfile{
	name:         "secrets",
	dsn:          func(path string) string { return path },
	maxOpenConns: 1,
}

// sqliteProfileTiles is the map tile cache: WAL, a 5 second busy timeout,
// synchronous(NORMAL) and four connections (tileCacheMaxOpenConns), so tile
// reads do not queue behind a prefetch.
var sqliteProfileTiles = sqliteProfile{
	name: "tiles",
	dsn: func(path string) string {
		return path + "?_pragma=busy_timeout(5000)&_pragma=journal_mode(WAL)&_pragma=synchronous(NORMAL)"
	},
	maxOpenConns: tileCacheMaxOpenConns,
}

// openSQLite opens (creating the file's directory and the file if necessary)
// the database at path under profile, timed into the process-wide vitals.
// store is the short name the vitals use for it: "documents", "assistant",
// "sessions" and so on.
//
// The business profile reads foreign_keys and journal_mode back rather than
// trusting the DSN, because modernc's _pragma parameters are otherwise
// unverified.
func openSQLite(store, path string, profile sqliteProfile) (*sql.DB, error) {
	return openSQLiteIn(serverVitals, store, path, profile)
}

// openSQLiteIn is openSQLite recording into reg, so a test can open a store
// against its own registry and key budget.
func openSQLiteIn(reg *vitalsRegistry, store, path string, profile sqliteProfile) (*sql.DB, error) {
	if dir := filepath.Dir(path); dir != "" && dir != "." {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return nil, fmt.Errorf("create %s database directory: %w", store, err)
		}
	}
	db := sql.OpenDB(newTimedConnector(profile.dsn(path), newSQLTimer(store, reg)))
	db.SetMaxOpenConns(profile.maxOpenConns)
	if profile.verify != nil {
		if err := profile.verify(db); err != nil {
			db.Close()
			return nil, err
		}
	}
	reg.watchPool(store, db)
	return db, nil
}

func verifyBusinessPragmas(db *sql.DB) error {
	var fk int
	if err := db.QueryRow(`PRAGMA foreign_keys`).Scan(&fk); err != nil {
		return fmt.Errorf("check foreign_keys pragma: %w", err)
	}
	if fk != 1 {
		return fmt.Errorf("foreign_keys pragma did not take effect (got %d, want 1)", fk)
	}
	var journalMode string
	if err := db.QueryRow(`PRAGMA journal_mode`).Scan(&journalMode); err != nil {
		return fmt.Errorf("check journal_mode pragma: %w", err)
	}
	if !strings.EqualFold(journalMode, "wal") {
		return fmt.Errorf("journal_mode pragma did not take effect (got %q, want %q)", journalMode, "wal")
	}
	return nil
}

// ── SQL shapes ───────────────────────────────────────────────────────────

const (
	sqlShapeMaxLen         = 80
	sqlKeyLimit            = 2000 // about 470 literal shapes exist in production code; room for dynamic SQL
	slowStatementThreshold = 250 * time.Millisecond
)

// sqlShape is a statement's text with runs of whitespace collapsed to one
// space, cut to 80 bytes at a character boundary. It reads the statement's
// text only, never its bound arguments.
func sqlShape(query string) string {
	var b strings.Builder
	b.Grow(sqlShapeMaxLen)
	pendingSpace := false
	for i := 0; i < len(query); i++ {
		c := query[i]
		if c == ' ' || c == '\t' || c == '\n' || c == '\r' {
			pendingSpace = b.Len() > 0
			continue
		}
		extra := 1
		if pendingSpace {
			extra = 2
		}
		if b.Len()+extra > sqlShapeMaxLen {
			break
		}
		if pendingSpace {
			b.WriteByte(' ')
			pendingSpace = false
		}
		b.WriteByte(c)
	}
	s := b.String()
	// The cut may have split the last character; drop its stub.
	for i := 0; i < utf8.UTFMax && len(s) > 0; i++ {
		if r, size := utf8.DecodeLastRuneInString(s); r == utf8.RuneError && size <= 1 {
			s = s[:len(s)-1]
		}
	}
	return s
}

// sqlKeys is the registry's key budget. It turns (store, shape) into the
// vitals key and caps how many distinct keys exist, so SQL built with values
// pasted in cannot grow the recorder without bound. Past the cap a statement
// is recorded under "<store>:<overflow>", and one warning says so.
// Transactions draw on the same budget through admit.
type sqlKeys struct {
	limit  int
	logf   func(format string, args ...any)
	mu     sync.RWMutex
	known  map[string]struct{}
	warned bool
}

func newSQLKeys(limit int, logf func(format string, args ...any)) *sqlKeys {
	return &sqlKeys{limit: limit, logf: logf, known: map[string]struct{}{}}
}

func (k *sqlKeys) resolve(store, shape string) string {
	return k.admit(store+": "+shape, store+":<overflow>")
}

// admit returns key while the budget has room for it (or knows it already),
// and overflow once it does not.
func (k *sqlKeys) admit(key, overflow string) string {
	k.mu.RLock()
	_, ok := k.known[key]
	k.mu.RUnlock()
	if ok {
		return key
	}
	k.mu.Lock()
	defer k.mu.Unlock()
	if _, ok := k.known[key]; ok {
		return key
	}
	if len(k.known) < k.limit {
		k.known[key] = struct{}{}
		return key
	}
	if !k.warned {
		k.warned = true
		k.logf("server vitals: more than %d distinct keys; further ones are recorded under an <overflow> key", k.limit)
	}
	return overflow
}

// sqlTimer carries what the wrappers need to time and report a statement.
type sqlTimer struct {
	store string
	rec   vitalsRecorder
	now   func() time.Time
	logf  func(format string, args ...any)
	keys  *sqlKeys

	// The keys of the three statements every transaction runs, resolved once.
	beginKey, commitKey, rollbackKey string

	slowMu sync.Mutex
	slow   map[string]*rateLimitedLog // one limiter per key
}

// newSQLTimer times store's statements into reg, under reg's key budget.
func newSQLTimer(store string, reg *vitalsRegistry) *sqlTimer {
	return newSQLTimerWith(store, reg, reg.keys)
}

// newSQLTimerWith is newSQLTimer for a recorder and key budget of the
// caller's own.
func newSQLTimerWith(store string, rec vitalsRecorder, keys *sqlKeys) *sqlTimer {
	t := &sqlTimer{store: store, rec: rec, now: time.Now, logf: log.Printf, keys: keys}
	t.beginKey = keys.resolve(store, "BEGIN")
	t.commitKey = keys.resolve(store, "COMMIT")
	t.rollbackKey = keys.resolve(store, "ROLLBACK")
	return t
}

func (t *sqlTimer) key(query string) string { return t.keys.resolve(t.store, sqlShape(query)) }

// finish records a statement's duration under key and logs it when slow.
func (t *sqlTimer) finish(key string, start time.Time) {
	d := t.now().Sub(start)
	t.rec.record(key, d)
	t.noteSlow(key, d)
}

// finishRows is finish for a query that read n rows.
func (t *sqlTimer) finishRows(key string, start time.Time, n int64) {
	d := t.now().Sub(start)
	t.rec.recordRows(key, d, n)
	t.noteSlow(key, d)
}

// timeOp runs call and records its duration under key. A driver.ErrSkip is
// not a statement that ran, so it is passed back unrecorded.
func timeOp[T any](t *sqlTimer, key string, call func() (T, error)) (T, error) {
	start := t.now()
	res, err := call()
	if err != driver.ErrSkip {
		t.finish(key, start)
	}
	return res, err
}

// timeRows runs a call that returns rows. On success the rows are wrapped to
// stop the clock at Close; on failure the clock stops here.
func timeRows(t *sqlTimer, key string, call func() (driver.Rows, error)) (driver.Rows, error) {
	start := t.now()
	rows, err := call()
	switch {
	case err == driver.ErrSkip:
		return nil, err
	case err != nil:
		t.finish(key, start)
		return nil, err
	}
	return &timedRows{inner: rows, timer: t, key: key, start: start}, nil
}

// ── timed driver ─────────────────────────────────────────────────────────

// timedConnector opens connections on modernc's driver and wraps each one.
type timedConnector struct {
	drv   driver.Driver
	dsn   string
	timer *sqlTimer
}

var (
	sqliteDriverOnce sync.Once
	sqliteDriver     driver.Driver
)

// baseSQLiteDriver is the driver modernc registered as "sqlite".
func baseSQLiteDriver() driver.Driver {
	sqliteDriverOnce.Do(func() {
		db, err := sql.Open("sqlite", "")
		if err != nil {
			panic("modernc sqlite driver is not registered: " + err.Error())
		}
		sqliteDriver = db.Driver()
		db.Close()
	})
	return sqliteDriver
}

func newTimedConnector(dsn string, timer *sqlTimer) *timedConnector {
	return &timedConnector{drv: baseSQLiteDriver(), dsn: dsn, timer: timer}
}

func (c *timedConnector) Connect(context.Context) (driver.Conn, error) {
	inner, err := c.drv.Open(c.dsn)
	if err != nil {
		return nil, err
	}
	tc := &timedConn{inner: inner, timer: c.timer}
	var missing [5]error
	tc.prep, missing[0] = asDriver[driver.ConnPrepareContext](inner, "ConnPrepareContext")
	tc.begin, missing[1] = asDriver[driver.ConnBeginTx](inner, "ConnBeginTx")
	tc.exec, missing[2] = asDriver[driver.ExecerContext](inner, "ExecerContext")
	tc.query, missing[3] = asDriver[driver.QueryerContext](inner, "QueryerContext")
	tc.ping, missing[4] = asDriver[driver.Pinger](inner, "Pinger")
	if err := errors.Join(missing[:]...); err != nil {
		inner.Close()
		return nil, err
	}
	return tc, nil
}

func (c *timedConnector) Driver() driver.Driver { return c.drv }

// asDriver asserts that v, something modernc's driver returned, implements
// the interface T. modernc v1.34.4 does; if an upgrade drops one, opening
// fails here instead of statements going untimed.
func asDriver[T any](v any, name string) (T, error) {
	t, ok := v.(T)
	if !ok {
		return t, fmt.Errorf("sqlite driver %T does not implement driver.%s", v, name)
	}
	return t, nil
}

// timedConn forwards every optional interface modernc's connection
// implements that database/sql uses: ExecerContext, QueryerContext,
// ConnPrepareContext, ConnBeginTx and Pinger, which Connect requires, and
// SessionResetter and Validator, which pass through when present. (modernc
// has no NamedValueChecker, so none is forwarded.)
type timedConn struct {
	inner driver.Conn
	prep  driver.ConnPrepareContext
	begin driver.ConnBeginTx
	exec  driver.ExecerContext
	query driver.QueryerContext
	ping  driver.Pinger
	timer *sqlTimer
}

var (
	_ driver.Conn               = (*timedConn)(nil)
	_ driver.ExecerContext      = (*timedConn)(nil)
	_ driver.QueryerContext     = (*timedConn)(nil)
	_ driver.ConnPrepareContext = (*timedConn)(nil)
	_ driver.ConnBeginTx        = (*timedConn)(nil)
	_ driver.Pinger             = (*timedConn)(nil)
	_ driver.SessionResetter    = (*timedConn)(nil)
	_ driver.Validator          = (*timedConn)(nil)
)

// wrapStmt wraps a prepared statement s, which must carry the context
// methods the way modernc's does, to time it under key.
func (c *timedConn) wrapStmt(s driver.Stmt, key string) (driver.Stmt, error) {
	e, errE := asDriver[driver.StmtExecContext](s, "StmtExecContext")
	q, errQ := asDriver[driver.StmtQueryContext](s, "StmtQueryContext")
	if err := errors.Join(errE, errQ); err != nil {
		s.Close()
		return nil, err
	}
	return &timedStmt{inner: s, exec: e, query: q, timer: c.timer, key: key}, nil
}

func (c *timedConn) Prepare(query string) (driver.Stmt, error) {
	s, err := c.inner.Prepare(query)
	if err != nil {
		return nil, err
	}
	return c.wrapStmt(s, c.timer.key(query))
}

func (c *timedConn) PrepareContext(ctx context.Context, query string) (driver.Stmt, error) {
	s, err := c.prep.PrepareContext(ctx, query)
	if err != nil {
		return nil, err
	}
	return c.wrapStmt(s, c.timer.key(query))
}

func (c *timedConn) Close() error { return c.inner.Close() }

// timeBegin times call, a BEGIN, and wraps the transaction it opens.
func (c *timedConn) timeBegin(call func() (driver.Tx, error)) (driver.Tx, error) {
	tx, err := timeOp(c.timer, c.timer.beginKey, call)
	if err != nil {
		return nil, err
	}
	return &timedTx{inner: tx, timer: c.timer}, nil
}

func (c *timedConn) Begin() (driver.Tx, error) {
	return c.timeBegin(c.inner.Begin) //nolint:staticcheck // forwards the deprecated entry point too
}

func (c *timedConn) BeginTx(ctx context.Context, opts driver.TxOptions) (driver.Tx, error) {
	return c.timeBegin(func() (driver.Tx, error) { return c.begin.BeginTx(ctx, opts) })
}

func (c *timedConn) ExecContext(ctx context.Context, query string, args []driver.NamedValue) (driver.Result, error) {
	return timeOp(c.timer, c.timer.key(query), func() (driver.Result, error) { return c.exec.ExecContext(ctx, query, args) })
}

func (c *timedConn) QueryContext(ctx context.Context, query string, args []driver.NamedValue) (driver.Rows, error) {
	return timeRows(c.timer, c.timer.key(query), func() (driver.Rows, error) { return c.query.QueryContext(ctx, query, args) })
}

func (c *timedConn) Ping(ctx context.Context) error { return c.ping.Ping(ctx) }

func (c *timedConn) ResetSession(ctx context.Context) error {
	if r, ok := c.inner.(driver.SessionResetter); ok {
		return r.ResetSession(ctx)
	}
	return nil
}

func (c *timedConn) IsValid() bool {
	if v, ok := c.inner.(driver.Validator); ok {
		return v.IsValid()
	}
	return true
}

// timedStmt times a prepared statement's Exec and Query under the text it
// was prepared from.
type timedStmt struct {
	inner driver.Stmt
	exec  driver.StmtExecContext
	query driver.StmtQueryContext
	timer *sqlTimer
	key   string
}

var (
	_ driver.Stmt             = (*timedStmt)(nil)
	_ driver.StmtExecContext  = (*timedStmt)(nil)
	_ driver.StmtQueryContext = (*timedStmt)(nil)
)

func (s *timedStmt) Close() error  { return s.inner.Close() }
func (s *timedStmt) NumInput() int { return s.inner.NumInput() }

// Exec and Query are the deprecated entry points database/sql does not use
// once a statement has the context methods; they forward for completeness.
func (s *timedStmt) Exec(args []driver.Value) (driver.Result, error) {
	return timeOp(s.timer, s.key, func() (driver.Result, error) { return s.inner.Exec(args) }) //nolint:staticcheck // forwards the deprecated entry point too
}

func (s *timedStmt) Query(args []driver.Value) (driver.Rows, error) {
	return timeRows(s.timer, s.key, func() (driver.Rows, error) { return s.inner.Query(args) }) //nolint:staticcheck // forwards the deprecated entry point too
}

func (s *timedStmt) ExecContext(ctx context.Context, args []driver.NamedValue) (driver.Result, error) {
	return timeOp(s.timer, s.key, func() (driver.Result, error) { return s.exec.ExecContext(ctx, args) })
}

func (s *timedStmt) QueryContext(ctx context.Context, args []driver.NamedValue) (driver.Rows, error) {
	return timeRows(s.timer, s.key, func() (driver.Rows, error) { return s.query.QueryContext(ctx, args) })
}

// timedRows stops a query's clock at Close, which is when the caller has
// finished with the rows, and counts the rows it read.
type timedRows struct {
	inner  driver.Rows
	timer  *sqlTimer
	key    string
	start  time.Time
	n      int64
	closed bool
}

var (
	_ driver.Rows                           = (*timedRows)(nil)
	_ driver.RowsColumnTypeDatabaseTypeName = (*timedRows)(nil)
	_ driver.RowsColumnTypeLength           = (*timedRows)(nil)
	_ driver.RowsColumnTypeNullable         = (*timedRows)(nil)
	_ driver.RowsColumnTypePrecisionScale   = (*timedRows)(nil)
	_ driver.RowsColumnTypeScanType         = (*timedRows)(nil)
)

func (r *timedRows) Columns() []string { return r.inner.Columns() }

func (r *timedRows) Next(dest []driver.Value) error {
	err := r.inner.Next(dest)
	if err == nil {
		r.n++
	}
	return err
}

func (r *timedRows) Close() error {
	err := r.inner.Close()
	if !r.closed {
		r.closed = true
		r.timer.finishRows(r.key, r.start, r.n)
	}
	return err
}

func (r *timedRows) ColumnTypeDatabaseTypeName(i int) string {
	if v, ok := r.inner.(driver.RowsColumnTypeDatabaseTypeName); ok {
		return v.ColumnTypeDatabaseTypeName(i)
	}
	return ""
}

func (r *timedRows) ColumnTypeLength(i int) (int64, bool) {
	if v, ok := r.inner.(driver.RowsColumnTypeLength); ok {
		return v.ColumnTypeLength(i)
	}
	return 0, false
}

func (r *timedRows) ColumnTypeNullable(i int) (bool, bool) {
	if v, ok := r.inner.(driver.RowsColumnTypeNullable); ok {
		return v.ColumnTypeNullable(i)
	}
	return false, false
}

func (r *timedRows) ColumnTypePrecisionScale(i int) (int64, int64, bool) {
	if v, ok := r.inner.(driver.RowsColumnTypePrecisionScale); ok {
		return v.ColumnTypePrecisionScale(i)
	}
	return 0, 0, false
}

func (r *timedRows) ColumnTypeScanType(i int) reflect.Type {
	if v, ok := r.inner.(driver.RowsColumnTypeScanType); ok {
		return v.ColumnTypeScanType(i)
	}
	return reflect.TypeOf((*any)(nil)).Elem()
}

// timedTx times Commit and Rollback.
type timedTx struct {
	inner driver.Tx
	timer *sqlTimer
}

func (t *timedTx) Commit() error {
	_, err := timeOp(t.timer, t.timer.commitKey, func() (struct{}, error) { return struct{}{}, t.inner.Commit() })
	return err
}

func (t *timedTx) Rollback() error {
	_, err := timeOp(t.timer, t.timer.rollbackKey, func() (struct{}, error) { return struct{}{}, t.inner.Rollback() })
	return err
}

// noteSlow logs a statement that took slowStatementThreshold or longer.
func (t *sqlTimer) noteSlow(key string, d time.Duration) {
	if d < slowStatementThreshold {
		return
	}
	t.slowMu.Lock()
	l := t.slow[key]
	if l == nil {
		if t.slow == nil {
			t.slow = map[string]*rateLimitedLog{}
		}
		l = &rateLimitedLog{now: t.now, logf: t.logf, interval: time.Minute}
		t.slow[key] = l
	}
	t.slowMu.Unlock()
	l.log(" (%d suppressed since the last line)", "slow statement (query time includes reading rows): %s took %s", key, d.Round(time.Millisecond))
}
