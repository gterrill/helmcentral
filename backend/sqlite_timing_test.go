package main

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"fmt"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

// fakeVitals records what the connector reports, so a test can read it back
// without going through the process-wide registry.
type fakeVitals struct {
	mu      sync.Mutex
	timings map[string][]time.Duration
	counts  map[string]int64
}

func newFakeVitals() *fakeVitals {
	return &fakeVitals{timings: map[string][]time.Duration{}, counts: map[string]int64{}}
}

func (f *fakeVitals) record(name string, d time.Duration) {
	f.mu.Lock()
	f.timings[name] = append(f.timings[name], d)
	f.mu.Unlock()
}
func (f *fakeVitals) count(name string, n int64) {
	f.mu.Lock()
	f.counts[name] += n
	f.mu.Unlock()
}
func (f *fakeVitals) recordRows(name string, d time.Duration, n int64) {
	f.record(name, d)
	f.count("rows: "+name, n)
}

func (f *fakeVitals) only(t *testing.T, name string) time.Duration {
	t.Helper()
	f.mu.Lock()
	defer f.mu.Unlock()
	got := f.timings[name]
	if len(got) != 1 {
		t.Fatalf("timings[%q] = %v, want exactly one (have keys %v)", name, got, f.keysLocked())
	}
	return got[0]
}

func (f *fakeVitals) keysLocked() []string {
	var k []string
	for name := range f.timings {
		k = append(k, name)
	}
	return k
}

// stepClock is a clock a test drives: every read returns t, then moves it on
// by step.
type stepClock struct {
	t    time.Time
	step time.Duration
}

func (c *stepClock) now() time.Time {
	v := c.t
	c.t = c.t.Add(c.step)
	return v
}

type logCapture struct {
	mu    sync.Mutex
	lines []string
}

func (l *logCapture) logf(format string, args ...any) {
	l.mu.Lock()
	l.lines = append(l.lines, fmt.Sprintf(format, args...))
	l.mu.Unlock()
}

func newTestTimedDB(t *testing.T, store, dsn string, clock *stepClock, rec vitalsRecorder, logs *logCapture, keyLimit int) *sql.DB {
	t.Helper()
	timer := newSQLTimerWith(store, rec, newSQLKeys(keyLimit, logs.logf))
	timer.now, timer.logf = clock.now, logs.logf
	db := sql.OpenDB(newTimedConnector(dsn, timer))
	db.SetMaxOpenConns(1)
	t.Cleanup(func() { db.Close() })
	return db
}

func TestSQLShape_CollapsesWhitespaceAndTruncates(t *testing.T) {
	got := sqlShape("  SELECT   a,\n\tb\n FROM   t  WHERE x = ?  ")
	if want := "SELECT a, b FROM t WHERE x = ?"; got != want {
		t.Errorf("sqlShape = %q, want %q", got, want)
	}
	long := "SELECT " + strings.Repeat("column_name, ", 40) + "z FROM t"
	got = sqlShape(long)
	if len(got) > 80 {
		t.Errorf("len(sqlShape(long)) = %d, want <= 80", len(got))
	}
	if !strings.HasPrefix(got, "SELECT column_name, column_name,") {
		t.Errorf("sqlShape(long) = %q, want the statement's own start", got)
	}
}

func TestSQLShape_TruncatesOnARuneBoundary(t *testing.T) {
	got := sqlShape("SELECT '" + strings.Repeat("é", 100) + "'")
	if strings.ContainsRune(got, '�') {
		t.Errorf("sqlShape cut a rune in half: %q", got)
	}
}

