package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"os"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// ── the stored sea state behind each recorded spell (ADR 0299) ──────────────

// fakeHindcast is a wave provider with the hindcast export. answer builds the
// reply; the default gives every hour of every point a swell and a wind wave.
type fakeHindcast struct {
	calls       []hindcastCall
	err         error
	unsupported bool
	answer      func(points []currentsPoint, start, end time.Time) waveHindcastBundle
}

type hindcastCall struct {
	points     []currentsPoint
	start, end time.Time
}

func (f *fakeHindcast) ID() string          { return "fake-waves" }
func (f *fakeHindcast) Name() string        { return "Fake waves" }
func (f *fakeHindcast) Description() string { return "" }
func (f *fakeHindcast) TTLSeconds() int64   { return 3600 }
func (f *fakeHindcast) FetchWaves(lat, lon float64, days int) (waveForecastBundle, error) {
	panic("the hindcast job must never read the forecast")
}

func (f *fakeHindcast) SupportsWaveHindcast() bool { return !f.unsupported }

func (f *fakeHindcast) FetchWaveHindcast(ctx context.Context, points []currentsPoint, start, end time.Time) (waveHindcastBundle, error) {
	f.calls = append(f.calls, hindcastCall{points, start, end})
	if f.err != nil {
		return waveHindcastBundle{}, f.err
	}
	if f.answer != nil {
		return f.answer(points, start, end), nil
	}
	return defaultHindcastAnswer(points, start, end), nil
}

// seriesFor builds one point's hours from start to end, each made by hour.
func seriesFor(start, end time.Time, hour func(t time.Time) waveHindcastHour) waveHindcastSeries {
	s := waveHindcastSeries{Model: "ncep_gfswave025", ModelLabel: "GFS-Wave"}
	for h := start; !h.After(end); h = h.Add(time.Hour) {
		s.Hourly = append(s.Hourly, hour(h))
	}
	return s
}

func answerEveryPoint(points []currentsPoint, start, end time.Time, hour func(p currentsPoint, t time.Time) waveHindcastHour) waveHindcastBundle {
	var b waveHindcastBundle
	for _, p := range points {
		b.Points = append(b.Points, seriesFor(start, end, func(t time.Time) waveHindcastHour { return hour(p, t) }))
	}
	return b
}

func swellAndChop(t time.Time) waveHindcastHour {
	return waveHindcastHour{waveHourPoint: waveHourPoint{
		Time: t, WavePeriodS: pfloat(9), WaveHeightM: pfloat(1), WaveDirectionDeg: pfloat(270),
		SwellWaveHeightM: 0.8, SwellWavePeriodS: 9, SwellWaveDirectionDeg: 270,
		WindWaveHeightM: 0.6, WindWavePeriodS: 4, WindWaveDirectionDeg: 315,
	}}
}

func defaultHindcastAnswer(points []currentsPoint, start, end time.Time) waveHindcastBundle {
	return answerEveryPoint(points, start, end, func(_ currentsPoint, t time.Time) waveHindcastHour { return swellAndChop(t) })
}

func resolveTo(p waveProvider) func() (waveProvider, string, error) {
	return func() (waveProvider, string, error) { return p, "fake-waves", nil }
}

// seaPassage stores one passage with two 15-minute spells in the 10:00 UTC
// hour, with a heading of due north, and returns its spells.
func seaPassage(t *testing.T) (*passageStore, passageRecord, []feltSpell) {
	t.Helper()
	store := newTestPassageStore(t)
	b := newTrackBuilder()
	b.t = passageEpoch.Add(10 * time.Hour)
	b.run(time.Minute, time.Minute, 0)
	b.run(30*time.Minute, time.Minute, 6)
	setSway(b.pts, 1, 30, 1, 1)
	for i := range b.pts {
		b.pts[i].HeadingDeg = pfloat(0)
	}
	appendAll(t, store, b.pts)
	rec, err := store.InsertPassage(passageRecord{Start: b.pts[0].At, End: b.pts[len(b.pts)-1].At, Title: "sea"})
	if err != nil {
		t.Fatal(err)
	}
	f, err := store.PassageFiguresFor(rec)
	if err != nil {
		t.Fatal(err)
	}
	if len(f.Stats.FeltSpells) != 2 {
		t.Fatalf("spells: %+v", f.Stats.FeltSpells)
	}
	return store, rec, f.Stats.FeltSpells
}

// seaMovingPassage stores a passage of the given hours at the given speed, due
// north, with a measured sway throughout, and returns its spells. A boat that
// is under way at all passes a cell about every half hour.
func seaMovingPassage(t *testing.T, hours int, sog float64) (*passageStore, []feltSpell) {
	t.Helper()
	store := newTestPassageStore(t)
	b := newTrackBuilder()
	b.t = passageEpoch.Add(time.Hour)
	b.run(time.Minute, time.Minute, 0)
	b.run(time.Duration(hours)*time.Hour, time.Minute, sog)
	setSway(b.pts, 1, len(b.pts)-1, 1, 1)
	appendAll(t, store, b.pts)
	rec, err := store.InsertPassage(passageRecord{Start: b.pts[0].At, End: b.pts[len(b.pts)-1].At, Title: "moving"})
	if err != nil {
		t.Fatal(err)
	}
	f, err := store.PassageFiguresFor(rec)
	if err != nil {
		t.Fatal(err)
	}
	return store, f.Stats.FeltSpells
}

// seaNow is a day after the epoch: the spells in the 10:00 hour are 14 h old,
// past the 12 h an hour must reach before its sea is fetched.
var seaNow = passageEpoch.Add(24 * time.Hour)

func logOf(t *testing.T) *bytes.Buffer {
	t.Helper()
	var buf bytes.Buffer
	log.SetOutput(&buf)
	t.Cleanup(func() { log.SetOutput(os.Stderr) })
	return &buf
}

func distinctSeaKeys(spells []feltSpell) map[seaKey]bool {
	out := map[seaKey]bool{}
	for _, sp := range spells {
		if k, ok := sp.seaKey(); ok {
			out[k] = true
		}
	}
	return out
}

func distinctCells(spells []feltSpell) map[seaCell]bool {
	out := map[seaCell]bool{}
	for k := range distinctSeaKeys(spells) {
		out[k.cell()] = true
	}
	return out
}

// storedSea is the rows kept for the spells' keys.
func storedSea(t *testing.T, store *passageStore, spells []feltSpell) map[seaKey]seaHindcastRow {
	t.Helper()
	keys := distinctSeaKeys(spells)
	first, last, ok := hourRange(keys)
	if !ok {
		return nil
	}
	rows, err := store.SeaHindcastRowsBetween(first, last)
	if err != nil {
		t.Fatal(err)
	}
	out := map[seaKey]seaHindcastRow{}
	for k, r := range rows {
		if keys[k] {
			out[k] = r
		}
	}
	return out
}

