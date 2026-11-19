package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"log"
	"maps"
	"math"
	"net/http"
	"os"
	"path/filepath"
	"runtime/metrics"
	"sort"
	"strings"
	"sync"
	"time"
	"weak"

	"github.com/labstack/echo/v4"
)

// Server vitals (ADR 0290): how long Helmcentral's own work takes, recorded
// always, in memory, and written to one line a minute. The numbers describe
// the server process: SQLite statements, store transactions, the Go runtime.
// They are not boat data, which this codebase calls telemetry.

// vitalsRecorder is the whole surface the rest of the code records through.
type vitalsRecorder interface {
	// record adds one timing under name.
	record(name string, d time.Duration)
	// count adds n to the counter name.
	count(name string, n int64)
	// recordRows adds one timing under name and n rows read to it, taking the
	// lock once. The rows come out as the counter "rows: <name>".
	recordRows(name string, d time.Duration, n int64)
}

// rateLimitedLog writes at most one line per interval. The lines it holds
// back are counted, and the next line that goes out says how many.
type rateLimitedLog struct {
	now      func() time.Time
	logf     func(format string, args ...any)
	interval time.Duration

	mu         sync.Mutex
	last       time.Time
	suppressed int
}

// log writes format with args unless a line went out within the interval.
// When it follows held-back lines, it appends note, a format with one %d for
// their count.
func (l *rateLimitedLog) log(note, format string, args ...any) {
	l.mu.Lock()
	now := l.now()
	if !l.last.IsZero() && now.Sub(l.last) < l.interval {
		l.suppressed++
		l.mu.Unlock()
		return
	}
	held := l.suppressed
	l.last, l.suppressed = now, 0
	l.mu.Unlock()
	if held > 0 {
		format += note
		args = append(args, held)
	}
	l.logf(format, args...)
}

const (
	vitalsBucketsPerDecade = 8
	vitalsBoundCount       = 5*vitalsBucketsPerDecade + 1 // 0.1 ms ... 10 s inclusive
	vitalsFileKeepDays     = 7
	vitalsFilePrefix       = "server-vitals-"
	vitalsFileSuffix       = ".jsonl"
	vitalsRollupEvery      = time.Minute
)

// vitalsBounds are the upper edges of the histogram buckets, 8 per decade
// from 0.1 ms to 10 s. A timing above the last edge lands in the overflow
// bucket, one past the end.
var vitalsBounds = func() [vitalsBoundCount]time.Duration {
	var b [vitalsBoundCount]time.Duration
	for i := range b {
		b[i] = time.Duration(math.Round(float64(100*time.Microsecond) * math.Pow(10, float64(i)/vitalsBucketsPerDecade)))
	}
	return b
}()

// histogram counts timings into the fixed buckets and keeps count, sum and
// max exactly, so a percentile is an estimate (the bucket's upper edge,
// never above the true max) while the totals are not.
type histogram struct {
	counts [vitalsBoundCount + 1]uint64
	n      uint64
	sum    int64 // nanoseconds
	max    int64 // nanoseconds

	// rows is the rows read by the queries timed here; hasRows says some
	// timing arrived through recordRows. Neither is merged into the totals:
	// roll-up turns them into the counter "rows: <name>".
	rows    int64
	hasRows bool
}

func vitalsBucketFor(d time.Duration) int {
	return sort.Search(vitalsBoundCount, func(i int) bool { return vitalsBounds[i] >= d })
}

func (h *histogram) add(d time.Duration) {
	if d < 0 {
		d = 0
	}
	h.counts[vitalsBucketFor(d)]++
	h.n++
	h.sum += int64(d)
	if int64(d) > h.max {
		h.max = int64(d)
	}
}

func (h *histogram) merge(o *histogram) {
	for i, c := range o.counts {
		h.counts[i] += c
	}
	h.n += o.n
	h.sum += o.sum
	if o.max > h.max {
		h.max = o.max
	}
}

