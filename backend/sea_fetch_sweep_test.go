package main

import (
	"context"
	"encoding/json"
	"reflect"
	"strings"
	"testing"
	"time"
)

// ── the fetch figures the sea sweep stores and the page reads (ADR 0300) ─────

// fetchRig is a passage with two spells at the test position, a hindcast for
// them, and a coastline held around them: everything the sweep and the page
// need, with the shore given.
type fetchRig struct {
	t      *testing.T
	store  *passageStore
	rec    passageRecord
	lines  []shorelineLine
	reader *offlineCoastlineReader
}

func (r *fetchRig) coast() *offlineCoastlineReader { return r.reader }

// pinFigures rewrites each spell of a passage's stored figures, as the stats
// would have kept them at a position under a wind.
func pinFigures(t *testing.T, store *passageStore, id string, edit func(i int, spell map[string]any)) {
	t.Helper()
	var raw string
	if err := store.db.QueryRow(`SELECT stats_json FROM passages WHERE id = ?`, id).Scan(&raw); err != nil {
		t.Fatal(err)
	}
	var doc map[string]any
	if err := json.Unmarshal([]byte(raw), &doc); err != nil {
		t.Fatal(err)
	}
	for i, s := range doc["stats"].(map[string]any)["felt_spells"].([]any) {
		edit(i, s.(map[string]any))
	}
	out, _ := json.Marshal(doc)
	if _, err := store.db.Exec(`UPDATE passages SET stats_json = ? WHERE id = ?`, string(out), id); err != nil {
		t.Fatal(err)
	}
}

// windAt puts a spell at a position, heading north, under a true wind of kts from
// the direction given.
func windAt(lat, lon, kts, from float64) func(int, map[string]any) {
	return func(_ int, s map[string]any) {
		s["mid_lat"], s["mid_lon"], s["mean_heading_deg"] = lat, lon, 0.0
		s["wind_kept"], s["wind_kts"], s["wind_from_deg"] = true, kts, from
	}
}

// newFetchRig holds the coastline given round the test position, pins the
// passage's spells to it under a wind, and fills the hindcast.
func newFetchRig(t *testing.T, kts, from float64, lines ...shorelineLine) *fetchRig {
	t.Helper()
	store, rec, _ := seaPassage(t)
	pinFigures(t, store, rec.ID, windAt(fetchTestLat, fetchTestLon, kts, from))
	r := &fetchRig{t: t, store: store, rec: rec, lines: lines, reader: fetchReader(t, lines...)}
	r.hindcast()
	return r
}

func (r *fetchRig) hindcast() {
	r.t.Helper()
	runSeaHindcast(context.Background(), r.store, resolveTo(&fakeHindcast{}), seaNow)
}

func (r *fetchRig) sweep(budget int) seaFetchSummary {
	r.t.Helper()
	return runSeaFetchWithin(context.Background(), r.store, r.coast, budget)
}

const noBudgetLimit = 1 << 20

// view is the page's judge for the passage, as the handler builds it.
func (r *fetchRig) view() (*fetchLookup, passageStats) {
	r.t.Helper()
	f, err := r.store.PassageFiguresFor(r.rec)
	if err != nil {
		r.t.Fatal(err)
	}
	return newFetchView(r.reader, r.store, f.Stats.FeltSpells), f.Stats
}

// page is the felt block of the passage page.
func (r *fetchRig) page() passageFelt {
	r.t.Helper()
	view, stats := r.view()
	return buildPassageFelt(stats, passageSidewaysG{}, comfortBands{}, r.store.seaLookupFor(stats.FeltSpells), view)
}

// kinds counts the stored rows by kind of ray.
func (r *fetchRig) kinds() map[string]int {
	r.t.Helper()
	rows, err := r.store.db.Query(`SELECT kind, COUNT(*) FROM sea_fetch_rays GROUP BY kind`)
	if err != nil {
		r.t.Fatal(err)
	}
	defer rows.Close()
	out := map[string]int{}
	for rows.Next() {
		var k string
		var n int
		if err := rows.Scan(&k, &n); err != nil {
			r.t.Fatal(err)
		}
		out[k] = n
	}
	return out
}