// seaHindcastCount is every row the table holds.
func (s *passageStore) seaHindcastCount() int {
	var n int
	_ = s.db.QueryRow(`SELECT COUNT(*) FROM sea_hindcast_hours`).Scan(&n)
	return n
}

func TestSeaHindcastFillsEverySpellHourOnceAndAsksAtTheCellCentres(t *testing.T) {
	store, _, spells := seaPassage(t)
	fake := &fakeHindcast{}
	sum := runSeaHindcast(context.Background(), store, resolveTo(fake), seaNow)
	keys := distinctSeaKeys(spells)
	if sum.Filled != len(keys) || sum.NoValue != 0 || sum.Failed != 0 || sum.Skipped != "" {
		t.Fatalf("summary %+v, want %d filled", sum, len(keys))
	}
	if len(fake.calls) != 1 {
		t.Fatalf("one call covers every position, got %d", len(fake.calls))
	}
	c := fake.calls[0]
	cells := distinctCells(spells)
	if len(c.points) != len(cells) {
		t.Errorf("asked about %d positions, spells lie in %d cells", len(c.points), len(cells))
	}
	for _, p := range c.points {
		latC, lonC := centiDegrees(p.Lat), centiDegrees(p.Lon)
		if !cells[seaCell{LatC: latC, LonC: lonC}] {
			t.Errorf("asked about %v, %v: not a spell cell", p.Lat, p.Lon)
		}
		if !approx(p.Lat*100, float64(latC), 1e-6) || !approx(p.Lon*100, float64(lonC), 1e-6) {
			t.Errorf("must ask at the 2 dp cell centre, got %v, %v", p.Lat, p.Lon)
		}
	}
	if !c.end.Equal(c.start) {
		t.Errorf("one hour is needed, asked for %v to %v", c.start, c.end)
	}
	got := storedSea(t, store, spells)
	for k := range keys {
		r, ok := got[k]
		if !ok || r.Provider != "fake-waves" || r.Model != "ncep_gfswave025" || r.FetchedMs != seaNow.UnixMilli() {
			t.Fatalf("row for %+v: %+v", k, r)
		}
		if r.Swell == nil || r.Swell.HeightM != 0.8 || r.Swell.PeriodS != 9 || r.Swell.FromDeg != 270 {
			t.Errorf("swell %+v", r.Swell)
		}
		if r.WindWave == nil || r.WindWave.HeightM != 0.6 || r.WindWave.PeriodS != 4 || r.WindWave.FromDeg != 315 {
			t.Errorf("wind wave %+v", r.WindWave)
		}
		if r.MissingReason != "" {
			t.Errorf("missing reason %q", r.MissingReason)
		}
	}

	// A second run has nothing to fill and asks for nothing.
	again := &fakeHindcast{}
	sum = runSeaHindcast(context.Background(), store, resolveTo(again), seaNow)
	if sum.Filled != 0 || len(again.calls) != 0 {
		t.Errorf("second run: %+v, %d calls", sum, len(again.calls))
	}
}

// A moving boat is one request for the whole passage, not one for each cell, and
// every hour is matched to its own cell's series.
func TestSeaHindcastAsksOnceForAMovingPassageAndMatchesEachHourToItsOwnCell(t *testing.T) {
	store, spells := seaMovingPassage(t, 30, 1.2)
	cells := distinctCells(spells)
	if len(cells) < 30 {
		t.Fatalf("the passage should cross at least 30 cells, got %d", len(cells))
	}
	fake := &fakeHindcast{answer: func(points []currentsPoint, start, end time.Time) waveHindcastBundle {
		return answerEveryPoint(points, start, end, func(p currentsPoint, t time.Time) waveHindcastHour {
			h := swellAndChop(t)
			h.SwellWaveHeightM = -p.Lat // each cell tells its own series by its latitude
			return h
		})
	}}
	sum := runSeaHindcast(context.Background(), store, resolveTo(fake), passageEpoch.Add(48*time.Hour))
	if sum.Filled != len(distinctSeaKeys(spells)) || sum.Failed != 0 {
		t.Fatalf("summary %+v, want %d filled", sum, len(distinctSeaKeys(spells)))
	}
	if len(fake.calls) != 1 {
		t.Fatalf("a 30-hour passage across %d cells should be 1 call, got %d", len(cells), len(fake.calls))
	}
	if len(fake.calls[0].points) != len(cells) {
		t.Errorf("the call should carry every distinct cell: %d of %d", len(fake.calls[0].points), len(cells))
	}
	for k, r := range storedSea(t, store, spells) {
		if want := -float64(k.LatC) / 100; r.Swell == nil || !approx(r.Swell.HeightM, want, 1e-9) {
			t.Errorf("hour %d at cell %d: swell %+v, want the series of its own cell (%v)", k.HourMs, k.LatC, r.Swell, want)
		}
	}
}

// More cells than one call carries go in several calls over the same window.
func TestSeaHindcastSplitsMoreCellsThanOneCallCarries(t *testing.T) {
	store, spells := seaMovingPassage(t, 80, 1.2)
	cells := distinctCells(spells)
	if len(cells) <= maxHindcastPoints {
		t.Fatalf("the test needs more than %d cells, got %d", maxHindcastPoints, len(cells))
	}
	fake := &fakeHindcast{}
	sum := runSeaHindcast(context.Background(), store, resolveTo(fake), passageEpoch.Add(120*time.Hour))
	if sum.Filled != len(distinctSeaKeys(spells)) || sum.Failed != 0 {
		t.Fatalf("summary %+v", sum)
	}
	if want := (len(cells) + maxHindcastPoints - 1) / maxHindcastPoints; len(fake.calls) != want {
		t.Errorf("%d cells make %d calls, got %d", len(cells), want, len(fake.calls))
	}
	for _, c := range fake.calls {
		if len(c.points) > maxHindcastPoints {
			t.Errorf("a call carried %d points", len(c.points))
		}
		if !c.start.Equal(fake.calls[0].start) || !c.end.Equal(fake.calls[0].end) {
			t.Errorf("the calls share one window")
		}
	}
}

