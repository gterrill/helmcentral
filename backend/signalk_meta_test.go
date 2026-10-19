package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/labstack/echo/v4"
)

// The frames under testdata/signalk_meta are real: captured from the boat's
// SignalK 2.33.0 stream with sendMeta=all (see README.txt there). The earlier
// tests of unit lookup wrote meta straight into the tree, a shape the stream
// never produced, which is how units went missing in production unnoticed.

const socPath = "electrical.batteries.512.capacity.stateOfCharge"

func metaFixture(t *testing.T, name string) signalKDelta {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join("testdata", "signalk_meta", name))
	if err != nil {
		t.Fatalf("read fixture %s: %v", name, err)
	}
	var d signalKDelta
	if err := json.Unmarshal(raw, &d); err != nil {
		t.Fatalf("decode fixture %s: %v", name, err)
	}
	return d
}

func metaFixtureRaw(t *testing.T, name string) string {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join("testdata", "signalk_meta", name))
	if err != nil {
		t.Fatalf("read fixture %s: %v", name, err)
	}
	return string(raw)
}

// snapshotFromFixtures is a snapshot that has seen the real hello frame.
func snapshotFromFixtures(t *testing.T) *signalKSnapshot {
	t.Helper()
	s := newSignalKSnapshot()
	s.setSelfContext(metaFixture(t, "hello.json").Self)
	return s
}

func openUnitStore(t *testing.T, path string) (*documentStore, *signalKUnitStore) {
	t.Helper()
	ds, err := newDocumentStore(path)
	if err != nil {
		t.Fatalf("newDocumentStore: %v", err)
	}
	t.Cleanup(func() { ds.Close() })
	us, err := newSignalKUnitStore(ds)
	if err != nil {
		t.Fatalf("newSignalKUnitStore: %v", err)
	}
	return ds, us
}

// countingUnits wraps the real store: it counts the transactions that wrote
// rows and can be told to fail them, so tests observe the flusher without
// the production store carrying test-only state.
type countingUnits struct {
	inner *signalKUnitStore
	mu    sync.Mutex
	saves int
	fail  bool
}

func (c *countingUnits) load(vessel string) (map[string]string, error) { return c.inner.load(vessel) }

func (c *countingUnits) save(changes []rememberedUnit) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.fail {
		return fmt.Errorf("injected write failure")
	}
	if err := c.inner.save(changes); err != nil {
		return err
	}
	c.saves++
	return nil
}

func (c *countingUnits) setFail(v bool) { c.mu.Lock(); c.fail = v; c.mu.Unlock() }
func (c *countingUnits) count() int     { c.mu.Lock(); defer c.mu.Unlock(); return c.saves }

func declareUnit(s *signalKSnapshot, vessel, path, units string) {
	s.applyDelta(signalKDelta{Context: vessel, Updates: []signalKUpdate{{
		Meta: []signalKMeta{{Path: path, Value: map[string]any{"units": units}}},
	}}}, time.Now().UTC())
}

func TestStreamedMetaGivesTheBoatsUnit(t *testing.T) {
	stub := newStreamStub(func(ctx context.Context, c *websocket.Conn, _ int) {
		_ = writeFrame(ctx, c, metaFixtureRaw(t, "hello.json"))
		_ = writeFrame(ctx, c, metaFixtureRaw(t, "meta_soc.json"))
		_ = writeFrame(ctx, c, metaFixtureRaw(t, "values_soc.json"))
		<-ctx.Done()
	})
	defer stub.close()

	snapshot := newSignalKSnapshot()
	client := testStreamClient(snapshot)
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	go client.connectOnce(ctx, stub.wsURL(), "")

	waitFor(t, 2*time.Second, "unit to resolve from streamed meta", func() bool {
		return unitForAlarmPath(snapshot, socPath) == "ratio"
	})
}