// bucketQuantile is the index of the bucket holding the q-th quantile (0..1)
// of counts, or -1 when counts is empty.
func bucketQuantile(counts []uint64, q float64) int {
	var n uint64
	for _, c := range counts {
		n += c
	}
	if n == 0 {
		return -1
	}
	target := max(uint64(math.Ceil(q*float64(n))), 1)
	var seen uint64
	for i, c := range counts {
		seen += c
		if seen >= target {
			return i
		}
	}
	return len(counts) - 1
}

// quantile estimates the q-th quantile (0..1).
func (h *histogram) quantile(q float64) time.Duration {
	i := bucketQuantile(h.counts[:], q)
	switch {
	case i < 0:
		return 0
	case i >= vitalsBoundCount:
		return time.Duration(h.max)
	}
	return min(vitalsBounds[i], time.Duration(h.max))
}

// histogramJSON is a histogram as one minute's line carries it: count, sum,
// max and the non-empty buckets, so vitals-summary can merge minutes and
// still estimate percentiles. The endpoint adds the percentiles itself.
type histogramJSON struct {
	Count   uint64      `json:"count"`
	SumMs   float64     `json:"sum_ms"`
	MaxMs   float64     `json:"max_ms"`
	P50Ms   float64     `json:"p50_ms,omitempty"`
	P95Ms   float64     `json:"p95_ms,omitempty"`
	P99Ms   float64     `json:"p99_ms,omitempty"`
	Buckets [][2]uint64 `json:"b"`
}

func durationMs(d time.Duration) float64 { return float64(d) / float64(time.Millisecond) }

func (h *histogram) toJSON() histogramJSON {
	out := histogramJSON{
		Count: h.n,
		SumMs: durationMs(time.Duration(h.sum)),
		MaxMs: durationMs(time.Duration(h.max)),
	}
	for i, c := range h.counts {
		if c > 0 {
			out.Buckets = append(out.Buckets, [2]uint64{uint64(i), c})
		}
	}
	return out
}

// mergeJSON adds a line's histogram into h. A bucket index outside the
// fixed range means the file came from a different build; fail rather than
// fold it into the wrong bucket.
func (h *histogram) mergeJSON(j histogramJSON) error {
	var o histogram
	for _, b := range j.Buckets {
		if b[0] > vitalsBoundCount {
			return fmt.Errorf("bucket index %d is out of range (0..%d)", b[0], vitalsBoundCount)
		}
		o.counts[b[0]] += b[1]
	}
	o.n = j.Count
	o.sum = int64(j.SumMs * float64(time.Millisecond))
	o.max = int64(j.MaxMs * float64(time.Millisecond))
	h.merge(&o)
	return nil
}

// vitalsFormat is the version of the minute-file line. Each line stores
// histogram bucket indexes into this build's vitalsBounds, so changing
// vitalsBounds or vitalsBucketsPerDecade (or any other meaning of a stored
// field) must bump this number. vitals-summary rejects a line with a missing or
// different format rather than read its buckets against the wrong edges.
const vitalsFormat = 1

// vitalsLine is one minute of numbers, the same shape in the file and in the
// endpoint's two parts. Format is set on file lines and last_minute only;
// since_start omits it and the gauges, which only mean something per minute.
type vitalsLine struct {
	Format    int                      `json:"format,omitempty"`
	StartedAt time.Time                `json:"started_at"`
	At        time.Time                `json:"at"`
	Seconds   float64                  `json:"seconds"`
	Timings   map[string]histogramJSON `json:"timings"`
	Counts    map[string]int64         `json:"counts"`
	Gauges    map[string]float64       `json:"gauges,omitempty"`
}

