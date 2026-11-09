package main

import (
	"fmt"
	"log"
	"sync"
	"time"
)

// The readings the local tide calibration (ADR 0274) learns from: one row per
// minute while an anchor watch is set, keyed to the watch's set_at. They are
// kept after Raise and survive a restart; the fit is rebuilt from them when it
// is out of date. A new Drop has a new set_at and so starts fresh; an Adjust keeps
// set_at, so it keeps the readings.
//
// The position is the GPS antenna's, with no transducer offset. The depth is the
// median of the sounder's readings across the minute, taken at the poll tick.

// A minute is stored only if the poll tick sampled it at least half as often as
// the poll interval would give, so the figure is a median of several readings
// rather than a single one.
const tideCalMinSamples = int(tideCalRecordInterval/trackPollInterval) / 2

func createTideCalibrationSchema(db sqlExecer) error {
	for _, q := range []string{
		`CREATE TABLE IF NOT EXISTS tide_calibration_readings (
			set_at_ms INTEGER NOT NULL,
			t_ms INTEGER NOT NULL,
			lat REAL NOT NULL,
			lon REAL NOT NULL,
			depth_m REAL NOT NULL,
			PRIMARY KEY (set_at_ms, t_ms)
		) WITHOUT ROWID`,
		// The highs and lows as the charts delivered them during the stay, each
		// tagged with the source (provider and station) that predicted it, so the stay can be fitted on its own, long after the chart has
		// moved on.
		`CREATE TABLE IF NOT EXISTS tide_calibration_extremes (
			set_at_ms INTEGER NOT NULL,
			source TEXT NOT NULL,
			t_ms INTEGER NOT NULL,
			height_m REAL NOT NULL,
			high INTEGER NOT NULL,
			PRIMARY KEY (set_at_ms, source, t_ms)
		) WITHOUT ROWID`,
	} {
		if _, err := db.Exec(q); err != nil {
			return fmt.Errorf("create tide calibration schema: %w", err)
		}
	}
	return nil
}

func (s *passageStore) appendTideCalReading(setAt time.Time, r tideCalReading) error {
	_, err := s.db.Exec(`INSERT OR REPLACE INTO tide_calibration_readings (set_at_ms, t_ms, lat, lon, depth_m) VALUES (?,?,?,?,?)`,
		setAt.UnixMilli(), r.At.UnixMilli(), r.Lat, r.Lon, r.DepthM)
	if err != nil {
		return fmt.Errorf("append tide calibration reading: %w", err)
	}
	return nil
}

// tideCalReadings are the stored minutes of one watch, oldest first.
func (s *passageStore) tideCalReadings(setAt time.Time) ([]tideCalReading, error) {
	rows, err := s.db.Query(`SELECT t_ms, lat, lon, depth_m FROM tide_calibration_readings WHERE set_at_ms = ? ORDER BY t_ms`, setAt.UnixMilli())
	if err != nil {
		return nil, fmt.Errorf("read tide calibration readings: %w", err)
	}
	defer rows.Close()
	var out []tideCalReading
	for rows.Next() {
		var ms int64
		var r tideCalReading
		if err := rows.Scan(&ms, &r.Lat, &r.Lon, &r.DepthM); err != nil {
			return nil, fmt.Errorf("read tide calibration readings: %w", err)
		}
		r.At = time.UnixMilli(ms).UTC()
		out = append(out, r)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("read tide calibration readings: %w", err)
	}
	return out, nil
}