func TestMetaNeverEntersTheTree(t *testing.T) {
	s := snapshotFromFixtures(t)
	now := time.Now().UTC()
	self := metaFixture(t, "hello.json").Self
	s.applyDelta(metaFixture(t, "values_soc.json"), now)
	s.applyDelta(signalKDelta{Context: self, Updates: []signalKUpdate{{
		Values: []signalKValue{{Path: "name", Value: "Pikorua"}},
	}}}, now)
	before, _ := json.Marshal(s.treeFor(self))

	s.applyDelta(metaFixture(t, "meta_soc.json"), now)
	// Meta at a scalar, under a scalar, on a branch, and on a path nothing
	// has ever published: none may touch the tree.
	s.applyDelta(signalKDelta{Context: self, Updates: []signalKUpdate{{
		Meta: []signalKMeta{
			{Path: "name", Value: map[string]any{"units": "x", "description": "d"}},
			{Path: "name.deeper.still", Value: map[string]any{"units": "y"}},
			{Path: "electrical.batteries", Value: map[string]any{"description": "Batteries"}},
			{Path: "never.published", Value: map[string]any{"units": "m"}},
		},
	}}}, now)

	after, _ := json.Marshal(s.treeFor(self))
	if string(before) != string(after) {
		t.Fatalf("meta changed the tree:\nbefore %s\nafter  %s", before, after)
	}
	if got := s.liveSelfTelemetry(now).Sample("name").Value; got != "Pikorua" {
		t.Fatalf("scalar clobbered: %v", got)
	}
	if got := s.liveSelfTelemetry(now).Children("electrical.batteries"); len(got) != 1 || got[0] != "512" {
		t.Fatalf("children = %v, want [512]", got)
	}
	// The declared units are still known.
	if got := unitForAlarmPath(s, "never.published"); got != "m" {
		t.Fatalf("units for a path with meta but no value = %q, want m", got)
	}
}

func TestLiveUnitsAreCapped(t *testing.T) {
	s := snapshotFromFixtures(t)
	self := metaFixture(t, "hello.json").Self
	metas := make([]signalKMeta, 0, signalKContextMaxDistinctPaths+50)
	for i := 0; i < signalKContextMaxDistinctPaths+50; i++ {
		metas = append(metas, signalKMeta{Path: fmt.Sprintf("p.n%d", i), Value: map[string]any{"units": "m"}})
	}
	s.applyDelta(signalKDelta{Context: self, Updates: []signalKUpdate{{Meta: metas}}}, time.Now().UTC())
	s.mu.RLock()
	n := len(s.rememberedUnits)
	s.mu.RUnlock()
	if n != signalKContextMaxDistinctPaths {
		t.Fatalf("rememberedUnits = %d, want capped at %d", n, signalKContextMaxDistinctPaths)
	}
}

// freshSnapshotWithRemembered is a restarted process: the unit is only in
// SQLite, and the live tree holds the value but never saw the meta.
func freshSnapshotWithRemembered(t *testing.T) *signalKSnapshot {
	t.Helper()
	_, us := openUnitStore(t, filepath.Join(t.TempDir(), "helmcentral.sqlite"))
	first := snapshotFromFixtures(t)
	if err := first.useUnitStore(us); err != nil {
		t.Fatal(err)
	}
	first.applyDelta(metaFixture(t, "meta_soc.json"), time.Now().UTC())
	first.flushUnits()

	second := snapshotFromFixtures(t)
	if err := second.useUnitStore(us); err != nil {
		t.Fatal(err)
	}
	second.applyDelta(metaFixture(t, "values_soc.json"), time.Now().UTC())
	return second
}

func TestEveryReaderResolvesRememberedUnit(t *testing.T) {
	s := freshSnapshotWithRemembered(t)
	now := time.Now().UTC()

	if got := s.liveSelfTelemetry(now).Sample(socPath); got.Units != "ratio" {
		t.Fatalf("live Sample units = %q, want ratio", got.Units)
	}
	if got := s.selfTelemetryAt(now).Sample(socPath); got.Units != "ratio" {
		t.Fatalf("copied Sample units = %q, want ratio", got.Units)
	}
	if got := s.liveSelfTelemetry(now).unitsAt(socPath); got != "ratio" {
		t.Fatalf("unitsAt = %q", got)
	}
	for _, view := range []*telemetryView{s.liveSelfTelemetry(now), s.selfTelemetryAt(now)} {
		found := false
		for _, sample := range view.pathSamples() {
			if sample.Path == socPath {
				found = true
				if sample.Units != "ratio" {
					t.Fatalf("pathSamples units = %q, want ratio", sample.Units)
				}
			}
		}
		if !found {
			t.Fatal("pathSamples did not list the path")
		}
		for _, p := range view.Paths() {
			if p.Path == socPath && p.Units != "ratio" {
				t.Fatalf("Paths units = %q, want ratio", p.Units)
			}
		}
	}
}