// addPercentiles sets p50, p95 and p99 on each of timings from its histogram
// in hists. Only the endpoint carries them: the minute file leaves them out
// (they would be the bulk of each line) and vitals-summary works them out from
// the buckets.
func addPercentiles(timings map[string]histogramJSON, hists map[string]*histogram) {
	for k, j := range timings {
		h := hists[k]
		j.P50Ms, j.P95Ms, j.P99Ms = durationMs(h.quantile(0.50)), durationMs(h.quantile(0.95)), durationMs(h.quantile(0.99))
		timings[k] = j
	}
}

// addRowCounts adds each histogram's rows read to counts as "rows: <name>".
func addRowCounts(counts map[string]int64, hists map[string]*histogram) {
	for k, h := range hists {
		if h.hasRows {
			counts["rows: "+k] += h.rows
		}
	}
}

// vitalsWriteError is the last failed file write (or, for a prune, the last
// failed clean-up of old files), shown by the endpoint until the next one
// succeeds.
type vitalsWriteError struct {
	Error string    `json:"error"`
	At    time.Time `json:"at"`
}

// vitalsSnapshot is GET /api/debug/vitals.
type vitalsSnapshot struct {
	StartedAt      time.Time         `json:"started_at"`
	Now            time.Time         `json:"now"`
	LastMinute     *vitalsLine       `json:"last_minute"`
	SinceStart     vitalsLine        `json:"since_start"`
	File           string            `json:"file,omitempty"`
	LastWriteError *vitalsWriteError `json:"last_write_error"`
	LastPruneError *vitalsWriteError `json:"last_prune_error"`
}

// poolWatch remembers what one pool's WaitCount and WaitDuration read at the
// last roll-up, since sql.DBStats counts from open. store is only a label:
// the pool itself is the key, held weakly (see watchPool).
type poolWatch struct {
	store    string
	lastWait int64
	lastDur  time.Duration
}

// vitalsRegistry is the always-on recorder. One mutex guards it: a record is
// a map lookup and a few additions, and the benchmark in sqlite_timing_test.go
// keeps that honest.
type vitalsRegistry struct {
	now       func() time.Time
	startedAt time.Time

	// keys is the registry's key budget: every key a timing is recorded under
	// comes through it, so one cap covers statements and transactions alike.
	keys *sqlKeys

	mu          sync.Mutex
	cur         map[string]*histogram
	curCounts   map[string]int64
	total       map[string]*histogram
	totalCounts map[string]int64
	pools       map[weak.Pointer[sql.DB]]*poolWatch
	lastRollup  time.Time
	last        *vitalsLine
	lastHists   map[string]*histogram // the histograms behind last
	rt          *runtimeSampler

	writer   *vitalsFileWriter
	writeErr *vitalsWriteError
	writeLog rateLimitedLog

	pruneErr *vitalsWriteError
	pruneLog rateLimitedLog

	// rollupHook, nil outside tests, runs once the minute has moved into the
	// totals and before the runtime is sampled.
	rollupHook func()
}

func newVitalsRegistry(now func() time.Time) *vitalsRegistry {
	t := now()
	return &vitalsRegistry{
		now:         now,
		startedAt:   t,
		keys:        newSQLKeys(sqlKeyLimit, log.Printf),
		cur:         map[string]*histogram{},
		curCounts:   map[string]int64{},
		total:       map[string]*histogram{},
		totalCounts: map[string]int64{},
		lastRollup:  t,
		rt:          newRuntimeSampler(t, processCPUTime),
		writeLog:    rateLimitedLog{now: now, logf: log.Printf, interval: vitalsRollupEvery},
		pruneLog:    rateLimitedLog{now: now, logf: log.Printf, interval: vitalsRollupEvery},
	}
}

// serverVitals is the process-wide recorder every store and the connector
// write to; main starts its roll-up loop.
var serverVitals = newVitalsRegistry(time.Now)

// hist is the current minute's histogram for name. The caller holds v.mu.
func (v *vitalsRegistry) hist(name string) *histogram {
	h := v.cur[name]
	if h == nil {
		h = &histogram{}
		v.cur[name] = h
	}
	return h
}