func TestSQLKeys_CapsDistinctKeysAndRecordsOverflowVisibly(t *testing.T) {
	logs := &logCapture{}
	keys := newSQLKeys(3, logs.logf)
	for i := 0; i < 3; i++ {
		if got, want := keys.resolve("docs", fmt.Sprintf("SELECT %d", i)), fmt.Sprintf("docs: SELECT %d", i); got != want {
			t.Fatalf("resolve = %q, want %q", got, want)
		}
	}
	// A known key keeps resolving to itself after the cap is reached.
	if got := keys.resolve("docs", "SELECT 1"); got != "docs: SELECT 1" {
		t.Errorf("known key after cap = %q", got)
	}
	if len(logs.lines) != 0 {
		t.Fatalf("warned before the cap was exceeded: %v", logs.lines)
	}
	for i := 3; i < 6; i++ {
		if got := keys.resolve("docs", fmt.Sprintf("SELECT %d", i)); got != "docs:<overflow>" {
			t.Errorf("overflow key = %q, want docs:<overflow>", got)
		}
	}
	if got := keys.resolve("assistant", "SELECT 99"); got != "assistant:<overflow>" {
		t.Errorf("overflow key for another store = %q", got)
	}
	if len(logs.lines) != 1 {
		t.Errorf("want exactly one warning, got %v", logs.lines)
	}
}

func TestTimedDB_ExecIsRecordedUnderStoreAndShape(t *testing.T) {
	rec, logs := newFakeVitals(), &logCapture{}
	clock := &stepClock{t: time.Unix(1000, 0), step: 7 * time.Millisecond}
	db := newTestTimedDB(t, "docs", filepath.Join(t.TempDir(), "a.sqlite"), clock, rec, logs, 500)

	if _, err := db.Exec("CREATE TABLE   t (id INTEGER, name TEXT)"); err != nil {
		t.Fatal(err)
	}
	if got := rec.only(t, "docs: CREATE TABLE t (id INTEGER, name TEXT)"); got != 7*time.Millisecond {
		t.Errorf("exec duration = %v, want 7ms", got)
	}
	if _, err := db.Exec("INSERT INTO t VALUES (?, ?)", 1, "secret-value"); err != nil {
		t.Fatal(err)
	}
	for _, k := range rec.keysLocked() {
		if strings.Contains(k, "secret-value") {
			t.Errorf("key %q carries an argument value", k)
		}
	}
	rec.only(t, "docs: INSERT INTO t VALUES (?, ?)")
}

func TestTimedDB_QueryClockStopsAtRowsClose(t *testing.T) {
	rec, logs := newFakeVitals(), &logCapture{}
	clock := &stepClock{t: time.Unix(1000, 0)}
	db := newTestTimedDB(t, "docs", filepath.Join(t.TempDir(), "a.sqlite"), clock, rec, logs, 500)
	if _, err := db.Exec("CREATE TABLE t (id INTEGER)"); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec("INSERT INTO t VALUES (1), (2), (3)"); err != nil {
		t.Fatal(err)
	}

	rows, err := db.Query("SELECT id FROM t")
	if err != nil {
		t.Fatal(err)
	}
	clock.t = clock.t.Add(40 * time.Millisecond) // the caller takes its time reading
	for rows.Next() {
	}
	// database/sql closes drained rows itself, so the clock stops there.
	if err := rows.Close(); err != nil {
		t.Fatal(err)
	}

	if got := rec.only(t, "docs: SELECT id FROM t"); got != 40*time.Millisecond {
		t.Errorf("query duration = %v, want 40ms (start to rows.Close, not just to the driver's return)", got)
	}
	if got := rec.counts["rows: docs: SELECT id FROM t"]; got != 3 {
		t.Errorf("rows returned = %d, want 3", got)
	}
}

func TestTimedDB_SlowStatementIsLoggedOnce(t *testing.T) {
	rec, logs := newFakeVitals(), &logCapture{}
	clock := &stepClock{t: time.Unix(1000, 0), step: 300 * time.Millisecond}
	db := newTestTimedDB(t, "alarm_log", filepath.Join(t.TempDir(), "a.sqlite"), clock, rec, logs, 500)

	if _, err := db.Exec("CREATE TABLE t (id INTEGER)"); err != nil {
		t.Fatal(err)
	}
	if len(logs.lines) != 1 {
		t.Fatalf("want one slow-statement line, got %v", logs.lines)
	}
	for _, want := range []string{"alarm_log", "CREATE TABLE t (id INTEGER)", "300ms"} {
		if !strings.Contains(logs.lines[0], want) {
			t.Errorf("slow line %q is missing %q", logs.lines[0], want)
		}
	}
}