// upsertTideCalExtremes stores the highs and lows of a chart against the watch,
// tagged with the source that predicted them. A chart supersedes what the same
// source had stored for the span it covers, whatever differs, time or height, so
// a revised prediction replaces the old one rather than sitting beside it. The
// source's extremes before that span stay, and another source's are not touched.
func (s *passageStore) upsertTideCalExtremes(setAt time.Time, source string, extremes []tideExtremePoint) error {
	if len(extremes) == 0 {
		return nil
	}
	first, last := extremes[0].Time, extremes[0].Time
	for _, e := range extremes {
		if e.Time.Before(first) {
			first = e.Time
		}
		if e.Time.After(last) {
			last = e.Time
		}
	}
	tx, err := s.db.Begin()
	if err != nil {
		return fmt.Errorf("store tide calibration extremes: %w", err)
	}
	defer tx.Rollback()
	if _, err := tx.Exec(`DELETE FROM tide_calibration_extremes WHERE set_at_ms = ? AND source = ? AND t_ms BETWEEN ? AND ?`, setAt.UnixMilli(), source, first.UnixMilli(), last.UnixMilli()); err != nil {
		return fmt.Errorf("store tide calibration extremes: %w", err)
	}
	for _, e := range extremes {
		high := 0
		if e.High {
			high = 1
		}
		if _, err := tx.Exec(`INSERT OR REPLACE INTO tide_calibration_extremes (set_at_ms, source, t_ms, height_m, high) VALUES (?,?,?,?,?)`, setAt.UnixMilli(), source, e.Time.UnixMilli(), e.HeightM, high); err != nil {
			return fmt.Errorf("store tide calibration extremes: %w", err)
		}
	}
	return tx.Commit()
}

// tideCalExtremes are the stored highs and lows one source predicted during a
// watch, oldest first.
func (s *passageStore) tideCalExtremes(setAt time.Time, source string) ([]tideExtremePoint, error) {
	rows, err := s.db.Query(`SELECT t_ms, height_m, high FROM tide_calibration_extremes WHERE set_at_ms = ? AND source = ? ORDER BY t_ms`, setAt.UnixMilli(), source)
	if err != nil {
		return nil, fmt.Errorf("read tide calibration extremes: %w", err)
	}
	defer rows.Close()
	var out []tideExtremePoint
	for rows.Next() {
		var ms int64
		var high int
		var e tideExtremePoint
		if err := rows.Scan(&ms, &e.HeightM, &high); err != nil {
			return nil, fmt.Errorf("read tide calibration extremes: %w", err)
		}
		e.Time, e.High = time.UnixMilli(ms).UTC(), high == 1
		out = append(out, e)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("read tide calibration extremes: %w", err)
	}
	return out, nil
}

// tideCalMinute gathers the poll tick's samples of one minute.
type tideCalRecorder struct {
	mu                 sync.Mutex
	setAt              time.Time
	minute             time.Time
	depths, lats, lons []float64
	seen               tideCalWatchSeen
}

// observe takes one sample at now for the watch set at setAt. When the sample
// starts a new minute it returns the minute just finished as a reading, or false when that minute had too few samples. A new
// setAt discards what was gathered for the old watch.
func (r *tideCalRecorder) observe(now, setAt time.Time, depthM, lat, lon float64) (tideCalReading, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	var done tideCalReading
	finished := false
	minute := now.UTC().Truncate(time.Minute)
	if !setAt.Equal(r.setAt) {
		r.setAt = setAt
		r.depths, r.lats, r.lons = nil, nil, nil
	} else if !minute.Equal(r.minute) && len(r.depths) >= tideCalMinSamples {
		done = tideCalReading{At: r.minute.Add(30 * time.Second), Lat: median(r.lats), Lon: median(r.lons), DepthM: median(r.depths)}
		finished = true
	}
	if !minute.Equal(r.minute) {
		r.minute = minute
		r.depths, r.lats, r.lons = r.depths[:0], r.lats[:0], r.lons[:0]
	}
	r.depths = append(r.depths, depthM)
	r.lats = append(r.lats, lat)
	r.lons = append(r.lons, lon)
	return done, finished
}