// A view method that re-took the snapshot read lock while withRoot already
// held it would deadlock once a writer queued between the two. Hammer the
// readers against a writer to catch that.
func TestUnitLookupDoesNotDeadlockBehindAWriter(t *testing.T) {
	s := freshSnapshotWithRemembered(t)
	self := metaFixture(t, "hello.json").Self
	stop := make(chan struct{})
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := 0; ; i++ {
			select {
			case <-stop:
				return
			default:
			}
			s.applyDelta(signalKDelta{Context: self, Updates: []signalKUpdate{{
				Meta:   []signalKMeta{{Path: fmt.Sprintf("x.n%d", i%50), Value: map[string]any{"units": "m"}}},
				Values: []signalKValue{{Path: "x.v", Value: float64(i)}},
			}}}, time.Now().UTC())
		}
	}()
	done := make(chan struct{})
	go func() {
		defer close(done)
		now := time.Now().UTC()
		for i := 0; i < 3000; i++ {
			v := s.liveSelfTelemetry(now)
			v.Sample(socPath)
			v.unitsAt(socPath)
			v.pathSamples()
			unitForAlarmPath(s, socPath)
		}
	}()
	select {
	case <-done:
	case <-time.After(20 * time.Second):
		t.Fatal("unit lookup deadlocked behind a queued writer")
	}
	close(stop)
	wg.Wait()
}

func TestFailedFlushKeepsDeclaredUnitAndRetries(t *testing.T) {
	_, inner := openUnitStore(t, filepath.Join(t.TempDir(), "helmcentral.sqlite"))
	us := &countingUnits{inner: inner}
	s := snapshotFromFixtures(t)
	if err := s.useUnitStore(us); err != nil {
		t.Fatal(err)
	}
	self := metaFixture(t, "hello.json").Self
	declareUnit(s, self, socPath, "ratio")
	s.flushUnits()

	us.setFail(true)
	declareUnit(s, self, socPath, "percent")
	s.flushUnits()
	if got := s.rememberedUnit(socPath); got != "percent" {
		t.Fatalf("after a failed flush memory = %q, want the declared percent", got)
	}
	us.setFail(false)
	s.flushUnits()
	loaded, err := inner.load(self)
	if err != nil || loaded[socPath] != "percent" {
		t.Fatalf("after a good flush disk = %v (err %v), want percent", loaded, err)
	}
}

func TestDeclaredAThenBThenCEndsAtC(t *testing.T) {
	_, inner := openUnitStore(t, filepath.Join(t.TempDir(), "helmcentral.sqlite"))
	us := &countingUnits{inner: inner}
	s := snapshotFromFixtures(t)
	if err := s.useUnitStore(us); err != nil {
		t.Fatal(err)
	}
	self := metaFixture(t, "hello.json").Self
	declareUnit(s, self, socPath, "A")
	s.flushUnits()

	us.setFail(true)
	declareUnit(s, self, socPath, "B")
	s.flushUnits()
	declareUnit(s, self, socPath, "C")
	s.flushUnits()
	if got := s.rememberedUnit(socPath); got != "C" {
		t.Fatalf("memory = %q, want C", got)
	}
	us.setFail(false)
	s.flushUnits()
	if got := s.rememberedUnit(socPath); got != "C" {
		t.Fatalf("memory after good flush = %q, want C", got)
	}
	loaded, _ := inner.load(self)
	if loaded[socPath] != "C" {
		t.Fatalf("disk = %q, want C", loaded[socPath])
	}
}