func (r *fetchRig) rowCount() int {
	var n int
	if err := r.store.db.QueryRow(`SELECT COUNT(*) FROM sea_fetch_rays`).Scan(&n); err != nil {
		r.t.Fatal(err)
	}
	return n
}

func firstSea(t *testing.T, f passageFelt) spellSea {
	t.Helper()
	if len(f.Spells) == 0 || f.Spells[0].Sea == nil {
		t.Fatalf("no sea on the first spell: %+v", f.Spells)
	}
	return *f.Spells[0].Sea
}

// A neighbour of the test position's own square that the coastline must hold.
func neighbourSquare(t *testing.T) coverageSquare {
	t.Helper()
	needed, _ := squaresWithin(fetchTestLat, fetchTestLon, shelterMaxFetchNm)
	own, _ := squareContaining(fetchTestLat, fetchTestLon)
	for _, sq := range needed {
		if sq != own && !squareInland(sq) {
			return sq
		}
	}
	t.Fatal("no neighbouring square within reach")
	return coverageSquare{}
}

// 1. What the sweep stores is what the shore says live.
func TestTheSweepThenThePageSayWhatTheShoreSaysLive(t *testing.T) {
	for name, line := range map[string]shorelineLine{"land two miles east": landEastOf(2), "land two miles west": landWestOf(2)} {
		rig := newFetchRig(t, 20, 90, line)
		sum := rig.sweep(noBudgetLimit)
		if sum.Built != 1 || sum.Rows != 2 || sum.Skipped != "" {
			t.Fatalf("%s: summary %+v, want one position and a wind row and a swell row", name, sum)
		}
		page := firstSea(t, rig.page())
		f, err := rig.store.PassageFiguresFor(rig.rec)
		if err != nil {
			t.Fatal(err)
		}
		sp := f.Stats.FeltSpells[0]
		live := judged(sp, &seaTrainFigures{HeightM: 0.8, PeriodS: 9, FromDeg: 270}, &seaTrainFigures{HeightM: 0.6, PeriodS: 4, FromDeg: 315}, rig.reader)
		if !reflect.DeepEqual(page.WindWave, live.WindWave) || !reflect.DeepEqual(page.Swell, live.Swell) {
			t.Errorf("%s: page %+v / %+v, live %+v / %+v", name, page.WindWave, page.Swell, live.WindWave, live.Swell)
		}
	}
	east := firstSea(t, func() passageFelt {
		r := newFetchRig(t, 20, 90, landEastOf(2))
		r.sweep(noBudgetLimit)
		return r.page()
	}())
	if east.WindWave.Fetch.Check != seaCheckCapped {
		t.Errorf("land two miles upwind caps: %+v", east.WindWave)
	}
	west := firstSea(t, func() passageFelt {
		r := newFetchRig(t, 20, 90, landWestOf(2))
		r.sweep(noBudgetLimit)
		return r.page()
	}())
	if west.WindWave.Fetch.Check != seaCheckOpen {
		t.Errorf("land two miles downwind leaves it open: %+v", west.WindWave)
	}
}

// 2. Until the sweep has been, both notes say so, and the spell is out of the peaks.
func TestASpellTheSweepHasNotWorkedOutSaysSoAndStaysOutOfThePeaks(t *testing.T) {
	rig := newFetchRig(t, 20, 90, landEastOf(2))
	felt := rig.page()
	sea := firstSea(t, felt)
	if sea.WindWave.Note != "not checked against fetch: not worked out yet" || sea.WindWave.Fetch.Check != seaCheckUnchecked || sea.WindWave.HeightM != 0.6 {
		t.Errorf("wind wave %+v", sea.WindWave)
	}
	if sea.Swell.Note != "not checked for enclosed water: not worked out yet" || !sea.Swell.Unchecked {
		t.Errorf("swell %+v", sea.Swell)
	}
	if felt.Sea.WindWave != nil || felt.Sea.Swell != nil {
		t.Errorf("peaks %+v %+v, want none", felt.Sea.WindWave, felt.Sea.Swell)
	}
	if !strings.Contains(felt.Sea.Reason, "not checked against fetch for 2 of 2 spells") || !strings.Contains(felt.Sea.Reason, "not checked for enclosed water for 2 of 2 spells") {
		t.Errorf("the passage says how many were not checked: %q", felt.Sea.Reason)
	}
}

