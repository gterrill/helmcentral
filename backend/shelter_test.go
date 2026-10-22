package main

import (
	"compress/gzip"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"sync"
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
	f, err := os.Open("testdata/shelter/" + name + ".json.gz")
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

type shorelineInput struct {
	lat, lon float64
	radiusM  int
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
	// estimate overshoots the south end - the known limitation in ADR 0201).
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

// ── cache + tool ────────────────────────────────────────────────────────

type fakeShorelineProvider struct {
	fakeSearchPlacesProvider
	lines     []shorelineLine
	shoreErr  error
	shoreMu   sync.Mutex
	shoreHits []shorelineInput
	unsupp    bool
}

func (f *fakeShorelineProvider) SupportsShoreline() bool { return !f.unsupp }

func (f *fakeShorelineProvider) ShorelineNear(lat, lon float64, radiusM int) ([]shorelineLine, error) {
	f.shoreMu.Lock()
	f.shoreHits = append(f.shoreHits, shorelineInput{lat, lon, radiusM})
	f.shoreMu.Unlock()
	if f.shoreErr != nil {
		return nil, f.shoreErr
	}
	return f.lines, nil
}

func shelterDeps(provider placeNameProvider) assistantToolDeps {
	shorelineCache.reset()
	return assistantToolDeps{
		placeNames: func() (placeNameProvider, string, error) { return provider, "fake", nil },
	}
}

func TestAnchorageShelterTool_ReturnsArcsTableAndCaveat(t *testing.T) {
	p := &fakeShorelineProvider{lines: loadShorelineFixture(t, "stonehaven")}
	p.id = "fake"
	deps := shelterDeps(p)

	raw, err := deps.execute(context.Background(), "anchorage_shelter", json.RawMessage(`{"lat":-20.097633,"lon":148.902533}`))
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	var out struct {
		Summary  string           `json:"summary"`
		Arcs     []shelterArc     `json:"arcs"`
		Bearings []shelterBearing `json:"bearings"`
		Caveat   string           `json:"caveat"`
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
	if len(p.shoreHits) != 1 {
		t.Fatalf("shoreline fetches: %d", len(p.shoreHits))
	}
	// The query must reach the full ray length from anywhere in its cache cell.
	if min := int(shelterMaxFetchNm * metersPerNauticalMile); p.shoreHits[0].radiusM <= min {
		t.Errorf("radius %d m does not cover the %d m rays plus the cache cell", p.shoreHits[0].radiusM, min)
	}
}

func TestAnchorageShelterTool_CachesByRoundedPosition(t *testing.T) {
	p := &fakeShorelineProvider{lines: loadShorelineFixture(t, "stonehaven")}
	deps := shelterDeps(p)
	ctx := context.Background()
	for _, args := range []string{
		`{"lat":-20.097633,"lon":148.902533}`,
		`{"lat":-20.0990,"lon":148.9040}`, // same 0.02 degree cell
	} {
		if _, err := deps.execute(ctx, "anchorage_shelter", json.RawMessage(args)); err != nil {
			t.Fatal(err)
		}
	}
	if len(p.shoreHits) != 1 {
		t.Errorf("want one provider call for two nearby anchors, got %d", len(p.shoreHits))
	}
	if _, err := deps.execute(ctx, "anchorage_shelter", json.RawMessage(`{"lat":-20.3,"lon":148.9}`)); err != nil {
		t.Fatal(err)
	}
	if len(p.shoreHits) != 2 {
		t.Errorf("a different cell must fetch again, got %d", len(p.shoreHits))
	}
}

func TestAnchorageShelterTool_FailsFast(t *testing.T) {
	ctx := context.Background()

	// Upstream error: surfaced, not cached, not masked.
	p := &fakeShorelineProvider{shoreErr: fmt.Errorf("overpass query failed: runtime error: timed out")}
	deps := shelterDeps(p)
	_, err := deps.execute(ctx, "anchorage_shelter", json.RawMessage(`{"lat":-20.1,"lon":148.9}`))
	if err == nil || !strings.Contains(err.Error(), "timed out") {
		t.Fatalf("want the upstream error, got %v", err)
	}
	p.shoreErr = nil
	p.lines = loadShorelineFixture(t, "stonehaven")
	if _, err := deps.execute(ctx, "anchorage_shelter", json.RawMessage(`{"lat":-20.1,"lon":148.9}`)); err != nil {
		t.Fatalf("a failure must not be cached: %v", err)
	}

	// Plugin without the export.
	old := &fakeShorelineProvider{unsupp: true}
	_, err = shelterDeps(old).execute(ctx, "anchorage_shelter", json.RawMessage(`{"lat":-20.1,"lon":148.9}`))
	if err == nil || !strings.Contains(err.Error(), "shoreline_near") {
		t.Errorf("want an error naming shoreline_near, got %v", err)
	}

	// A provider type that knows nothing of shorelines at all.
	_, err = shelterDeps(&fakeSearchPlacesProvider{id: "plain"}).execute(ctx, "anchorage_shelter", json.RawMessage(`{"lat":-20.1,"lon":148.9}`))
	if err == nil || !strings.Contains(err.Error(), "shoreline_near") {
		t.Errorf("want an error naming shoreline_near, got %v", err)
	}

	// Bad arguments.
	for _, args := range []string{`{}`, `{"lat":95,"lon":10}`, `{"lat":-20,"lon":200}`, `{"lat":0,"lon":0}`} {
		if _, err := deps.execute(ctx, "anchorage_shelter", json.RawMessage(args)); err == nil {
			t.Errorf("args %s should be refused", args)
		}
	}
}

func TestShorelineCache_ExpiresAfterTTL(t *testing.T) {
	now := time.Unix(1_800_000_000, 0)
	c := newShorelineCacheStore(func() time.Time { return now })
	c.put("k", []shorelineLine{{ID: "way/1"}})
	if _, ok := c.get("k"); !ok {
		t.Fatal("fresh entry missing")
	}
	now = now.Add(shorelineCacheTTL + time.Second)
	if _, ok := c.get("k"); ok {
		t.Error("expired entry served")
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

// ── host side of shoreline_near ─────────────────────────────────────────

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
		t.Fatal("configecho exports no shoreline_near")
	}
	p := newWasmPOIProviderFromBase(base)
	if _, err := p.ShorelineNear(-20, 149, 1000); err == nil || !strings.Contains(err.Error(), "shoreline_near") {
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
		t.Fatal("rebuilt osm-overpass must export shoreline_near")
	}
}