func TestUnitsAreKeyedByVessel(t *testing.T) {
	_, inner := openUnitStore(t, filepath.Join(t.TempDir(), "helmcentral.sqlite"))
	s := snapshotFromFixtures(t)
	if err := s.useUnitStore(inner); err != nil {
		t.Fatal(err)
	}
	a := metaFixture(t, "hello.json").Self
	b := "vessels.urn:mrn:imo:mmsi:503000999"
	declareUnit(s, a, socPath, "ratio")
	s.flushUnits()

	s.setSelfContext(b)
	if got := unitForAlarmPath(s, socPath); got != "" {
		t.Fatalf("vessel B resolved vessel A's unit %q", got)
	}
	declareUnit(s, b, "other.path", "m")
	// B has not flushed yet; going back to A must still find A's unit and
	// B's pending declaration must survive the round trip.
	s.setSelfContext(a)
	if got := unitForAlarmPath(s, socPath); got != "ratio" {
		t.Fatalf("back on vessel A unit = %q, want ratio", got)
	}
	if got := unitForAlarmPath(s, "other.path"); got != "" {
		t.Fatalf("vessel A resolved vessel B's unit %q", got)
	}
	s.flushUnits()
	s.setSelfContext(b)
	if got := unitForAlarmPath(s, "other.path"); got != "m" {
		t.Fatalf("vessel B unit = %q, want m", got)
	}
	loaded, _ := inner.load(b)
	if loaded["other.path"] != "m" || len(loaded) != 1 {
		t.Fatalf("vessel B rows = %v", loaded)
	}
}