// 3. The page never reads a line of coastline.
func TestBuildingThePageNeverReadsTheCoastlineGeometry(t *testing.T) {
	rig := newFetchRig(t, 20, 90, landEastOf(2))
	rig.sweep(noBudgetLimit)
	// With every square's geometry spoiled, a page that read it would fail; the
	// stored rows do not need it.
	if _, err := rig.reader.Store.db.Exec(`UPDATE coastline_squares SET geometry = X'00'`); err != nil {
		t.Fatal(err)
	}
	view, stats := rig.view()
	felt := buildPassageFelt(stats, passageSidewaysG{}, comfortBands{}, rig.store.seaLookupFor(stats.FeltSpells), view)
	if w := firstSea(t, felt).WindWave; w.Fetch.Check != seaCheckCapped {
		t.Errorf("the page reads the stored rows: %+v", w)
	}
	if len(view.loader.lines) != 0 {
		t.Errorf("the page read %d squares' lines", len(view.loader.lines))
	}
}

// 4. A square downloaded again is a new state of the coastline.
func TestASquareDownloadedAgainIsNotWorkedOutUntilTheNextSweep(t *testing.T) {
	rig := newFetchRig(t, 20, 90, landEastOf(2))
	rig.sweep(noBudgetLimit)
	own, _ := squareContaining(fetchTestLat, fetchTestLon)
	if err := rig.reader.Store.SaveSquareAsOf(own, rig.lines, time.Now().Add(2*time.Hour), coastlineTestDataAsOf); err != nil {
		t.Fatal(err)
	}
	if n := firstSea(t, rig.page()).WindWave.Note; n != "not checked against fetch: not worked out yet" {
		t.Errorf("after the square came again: %q", n)
	}
	if sum := rig.sweep(noBudgetLimit); sum.Built != 1 {
		t.Errorf("the next sweep builds the position again: %+v", sum)
	}
	if w := firstSea(t, rig.page()).WindWave; w.Fetch.Check != seaCheckCapped {
		t.Errorf("then it is checked: %+v", w)
	}
}

// 5. A square taken away is named; one added is worked out by the next sweep.
func TestARemovedSquareIsNamedAndAnAddedOneIsWorkedOutNextSweep(t *testing.T) {
	rig := newFetchRig(t, 20, 90, landEastOf(2))
	sq := neighbourSquare(t)
	if _, err := rig.reader.Store.db.Exec(`DELETE FROM coastline_squares WHERE lat = ? AND lon = ?`, sq.Lat, sq.Lon); err != nil {
		t.Fatal(err)
	}
	if sum := rig.sweep(noBudgetLimit); sum.Built != 0 || rig.rowCount() != 0 {
		t.Errorf("with a square missing nothing is worked out: %+v, %d rows", sum, rig.rowCount())
	}
	if n := firstSea(t, rig.page()).WindWave.Note; !strings.Contains(n, sq.Label()) || !strings.Contains(n, "outside coverage") {
		t.Errorf("the page names the missing square: %q", n)
	}
	if err := rig.reader.Store.SaveSquareAsOf(sq, nil, time.Now(), coastlineTestDataAsOf); err != nil {
		t.Fatal(err)
	}
	if n := firstSea(t, rig.page()).WindWave.Note; n != "not checked against fetch: not worked out yet" {
		t.Errorf("once the square is held: %q", n)
	}
	rig.sweep(noBudgetLimit)
	if w := firstSea(t, rig.page()).WindWave; w.Fetch.Check != seaCheckCapped {
		t.Errorf("then it is checked: %+v", w)
	}
	// Taken away again, the page names it though the rows are there.
	if _, err := rig.reader.Store.db.Exec(`DELETE FROM coastline_squares WHERE lat = ? AND lon = ?`, sq.Lat, sq.Lon); err != nil {
		t.Fatal(err)
	}
	if n := firstSea(t, rig.page()).WindWave.Note; !strings.Contains(n, sq.Label()) {
		t.Errorf("a square removed after the sweep: %q", n)
	}
}

