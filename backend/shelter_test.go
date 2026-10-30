package main

import (
	"compress/gzip"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	extism "github.com/extism/go-sdk"
)

// Fixtures are raw Overpass `out geom` captures of natural=coastline and
// natural=reef, taken from the real server and cropped to 16 nm around the
// anchor (testdata/shelter). The loader below does what the osm-overpass
// plugin's shoreline_near does on the far side of the plugin boundary: turn
// elements into lines.

type overpassGeomFixture struct {
	Elements []struct {
		Type    string                       `json:"type"`
		ID      int64                        `json:"id"`
		Tags    map[string]string            `json:"tags"`
		Geom    []struct{ Lat, Lon float64 } `json:"geometry"`
		Members []struct {
			Role string                       `json:"role"`
			Geom []struct{ Lat, Lon float64 } `json:"geometry"`
		} `json:"members"`
	} `json:"elements"`
}

func loadShorelineFixture(t *testing.T, name string) []shorelineLine {
	t.Helper()
	return loadShorelineFile(t, "testdata/shelter/"+name+".json.gz")
}

func loadShorelineFile(t *testing.T, path string) []shorelineLine {
	t.Helper()
	f, err := os.Open(path)
	if err != nil {
		t.Fatalf("open fixture: %v", err)
	}
	defer f.Close()
	zr, err := gzip.NewReader(f)
	if err != nil {
		t.Fatalf("gunzip fixture: %v", err)
	}
	var raw overpassGeomFixture
	if err := json.NewDecoder(zr).Decode(&raw); err != nil {
		t.Fatalf("decode fixture: %v", err)
	}
	var lines []shorelineLine
	for _, e := range raw.Elements {
		kind := e.Tags["natural"]
		add := func(id string, geom []struct{ Lat, Lon float64 }) {
			if len(geom) < 2 {
				return
			}
			pts := make([]shorelinePoint, len(geom))
			for i, p := range geom {
				pts[i] = shorelinePoint{Lat: p.Lat, Lon: p.Lon}
			}
			lines = append(lines, shorelineLine{
				ID: id, Kind: kind, Name: e.Tags["name"],
				Closed: pts[0] == pts[len(pts)-1], Points: pts,
			})
		}
		if e.Type == "way" {
			add(fmt.Sprintf("way/%d", e.ID), e.Geom)
			continue
		}
		for i, m := range e.Members {
			if m.Role == "outer" {
				add(fmt.Sprintf("relation/%d/%d", e.ID, i), m.Geom)
			}
		}
	}
	return lines
}

const (
	stonehavenLat, stonehavenLon = -20.097633, 148.902533
	faireyLat, faireyLon         = -19.5434, 148.939367
)

func bearingRow(t *testing.T, est shelterEstimate, deg int) shelterBearing {
	t.Helper()
	for _, b := range est.Bearings {
		if b.BearingDeg == deg {
			return b
		}
	}
	t.Fatalf("no row for %d", deg)
	return shelterBearing{}
}

func TestEstimateShelter_StonehavenArcs(t *testing.T) {
	est := estimateShelter(stonehavenLat, stonehavenLon, loadShorelineFixture(t, "stonehaven"))

	if est.InsideReef {
		t.Fatalf("Stonehaven is not inside a reef")
	}
	if len(est.Bearings) != 36 {
		t.Fatalf("want 36 bearings, got %d", len(est.Bearings))
	}

	// Sheltered 030..190 (noforeignland's crowd data says about 020..155; the
	// estimate overshoots the south end - the known limitation in ADR 0203).
	for d := 30; d <= 190; d += 10 {
		if g := bearingRow(t, est, d).Grade; g != shelterSheltered {
			t.Errorf("%03d: grade %q, want sheltered", d, g)
		}
	}
	for d := 230; d <= 330; d += 10 {
		if g := bearingRow(t, est, d).Grade; g != shelterExposed {
			t.Errorf("%03d: grade %q, want exposed", d, g)
		}
	}
	// Hayman Island 2.2-2.6 nm to the NNW/N, and the shoulder south of the
	// sheltered arc.
	for _, d := range []int{340, 350, 0, 10, 20, 200, 210, 220} {
		if g := bearingRow(t, est, d).Grade; g != shelterPartial {
			t.Errorf("%03d: grade %q, want partial", d, g)
		}
	}

	// Without the window, north (1 nm of land at 000) would read sheltered
	// even though 330 is open water; the window is what stops that.
	if b := bearingRow(t, est, 0); b.FetchNm > 1.5 {
		t.Errorf("000 own fetch %.2f nm, want <= 1.5 (the case the window exists for)", b.FetchNm)
	}

	// Open water beyond the query radius is reported open.
	if b := bearingRow(t, est, 300); !b.Open || b.FetchNm != shelterMaxFetchNm {
		t.Errorf("300: %+v, want open at %v nm", b, shelterMaxFetchNm)
	}
	// Named blockers come through.
	if b := bearingRow(t, est, 350); !strings.Contains(b.Blocker, "Hayman Island") {
		t.Errorf("350 blocker %q, want Hayman Island", b.Blocker)
	}

	want := []struct {
		grade    string
		from, to int
	}{
		{shelterSheltered, 30, 190},
		{shelterPartial, 200, 220},
		{shelterExposed, 230, 330},
		{shelterPartial, 340, 20},
	}
	if len(est.Arcs) != len(want) {
		t.Fatalf("arcs: %+v", est.Arcs)
	}
	for i, w := range want {
		a := est.Arcs[i]
		if a.Grade != w.grade || a.FromDeg != w.from || a.ToDeg != w.to {
			t.Errorf("arc %d: %+v, want %s %d-%d", i, a, w.grade, w.from, w.to)
		}
	}
	if !strings.Contains(est.Summary, "Sheltered from NNE through S (030°–190°)") {
		t.Errorf("summary %q", est.Summary)
	}
}

