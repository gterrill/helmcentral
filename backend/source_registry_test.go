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
// wrote rows and can be told to fail them.
type flakyRegistryStore struct {
	inner *sourceRegistryStore
	mu    sync.Mutex
	saves int
	rows  int
	fail  bool
}

func (f *flakyRegistryStore) load(vessel string) ([]seenRow, *registryMeta, error) {
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

func (f *flakyRegistryStore) set(fail bool) { f.mu.Lock(); f.fail = fail; f.mu.Unlock() }
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

func unconfiguredSeed() (func() bool, func() (map[string]time.Time, error)) {
	return func() bool { return false }, func() (map[string]time.Time, error) { panic("seed must not query") }
}

// reportAt puts a self-vessel reading in the tree that arrived age ago.
func reportAt(s *signalKSnapshot, path string, now time.Time, age time.Duration) {
	applyNumeric(s, "vessels.self", path, 1, now.Add(-age))
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

	rec := reg.record(registryT0)
	if _, ok := rec.Last["propulsion.port.revolutions"]; !ok {
		t.Fatalf("an engine reading is recorded: %v", rec.Last)
	}
	if got := rec.Last["propulsion.port.revolutions"]; !got.Equal(registryT0.Add(-5 * time.Second)) {
		t.Errorf("last seen is when the reading arrived, got %v", got)
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

	for i := 1; i <= 4; i++ {
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

	rec := second.record(later)
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
	if rec := reg.record(registryT0); len(rec.Last) != 1 {
		t.Fatalf("memory keeps what was seen while the write fails: %v", rec.Last)
	}

	store.set(false)
	reg.tick(snap, registryT0.Add(5*time.Minute))
	if saves, _ := store.counts(); saves == 0 {
		t.Fatal("the pending rows are retried")
	}
	other := newSourceRegistry(store, configured, fetch)
	other.tick(newAnomalyTestSnapshot(), registryT0.Add(6*time.Minute))
	if rec := other.record(registryT0); len(rec.Last) != 1 {
		t.Fatalf("the retried write reached the table: %v", rec.Last)
	}
	if !strings.Contains(logs.String(), "writing again") {
		t.Errorf("recovery is logged:\n%s", logs.String())
	}
}

func TestRegistryJudgesAfterSevenDaysOrASeed(t *testing.T) {
	store := newRegistryStore(t)
	configured, fetch := unconfiguredSeed()
	reg := newSourceRegistry(store, configured, fetch)
	snap := newAnomalyTestSnapshot()
	reportAt(snap, "propulsion.0.exhaustTemperature", registryT0, time.Second)
	reg.tick(snap, registryT0)

	if reg.record(registryT0.Add(sourceRecordMinTracking - time.Hour)).Covers {
		t.Error("six days is not enough to call a source missing its signature")
	}
	rec := reg.record(registryT0.Add(sourceRecordMinTracking))
	if !rec.Covers || rec.Seeded {
		t.Errorf("seven days of tracking covers: %+v", rec)
	}
	if got := signatureState(false, rec, sourceEngine, "propulsion.0"); got != signatureMissing {
		t.Errorf("after the covered period: %s", got)
	}
	early := reg.record(registryT0.Add(time.Hour))
	if got := signatureState(false, early, sourceEngine, "propulsion.0"); got != signatureUnknown {
		t.Errorf("before it: %s", got)
	}
}

func TestRegistrySeedsOnceFromInfluxHistory(t *testing.T) {
	store := newRegistryStore(t)
	history := signatureHistoryFixture(t).Last
	var fetches int
	reg := newSourceRegistry(store, func() bool { return true }, func() (map[string]time.Time, error) {
		fetches++
		return history, nil
	})
	snap := newAnomalyTestSnapshot()

	reg.tick(snap, registryT0)
	rec := reg.record(registryT0)
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
	if fetches != 1 {
		t.Fatalf("the seed runs once, ran %d times", fetches)
	}
	// And it is remembered across a restart.
	again := newSourceRegistry(store, func() bool { return true }, func() (map[string]time.Time, error) {
		fetches++
		return history, nil
	})
	again.tick(snap, registryT0.Add(2*time.Hour))
	if fetches != 1 || !again.record(registryT0).Seeded {
		t.Fatalf("a seeded vessel is not seeded again (fetches=%d)", fetches)
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
	reg := newSourceRegistry(store, func() bool { return true }, func() (map[string]time.Time, error) {
		fetches++
		if failWith != nil {
			return nil, failWith
		}
		return map[string]time.Time{"propulsion.port.revolutions": registryT0}, nil
	})
	snap := newAnomalyTestSnapshot()

	reg.tick(snap, registryT0)
	reg.tick(snap, registryT0.Add(time.Minute))
	if fetches != 1 {
		t.Fatalf("a failed seed waits %v before trying again: %d", sourceSeedRetry, fetches)
	}
	if reg.record(registryT0).Seeded {
		t.Fatal("a failed seed is not recorded as done")
	}
	reg.tick(snap, registryT0.Add(sourceSeedRetry+time.Minute))
	if fetches != 2 {
		t.Fatalf("retry after the wait: %d", fetches)
	}
	failWith = nil
	reg.tick(snap, registryT0.Add(2*sourceSeedRetry+2*time.Minute))
	if !reg.record(registryT0).Seeded {
		t.Fatal("the retry seeds")
	}
	if n := strings.Count(logs.String(), "connection refused"); n != 1 {
		t.Fatalf("a failing streak logs once, got %d", n)
	}

	// No Influx: live tracking only, nothing queried, nothing retried.
	unconfigured := newSourceRegistry(newRegistryStore(t), func() bool { return false }, func() (map[string]time.Time, error) {
		t.Fatal("an unconfigured Influx is never queried")
		return nil, nil
	})
	unconfigured.tick(snap, registryT0)
	unconfigured.tick(snap, registryT0.Add(24*time.Hour))
	if unconfigured.record(registryT0).Seeded {
		t.Error("nothing seeded without Influx")
	}
}

func TestRegistryReloadsWhenTheSelfVesselChanges(t *testing.T) {
	store := newRegistryStore(t)
	configured, fetch := unconfiguredSeed()
	reg := newSourceRegistry(store, configured, fetch)

	a := newSignalKSnapshot()
	a.setSelfContext("vessels.urn:mrn:imo:mmsi:111")
	applyNumeric(a, "vessels.urn:mrn:imo:mmsi:111", "propulsion.port.revolutions", 1, registryT0.Add(-time.Second))
	reg.tick(a, registryT0)

	b := newSignalKSnapshot()
	b.setSelfContext("vessels.urn:mrn:imo:mmsi:222")
	reg.tick(b, registryT0.Add(time.Minute))
	if rec := reg.record(registryT0.Add(time.Minute)); len(rec.Last) != 0 {
		t.Fatalf("another boat's sightings are never served as this boat's: %v", rec.Last)
	}
	reg.tick(a, registryT0.Add(2*time.Minute))
	if rec := reg.record(registryT0.Add(2 * time.Minute)); len(rec.Last) != 1 {
		t.Fatalf("the first boat's record comes back from the table: %v", rec.Last)
	}
}