// noteWatch records the watch and anchor the recorder last saw and says whether
// they have changed since.
func (r *tideCalRecorder) noteWatch(w anchorWatchData) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	seen := tideCalWatchSeen{setAtMs: w.SetAt.UnixMilli(), lat: w.Lat, lon: w.Lon, set: true}
	if r.seen == seen {
		return false
	}
	r.seen = seen
	return true
}

type tideCalWatchSeen struct {
	setAtMs  int64
	lat, lon float64
	set      bool
}

// reset forgets the minute in hand, when the watch is raised.
func (r *tideCalRecorder) reset() {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.setAt, r.minute = time.Time{}, time.Time{}
	r.seen = tideCalWatchSeen{}
	r.depths, r.lats, r.lons = nil, nil, nil
}

var globalTideCalRecorder tideCalRecorder

// tideCalChart is the tide chart the calibration uses, with who predicted it.
type tideCalChart struct {
	tideChartResult
	// Source names the prediction: the provider and the station id. A position
	// based provider's station id is the rounded position, so a boat that moves
	// to another position starts another source.
	Source string
	// PositionBased is the provider's own flag (ADR 0273): the tide comes from a
	// position, not a catalog station, so there is no station to name or measure
	// to. The point's id is tidePointID of the anchor while a watch is set, so it
	// is the same for as long as the anchor is where it is.
	PositionBased bool
	// Search lists the provider's stations, for a station-based chart that came
	// without coordinates. It is nil for a chart of a position, which has no
	// station list to look in.
	Search func() []tideStation
}

// tideCalChartSource is the configured provider's tide chart; a test stands in
// its own.
var tideCalChartSource = liveTideCalChart

// liveTideCalChart reads the tide chart of the configured provider and station,
// the one the Depth and Tide tile shows.
func liveTideCalChart() (tideCalChart, error) {
	settings, err := readSettings(getEnv("SETTINGS_FILE", "../settings.yaml"))
	if err != nil {
		return tideCalChart{}, fmt.Errorf("failed to read settings: %w", err)
	}
	uiMap, _ := settings["ui"].(map[string]any)
	provider, providerID, chart, terr := configuredTideChart(uiMap, tideBoatFixNow)
	if terr != nil {
		return tideCalChart{}, terr
	}
	out := tideCalChart{tideChartResult: chart, Source: providerID + "/" + chart.Station.StationID, PositionBased: tideProviderPositionBased(provider)}
	if !out.PositionBased {
		out.Search = func() []tideStation { return provider.SearchStations("", maxStationsForNearestLookup) }
	}
	return out, nil
}

// recordTideCalibrationSample is called on every poll tick. It gathers the depth
// and position while an anchor watch is set, stores a reading at the end of each
// minute and wakes the fitting worker for it. Nothing here waits on a fit or the
// tide chart.
func recordTideCalibrationSample(state vesselStateData, now time.Time) {
	watch, active, _ := currentAnchorWatch()
	if !active {
		globalTideCalRecorder.reset()
		return
	}
	// A new watch or a corrected anchor is asked for at once, once. The worker is
	// otherwise woken only when a minute is stored, so a provider that is down is
	// asked at most once a minute and never on every tick.
	if globalTideCalRecorder.noteWatch(watch) {
		tideCalSignal()
	}
	age, known := secondsAge(state.DepthLastUpdateAge)
	if !known || age > vesselStatusFreshAge || !finite(state.Depth) || state.Depth <= 0 || !hasUsableVesselPosition(state.Latitude, state.Longitude) {
		return
	}
	reading, done := globalTideCalRecorder.observe(now, watch.SetAt, state.Depth, state.Latitude, state.Longitude)
	if !done {
		return
	}
	if globalPassageStore == nil {
		log.Printf("tide calibration: track store unavailable, reading not recorded")
		return
	}
	if err := globalPassageStore.appendTideCalReading(watch.SetAt, reading); err != nil {
		log.Printf("tide calibration: %v", err)
		return
	}
	tideCalStayAppend(globalPassageStore, watch.SetAt, reading)
	tideCalSignal()
}