func TestPlanSeaWindowsCutsAtSevenDays(t *testing.T) {
	h := func(hours int) seaKey {
		return seaKey{HourMs: passageEpoch.Add(time.Duration(hours) * time.Hour).UnixMilli(), LatC: hours}
	}
	windows := planSeaWindows([][]seaKey{{h(0), h(1), h(100), h(167), h(168), h(400)}})
	if len(windows) != 3 {
		t.Fatalf("windows %+v", windows)
	}
	if len(windows[0].Keys) != 4 || !windows[0].End.Equal(passageEpoch.Add(167*time.Hour)) {
		t.Errorf("the first window holds every hour under seven days from its start: %+v", windows[0])
	}
	if !windows[1].Start.Equal(passageEpoch.Add(168*time.Hour)) || !windows[2].Start.Equal(passageEpoch.Add(400*time.Hour)) {
		t.Errorf("windows %+v", windows)
	}
}

// An hour's forecast-lead values settle for half a day: one 11 h old is left
// alone and one 13 h old is fetched.
func TestSeaHindcastLeavesHoursLessThanTwelveHoursOldAlone(t *testing.T) {
	store, _, _ := seaPassage(t)
	fake := &fakeHindcast{}
	// The spells are in the 10:00 hour.
	sum := runSeaHindcast(context.Background(), store, resolveTo(fake), passageEpoch.Add(21*time.Hour))
	if sum.Filled != 0 || len(fake.calls) != 0 {
		t.Errorf("an hour 11 h old was fetched: %+v with %d calls", sum, len(fake.calls))
	}
	sum = runSeaHindcast(context.Background(), store, resolveTo(fake), passageEpoch.Add(23*time.Hour))
	if sum.Filled == 0 || len(fake.calls) != 1 {
		t.Errorf("an hour 13 h old was left: %+v with %d calls", sum, len(fake.calls))
	}
}

// A spell's sea hour is its middle rounded to the nearest hour: the model's
// values are instantaneous at the hour, so 10:47 is the 11:00 value.
func TestASpellsSeaHourIsItsMiddleRoundedToTheNearestHour(t *testing.T) {
	north := 0.0
	at := func(from, to time.Duration) int64 {
		sp := spellWithHeading(&north)
		sp.Start = passageEpoch.Add(10*time.Hour + from).Unix()
		sp.End = passageEpoch.Add(10*time.Hour + to).Unix()
		k, ok := sp.seaKey()
		if !ok {
			t.Fatal("a spell with a position has a key")
		}
		return k.HourMs
	}
	hour := func(h int) int64 { return passageEpoch.Add(time.Duration(h) * time.Hour).UnixMilli() }
	if got := at(40*time.Minute, 54*time.Minute); got != hour(11) {
		t.Errorf("a middle at 10:47 is keyed to %d, want 11:00", got)
	}
	if got := at(10*time.Minute, 24*time.Minute); got != hour(10) {
		t.Errorf("a middle at 10:17 is keyed to %d, want 10:00", got)
	}
	if got := at(23*time.Minute, 37*time.Minute); got != hour(11) {
		t.Errorf("a middle at 10:30 rounds up to 11:00, got %d", got)
	}
}

// The age cutoff is the rounded hour's: the passage's spells round to 10:00,
// which is 12 h old at 22:00 and not at 21:59.
func TestTheAgeCutoffAppliesToTheRoundedHour(t *testing.T) {
	store, _, _ := seaPassage(t)
	fake := &fakeHindcast{}
	if sum := runSeaHindcast(context.Background(), store, resolveTo(fake), passageEpoch.Add(21*time.Hour+59*time.Minute)); sum.Filled != 0 || len(fake.calls) != 0 {
		t.Errorf("an hour just under 12 h old was fetched: %+v", sum)
	}
	if sum := runSeaHindcast(context.Background(), store, resolveTo(fake), passageEpoch.Add(22*time.Hour)); sum.Filled == 0 {
		t.Errorf("an hour 12 h old was left: %+v", sum)
	}
}

// A passage whose stored figures are old is worked out on the way, not skipped.
func TestSeaHindcastWorksOutAStalePassagesFiguresInsteadOfSkippingIt(t *testing.T) {
	store, rec, spells := seaPassage(t)
	if _, err := store.db.Exec(`UPDATE passages SET stats_json = NULL WHERE id = ?`, rec.ID); err != nil {
		t.Fatal(err)
	}
	fake := &fakeHindcast{}
	sum := runSeaHindcast(context.Background(), store, resolveTo(fake), seaNow)
	if sum.Filled != len(distinctSeaKeys(spells)) {
		t.Errorf("a passage with no figures yet was skipped: %+v", sum)
	}
}

func missingHour(reason string) func(points []currentsPoint, start, end time.Time) waveHindcastBundle {
	return func(points []currentsPoint, start, end time.Time) waveHindcastBundle {
		return answerEveryPoint(points, start, end, func(_ currentsPoint, t time.Time) waveHindcastHour {
			return waveHindcastHour{waveHourPoint: waveHourPoint{Time: t}, MissingReason: reason}
		})
	}
}

func TestSeaHindcastStoresThePluginsReasonAsGivenAndNeverRefetchesIt(t *testing.T) {
	store, _, spells := seaPassage(t)
	buf := logOf(t)
	fake := &fakeHindcast{answer: missingHour("GFS-Wave has no value at this cell")}
	sum := runSeaHindcast(context.Background(), store, resolveTo(fake), seaNow)
	n := len(distinctSeaKeys(spells))
	if sum.NoValue != n || sum.Filled != 0 {
		t.Fatalf("summary %+v, want %d with no value", sum, n)
	}
	for k, r := range storedSea(t, store, spells) {
		if r.MissingReason != "GFS-Wave has no value at this cell" || r.Swell != nil || r.WindWave != nil {
			t.Errorf("row %+v: %+v", k, r)
		}
	}
	if !strings.Contains(buf.String(), "with no value") || !strings.Contains(buf.String(), "GFS-Wave has no value at this cell") {
		t.Errorf("the summary line should give the reason: %s", buf.String())
	}
	again := &fakeHindcast{}
	runSeaHindcast(context.Background(), store, resolveTo(again), seaNow)
	if len(again.calls) != 0 {
		t.Errorf("a no-value hour is a settled answer; it was asked again %d times", len(again.calls))
	}
}

// The model the plugin names is the model that is kept, whatever it is called.
func TestSeaHindcastKeepsTheModelEachPointNames(t *testing.T) {
	store, _, spells := seaPassage(t)
	fake := &fakeHindcast{answer: func(points []currentsPoint, start, end time.Time) waveHindcastBundle {
		b := defaultHindcastAnswer(points, start, end)
		for i := range b.Points {
			b.Points[i].Model, b.Points[i].ModelLabel = "best_match", "The blend"
		}
		return b
	}}
	runSeaHindcast(context.Background(), store, resolveTo(fake), seaNow)
	for _, r := range storedSea(t, store, spells) {
		if r.Model != "best_match" {
			t.Errorf("row %+v", r)
		}
	}
}

