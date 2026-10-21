package main

import (
	"bytes"
	"errors"
	"log"
	"strings"
	"sync"
	"testing"
	"time"
)

var registryT0 = time.Date(2026, 10, 21, 12, 0, 0, 0, time.UTC)

// flakyRegistryStore wraps the real store: it counts the transactions that
// wrote rows and can be told to fail writes or loads.
type flakyRegistryStore struct {
	inner    *sourceRegistryStore
	mu       sync.Mutex
	saves    int
	rows     int
	fail     bool
	failLoad bool
}

func (f *flakyRegistryStore) load(vessel string) ([]seenRow, *registryMeta, bool, error) {
	f.mu.Lock()
	failLoad := f.failLoad
	f.mu.Unlock()
	if failLoad {
		return nil, nil, false, errors.New("injected read failure")
	}
	return f.inner.load(vessel)
}

func (f *flakyRegistryStore) save(vessel string, rows []seenRow, meta *registryMeta) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.fail {
		return errors.New("injected write failure")
	}
	if err := f.inner.save(vessel, rows, meta); err != nil {
		return err
	}
	f.saves++
	f.rows += len(rows)
	return nil
}

func (f *flakyRegistryStore) set(fail bool)     { f.mu.Lock(); f.fail = fail; f.mu.Unlock() }
func (f *flakyRegistryStore) setLoad(fail bool) { f.mu.Lock(); f.failLoad = fail; f.mu.Unlock() }
func (f *flakyRegistryStore) counts() (int, int) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.saves, f.rows
}

func newRegistryStore(t *testing.T) *flakyRegistryStore {
	t.Helper()
	ds := newTestDocumentStore(t)
	inner, err := newSourceRegistryStore(ds)
	if err != nil {
		t.Fatal(err)
	}
	return &flakyRegistryStore{inner: inner}
}

func unconfiguredSeed() (func() bool, func() (sourceHistory, error)) {
	return func() bool { return false }, func() (sourceHistory, error) { panic("seed must not query") }
}

// reportAt puts a self-vessel reading in the tree that arrived age ago.
func reportAt(s *signalKSnapshot, path string, now time.Time, age time.Duration) {
	applyNumeric(s, "vessels.self", path, 1, now.Add(-age))
}

func vesselSnapshot(id string) *signalKSnapshot {
	s := newSignalKSnapshot()
	s.setSelfContext("vessels." + id)
	return s
}

func reportOn(s *signalKSnapshot, id, path string, now time.Time, age time.Duration) {
	applyNumeric(s, "vessels."+id, path, 1, now.Add(-age))
}

func TestRegistryRecordsOnlyPathsUnderEquipmentSourcesThatReportedRecently(t *testing.T) {
	store := newRegistryStore(t)
	configured, fetch := unconfiguredSeed()
	reg := newSourceRegistry(store, configured, fetch)
	snap := newAnomalyTestSnapshot()
	reportAt(snap, "propulsion.port.revolutions", registryT0, 5*time.Second)
	reportAt(snap, "propulsion.0.exhaustTemperature", registryT0, 20*time.Second)
	reportAt(snap, "navigation.speedOverGround", registryT0, 2*time.Second)
	reportAt(snap, "electrical.batteries.3.voltage", registryT0, time.Hour) // quiet: not seen now

	reg.tick(snap, registryT0)

	rec := reg.record()
	if got := rec.Last["propulsion.port.revolutions"]; !got.Equal(registryT0.Add(-5 * time.Second)) {
		t.Fatalf("an engine reading is recorded at its arrival: %v", rec.Last)
	}
	if _, ok := rec.Last["navigation.speedOverGround"]; ok {
		t.Error("only paths under the equipment source shapes are recorded")
	}
	if _, ok := rec.Last["electrical.batteries.3.voltage"]; ok {
		t.Error("a reading that has been quiet for an hour was not seen now")
	}
	if _, ok := rec.Active["propulsion.0"]; !ok {
		t.Errorf("active sources = %v", rec.Active)
	}
}