func (v *vitalsRegistry) record(name string, d time.Duration) {
	v.mu.Lock()
	v.hist(name).add(d)
	v.mu.Unlock()
}

func (v *vitalsRegistry) recordRows(name string, d time.Duration, n int64) {
	v.mu.Lock()
	h := v.hist(name)
	h.add(d)
	h.rows += n
	h.hasRows = true
	v.mu.Unlock()
}

func (v *vitalsRegistry) count(name string, n int64) {
	v.mu.Lock()
	v.curCounts[name] += n
	v.mu.Unlock()
}

// watchPool samples db's WaitCount and WaitDuration at every roll-up, summed
// per store name. The registry holds the pool only through a weak pointer, so
// watching never keeps a pool alive: a pool that has been collected drops out
// at the next roll-up. A pool that is closed but still referenced stays
// watched and adds nothing, since its counts stop moving. Two pools under one
// store name are both counted.
func (v *vitalsRegistry) watchPool(store string, db *sql.DB) {
	v.mu.Lock()
	if v.pools == nil {
		v.pools = map[weak.Pointer[sql.DB]]*poolWatch{}
	}
	v.pools[weak.Make(db)] = &poolWatch{store: store}
	v.mu.Unlock()
}

func linesFrom(timings map[string]*histogram, counts map[string]int64) (map[string]histogramJSON, map[string]int64) {
	t := make(map[string]histogramJSON, len(timings))
	for k, h := range timings {
		t[k] = h.toJSON()
	}
	c := make(map[string]int64, len(counts))
	for k, n := range counts {
		c[k] = n
	}
	return t, c
}

// rollup closes the current minute: it moves that minute's numbers into the
// since-start totals, samples the pools and the Go runtime, keeps the line as
// "last minute" and appends it to the day's file. now is passed in so tests
// can step through minutes and midnights without sleeping.
func (v *vitalsRegistry) rollup(now time.Time) {
	v.mu.Lock()
	cur, curCounts := v.cur, v.curCounts
	v.cur, v.curCounts = map[string]*histogram{}, map[string]int64{}
	elapsed := now.Sub(v.lastRollup)
	v.lastRollup = now
	pools := maps.Clone(v.pools)
	// The minute moves into the totals in the same locked section that takes
	// it out of cur, so a snapshot never sees it in neither.
	addRowCounts(curCounts, cur)
	for k, h := range cur {
		t := v.total[k]
		if t == nil {
			t = &histogram{}
			v.total[k] = t
		}
		t.merge(h)
	}
	for k, n := range curCounts {
		v.totalCounts[k] += n
	}
	v.mu.Unlock()

	if v.rollupHook != nil {
		v.rollupHook()
	}

	// Sampling happens outside the lock; its counts join the totals after.
	extra := map[string]int64{}
	var gone []weak.Pointer[sql.DB]
	for key, p := range pools {
		db := key.Value()
		if db == nil {
			gone = append(gone, key)
			continue
		}
		st := db.Stats()
		if dc := st.WaitCount - p.lastWait; dc > 0 {
			extra["db.pool_wait_count."+p.store] += dc
			extra["db.pool_wait_us."+p.store] += (st.WaitDuration - p.lastDur).Microseconds()
		}
		p.lastWait, p.lastDur = st.WaitCount, st.WaitDuration
	}

	gaugeVals, rtCounts := v.rt.sample(now)
	for k, n := range rtCounts {
		extra[k] += n
	}

	lineCounts := maps.Clone(curCounts)
	for k, n := range extra {
		lineCounts[k] += n
	}
	timings, counts := linesFrom(cur, lineCounts)
	line := vitalsLine{
		Format:    vitalsFormat,
		StartedAt: v.startedAt,
		At:        now,
		Seconds:   elapsed.Seconds(),
		Timings:   timings,
		Counts:    counts,
		Gauges:    gaugeVals,
	}

	v.mu.Lock()
	for k, n := range extra {
		v.totalCounts[k] += n
	}
	for _, key := range gone {
		delete(v.pools, key)
	}
	v.last, v.lastHists = &line, cur
	w := v.writer
	v.mu.Unlock()

	if w == nil {
		return
	}
	err := w.write(line)
	v.mu.Lock()
	if err != nil {
		v.writeErr = &vitalsWriteError{Error: err.Error(), At: now}
		v.mu.Unlock()
		v.writeLog.log(" (%d more failed since the last line)", "server vitals: cannot write %s: %v", w.dir, err)
		return
	}
	v.writeErr = nil
	v.mu.Unlock()

	// Old files are removed at most once a day, and a failure to do so is its
	// own report: the line above was written.
	ran, perr := w.pruneOncePerDay(now)
	if !ran {
		return
	}
	v.mu.Lock()
	if perr == nil {
		v.pruneErr = nil
		v.mu.Unlock()
		return
	}
	v.pruneErr = &vitalsWriteError{Error: perr.Error(), At: now}
	v.mu.Unlock()
	v.pruneLog.log(" (%d more failed since the last line)", "server vitals: cannot prune old files in %s: %v", w.dir, perr)
}