func TestSeaHindcastKeepsAFlatComponentAsNoneNotAsMissing(t *testing.T) {
	store, _, spells := seaPassage(t)
	fake := &fakeHindcast{answer: func(points []currentsPoint, start, end time.Time) waveHindcastBundle {
		// Wind sea only: the swell has no period, which is how the model says none.
		return answerEveryPoint(points, start, end, func(_ currentsPoint, t time.Time) waveHindcastHour {
			return waveHindcastHour{waveHourPoint: waveHourPoint{
				Time: t, WavePeriodS: pfloat(4), WaveHeightM: pfloat(0.6), WaveDirectionDeg: pfloat(54),
				WindWaveHeightM: 0.6, WindWavePeriodS: 4, WindWaveDirectionDeg: 54,
			}}
		})
	}}
	runSeaHindcast(context.Background(), store, resolveTo(fake), seaNow)
	for _, r := range storedSea(t, store, spells) {
		if r.MissingReason != "" || r.Swell != nil || r.WindWave == nil {
			t.Errorf("row %+v", r)
		}
	}
}

// A hour with neither system and no reason is a flat sea the model gave, kept
// as one: it is filled, and carries no reason.
func TestSeaHindcastKeepsAFlatSeaAsFilled(t *testing.T) {
	store, _, spells := seaPassage(t)
	fake := &fakeHindcast{answer: func(points []currentsPoint, start, end time.Time) waveHindcastBundle {
		return answerEveryPoint(points, start, end, func(_ currentsPoint, t time.Time) waveHindcastHour {
			return waveHindcastHour{waveHourPoint: waveHourPoint{Time: t}}
		})
	}}
	sum := runSeaHindcast(context.Background(), store, resolveTo(fake), seaNow)
	if sum.Filled != len(distinctSeaKeys(spells)) || sum.NoValue != 0 {
		t.Errorf("summary %+v", sum)
	}
}

func TestSeaHindcastStoresNothingOnATransientErrorAndTriesAgainNextTime(t *testing.T) {
	store, _, spells := seaPassage(t)
	buf := logOf(t)
	bad := &fakeHindcast{err: errors.New("open-meteo marine hindcast API returned 503")}
	sum := runSeaHindcast(context.Background(), store, resolveTo(bad), seaNow)
	if sum.Failed != len(distinctSeaKeys(spells)) || sum.Filled != 0 {
		t.Fatalf("summary %+v", sum)
	}
	if got := storedSea(t, store, spells); len(got) != 0 {
		t.Errorf("a failed request stores nothing, got %+v", got)
	}
	if !strings.Contains(buf.String(), "503") || !strings.Contains(buf.String(), "failed") {
		t.Errorf("log: %s", buf.String())
	}
	good := &fakeHindcast{}
	if sum = runSeaHindcast(context.Background(), store, resolveTo(good), seaNow); sum.Filled != len(distinctSeaKeys(spells)) {
		t.Errorf("the next run fills them: %+v", sum)
	}
}

// An answer that leaves an hour out is the plugin's definite word that it has no
// figure for it: the hour is stored with a reason and never guessed or asked again.
func TestSeaHindcastWithAnAnswerMissingTheHourSettlesItWithAReason(t *testing.T) {
	store, _, spells := seaPassage(t)
	logOf(t)
	fake := &fakeHindcast{answer: func(points []currentsPoint, start, end time.Time) waveHindcastBundle {
		return answerEveryPoint(points, start, end, func(_ currentsPoint, t time.Time) waveHindcastHour { return swellAndChop(t.Add(-24 * time.Hour)) })
	}}
	sum := runSeaHindcast(context.Background(), store, resolveTo(fake), seaNow)
	n := len(distinctSeaKeys(spells))
	if sum.NoValue != n || sum.Failed != 0 {
		t.Errorf("summary %+v, want %d with no value", sum, n)
	}
	got := storedSea(t, store, spells)
	if len(got) != n {
		t.Fatalf("rows %+v", got)
	}
	for k, r := range got {
		if r.MissingReason != "The wave plugin gave no figure for this hour." || r.Swell != nil || r.WindWave != nil {
			t.Errorf("row %+v: %+v", k, r)
		}
	}
	again := &fakeHindcast{}
	runSeaHindcast(context.Background(), store, resolveTo(again), seaNow)
	if len(again.calls) != 0 {
		t.Errorf("a settled hour was asked again %d times", len(again.calls))
	}
}

// A definite refusal from the data source (the plugin says so) settles every
// hour of the request with the plugin's sentence.
func TestSeaHindcastSettlesEveryHourOfARefusedRequestWithThePluginsSentence(t *testing.T) {
	store, _, spells := seaPassage(t)
	logOf(t)
	const why = "Open-Meteo wouldn't give the sea for this hour: Parameter 'start_hour' is out of allowed range."
	refuse := &fakeHindcast{answer: func([]currentsPoint, time.Time, time.Time) waveHindcastBundle {
		return waveHindcastBundle{Refused: why}
	}}
	sum := runSeaHindcast(context.Background(), store, resolveTo(refuse), seaNow)
	n := len(distinctSeaKeys(spells))
	if sum.NoValue != n || sum.Failed != 0 {
		t.Fatalf("summary %+v, want %d with no value", sum, n)
	}
	for k, r := range storedSea(t, store, spells) {
		if r.MissingReason != why {
			t.Errorf("row %+v: %+v", k, r)
		}
	}
	again := &fakeHindcast{}
	runSeaHindcast(context.Background(), store, resolveTo(again), seaNow)
	if len(again.calls) != 0 {
		t.Errorf("a refused hour was asked again %d times", len(again.calls))
	}
}

func TestSeaReportCountsFromOutcomesAndAStoreFailureCountsAsFailed(t *testing.T) {
	k := func(h int64) seaKey { return seaKey{HourMs: h} }
	outcomes := []seaOutcome{
		{Key: k(1), Row: &seaHindcastRow{}},
		{Key: k(2), Row: &seaHindcastRow{MissingReason: "no value"}},
		{Key: k(3), Failure: "503"},
	}
	var before seaReport
	for _, o := range outcomes {
		before.add(o)
	}
	if before.sum.Filled != 1 || before.sum.NoValue != 1 || before.sum.Failed != 1 {
		t.Errorf("counts %+v", before.sum)
	}
	failStored(outcomes, errors.New("disk full"))
	var after seaReport
	for _, o := range outcomes {
		after.add(o)
	}
	if after.sum.Filled != 0 || after.sum.NoValue != 0 || after.sum.Failed != 3 {
		t.Errorf("a store that kept nothing: %+v", after.sum)
	}
	if len(after.failures) != 2 || after.failures[0] != "disk full" {
		t.Errorf("failures %v", after.failures)
	}
}