func TestRegistryWritesAtMostOncePerPathPerInterval(t *testing.T) {
	store := newRegistryStore(t)
	configured, fetch := unconfiguredSeed()
	reg := newSourceRegistry(store, configured, fetch)
	snap := newAnomalyTestSnapshot()
	path := "propulsion.port.revolutions"

	reportAt(snap, path, registryT0, time.Second)
	reg.tick(snap, registryT0)
	saves, rows := store.counts()
	if saves != 1 || rows != 1 {
		t.Fatalf("first sight is written: saves=%d rows=%d", saves, rows)
	}

	for i := 1; i <= 3; i++ {
		now := registryT0.Add(time.Duration(i) * time.Minute)
		reportAt(snap, path, now, time.Second)
		reg.tick(snap, now)
	}
	if saves, _ := store.counts(); saves != 1 {
		t.Fatalf("a path seen again inside %v is not rewritten: saves=%d", sourceRegistryWriteInterval, saves)
	}

	now := registryT0.Add(sourceRegistryWriteInterval + time.Minute)
	reportAt(snap, path, now, time.Second)
	reg.tick(snap, now)
	if saves, rows := store.counts(); saves != 2 || rows != 2 {
		t.Fatalf("after the interval last_seen is rewritten: saves=%d rows=%d", saves, rows)
	}
}

func TestRegistrySurvivesARestartAndKeepsFirstSeen(t *testing.T) {
	store := newRegistryStore(t)
	configured, fetch := unconfiguredSeed()
	snap := newAnomalyTestSnapshot()
	reportAt(snap, "propulsion.port.revolutions", registryT0, time.Second)

	first := newSourceRegistry(store, configured, fetch)
	first.tick(snap, registryT0)

	// A new process: the engines are off, the tree is empty.
	later := registryT0.Add(48 * time.Hour)
	second := newSourceRegistry(store, configured, fetch)
	second.tick(newAnomalyTestSnapshot(), later)

	rec := second.record()
	if got := rec.Last["propulsion.port.revolutions"]; !got.Equal(registryT0.Add(-time.Second)) {
		t.Fatalf("the engine's last sighting survives a restart: %v", rec.Last)
	}
	if !rec.Since.Equal(registryT0) {
		t.Errorf("tracking began at %v, got %v", registryT0, rec.Since)
	}
}

func TestRegistryKeepsFailedWritesPendingAndLogsOncePerStreak(t *testing.T) {
	var logs bytes.Buffer
	prev := log.Writer()
	log.SetOutput(&logs)
	t.Cleanup(func() { log.SetOutput(prev) })

	store := newRegistryStore(t)
	configured, fetch := unconfiguredSeed()
	reg := newSourceRegistry(store, configured, fetch)
	snap := newAnomalyTestSnapshot()
	reportAt(snap, "propulsion.port.revolutions", registryT0, time.Second)

	store.set(true)
	for i := 0; i < 3; i++ {
		reg.tick(snap, registryT0.Add(time.Duration(i)*time.Minute))
	}
	if n := strings.Count(logs.String(), "injected write failure"); n != 1 {
		t.Fatalf("a failing streak logs once, got %d:\n%s", n, logs.String())
	}
	if rec := reg.record(); len(rec.Last) != 1 {
		t.Fatalf("memory keeps what was seen while the write fails: %v", rec.Last)
	}

	store.set(false)
	reg.tick(snap, registryT0.Add(5*time.Minute))
	other := newSourceRegistry(store, configured, fetch)
	other.tick(newAnomalyTestSnapshot(), registryT0.Add(6*time.Minute))
	if rec := other.record(); len(rec.Last) != 1 {
		t.Fatalf("the retried write reached the table: %v", rec.Last)
	}
	if !strings.Contains(logs.String(), "writing again") {
		t.Errorf("recovery is logged:\n%s", logs.String())
	}
}

