package main

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/labstack/echo/v4"
)

// vitalsClock is a clock the test moves by hand.
type vitalsClock struct {
	mu sync.Mutex
	t  time.Time
}

func (c *vitalsClock) now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.t
}

func (c *vitalsClock) advance(d time.Duration) {
	c.mu.Lock()
	c.t = c.t.Add(d)
	c.mu.Unlock()
}

func newTestVitals(t *testing.T, dir string) (*vitalsRegistry, *vitalsClock) {
	t.Helper()
	clock := &vitalsClock{t: time.Date(2026, 11, 17, 23, 58, 30, 0, time.UTC)}
	v := newVitalsRegistry(clock.now)
	if dir != "" {
		v.writer = &vitalsFileWriter{dir: dir, keepDays: vitalsFileKeepDays}
	}
	return v, clock
}

func within(t *testing.T, label string, got, want time.Duration) {
	t.Helper()
	// The buckets are 8 per decade, so an estimate is at most 10^(1/8) = 1.34x
	// above the true value and never below it.
	if got < want || float64(got) > float64(want)*1.34 {
		t.Errorf("%s = %v, want between %v and %v", label, got, want, time.Duration(float64(want)*1.34))
	}
}

func TestHistogram_PercentilesAreBucketEstimatesNeverBelowTruth(t *testing.T) {
	var h histogram
	for i := 1; i <= 100; i++ {
		h.add(time.Duration(i) * time.Millisecond)
	}
	if h.n != 100 {
		t.Fatalf("count = %d", h.n)
	}
	within(t, "p50", h.quantile(0.50), 50*time.Millisecond)
	within(t, "p95", h.quantile(0.95), 95*time.Millisecond)
	within(t, "p99", h.quantile(0.99), 99*time.Millisecond)
	if got := h.quantile(1); got != 100*time.Millisecond {
		t.Errorf("p100 = %v, want the exact max 100ms", got)
	}
	if got, want := time.Duration(h.sum), 5050*time.Millisecond; got != want {
		t.Errorf("sum = %v, want %v", got, want)
	}
}

func TestHistogram_EdgesAndOverflow(t *testing.T) {
	var h histogram
	h.add(10 * time.Microsecond) // below 0.1 ms: first bucket
	h.add(10 * time.Second)      // the last edge
	h.add(25 * time.Second)      // overflow
	if h.counts[0] != 1 || h.counts[vitalsBoundCount-1] != 1 || h.counts[vitalsBoundCount] != 1 {
		t.Errorf("bucket counts = first %d, last edge %d, overflow %d, want 1 each",
			h.counts[0], h.counts[vitalsBoundCount-1], h.counts[vitalsBoundCount])
	}
	if got := h.quantile(0.99); got != 25*time.Second {
		t.Errorf("p99 with an overflow sample = %v, want the max 25s", got)
	}
	if got := vitalsBounds[vitalsBoundCount-1]; got != 10*time.Second {
		t.Errorf("last bound = %v, want 10s", got)
	}
	if got := vitalsBounds[0]; got != 100*time.Microsecond {
		t.Errorf("first bound = %v, want 0.1ms", got)
	}
}

func TestVitals_RollupHoldsOnlyThatMinutesNumbers(t *testing.T) {
	dir := t.TempDir()
	v, clock := newTestVitals(t, dir)

	v.record("db.x", 5*time.Millisecond)
	v.record("db.x", 7*time.Millisecond)
	v.count("hits", 3)
	clock.advance(30 * time.Second)
	v.rollup(clock.now())

	v.record("db.x", 9*time.Millisecond)
	clock.advance(time.Minute)
	v.rollup(clock.now())

	snap := v.snapshot()
	if got := snap.LastMinute.Timings["db.x"].Count; got != 1 {
		t.Errorf("last minute count = %d, want only that minute's 1", got)
	}
	if _, ok := snap.LastMinute.Counts["hits"]; ok && snap.LastMinute.Counts["hits"] != 0 {
		t.Errorf("last minute carries the previous minute's counter: %v", snap.LastMinute.Counts)
	}
	if got := snap.SinceStart.Timings["db.x"].Count; got != 3 {
		t.Errorf("since start count = %d, want 3", got)
	}
	if got := snap.SinceStart.Counts["hits"]; got != 3 {
		t.Errorf("since start hits = %d, want 3", got)
	}
	if !snap.StartedAt.Equal(time.Date(2026, 11, 17, 23, 58, 30, 0, time.UTC)) {
		t.Errorf("started_at = %v", snap.StartedAt)
	}
	if got := snap.LastMinute.Seconds; got != 60 {
		t.Errorf("last minute seconds = %v, want 60", got)
	}
}