func TestOldUnitTableShapeIsRecreated(t *testing.T) {
	ds, err := newDocumentStore(filepath.Join(t.TempDir(), "helmcentral.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { ds.Close() })
	if _, err := ds.db.Exec(`CREATE TABLE signalk_path_units (path TEXT PRIMARY KEY, units TEXT NOT NULL, declared_at INTEGER NOT NULL)`); err != nil {
		t.Fatal(err)
	}
	if _, err := ds.db.Exec(`INSERT INTO signalk_path_units VALUES ('a.b','m',1)`); err != nil {
		t.Fatal(err)
	}
	us, err := newSignalKUnitStore(ds)
	if err != nil {
		t.Fatalf("old table shape not tolerated: %v", err)
	}
	if err := us.save([]rememberedUnit{{vessel: "v", path: "a.b", units: "s"}}); err != nil {
		t.Fatal(err)
	}
	if got, _ := us.load("v"); got["a.b"] != "s" {
		t.Fatalf("load = %v", got)
	}
}

func TestCopiedViewResolvesUnitsWithoutTheSnapshotLock(t *testing.T) {
	s := freshSnapshotWithRemembered(t)
	view := s.selfTelemetryAt(time.Now().UTC())

	s.mu.Lock() // a writer holds the snapshot
	done := make(chan []signalKPath, 1)
	go func() { done <- view.Paths() }()
	select {
	case paths := <-done:
		found := false
		for _, p := range paths {
			if p.Path == socPath {
				found = p.Units == "ratio"
			}
		}
		if !found {
			t.Error("copied Paths did not carry the remembered unit")
		}
	case <-time.After(2 * time.Second):
		s.mu.Unlock()
		t.Fatal("copied view's Paths waited on the snapshot lock")
	}
	s.mu.Unlock()
}

func TestBurstOfMetaIsOneTransaction(t *testing.T) {
	_, inner := openUnitStore(t, filepath.Join(t.TempDir(), "helmcentral.sqlite"))
	us := &countingUnits{inner: inner}
	s := snapshotFromFixtures(t)
	if err := s.useUnitStore(us); err != nil {
		t.Fatal(err)
	}
	self := metaFixture(t, "hello.json").Self
	for i := 0; i < 600; i++ {
		declareUnit(s, self, fmt.Sprintf("sensors.n%d", i), "m")
	}
	if us.count() != 0 {
		t.Fatalf("applyDelta wrote %d transactions on the stream goroutine", us.count())
	}
	if got := s.rememberedUnit("sensors.n599"); got != "m" {
		t.Fatalf("remembered not updated in memory: %q", got)
	}
	s.flushUnits()
	if us.count() != 1 {
		t.Fatalf("transactions = %d, want 1 for the burst", us.count())
	}
	loaded, err := inner.load(self)
	if err != nil || len(loaded) != 600 {
		t.Fatalf("persisted %d rows (err %v), want 600", len(loaded), err)
	}
}

func TestUnitFlusherDebouncesAndFlushesOnShutdown(t *testing.T) {
	_, us := openUnitStore(t, filepath.Join(t.TempDir(), "helmcentral.sqlite"))
	s := snapshotFromFixtures(t)
	if err := s.useUnitStore(us); err != nil {
		t.Fatal(err)
	}
	s.unitFlushDelay = 50 * time.Millisecond
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { s.runUnitFlusher(ctx); close(done) }()

	self := metaFixture(t, "hello.json").Self
	for i := 0; i < 20; i++ {
		s.applyDelta(signalKDelta{Context: self, Updates: []signalKUpdate{{
			Meta: []signalKMeta{{Path: fmt.Sprintf("a.n%d", i), Value: map[string]any{"units": "m"}}},
		}}}, time.Now().UTC())
	}
	waitFor(t, 2*time.Second, "debounced flush", func() bool {
		loaded, _ := us.load(metaFixture(t, "hello.json").Self)
		return len(loaded) == 20
	})

	s.applyDelta(signalKDelta{Context: self, Updates: []signalKUpdate{{
		Meta: []signalKMeta{{Path: "late.one", Value: map[string]any{"units": "m"}}},
	}}}, time.Now().UTC())
	cancel()
	<-done
	loaded, _ := us.load(self)
	if loaded["late.one"] != "m" {
		t.Fatal("pending units were not flushed on shutdown")
	}
}

func TestMetaWithEmptyPathOrOtherVesselIsIgnored(t *testing.T) {
	s := snapshotFromFixtures(t)
	self := metaFixture(t, "hello.json").Self
	now := time.Now().UTC()
	s.applyDelta(signalKDelta{Context: self, Updates: []signalKUpdate{{
		Meta: []signalKMeta{{Path: "", Value: map[string]any{"units": "bogus"}}},
	}}}, now)
	if got := unitForAlarmPath(s, ""); got != "" {
		t.Fatalf("empty-path meta resolved %q", got)
	}

	other := "vessels.urn:mrn:imo:mmsi:503000137"
	s.applyDelta(signalKDelta{Context: other, Updates: []signalKUpdate{{
		Meta: []signalKMeta{{Path: socPath, Value: map[string]any{"units": "ratio"}}},
	}}}, now)
	if s.treeFor(other) != nil && len(s.treeFor(other)) != 0 {
		t.Fatalf("another vessel's meta built a tree: %v", s.treeFor(other))
	}
	if got := unitForAlarmPath(s, socPath); got != "" {
		t.Fatalf("another vessel's unit leaked into self: %q", got)
	}
}

func TestPathWithoutTopLevelUnitsStaysEmpty(t *testing.T) {
	s := snapshotFromFixtures(t)
	s.applyDelta(metaFixture(t, "meta_no_units.json"), time.Now().UTC())
	if got := unitForAlarmPath(s, "design.draft"); got != "" {
		t.Fatalf("units = %q, want empty: the meta declared none at top level", got)
	}
	if got := unitForAlarmPath(s, "navigation.speedOverGround"); got != "" {
		t.Fatalf("units = %q for a path nothing declared", got)
	}
}

func TestRememberedUnitSurvivesFreshSnapshotAndReopen(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "helmcentral.sqlite")
	ds, us := openUnitStore(t, dbPath)

	first := snapshotFromFixtures(t)
	if err := first.useUnitStore(us); err != nil {
		t.Fatalf("useUnitStore: %v", err)
	}
	first.applyDelta(metaFixture(t, "meta_soc.json"), time.Now().UTC())
	first.flushUnits()

	// A fresh snapshot has no tree at all: only the remembered unit remains.
	second := snapshotFromFixtures(t)
	if got := unitForAlarmPath(second, socPath); got != "" {
		t.Fatalf("a snapshot with no store resolved %q", got)
	}
	if err := second.useUnitStore(us); err != nil {
		t.Fatalf("useUnitStore: %v", err)
	}
	if got := unitForAlarmPath(second, socPath); got != "ratio" {
		t.Fatalf("remembered unit = %q, want ratio", got)
	}

	// And across a reopen of the database file.
	if err := ds.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}
	_, reopened := openUnitStore(t, dbPath)
	third := snapshotFromFixtures(t)
	if err := third.useUnitStore(reopened); err != nil {
		t.Fatalf("useUnitStore: %v", err)
	}
	if got := unitForAlarmPath(third, socPath); got != "ratio" {
		t.Fatalf("unit after reopen = %q, want ratio", got)
	}
	if got := unitForAlarmPath(third, "design.draft"); got != "" {
		t.Fatalf("never-declared path resolved %q", got)
	}
}