// The seven days are time spent watching, not time since the first start:
// backend downtime and SignalK outages are not credited.
func TestRegistryCountsWatchedTimeNotWallClock(t *testing.T) {
	const day = 24 * time.Hour
	run := func(outageDays int) sourceRecord {
		store := newRegistryStore(t)
		configured, fetch := unconfiguredSeed()
		reg := newSourceRegistry(store, configured, fetch)
		reg.creditCap = 2 * day // each tick below is a day apart
		snap := newAnomalyTestSnapshot()
		at := registryT0
		feed := func(on bool) {
			if on {
				reportAt(snap, "propulsion.0.exhaustTemperature", at, time.Second)
			}
			reg.tick(snap, at)
			at = at.Add(day)
		}
		feed(true)
		feed(true) // day 1: one day watched
		for i := 0; i < outageDays; i++ {
			feed(false) // SignalK down: the tree goes stale
		}
		for i := 0; i < 6; i++ {
			feed(true)
		}
		return reg.record()
	}

	if rec := run(0); !rec.Covers {
		t.Fatalf("seven days of the tree being fed covers: %+v", rec)
	}
	rec := run(4)
	if rec.Covers {
		t.Fatalf("a four-day outage must not count toward the week: %+v", rec)
	}
	if got := signatureState(false, rec, sourceEngine, "propulsion.0"); got != signatureUnknown {
		t.Errorf("not yet judged: %s", got)
	}
}

func TestRegistrySeedsOnceFromInfluxHistory(t *testing.T) {
	store := newRegistryStore(t)
	history := sourceHistory{Last: signatureHistoryFixture(t).Last, Earliest: registryT0.AddDate(0, 0, -30)}
	var fetches int
	fetchHistory := func() (sourceHistory, error) {
		fetches++
		return history, nil
	}
	reg := newSourceRegistry(store, func() bool { return true }, fetchHistory)
	snap := newAnomalyTestSnapshot()

	reg.tick(snap, registryT0)
	rec := reg.record()
	if !rec.Seeded || !rec.Covers {
		t.Fatalf("a seeded record judges at once: %+v", rec)
	}
	port := time.Date(2026, 10, 16, 0, 56, 35, 331000000, time.UTC)
	if got := rec.Last["propulsion.port.revolutions"]; !got.Equal(port) {
		t.Errorf("port last seen = %v", got)
	}
	if got := signatureState(false, rec, sourceEngine, "propulsion.0"); got != signatureMissing {
		t.Errorf("propulsion.0 logged only exhaust temperature: %s", got)
	}
	if got := signatureState(false, rec, sourceEngine, "propulsion.2"); got != signatureUnknown {
		t.Errorf("propulsion.2 never reported anything: %s", got)
	}

	reg.tick(snap, registryT0.Add(time.Hour))
	again := newSourceRegistry(store, func() bool { return true }, fetchHistory)
	again.tick(snap, registryT0.Add(2*time.Hour))
	if fetches != 1 || !again.record().Seeded {
		t.Fatalf("a seeded vessel is not seeded again (fetches=%d)", fetches)
	}
}