func TestTimedDB_SlowStatementLogIsLimitedToOnePerKeyPerMinute(t *testing.T) {
	rec, logs := newFakeVitals(), &logCapture{}
	clock := &stepClock{t: time.Unix(1000, 0), step: 300 * time.Millisecond}
	db := newTestTimedDB(t, "docs", filepath.Join(t.TempDir(), "a.sqlite"), clock, rec, logs, 500)
	if _, err := db.Exec("CREATE TABLE t (id INTEGER)"); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 3; i++ {
		if _, err := db.Exec("DELETE FROM t"); err != nil {
			t.Fatal(err)
		}
	}
	// One line for CREATE, one for the first DELETE; the other two DELETEs are held back.
	if len(logs.lines) != 2 {
		t.Fatalf("want 2 lines within a minute, got %v", logs.lines)
	}
	if !strings.Contains(logs.lines[0], "slow statement (query time includes reading rows)") {
		t.Errorf("line wording: %q", logs.lines[0])
	}
	clock.t = clock.t.Add(time.Minute)
	if _, err := db.Exec("DELETE FROM t"); err != nil {
		t.Fatal(err)
	}
	if len(logs.lines) != 3 || !strings.Contains(logs.lines[2], "2 suppressed") {
		t.Errorf("next line should report 2 suppressed: %v", logs.lines)
	}
}

func TestTimedDB_FastStatementIsNotLogged(t *testing.T) {
	rec, logs := newFakeVitals(), &logCapture{}
	clock := &stepClock{t: time.Unix(1000, 0), step: 249 * time.Millisecond}
	db := newTestTimedDB(t, "docs", filepath.Join(t.TempDir(), "a.sqlite"), clock, rec, logs, 500)
	if _, err := db.Exec("CREATE TABLE t (id INTEGER)"); err != nil {
		t.Fatal(err)
	}
	if len(logs.lines) != 0 {
		t.Errorf("logged a statement under the threshold: %v", logs.lines)
	}
}