// snapshot is what the endpoint returns: the last full minute and
// everything since the process started, including the minute in progress.
func (v *vitalsRegistry) snapshot() vitalsSnapshot {
	now := v.now()
	v.mu.Lock()
	all := make(map[string]*histogram, len(v.total)+len(v.cur))
	for k, h := range v.total {
		c := *h
		all[k] = &c
	}
	for k, h := range v.cur {
		t := all[k]
		if t == nil {
			t = &histogram{}
			all[k] = t
		}
		t.merge(h)
	}
	counts := make(map[string]int64, len(v.totalCounts)+len(v.curCounts))
	for k, n := range v.totalCounts {
		counts[k] += n
	}
	for k, n := range v.curCounts {
		counts[k] += n
	}
	addRowCounts(counts, v.cur)
	last, lastHists := v.last, v.lastHists
	werr, perr := v.writeErr, v.pruneErr
	var file string
	if v.writer != nil {
		file = v.writer.pathFor(now)
	}
	v.mu.Unlock()

	var lastOut *vitalsLine
	if last != nil {
		l := *last
		l.Timings = maps.Clone(last.Timings)
		addPercentiles(l.Timings, lastHists)
		lastOut = &l
	}
	timings, countsOut := linesFrom(all, counts)
	addPercentiles(timings, all)
	snap := vitalsSnapshot{
		StartedAt:  v.startedAt,
		Now:        now,
		LastMinute: lastOut,
		SinceStart: vitalsLine{
			StartedAt: v.startedAt,
			At:        now,
			Seconds:   now.Sub(v.startedAt).Seconds(),
			Timings:   timings,
			Counts:    countsOut,
		},
		File:           file,
		LastWriteError: werr,
		LastPruneError: perr,
	}
	return snap
}

// timing returns the since-start histogram for name, for tests and callers
// that want one figure.
func (v *vitalsRegistry) timing(name string) (histogramJSON, bool) {
	s := v.snapshot()
	h, ok := s.SinceStart.Timings[name]
	return h, ok
}

// start points the roll-up at dir and runs it every minute until ctx ends,
// with a last roll-up on the way out so a clean stop keeps its final minute.
// The returned channel closes once the loop has ended and that last roll-up is
// written; a caller that is about to exit waits on it.
func (v *vitalsRegistry) start(ctx context.Context, dir string) <-chan struct{} {
	v.mu.Lock()
	v.writer = &vitalsFileWriter{dir: dir, keepDays: vitalsFileKeepDays}
	v.lastRollup = v.now()
	v.mu.Unlock()
	done := make(chan struct{})
	go func() {
		defer close(done)
		t := time.NewTicker(vitalsRollupEvery)
		defer t.Stop()
		for {
			select {
			case <-ctx.Done():
				v.rollup(v.now())
				return
			case <-t.C:
				v.rollup(v.now())
			}
		}
	}()
	return done
}

