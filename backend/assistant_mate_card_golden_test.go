package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"testing"
	"time"
)

// Every Mate card kind has golden fixtures in testdata/mate_cards, written by
// the real card builders and read by the browser's decoder tests (ADR 0261).
// `go test -run MateCardGoldens -update` rewrites them.

// mateCardGolden is one fixture and the real builder that writes it.
type mateCardGolden struct {
	file  string
	build func(t *testing.T) mateCard
}

// mateCardGoldens is every fixture in testdata/mate_cards. A fixture belongs
// here or TestEveryMateCardKindHasAGoldenFixture fails, so deleting a builder
// cannot leave its file standing in for coverage.
var mateCardGoldens = []mateCardGolden{
	{"marina_coral_sea.json", marinaGoldenCard},
	{"marina_small.json", marinaSmallCard},
	{"anchorage_butterfly_bay.json", anchorageGoldenCard},
	{"anchorage_no_depth.json", anchorageNoDepthCard},
	{"passage_window_two_days.json", passageWindowTwoDaysCard},
	{"passage_window_itinerary.json", passageWindowItineraryCard},
	{"passage_window_daylight.json", passageWindowDaylightCard},
	{"passage_window_decided_operator.json", func(t *testing.T) mateCard {
		return passageWindowStoredCard(t, func(asst *assistantStore, id int64) error {
			date := "2026-10-21"
			_, err := asst.SetOperatorPassageDecision(id, "went", &date)
			return err
		})
	}},
	{"passage_window_decided_inferred.json", func(t *testing.T) mateCard {
		return passageWindowStoredCard(t, func(asst *assistantStore, id int64) error {
			_, err := asst.ApplyPassageDecisionUpdates([]decisionUpdate{{ID: id, State: "went", RowDate: "2026-10-20", PassageID: "passage-1"}})
			return err
		})
	}},
	{"telemetry_chart_house_soc.json", telemetryTimeCard},
	{"telemetry_chart_xy.json", telemetryXYCard},
	{"wind_rose_eight_sectors.json", windRoseCard},
	{"wind_rose_no_gust.json", windRoseNoGustCard},
	{"tile_depth_tide.json", tileFullCard},
	{"tile_depth_tide_missing.json", tileMissingCard},
}