func TestEstimateShelter_FaireyAnchorInsideReef(t *testing.T) {
	est := estimateShelter(faireyLat, faireyLon, loadShorelineFixture(t, "fairey"))

	if !est.InsideReef {
		t.Fatalf("anchor lies inside reef way 300537195")
	}
	if est.ReefID != "way/300537195" {
		t.Errorf("reef id %q", est.ReefID)
	}
	for _, b := range est.Bearings {
		if b.Grade != shelterReef {
			t.Errorf("%03d: grade %q, want reef", b.BearingDeg, b.Grade)
		}
		if b.ReefWidthNm <= 0 || b.ReefWidthNm != b.FetchNm {
			t.Errorf("%03d: reef width %v fetch %v", b.BearingDeg, b.ReefWidthNm, b.FetchNm)
		}
	}
	w := bearingRow(t, est, 280).ReefWidthNm
	if w < 0.3 || w > 0.45 {
		t.Errorf("west reef width %.2f nm, want about 0.37", w)
	}
	for _, d := range []int{100, 120, 150, 180} {
		if wd := bearingRow(t, est, d).ReefWidthNm; wd < 0.9 || wd > 1.8 {
			t.Errorf("%03d reef width %.2f nm, want 0.9-1.8", d, wd)
		}
	}
	if len(est.Arcs) != 1 || est.Arcs[0].Grade != shelterReef || est.Arcs[0].FromDeg != 0 || est.Arcs[0].ToDeg != 350 {
		t.Errorf("arcs %+v, want one all-round reef arc", est.Arcs)
	}
	if !strings.Contains(strings.ToLower(est.Summary), "tide") {
		t.Errorf("summary must say reef shelter depends on the tide: %q", est.Summary)
	}
}

func TestEstimateShelter_OpenFragmentsAreNotContainment(t *testing.T) {
	// A coastline way is an open fragment; the anchor sitting "between" its
	// ends is not inside anything.
	lines := []shorelineLine{{
		ID: "way/1", Kind: "coastline", Closed: false,
		Points: []shorelinePoint{{-20.0, 149.0}, {-20.0, 149.05}, {-20.05, 149.05}, {-20.05, 149.0}},
	}}
	est := estimateShelter(-20.025, 149.025, lines)
	if est.InsideReef {
		t.Errorf("an open coastline fragment must not count as a reef ring")
	}
}

func TestEstimateShelter_ClosedCoastlineIsNotAReef(t *testing.T) {
	// Even a closed coastline ring (an island) is land, never "reef shelter".
	ring := []shorelinePoint{{-20.0, 149.0}, {-20.0, 149.05}, {-20.05, 149.05}, {-20.05, 149.0}, {-20.0, 149.0}}
	est := estimateShelter(-20.025, 149.025, []shorelineLine{{ID: "way/2", Kind: "coastline", Closed: true, Points: ring}})
	if est.InsideReef {
		t.Errorf("closed coastline must not trigger the reef case")
	}
}