// 6. The page says the coastline is off whatever is stored; a sweep with it off stores nothing.
func TestCoastlineOffStoresNothingAndThePageSaysSoEvenWithRows(t *testing.T) {
	rig := newFetchRig(t, 20, 90, landEastOf(2))
	rig.sweep(noBudgetLimit)
	rig.reader.Enabled = func() (bool, error) { return false, nil }
	if n := firstSea(t, rig.page()).WindWave.Note; !strings.Contains(n, "the Offline coastline is off") {
		t.Errorf("page with stored rows and the coastline off: %q", n)
	}

	off := newFetchRig(t, 20, 90, landEastOf(2))
	off.reader.Enabled = func() (bool, error) { return false, nil }
	buf := logOf(t)
	sum := off.sweep(noBudgetLimit)
	if off.rowCount() != 0 || sum.Built != 0 || !strings.Contains(sum.Skipped, "off") {
		t.Errorf("sweep with the coastline off: %+v, %d rows", sum, off.rowCount())
	}
	if got := strings.Count(buf.String(), "sea fetch skipped"); got != 1 {
		t.Errorf("%d skip lines, want one: %s", got, buf.String())
	}
}

// 7. A new wind direction is new work; a new wind speed is not.
func TestANewWindDirectionIsNewGeometryAndANewSpeedIsNot(t *testing.T) {
	rig := newFetchRig(t, 20, 90, landEastOf(2))
	rig.sweep(noBudgetLimit)
	if rig.rowCount() != 2 {
		t.Fatalf("%d rows, want a wind row and a swell row", rig.rowCount())
	}
	before := *firstSea(t, rig.page()).WindWave.Fetch.CapHeightM

	pinFigures(t, rig.store, rig.rec.ID, windAt(fetchTestLat, fetchTestLon, 20, 95))
	if sum := rig.sweep(noBudgetLimit); sum.Built != 1 || sum.Rows != 1 || rig.rowCount() != 3 {
		t.Errorf("a wind from 095 needs its own row: %+v, %d rows", sum, rig.rowCount())
	}

	pinFigures(t, rig.store, rig.rec.ID, windAt(fetchTestLat, fetchTestLon, 12, 95))
	if sum := rig.sweep(noBudgetLimit); sum.Built != 0 || sum.Rows != 0 || rig.rowCount() != 3 {
		t.Errorf("a different speed needs no new geometry: %+v, %d rows", sum, rig.rowCount())
	}
	w := firstSea(t, rig.page()).WindWave
	if w.Fetch.WindKts == nil || *w.Fetch.WindKts != 12 || *w.Fetch.CapHeightM >= before {
		t.Errorf("the cap is worked out again from the new speed: %+v (was %.3f m)", w.Fetch, before)
	}
}

// 8. A calm takes the one calm row, and an open ray is stored open, never as 15 nm.
func TestACalmSpellUsesTheCalmRowAndAnOpenRayIsStoredOpen(t *testing.T) {
	shut := newFetchRig(t, 0.5, 90, waterRing(2))
	shut.sweep(noBudgetLimit)
	if got := shut.kinds(); got[fetchKindCalm] != 1 || got[fetchKindWind] != 0 {
		t.Errorf("rows by kind %v, want one calm row", got)
	}
	if w := firstSea(t, shut.page()).WindWave; w.Fetch.Check != seaCheckCapped || !w.Fetch.Calm || !strings.Contains(w.Note, "in a calm") {
		t.Errorf("a calm in a ring of land: %+v", w)
	}

	open := newFetchRig(t, 0.5, 90, landEastOf(2))
	open.sweep(noBudgetLimit)
	var isOpen bool
	var noDist bool
	if err := open.store.db.QueryRow(`SELECT open, dist_m IS NULL FROM sea_fetch_rays WHERE kind = 'calm'`).Scan(&isOpen, &noDist); err != nil {
		t.Fatal(err)
	}
	if !isOpen || !noDist {
		t.Errorf("open stored as %v with no distance %v; an open ray is never 15 nm", isOpen, noDist)
	}
	if w := firstSea(t, open.page()).WindWave; w.Fetch.Check != seaCheckOpen || w.Note != "open water nearby" {
		t.Errorf("a calm with an open bearing: %+v", w)
	}
}

