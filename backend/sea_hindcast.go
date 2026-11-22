package main

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log"
	"math"
	"sort"
	"strings"
	"time"
)

// The sea state behind every recorded spell (ADR 0299). A spell is 15 minutes of
// motion; the sea that produced it is what a comfort model is fitted against, so
// it is fetched once for the spell's hour and position and kept, and a fit never
// depends on Open-Meteo's archive later. Rows are keyed by the hour and the
// position rounded to 0.01 degrees, not by passage, so splitting, merging or
// trimming a passage never orphans one.

const (
	// seaHindcastMinAge is how old an hour must be before its sea is fetched: the
	// model's values for an hour keep changing for about half a day as its
	// forecast-lead runs are replaced.
	seaHindcastMinAge = 12 * time.Hour
	// seaHindcastWindow is the longest span one plugin request asks for.
	seaHindcastWindow = 7 * 24 * time.Hour
	// seaHindcastListed is how many reasons the summary line gives.
	seaHindcastListed = 3
	// seaHindcastLocationBudget is the most positions one sweep asks Open-Meteo
	// about (each position in a request counts, as Open-Meteo counts them against
	// its per-minute limit). The first sweep after an upgrade has weeks of passages
	// to fill, and asking for all of it at once would use up the limit the forecast
	// shares from the same address. A backfill instead spreads over several hourly
	// sweeps, oldest passage first.
	seaHindcastLocationBudget = 300
)

// seaKey names one hour at one position: the hour as Unix milliseconds, the
// position in hundredths of a degree.
type seaKey struct {
	HourMs int64
	LatC   int
	LonC   int
}

// seaCell is a position in hundredths of a degree, the part of a seaKey the
// plugin is asked about.
type seaCell struct {
	LatC int
	LonC int
}

func (k seaKey) cell() seaCell { return seaCell{LatC: k.LatC, LonC: k.LonC} }

// point is the cell's centre, which is where the plugin is asked.
func (c seaCell) point() currentsPoint {
	return currentsPoint{Lat: float64(c.LatC) / 100, Lon: float64(c.LonC) / 100}
}

// centiDegrees rounds a coordinate to hundredths of a degree, the precision of
// passagePointKey.
func centiDegrees(v float64) int { return int(math.Round(v * 100)) }

// hasPosition says the spell was stored with the position nearest its middle.
func (sp feltSpell) hasPosition() bool { return sp.MidLat != nil && sp.MidLon != nil }

// seaKey is the hour and position the spell's sea is kept under: the UTC hour
// nearest its middle (the model's values are instantaneous at the hour, so 10:47
// is the 11:00 value), and the cell holding its middle position. ok is false for
// a spell with no position, which has no sea to look up and is never asked about.
func (sp feltSpell) seaKey() (k seaKey, ok bool) {
	if !sp.hasPosition() {
		return seaKey{}, false
	}
	mid := time.Unix(sp.Start+(sp.End-sp.Start)/2, 0).UTC().Round(time.Hour)
	return seaKey{HourMs: mid.UnixMilli(), LatC: centiDegrees(*sp.MidLat), LonC: centiDegrees(*sp.MidLon)}, true
}

// seaTrainFigures is one wave system: height, period and the direction it comes
// from, degrees true.
type seaTrainFigures struct {
	HeightM float64 `json:"height_m"`
	PeriodS float64 `json:"period_s"`
	FromDeg float64 `json:"from_deg"`
}

// seaHindcastRow is one stored hour. A nil system is one the model has none of;
// MissingReason is set instead when the model has no value for the hour at all.
type seaHindcastRow struct {
	seaKey
	Provider      string
	Model         string
	FetchedMs     int64
	Swell         *seaTrainFigures
	WindWave      *seaTrainFigures
	MissingReason string
	// WaveHeightM is the combined wave height the model gave for the hour, nil when
	// it gave none or the row was stored before the figure was kept.
	WaveHeightM *float64
	// WaveHeightSeen says the row was stored with the figure kept, so a nil
	// WaveHeightM is the plugin's own answer and not a row from before.
	WaveHeightSeen bool
}

// noSplit says the hour has neither a swell nor a wind wave, yet is not known to
// be calm: its combined height is 0.1 m or more, or unknown. A system with no
// height is no system. The page and the comfort fit both read such an hour as one
// the plugin gave no split for, never as a flat sea, and the forecast side holds
// to the same line (comfortGap).
func (r seaHindcastRow) noSplit() bool {
	return seaHasNoSplit(r.Swell, r.WindWave, r.WaveHeightM)
}

func createSeaHindcastSchema(db *sql.DB) error {
	if _, err := db.Exec(`CREATE TABLE IF NOT EXISTS sea_hindcast_hours (
		hour_ms INTEGER NOT NULL,
		lat_c INTEGER NOT NULL,
		lon_c INTEGER NOT NULL,
		provider TEXT NOT NULL,
		model TEXT NOT NULL,
		fetched_ms INTEGER NOT NULL,
		swell_height_m REAL,
		swell_period_s REAL,
		swell_dir_deg REAL,
		wind_wave_height_m REAL,
		wind_wave_period_s REAL,
		wind_wave_dir_deg REAL,
		missing_reason TEXT NOT NULL DEFAULT '',
		PRIMARY KEY (hour_ms, lat_c, lon_c)
	)`); err != nil {
		return fmt.Errorf("create sea hindcast schema: %w", err)
	}
	// The combined wave height the model gave for the hour, kept so an hour with
	// neither system can be told from a calm one. wave_height_seen is 1 once the
	// plugin has been asked for the hour with the figure kept, so a plugin that
	// never gives it is not asked again every sweep.
	for _, c := range [][2]string{{"wave_height_m", "REAL"}, {"wave_height_seen", "INTEGER NOT NULL DEFAULT 0"}} {
		if err := ensureColumn(db, "sea_hindcast_hours", c[0], c[1]); err != nil {
			return err
		}
	}
	return nil
}