func TestEstimateShelter_NoShorelineIsAllExposed(t *testing.T) {
	est := estimateShelter(-20.0, 149.0, nil)
	if len(est.Arcs) != 1 || est.Arcs[0].Grade != shelterExposed {
		t.Errorf("arcs %+v", est.Arcs)
	}
	if !strings.Contains(est.Summary, "all round") {
		t.Errorf("summary %q", est.Summary)
	}
}

// ── tool over stored coverage squares ───────────────────────────────────

type shelterWorld struct {
	store   *coastlineStore
	enabled bool
	fix     bool
	boatLat float64
	boatLon float64
}

func newShelterWorld(t *testing.T) *shelterWorld {
	t.Helper()
	return &shelterWorld{store: newTestCoastlineStore(t), enabled: true, fix: true, boatLat: stonehavenLat, boatLon: stonehavenLon}
}

// hold stores lines in home and empty (open sea) squares in others, the way
// a fixture cropped around one anchorage lands in the squares around it.
func (w *shelterWorld) hold(t *testing.T, lines []shorelineLine, home coverageSquare, others ...coverageSquare) {
	t.Helper()
	if err := w.store.SaveSquare(home, lines, coastlineTestAt); err != nil {
		t.Fatal(err)
	}
	for _, sq := range others {
		if err := w.store.SaveSquare(sq, nil, coastlineTestAt.Add(24*time.Hour)); err != nil {
			t.Fatal(err)
		}
	}
}

func (w *shelterWorld) deps() assistantToolDeps {
	return assistantToolDeps{
		offlineCoastline: func() *offlineCoastlineReader {
			return &offlineCoastlineReader{
				Enabled: func() (bool, error) { return w.enabled, nil },
				Store:   w.store,
				Boat:    func() (float64, float64, bool) { return w.boatLat, w.boatLon, w.fix },
			}
		},
	}
}

func (w *shelterWorld) run(args string) (string, error) {
	return w.deps().execute(context.Background(), "anchorage_shelter", json.RawMessage(args))
}

var (
	stonehavenSquares = []coverageSquare{{-21, 149}, {-20, 148}, {-20, 149}}
	faireySquares     = []coverageSquare{{-20, 149}}
)

func TestAnchorageShelterTool_StonehavenFromStoredSquaresMatchesTheDirectEstimate(t *testing.T) {
	lines := loadShorelineFixture(t, "stonehaven")
	w := newShelterWorld(t)
	w.hold(t, lines, coverageSquare{-21, 148}, stonehavenSquares...)

	raw, err := w.run(`{"lat":-20.097633,"lon":148.902533}`)
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if !strings.Contains(raw, `"reading":"From the Offline coastline downloaded `) {
		t.Errorf("anchorage_shelter should say how old the shoreline is, got %s", raw)
	}
	var out struct {
		Summary             string           `json:"summary"`
		Source              string           `json:"source"`
		CoastlineDownloaded string           `json:"coastline_downloaded"`
		Arcs                []shelterArc     `json:"arcs"`
		Bearings            []shelterBearing `json:"bearings"`
		Caveat              string           `json:"caveat"`
	}
	if err := json.Unmarshal([]byte(raw), &out); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(out.Bearings) != 36 || len(out.Arcs) == 0 || out.Summary == "" {
		t.Fatalf("incomplete result: %s", raw)
	}
	for _, want := range []string{"wind bending", "valleys", "swell", "forecast"} {
		if !strings.Contains(out.Caveat, want) {
			t.Errorf("caveat misses %q: %q", want, out.Caveat)
		}
	}
	if len(raw) > assistantMaxToolResultChars {
		t.Errorf("result %d chars exceeds the tool cap", len(raw))
	}
	if !strings.Contains(out.Source, "Offline coastline") || out.CoastlineDownloaded != "2026-10-03" {
		t.Errorf("source %q downloaded %q: the oldest square's date is what Mate reports", out.Source, out.CoastlineDownloaded)
	}

	direct := estimateShelter(stonehavenLat, stonehavenLon, lines)
	if out.Summary != direct.Summary || len(out.Arcs) != len(direct.Arcs) {
		t.Errorf("stored squares changed the answer:\n tool   %s\n direct %s", out.Summary, direct.Summary)
	}
	for i, b := range out.Bearings {
		if b != direct.Bearings[i] {
			t.Fatalf("bearing %d differs: %+v vs %+v", b.BearingDeg, b, direct.Bearings[i])
		}
	}
}