func TestSeaHindcastWithAPluginThatLacksTheExportStopsAndSaysWhatToInstall(t *testing.T) {
	store, _, spells := seaPassage(t)
	buf := logOf(t)
	fake := &fakeHindcast{unsupported: true}
	sum := runSeaHindcast(context.Background(), store, resolveTo(fake), seaNow)
	if !strings.Contains(sum.Skipped, "install the updated fake-waves plugin") {
		t.Errorf("skipped %q", sum.Skipped)
	}
	if len(fake.calls) != 0 {
		t.Errorf("the capability is checked once, up front; asked %d times", len(fake.calls))
	}
	if got := storedSea(t, store, spells); len(got) != 0 {
		t.Errorf("stored %+v", got)
	}
	if !strings.Contains(buf.String(), "install the updated fake-waves plugin") || !strings.Contains(buf.String(), "tries again at the next hourly sweep") {
		t.Errorf("log: %s", buf.String())
	}
}

// A provider that cannot give a hindcast at all is the same as a plugin without
// the export.
func TestSeaHindcastWithAProviderWithoutTheCapabilityIsSkippedWithTheSameHint(t *testing.T) {
	store, _, _ := seaPassage(t)
	logOf(t)
	sum := runSeaHindcast(context.Background(), store, resolveTo(plainWaves{}), seaNow)
	if !strings.Contains(sum.Skipped, "install the updated fake-waves plugin") {
		t.Errorf("skipped %q", sum.Skipped)
	}
}

type plainWaves struct{}

func (plainWaves) ID() string          { return "plain" }
func (plainWaves) Name() string        { return "Plain" }
func (plainWaves) Description() string { return "" }
func (plainWaves) TTLSeconds() int64   { return 3600 }
func (plainWaves) FetchWaves(lat, lon float64, days int) (waveForecastBundle, error) {
	return waveForecastBundle{}, nil
}

func TestSeaHindcastWithNoWavePluginStoresNothingAndSaysSo(t *testing.T) {
	store, _, spells := seaPassage(t)
	buf := logOf(t)
	sum := runSeaHindcast(context.Background(), store, func() (waveProvider, string, error) {
		return nil, "", fmt.Errorf("unknown wave provider configured: %q: %w", "open-meteo-marine", errNoWaveProviderInstalled)
	}, seaNow)
	if sum.Skipped == "" || !strings.Contains(buf.String(), "no wave provider") {
		t.Errorf("summary %+v, log %s", sum, buf.String())
	}
	if got := storedSea(t, store, spells); len(got) != 0 {
		t.Errorf("stored %+v", got)
	}
}

func TestSeaHindcastStopsWhenTheContextEnds(t *testing.T) {
	store, _, _ := seaPassage(t)
	logOf(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	fake := &fakeHindcast{}
	sum := runSeaHindcast(ctx, store, resolveTo(fake), seaNow)
	if len(fake.calls) != 0 || sum.Filled != 0 {
		t.Errorf("summary %+v, %d calls", sum, len(fake.calls))
	}
}

func TestSeaHindcastKeyIsTheHourAndThePositionNotThePassage(t *testing.T) {
	store, rec, spells := seaPassage(t)
	runSeaHindcast(context.Background(), store, resolveTo(&fakeHindcast{}), seaNow)
	// Trim the passage to its first spell: the rows stay, keyed by hour and cell.
	if _, err := store.db.Exec(`UPDATE passages SET end_ms = ? WHERE id = ?`, rec.Start.Add(16*time.Minute).UnixMilli(), rec.ID); err != nil {
		t.Fatal(err)
	}
	got := storedSea(t, store, spells)
	if len(got) != len(distinctSeaKeys(spells)) {
		t.Errorf("rows %d, want %d", len(got), len(distinctSeaKeys(spells)))
	}
	if n := store.seaHindcastCount(); n != len(distinctSeaKeys(spells)) {
		t.Errorf("table holds %d rows", n)
	}
}

// noCoastline is the sweep's coastline on an install that has none.
func noCoastline() *offlineCoastlineReader { return nil }

// startUpOnce runs the start-up work with a sweep that stops after its first
// pass: the clock cancels the context as the pass takes the time.
func startUpOnce(store *passageStore, settingsPath string) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	startPassageFiguresWith(ctx, store, settingsPath, waveProviderResolver(settingsPath), noCoastline, time.Hour, func() time.Time {
		cancel()
		return time.Now().UTC()
	})
}

func TestStartUpChainsTheHindcastAfterTheRecompute(t *testing.T) {
	store, _, _ := seaPassage(t)
	buf := logOf(t)
	startUpOnce(store, "no-such-settings.yaml")
	out := buf.String()
	rec := strings.Index(out, "start-up recompute")
	sea := strings.Index(out, "sea hindcast")
	if rec < 0 || sea < 0 || sea < rec {
		t.Errorf("the recompute line must come before the hindcast line:\n%s", out)
	}
}

// sweepClock hands the sweep a time on each pass and says each time it was asked.
type sweepClock struct {
	times []time.Time // the time for the first pass, the second and so on; the last repeats
	asked chan int
	n     atomic.Int32
}

func newSweepClock(times ...time.Time) *sweepClock {
	return &sweepClock{times: times, asked: make(chan int, 100)}
}

func (c *sweepClock) now() time.Time {
	n := int(c.n.Add(1))
	c.asked <- n
	return c.times[min(n, len(c.times))-1]
}

// waitFor blocks until the clock has been asked n times.
func (c *sweepClock) waitFor(t *testing.T, n int) {
	t.Helper()
	timeout := time.After(10 * time.Second)
	for {
		select {
		case got := <-c.asked:
			if got >= n {
				return
			}
		case <-timeout:
			t.Fatalf("the sweep ran %d passes, want %d", c.n.Load(), n)
		}
	}
}

// startSweeps runs the sweep in a goroutine; the returned func cancels it and
// waits for it to stop.
func startSweeps(t *testing.T, store *passageStore, fake waveProvider, every time.Duration, clock *sweepClock) (stop func(), done <-chan struct{}) {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	d := make(chan struct{})
	go func() {
		defer close(d)
		runSeaSweeps(ctx, store, resolveTo(fake), noCoastline, every, clock.now)
	}()
	stop = func() {
		cancel()
		select {
		case <-d:
		case <-time.After(10 * time.Second):
			t.Fatal("the sweep did not stop when its context ended")
		}
	}
	t.Cleanup(cancel)
	return stop, d
}