// trainColumns is a wave system as the three columns it is kept in: all NULL for
// a system the model has none of.
func trainColumns(t *seaTrainFigures) (h, p, d any) {
	if t == nil {
		return nil, nil, nil
	}
	return t.HeightM, t.PeriodS, t.FromDeg
}

// trainFromColumns is trainColumns the other way round.
func trainFromColumns(h, p, d sql.NullFloat64) *seaTrainFigures {
	if !h.Valid || !p.Valid || !d.Valid {
		return nil
	}
	return &seaTrainFigures{HeightM: h.Float64, PeriodS: p.Float64, FromDeg: d.Float64}
}

// StoreSeaHindcastRows keeps the rows in one transaction. An hour already held is
// left as it is, except one stored before the combined wave height was kept: it
// takes the combined height from the row fetched now, once, when that row is good
// and from the same model, and is marked as asked either way. Nothing else of the
// stored row changes (every row stored here has seen the figure asked for).
func (s *passageStore) StoreSeaHindcastRows(rows []seaHindcastRow) error {
	if len(rows) == 0 {
		return nil
	}
	tx, err := s.db.Begin()
	if err != nil {
		return fmt.Errorf("store sea hindcast: %w", err)
	}
	defer tx.Rollback()
	for _, r := range rows {
		sh, sp, sd := trainColumns(r.Swell)
		wh, wp, wd := trainColumns(r.WindWave)
		var combined any
		if r.WaveHeightM != nil {
			combined = *r.WaveHeightM
		}
		if _, err := tx.Exec(`INSERT INTO sea_hindcast_hours
			(hour_ms, lat_c, lon_c, provider, model, fetched_ms, swell_height_m, swell_period_s, swell_dir_deg,
			 wind_wave_height_m, wind_wave_period_s, wind_wave_dir_deg, missing_reason, wave_height_m, wave_height_seen)
			VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,1)
			ON CONFLICT (hour_ms, lat_c, lon_c) DO UPDATE SET
			 wave_height_m = CASE WHEN excluded.missing_reason = '' AND excluded.model = sea_hindcast_hours.model
			                      THEN excluded.wave_height_m ELSE sea_hindcast_hours.wave_height_m END,
			 wave_height_seen = 1
			WHERE sea_hindcast_hours.wave_height_seen = 0 AND sea_hindcast_hours.missing_reason = ''`,
			r.HourMs, r.LatC, r.LonC, r.Provider, r.Model, r.FetchedMs, sh, sp, sd, wh, wp, wd, r.MissingReason, combined); err != nil {
			return fmt.Errorf("store sea hindcast: %w", err)
		}
	}
	return tx.Commit()
}

// SeaHindcastRowsBetween reads, in one query, every stored row for the hours
// first to last inclusive (Unix milliseconds), at any position. The caller picks
// the cells it wants out of the answer.
func (s *passageStore) SeaHindcastRowsBetween(first, last int64) (map[seaKey]seaHindcastRow, error) {
	rows, err := s.db.Query(`SELECT hour_ms, lat_c, lon_c, provider, model, fetched_ms, swell_height_m, swell_period_s, swell_dir_deg,
		wind_wave_height_m, wind_wave_period_s, wind_wave_dir_deg, missing_reason, wave_height_m, wave_height_seen
		FROM sea_hindcast_hours WHERE hour_ms BETWEEN ? AND ?`, first, last)
	if err != nil {
		return nil, fmt.Errorf("read sea hindcast: %w", err)
	}
	defer rows.Close()
	out := map[seaKey]seaHindcastRow{}
	for rows.Next() {
		var r seaHindcastRow
		var sh, sp, sd, wh, wp, wd, combined sql.NullFloat64
		if err := rows.Scan(&r.HourMs, &r.LatC, &r.LonC, &r.Provider, &r.Model, &r.FetchedMs, &sh, &sp, &sd, &wh, &wp, &wd, &r.MissingReason, &combined, &r.WaveHeightSeen); err != nil {
			return nil, fmt.Errorf("read sea hindcast: %w", err)
		}
		r.Swell, r.WindWave = trainFromColumns(sh, sp, sd), trainFromColumns(wh, wp, wd)
		if combined.Valid {
			c := combined.Float64
			r.WaveHeightM = &c
		}
		out[r.seaKey] = r
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("read sea hindcast: %w", err)
	}
	return out, nil
}

// hourRange is the first and last hour of keys, and false when there are none.
func hourRange(keys map[seaKey]bool) (first, last int64, ok bool) {
	for k := range keys {
		if !ok || k.HourMs < first {
			first = k.HourMs
		}
		if !ok || k.HourMs > last {
			last = k.HourMs
		}
		ok = true
	}
	return first, last, ok
}

// sortSeaKeys puts keys in hour order, then by cell.
func sortSeaKeys(keys []seaKey) {
	sort.Slice(keys, func(i, j int) bool {
		a, b := keys[i], keys[j]
		if a.HourMs != b.HourMs {
			return a.HourMs < b.HourMs
		}
		if a.LatC != b.LatC {
			return a.LatC < b.LatC
		}
		return a.LonC < b.LonC
	})
}