// assertMateCardGolden compares a card as the store holds and serves it, with
// its row id and passage decision when it has them, against its fixture.
func assertMateCardGolden(t *testing.T, file string, card mateCard) {
	t.Helper()
	envelope, err := json.MarshalIndent(card, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	envelope = append(envelope, '\n')
	path := filepath.Join("testdata", "mate_cards", file)
	if *updateMateFixtures {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, envelope, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("%v; run go test -run MateCardGoldens -update to write it", err)
	}
	if !bytes.Equal(envelope, want) {
		t.Errorf("%s card drifted from %s; run go test -run MateCardGoldens -update if intended", card.Kind, path)
	}
}

func TestMateCardGoldens(t *testing.T) {
	for _, g := range mateCardGoldens {
		t.Run(g.file, func(t *testing.T) {
			assertMateCardGolden(t, g.file, g.build(t))
		})
	}
}

func onlyCard(t *testing.T, cards []mateCard, kind string) mateCard {
	t.Helper()
	if len(cards) != 1 || cards[0].Kind != kind {
		t.Fatalf("cards = %+v, want one %s", cards, kind)
	}
	return cards[0]
}

func passageWindowTwoDaysCard(t *testing.T) mateCard {
	d := pwToolDeps(pwLimitsAll())
	result, _, err := d.execute(context.Background(), "assess_passage_window", json.RawMessage(pwToolArgs))
	if err != nil {
		t.Fatal(err)
	}
	return onlyCard(t, d.takeCards(result), passageWindowCardKind)
}

// passageWindowStoredCard saves the two-day card in a reply, lets decide
// answer its blank decision, and reads the card back the way the thread does.
func passageWindowStoredCard(t *testing.T, decide func(asst *assistantStore, decisionID int64) error) mateCard {
	t.Helper()
	asst := newTestAssistantStore(t)
	conv, _ := asst.CreateConversation("c")
	msg, err := appendCards(t, asst, conv.ID, passageWindowTwoDaysCard(t))
	if err != nil {
		t.Fatal(err)
	}
	if err := decide(asst, msg.Cards[0].Decision.ID); err != nil {
		t.Fatal(err)
	}
	msgs, err := asst.ListMessages(conv.ID)
	if err != nil || len(msgs) != 1 {
		t.Fatalf("messages = %v, err = %v", msgs, err)
	}
	return onlyCard(t, msgs[0].Cards, passageWindowCardKind)
}

// passageWindowItineraryCard is two legs with a night between and six
// departure days that each trip a different limit. The wind holds steady for a
// local day: 5 kn astern, 25 kn astern, 25 kn ahead, 30 kn abeam, then 5 kn
// with 2.5 m waves ahead, then 10 kn gusting 45, and no forecast after the 26th.
func passageWindowItineraryCard(t *testing.T) mateCard {
	t.Helper()
	type day struct{ kn, gustKn, from float64 }
	days := map[int]day{
		19: {5, 6, 180}, 20: {5, 6, 180}, 21: {25, 28, 180}, 22: {25, 26, 0},
		23: {30, 32, 90}, 24: {5, 6, 180}, 25: {10, 45, 180}, 26: {5, 6, 180},
	}
	var hours []weatherHourPoint
	for h := pwDay(19, 0); !h.After(pwDay(26, 23)); h = h.Add(time.Hour) {
		w := days[h.Day()]
		hours = append(hours, weatherHourPoint{Time: h, WindSpeedMS: kn(w.kn), WindGustMS: kn(w.gustKn), WindDirectionDeg: w.from})
	}
	waveM, waveDir, waveS := 2.5, 0.0, 6.0
	var waves []waveHourPoint
	for h := pwDay(24, 0); !h.After(pwDay(24, 23)); h = h.Add(time.Hour) {
		waves = append(waves, waveHourPoint{Time: h, WaveHeightM: &waveM, WaveDirectionDeg: &waveDir, WavePeriodS: &waveS})
	}
	limits := pwLimitsAll()
	limits.Ahead.WaveMNoGo = pwF(2)
	limits.DaylightArrival = true
	limits.WindAgainstTideOffsetKn = pwF(10)
	d := pwToolDeps(limits)
	d.weather = func() (weatherProvider, string, error) {
		return &pwWeather{bundle: weatherForecastBundle{Hourly: hours}}, "wx", nil
	}
	d.waves = func() (waveProvider, string, error) {
		return &stubWaveProvider{id: "wv", bundle: waveForecastBundle{Hourly: waves}}, "wv", nil
	}
	args := `{"legs":[` +
		`{"route":[{"lat":-19,"lon":147,"name":"Alpha"},{"lat":-18,"lon":147,"name":"Bravo"}],"passage_hours":10,"flood_set_deg":0},` +
		`{"route":[{"lat":-18,"lon":147,"name":"Bravo"},{"lat":-17,"lon":147,"name":"Charlie"}],"passage_hours":10}],` +
		`"stays_nights":[1],"departure_dates":["2026-10-20","2026-10-21","2026-10-22","2026-10-23","2026-10-24","2026-10-25","2026-10-29"],"depart_time":"06:00"}`
	result, _, err := d.execute(context.Background(), "assess_passage_window", json.RawMessage(args))
	if err != nil {
		t.Fatal(err)
	}
	return onlyCard(t, d.takeCards(result), passageWindowCardKind)
}

// passageWindowDaylightCard leaves at noon in a light southerly on a ten-hour
// leg, so it arrives after dark, and the limits ask for arrival in daylight.
func passageWindowDaylightCard(t *testing.T) mateCard {
	limits := pwLimitsAll()
	limits.DaylightArrival = true
	d := pwToolDeps(limits)
	var calm []weatherHourPoint
	for h := pwDay(19, 0); !h.After(pwDay(22, 23)); h = h.Add(time.Hour) {
		calm = append(calm, weatherHourPoint{Time: h, WindSpeedMS: kn(5), WindGustMS: kn(6), WindDirectionDeg: 180})
	}
	d.weather = func() (weatherProvider, string, error) {
		return &pwWeather{bundle: weatherForecastBundle{Hourly: calm}}, "wx", nil
	}
	args := `{"legs":[{"route":[{"lat":-19,"lon":147,"name":"Alpha"},{"lat":-18,"lon":147,"name":"Bravo"}],"passage_hours":10}],"departure_dates":["2026-10-20"],"depart_time":"12:00"}`
	result, _, err := d.execute(context.Background(), "assess_passage_window", json.RawMessage(args))
	if err != nil {
		t.Fatal(err)
	}
	return onlyCard(t, d.takeCards(result), passageWindowCardKind)
}

func telemetryTimeCard(t *testing.T) mateCard {
	f := &tcFake{
		series:  map[string][]telemetryPoint{tcSoc: tcHourly(tcStart(), 12, func(i int) float64 { return 0.9 - float64(i)*0.05 })},
		sources: map[string][]string{tcSoc: {"bms"}},
	}
	d := tcDeps(f, tcVessel(), "metric")
	res, err := tcRun(t, d, `{"series":[{"role":"house_bank_soc"}],"from":"2026-10-12T00:00:00Z","to":"2026-10-12T06:00:00Z","compare":[{"label":"Later","from":"2026-10-12T06:00:00Z","to":"2026-10-12T12:00:00Z"}]}`)
	if err != nil {
		t.Fatal(err)
	}
	return onlyCard(t, d.takeCards(res), "telemetry_chart")
}

// telemetryXYCard plots alternator current against charge, with the source of
// the current named because two devices report it.
func telemetryXYCard(t *testing.T) mateCard {
	f := &tcFake{
		series: map[string][]telemetryPoint{
			tcSoc:     tcHourly(tcStart(), 6, func(i int) float64 { return 0.5 + 0.1*float64(i) }),
			tcAltStbd: tcHourly(tcStart(), 6, func(i int) float64 { return float64(60 - i*10) }),
		},
		sources: map[string][]string{tcAltStbd: {"alt-a", "alt-b"}, tcSoc: {"bms"}},
	}
	f.series[tcAltStbd+"|alt-a"] = f.series[tcAltStbd]
	d := tcDeps(f, tcVessel(), "metric")
	res, err := tcRun(t, d, `{"series":[{"role":"house_bank_soc"},{"role":"starboard_alternator_current","source":"alt-a"}],"from":"2026-10-12T00:00:00Z","to":"2026-10-12T06:00:00Z","view":"xy","x":0,"y":1}`)
	if err != nil {
		t.Fatal(err)
	}
	return onlyCard(t, d.takeCards(res), "telemetry_chart")
}

func windRoseFake() wrFake {
	return wrFake{
		n:       6,
		dirDeg:  func(i int) float64 { return []float64{90, 90, 180, 180, 270, 45}[i] },
		speedMS: func(i int) float64 { return []float64{0.2, 8, 12, 22, 12, 30}[i] * knMS },
	}
}

func windRoseCard(t *testing.T) mateCard {
	useFreshBoatZone(t)
	d := wrDeps(windRoseFake())
	res, err := wrRun(t, d, `{"from":"2026-10-12T00:00:00Z","to":"2026-10-12T01:00:00Z","sectors":8}`)
	if err != nil {
		t.Fatal(err)
	}
	return onlyCard(t, d.takeCards(res), "wind_rose")
}

// windRoseNoGustCard has no logged gust, so the card names no strongest wind,
// and counts the default 16 sectors.
func windRoseNoGustCard(t *testing.T) mateCard {
	useFreshBoatZone(t)
	f := windRoseFake()
	d := wrDeps(f)
	d.influxPathHistoryStat = func(ctx context.Context, path, source string, start, stop time.Time, bucket, agg string) ([]telemetryPoint, error) {
		if agg == "max" {
			return nil, nil
		}
		return f.stat(ctx, path, source, start, stop, bucket, agg)
	}
	res, err := wrRun(t, d, `{"from":"2026-10-12T00:00:00Z","to":"2026-10-12T01:00:00Z"}`)
	if err != nil {
		t.Fatal(err)
	}
	return onlyCard(t, d.takeCards(res), "wind_rose")
}

func tileFullCard(t *testing.T) mateCard {
	useFreshBoatZone(t)
	d := tileCardDeps(vesselStateData{Status: "anchored", Depth: 3.3, DepthLastUpdateAge: 2, Latitude: -20, Longitude: 148})
	d.tileTide = func(context.Context, vesselStateData, time.Time) (tideTodayResponse, error) {
		return tideTodayResponse{
			Datetime: tileCardNow.Format(time.RFC3339), CurrentTideHeightFt: ptr(6.56), TideDirection: "Rising",
			HighTideTime: "2026-10-29T04:12:00Z", HighTideHeightFt: ptr(9.84),
			LowTideTime: "2026-10-29T10:30:00Z", LowTideHeightFt: ptr(1.64),
			StationName: "Example Harbour", Provider: "test",
		}, nil
	}
	d.tileDepthTrend = func(context.Context, string) depthTrendResponse {
		var pts []depthTrendPoint
		for i := 0; i < 4; i++ {
			pts = append(pts, depthTrendPoint{Time: tileCardNow.Add(time.Duration(i-4) * time.Hour), DepthM: 3 + float64(i)/4})
		}
		return depthTrendResponse{Points: pts, Since: "tide", TideType: "low", TideDepthM: 2.6}
	}
	out, _ := tileCardRun(t, d, `{"tile":"depth-tide"}`)
	return onlyCard(t, d.takeCards(out), "tile")
}

// tileMissingCard has no depth reading and no tide station, so the snapshot
// holds explicit nulls, and a trend over a plain window.
func tileMissingCard(t *testing.T) mateCard {
	useFreshBoatZone(t)
	d := tileCardDeps(vesselStateData{Depth: -1, DepthLastUpdateAge: -1})
	d.tileTide = func(context.Context, vesselStateData, time.Time) (tideTodayResponse, error) {
		return tideTodayResponse{}, &tideTodayError{status: 502, reason: tideNoStation, message: "no tide provider configured"}
	}
	d.tileDepthTrend = func(context.Context, string) depthTrendResponse {
		return depthTrendResponse{Points: []depthTrendPoint{}, Since: "window"}
	}
	out, _ := tileCardRun(t, d, `{"tile":"depth-tide"}`)
	return onlyCard(t, d.takeCards(out), "tile")
}

// mateCardKindsIn reads the kind of every card the given sources build (file
// name to text). It follows a card built as a literal of type mateCard, as an
// element of a []mateCard or map of them, keyed or positional, with a string
// or a string constant for its kind. It refuses what it cannot read: a Kind
// that is not a literal or constant, a literal with no Kind, and a card handed
// to a sink's put that was not built in that call.
func mateCardKindsIn(sources map[string]string) ([]string, error) {
	fset := token.NewFileSet()
	var files []*ast.File
	names := make([]string, 0, len(sources))
	for name := range sources {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		f, err := parser.ParseFile(fset, name, sources[name], 0)
		if err != nil {
			return nil, err
		}
		files = append(files, f)
	}
	consts := map[string]string{}
	for _, f := range files {
		ast.Inspect(f, func(n ast.Node) bool {
			if spec, ok := n.(*ast.ValueSpec); ok {
				for i, name := range spec.Names {
					if i < len(spec.Values) {
						if lit, ok := spec.Values[i].(*ast.BasicLit); ok && lit.Kind == token.STRING {
							if s, err := strconv.Unquote(lit.Value); err == nil {
								consts[name.Name] = s
							}
						}
					}
				}
			}
			return true
		})
	}
	isCard := func(e ast.Expr) bool {
		if star, ok := e.(*ast.StarExpr); ok {
			e = star.X
		}
		id, ok := e.(*ast.Ident)
		return ok && id.Name == "mateCard"
	}
	// The card sink is the field or variable called cards (assistantToolDeps.cards).
	isCardSink := func(e ast.Expr) bool {
		switch x := e.(type) {
		case *ast.SelectorExpr:
			return x.Sel.Name == "cards"
		case *ast.Ident:
			return x.Name == "cards"
		}
		return false
	}
	seen := map[string]bool{}
	var failures []string
	fail := func(pos token.Pos, format string, args ...any) {
		failures = append(failures, fset.Position(pos).String()+": "+fmt.Sprintf(format, args...))
	}
	kindOf := func(lit *ast.CompositeLit) {
		var value ast.Expr
		keyed := false
		for i, el := range lit.Elts {
			if kv, ok := el.(*ast.KeyValueExpr); ok {
				keyed = true
				if key, ok := kv.Key.(*ast.Ident); ok && key.Name == "Kind" {
					value = kv.Value
				}
			} else if i == 1 { // positional: ID, Kind, ...
				value = el
			}
		}
		if value == nil {
			fail(lit.Pos(), "a mateCard literal with no Kind (keyed %v)", keyed)
			return
		}
		switch v := value.(type) {
		case *ast.BasicLit:
			if s, err := strconv.Unquote(v.Value); err == nil && v.Kind == token.STRING {
				seen[s] = true
				return
			}
		case *ast.Ident:
			if s, ok := consts[v.Name]; ok {
				seen[s] = true
				return
			}
		}
		fail(value.Pos(), "a mateCard Kind that is not a string literal or constant")
	}
	for _, f := range files {
		ast.Inspect(f, func(n ast.Node) bool {
			switch n := n.(type) {
			case *ast.CompositeLit:
				switch typ := n.Type.(type) {
				case *ast.ArrayType:
					if isCard(typ.Elt) {
						for _, el := range n.Elts {
							if lit, ok := el.(*ast.CompositeLit); ok && lit.Type == nil {
								kindOf(lit)
							}
						}
					}
				case *ast.MapType:
					if isCard(typ.Value) {
						for _, el := range n.Elts {
							if kv, ok := el.(*ast.KeyValueExpr); ok {
								if lit, ok := kv.Value.(*ast.CompositeLit); ok && lit.Type == nil {
									kindOf(lit)
								}
							}
						}
					}
				default:
					if n.Type != nil && isCard(n.Type) {
						kindOf(n)
					}
				}
			case *ast.CallExpr:
				if sel, ok := n.Fun.(*ast.SelectorExpr); ok && sel.Sel.Name == "put" && len(n.Args) == 2 && isCardSink(sel.X) {
					if lit, ok := n.Args[1].(*ast.CompositeLit); !ok || lit.Type == nil || !isCard(lit.Type) {
						fail(n.Pos(), "put is handed a card that is not built in the call")
					}
				}
			}
			return true
		})
	}
	if len(failures) > 0 {
		return nil, fmt.Errorf("%s", strings.Join(failures, "\n"))
	}
	kinds := make([]string, 0, len(seen))
	for k := range seen {
		kinds = append(kinds, k)
	}
	sort.Strings(kinds)
	return kinds, nil
}

func TestMateCardKindsIn_ReadsEveryShapeAKindIsBuiltIn(t *testing.T) {
	src := `package main
const named = "from_const"
func f(d deps) {
	d.cards.put("r", mateCard{Kind: "typed"})
	d.cards.put("r", mateCard{Kind: named, Payload: nil})
	_ = []mateCard{{Kind: "in_slice"}, {Kind: "in_slice_too"}}
	_ = map[string]mateCard{"a": {Kind: "in_map"}}
	_ = mateCard{0, "positional", nil, nil}
	_ = &mateCard{Kind: "pointer"}
	_ = []*mateCard{{Kind: "pointer_slice"}}
	_ = map[string][]mateCard{}
}`
	got, err := mateCardKindsIn(map[string]string{"a.go": src})
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"from_const", "in_map", "in_slice", "in_slice_too", "pointer", "pointer_slice", "positional", "typed"}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Errorf("kinds = %v, want %v", got, want)
	}
}