// The sweep runs once at the start and again every interval, taking the time
// afresh each pass: the first pass is too early to fetch the passage's hour and
// the second, on a later clock, fetches it.
func TestTheSeaSweepRunsAtStartAndAgainAfterTheIntervalWithAFreshClock(t *testing.T) {
	store, _, spells := seaPassage(t)
	logOf(t)
	fake := &fakeHindcast{}
	clock := newSweepClock(passageEpoch.Add(11*time.Hour), seaNow)
	stop, _ := startSweeps(t, store, fake, 5*time.Millisecond, clock)
	clock.waitFor(t, 3) // the third pass starts only after the second has finished
	stop()
	if len(fake.calls) != 1 {
		t.Fatalf("one fetch in all: the first pass is too early, the second fills, the third finds nothing; got %d", len(fake.calls))
	}
	got := storedSea(t, store, spells)
	if len(got) != len(distinctSeaKeys(spells)) {
		t.Fatalf("rows %+v", got)
	}
	for k, r := range got {
		if r.FetchedMs != seaNow.UnixMilli() {
			t.Errorf("row %+v was fetched at %d, want the second pass's clock", k, r.FetchedMs)
		}
	}
}

func TestTheSeaSweepStopsWhenItsContextEnds(t *testing.T) {
	store, _, _ := seaPassage(t)
	logOf(t)
	clock := newSweepClock(seaNow)
	stop, _ := startSweeps(t, store, &fakeHindcast{}, time.Hour, clock)
	clock.waitFor(t, 1)
	stop() // fails the test if the sweep, waiting out its hour, does not return
}

// A sweep that finds nothing to do says nothing.
func TestAQuietSweepLogsNothing(t *testing.T) {
	store, _, _ := seaPassage(t)
	runSeaHindcast(context.Background(), store, resolveTo(&fakeHindcast{}), seaNow)
	buf := logOf(t)
	fake := &fakeHindcast{}
	runSeaHindcast(context.Background(), store, resolveTo(fake), seaNow)
	if buf.Len() != 0 || len(fake.calls) != 0 {
		t.Errorf("a quiet pass logged %q and asked %d times", buf.String(), len(fake.calls))
	}
}

// A passage recorded, edited or imported since the last pass is found by the
// next one: nothing else asks for its sea.
func TestTheNextSweepFindsAPassageAddedSinceTheLast(t *testing.T) {
	store, _, firstSpells := seaPassage(t)
	logOf(t)
	fake := &fakeHindcast{}
	runSeaHindcast(context.Background(), store, resolveTo(fake), seaNow)
	_, laterSpells := addSeaPassage(t, store, 11)
	runSeaHindcast(context.Background(), store, resolveTo(fake), seaNow)
	if len(fake.calls) != 2 {
		t.Fatalf("asks %d, want one for each pass that had something", len(fake.calls))
	}
	if got := len(storedSea(t, store, laterSpells)); got != len(distinctSeaKeys(laterSpells)) || got == 0 {
		t.Errorf("the new passage has %d rows", got)
	}
	if got := len(storedSea(t, store, firstSpells)); got != len(distinctSeaKeys(firstSpells)) {
		t.Errorf("the first passage has %d rows", got)
	}
}

// ── what the logbook shows ───────────────────────────────────────────────

func TestSeaWordsGiveTheDirectionRelativeToTheHeading(t *testing.T) {
	cases := []struct {
		heading, from float64
		want          string
	}{
		{0, 0, "ahead"},
		{0, 30, "ahead"},
		{0, 330, "ahead"},
		{0, 45, "ahead"},
		{0, 50, "on the starboard bow"},
		{0, 310, "on the port bow"},
		{0, 80, "on the starboard bow"},
		{0, 81, "on the starboard beam"},
		{0, 270, "on the port beam"},
		{0, 90, "on the starboard beam"},
		{0, 100, "on the starboard beam"},
		{0, 120, "on the starboard quarter"},
		{0, 135, "on the starboard quarter"},
		{0, 240, "on the port quarter"},
		{0, 136, "astern"},
		{0, 180, "astern"},
		{350, 355, "ahead"},
		{350, 10, "ahead"},
		{90, 0, "on the port beam"},
	}
	for _, c := range cases {
		if got := seaWords(&c.heading, c.from); got != c.want {
			t.Errorf("heading %v, from %v: %q, want %q", c.heading, c.from, got, c.want)
		}
	}
	if got := seaWords(nil, 270); got != "from 270° true, no heading recorded" {
		t.Errorf("no heading: %q", got)
	}
}

// Mate and the logbook describe one sea the same way: the logbook's words follow
// relativeAngleLabel's bands at every bearing.
func TestSeaWordsKeepToTheBandsMateUses(t *testing.T) {
	heading := 40.0
	for from := 0; from < 360; from++ {
		want := map[string]string{"head": "ahead", "bow": "bow", "beam": "beam", "quarter": "quarter", "following": "astern"}[relativeAngleLabel(relativeAngleDeg(heading, float64(from)))]
		if got := seaWords(&heading, float64(from)); !strings.Contains(got, want) {
			t.Errorf("from %d: logbook says %q, Mate's band wants %q", from, got, want)
		}
	}
}

func spellWithHeading(h *float64) feltSpell {
	lat, lon := -20.0, 149.0
	return feltSpell{
		feltSpellCore:   feltSpellCore{Start: seaNow.Unix(), End: seaNow.Unix() + 900},
		feltFitFeatures: feltFitFeatures{MeanHeadingDeg: h, MidLat: &lat, MidLon: &lon},
	}
}

func TestSeaForSpellShapesTheTrainsAndSaysWhyWhenThereIsNoSea(t *testing.T) {
	north := 0.0
	row := &seaHindcastRow{Model: "ncep_gfswave025",
		Swell:    &seaTrainFigures{HeightM: 0.8, PeriodS: 9, FromDeg: 270},
		WindWave: &seaTrainFigures{HeightM: 0.6, PeriodS: 4, FromDeg: 315}}
	got := seaForSpell(spellWithHeading(&north), row, nil)
	if got.Reason != "" || got.Swell == nil || got.WindWave == nil {
		t.Fatalf("%+v", got)
	}
	if got.Swell.HeightM != 0.8 || got.Swell.PeriodS != 9 || got.Swell.Direction != "on the port beam" {
		t.Errorf("swell %+v", got.Swell)
	}
	if got.WindWave.Direction != "ahead" { // 45 degrees off the bow is Mate's head band
		t.Errorf("wind wave %+v", got.WindWave)
	}

	// A spell stored with no position is never given a sea.
	noPos := spellWithHeading(&north)
	noPos.MidLat, noPos.MidLon = nil, nil
	if r := seaForSpell(noPos, row, nil).Reason; r != "This spell has no recorded position to look up the sea for." {
		t.Errorf("no position: %q", r)
	}

	flat := seaForSpell(spellWithHeading(&north), &seaHindcastRow{Model: "ncep_gfswave025"}, nil)
	if flat.Swell != nil || flat.WindWave != nil || flat.Reason != "" {
		t.Errorf("a flat sea: %+v", flat)
	}

	missing := seaForSpell(spellWithHeading(&north), &seaHindcastRow{MissingReason: "GFS-Wave has no value at this cell"}, nil)
	if missing.Reason != "GFS-Wave has no value at this cell." || missing.Swell != nil {
		t.Errorf("a missing hour: %+v", missing)
	}
	// Every reason the page shows ends in a full stop, whoever worded it.
	stopped := seaForSpell(spellWithHeading(&north), &seaHindcastRow{MissingReason: "Already has one."}, nil)
	if stopped.Reason != "Already has one." {
		t.Errorf("a reason with its stop: %q", stopped.Reason)
	}

	notYet := seaForSpell(spellWithHeading(&north), nil, nil)
	if !strings.Contains(notYet.Reason, "hasn't been fetched yet") {
		t.Errorf("no row yet: %+v", notYet)
	}

	unreadable := seaForSpell(spellWithHeading(&north), nil, errors.New("disk I/O error"))
	if !strings.Contains(unreadable.Reason, "couldn't be read") {
		t.Errorf("a failed read: %+v", unreadable)
	}
}