// 9. A hindcast hour fetched this pass has its shore worked out this pass, and a
// spell with no hindcast has nothing worked out.
func TestAHindcastHourFetchedThisPassIsWorkedOutThisPass(t *testing.T) {
	store, rec, _ := seaPassage(t)
	pinFigures(t, store, rec.ID, windAt(fetchTestLat, fetchTestLon, 20, 90))
	rig := &fetchRig{t: t, store: store, rec: rec, lines: []shorelineLine{landEastOf(2)}}
	rig.reader = fetchReader(t, rig.lines...)
	runSeaPass(context.Background(), store, resolveTo(&fakeHindcast{}), rig.coast, seaNow)
	if got := rig.kinds(); got[fetchKindWind] != 1 || got[fetchKindSwell] != 1 {
		t.Errorf("rows after one pass: %v", got)
	}

	none, noRec, _ := seaPassage(t)
	pinFigures(t, none, noRec.ID, windAt(fetchTestLat, fetchTestLon, 20, 90))
	logOf(t)
	broken := &fetchRig{t: t, store: none, rec: noRec, reader: rig.reader}
	runSeaHindcast(context.Background(), none, resolveTo(&fakeHindcast{err: context.DeadlineExceeded}), seaNow)
	if sum := broken.sweep(noBudgetLimit); sum.Built != 0 || broken.rowCount() != 0 {
		t.Errorf("a spell with no hindcast row is not worked out: %+v", sum)
	}
}

// 10. The sweep builds so many geometries at a time, and stops when asked.
func TestTheSweepStopsAtItsBudgetAndTheNextOneFinishes(t *testing.T) {
	rig := newFetchRig(t, 20, 90, landEastOf(2))
	second, _ := addSeaPassage(t, rig.store, 11)
	at := func(i int) func(int, map[string]any) {
		return func(j int, s map[string]any) {
			windAt(fetchTestLat+0.02*float64(i+j), fetchTestLon, 20, 90)(j, s)
		}
	}
	pinFigures(t, rig.store, rig.rec.ID, at(0))
	pinFigures(t, rig.store, second.ID, at(2))
	rig.hindcast()
	logOf(t)

	first := rig.sweep(2)
	if first.Built != 2 || first.Stopped != seaStopBudget || first.Rows != 4 {
		t.Fatalf("first sweep %+v, want two positions built and the budget reached", first)
	}
	next := rig.sweep(2)
	if next.Built != 2 || next.Stopped != seaRanOn || next.Rows != 4 {
		t.Fatalf("next sweep %+v, want the other two", next)
	}
	if again := rig.sweep(2); again.Built != 0 || again.Rows != 0 {
		t.Errorf("a third sweep has nothing to do: %+v", again)
	}

	fresh := newFetchRig(t, 20, 90, landEastOf(2))
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if sum := runSeaFetchWithin(ctx, fresh.store, fresh.coast, noBudgetLimit); sum.Built != 0 || fresh.rowCount() != 0 || sum.Stopped != seaStopped {
		t.Errorf("a cancelled sweep: %+v, %d rows", sum, fresh.rowCount())
	}
}

// 11. Passages over one position share their rows and the squares are read once.
func TestPassagesAtOnePositionBuildItOnceAndReadEachSquareOnce(t *testing.T) {
	rig := newFetchRig(t, 20, 90, landEastOf(2))
	second, _ := addSeaPassage(t, rig.store, 11)
	pinFigures(t, rig.store, second.ID, windAt(fetchTestLat, fetchTestLon, 20, 90))
	rig.hindcast()
	sum := rig.sweep(noBudgetLimit)
	if sum.Built != 1 {
		t.Errorf("one position under two passages: %+v, want one geometry", sum)
	}
	needed, _ := squaresWithin(fetchTestLat, fetchTestLon, shelterMaxFetchNm)
	held := 0
	for _, sq := range needed {
		if !squareInland(sq) {
			held++
		}
	}
	if sum.Squares != held {
		t.Errorf("read %d squares, want each of the %d held once", sum.Squares, held)
	}
	if again := rig.sweep(noBudgetLimit); again.Built != 0 || again.Squares != 0 {
		t.Errorf("rows already stored cost nothing: %+v", again)
	}
}