// vitalsDir is where the minute files live: a "vitals" folder beside the
// business database, so it follows HELMCENTRAL_DB_PATH and the state
// directory the way every other data file does.
func vitalsDir() string {
	return filepath.Join(filepath.Dir(helmcentralDBPath()), "vitals")
}

// vitalsFileWriter appends lines to one file per UTC day and keeps a week.
type vitalsFileWriter struct {
	dir        string
	keepDays   int
	lastPruned string
}

func (w *vitalsFileWriter) pathFor(t time.Time) string {
	return filepath.Join(w.dir, vitalsFilePrefix+t.UTC().Format("2006-01-02")+vitalsFileSuffix)
}

func (w *vitalsFileWriter) write(line vitalsLine) error {
	if err := os.MkdirAll(w.dir, 0o755); err != nil {
		return err
	}
	b, err := json.Marshal(line)
	if err != nil {
		return err
	}
	b = append(b, '\n')
	f, err := os.OpenFile(w.pathFor(line.At), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	if _, err := f.Write(b); err != nil {
		f.Close()
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	return nil
}

// pruneOncePerDay runs prune the first time it is called on each UTC day,
// whether or not that attempt works, so a stuck file is retried tomorrow
// rather than every minute. ran says whether it attempted a prune.
func (w *vitalsFileWriter) pruneOncePerDay(now time.Time) (ran bool, err error) {
	day := now.UTC().Format("2006-01-02")
	if day == w.lastPruned {
		return false, nil
	}
	w.lastPruned = day
	return true, w.prune(now)
}

// prune removes minute files older than keepDays days, today included.
func (w *vitalsFileWriter) prune(now time.Time) error {
	entries, err := os.ReadDir(w.dir)
	if err != nil {
		return err
	}
	today := now.UTC().Truncate(24 * time.Hour)
	oldest := today.AddDate(0, 0, -(w.keepDays - 1))
	for _, e := range entries {
		name := e.Name()
		if !strings.HasPrefix(name, vitalsFilePrefix) || !strings.HasSuffix(name, vitalsFileSuffix) {
			continue
		}
		day, err := time.Parse("2006-01-02", strings.TrimSuffix(strings.TrimPrefix(name, vitalsFilePrefix), vitalsFileSuffix))
		if err != nil {
			continue
		}
		if day.Before(oldest) {
			if err := os.Remove(filepath.Join(w.dir, name)); err != nil {
				return err
			}
		}
	}
	return nil
}

// ── Go runtime figures ───────────────────────────────────────────────────

var runtimeMetricNames = []string{
	"/gc/heap/live:bytes",
	"/gc/heap/allocs:bytes",
	"/gc/cycles/total:gc-cycles",
	"/sched/goroutines:goroutines",
	"/sched/pauses/total/gc:seconds",
}

// runtimeSampler reads runtime/metrics and turns the cumulative ones into
// per-interval figures. The CPU figure comes from the operating system
// instead: the runtime's /cpu/classes figures only move at the end of a GC
// cycle, so a one-minute difference of them means nothing.
type runtimeSampler struct {
	samples []metrics.Sample
	prev    map[string]float64
	prevHis []uint64
	prevAt  time.Time

	// processCPU reads the process's user plus system CPU time; a func so
	// tests can feed values.
	processCPU func() (time.Duration, error)
	prevCPU    time.Duration
	prevCPUOK  bool
}

func newRuntimeSampler(now time.Time, processCPU func() (time.Duration, error)) *runtimeSampler {
	s := &runtimeSampler{prevAt: now, processCPU: processCPU}
	for _, n := range runtimeMetricNames {
		s.samples = append(s.samples, metrics.Sample{Name: n})
	}
	s.read()
	s.prevCPU, s.prevCPUOK = s.readCPU()
	return s
}

func (s *runtimeSampler) readCPU() (time.Duration, bool) {
	cpu, err := s.processCPU()
	return cpu, err == nil
}

func (s *runtimeSampler) read() (vals map[string]float64, pauses []uint64) {
	metrics.Read(s.samples)
	vals = map[string]float64{}
	for _, m := range s.samples {
		switch m.Value.Kind() {
		case metrics.KindUint64:
			vals[m.Name] = float64(m.Value.Uint64())
		case metrics.KindFloat64:
			vals[m.Name] = m.Value.Float64()
		case metrics.KindFloat64Histogram:
			pauses = append([]uint64(nil), m.Value.Float64Histogram().Counts...)
		}
	}
	return vals, pauses
}

// sample returns the figures for the interval ending at now: gauges (live
// heap, allocation rate, goroutines, GC pause p99, process CPU cores busy) and
// counts (GC cycles). A metric the running Go does not report is left out
// rather than guessed; the test pins the list.
func (s *runtimeSampler) sample(now time.Time) (map[string]float64, map[string]int64) {
	vals, pauses := s.read()
	secs := now.Sub(s.prevAt).Seconds()
	gauges := map[string]float64{}
	counts := map[string]int64{}

	if v, ok := vals["/gc/heap/live:bytes"]; ok {
		gauges["runtime.heap_live_bytes"] = v
	}
	if v, ok := vals["/sched/goroutines:goroutines"]; ok {
		gauges["runtime.goroutines"] = v
	}
	if v, ok := vals["/gc/heap/allocs:bytes"]; ok && secs > 0 {
		gauges["runtime.alloc_bytes_per_s"] = (v - s.prev["/gc/heap/allocs:bytes"]) / secs
	}
	if v, ok := vals["/gc/cycles/total:gc-cycles"]; ok {
		counts["runtime.gc_cycles"] = int64(v - s.prev["/gc/cycles/total:gc-cycles"])
	}
	cpu, cpuOK := s.readCPU()
	if cpuOK && s.prevCPUOK && secs > 0 {
		gauges["runtime.cpu_cores"] = (cpu - s.prevCPU).Seconds() / secs
	}
	s.prevCPU, s.prevCPUOK = cpu, cpuOK
	if pauses != nil {
		gauges["runtime.gc_pause_p99_ms"] = s.pauseP99(pauses)
		s.prevHis = pauses
	}
	s.prev = vals
	s.prevAt = now
	return gauges, counts
}

// pauseP99 is the 99th percentile GC pause, in milliseconds, among the
// pauses since the last sample (the runtime's histogram is cumulative).
func (s *runtimeSampler) pauseP99(cur []uint64) float64 {
	var bounds []float64
	for _, m := range s.samples {
		if m.Value.Kind() == metrics.KindFloat64Histogram {
			bounds = m.Value.Float64Histogram().Buckets
		}
	}
	delta := make([]uint64, len(cur))
	for i, c := range cur {
		var p uint64
		if i < len(s.prevHis) {
			p = s.prevHis[i]
		}
		delta[i] = c - p
	}
	i := bucketQuantile(delta, 0.99)
	if i < 0 || len(bounds) != len(cur)+1 {
		return 0
	}
	upper := bounds[i+1]
	if math.IsInf(upper, 1) {
		upper = bounds[i]
	}
	return upper * 1000
}

// ── endpoint ─────────────────────────────────────────────────────────────

func serverVitalsHandler(v *vitalsRegistry) echo.HandlerFunc {
	return func(c echo.Context) error {
		return c.JSON(http.StatusOK, v.snapshot())
	}
}

// serverVitalsRoutes is the debug page for the numbers, on the same tier as
// the log buffer it sits beside.
func serverVitalsRoutes() []apiRoute {
	return []apiRoute{
		{http.MethodGet, "/api/debug/vitals", tierRead, serverVitalsHandler(serverVitals)},
	}
}
