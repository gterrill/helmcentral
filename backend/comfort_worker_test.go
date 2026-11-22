package main

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// ── when the comfort model is fitted again (ADR 0301) ────────────────────

// signalled says whether a fit is waiting, and takes the signal.
func signalled(w *comfortModelWorker) bool {
	select {
	case <-w.wake:
		return true
	default:
		return false
	}
}

func TestAPassThatStoredNothingDoesNotAskForAFit(t *testing.T) {
	store := newTestPassageStore(t)
	runSeaPass(context.Background(), store, resolveTo(&fakeHindcast{}), noCoastline, seaNow)
	if signalled(store.comfort) {
		t.Error("a pass with nothing to store asked for a fit")
	}

	store, _, _ = seaPassage(t)
	signalled(store.comfort) // working the passage's figures out asked for a fit
	runSeaPass(context.Background(), store, resolveTo(&fakeHindcast{err: context.DeadlineExceeded}), noCoastline, seaNow)
	if signalled(store.comfort) {
		t.Error("a pass whose hindcast failed stored nothing and must not ask for a fit")
	}
}

func TestAPassThatFilledTheHindcastAsksForAFit(t *testing.T) {
	store, _, _ := seaPassage(t)
	runSeaPass(context.Background(), store, resolveTo(&fakeHindcast{}), noCoastline, seaNow)
	if !signalled(store.comfort) {
		t.Error("new sea hours change what the model learns from, so the pass must ask for a fit")
	}
}

func TestAPassThatStoredFetchRowsAsksForAFit(t *testing.T) {
	rig := newFetchRig(t, 20, 90, landEastOf(2))
	signalled(rig.store.comfort) // the hindcast the rig filled
	runSeaPass(context.Background(), rig.store, resolveTo(&fakeHindcast{}), rig.coast, seaNow)
	if !signalled(rig.store.comfort) {
		t.Error("new fetch rows change which spells can train, so the pass must ask for a fit")
	}
	runSeaPass(context.Background(), rig.store, resolveTo(&fakeHindcast{}), rig.coast, seaNow)
	if signalled(rig.store.comfort) {
		t.Error("a second pass with every row already held asked for a fit")
	}
}

func TestEditingAPassageAsksForAFit(t *testing.T) {
	store, rec, _ := seaPassage(t)
	if _, err := store.TrimPassage(rec.ID, rec.Start.Add(time.Minute), rec.End); err != nil {
		t.Fatal(err)
	}
	if !signalled(store.comfort) {
		t.Error("a trim changes the spells, so it must ask for a fit")
	}
	a, b, err := store.SplitPassage(rec.ID, rec.Start.Add(15*time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	if !signalled(store.comfort) {
		t.Error("a split must ask for a fit")
	}
	if _, err := store.MergePassages(a.ID, b.ID); err != nil {
		t.Fatal(err)
	}
	if !signalled(store.comfort) {
		t.Error("a merge must ask for a fit")
	}
	if _, err := store.PatchPassage(rec.ID, passagePatch{Rating: pstr(ratingComfortable)}); err != nil {
		t.Fatal(err)
	}
	if signalled(store.comfort) {
		t.Error("a rating feeds the bands and not the fit, so it must not ask for one")
	}
}

// runStartUp runs the start-up work until stop is called, with the first sweep
// pass's time asked for on firstPass. It returns when ctx ends.
func runStartUp(t *testing.T, store *passageStore, coast func() *offlineCoastlineReader, firstPass func()) (stop func()) {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	var once sync.Once
	go func() {
		defer close(done)
		startPassageFiguresWith(ctx, store, "no-such-settings.yaml", waveProviderResolver("no-such-settings.yaml"), coast, time.Hour, func() time.Time {
			once.Do(firstPass)
			return time.Now().UTC()
		})
	}()
	return func() {
		cancel()
		<-done
	}
}

func waitForFits(w *comfortModelWorker, n int32) bool {
	deadline := time.Now().Add(10 * time.Second)
	for w.fits.Load() < n && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
	return w.fits.Load() >= n
}

func TestAnImportAsksForAFitOnlyWhenItStoredSomething(t *testing.T) {
	store := newTestPassageStore(t)
	files := importTrip(simpleTrip(2 * time.Hour).pts)
	sum, err := store.ImportGPX(files)
	if err != nil {
		t.Fatal(err)
	}
	if sum.PassagesCreated == 0 || !signalled(store.comfort) {
		t.Errorf("an import that stored points and a passage must ask for a fit: %+v", sum)
	}
	if _, err := store.ImportGPX(files); err != nil {
		t.Fatal(err)
	}
	if signalled(store.comfort) {
		t.Error("a second import of the same file stored nothing and must not ask for a fit")
	}
}

func TestStartUpFitsTheComfortModelOnce(t *testing.T) {
	store := newTestPassageStore(t) // a first pass with nothing to store asks for no more
	stop := runStartUp(t, store, noCoastline, func() {})
	defer stop()
	if !waitForFits(store.comfort, 1) {
		t.Fatal("start-up never fitted")
	}
	time.Sleep(300 * time.Millisecond)
	if n := store.comfort.fits.Load(); n != 1 {
		t.Errorf("start-up fitted %d times, want once", n)
	}
	if store.comfort.Load() == nil {
		t.Error("start-up left no status")
	}
}

func TestTheFirstSeaPassDoesNotWaitForTheStartUpFit(t *testing.T) {
	store, _, _ := seaPassage(t)
	release := make(chan struct{})
	var waited atomic.Bool
	coast := func() *offlineCoastlineReader {
		select {
		case <-release:
		case <-time.After(2 * time.Second):
			waited.Store(true)
		}
		return nil
	}
	passStarted := make(chan struct{})
	stop := runStartUp(t, store, coast, func() { close(release); close(passStarted) })
	defer stop()
	select {
	case <-passStarted:
	case <-time.After(10 * time.Second):
		t.Fatal("the first sea pass never started")
	}
	if waited.Load() {
		t.Error("the first sea pass waited for the start-up fit to finish")
	}
	if !waitForFits(store.comfort, 1) {
		t.Error("the worker never made the start-up fit")
	}
}

func TestTheWorkerFitsWhenAskedAndNotBefore(t *testing.T) {
	store, _, _ := seaPassage(t)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		defer close(done)
		store.comfort.run(ctx, store, noCoastline)
	}()
	store.comfort.signal()
	store.comfort.signal() // two asks while one is pending are one fit
	deadline := time.Now().Add(10 * time.Second)
	for store.comfort.fits.Load() == 0 && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
	cancel()
	<-done
	if n := store.comfort.fits.Load(); n < 1 || n > 2 {
		t.Errorf("fits after two asks: %d", n)
	}

	idle, _, _ := seaPassage(t)
	ctx, cancel = context.WithCancel(context.Background())
	cancel()
	idle.comfort.run(ctx, idle, noCoastline)
	if n := idle.comfort.fits.Load(); n != 0 {
		t.Errorf("a worker nobody asked fitted %d times", n)
	}
}