// A seed covers only the span of history it read: two days of logging is not
// the week a source needs to be called missing, but it counts toward it with
// the time Helmcentral then watches.
func TestRegistrySeedCoverageFollowsTheSpanItRead(t *testing.T) {
	seed := func(days int) *sourceRegistry {
		history := sourceHistory{
			Last:     map[string]time.Time{"propulsion.0.exhaustTemperature": registryT0},
			Earliest: registryT0.AddDate(0, 0, -days),
		}
		return newSourceRegistry(newRegistryStore(t), func() bool { return true },
			func() (sourceHistory, error) { return history, nil })
	}
	snap := newAnomalyTestSnapshot()

	short := seed(2)
	short.tick(snap, registryT0)
	if rec := short.record(); !rec.Seeded || rec.Covers {
		t.Fatalf("two days of history is a seed but not coverage: %+v", rec)
	}
	if got := signatureState(false, short.record(), sourceEngine, "propulsion.0"); got != signatureUnknown {
		t.Errorf("not judged on two days: %s", got)
	}

	// Five more days of watching make the week together.
	now := registryT0
	for i := 0; i < 5*24*60+10; i++ {
		now = now.Add(time.Minute)
		reportAt(snap, "propulsion.0.exhaustTemperature", now, time.Second)
		short.tick(snap, now)
	}
	if rec := short.record(); !rec.Covers {
		t.Fatalf("two days seeded plus five watched cover the week: %+v", rec)
	}

	long := seed(30)
	long.tick(snap, registryT0)
	if rec := long.record(); !rec.Seeded || !rec.Covers {
		t.Fatalf("thirty days of history covers: %+v", rec)
	}
	// The span survives a restart.
	long2 := newSourceRegistry(long.store, func() bool { return true }, func() (sourceHistory, error) { t.Fatal("seeded once"); return sourceHistory{}, nil })
	long2.tick(snap, registryT0.Add(time.Minute))
	if rec := long2.record(); !rec.Covers {
		t.Fatalf("coverage is remembered: %+v", rec)
	}
}

// A seed that returned nothing under an equipment source is no coverage, and
// is not repeated.
func TestRegistryEmptySeedGivesNoCoverageAndIsNotRepeated(t *testing.T) {
	store := newRegistryStore(t)
	fetches := 0
	fetchHistory := func() (sourceHistory, error) {
		fetches++
		return sourceHistory{Last: map[string]time.Time{"navigation.speedOverGround": registryT0}, Earliest: registryT0.AddDate(0, 0, -30)}, nil
	}
	reg := newSourceRegistry(store, func() bool { return true }, fetchHistory)
	snap := newAnomalyTestSnapshot()
	reportAt(snap, "propulsion.0.exhaustTemperature", registryT0, time.Second)

	reg.tick(snap, registryT0)
	reg.tick(snap, registryT0.Add(time.Minute))
	rec := reg.record()
	if rec.Seeded || rec.Covers {
		t.Fatalf("an empty seed covers nothing: %+v", rec)
	}
	if got := signatureState(false, rec, sourceEngine, "propulsion.0"); got != signatureUnknown {
		t.Errorf("propulsion.0 is not judged on an empty seed: %s", got)
	}
	newSourceRegistry(store, func() bool { return true }, fetchHistory).tick(snap, registryT0.Add(2*time.Minute))
	if fetches != 1 {
		t.Fatalf("an empty seed is remembered, not retried: %d fetches", fetches)
	}
}

// InfluxDB logs this boat only, so a new self URN is not seeded again.
func TestRegistrySeedsAtMostOncePerInstallation(t *testing.T) {
	store := newRegistryStore(t)
	history := sourceHistory{Last: map[string]time.Time{"propulsion.port.revolutions": registryT0}, Earliest: registryT0.AddDate(0, 0, -30)}
	fetches := 0
	reg := newSourceRegistry(store, func() bool { return true }, func() (sourceHistory, error) {
		fetches++
		return history, nil
	})
	a, b := vesselSnapshot("a"), vesselSnapshot("b")
	reg.tick(a, registryT0)
	reg.tick(b, registryT0.Add(time.Minute))
	if fetches != 1 {
		t.Fatalf("the second vessel is not seeded: %d fetches", fetches)
	}
	if rec := reg.record(); rec.Seeded || len(rec.Last) != 0 {
		t.Fatalf("the second vessel starts from live tracking: %+v", rec)
	}
	// Also after a restart.
	restarted := newSourceRegistry(store, func() bool { return true }, func() (sourceHistory, error) {
		fetches++
		return history, nil
	})
	restarted.tick(vesselSnapshot("c"), registryT0.Add(2*time.Minute))
	if fetches != 1 {
		t.Fatalf("a third vessel after a restart is not seeded: %d fetches", fetches)
	}
}