func spellOut(start, end int64, sea spellSea) feltSpellOut {
	return feltSpellOut{feltSpellCore: feltSpellCore{Start: start, End: end}, Sea: &sea}
}

func TestPassageSeaSummaryNamesTheLargestSwellAndWindWaveAndWhen(t *testing.T) {
	train := func(h, p float64) *seaTrain {
		return &seaTrain{seaTrainFigures: seaTrainFigures{HeightM: h, PeriodS: p}}
	}
	spells := []feltSpellOut{
		spellOut(1000, 1900, spellSea{Swell: train(0.8, 9), WindWave: train(0.3, 3)}),
		spellOut(2000, 2900, spellSea{Swell: train(1.4, 11), WindWave: train(0.9, 4)}),
		spellOut(3000, 3900, spellSea{Swell: train(1.1, 10)}),
		spellOut(4000, 4900, spellSea{Reason: "GFS-Wave has no value at this cell"}),
	}
	sum := seaSummaryOf(spells)
	if sum.Reason != "" || sum.Swell == nil || sum.WindWave == nil {
		t.Fatalf("%+v", sum)
	}
	if sum.Swell.HeightM != 1.4 || sum.Swell.PeriodS != 11 || sum.Swell.At != 2450 {
		t.Errorf("largest swell %+v", sum.Swell)
	}
	if sum.WindWave.HeightM != 0.9 || sum.WindWave.At != 2450 {
		t.Errorf("largest wind wave %+v", sum.WindWave)
	}
	// One spell of the four has no sea: the peaks do not cover the whole passage,
	// and the summary says so.
	if sum.Partial != "no sea for 1 of 4 spells" {
		t.Errorf("partial coverage %q", sum.Partial)
	}
}

// A passage whose spells all have a sea says nothing of coverage.
func TestPassageSeaSummaryHasNothingToSayOfCoverageWhenEverySpellHasASea(t *testing.T) {
	train := &seaTrain{seaTrainFigures: seaTrainFigures{HeightM: 0.8, PeriodS: 9}}
	sum := seaSummaryOf([]feltSpellOut{spellOut(1000, 1900, spellSea{Swell: train}), spellOut(2000, 2900, spellSea{})})
	if sum.Partial != "" || sum.Reason != "" {
		t.Errorf("%+v", sum)
	}
}

func TestPassageSeaSummaryGivesTheReasonWhenNoSpellHasASeaState(t *testing.T) {
	reason := spellSea{Reason: "GFS-Wave has no value at this cell"}
	sum := seaSummaryOf([]feltSpellOut{spellOut(1000, 1900, reason), spellOut(2000, 2900, reason)})
	if sum.Swell != nil || sum.WindWave != nil || sum.Reason != "No spell on this passage has a sea state; each spell says why." {
		t.Errorf("%+v", sum)
	}
	// The passage's own words, never a spell's "this hour".
	notYet := spellSea{Reason: seaReasonNotFetched}
	if r := seaSummaryOf([]feltSpellOut{spellOut(1000, 1900, notYet), spellOut(2000, 2900, notYet)}).Reason; r != "The sea state for this passage hasn't been fetched yet." {
		t.Errorf("not fetched: %q", r)
	}
	noPos := spellSea{Reason: seaReasonNoPosition}
	if r := seaSummaryOf([]feltSpellOut{spellOut(1000, 1900, noPos)}).Reason; r != "No spell on this passage has a recorded position to look up the sea for." {
		t.Errorf("no position: %q", r)
	}
	unreadable := spellSea{Reason: seaReasonUnreadable}
	if r := seaSummaryOf([]feltSpellOut{spellOut(1000, 1900, unreadable)}).Reason; r != "The recorded sea state for this passage couldn't be read." {
		t.Errorf("unreadable: %q", r)
	}
	if none := seaSummaryOf(nil); none.Reason != "" || none.Swell != nil {
		t.Errorf("a passage with no spells has nothing to say about the sea: %+v", none)
	}
	// A flat sea all through is no reason: the page then says there was no swell
	// or wind wave.
	flat := seaSummaryOf([]feltSpellOut{spellOut(1000, 1900, spellSea{}), spellOut(2000, 2900, spellSea{})})
	if flat.Reason != "" || flat.Swell != nil || flat.WindWave != nil || flat.Partial != "" {
		t.Errorf("a flat sea all through: %+v", flat)
	}
	// A flat spell beside ones with no sea state is not a flat passage: the page
	// must not say "no swell or wind wave" for spells that were never fetched.
	mixed := seaSummaryOf([]feltSpellOut{spellOut(1000, 1900, spellSea{}), spellOut(2000, 2900, reason), spellOut(3000, 3900, reason)})
	if mixed.Reason != "No sea state for 2 of 3 spells, and no swell or wind wave in the rest." {
		t.Errorf("a flat spell beside two without: %+v", mixed)
	}
}