func TestVitals_SinceStartIncludesTheMinuteInProgress(t *testing.T) {
	v, _ := newTestVitals(t, "")
	v.record("db.x", time.Millisecond)
	snap := v.snapshot()
	if snap.LastMinute != nil {
		t.Errorf("last_minute = %+v before any roll-up, want null", snap.LastMinute)
	}
	if got := snap.SinceStart.Timings["db.x"].Count; got != 1 {
		t.Errorf("since start = %d, want 1", got)
	}
}

func TestVitals_RuntimeFiguresArePresent(t *testing.T) {
	v, clock := newTestVitals(t, "")
	clock.advance(time.Minute)
	v.rollup(clock.now())
	g := v.snapshot().LastMinute.Gauges
	for _, k := range []string{"runtime.heap_live_bytes", "runtime.alloc_bytes_per_s", "runtime.goroutines", "runtime.gc_pause_p99_ms", "runtime.cpu_cores"} {
		if _, ok := g[k]; !ok {
			t.Errorf("gauge %s missing (have %v)", k, g)
		}
	}
	if g["runtime.heap_live_bytes"] <= 0 || g["runtime.goroutines"] < 1 {
		t.Errorf("runtime gauges look empty: %v", g)
	}
	if _, ok := v.snapshot().LastMinute.Counts["runtime.gc_cycles"]; !ok {
		t.Error("runtime.gc_cycles missing from counts")
	}
}

func openOneConnPool(t *testing.T) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite", filepath.Join(t.TempDir(), "p.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	db.SetMaxOpenConns(1)
	t.Cleanup(func() { db.Close() })
	return db
}