// seaHoursNeeded is, for each passage (hidden ones too) with any, the hours with
// no row yet, at least seaHindcastMinAge old at now, that its spells lie in, in
// hour order; the passages are in order of their first hour. An hour two passages
// share is given to the first only. A passage's figures are read through
// PassageFiguresFor, which works them out when they are missing or old, so no
// passage is skipped for being stale. For each passage one query reads the stored
// rows over its hour range. A spell with no recorded position is left out, since
// there is nowhere to ask about. A passage whose figures cannot be read is logged
// and its error returned in failed, and the rest carry on.
func (s *passageStore) seaHoursNeeded(now time.Time) (needed [][]seaKey, failed []string, err error) {
	records, err := s.allPassageRecords()
	if err != nil {
		return nil, nil, err
	}
	cutoff := now.Add(-seaHindcastMinAge).UnixMilli()
	claimed := map[seaKey]bool{}
	for _, rec := range records {
		f, err := s.PassageFiguresFor(rec)
		if err != nil {
			log.Printf("passages: sea hindcast: passage %s left out: %v", rec.ID, err)
			failed = append(failed, err.Error())
			continue
		}
		keys := map[seaKey]bool{}
		for _, sp := range f.Stats.FeltSpells {
			if k, ok := sp.seaKey(); ok && k.HourMs <= cutoff {
				keys[k] = true
			}
		}
		first, last, ok := hourRange(keys)
		if !ok {
			continue
		}
		held, err := s.SeaHindcastRowsBetween(first, last)
		if err != nil {
			return nil, nil, err
		}
		var mine []seaKey
		for k := range keys {
			// An hour stored before the combined height was kept is asked for again,
			// once: without it the page cannot tell a calm hour from one with no split.
			row, have := held[k]
			if (!have || (!row.WaveHeightSeen && row.MissingReason == "")) && !claimed[k] {
				mine = append(mine, k)
			}
		}
		if len(mine) == 0 {
			continue
		}
		for _, k := range mine {
			claimed[k] = true
		}
		sortSeaKeys(mine)
		needed = append(needed, mine)
	}
	sort.SliceStable(needed, func(i, j int) bool { return needed[i][0].HourMs < needed[j][0].HourMs })
	return needed, failed, nil
}

// seaHindcastSummary is what one run did.
type seaHindcastSummary struct {
	// Filled is the hours stored with a sea state, NoValue the hours stored with
	// a reason, Failed the hours asked for and not stored.
	Filled, NoValue, Failed int
	// PassagesFailed is the passages whose spells could not be read, and so were
	// left out of the run.
	PassagesFailed int
	// Skipped is why the run did nothing, when it did nothing.
	Skipped string
}

// ── planning ─────────────────────────────────────────────────────────────

// seaWindow is one plugin request: the span of hours it asks for, and the keys
// (hour and cell) wanted from its answer.
type seaWindow struct {
	Start, End time.Time
	Keys       []seaKey
}

// planSeaWindows cuts each passage's wanted keys, in hour order, into windows of
// under seaHindcastWindow. Passages are planned apart: a window covers one
// passage's cells over that passage's own hours, so a refusal for the oldest
// passage settles only its hours, and no cell is asked for an hour of another
// passage's. A window covers every cell wanted in it, so one request serves them.
func planSeaWindows(passages [][]seaKey) []seaWindow {
	var out []seaWindow
	for _, keys := range passages {
		for i := 0; i < len(keys); {
			end := i
			for end+1 < len(keys) && time.Duration(keys[end+1].HourMs-keys[i].HourMs)*time.Millisecond < seaHindcastWindow {
				end++
			}
			out = append(out, seaWindow{
				Start: time.UnixMilli(keys[i].HourMs).UTC(),
				End:   time.UnixMilli(keys[end].HourMs).UTC(),
				Keys:  keys[i : end+1],
			})
			i = end + 1
		}
	}
	return out
}

// cells is the window's distinct cells, in order, and the keys wanted from each.
func (w seaWindow) cells() ([]seaCell, map[seaCell][]seaKey) {
	byCell := map[seaCell][]seaKey{}
	var cells []seaCell
	for _, k := range w.Keys {
		c := k.cell()
		if _, seen := byCell[c]; !seen {
			cells = append(cells, c)
		}
		byCell[c] = append(byCell[c], k)
	}
	sort.Slice(cells, func(i, j int) bool {
		if cells[i].LatC != cells[j].LatC {
			return cells[i].LatC < cells[j].LatC
		}
		return cells[i].LonC < cells[j].LonC
	})
	return cells, byCell
}

// ── fetching ─────────────────────────────────────────────────────────────

// seaOutcome is what became of one wanted hour: the row to keep, or why there is
// none. The counts and the log line are worked out from outcomes alone.
type seaOutcome struct {
	Key     seaKey
	Row     *seaHindcastRow
	Failure string
}

// seaRowFor turns one hour the plugin answered into the row to keep. The model
// and the reason are the plugin's, kept as given: an hour it has no value for
// becomes a row with its reason, and nothing is guessed. A system with no period
// is no system.
func seaRowFor(k seaKey, providerID string, series waveHindcastSeries, fetched time.Time, h waveHindcastHour) seaHindcastRow {
	row := seaHindcastRow{seaKey: k, Provider: providerID, Model: series.Model, FetchedMs: fetched.UnixMilli(), MissingReason: h.MissingReason}
	if row.MissingReason != "" {
		return row
	}
	row.Swell, row.WindWave = h.systems()
	row.WaveHeightM = h.WaveHeightM
	return row
}