func TestPassageDetailCarriesEachSpellsSeaAndTheSummary(t *testing.T) {
	store, rec, _ := seaPassage(t)
	prev := globalPassageStore
	globalPassageStore = store
	t.Cleanup(func() { globalPassageStore = prev })
	withHelmHeight(t, nil)

	// Before the job has run, each spell says so.
	code, body := getJSON(t, getPassageHandler, "/api/passages/x", rec.ID)
	if code != 200 {
		t.Fatal(code, body)
	}
	felt := body["passage"].(map[string]any)["felt"].(map[string]any)
	first := felt["spells"].([]any)[0].(map[string]any)
	sea := first["sea"].(map[string]any)
	if !strings.Contains(sea["reason"].(string), "hasn't been fetched yet") {
		t.Errorf("sea before the job: %+v", sea)
	}
	if s := felt["sea"].(map[string]any); s["reason"] == nil || s["reason"] == "" {
		t.Errorf("summary before the job: %+v", s)
	}

	runSeaHindcast(context.Background(), store, resolveTo(&fakeHindcast{}), seaNow)
	_, body = getJSON(t, getPassageHandler, "/api/passages/x", rec.ID)
	felt = body["passage"].(map[string]any)["felt"].(map[string]any)
	first = felt["spells"].([]any)[0].(map[string]any)
	sea = first["sea"].(map[string]any)
	swell := sea["swell"].(map[string]any)
	if swell["height_m"].(float64) != 0.8 || swell["period_s"].(float64) != 9 || swell["from_deg"].(float64) != 270 || swell["direction"] != "on the port beam" {
		t.Errorf("swell %+v", swell)
	}
	for _, gone := range []string{"flat", "model"} {
		if _, ok := sea[gone]; ok {
			t.Errorf("the wire carries %q, which nothing reads: %+v", gone, sea)
		}
	}
	summary := felt["sea"].(map[string]any)
	// No coastline is held in this test, so the swell was not judged for enclosed
	// water: it keeps its figure, says so, and is out of the passage's peak.
	if swell["unchecked"] != true || !strings.HasPrefix(swell["note"].(string), "not checked for enclosed water: ") {
		t.Errorf("swell %+v, want it labelled not checked", swell)
	}
	if summary["swell"] != nil || !strings.Contains(fmt.Sprint(summary["reason"], summary["partial"]), "swell not checked for enclosed water") {
		t.Errorf("summary %+v", summary)
	}
	if _, ok := summary["flat"]; ok {
		t.Errorf("the summary carries flat: %+v", summary)
	}
}

// ── review fixes ─────────────────────────────────────────────────────────

// addSeaPassage stores a second passage, with two spells, starting at the given
// hour after the epoch and a degree further south, and returns it with its spells.
func addSeaPassage(t *testing.T, store *passageStore, hour int) (passageRecord, []feltSpell) {
	t.Helper()
	b := newTrackBuilder()
	b.lat = -21.0
	b.t = passageEpoch.Add(time.Duration(hour) * time.Hour)
	b.run(time.Minute, time.Minute, 0)
	b.run(30*time.Minute, time.Minute, 6)
	setSway(b.pts, 1, 30, 1, 1)
	for i := range b.pts {
		b.pts[i].HeadingDeg = pfloat(0)
	}
	appendAll(t, store, b.pts)
	rec, err := store.InsertPassage(passageRecord{Start: b.pts[0].At, End: b.pts[len(b.pts)-1].At, Title: "later"})
	if err != nil {
		t.Fatal(err)
	}
	f, err := store.PassageFiguresFor(rec)
	if err != nil {
		t.Fatal(err)
	}
	return rec, f.Stats.FeltSpells
}

// stripSpellPositions rewrites a passage's stored stats as a version 4 row
// carried to version 5 without a recompute leaves them: spells with no position.
func stripSpellPositions(t *testing.T, store *passageStore, id string) {
	t.Helper()
	var raw string
	if err := store.db.QueryRow(`SELECT stats_json FROM passages WHERE id = ?`, id).Scan(&raw); err != nil {
		t.Fatal(err)
	}
	var doc map[string]any
	if err := json.Unmarshal([]byte(raw), &doc); err != nil {
		t.Fatal(err)
	}
	for _, s := range doc["stats"].(map[string]any)["felt_spells"].([]any) {
		delete(s.(map[string]any), "mid_lat")
		delete(s.(map[string]any), "mid_lon")
	}
	out, _ := json.Marshal(doc)
	if _, err := store.db.Exec(`UPDATE passages SET stats_json = ? WHERE id = ?`, string(out), id); err != nil {
		t.Fatal(err)
	}
}

func TestSeaHindcastNeverAsksAboutASpellWithNoPosition(t *testing.T) {
	store, rec, _ := seaPassage(t)
	logOf(t)
	stripSpellPositions(t, store, rec.ID)
	fake := &fakeHindcast{}
	sum := runSeaHindcast(context.Background(), store, resolveTo(fake), seaNow)
	if len(fake.calls) != 0 {
		t.Errorf("asked about %+v; a spell with no position has nowhere to ask", fake.calls[0].points)
	}
	if sum.Filled+sum.NoValue+sum.Failed != 0 || store.seaHindcastCount() != 0 {
		t.Errorf("summary %+v, rows %d", sum, store.seaHindcastCount())
	}
	f, err := store.PassageFiguresFor(rec)
	if err != nil {
		t.Fatal(err)
	}
	for _, sp := range f.Stats.FeltSpells {
		lookup := store.seaLookupFor(f.Stats.FeltSpells)
		if got := lookup.forSpell(sp).Reason; got != "This spell has no recorded position to look up the sea for." {
			t.Errorf("reason %q", got)
		}
	}
}

// failFiguresOf makes storing the figures of one passage fail, as a bad row would.
func failFiguresOf(t *testing.T, store *passageStore, id string) {
	t.Helper()
	if _, err := store.db.Exec(`UPDATE passages SET stats_json = NULL WHERE id = ?`, id); err != nil {
		t.Fatal(err)
	}
	if _, err := store.db.Exec(`CREATE TRIGGER fail_figures BEFORE UPDATE ON passages WHEN NEW.id = '` + id + `'
		BEGIN SELECT RAISE(ABORT, 'disk on fire'); END`); err != nil {
		t.Fatal(err)
	}
}

func TestSeaHindcastCarriesOnPastAPassageWhoseFiguresFail(t *testing.T) {
	store, bad, _ := seaPassage(t)
	buf := logOf(t)
	_, goodSpells := addSeaPassage(t, store, 11)
	failFiguresOf(t, store, bad.ID)
	fake := &fakeHindcast{}
	sum := runSeaHindcast(context.Background(), store, resolveTo(fake), seaNow)
	if want := len(distinctSeaKeys(goodSpells)); sum.Filled != want {
		t.Errorf("filled %d, want the other passage's %d: %+v", sum.Filled, want, sum)
	}
	if sum.PassagesFailed != 1 {
		t.Errorf("passages failed %d, want 1", sum.PassagesFailed)
	}
	out := buf.String()
	if !strings.Contains(out, "disk on fire") || !strings.Contains(out, "1 passage") {
		t.Errorf("the log should name the failed passage and say how many: %s", out)
	}
}
