package main

import (
	"context"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"log"
	"math"
	"sort"
	"sync"
	"sync/atomic"
	"time"
)

// Fitting runs off the poll tick. The recorder stores a minute and wakes one
// worker; the worker fits the stay and publishes the result; requests read the
// published fit and predict from it. A request never fetches the chart, reads or
// writes the store, or waits on a lock the recorder holds. The worker wakes only
// when a minute is stored or the watch or its anchor changes, so a tide provider
// that is down is asked at most once a minute. Nothing calls a plugin (the chart,
// the station list) while holding the stay's lock: the lock guards memory only.

// tideCalFitEntry is a published fit, or the reason there is none, for one watch
// at one anchor position. Hours is the one source of the hours of readings the
// fit used.
type tideCalFitEntry struct {
	setAtMs  int64
	lat, lon float64
	fit      *tideCalFit
	err      *tideCalError
	hours    float64
	// What the tide chart said of its station, for the note when the tide here
	// does not follow it.
	stationName      string
	stationDistanceM *float64
	stationIsPoint   bool
}

// tideCalPublished is the latest fit. The worker is its only writer.
var tideCalPublished atomic.Pointer[tideCalFitEntry]

// tideCalPublishedEntry is the published fit if it is for this watch at its
// current anchor position, and nil otherwise. It loads the pointer once, so the
// entry it returns is the one it checked.
func tideCalPublishedEntry(watch anchorWatchData) *tideCalFitEntry {
	e := tideCalPublished.Load()
	if e != nil && e.setAtMs == watch.SetAt.UnixMilli() && e.lat == watch.Lat && e.lon == watch.Lon {
		return e
	}
	return nil
}

// tideCalPublishedFor says whether a fit is published for this watch and anchor.
func tideCalPublishedFor(watch anchorWatchData) bool { return tideCalPublishedEntry(watch) != nil }

var tideCalWake = make(chan struct{}, 1)

// tideCalSignal asks for a fit. Signals that arrive while one is pending or
// running collapse into one more fit.
func tideCalSignal() {
	select {
	case tideCalWake <- struct{}{}:
	default:
	}
}

var tideCalWorkFailing, soundedWorkFailing atomic.Bool

// startTideCalibrationWorker runs the single fitting goroutine until ctx ends.
// It starts with a signal of its own, so after a restart the first fit is made
// from the stored stay in the background.
func startTideCalibrationWorker(ctx context.Context) <-chan struct{} {
	done := make(chan struct{})
	tideCalSignal()
	go func() {
		defer close(done)
		for {
			select {
			case <-ctx.Done():
				return
			case <-tideCalWake:
				tideCalWorkOnce()
			}
		}
	}()
	return done
}

// tideCalWorkOnce fits the current watch against the current chart, logging a
// failure once and its recovery once.
func tideCalWorkOnce() {
	watch, active, _ := currentAnchorWatch()
	if !active || globalPassageStore == nil {
		return
	}
	// Which ground has been crossed needs no tide chart, so it is published first.
	if err := publishSoundedCells(watch, globalPassageStore); err != nil {
		if soundedWorkFailing.CompareAndSwap(false, true) {
			log.Printf("tide calibration: the sounded cells wait: %v", err)
		}
	} else if soundedWorkFailing.CompareAndSwap(true, false) {
		log.Printf("tide calibration: sounded cells publishing again")
	}
	chart, err := tideCalChartSource()
	if err == nil {
		_, err = tideCalFitNow(watch, globalPassageStore, chart)
	}
	if err != nil {
		if tideCalWorkFailing.CompareAndSwap(false, true) {
			log.Printf("tide calibration: the fit waits: %v", err)
		}
		return
	}
	if tideCalWorkFailing.CompareAndSwap(true, false) {
		log.Printf("tide calibration: fitting again")
	}
}

// tideCalStay holds the current stay in memory for the worker and the recorder:
// its readings, loaded once from the store and added to as each minute is stored,
// and what the stay knows of the chart last stored.
type tideCalStay struct {
	store      *passageStore
	setAtMs    int64
	readings   []tideCalReading
	storedHash string // content of the extremes last stored, with their source
	// stationFor is the chart content the station lookup was last tried for, and
	// station its answer. A lookup that came back empty is retried when the chart
	// changes, not before.
	stationFor string
	station    stationCoord
	// cells are the distinct cells of the readings in the frame of
	// cellLat, cellLon, kept as minutes arrive so a publish does not rescan
	// the readings. Nil until first built; rebuilt if the anchor moves.
	cells            map[tideCalCell]struct{}
	cellLat, cellLon float64
	// extremes are the stored highs and lows read back after the chart with
	// content hash extremesFor was filed, so a minute added to the stay does not
	// read the table again. warm is the last fit's weights, the start of the
	// next fit while the chart, the anchor and the stay are the same ones.
	extremes    []tideExtremePoint
	extremesFor string
	warm        *tideCalWarm
}