// systems are the hour's two wave systems, nil for one with no period: a system
// with no period is no system. The stored hindcast and the forecast hour both
// read an hour through here, so the sea the model learns from and the sea it
// predicts for are cut the same way.
func (p waveHourPoint) systems() (swell, wind *seaTrainFigures) {
	if p.SwellWavePeriodS > 0 {
		swell = &seaTrainFigures{HeightM: p.SwellWaveHeightM, PeriodS: p.SwellWavePeriodS, FromDeg: p.SwellWaveDirectionDeg}
	}
	if p.WindWavePeriodS > 0 {
		wind = &seaTrainFigures{HeightM: p.WindWaveHeightM, PeriodS: p.WindWavePeriodS, FromDeg: p.WindWaveDirectionDeg}
	}
	return swell, wind
}

func failedOutcomes(keys []seaKey, why string) []seaOutcome {
	out := make([]seaOutcome, len(keys))
	for i, k := range keys {
		out[i] = seaOutcome{Key: k, Failure: why}
	}
	return out
}

// fetchSeaWindow asks the plugin for the window's cells, in one call (or one for
// each maxHindcastPoints cells), and matches every wanted hour to its own cell's
// series. A failed call fails its cells' hours; an hour the answer leaves out
// fails alone. stop says why the sweep must end before every cell was asked: ctx
// ended, Open-Meteo rate limited a request, or budget (the positions the sweep
// may still ask about, spent as cells are asked) ran out.
func fetchSeaWindow(ctx context.Context, hp waveHindcastProvider, providerID string, w seaWindow, fetched time.Time, budget *int) (outcomes []seaOutcome, stop seaStop) {
	cells, byCell := w.cells()
	for from := 0; from < len(cells); from += maxHindcastPoints {
		if ctx.Err() != nil {
			return outcomes, seaStopped
		}
		chunk := cells[from:min(from+maxHindcastPoints, len(cells))]
		if len(chunk) > *budget {
			return outcomes, seaStopBudget
		}
		*budget -= len(chunk)
		places := make([]currentsPoint, len(chunk))
		var chunkKeys []seaKey
		for i, c := range chunk {
			places[i] = c.point()
			chunkKeys = append(chunkKeys, byCell[c]...)
		}
		// The provider's boundary (mapWasmWaveHindcastOutput) has already checked
		// that it answered every position asked about.
		bundle, err := hp.FetchWaveHindcast(ctx, places, w.Start, w.End)
		if err != nil {
			outcomes = append(outcomes, failedOutcomes(chunkKeys, fmt.Sprintf("%d positions from %.2f, %.2f: %v", len(places), places[0].Lat, places[0].Lon, err))...)
			if ctx.Err() != nil {
				return outcomes, seaStopped
			}
			if errors.Is(err, errWaveHindcastRateLimited) {
				return outcomes, seaStopRateLimited
			}
			continue
		}
		if bundle.Refused != "" {
			// The data source's definite no: every hour asked for is settled with
			// its sentence and not asked again.
			for _, k := range chunkKeys {
				row := seaHindcastRow{seaKey: k, Provider: providerID, FetchedMs: fetched.UnixMilli(), MissingReason: bundle.Refused}
				outcomes = append(outcomes, seaOutcome{Key: k, Row: &row})
			}
			continue
		}
		for i, c := range chunk {
			series := bundle.Points[i]
			answered := make(map[int64]waveHindcastHour, len(series.Hourly))
			for _, h := range series.Hourly {
				answered[h.Time.UTC().Truncate(time.Hour).UnixMilli()] = h
			}
			for _, k := range byCell[c] {
				h, ok := answered[k.HourMs]
				if !ok {
					// The plugin answered the position and left this hour out: its
					// definite word that it has no figure, so the hour is settled with
					// a reason and never filled in from a neighbour.
					row := seaHindcastRow{seaKey: k, Provider: providerID, Model: series.Model, FetchedMs: fetched.UnixMilli(), MissingReason: seaReasonNoFigure}
					outcomes = append(outcomes, seaOutcome{Key: k, Row: &row})
					continue
				}
				row := seaRowFor(k, providerID, series, fetched, h)
				outcomes = append(outcomes, seaOutcome{Key: k, Row: &row})
			}
		}
	}
	return outcomes, seaRanOn
}

// seaStop is why a sweep ended before it had asked for everything.
type seaStop string

const (
	seaRanOn           seaStop = ""
	seaStopped         seaStop = "stopped early"
	seaStopRateLimited seaStop = "Open-Meteo rate limited a request, so it stopped; the next sweep carries on"
	seaStopBudget      seaStop = "reached its budget of locations; the next sweep carries on"
)

// rowsOf is the rows the outcomes say to keep.
func rowsOf(outcomes []seaOutcome) []seaHindcastRow {
	var rows []seaHindcastRow
	for _, o := range outcomes {
		if o.Row != nil {
			rows = append(rows, *o.Row)
		}
	}
	return rows
}

// failStored turns every kept row into a failure with err's reason, for a store
// that did not keep them.
func failStored(outcomes []seaOutcome, err error) {
	for i := range outcomes {
		if outcomes[i].Row != nil {
			outcomes[i].Row, outcomes[i].Failure = nil, err.Error()
		}
	}
}

// ── reporting ────────────────────────────────────────────────────────────

// seaReport counts outcomes and keeps the first few distinct reasons and errors
// for the one-line summary.
type seaReport struct {
	sum                             seaHindcastSummary
	noValue, failures, passageFails []string
	stopped                         seaStop
}