func TestLiveUnitBeatsRememberedAndChangeOverwrites(t *testing.T) {
	_, us := openUnitStore(t, filepath.Join(t.TempDir(), "helmcentral.sqlite"))
	s := snapshotFromFixtures(t)
	if err := s.useUnitStore(us); err != nil {
		t.Fatal(err)
	}
	self := metaFixture(t, "hello.json").Self
	now := time.Now().UTC()
	declare := func(units string) {
		s.applyDelta(signalKDelta{Context: self, Updates: []signalKUpdate{{
			Meta: []signalKMeta{{Path: socPath, Value: map[string]any{"units": units}}},
		}}}, now)
	}
	declare("ratio")
	declare("percent")
	if got := unitForAlarmPath(s, socPath); got != "percent" {
		t.Fatalf("live unit = %q, want percent", got)
	}
	s.flushUnits()

	fresh := snapshotFromFixtures(t)
	if err := fresh.useUnitStore(us); err != nil {
		t.Fatal(err)
	}
	if got := unitForAlarmPath(fresh, socPath); got != "percent" {
		t.Fatalf("remembered unit = %q, want the changed one", got)
	}
}

func TestUnchangedUnitIsNotWrittenAgain(t *testing.T) {
	_, inner := openUnitStore(t, filepath.Join(t.TempDir(), "helmcentral.sqlite"))
	us := &countingUnits{inner: inner}
	s := snapshotFromFixtures(t)
	if err := s.useUnitStore(us); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 3; i++ {
		s.applyDelta(metaFixture(t, "meta_soc.json"), time.Now().UTC())
		s.flushUnits()
	}
	if us.count() != 1 {
		t.Fatalf("transactions = %d, want 1: the same declaration must not be stored again", us.count())
	}
}

func TestSignalKPathsListsUnitsForRealPaths(t *testing.T) {
	prev := globalSignalKSnapshot
	t.Cleanup(func() { globalSignalKSnapshot = prev })
	globalSignalKSnapshot = snapshotFromFixtures(t)
	now := time.Now().UTC()
	globalSignalKSnapshot.applyDelta(metaFixture(t, "meta_soc.json"), now)
	globalSignalKSnapshot.applyDelta(metaFixture(t, "values_soc.json"), now)

	e := echo.New()
	rec := httptest.NewRecorder()
	if err := signalKPathsHandler(e.NewContext(httptest.NewRequest(http.MethodGet, "/api/signalk/paths", nil), rec)); err != nil {
		t.Fatalf("handler: %v", err)
	}
	var body struct {
		Paths []signalKPath `json:"paths"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	for _, p := range body.Paths {
		if p.Path == socPath {
			if p.Units != "ratio" {
				t.Fatalf("units = %q, want ratio", p.Units)
			}
			return
		}
	}
	t.Fatalf("%s not listed", socPath)
}

// afterLoadUnits runs a hook once the real load has read the table, to place
// a declaration and its flush between the load and the map swap.
type afterLoadUnits struct {
	inner *signalKUnitStore
	hook  func()
}

func (a *afterLoadUnits) load(vessel string) (map[string]string, error) {
	m, err := a.inner.load(vessel)
	if a.hook != nil {
		a.hook()
	}
	return m, err
}
func (a *afterLoadUnits) save(c []rememberedUnit) error { return a.inner.save(c) }

func TestReloadKeepsAUnitDeclaredAndFlushedWhileItLoaded(t *testing.T) {
	_, inner := openUnitStore(t, filepath.Join(t.TempDir(), "helmcentral.sqlite"))
	p := &afterLoadUnits{inner: inner}
	s := newSignalKSnapshot()
	if err := s.useUnitStore(p); err != nil {
		t.Fatal(err)
	}
	vessel := metaFixture(t, "hello.json").Self
	p.hook = func() {
		declareUnit(s, vessel, "late.path", "m")
		s.flushUnits()
		p.hook = nil
	}
	s.setSelfContext(vessel)
	if got := s.rememberedUnit("late.path"); got != "m" {
		t.Fatalf("late.path unit = %q, want m", got)
	}
}