func TestAnchorageShelterTool_FaireyReefFromStoredSquaresMatchesTheDirectEstimate(t *testing.T) {
	lines := loadShorelineFixture(t, "fairey")
	w := newShelterWorld(t)
	w.boatLat, w.boatLon = faireyLat, faireyLon
	w.hold(t, lines, coverageSquare{-20, 148}, faireySquares...)

	raw, err := w.run(`{"lat":-19.5434,"lon":148.939367}`)
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	var out struct {
		InsideReef bool             `json:"inside_reef"`
		Summary    string           `json:"summary"`
		Bearings   []shelterBearing `json:"bearings"`
	}
	if err := json.Unmarshal([]byte(raw), &out); err != nil {
		t.Fatal(err)
	}
	direct := estimateShelter(faireyLat, faireyLon, lines)
	if !out.InsideReef || out.Summary != direct.Summary {
		t.Errorf("inside_reef %v, summary %q vs %q", out.InsideReef, out.Summary, direct.Summary)
	}
	for _, d := range []int{100, 120, 150, 180} {
		got, want := bearingRow(t, shelterEstimate{Bearings: out.Bearings}, d), bearingRow(t, direct, d)
		if got != want {
			t.Errorf("%03d: %+v vs %+v", d, got, want)
		}
	}
}

func TestAnchorageShelterTool_ReefSpanningTwoSquaresIsOneRing(t *testing.T) {
	// Fairey's reef ring split across two squares by the 149°E meridian, held
	// whole in both as the plugin returns it: the answer must not change.
	lines := loadShorelineFixture(t, "fairey")
	w := newShelterWorld(t)
	w.boatLat, w.boatLon = faireyLat, faireyLon
	w.hold(t, lines, coverageSquare{-20, 148})
	if err := w.store.SaveSquare(coverageSquare{-20, 149}, lines, coastlineTestAt); err != nil {
		t.Fatal(err)
	}
	raw, err := w.run(`{"lat":-19.5434,"lon":148.939367}`)
	if err != nil {
		t.Fatal(err)
	}
	var out struct {
		Summary string `json:"summary"`
	}
	_ = json.Unmarshal([]byte(raw), &out)
	if direct := estimateShelter(faireyLat, faireyLon, lines); out.Summary != direct.Summary {
		t.Errorf("duplicated reef changed the answer: %q vs %q", out.Summary, direct.Summary)
	}
}

func TestAnchorageShelterTool_SaysExactlyWhyItCannotAnswer(t *testing.T) {
	const args = `{"lat":-20.097633,"lon":148.902533}`
	lines := loadShorelineFixture(t, "stonehaven")

	// Off: tells the operator where the switch is, whatever is held.
	w := newShelterWorld(t)
	w.hold(t, lines, coverageSquare{-21, 148}, stonehavenSquares...)
	w.enabled = false
	_, err := w.run(args)
	if err == nil || !strings.Contains(err.Error(), "Offline coastline is off") || !strings.Contains(err.Error(), "Settings → Offline coastline") {
		t.Errorf("off: %v", err)
	}

	// Nothing held, boat nearby with a fix: still downloading.
	w = newShelterWorld(t)
	_, err = w.run(args)
	if err == nil || !strings.Contains(err.Error(), "still downloading") || !strings.Contains(err.Error(), "21°S 148°E") {
		t.Errorf("downloading: %v", err)
	}

	// The square failed: the reason is passed on.
	_ = w.store.RecordFailure(coverageSquare{-21, 148}, "overpass rate limited", coastlineTestAt, coastlineTestAt)
	_, err = w.run(args)
	if err == nil || !strings.Contains(err.Error(), "download failed: overpass rate limited") {
		t.Errorf("failed: %v", err)
	}

	// Boat nowhere near, nothing held: outside coverage.
	w = newShelterWorld(t)
	w.boatLat, w.boatLon = 51.5, -1.2
	_, err = w.run(args)
	if err == nil || !strings.Contains(err.Error(), "outside Offline coastline coverage") {
		t.Errorf("outside: %v", err)
	}

	// No fix and nothing held: no way to download it either.
	w = newShelterWorld(t)
	w.fix = false
	_, err = w.run(args)
	if err == nil || !strings.Contains(err.Error(), "outside Offline coastline coverage") {
		t.Errorf("no fix: %v", err)
	}

	// Own square held, neighbours missing: names them and why.
	w = newShelterWorld(t)
	w.hold(t, lines, coverageSquare{-21, 148}, coverageSquare{-20, 148})
	_ = w.store.RecordFailure(coverageSquare{-21, 149}, "timed out", coastlineTestAt, coastlineTestAt)
	_, err = w.run(args)
	if err == nil {
		t.Fatal("a missing neighbour must be an error: rays reach 15 nm")
	}
	for _, want := range []string{"neighbouring", "21°S 149°E (download failed: timed out)", "20°S 149°E (still downloading)"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("neighbours: %q misses %q", err, want)
		}
	}
	if strings.Contains(err.Error(), "20°S 148°E") {
		t.Errorf("a square that is held must not be listed: %v", err)
	}

	// A neighbour that will never come (boat far off, not held).
	w = newShelterWorld(t)
	w.boatLat, w.boatLon = 51.5, -1.2
	w.hold(t, lines, coverageSquare{-21, 148})
	if _, err = w.run(args); err == nil || !strings.Contains(err.Error(), "outside coverage") {
		t.Errorf("held own square, no neighbours, boat elsewhere: %v", err)
	}

	// Across the antimeridian.
	w = newShelterWorld(t)
	if _, err = w.run(`{"lat":-17,"lon":179.95}`); err == nil || !strings.Contains(err.Error(), "180") {
		t.Errorf("antimeridian: %v", err)
	}
}