// passagesFailed counts the passages left out because their figures could not be
// read, and keeps the first few errors.
func (r *seaReport) passagesFailed(errs []string) {
	r.sum.PassagesFailed += len(errs)
	for _, e := range errs {
		noteFirst(&r.passageFails, e)
	}
}

func noteFirst(list *[]string, msg string) {
	for _, m := range *list {
		if m == msg {
			return
		}
	}
	if len(*list) < seaHindcastListed {
		*list = append(*list, msg)
	}
}

func (r *seaReport) add(o seaOutcome) {
	switch {
	case o.Row == nil:
		r.sum.Failed++
		noteFirst(&r.failures, o.Failure)
	case o.Row.MissingReason != "":
		r.sum.NoValue++
		noteFirst(&r.noValue, o.Row.MissingReason)
	default:
		r.sum.Filled++
	}
}

func (r *seaReport) log() {
	line := fmt.Sprintf("passages: sea hindcast: filled %d hours, %d with no value", r.sum.Filled, r.sum.NoValue)
	if len(r.noValue) > 0 {
		line += " (" + strings.Join(r.noValue, "; ") + ")"
	}
	line += fmt.Sprintf(", %d failed", r.sum.Failed)
	if len(r.failures) > 0 {
		line += " (" + strings.Join(r.failures, "; ") + "); they are tried again at the next hourly sweep"
	}
	if r.sum.PassagesFailed > 0 {
		noun := "passages"
		if r.sum.PassagesFailed == 1 {
			noun = "passage"
		}
		line += fmt.Sprintf(", %d %s left out (%s)", r.sum.PassagesFailed, noun, strings.Join(r.passageFails, "; "))
	}
	if r.stopped != seaRanOn {
		line += ", " + string(r.stopped)
	}
	log.Print(line)
}

// skipSeaHindcast logs why a run did nothing and says so in its summary.
func skipSeaHindcast(err error) seaHindcastSummary {
	log.Printf("passages: sea hindcast skipped: %v; nothing stored, it tries again at the next hourly sweep", err)
	return seaHindcastSummary{Skipped: err.Error()}
}

// seaReasonCantFetch starts the reason the page gives while the sweep cannot ask
// for a sea at all.
const seaReasonCantFetch = "The sea state can't be fetched: "

// recordSkip logs the skip as skipSeaHindcast does, and keeps err as the reason
// the page gives for spells with no sea until a sweep can ask again.
func (s *passageStore) recordSkip(err error) seaHindcastSummary {
	s.setSeaProblem(seaReasonCantFetch + strings.TrimSuffix(err.Error(), ".") + ".")
	return skipSeaHindcast(err)
}

// setSeaProblem keeps the reason the last sweep could not ask, or "" when it
// could. It lives in memory: the next sweep, at start-up and then hourly, works
// it out afresh.
func (s *passageStore) setSeaProblem(reason string) {
	s.seaMu.Lock()
	changed := s.seaProblem != reason
	s.seaProblem = reason
	s.seaMu.Unlock()
	// The comfort fit says when new sea hours wait for the plugin, so a change asks for one.
	if changed && s.comfort != nil {
		s.comfort.signal()
	}
}

// seaProblemNow is the reason the last sweep could not ask, or "".
func (s *passageStore) seaProblemNow() string {
	s.seaMu.Lock()
	defer s.seaMu.Unlock()
	return s.seaProblem
}

// runSeaHindcast fills the sea state for every spell hour that has none yet.
// resolve names the wave plugin to ask. It never guesses and never falls back:
//
//   - no wave plugin, a plugin without the hindcast export, or a request that
//     fails stores nothing for what it covers, logs why, and leaves the hours for
//     the next pass (runSeaSweeps makes one every hour);
//   - an hour the model has no value for, an hour the plugin's answer leaves out,
//     and a request the data source definitely refused (the plugin says so) are
//     stored with a reason and not asked again; a network error, a timeout or a
//     server error is not definite, so it stores nothing;
//   - a passage whose figures cannot be read is logged and counted, and the
//     others carry on.
//
// One request covers every cell of a window of up to seven days. It honours ctx
// and logs one summary line when it did something or failed, and nothing when
// there was nothing to do. Only runSeaSweeps calls it, so two never overlap.
//
// Each passage is asked apart, over its own hours (planSeaWindows). A sweep stops
// at the first rate-limited request, and after asking about
// seaHindcastLocationBudget positions; the next sweep carries on. When it cannot
// ask at all (no wave plugin, or one without the export) the sweep keeps that as
// the reason the page gives for every spell it has no sea for, until a sweep
// that can ask clears it.
func runSeaHindcast(ctx context.Context, store *passageStore, resolve func() (waveProvider, string, error), now time.Time) seaHindcastSummary {
	return runSeaHindcastWithin(ctx, store, resolve, now, seaHindcastLocationBudget)
}