// 12. A position ashore is stored so, and spells with no usable wind get no wind row.
func TestAPositionAshoreIsStoredAndSpellsWithoutWindGetNoWindWork(t *testing.T) {
	ashore := newFetchRig(t, 15, 90, landRing(0.5))
	ashore.sweep(noBudgetLimit)
	sea := firstSea(t, ashore.page())
	if sea.WindWave.Note != "not checked against fetch: the position is charted ashore" || sea.Swell.Note != "not checked for enclosed water: the position is charted ashore" {
		t.Errorf("notes %q / %q", sea.WindWave.Note, sea.Swell.Note)
	}

	for name, edit := range map[string]func(int, map[string]any){
		"stored before wind was kept": func(_ int, s map[string]any) {
			delete(s, "wind_kept")
			delete(s, "wind_kts")
			delete(s, "wind_from_deg")
		},
		"no true wind":     func(_ int, s map[string]any) { delete(s, "wind_kts"); delete(s, "wind_from_deg") },
		"direction varied": func(_ int, s map[string]any) { delete(s, "wind_from_deg"); s["wind_dir_missing"] = windDirVaried },
	} {
		store, rec, _ := seaPassage(t)
		pinFigures(t, store, rec.ID, func(i int, s map[string]any) {
			windAt(fetchTestLat, fetchTestLon, 20, 90)(i, s)
			edit(i, s)
		})
		rig := &fetchRig{t: t, store: store, rec: rec, reader: fetchReader(t, landEastOf(2))}
		rig.hindcast()
		rig.sweep(noBudgetLimit)
		if got := rig.kinds(); got[fetchKindWind] != 0 || got[fetchKindCalm] != 0 || got[fetchKindSwell] != 1 {
			t.Errorf("%s: rows by kind %v, want the swell's alone", name, got)
		}
	}
}

// The page asks the coastline's switch and plans each position once.
func TestThePageChecksTheSwitchAndPlansEachPositionOnce(t *testing.T) {
	rig := newFetchRig(t, 20, 90, landEastOf(2))
	rig.sweep(noBudgetLimit)
	reads := 0
	enabled := rig.reader.Enabled
	rig.reader.Enabled = func() (bool, error) { reads++; return enabled() }
	view, stats := rig.view()
	buildPassageFelt(stats, passageSidewaysG{}, comfortBands{}, rig.store.seaLookupFor(stats.FeltSpells), view)
	if reads != 1 || len(view.loader.plans) != 1 {
		t.Errorf("switch read %d times and %d positions planned, want one each", reads, len(view.loader.plans))
	}
}

// A passage whose hindcast rows cannot be read is logged and counted, not skipped
// in silence.
func TestAPassageWithUnreadableHindcastRowsIsCountedAndLogged(t *testing.T) {
	rig := newFetchRig(t, 20, 90, landEastOf(2))
	if _, err := rig.store.db.Exec(`DROP TABLE sea_hindcast_hours`); err != nil {
		t.Fatal(err)
	}
	buf := logOf(t)
	sum := rig.sweep(noBudgetLimit)
	if sum.PassagesFailed != 1 {
		t.Errorf("summary %+v, want the passage counted as failed", sum)
	}
	if !strings.Contains(buf.String(), rig.rec.ID) || !strings.Contains(buf.String(), "1 passages left out") {
		t.Errorf("log %q, want the passage named and the summary line to count it", buf.String())
	}
}

// A shore that cannot be read fails once for its position, not once for each spell
// there.
func TestAFailedShoreReadIsCountedOncePerPosition(t *testing.T) {
	rig := newFetchRig(t, 20, 90, landEastOf(2))
	if _, err := rig.reader.Store.db.Exec(`UPDATE coastline_squares SET geometry = X'00'`); err != nil {
		t.Fatal(err)
	}
	buf := logOf(t)
	sum := rig.sweep(noBudgetLimit)
	if sum.Failed != 1 || sum.Built != 0 {
		t.Errorf("summary %+v, want one failed position over both spells", sum)
	}
	if !strings.Contains(buf.String(), "1 positions failed") {
		t.Errorf("log %q, want one failure", buf.String())
	}
}