func TestTimedDB_TransactionAndPreparedStatementAreTimed(t *testing.T) {
	rec, logs := newFakeVitals(), &logCapture{}
	clock := &stepClock{t: time.Unix(1000, 0), step: time.Millisecond}
	db := newTestTimedDB(t, "docs", filepath.Join(t.TempDir(), "a.sqlite"), clock, rec, logs, 500)
	if _, err := db.Exec("CREATE TABLE t (id INTEGER)"); err != nil {
		t.Fatal(err)
	}

	tx, err := db.Begin()
	if err != nil {
		t.Fatal(err)
	}
	stmt, err := tx.Prepare("INSERT INTO t VALUES (?)")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := stmt.Exec(1); err != nil {
		t.Fatal(err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
	tx, err = db.Begin()
	if err != nil {
		t.Fatal(err)
	}
	if err := tx.Rollback(); err != nil {
		t.Fatal(err)
	}

	for _, k := range []string{"docs: INSERT INTO t VALUES (?)"} {
		rec.only(t, k)
	}
	rec.mu.Lock()
	defer rec.mu.Unlock()
	if n := len(rec.timings["docs: BEGIN"]); n != 2 {
		t.Errorf("BEGIN recorded %d times, want 2", n)
	}
	if n := len(rec.timings["docs: COMMIT"]); n != 1 {
		t.Errorf("COMMIT recorded %d times, want 1", n)
	}
	if n := len(rec.timings["docs: ROLLBACK"]); n != 1 {
		t.Errorf("ROLLBACK recorded %d times, want 1", n)
	}
}

func TestTimedDB_ForwardsDriverInterfaces(t *testing.T) {
	rec, logs := newFakeVitals(), &logCapture{}
	clock := &stepClock{t: time.Unix(1000, 0)}
	db := newTestTimedDB(t, "docs", filepath.Join(t.TempDir(), "a.sqlite"), clock, rec, logs, 500)
	ctx := context.Background()

	if err := db.PingContext(ctx); err != nil {
		t.Fatalf("Pinger: %v", err)
	}
	if _, err := db.ExecContext(ctx, "CREATE TABLE t (id INTEGER NOT NULL, label VARCHAR(10))"); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, "INSERT INTO t VALUES (1, 'x')"); err != nil {
		t.Fatal(err)
	}
	rows, err := db.QueryContext(ctx, "SELECT id, label FROM t")
	if err != nil {
		t.Fatal(err)
	}
	cols, err := rows.ColumnTypes()
	if err != nil {
		t.Fatalf("column types: %v", err)
	}
	if got := cols[0].DatabaseTypeName(); got != "INTEGER" {
		t.Errorf("DatabaseTypeName = %q, want INTEGER", got)
	}
	if got, ok := cols[1].Length(); !ok || got != 10 {
		// modernc reports the declared length through ColumnTypeLength.
		t.Logf("Length = %d, %v", got, ok)
	}
	if cols[0].ScanType() == nil {
		t.Error("ScanType is nil")
	}
	rows.Close()

	// A connection a caller marks bad through Raw is dropped and replaced.
	conn, err := db.Conn(ctx)
	if err != nil {
		t.Fatal(err)
	}
	_ = conn.Raw(func(any) error { return driver.ErrBadConn })
	conn.Close()
	if err := db.PingContext(ctx); err != nil {
		t.Fatalf("pool did not recover after a discarded connection: %v", err)
	}
	var n int
	if err := db.QueryRowContext(ctx, "SELECT COUNT(*) FROM t").Scan(&n); err != nil || n != 1 {
		t.Fatalf("count = %d, %v", n, err)
	}
}

func TestOpenSQLite_BusinessProfileKeepsItsSettings(t *testing.T) {
	reg := newVitalsRegistry(time.Now)

	// The opener makes the file's directory too.
	path := filepath.Join(t.TempDir(), "state", "business.sqlite")
	db, err := openSQLiteIn(reg, "optest_business", path, sqliteProfileBusiness)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	assertPragma(t, db, "foreign_keys", "1")
	assertPragma(t, db, "journal_mode", "wal")
	assertPragma(t, db, "busy_timeout", "30000")
	assertPragma(t, db, "synchronous", "1")
	if got := db.Stats().MaxOpenConnections; got != 1 {
		t.Errorf("MaxOpenConnections = %d, want 1", got)
	}

	// _txlock=immediate: Begin takes the write lock, so a second connection
	// cannot write while the transaction is open, even before its first
	// statement runs.
	if _, err := db.Exec("CREATE TABLE t (id INTEGER)"); err != nil {
		t.Fatal(err)
	}
	tx, err := db.Begin()
	if err != nil {
		t.Fatal(err)
	}
	other, err := sql.Open("sqlite", path+"?_pragma=busy_timeout(100)")
	if err != nil {
		t.Fatal(err)
	}
	defer other.Close()
	if _, err := other.Exec("INSERT INTO t VALUES (1)"); err == nil || !strings.Contains(err.Error(), "locked") {
		t.Errorf("second writer during an open transaction: err = %v, want a locked error", err)
	}
	if err := tx.Rollback(); err != nil {
		t.Fatal(err)
	}
	if _, err := other.Exec("INSERT INTO t VALUES (1)"); err != nil {
		t.Errorf("second writer after rollback: %v", err)
	}

	if _, ok := reg.timing("optest_business: CREATE TABLE t (id INTEGER)"); !ok {
		t.Error("the opener's statements are not reaching the registry")
	}
}

func TestVitalsRegistry_KeyBudgetCoversTransactionKeys(t *testing.T) {
	reg := newVitalsRegistry(time.Now)
	reg.keys = newSQLKeys(2, t.Logf)
	for _, op := range []string{"a", "b", "c", "d"} {
		reg.record(reg.keys.admit("tx.time."+op, "tx.time.<overflow>"), time.Millisecond)
	}
	snap := reg.snapshot().SinceStart.Timings
	if len(snap) != 3 || snap["tx.time.<overflow>"].Count != 2 {
		t.Errorf("timings = %v, want tx.time.a, tx.time.b and an overflow key holding 2", snap)
	}
}

func TestBucketQuantile(t *testing.T) {
	if got := bucketQuantile([]uint64{0, 0, 0}, 0.99); got != -1 {
		t.Errorf("empty = %d, want -1", got)
	}
	if got := bucketQuantile([]uint64{1, 0, 98, 1}, 0.5); got != 2 {
		t.Errorf("p50 = %d, want 2", got)
	}
	if got := bucketQuantile([]uint64{1, 0, 98, 1}, 1); got != 3 {
		t.Errorf("p100 = %d, want 3", got)
	}
}

func TestOpenSQLite_OtherProfilesKeepTheirSettings(t *testing.T) {
	cases := []struct {
		profile  sqliteProfile
		store    string
		wantConn int
		fk       string
		journal  string
		busy     string
		sync     string
	}{
		{sqliteProfileSessions, "optest_sessions", 1, "0", "delete", "0", "2"},
		{sqliteProfileSecrets, "optest_secrets", 1, "0", "delete", "0", "2"},
		{sqliteProfileTiles, "optest_tiles", 4, "0", "wal", "5000", "1"},
	}
	for _, tc := range cases {
		t.Run(tc.store, func(t *testing.T) {
			db, err := openSQLite(tc.store, filepath.Join(t.TempDir(), "x.sqlite"), tc.profile)
			if err != nil {
				t.Fatal(err)
			}
			defer db.Close()
			if got := db.Stats().MaxOpenConnections; got != tc.wantConn {
				t.Errorf("MaxOpenConnections = %d, want %d", got, tc.wantConn)
			}
			assertPragma(t, db, "foreign_keys", tc.fk)
			assertPragma(t, db, "journal_mode", tc.journal)
			assertPragma(t, db, "busy_timeout", tc.busy)
			assertPragma(t, db, "synchronous", tc.sync)
		})
	}
}

func assertPragma(t *testing.T, db *sql.DB, name, want string) {
	t.Helper()
	var got string
	if err := db.QueryRow("PRAGMA " + name).Scan(&got); err != nil {
		t.Fatalf("PRAGMA %s: %v", name, err)
	}
	if !strings.EqualFold(got, want) {
		t.Errorf("PRAGMA %s = %q, want %q", name, got, want)
	}
}

// BenchmarkSQLExec compares one trivial statement through the plain driver
// and through the timed one; the difference is the per-statement overhead.
func BenchmarkSQLExec(b *testing.B) {
	run := func(b *testing.B, db *sql.DB) {
		db.SetMaxOpenConns(1)
		if _, err := db.Exec("CREATE TABLE t (id INTEGER)"); err != nil {
			b.Fatal(err)
		}
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			if _, err := db.Exec("DELETE FROM t WHERE id = ?", i); err != nil {
				b.Fatal(err)
			}
		}
	}
	b.Run("plain", func(b *testing.B) {
		db, err := sql.Open("sqlite", filepath.Join(b.TempDir(), "p.sqlite"))
		if err != nil {
			b.Fatal(err)
		}
		defer db.Close()
		run(b, db)
	})
	b.Run("timed", func(b *testing.B) {
		reg := newVitalsRegistry(time.Now)
		timer := newSQLTimer("bench", reg)
		db := sql.OpenDB(newTimedConnector(filepath.Join(b.TempDir(), "t.sqlite"), timer))
		defer db.Close()
		run(b, db)
	})
}