// runSeaHindcastWithin is runSeaHindcast with the sweep's budget of positions
// given, so a test can use a small one.
func runSeaHindcastWithin(ctx context.Context, store *passageStore, resolve func() (waveProvider, string, error), now time.Time, budget int) seaHindcastSummary {
	needed, passageErrs, err := store.seaHoursNeeded(now)
	if err != nil {
		return skipSeaHindcast(err)
	}
	var rep seaReport
	rep.passagesFailed(passageErrs)
	if len(needed) == 0 {
		if rep.sum.PassagesFailed > 0 {
			rep.log()
		}
		return rep.sum
	}
	provider, providerID, err := resolve()
	if err != nil {
		return store.recordSkip(fmt.Errorf("no wave provider could be used (%w)", err))
	}
	hp, ok := provider.(waveHindcastProvider)
	if !ok || !hp.SupportsWaveHindcast() {
		return store.recordSkip(noWaveHindcastHint(providerID))
	}
	store.setSeaProblem("")

	for _, w := range planSeaWindows(needed) {
		if ctx.Err() != nil {
			rep.stopped = seaStopped
			break
		}
		outcomes, stop := fetchSeaWindow(ctx, hp, providerID, w, now, &budget)
		if err := store.StoreSeaHindcastRows(rowsOf(outcomes)); err != nil {
			failStored(outcomes, err)
		}
		for _, o := range outcomes {
			rep.add(o)
		}
		if stop != seaRanOn {
			rep.stopped = stop
			break
		}
	}
	rep.log()
	return rep.sum
}

// waveProviderResolver is how runSeaHindcast finds the operator's wave plugin.
func waveProviderResolver(settingsPath string) func() (waveProvider, string, error) {
	return func() (waveProvider, string, error) { return resolveWaveProvider(settingsPath) }
}

// seaSweepEvery is how often the sweep looks for spell hours with no sea yet.
const seaSweepEvery = time.Hour

// runSeaSweeps is the one loop that keeps the sea filled (ADR 0299) and what the
// shore says of it worked out (ADR 0300): a pass now, then one every interval,
// until ctx ends. Each pass fills the hindcast, then the fetch figures of every
// spell that has a sea state. It takes the time afresh from now and picks up
// whatever passages, edits, imports and coastline squares have come since the
// last, so nothing else needs to ask for a fill. A pass that finds nothing to do
// logs nothing. A pass that stored something asks the comfort model worker for a
// fit (ADR 0301); the fit itself does not run on the sweep.
func runSeaSweeps(ctx context.Context, store *passageStore, resolve func() (waveProvider, string, error), coast func() *offlineCoastlineReader, every time.Duration, now func() time.Time) {
	for ctx.Err() == nil {
		runSeaPass(ctx, store, resolve, coast, now().UTC())
		timer := time.NewTimer(every)
		select {
		case <-ctx.Done():
			timer.Stop()
			return
		case <-timer.C:
		}
	}
}

// ── what the logbook shows ───────────────────────────────────────────────

// seaTrain is one wave system as the page shows it. Direction is in words
// relative to the boat's heading.
type seaTrain struct {
	seaTrainFigures
	Direction string `json:"direction"`
	// Fetch is the fetch check on a wind wave (ADR 0300). Nil on a swell, and when
	// no check was made.
	Fetch *fetchCheck `json:"fetch,omitempty"`
	// Enclosed says a swell was not judged: the shore closes the quarter it comes
	// from, so its figures say nothing about this sea.
	Enclosed bool `json:"enclosed,omitempty"`
	// Unchecked says a swell kept the model's figure because the shore could not be
	// read to judge it for enclosed water; Note says why.
	Unchecked bool `json:"unchecked,omitempty"`
	// Note is what the check made of the train, in the words the page shows after
	// its figures (wind wave) or in their place (enclosed swell). Empty when no
	// check was made.
	Note string `json:"note,omitempty"`
}

// seaVerdict says whether a wave system was judged against the shore. The page's
// peaks and the comfort model both set aside what was not, and read it here, so
// the two cannot disagree on what counts as judged.
type seaVerdict int

const (
	seaJudged seaVerdict = iota
	seaSwellEnclosed
	seaSwellUnchecked
	seaWindUnchecked
)

// swellVerdict is the verdict on a swell. A swell not checked for enclosed water
// comes before one found enclosed. Nil is judged: there is no swell to set aside.
func (t *seaTrain) swellVerdict() seaVerdict {
	switch {
	case t == nil:
	case t.Unchecked:
		return seaSwellUnchecked
	case t.Enclosed:
		return seaSwellEnclosed
	}
	return seaJudged
}

// windVerdict is the verdict on a wind wave.
func (t *seaTrain) windVerdict() seaVerdict {
	if t != nil && t.Fetch != nil && t.Fetch.Check == seaCheckUnchecked {
		return seaWindUnchecked
	}
	return seaJudged
}

// spellSea is the sea behind one spell. With no sea state Reason says why. A
// sea state with neither swell nor wind wave is a flat sea.
type spellSea struct {
	Swell    *seaTrain `json:"swell"`
	WindWave *seaTrain `json:"wind_wave"`
	Reason   string    `json:"reason,omitempty"`
}

const (
	seaReasonNotFetched = "The sea state for this hour hasn't been fetched yet."
	seaReasonUnreadable = "The recorded sea state couldn't be read."
	seaReasonNoPosition = "This spell has no recorded position to look up the sea for."
	// seaReasonNoFigure is stored for an hour the wave plugin's answer leaves out.
	seaReasonNoFigure = "The wave plugin gave no figure for this hour."
	// seaReasonNoSplit is the reason for an hour whose stored sea has neither a swell
	// nor a wind wave but a combined height that is not a calm one, or none.
	seaReasonNoSplit = "The wave plugin gave no swell and wind-wave split for this hour."
)