func TestRegistryRetriesAFailedSeedButNotAnUnconfiguredOne(t *testing.T) {
	var logs bytes.Buffer
	prev := log.Writer()
	log.SetOutput(&logs)
	t.Cleanup(func() { log.SetOutput(prev) })

	store := newRegistryStore(t)
	fetches := 0
	var failWith error = errors.New("connection refused")
	reg := newSourceRegistry(store, func() bool { return true }, func() (sourceHistory, error) {
		fetches++
		if failWith != nil {
			return sourceHistory{}, failWith
		}
		return sourceHistory{Last: map[string]time.Time{"propulsion.port.revolutions": registryT0}, Earliest: registryT0.AddDate(0, 0, -30)}, nil
	})
	snap := newAnomalyTestSnapshot()

	reg.tick(snap, registryT0)
	reg.tick(snap, registryT0.Add(time.Minute))
	if fetches != 1 {
		t.Fatalf("a failed seed waits %v before trying again: %d", sourceSeedRetry, fetches)
	}
	reg.tick(snap, registryT0.Add(sourceSeedRetry+time.Minute))
	if fetches != 2 {
		t.Fatalf("retry after the wait: %d", fetches)
	}
	failWith = nil
	reg.tick(snap, registryT0.Add(2*sourceSeedRetry+2*time.Minute))
	if !reg.record().Seeded {
		t.Fatal("the retry seeds")
	}
	if n := strings.Count(logs.String(), "connection refused"); n != 1 {
		t.Fatalf("a failing streak logs once, got %d", n)
	}

	unconfigured := newSourceRegistry(newRegistryStore(t), func() bool { return false }, func() (sourceHistory, error) {
		t.Fatal("an unconfigured Influx is never queried")
		return sourceHistory{}, nil
	})
	unconfigured.tick(snap, registryT0)
	unconfigured.tick(snap, registryT0.Add(24*time.Hour))
	if unconfigured.record().Seeded {
		t.Error("nothing seeded without Influx")
	}
}

func TestRegistryReloadsWhenTheSelfVesselChanges(t *testing.T) {
	store := newRegistryStore(t)
	configured, fetch := unconfiguredSeed()
	reg := newSourceRegistry(store, configured, fetch)

	a, b := vesselSnapshot("a"), vesselSnapshot("b")
	reportOn(a, "a", "propulsion.port.revolutions", registryT0, time.Second)
	reg.tick(a, registryT0)
	reg.tick(b, registryT0.Add(time.Minute))
	if rec := reg.record(); len(rec.Last) != 0 {
		t.Fatalf("another boat's sightings are never served as this boat's: %v", rec.Last)
	}
	reg.tick(a, registryT0.Add(2*time.Minute))
	if rec := reg.record(); len(rec.Last) != 1 {
		t.Fatalf("the first boat's record comes back from the table: %v", rec.Last)
	}
}

// A change of self vessel must not lose sightings that were still waiting to
// be written.
func TestRegistryKeepsPendingSightingsAcrossAVesselChange(t *testing.T) {
	store := newRegistryStore(t)
	configured, fetch := unconfiguredSeed()
	reg := newSourceRegistry(store, configured, fetch)
	a, b := vesselSnapshot("a"), vesselSnapshot("b")

	reportOn(a, "a", "propulsion.port.revolutions", registryT0, time.Second)
	store.set(true)
	reg.tick(a, registryT0) // seen, but the write fails: pending
	reg.tick(b, registryT0.Add(time.Minute))
	store.set(false)
	reg.tick(b, registryT0.Add(2*time.Minute)) // the stash is written now

	// The boat is back, its engine off: only the table can still know the sighting.
	reg.tick(vesselSnapshot("a"), registryT0.Add(3*time.Minute))
	if rec := reg.record(); len(rec.Last) != 1 {
		t.Fatalf("the sighting made before the vessel changed survived: %v", rec.Last)
	}
}