func TestMateCardKindsIn_RefusesWhatItCannotRead(t *testing.T) {
	cases := map[string]string{
		"assigned then put":  `func f(d deps) { var c mateCard; c.Kind = "late"; d.cards.put("r", c) }`,
		"kind from a call":   `func f(d deps) { d.cards.put("r", mateCard{Kind: kindFor()}) }`,
		"unknown constant":   `func f() { _ = mateCard{Kind: elsewhere} }`,
		"literal without it": `func f() { _ = mateCard{Payload: nil} }`,
		"element without it": `func f() { _ = []mateCard{{Payload: nil}} }`,
		"built apart":        `func f(d deps, c mateCard) { d.cards.put("r", c) }`,
	}
	for name, body := range cases {
		if kinds, err := mateCardKindsIn(map[string]string{"a.go": "package main\n" + body}); err == nil {
			t.Errorf("%s: read %v, want an error", name, kinds)
		}
	}
}

// backendSources is the non-test Go source of the backend.
func backendSources(t *testing.T) map[string]string {
	t.Helper()
	paths, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatal(err)
	}
	out := map[string]string{}
	for _, p := range paths {
		if strings.HasSuffix(p, "_test.go") {
			continue
		}
		raw, err := os.ReadFile(p)
		if err != nil {
			t.Fatal(err)
		}
		out[p] = string(raw)
	}
	return out
}