// seaWords says where a sea comes from, in words, relative to the heading:
// fromDeg is the direction it comes from, degrees true. With no heading it says
// so and gives the true direction, which is all that is known.
//
// The bands are relativeAngleLabel's, the scale Mate uses, so the two describe
// one sea alike. Head and following span both sides and take no side; bow, beam
// and quarter are given the side the sea comes from.
func seaWords(headingDeg *float64, fromDeg float64) string {
	if headingDeg == nil {
		return fmt.Sprintf("from %.0f° true, no heading recorded", normalizeDegrees(fromDeg))
	}
	label := relativeAngleLabel(relativeAngleDeg(*headingDeg, fromDeg))
	switch label {
	case "head":
		return "ahead"
	case "following":
		return "astern"
	}
	side := "starboard"
	if relativeBearingDeg(*headingDeg, fromDeg) > 180 {
		side = "port"
	}
	return "on the " + side + " " + label
}

// endSentence ends a reason with a full stop when it has none, so every reason
// the page shows reads alike whoever worded it.
func endSentence(s string) string {
	s = strings.TrimSpace(s)
	if s == "" || strings.HasSuffix(s, ".") || strings.HasSuffix(s, "!") || strings.HasSuffix(s, "?") {
		return s
	}
	return s + "."
}

func seaTrainOf(f *seaTrainFigures, heading *float64) *seaTrain {
	if f == nil {
		return nil
	}
	return &seaTrain{seaTrainFigures: *f, Direction: seaWords(heading, f.FromDeg)}
}

// seaForSpell shapes the stored hour for the page. row is nil when none is
// stored; readErr is a failure to read it.
func seaForSpell(sp feltSpell, row *seaHindcastRow, readErr error) spellSea {
	switch {
	case !sp.hasPosition():
		return spellSea{Reason: seaReasonNoPosition}
	case readErr != nil:
		return spellSea{Reason: seaReasonUnreadable}
	case row == nil:
		return spellSea{Reason: seaReasonNotFetched}
	case row.MissingReason != "":
		return spellSea{Reason: endSentence(row.MissingReason)}
	case row.noSplit():
		return spellSea{Reason: seaReasonNoSplit}
	}
	return spellSea{Swell: seaTrainOf(row.Swell, sp.MeanHeadingDeg), WindWave: seaTrainOf(row.WindWave, sp.MeanHeadingDeg)}
}

// seaLookup is the stored sea for a passage's spells, read once. A failed read is
// kept, and every spell then carries it as its reason.
//
// problem is why the last sweep could not ask for a sea at all, when it could
// not: a spell with no stored hour says that instead of "hasn't been fetched
// yet".
type seaLookup struct {
	rows    map[seaKey]seaHindcastRow
	err     error
	problem string
}

// seaLookupFor reads, in one query over the hours the spells lie in, the sea for
// them. It is nil for a passage with no spells. A failed read is logged.
func (s *passageStore) seaLookupFor(spells []feltSpell) *seaLookup {
	if len(spells) == 0 {
		return nil
	}
	keys := map[seaKey]bool{}
	for _, sp := range spells {
		if k, ok := sp.seaKey(); ok {
			keys[k] = true
		}
	}
	first, last, ok := hourRange(keys)
	if !ok {
		// No spell has a position: each says so, with nothing to read.
		return &seaLookup{}
	}
	rows, err := s.SeaHindcastRowsBetween(first, last)
	if err != nil {
		log.Printf("passages: %v", err)
		return &seaLookup{err: err}
	}
	return &seaLookup{rows: rows, problem: s.seaProblemNow()}
}

// hourFor is the stored hour behind a spell when it holds a sea state: false for
// a spell with no position, no stored hour, or an hour the model had no value for.
func (l *seaLookup) hourFor(sp feltSpell) (seaHindcastRow, bool) {
	k, ok := sp.seaKey()
	row, held := l.rows[k]
	return row, ok && held && row.MissingReason == ""
}

// forEachSeaPassage reads each of the passages in turn, oldest first, with the
// stored sea behind its spells, and hands them to fn until fn says to stop. A
// passage with no spells is passed over. It returns ctx's error when ctx ends
// first. A passage whose figures or hindcast rows cannot be read goes to
// readFailed, which says whether to carry on.
//
// PassageFiguresFor works out and stores the figures of a stale passage; readers
// that reach one together share the work (figuresFlight), so no walk needs a lock
// of its own.
func (s *passageStore) forEachSeaPassage(ctx context.Context, records []passageRecord, readFailed func(rec passageRecord, err error) (carryOn bool), fn func(rec passageRecord, spells []feltSpell, seas *seaLookup) (carryOn bool)) error {
	for _, rec := range records {
		if err := ctx.Err(); err != nil {
			return err
		}
		f, err := s.PassageFiguresFor(rec)
		var seas *seaLookup
		if err == nil {
			if seas = s.seaLookupFor(f.Stats.FeltSpells); seas != nil && seas.err != nil {
				err = fmt.Errorf("its hindcast rows could not be read: %w", seas.err)
			}
		}
		if err != nil {
			if !readFailed(rec, err) {
				return nil
			}
			continue
		}
		if seas == nil {
			continue
		}
		if !fn(rec, f.Stats.FeltSpells, seas) {
			return nil
		}
	}
	return nil
}

// forSpell is the sea behind one spell, from the row kept under its own key.
func (l *seaLookup) forSpell(sp feltSpell) spellSea {
	sea, _ := l.forSpellWithModel(sp)
	return sea
}

// forSpellWithModel is forSpell with the id of the wave model behind the stored
// hour, from the one lookup. The model is "" when no hour is stored.
func (l *seaLookup) forSpellWithModel(sp feltSpell) (sea spellSea, model string) {
	if k, ok := sp.seaKey(); ok {
		if row, held := l.rows[k]; held {
			return seaForSpell(sp, &row, l.err), row.Model
		}
	}
	if l.err == nil && l.problem != "" && sp.hasPosition() {
		return spellSea{Reason: l.problem}, ""
	}
	return seaForSpell(sp, nil, l.err), ""
}