func TestAnchorageShelterTool_NoLiveFallbackWithoutCoastline(t *testing.T) {
	// A place-names plugin that could answer must never be consulted.
	called := false
	deps := assistantToolDeps{
		placeNames: func() (placeNameProvider, string, error) { called = true; return nil, "", nil },
	}
	_, err := deps.execute(context.Background(), "anchorage_shelter", json.RawMessage(`{"lat":-20.1,"lon":148.9}`))
	if err == nil || !strings.Contains(err.Error(), "Offline coastline") {
		t.Errorf("want an Offline coastline error, got %v", err)
	}
	if called {
		t.Error("anchorage_shelter must not reach for a live source")
	}
}

func TestAnchorageShelterTool_RefusesBadArguments(t *testing.T) {
	w := newShelterWorld(t)
	for _, args := range []string{`{}`, `{"lat":95,"lon":10}`, `{"lat":-20,"lon":200}`, `{"lat":0,"lon":0}`, `{"lat":"x"}`} {
		if _, err := w.run(args); err == nil {
			t.Errorf("args %s should be refused", args)
		}
	}
}

func TestDescribeAnchorageShelter(t *testing.T) {
	tool := lookupMateTool("anchorage_shelter")
	if tool == nil {
		t.Fatal("anchorage_shelter is not registered")
	}
	if got := tool.describe(json.RawMessage(`{"lat":-20.1,"lon":148.9}`)); !strings.Contains(got, "shelter") {
		t.Errorf("describe %q", got)
	}
	for _, want := range []string{"estimate", "headland", "valley", "swell", "forecast"} {
		if !strings.Contains(strings.ToLower(tool.function.Description), want) {
			t.Errorf("description misses %q", want)
		}
	}
}

// ── host side of shoreline_square and shoreline_changes ─────────────────

func TestMapWasmShorelineOutput(t *testing.T) {
	ring := [][2]float64{{-20, 149}, {-20, 149.1}, {-20.1, 149.1}, {-20, 149}}
	lines, err := mapWasmShorelineOutput("p", wasmShorelineOutput{Lines: []wasmShorelineLine{
		{ID: "way/1", Kind: "reef", Name: "Fairey\nReef", Closed: true, Points: ring},
		{ID: "way/2", Kind: "coastline", Points: ring[:3]},
	}})
	if err != nil || len(lines) != 2 {
		t.Fatalf("lines %v err %v", lines, err)
	}
	if lines[0].Name != "FaireyReef" || !lines[0].Closed || lines[0].Points[1] != (shorelinePoint{-20, 149.1}) {
		t.Errorf("mapped %+v", lines[0])
	}

	if lines, err := mapWasmShorelineOutput("p", wasmShorelineOutput{}); err != nil || len(lines) != 0 {
		t.Errorf("an empty shoreline is a legitimate answer: %v %v", lines, err)
	}

	bad := map[string]wasmShorelineLine{
		"kind":       {ID: "a", Kind: "cliff", Points: ring},
		"one point":  {ID: "b", Kind: "coastline", Points: ring[:1]},
		"range":      {ID: "c", Kind: "coastline", Points: [][2]float64{{-20, 149}, {95, 149}}},
		"fake close": {ID: "d", Kind: "reef", Closed: true, Points: ring[:3]},
	}
	for name, l := range bad {
		if _, err := mapWasmShorelineOutput("p", wasmShorelineOutput{Lines: []wasmShorelineLine{l}}); err == nil {
			t.Errorf("%s: want an error", name)
		}
	}
}