func TestEveryMateCardKindHasAGoldenFixture(t *testing.T) {
	kinds, err := mateCardKindsIn(backendSources(t))
	if err != nil {
		t.Fatalf("a card kind the scan cannot read, so make the card readable or teach mateCardKindsIn:\n%v", err)
	}
	if len(kinds) < 6 {
		t.Fatalf("found only %v; the source scan has stopped seeing the card builders", kinds)
	}

	// Every file in the directory is written by a registered golden test.
	files, err := filepath.Glob(filepath.Join("testdata", "mate_cards", "*.json"))
	if err != nil {
		t.Fatal(err)
	}
	registered := map[string]bool{}
	built := map[string]bool{}
	for _, g := range mateCardGoldens {
		if registered[g.file] {
			t.Errorf("%s is registered twice", g.file)
		}
		registered[g.file] = true
		built[g.build(t).Kind] = true
	}
	for _, f := range files {
		if !registered[filepath.Base(f)] {
			t.Errorf("%s is written by no golden test; register its builder in mateCardGoldens or delete it", f)
		}
	}
	for _, g := range mateCardGoldens {
		if _, err := os.Stat(filepath.Join("testdata", "mate_cards", g.file)); err != nil {
			t.Errorf("%s is registered but missing: %v", g.file, err)
		}
	}
	for _, k := range kinds {
		if !built[k] {
			t.Errorf("card kind %q has no golden builder in mateCardGoldens (ADR 0261)", k)
		}
	}
}