// waitOnce makes db's pool record one wait for a connection.
func waitOnce(t *testing.T, db *sql.DB) {
	t.Helper()
	conn, err := db.Conn(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	done := make(chan struct{})
	go func() {
		c2, err := db.Conn(t.Context())
		if err == nil {
			c2.Close()
		}
		close(done)
	}()
	for db.Stats().WaitCount == 0 {
		time.Sleep(time.Millisecond)
	}
	time.Sleep(5 * time.Millisecond)
	conn.Close()
	<-done
}

func TestVitals_TwoPoolsUnderOneStoreNameBothCount(t *testing.T) {
	v, clock := newTestVitals(t, "")
	first, second := openOneConnPool(t), openOneConnPool(t)
	v.watchPool("docs", first)
	v.watchPool("docs", second)
	waitOnce(t, first)
	waitOnce(t, second)
	clock.advance(time.Minute)
	v.rollup(clock.now())
	if got := v.snapshot().LastMinute.Counts["db.pool_wait_count.docs"]; got != 2 {
		t.Errorf("pool wait count for docs = %d, want 2 (one from each pool)", got)
	}
}

func TestVitals_ACollectedPoolDropsOutAtTheNextRollup(t *testing.T) {
	v, clock := newTestVitals(t, "")
	keep := openOneConnPool(t)
	v.watchPool("keep", keep)
	func() {
		db, err := sql.Open("sqlite", filepath.Join(t.TempDir(), "gone.sqlite"))
		if err != nil {
			t.Fatal(err)
		}
		v.watchPool("gone", db)
		db.Close() // stops the pool's own goroutine, which would otherwise hold it
	}()
	for range 5 {
		runtime.GC()
	}
	clock.advance(time.Minute)
	v.rollup(clock.now())
	if len(v.pools) != 1 {
		t.Fatalf("watched pools = %d after the other was collected, want 1", len(v.pools))
	}
	runtime.KeepAlive(keep)
}

func TestVitals_AClosedButReferencedPoolAddsNothing(t *testing.T) {
	v, clock := newTestVitals(t, "")
	db := openOneConnPool(t)
	v.watchPool("closed", db)
	waitOnce(t, db)
	clock.advance(time.Minute)
	v.rollup(clock.now())
	db.Close()
	clock.advance(time.Minute)
	v.rollup(clock.now())
	if got := v.snapshot().LastMinute.Counts["db.pool_wait_count.closed"]; got != 0 {
		t.Errorf("closed pool contributed %d, want 0", got)
	}
}

func TestRuntimeSampler_CPUCoresComeFromProcessCPUTime(t *testing.T) {
	t0 := time.Date(2026, 11, 17, 12, 0, 0, 0, time.UTC)
	cpu := 10 * time.Second
	s := newRuntimeSampler(t0, func() (time.Duration, error) { return cpu, nil })
	cpu += 30 * time.Second
	gauges, _ := s.sample(t0.Add(time.Minute))
	if got := gauges["runtime.cpu_cores"]; got < 0.499 || got > 0.501 {
		t.Errorf("cpu cores = %v, want 0.5 (30s of CPU in a 60s minute)", got)
	}

	failing := newRuntimeSampler(t0, func() (time.Duration, error) { return 0, errors.New("no rusage") })
	if _, ok := func() (float64, bool) {
		g, _ := failing.sample(t0.Add(time.Minute))
		v, ok := g["runtime.cpu_cores"]
		return v, ok
	}(); ok {
		t.Error("cpu figure reported although the reading failed")
	}
}

func TestVitals_ARollupInFlightNeverHidesTheMinuteFromASnapshot(t *testing.T) {
	v, clock := newTestVitals(t, "")
	v.record("db.x", time.Millisecond)
	v.count("hits", 4)
	var midCount uint64
	var midHits int64
	v.rollupHook = func() {
		s := v.snapshot()
		midCount = s.SinceStart.Timings["db.x"].Count
		midHits = s.SinceStart.Counts["hits"]
	}
	clock.advance(time.Minute)
	v.rollup(clock.now())
	if midCount != 1 || midHits != 4 {
		t.Errorf("snapshot during the roll-up saw count %d and hits %d, want 1 and 4", midCount, midHits)
	}
}

func TestVitals_FileLinesCarryBucketsNotPercentiles(t *testing.T) {
	dir := t.TempDir()
	v, clock := newTestVitals(t, dir)
	v.record("db.x", 5*time.Millisecond)
	clock.advance(time.Minute)
	v.rollup(clock.now())
	b, err := os.ReadFile(filepath.Join(dir, "server-vitals-2026-11-17.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(b), `"p50_ms"`) || strings.Contains(string(b), `"p99_ms"`) {
		t.Errorf("file line carries percentiles: %s", b)
	}
	if !strings.Contains(string(b), `"b":[[`) {
		t.Errorf("file line lost its buckets: %s", b)
	}
	if got := v.snapshot().SinceStart.Timings["db.x"].P50Ms; got == 0 {
		t.Error("endpoint lost its percentiles")
	}
}

var (
	vitalsTestLockForTx = newLockOp("vitals-test-lockfortx")
	vitalsTestOp        = newLockOp("vitals-test-op")
	vitalsTestCoastOp   = newLockOp("vitals-coast-op")
	vitalsTestMCPOp     = newLockOp("vitals-mcp-op")
)

func TestHoldForTx_RecordsTheWaitForEveryCaller(t *testing.T) {
	reg := recordLocksInto(t)
	s := newTestDocumentStore(t)
	reached := make(chan struct{}, 1)
	s.lockWaitHook = func() { reached <- struct{}{} }
	s.mu.Lock()
	done := make(chan struct{})
	go func() {
		s.holdForTx(vitalsTestLockForTx).release()
		close(done)
	}()
	<-reached
	time.Sleep(40 * time.Millisecond)
	s.mu.Unlock()
	<-done
	if w, ok := reg.timing("tx.lock_wait.vitals-test-lockfortx"); !ok || w.MaxMs < 30 {
		t.Errorf("lock wait = %+v (found %v), want at least 30ms", w, ok)
	}
}

func TestVitals_PoolWaitsAreSampledPerMinute(t *testing.T) {
	v, clock := newTestVitals(t, "")
	db := openOneConnPool(t)
	v.watchPool("poolstore", db)
	waitOnce(t, db)

	clock.advance(time.Minute)
	v.rollup(clock.now())
	c := v.snapshot().LastMinute.Counts
	if c["db.pool_wait_count.poolstore"] != 1 {
		t.Errorf("pool wait count = %d, want 1 (%v)", c["db.pool_wait_count.poolstore"], c)
	}
	if c["db.pool_wait_us.poolstore"] < 1000 {
		t.Errorf("pool wait = %dus, want at least a millisecond", c["db.pool_wait_us.poolstore"])
	}

	clock.advance(time.Minute)
	v.rollup(clock.now())
	if got := v.snapshot().LastMinute.Counts["db.pool_wait_count.poolstore"]; got != 0 {
		t.Errorf("second minute re-reported the first minute's waits: %d", got)
	}
}

func readVitalsFile(t *testing.T, path string) []vitalsLine {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var out []vitalsLine
	for _, l := range strings.Split(strings.TrimSpace(string(b)), "\n") {
		var line vitalsLine
		if err := json.Unmarshal([]byte(l), &line); err != nil {
			t.Fatalf("bad line %q: %v", l, err)
		}
		out = append(out, line)
	}
	return out
}

func TestVitals_WritesOneLinePerMinuteToTheDaysFile(t *testing.T) {
	dir := t.TempDir()
	v, clock := newTestVitals(t, dir)
	v.record("db.x", time.Millisecond)
	clock.advance(time.Minute) // 23:59:30 on the 17th
	v.rollup(clock.now())
	clock.advance(time.Minute) // 00:00:30 on the 18th
	v.rollup(clock.now())

	d17 := filepath.Join(dir, "server-vitals-2026-11-17.jsonl")
	d18 := filepath.Join(dir, "server-vitals-2026-11-18.jsonl")
	l17 := readVitalsFile(t, d17)
	l18 := readVitalsFile(t, d18)
	if len(l17) != 1 || len(l18) != 1 {
		t.Fatalf("lines: day 17 has %d, day 18 has %d, want 1 each", len(l17), len(l18))
	}
	if l17[0].Timings["db.x"].Count != 1 {
		t.Errorf("day 17 line = %+v", l17[0].Timings)
	}
	if _, ok := l18[0].Timings["db.x"]; ok {
		t.Errorf("day 18 line repeats the previous minute: %+v", l18[0].Timings)
	}
	if !l18[0].StartedAt.Equal(v.startedAt) {
		t.Errorf("started_at = %v, want the process start %v", l18[0].StartedAt, v.startedAt)
	}
}

func TestVitals_KeepsSevenDaysOfFiles(t *testing.T) {
	dir := t.TempDir()
	for _, d := range []string{"2026-11-09", "2026-11-10", "2026-11-11", "2026-11-17"} {
		if err := os.WriteFile(filepath.Join(dir, "server-vitals-"+d+".jsonl"), []byte("{}\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(dir, "notes.txt"), []byte("mine"), 0o644); err != nil {
		t.Fatal(err)
	}
	v, clock := newTestVitals(t, dir)
	clock.advance(time.Minute) // the 17th: keep the 11th through the 17th
	v.rollup(clock.now())

	for name, want := range map[string]bool{
		"server-vitals-2026-11-09.jsonl": false,
		"server-vitals-2026-11-10.jsonl": false,
		"server-vitals-2026-11-11.jsonl": true,
		"server-vitals-2026-11-17.jsonl": true,
		"notes.txt":                      true,
	} {
		_, err := os.Stat(filepath.Join(dir, name))
		if exists := err == nil; exists != want {
			t.Errorf("%s exists = %v, want %v", name, exists, want)
		}
	}
}

func TestVitals_WriteFailureIsLoggedOncePerMinuteShownAndClearedOnSuccess(t *testing.T) {
	root := t.TempDir()
	blocker := filepath.Join(root, "blocker")
	if err := os.WriteFile(blocker, []byte("a file, not a directory"), 0o644); err != nil {
		t.Fatal(err)
	}
	dir := filepath.Join(blocker, "vitals") // MkdirAll under a file fails
	v, clock := newTestVitals(t, dir)

	var logs bytes.Buffer
	log.SetOutput(&logs)
	t.Cleanup(func() { log.SetOutput(os.Stderr) })

	v.record("db.x", time.Millisecond)
	clock.advance(10 * time.Second)
	v.rollup(clock.now())
	clock.advance(20 * time.Second) // same minute: no second log line
	v.rollup(clock.now())
	if got := strings.Count(logs.String(), "server vitals: cannot write"); got != 1 {
		t.Errorf("logged %d times within a minute, want 1:\n%s", got, logs.String())
	}
	clock.advance(time.Minute)
	v.rollup(clock.now())
	if got := strings.Count(logs.String(), "server vitals: cannot write"); got != 2 {
		t.Errorf("logged %d times after a minute, want 2", got)
	}
	if !strings.Contains(logs.String(), "(1 more failed since the last line)") {
		t.Errorf("second line should count the one held back:\n%s", logs.String())
	}

	// Serving and recording carry on, and the endpoint says what failed.
	v.record("db.y", time.Millisecond)
	snap := v.snapshot()
	if snap.LastWriteError == nil || snap.LastWriteError.Error == "" {
		t.Fatal("endpoint shows no write error")
	}
	if snap.LastMinute == nil || snap.SinceStart.Timings["db.x"].Count != 1 || snap.SinceStart.Timings["db.y"].Count != 1 {
		t.Errorf("recording stopped while the write was failing: %+v", snap.SinceStart.Timings)
	}

	v.writer.dir = filepath.Join(root, "ok")
	clock.advance(time.Minute)
	v.rollup(clock.now())
	if v.snapshot().LastWriteError != nil {
		t.Errorf("error not cleared after a good write: %+v", v.snapshot().LastWriteError)
	}
}

func TestVitals_APruneFailureIsReportedAsPruneNotWriteAndRetriedOnlyDaily(t *testing.T) {
	dir := t.TempDir()
	// A non-empty directory with an old file's name cannot be removed.
	stuck := filepath.Join(dir, "server-vitals-2026-11-01.jsonl")
	if err := os.MkdirAll(filepath.Join(stuck, "inner"), 0o755); err != nil {
		t.Fatal(err)
	}
	v, clock := newTestVitals(t, dir)

	var logs bytes.Buffer
	log.SetOutput(&logs)
	t.Cleanup(func() { log.SetOutput(os.Stderr) })

	clock.advance(10 * time.Second) // 23:58:40 on the 17th
	v.rollup(clock.now())
	if strings.Contains(logs.String(), "cannot write") {
		t.Errorf("a prune failure was logged as a write failure:\n%s", logs.String())
	}
	if !strings.Contains(logs.String(), "server vitals: cannot prune old files in "+dir) {
		t.Errorf("prune failure not logged:\n%s", logs.String())
	}
	snap := v.snapshot()
	if snap.LastWriteError != nil {
		t.Errorf("last_write_error = %+v, want nil: the line was written", snap.LastWriteError)
	}
	if snap.LastPruneError == nil || snap.LastPruneError.Error == "" {
		t.Error("endpoint shows no prune error")
	}
	if got := len(readVitalsFile(t, filepath.Join(dir, "server-vitals-2026-11-17.jsonl"))); got != 1 {
		t.Errorf("lines written = %d, want 1", got)
	}

	clock.advance(70 * time.Second) // 23:59:50, same day, past the log rate limit: no retry
	v.rollup(clock.now())
	if got := strings.Count(logs.String(), "cannot prune"); got != 1 {
		t.Errorf("prune retried within the day: logged %d times", got)
	}
}

func TestVitalsEndpoint_ReturnsLastMinuteAndSinceStart(t *testing.T) {
	v, clock := newTestVitals(t, t.TempDir())
	v.record("db.x", 3*time.Millisecond)
	clock.advance(time.Minute)
	v.rollup(clock.now())
	v.record("db.x", 4*time.Millisecond)

	e := echo.New()
	req := httptest.NewRequest(http.MethodGet, "/api/debug/vitals", nil)
	rec := httptest.NewRecorder()
	if err := serverVitalsHandler(v)(e.NewContext(req, rec)); err != nil {
		t.Fatal(err)
	}
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	var got vitalsSnapshot
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if got.LastMinute == nil || got.LastMinute.Timings["db.x"].Count != 1 {
		t.Errorf("last_minute = %+v", got.LastMinute)
	}
	if got.SinceStart.Timings["db.x"].Count != 2 {
		t.Errorf("since_start count = %d, want 2", got.SinceStart.Timings["db.x"].Count)
	}
}

func TestVitalsRoute_SitsOnTheLogsTier(t *testing.T) {
	var logsTier, vitalsTier apiTier
	found := false
	for _, r := range logRoutes() {
		if r.Path == "/api/logs" {
			logsTier = r.Tier
		}
	}
	for _, r := range serverVitalsRoutes() {
		if r.Path == "/api/debug/vitals" {
			vitalsTier = r.Tier
			found = true
		}
	}
	if !found || vitalsTier != logsTier {
		t.Errorf("vitals tier = %v, logs tier = %v, want the same", vitalsTier, logsTier)
	}
}

func TestRunTx_RecordsLockWaitAndTransactionTimeSeparately(t *testing.T) {
	reg := recordLocksInto(t)
	s := newTestDocumentStore(t)
	reached := make(chan struct{}, 1)
	s.lockWaitHook = func() { reached <- struct{}{} }

	s.mu.Lock()
	done := make(chan error, 1)
	go func() {
		done <- s.RunTx(vitalsTestOp, func(tx *sql.Tx, now time.Time) error {
			time.Sleep(20 * time.Millisecond)
			return nil
		})
	}()
	<-reached
	time.Sleep(60 * time.Millisecond)
	s.mu.Unlock()
	if err := <-done; err != nil {
		t.Fatal(err)
	}

	wait, ok := reg.timing("tx.lock_wait.vitals-test-op")
	if !ok || wait.MaxMs < 50 {
		t.Errorf("lock wait = %+v (found %v), want at least 50ms", wait, ok)
	}
	run, ok := reg.timing("tx.time.vitals-test-op")
	if !ok || run.MaxMs < 20 || run.MaxMs >= 50 {
		t.Errorf("transaction time = %+v (found %v), want about 20ms and not the wait", run, ok)
	}
}

func TestVitalsSummary_MergesFilesIntoOneTable(t *testing.T) {
	dir := t.TempDir()
	v, clock := newTestVitals(t, dir)
	for i := 1; i <= 50; i++ {
		v.record("db.slow", time.Duration(i)*time.Millisecond)
	}
	v.count("hits", 2)
	clock.advance(time.Minute)
	v.rollup(clock.now())
	for i := 51; i <= 100; i++ {
		v.record("db.slow", time.Duration(i)*time.Millisecond)
	}
	v.record("db.fast", time.Millisecond)
	v.count("hits", 5)
	clock.advance(time.Minute)
	v.rollup(clock.now())

	var out bytes.Buffer
	if err := writeVitalsSummary(&out, []string{filepath.Join(dir, "server-vitals-2026-11-17.jsonl"), filepath.Join(dir, "server-vitals-2026-11-18.jsonl")}); err != nil {
		t.Fatal(err)
	}
	text := out.String()
	for _, want := range []string{"KEY", "P50", "P95", "P99", "COUNT", "db.slow", "db.fast", "hits", "heap", "GC"} {
		if !strings.Contains(text, want) {
			t.Errorf("summary is missing %q:\n%s", want, text)
		}
	}
	var slowLine string
	for _, l := range strings.Split(text, "\n") {
		if strings.HasPrefix(l, "db.slow") {
			slowLine = l
		}
	}
	if !strings.Contains(slowLine, "100") {
		t.Errorf("db.slow row should show count 100 merged across minutes: %q", slowLine)
	}
	if strings.Index(text, "db.slow") > strings.Index(text, "db.fast") {
		t.Errorf("rows should list the most total time first:\n%s", text)
	}
}

func TestVitalsSummary_NoLinesIsAnErrorNamingTheFiles(t *testing.T) {
	p := filepath.Join(t.TempDir(), "empty.jsonl")
	if err := os.WriteFile(p, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	err := writeVitalsSummary(&out, []string{p})
	if err == nil || !strings.Contains(err.Error(), "no vitals lines in") || !strings.Contains(err.Error(), "empty.jsonl") {
		t.Errorf("err = %v, want one naming empty.jsonl", err)
	}
	if out.Len() != 0 {
		t.Errorf("printed %q before failing", out.String())
	}
}

func TestCoastlineAndMCPRunTx_RecordLockWaitAndTime(t *testing.T) {
	reg := recordLocksInto(t)
	coast := newTestCoastlineStore(t)
	if err := coast.RunTx(vitalsTestCoastOp, func(tx *sql.Tx) error { return nil }); err != nil {
		t.Fatal(err)
	}
	mcp := newTestMCPStore(t)
	if err := mcp.RunTx(vitalsTestMCPOp, func(tx *sql.Tx, now time.Time) error { return nil }); err != nil {
		t.Fatal(err)
	}
	for _, op := range []string{"vitals-coast-op", "vitals-mcp-op"} {
		for _, prefix := range []string{"tx.lock_wait.", "tx.time."} {
			if _, ok := reg.timing(prefix + op); !ok {
				t.Errorf("%s%s not recorded", prefix, op)
			}
		}
	}
}

func vitalsTestFile(t *testing.T, name string, lines ...string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(p, []byte(strings.Join(lines, "\n")+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestVitals_FileLinesCarryTheFormatVersion(t *testing.T) {
	dir := t.TempDir()
	v, clock := newTestVitals(t, dir)
	v.record("db.x", time.Millisecond)
	clock.advance(time.Minute)
	v.rollup(clock.now())
	b, err := os.ReadFile(filepath.Join(dir, "server-vitals-2026-11-18.jsonl"))
	if err != nil {
		matches, _ := filepath.Glob(filepath.Join(dir, "*.jsonl"))
		if len(matches) != 1 {
			t.Fatalf("read: %v (files %v)", err, matches)
		}
		b, _ = os.ReadFile(matches[0])
	}
	var m map[string]any
	if err := json.Unmarshal(bytes.TrimSpace(b), &m); err != nil {
		t.Fatal(err)
	}
	if got, ok := m["format"].(float64); !ok || int(got) != vitalsFormat {
		t.Errorf("format = %v, want %d", m["format"], vitalsFormat)
	}
}

func TestVitalsSummary_RejectsAMissingOrDifferentFormat(t *testing.T) {
	base := `"started_at":"2026-11-18T00:00:00Z","at":"2026-11-18T00:01:00Z","seconds":60,"timings":{},"counts":{},"gauges":{}`
	for name, line := range map[string]string{
		"missing.jsonl": `{` + base + `}`,
		"other.jsonl":   `{"format":999,` + base + `}`,
	} {
		p := vitalsTestFile(t, name, line)
		err := writeVitalsSummary(&bytes.Buffer{}, []string{p})
		if err == nil || !strings.Contains(err.Error(), name+":1") || !strings.Contains(err.Error(), "format") {
			t.Errorf("%s: err = %v, want one naming %s:1 and the format", name, err, name)
		}
	}
}

func TestVitalsSummary_AveragesOverMinutesThatHaveTheGauge(t *testing.T) {
	head := `{"format":1,"started_at":"2026-11-18T00:00:00Z","seconds":60,"timings":{},"counts":{},`
	p := vitalsTestFile(t, "g.jsonl",
		head+`"at":"2026-11-18T00:01:00Z","gauges":{"runtime.heap_live_bytes":10000000,"runtime.cpu_cores":2,"runtime.alloc_bytes_per_s":4000000}}`,
		head+`"at":"2026-11-18T00:02:00Z","gauges":{"runtime.heap_live_bytes":10000000}}`)
	var out bytes.Buffer
	if err := writeVitalsSummary(&out, []string{p}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "average 2.00 cores") || !strings.Contains(out.String(), "average 4.0 MB/s") {
		t.Errorf("averages should skip the minute without the gauge:\n%s", out.String())
	}

	p = vitalsTestFile(t, "n.jsonl",
		head+`"at":"2026-11-18T00:01:00Z","gauges":{"runtime.heap_live_bytes":10000000}}`)
	out.Reset()
	if err := writeVitalsSummary(&out, []string{p}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "CPU not recorded") || strings.Contains(out.String(), "0.00 cores") {
		t.Errorf("want CPU not recorded:\n%s", out.String())
	}
}

func TestVitals_SinceStartHasNoGauges(t *testing.T) {
	dir := t.TempDir()
	v, clock := newTestVitals(t, dir)
	clock.advance(time.Minute)
	v.rollup(clock.now())
	snap := v.snapshot()
	if snap.LastMinute == nil || len(snap.LastMinute.Gauges) == 0 {
		t.Fatalf("last_minute should keep its gauges: %+v", snap.LastMinute)
	}
	if len(snap.SinceStart.Gauges) != 0 {
		t.Errorf("since_start gauges = %v, want none", snap.SinceStart.Gauges)
	}
	b, _ := json.Marshal(snap.SinceStart)
	if strings.Contains(string(b), `"gauges"`) {
		t.Errorf("since_start JSON still has gauges: %s", b)
	}
}

func TestRunTx_PanicNamesTheOpWithoutTheMutex(t *testing.T) {
	defer func() {
		r := recover()
		msg, _ := r.(string)
		if msg != "store transaction run without the store mutex held (my-op)" {
			t.Errorf("panic = %v", r)
		}
	}()
	var mu sync.Mutex
	h := &heldLock{op: makeLockOp("my-op"), mu: &mu, clock: time.Now, reg: serverVitals}
	_ = h.runTx(nil, func(tx *sql.Tx) error { return nil })
}

func TestVitalsSummary_RejectsABadLineWithItsPosition(t *testing.T) {
	p := filepath.Join(t.TempDir(), "bad.jsonl")
	if err := os.WriteFile(p, []byte("{not json}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	err := writeVitalsSummary(&bytes.Buffer{}, []string{p})
	if err == nil || !strings.Contains(err.Error(), "bad.jsonl:1") {
		t.Errorf("err = %v, want one naming bad.jsonl:1", err)
	}
	if err := writeVitalsSummary(&bytes.Buffer{}, nil); err == nil || errors.Is(err, os.ErrNotExist) {
		t.Errorf("no files: err = %v, want a usage error", err)
	}
}

// A clean stop keeps the final minute: cancelling the context writes one last
// line, and the channel start returns closes only once that line is on disk.
func TestVitals_CancellingTheContextWritesTheFinalMinuteBeforeDoneCloses(t *testing.T) {
	dir := t.TempDir()
	v, _ := newTestVitals(t, "")
	ctx, cancel := context.WithCancel(context.Background())
	done := v.start(ctx, dir)
	v.record("db.x", time.Millisecond)
	cancel()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("done never closed after the context ended")
	}
	files, err := filepath.Glob(filepath.Join(dir, "server-vitals-*.jsonl"))
	if err != nil || len(files) != 1 {
		t.Fatalf("files = %v, err = %v, want one", files, err)
	}
	lines := readVitalsFile(t, files[0])
	if len(lines) != 1 || lines[0].Timings["db.x"].Count != 1 {
		t.Fatalf("final line = %+v, want the minute's one db.x call", lines)
	}
}