// seaPeak is the largest of one kind of wave and when it was: Unix seconds, the
// middle of the spell it was in.
type seaPeak struct {
	seaTrainFigures
	At int64 `json:"at"`
}

// passageSea sums up the sea over a passage. When every spell lacks a sea state,
// Reason says why, in words for the whole passage.
type passageSea struct {
	Swell    *seaPeak `json:"swell"`
	WindWave *seaPeak `json:"wind_wave"`
	Reason   string   `json:"reason,omitempty"`
	Partial  string   `json:"partial,omitempty"`
}

// seaRefusedPrefix starts the sentence the wave plugin gives for an hour Open-Meteo
// refused ("Open-Meteo wouldn't give the sea for this hour: <its reason>").
const seaRefusedPrefix = "Open-Meteo wouldn't give the sea"

// passageSeaReason words why a passage has no sea state at all. A spell's own
// reason talks of "this hour" or "this spell" and may carry Open-Meteo's raw
// text, which is wrong for a passage, so the passage line never repeats one.
// Each reason the host words has a passage wording, and so does a refusal; when
// the spells give any other reason, or differ, the page says so and leaves each
// spell to say why.
func passageSeaReason(reasons []string) string {
	refused := true
	same := true
	for _, r := range reasons {
		refused = refused && strings.HasPrefix(r, seaRefusedPrefix)
		same = same && r == reasons[0]
	}
	if refused {
		return "Open-Meteo wouldn't give the sea for this passage's hours."
	}
	if !same {
		return "No spell on this passage has a sea state; each spell says why."
	}
	switch r := reasons[0]; {
	case r == seaReasonNotFetched:
		return "The sea state for this passage hasn't been fetched yet."
	case r == seaReasonNoPosition:
		return "No spell on this passage has a recorded position to look up the sea for."
	case r == seaReasonUnreadable:
		return "The recorded sea state for this passage couldn't be read."
	case r == seaReasonNoSplit:
		return "The wave plugin gave no swell and wind-wave split for this passage's hours."
	case strings.HasPrefix(r, seaReasonCantFetch):
		// The host's own sentence, naming the plugin; it says nothing of an hour.
		return r
	}
	return "No spell on this passage has a sea state; each spell says why."
}

// seaSummaryOf names the largest swell and wind wave over the spells and when.
// When only some spells have a sea state the peaks cover only those, and the
// summary says how many have none (Partial). A passage with a flat sea in some
// spells and none for the rest is never called flat, since the rest were never
// looked at: Reason then says both.
func seaSummaryOf(spells []feltSpellOut) passageSea {
	var out passageSea
	var reasons []string
	allReasons := true
	// A swell not judged in enclosed water and a wind wave not checked against its
	// fetch are the model's figures for a sea the boat was not in, so neither is a
	// peak; each is counted instead (ADR 0300).
	var unchecked, enclosed, swellUnchecked int
	raisePeak := func(peak **seaPeak, t *seaTrain, at int64) {
		if *peak == nil || t.HeightM > (*peak).HeightM {
			*peak = &seaPeak{seaTrainFigures: t.seaTrainFigures, At: at}
		}
	}
	for _, sp := range spells {
		if sp.Sea == nil {
			allReasons = false
			continue
		}
		if sp.Sea.Reason != "" {
			reasons = append(reasons, sp.Sea.Reason)
			continue
		}
		allReasons = false
		at := sp.Start + (sp.End-sp.Start)/2
		if s := sp.Sea.Swell; s != nil {
			switch s.swellVerdict() {
			case seaSwellUnchecked:
				swellUnchecked++ // the shore could not be read, so it was not judged for enclosed water
			case seaSwellEnclosed:
				enclosed++
			default:
				raisePeak(&out.Swell, s, at)
			}
		}
		if w := sp.Sea.WindWave; w != nil {
			if w.windVerdict() == seaWindUnchecked {
				unchecked++
			} else {
				raisePeak(&out.WindWave, w, at)
			}
		}
	}
	var parts []string
	if unchecked > 0 {
		parts = append(parts, fmt.Sprintf("wind wave not checked against fetch for %d of %d spells", unchecked, len(spells)))
	}
	if enclosed > 0 {
		parts = append(parts, fmt.Sprintf("swell not judged in enclosed water for %d of %d spells", enclosed, len(spells)))
	}
	if swellUnchecked > 0 {
		parts = append(parts, fmt.Sprintf("swell not checked for enclosed water for %d of %d spells", swellUnchecked, len(spells)))
	}
	notes := strings.Join(parts, "; ")
	missing := len(reasons)
	noPeaks := out.Swell == nil && out.WindWave == nil
	switch {
	case missing > 0 && allReasons:
		out.Reason = passageSeaReason(reasons)
	case noPeaks && missing > 0 && notes != "":
		out.Reason = fmt.Sprintf("No sea state for %d of %d spells; %s.", missing, len(spells), notes)
	case noPeaks && missing > 0:
		out.Reason = fmt.Sprintf("No sea state for %d of %d spells, and no swell or wind wave in the rest.", missing, len(spells))
	case noPeaks && notes != "":
		// Never "flat": the spells that were left out were not looked at.
		out.Reason = capitalise(notes) + "."
	case noPeaks:
	default:
		if missing > 0 {
			parts = append([]string{fmt.Sprintf("no sea for %d of %d spells", missing, len(spells))}, parts...)
		}
		out.Partial = strings.Join(parts, "; ")
	}
	return out
}