// ensureCells builds the cell set for this anchor position if the stay does not
// hold it. The caller holds tideCalStayMu.
func (st *tideCalStay) ensureCells(lat, lon float64) {
	if st.cells != nil && st.cellLat == lat && st.cellLon == lon {
		return
	}
	st.cells = map[tideCalCell]struct{}{}
	st.cellLat, st.cellLon = lat, lon
	for _, c := range soundedCellsOf(st.readings, lat, lon) {
		st.cells[c] = struct{}{}
	}
}

type stationCoord struct {
	lat, lon float64
	found    bool
}

var (
	tideCalStayMu  sync.Mutex
	tideCalStayCur *tideCalStay
)

// tideCalStayAppend adds a stored minute to the stay in memory, if that is the
// stay held.
func tideCalStayAppend(store *passageStore, setAt time.Time, r tideCalReading) {
	tideCalStayMu.Lock()
	defer tideCalStayMu.Unlock()
	st := tideCalStayCur
	if st == nil || st.store != store || st.setAtMs != setAt.UnixMilli() {
		// A stay being loaded will take the minute when it is installed.
		for _, l := range tideCalLoads {
			if l.store == store && l.setAtMs == setAt.UnixMilli() {
				l.pending = append(l.pending, r)
			}
		}
		return
	}
	// A minute already in the stay, such as one a reload from the store has just
	// read, is not added twice: the store's key is (set_at, t).
	if n := len(st.readings); n > 0 && !r.At.After(st.readings[n-1].At) {
		return
	}
	st.readings = append(st.readings, r)
	if st.cells != nil {
		st.cells[tideCalCellAt(st.cellLat, st.cellLon, r.Lat, r.Lon)] = struct{}{}
	}
}

// chartHash is the content of a chart's extremes and its source. A revised
// height or time changes it; the same predictions fetched again do not.
func chartHash(source string, extremes []tideExtremePoint) string {
	sorted := append([]tideExtremePoint(nil), extremes...)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i].Time.Before(sorted[j].Time) })
	h := sha256.New()
	h.Write([]byte(source))
	var buf [17]byte
	for _, e := range sorted {
		binary.BigEndian.PutUint64(buf[0:], uint64(e.Time.UnixMilli()))
		binary.BigEndian.PutUint64(buf[8:], math.Float64bits(e.HeightM))
		buf[16] = 0
		if e.High {
			buf[16] = 1
		}
		h.Write(buf[:])
	}
	return hex.EncodeToString(h.Sum(nil))
}

// tideCalFitNow brings the stay up to date, files the chart's highs and lows
// against it, fits the stay on the current source's extremes alone, and
// publishes the result. It is the worker's job; tests call it directly.
func tideCalFitNow(watch anchorWatchData, store *passageStore, chart tideCalChart) (*tideCalFitEntry, error) {
	setAtMs := watch.SetAt.UnixMilli()
	hash := chartHash(chart.Source, chart.Extremes)

	// Under the lock: the stay in memory, and what is already stored of this chart.
	st, err := tideCalStayLoaded(watch, store)
	if err != nil {
		return nil, err
	}
	tideCalStayMu.Lock()
	needStore, needStation := st.storedHash != hash, st.stationFor != hash
	tideCalStayMu.Unlock()

	// Outside it: the store, and the provider's station list.
	if needStore {
		if err := store.upsertTideCalExtremes(watch.SetAt, chart.Source, chart.Extremes); err != nil {
			return nil, err
		}
	}
	var found stationCoord
	if needStation {
		found = lookUpStation(chart)
	}

	// Under it again, to swap in what was learned and take the readings.
	tideCalStayMu.Lock()
	if tideCalStayCur == st {
		if needStore {
			st.storedHash = hash
		}
		if needStation {
			st.stationFor, st.station = hash, found
		}
	}
	readings, station := st.readings, st.station
	extremes, warm := st.extremes, st.warm
	haveExtremes := st.extremesFor == hash && !needStore
	tideCalStayMu.Unlock()

	// The stored extremes change only when a chart is filed, so while the chart
	// is the one already read they are not read again.
	if !haveExtremes {
		var err error
		if extremes, err = store.tideCalExtremes(watch.SetAt, chart.Source); err != nil {
			return nil, err
		}
		tideCalStayMu.Lock()
		if tideCalStayCur == st {
			st.extremes, st.extremesFor = extremes, hash
		}
		tideCalStayMu.Unlock()
	}
	e := &tideCalFitEntry{setAtMs: setAtMs, lat: watch.Lat, lon: watch.Lon, stationName: chart.Station.Name}
	if e.stationIsPoint = chart.PositionBased; e.stationIsPoint {
		// A position has no station: no name, no distance.
		e.stationName = ""
	} else if lat, lon := chart.Station.Lat, chart.Station.Lon; lat != 0 || lon != 0 {
		e.stationDistanceM = floatPtr(haversineMeters(watch.Lat, watch.Lon, lat, lon))
	} else if station.found {
		e.stationDistanceM = floatPtr(haversineMeters(watch.Lat, watch.Lon, station.lat, station.lon))
	}
	var ferr *tideCalError
	var nextWarm *tideCalWarm
	e.fit, ferr, nextWarm = fitTideCalibrationWarm(readings, watch.Lat, watch.Lon, newTideCurve(extremes), hash, warm)
	e.err = ferr
	tideCalStayMu.Lock()
	if tideCalStayCur == st {
		st.warm = nextWarm
	}
	tideCalStayMu.Unlock()
	switch {
	case e.fit != nil:
		e.hours = e.fit.Hours()
	case ferr != nil:
		e.hours = ferr.HoursUsed
	}
	tideCalPublished.Store(e)
	return e, nil
}