// Going A -> B -> A -> B while writes keep failing must neither lose the
// sightings of the first visit when A is current again, nor drop them when A
// is left a second time.
func TestRegistryMergesStashesAcrossRepeatedVesselChanges(t *testing.T) {
	store := newRegistryStore(t)
	configured, fetch := unconfiguredSeed()
	reg := newSourceRegistry(store, configured, fetch)
	a, b := vesselSnapshot("a"), vesselSnapshot("b")
	store.set(true)

	reportOn(a, "a", "propulsion.port.revolutions", registryT0, time.Second)
	reg.tick(a, registryT0)
	reg.tick(b, registryT0.Add(time.Minute))

	back := vesselSnapshot("a")
	reg.tick(back, registryT0.Add(2*time.Minute))
	if rec := reg.record(); len(rec.Last) != 1 {
		t.Fatalf("returning to a vessel serves the sightings still waiting to be written: %v", rec.Last)
	}

	reportOn(back, "a", "propulsion.starboard.revolutions", registryT0.Add(3*time.Minute), time.Second)
	reg.tick(back, registryT0.Add(3*time.Minute))
	reg.tick(b, registryT0.Add(4*time.Minute))

	store.set(false)
	reg.tick(b, registryT0.Add(5*time.Minute)) // both visits' sightings are written now
	reg.tick(vesselSnapshot("a"), registryT0.Add(6*time.Minute))
	rec := reg.record()
	if _, ok := rec.Last["propulsion.port.revolutions"]; !ok || len(rec.Last) != 2 {
		t.Fatalf("both visits' sightings survived: %v", rec.Last)
	}
}

// If the new vessel's record cannot be read, nothing is served for it (never
// the previous vessel's paths) and readers are told why.
func TestRegistryServesNothingAndSaysWhyWhenTheLoadFails(t *testing.T) {
	store := newRegistryStore(t)
	configured, fetch := unconfiguredSeed()
	reg := newSourceRegistry(store, configured, fetch)
	a, b := vesselSnapshot("a"), vesselSnapshot("b")
	reportOn(a, "a", "propulsion.port.revolutions", registryT0, time.Second)
	reg.tick(a, registryT0)

	store.setLoad(true)
	reg.tick(b, registryT0.Add(time.Minute))
	rec := reg.record()
	if rec.Err == nil || len(rec.Last) != 0 || rec.Covers {
		t.Fatalf("a failed load serves nothing and carries the error: %+v", rec)
	}
	if got := signatureState(false, rec, sourceEngine, "propulsion.port"); got != signatureUnknown {
		t.Errorf("nothing is judged against an unreadable record: %s", got)
	}

	store.setLoad(false)
	reg.tick(b, registryT0.Add(2*time.Minute))
	if rec := reg.record(); rec.Err != nil {
		t.Fatalf("recovers on the next tick: %+v", rec)
	}
}

// Readers take the published record without the registry lock.
func TestRegistryRecordIsSafeToReadWhileTicking(t *testing.T) {
	store := newRegistryStore(t)
	configured, fetch := unconfiguredSeed()
	reg := newSourceRegistry(store, configured, fetch)
	snap := newAnomalyTestSnapshot()
	var wg sync.WaitGroup
	stop := make(chan struct{})
	for i := 0; i < 4; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for {
				select {
				case <-stop:
					return
				default:
					rec := reg.record()
					_ = signatureState(false, rec, sourceEngine, "propulsion.port")
					_ = rec.seenInstances(sourceEngine)
				}
			}
		}()
	}
	for i := 0; i < 50; i++ {
		now := registryT0.Add(time.Duration(i) * time.Minute)
		reportAt(snap, "propulsion.port.revolutions", now, time.Second)
		reg.tick(snap, now)
	}
	close(stop)
	wg.Wait()
}