func TestSupportsShoreline_FalseWithoutExport(t *testing.T) {
	manifest := extism.Manifest{Wasm: []extism.Wasm{extism.WasmFile{Path: configEchoFixtureWasm}}}
	base, err := newWasmPluginBase(manifest, "plugins/test")
	if err != nil {
		t.Fatal(err)
	}
	if base.SupportsShoreline() {
		t.Fatal("configecho exports no shoreline_square")
	}
	p := newWasmPOIProviderFromBase(base)
	if _, err := p.ShorelineSquare(coverageSquare{-21, 148}); err == nil || !strings.Contains(err.Error(), "shoreline_square") {
		t.Errorf("want a clear error, got %v", err)
	}
	if _, err := p.ShorelineChanges(coverageSquare{-21, 148}, time.Now()); err == nil || !strings.Contains(err.Error(), "shoreline_changes") {
		t.Errorf("want a clear error, got %v", err)
	}
}

func TestSupportsShoreline_TrueForBuiltOsmOverpass(t *testing.T) {
	if _, err := os.Stat(osmOverpassRealPluginWasm); err != nil {
		t.Skipf("osm-overpass.wasm not built: %v", err)
	}
	manifest := extism.Manifest{Wasm: []extism.Wasm{extism.WasmFile{Path: osmOverpassRealPluginWasm}}}
	base, err := newWasmPluginBase(manifest, "plugins/test")
	if err != nil {
		t.Fatal(err)
	}
	if !base.SupportsShoreline() {
		t.Fatal("rebuilt osm-overpass must export shoreline_square and shoreline_changes")
	}
}

func TestCallWithin_BudgetIsPerCallAndLeavesTheSharedOneAlone(t *testing.T) {
	manifest := extism.Manifest{Wasm: []extism.Wasm{extism.WasmFile{Path: configEchoFixtureWasm}}, Timeout: 15000}
	base, err := newWasmPluginBase(manifest, "plugins/test")
	if err != nil {
		t.Fatal(err)
	}
	// A generous per-call budget works, a one-nanosecond one fails that call
	// only, and a plain call afterwards still uses the manifest's budget.
	if _, err := base.callWithin("id", nil, time.Minute); err != nil {
		t.Fatalf("a long budget: %v", err)
	}
	if _, err := base.callWithin("id", nil, time.Nanosecond); err == nil {
		t.Error("a one nanosecond budget must expire")
	}
	if _, err := base.call("id", nil); err != nil {
		t.Fatalf("the shared budget must be untouched: %v", err)
	}
	if coastlineBackgroundCallBudget <= time.Duration(defaultWasmPluginTimeoutMS)*time.Millisecond {
		t.Error("background coastline calls need more than Mate's interactive budget")
	}
}

func TestEstimateShelter_InsideReefSummaryIgnoresBearingsBlockedByACay(t *testing.T) {
	// A lagoon about 1 nm across with a cay close to the east of the anchor.
	// Bearings that hit the cay are not reef width, so the summary must not
	// report them as a 0.0 nm minimum.
	const lat, lon = -20.0, 149.0
	d := 0.0166 // about 1 nm of latitude
	reef := []shorelinePoint{{lat - d, lon - d}, {lat - d, lon + d}, {lat + d, lon + d}, {lat + d, lon - d}, {lat - d, lon - d}}
	cay := []shorelinePoint{{lat - 0.0005, lon + 0.002}, {lat + 0.0005, lon + 0.002}, {lat + 0.0005, lon + 0.003}, {lat - 0.0005, lon + 0.003}, {lat - 0.0005, lon + 0.002}}
	est := estimateShelter(lat, lon, []shorelineLine{
		{ID: "way/1", Kind: "reef", Name: "Lagoon", Closed: true, Points: reef},
		{ID: "way/2", Kind: "coastline", Closed: true, Points: cay},
	})
	if !est.InsideReef {
		t.Fatalf("anchor is inside the lagoon reef")
	}
	if strings.Contains(est.Summary, "0.0 nm") {
		t.Errorf("summary reports a zero reef width: %q", est.Summary)
	}
}