// tideCalLoad is a read of a stay from the store that is under way. Minutes the
// recorder stores meanwhile are kept here, as the stay they belong to is not yet
// held. Other callers wanting the same stay wait on done rather than read it again.
type tideCalLoad struct {
	store   *passageStore
	setAtMs int64
	pending []tideCalReading
	done    chan struct{}
}

// tideCalLoads are the reads under way, and tideCalInstalls counts the stays
// installed, so a load can tell another stay was installed while it read.
// Guarded by tideCalStayMu.
var (
	tideCalLoads    []*tideCalLoad
	tideCalInstalls int
)

// tideCalAfterRead is a test seam, called between the store read and the lock
// that installs the stay.
var tideCalAfterRead func()

// tideCalStayLoaded is the stay in memory for this watch, loaded from the store
// if it is not the one held. The store is read without the lock, which guards
// memory only, so the recorder's tick is never held up by it.
func tideCalStayLoaded(watch anchorWatchData, store *passageStore) (*tideCalStay, error) {
	setAtMs := watch.SetAt.UnixMilli()
	for {
		tideCalStayMu.Lock()
		if st := tideCalStayCur; st != nil && st.store == store && st.setAtMs == setAtMs {
			tideCalStayMu.Unlock()
			return st, nil
		}
		var running *tideCalLoad
		for _, l := range tideCalLoads {
			if l.store == store && l.setAtMs == setAtMs {
				running = l
			}
		}
		if running == nil {
			load := &tideCalLoad{store: store, setAtMs: setAtMs, done: make(chan struct{})}
			tideCalLoads = append(tideCalLoads, load)
			installs := tideCalInstalls
			tideCalStayMu.Unlock()
			return tideCalRunLoad(watch, store, load, installs)
		}
		tideCalStayMu.Unlock()
		<-running.done
		// The loop takes the stay it installed, or reads again if it failed.
	}
}

// tideCalRunLoad reads the stay and installs it. A stay installed meanwhile for
// another watch is left in place; the caller still gets this one to use.
func tideCalRunLoad(watch anchorWatchData, store *passageStore, load *tideCalLoad, installs int) (*tideCalStay, error) {
	var readings []tideCalReading
	var err error
	defer func() {
		tideCalStayMu.Lock()
		for i, l := range tideCalLoads {
			if l == load {
				tideCalLoads = append(tideCalLoads[:i], tideCalLoads[i+1:]...)
				break
			}
		}
		tideCalStayMu.Unlock()
		close(load.done)
	}()
	readings, err = store.tideCalReadings(watch.SetAt)
	if tideCalAfterRead != nil {
		tideCalAfterRead()
	}
	if err != nil {
		return nil, err
	}
	tideCalStayMu.Lock()
	defer tideCalStayMu.Unlock()
	// Minutes stored while the read ran may or may not be in it; add those
	// after its last, in order.
	for _, r := range load.pending {
		if n := len(readings); n == 0 || r.At.After(readings[n-1].At) {
			readings = append(readings, r)
		}
	}
	st := &tideCalStay{store: store, setAtMs: load.setAtMs, readings: readings}
	if tideCalInstalls == installs {
		tideCalStayCur = st
		tideCalInstalls++
	}
	return st, nil
}

// lookUpStation finds the coordinates of a station-based chart that came without
// them, in the provider's station list. A position has no list to look in. An
// empty list, which is what a failed search looks like, finds nothing and is
// tried again when the chart changes.
func lookUpStation(chart tideCalChart) stationCoord {
	if chart.Search == nil || chart.Station.Lat != 0 || chart.Station.Lon != 0 {
		return stationCoord{}
	}
	for _, s := range chart.Search() {
		if s.StationID == chart.Station.StationID {
			return stationCoord{lat: s.Lat, lon: s.Lon, found: true}
		}
	}
	return stationCoord{}
}

// tideCalUnavailableErr wraps a failure that is not the readings' fault.
func tideCalUnavailableErr(err error) *tideCalError {
	if te, ok := err.(*tideCalError); ok {
		return te
	}
	return tideCalErr(tideCalUnavailable, "%v", err)
}
