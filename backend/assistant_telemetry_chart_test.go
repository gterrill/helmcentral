package main

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"strings"
	"sync"
	"testing"
	"time"
)

var tcNow = time.Date(2026, 10, 19, 12, 0, 0, 0, time.UTC)

const (
	tcSoc     = "electrical.batteries.1.capacity.stateOfCharge"
	tcAltStbd = "electrical.alternator.12.current"
	tcRPMPort = "propulsion.port.revolutions"
)

// tcFake is the synthetic Influx behind the tool: series by path (and
// optionally "path|source"), each a list of bucket-start points. min and max
// are the mean -1 and +1, so a summary built from the wrong aggregate shows.
type tcFake struct {
	mu      sync.Mutex
	series  map[string][]telemetryPoint
	sources map[string][]string
	// sourcesIn, when set, answers the source check per range instead of sources.
	sourcesIn func(path string, from, to time.Time) []string
	queried   []string
	// onStat, when set, runs at the top of every stat call.
	onStat func(ctx context.Context, path string, start time.Time)
}

func (f *tcFake) stat(ctx context.Context, path, source string, start, stop time.Time, _, agg string) ([]telemetryPoint, error) {
	key := path
	if source != "" {
		key = path + "|" + source
	}
	if f.onStat != nil {
		f.onStat(ctx, path, start)
	}
	f.mu.Lock()
	f.queried = append(f.queried, key)
	f.mu.Unlock()
	var out []telemetryPoint
	for _, p := range f.series[key] {
		if p.Timestamp.Before(start) || !p.Timestamp.Before(stop) {
			continue
		}
		switch agg {
		case "min":
			p.Value--
		case "max":
			p.Value++
		}
		out = append(out, p)
	}
	return out, nil
}

func (f *tcFake) pathSources(_ context.Context, path string, from, to time.Time) ([]string, error) {
	if f.sourcesIn != nil {
		return f.sourcesIn(path, from, to), nil
	}
	return f.sources[path], nil
}

// tcHourly is n hourly points from start, value fn(i).
func tcHourly(start time.Time, n int, fn func(i int) float64) []telemetryPoint {
	out := make([]telemetryPoint, n)
	for i := range out {
		out[i] = telemetryPoint{Timestamp: start.Add(time.Duration(i) * time.Hour), Value: fn(i)}
	}
	return out
}

func tcVessel() vesselView {
	return vesselView{
		HouseBank:   &vesselHouseBankSetting{Path: "electrical.batteries.1"},
		Alternators: vesselAlternatorSettings{PortInstance: "2", StarboardInstance: "12"},
		Engines: []vesselEngineSetting{
			{Instance: "port", Name: "Port"},
			{Instance: "starboard", Name: "Starboard"},
		},
	}
}

// tcDeclared is what the boat's SignalK declares (meta.units) for each path
// the tests chart. A path absent here declares no unit.
var tcDeclared = map[string]string{
	tcSoc:                             "ratio",
	"propulsion.starboard.engineLoad": "ratio",
	tcAltStbd:                         "A",
	"electrical.alternator.2.current": "A",
	"electrical.batteries.1.current":  "A",
	"electrical.batteries.1.voltage":  "V",
	tcRPMPort:                         "Hz",
	"propulsion.port.temperature":     "K",
	"environment.depth.belowKeel":     "m",
	"propulsion.port.fuel.rate":       "m3/s",
}

func tcDeps(f *tcFake, v vesselView, units string) assistantToolDeps {
	return assistantToolDeps{
		now:                   func() time.Time { return tcNow },
		vesselState:           func() (vesselStateData, error) { return vesselStateData{Latitude: -19, Longitude: 147}, nil },
		vesselView:            func() (vesselView, error) { return v, nil },
		displayUnits:          func() (string, error) { return units, nil },
		siUnit:                func(path string) string { return tcDeclared[path] },
		influxPathHistoryStat: f.stat,
		influxPathSources:     f.pathSources,
		cards:                 newAssistantCardSink(),
	}
}

func tcRun(t *testing.T, d assistantToolDeps, args string) (string, error) {
	t.Helper()
	return d.execute(context.Background(), "show_telemetry_chart", json.RawMessage(args))
}

func tcPayload(t *testing.T, d assistantToolDeps, result string) telemetryChartPayload {
	t.Helper()
	cards := d.takeCards(result)
	if len(cards) != 1 || cards[0].Kind != "telemetry_chart" {
		t.Fatalf("cards = %+v", cards)
	}
	var p telemetryChartPayload
	if err := json.Unmarshal(cards[0].Payload, &p); err != nil {
		t.Fatal(err)
	}
	return p
}

const tcRange = `"from":"2026-10-12T00:00:00Z","to":"2026-10-19T00:00:00Z"`

func tcStart() time.Time { return time.Date(2026, 10, 12, 0, 0, 0, 0, time.UTC) }

func TestShowTelemetryChart_RolesResolveThroughTheVesselView(t *testing.T) {
	pts := tcHourly(tcStart(), 24, func(i int) float64 { return 1 })
	f := &tcFake{series: map[string][]telemetryPoint{}}
	cases := []struct {
		series, wantPath, wantLabel string
	}{
		{`{"role":"house_bank_soc"}`, tcSoc, "House bank state of charge"},
		{`{"role":"house_bank_current"}`, "electrical.batteries.1.current", "House bank current"},
		{`{"role":"house_bank_voltage"}`, "electrical.batteries.1.voltage", "House bank voltage"},
		{`{"role":"port_alternator_current"}`, "electrical.alternator.2.current", "Port alternator current"},
		{`{"role":"starboard_alternator_current"}`, tcAltStbd, "Starboard alternator current"},
		{`{"role":"engine_rpm","engine":"port"}`, tcRPMPort, "Port engine RPM"},
		{`{"role":"engine_load","engine":"Starboard"}`, "propulsion.starboard.engineLoad", "Starboard engine load"},
		{`{"role":"engine_coolant_temperature","engine":"port"}`, "propulsion.port.temperature", "Port engine coolant temperature"},
	}
	for _, tc := range cases {
		f.series[tc.wantPath] = pts
		d := tcDeps(f, tcVessel(), "metric")
		res, err := tcRun(t, d, fmt.Sprintf(`{"series":[%s],%s}`, tc.series, tcRange))
		if err != nil {
			t.Fatalf("%s: %v", tc.series, err)
		}
		p := tcPayload(t, d, res)
		if p.Series[0].Path != tc.wantPath || p.Series[0].Label != tc.wantLabel {
			t.Errorf("%s: got %q %q, want %q %q", tc.series, p.Series[0].Path, p.Series[0].Label, tc.wantPath, tc.wantLabel)
		}
		if strings.Contains(res, "electrical.") || strings.Contains(res, "propulsion.") {
			t.Errorf("%s: a role's result must not show raw paths: %s", tc.series, res)
		}
	}
}

func TestShowTelemetryChart_UnassignedRoleIsAnErrorNamingTheRole(t *testing.T) {
	f := &tcFake{}
	cases := []struct {
		name, series string
		v            vesselView
		want         []string
	}{
		{"no house bank", `{"role":"house_bank_soc"}`, vesselView{}, []string{"house_bank_soc", "Settings → SignalK → Roles"}},
		{"unresolved bank", `{"role":"house_bank_current"}`, vesselView{HouseBankUnresolved: true}, []string{"house_bank_current", "Settings → SignalK → Roles"}},
		{"no port alternator", `{"role":"port_alternator_current"}`, vesselView{}, []string{"port_alternator_current", "Settings → SignalK → Roles"}},
		{"no engines", `{"role":"engine_rpm","engine":"port"}`, vesselView{}, []string{"engine_rpm", "Settings → SignalK → Roles"}},
		{"engine missing", `{"role":"engine_rpm"}`, tcVessel(), []string{"engine", "Port", "Starboard"}},
		{"engine unknown", `{"role":"engine_load","engine":"centre"}`, tcVessel(), []string{"centre", "Port", "Starboard"}},
		{"unknown role", `{"role":"keel_depth"}`, tcVessel(), []string{"keel_depth", "house_bank_soc"}},
	}
	for _, tc := range cases {
		d := tcDeps(f, tc.v, "metric")
		_, err := tcRun(t, d, fmt.Sprintf(`{"series":[%s],%s}`, tc.series, tcRange))
		if err == nil {
			t.Errorf("%s: expected an error", tc.name)
			continue
		}
		for _, w := range tc.want {
			if !strings.Contains(err.Error(), w) {
				t.Errorf("%s: error %q should contain %q", tc.name, err, w)
			}
		}
		if got := d.cards.take(""); got != nil {
			t.Errorf("%s: a refused call made a card", tc.name)
		}
	}
	if len(f.queried) != 0 {
		t.Errorf("nothing should be queried for a refused call: %v", f.queried)
	}
}

func TestShowTelemetryChart_RejectsBadArguments(t *testing.T) {
	f := &tcFake{series: map[string][]telemetryPoint{"a.b": tcHourly(tcStart(), 5, func(int) float64 { return 1 })}}
	both := `{"path":"a.b","role":"house_bank_soc"}`
	neither := `{"label":"x"}`
	one := `{"path":"a.b"}`
	cases := map[string]string{
		"both path and role": fmt.Sprintf(`{"series":[%s],%s}`, both, tcRange),
		"neither":            fmt.Sprintf(`{"series":[%s],%s}`, neither, tcRange),
		"no series":          fmt.Sprintf(`{"series":[],%s}`, tcRange),
		"five series":        fmt.Sprintf(`{"series":[%s,%s,%s,%s,%s],%s}`, one, one, one, one, one, tcRange),
		"flux path":          fmt.Sprintf(`{"series":[{"path":"${x}"}],%s}`, tcRange),
		"flux source":        fmt.Sprintf(`{"series":[{"path":"a.b","source":"{x"}],%s}`, tcRange),
		"bad from":           `{"series":[{"path":"a.b"}],"from":"yesterday","to":"2026-10-19T00:00:00Z"}`,
		"to before from":     `{"series":[{"path":"a.b"}],"from":"2026-10-19T00:00:00Z","to":"2026-10-12T00:00:00Z"}`,
		"over 60 days":       `{"series":[{"path":"a.b"}],"from":"2026-07-01T00:00:00Z","to":"2026-10-19T00:00:00Z"}`,
		"future to":          `{"series":[{"path":"a.b"}],"from":"2026-10-18T00:00:00Z","to":"2026-10-20T00:00:00Z"}`,
		"four compare":       fmt.Sprintf(`{"series":[%s],%s,"compare":[%s,%s,%s,%s]}`, one, tcRange, tcCmp("a"), tcCmp("b"), tcCmp("c"), tcCmp("d")),
		"compare no label":   fmt.Sprintf(`{"series":[%s],%s,"compare":[{"from":"2026-10-12T00:00:00Z","to":"2026-10-13T00:00:00Z"}]}`, one, tcRange),
		"long label":         fmt.Sprintf(`{"series":[{"path":"a.b","label":%q}],%s}`, strings.Repeat("x", chartMaxLabelLen+1), tcRange),
		"unknown view":       fmt.Sprintf(`{"series":[%s],%s,"view":"pie"}`, one, tcRange),
		"xy without x y":     fmt.Sprintf(`{"series":[%s,%s],%s,"view":"xy"}`, one, one, tcRange),
		"xy same index":      fmt.Sprintf(`{"series":[%s,%s],%s,"view":"xy","x":0,"y":0}`, one, one, tcRange),
		"xy index range":     fmt.Sprintf(`{"series":[%s,%s],%s,"view":"xy","x":0,"y":2}`, one, one, tcRange),
		"x on a time view":   fmt.Sprintf(`{"series":[%s],%s,"x":0,"y":1}`, one, tcRange),
	}
	for name, args := range cases {
		d := tcDeps(f, tcVessel(), "metric")
		if _, err := tcRun(t, d, args); err == nil {
			t.Errorf("%s: expected an error", name)
		}
	}
}

func tcCmp(label string) string {
	return fmt.Sprintf(`{"label":%q,"from":"2026-10-12T00:00:00Z","to":"2026-10-13T00:00:00Z"}`, label)
}

func TestShowTelemetryChart_MoreThanOneSourceIsRefusedNamingThem(t *testing.T) {
	f := &tcFake{
		series:  map[string][]telemetryPoint{tcAltStbd: tcHourly(tcStart(), 24, func(int) float64 { return 30 })},
		sources: map[string][]string{tcAltStbd: {"venus.c0619ab9d0ee", "can0.12"}},
	}
	d := tcDeps(f, tcVessel(), "metric")
	_, err := tcRun(t, d, fmt.Sprintf(`{"series":[{"role":"starboard_alternator_current"}],%s}`, tcRange))
	if err == nil || !strings.Contains(err.Error(), "venus.c0619ab9d0ee") || !strings.Contains(err.Error(), "can0.12") {
		t.Fatalf("error should name both sources: %v", err)
	}
	if len(f.queried) != 0 {
		t.Errorf("no history should be read before the source is chosen: %v", f.queried)
	}
}

func TestShowTelemetryChart_AnExplicitSourcePassesThroughToTheQueryAndPayload(t *testing.T) {
	f := &tcFake{
		series:  map[string][]telemetryPoint{tcAltStbd + "|venus.c0619ab9d0ee": tcHourly(tcStart(), 24, func(int) float64 { return 30 })},
		sources: map[string][]string{tcAltStbd: {"venus.c0619ab9d0ee", "can0.12"}},
	}
	d := tcDeps(f, tcVessel(), "metric")
	res, err := tcRun(t, d, fmt.Sprintf(`{"series":[{"role":"starboard_alternator_current","source":"venus.c0619ab9d0ee"}],%s}`, tcRange))
	if err != nil {
		t.Fatal(err)
	}
	p := tcPayload(t, d, res)
	if p.Series[0].Source == nil || *p.Series[0].Source != "venus.c0619ab9d0ee" {
		t.Errorf("source not in payload: %+v", p.Series[0])
	}
	for _, q := range f.queried {
		if q != tcAltStbd+"|venus.c0619ab9d0ee" {
			t.Errorf("queried %q, want the explicit source only", q)
		}
	}
}

func TestShowTelemetryChart_ASingleSourceNeedsNoChoiceAndPayloadSourceIsNull(t *testing.T) {
	f := &tcFake{
		series:  map[string][]telemetryPoint{tcAltStbd: tcHourly(tcStart(), 24, func(int) float64 { return 30 })},
		sources: map[string][]string{tcAltStbd: {"venus.c0619ab9d0ee"}},
	}
	d := tcDeps(f, tcVessel(), "metric")
	res, err := tcRun(t, d, fmt.Sprintf(`{"series":[{"role":"starboard_alternator_current"}],%s}`, tcRange))
	if err != nil {
		t.Fatal(err)
	}
	cards := d.takeCards(res)
	if !strings.Contains(string(cards[0].Payload), `"source":null`) {
		t.Errorf("payload source should be null when none was chosen: %s", cards[0].Payload)
	}
}

func TestShowTelemetryChart_NoDataInADrawnRangeIsAnErrorNamingSeriesAndRange(t *testing.T) {
	// Logged for the first day only.
	f := &tcFake{series: map[string][]telemetryPoint{
		tcSoc:     tcHourly(tcStart(), 48, func(int) float64 { return 0.5 }),
		tcAltStbd: tcHourly(tcStart(), 24, func(int) float64 { return 30 }),
	}}
	d := tcDeps(f, tcVessel(), "metric")
	args := fmt.Sprintf(`{"series":[{"role":"house_bank_soc"},{"role":"starboard_alternator_current"}],%s,
		"compare":[{"label":"Before","from":"2026-10-12T00:00:00Z","to":"2026-10-13T00:00:00Z"},{"label":"After","from":"2026-10-13T00:00:00Z","to":"2026-10-14T00:00:00Z"}]}`, tcRange)
	_, err := tcRun(t, d, args)
	if err == nil {
		t.Fatal("expected an error")
	}
	for _, w := range []string{"Starboard alternator current", "After"} {
		if !strings.Contains(err.Error(), w) {
			t.Errorf("error %q should contain %q", err, w)
		}
	}
	if strings.Contains(err.Error(), "House bank") {
		t.Errorf("the series with data must not be blamed: %v", err)
	}
	if got := d.cards.take(""); got != nil {
		t.Error("a refused call made a card")
	}
}

func TestShowTelemetryChart_NoDataInTheMainRangeWithoutCompareIsAnError(t *testing.T) {
	f := &tcFake{series: map[string][]telemetryPoint{}}
	d := tcDeps(f, tcVessel(), "metric")
	_, err := tcRun(t, d, fmt.Sprintf(`{"series":[{"path":"environment.depth.belowKeel"}],%s}`, tcRange))
	if err == nil || !strings.Contains(err.Error(), "Depth below keel") {
		t.Fatalf("got %v", err)
	}
}

func TestShowTelemetryChart_SummaryIsInDisplayUnits(t *testing.T) {
	f := &tcFake{series: map[string][]telemetryPoint{
		tcRPMPort:                     tcHourly(tcStart(), 3, func(i int) float64 { return 20 }), // 20 Hz = 1200 rpm
		tcSoc:                         tcHourly(tcStart(), 3, func(i int) float64 { return 0.85 }),
		"propulsion.port.temperature": tcHourly(tcStart(), 3, func(i int) float64 { return 353.15 }), // 80 C
		"environment.depth.belowKeel": tcHourly(tcStart(), 3, func(i int) float64 { return 4 }),
	}}
	d := tcDeps(f, tcVessel(), "metric")
	// 4 series, 4 units would be too many axes; chart them in pairs.
	for _, tc := range []struct {
		series     string
		wantUnits  []string
		wantMeans  []float64
		wantMinMax [][2]float64
	}{
		{`{"role":"engine_rpm","engine":"port"},{"role":"house_bank_soc"}`, []string{"rpm", "%"}, []float64{1200, 85}, [][2]float64{{1140, 1260}, {-15, 185}}},
		{`{"role":"engine_coolant_temperature","engine":"port"},{"path":"environment.depth.belowKeel"}`, []string{"°C", "m"}, []float64{80, 4}, [][2]float64{{79, 81}, {3, 5}}},
	} {
		res, err := tcRun(t, d, fmt.Sprintf(`{"series":[%s],%s}`, tc.series, tcRange))
		if err != nil {
			t.Fatal(err)
		}
		var m chartModelResult
		if err := json.Unmarshal([]byte(res), &m); err != nil {
			t.Fatalf("%v: %s", err, res)
		}
		for i := range tc.wantUnits {
			s := m.Series[i]
			r := s.Ranges[0]
			if s.Unit != tc.wantUnits[i] || r.Mean == nil || math.Abs(*r.Mean-tc.wantMeans[i]) > 0.01 ||
				math.Abs(*r.Min-tc.wantMinMax[i][0]) > 0.01 || math.Abs(*r.Max-tc.wantMinMax[i][1]) > 0.01 {
				t.Errorf("series %d: unit %q %+v, want %q mean %v minmax %v", i, s.Unit, r, tc.wantUnits[i], tc.wantMeans[i], tc.wantMinMax[i])
			}
		}
		_ = tcPayload(t, d, res)
	}
}

func TestShowTelemetryChart_ImperialUnitsFollowTheSetting(t *testing.T) {
	f := &tcFake{series: map[string][]telemetryPoint{
		"propulsion.port.temperature": tcHourly(tcStart(), 3, func(int) float64 { return 373.15 }), // 100 C = 212 F
	}}
	d := tcDeps(f, tcVessel(), "imperial")
	res, err := tcRun(t, d, fmt.Sprintf(`{"series":[{"role":"engine_coolant_temperature","engine":"port"}],%s}`, tcRange))
	if err != nil {
		t.Fatal(err)
	}
	p := tcPayload(t, d, res)
	var m chartModelResult
	_ = json.Unmarshal([]byte(res), &m)
	if p.Series[0].Unit != "°F" || math.Abs(*m.Series[0].Ranges[0].Mean-212) > 0.01 {
		t.Errorf("unit %q summary %+v", p.Series[0].Unit, m.Series[0].Ranges[0])
	}
}

func TestShowTelemetryChart_AxesFollowUnitsAndAThirdUnitIsRefused(t *testing.T) {
	f := &tcFake{series: map[string][]telemetryPoint{
		tcSoc:                             tcHourly(tcStart(), 5, func(int) float64 { return 0.5 }),
		tcAltStbd:                         tcHourly(tcStart(), 5, func(int) float64 { return 30 }),
		"electrical.alternator.2.current": tcHourly(tcStart(), 5, func(int) float64 { return 20 }),
		tcRPMPort:                         tcHourly(tcStart(), 5, func(int) float64 { return 20 }),
	}}
	d := tcDeps(f, tcVessel(), "metric")
	res, err := tcRun(t, d, fmt.Sprintf(`{"series":[{"role":"starboard_alternator_current"},{"role":"house_bank_soc"},{"role":"port_alternator_current"}],%s}`, tcRange))
	if err != nil {
		t.Fatal(err)
	}
	p := tcPayload(t, d, res)
	if got := []string{p.Series[0].Axis, p.Series[1].Axis, p.Series[2].Axis}; got[0] != "left" || got[1] != "right" || got[2] != "left" {
		t.Errorf("axes = %v, want amps left, percent right, amps left", got)
	}

	d = tcDeps(f, tcVessel(), "metric")
	_, err = tcRun(t, d, fmt.Sprintf(`{"series":[{"role":"starboard_alternator_current"},{"role":"house_bank_soc"},{"role":"engine_rpm","engine":"port"}],%s}`, tcRange))
	if err == nil || !strings.Contains(err.Error(), "two") {
		t.Fatalf("a third unit must be refused: %v", err)
	}
	if got := d.cards.take(""); got != nil {
		t.Error("a refused call made a card")
	}
}

func TestShowTelemetryChart_XYBinsMeanYOverX(t *testing.T) {
	// x: 10..100 % in ten hourly steps; y = 0,10,..90 A.
	f := &tcFake{series: map[string][]telemetryPoint{
		tcSoc:     tcHourly(tcStart(), 10, func(i int) float64 { return 0.1 * float64(i+1) }),
		tcAltStbd: tcHourly(tcStart(), 10, func(i int) float64 { return float64(i * 10) }),
	}}
	d := tcDeps(f, tcVessel(), "metric")
	res, err := tcRun(t, d, fmt.Sprintf(`{"series":[{"role":"starboard_alternator_current"},{"role":"house_bank_soc"}],%s,"view":"xy","x":1,"y":0}`, tcRange))
	if err != nil {
		t.Fatal(err)
	}
	var m chartModelResult
	if err := json.Unmarshal([]byte(res), &m); err != nil {
		t.Fatal(err)
	}
	if m.XY == nil || len(m.XY.Ranges) != 1 {
		t.Fatalf("xy summary: %s", res)
	}
	r := m.XY.Ranges[0]
	if r.Paired != 10 || r.Buckets != 10 {
		t.Errorf("paired %d of %d, want 10 of 10", r.Paired, r.Buckets)
	}
	wantN := []int{2, 1, 1, 1, 1, 1, 1, 2}
	wantY := []float64{5, 20, 30, 40, 50, 60, 70, 85}
	if len(r.Bins) != 8 {
		t.Fatalf("bins = %+v", r.Bins)
	}
	for i, b := range r.Bins {
		if b.N != wantN[i] || math.Abs(b.MeanY-wantY[i]) > 0.01 {
			t.Errorf("bin %d: n %d mean %v, want %d %v", i, b.N, b.MeanY, wantN[i], wantY[i])
		}
	}
	if r.Bins[0].XFrom != 10 || r.Bins[7].XTo != 100 {
		t.Errorf("bins should span the x range 10..100: %+v ... %+v", r.Bins[0], r.Bins[7])
	}
	p := tcPayload(t, d, res)
	if p.View != "xy" || p.XY == nil || p.XY.X != 1 || p.XY.Y != 0 {
		t.Errorf("payload xy: %+v", p)
	}
}

func TestShowTelemetryChart_XYDropsUnpairedBucketsAndOmitsEmptyBinsAndSharesBinsAcrossRanges(t *testing.T) {
	day2 := tcStart().Add(24 * time.Hour)
	soc := append(tcHourly(tcStart(), 4, func(i int) float64 { return 0.2 + 0.1*float64(i) }), // 20,30,40,50 %
		tcHourly(day2, 2, func(i int) float64 { return 0.9 + 0.1*float64(i) })...) // 90,100 %
	// y missing for the 3rd bucket of the first range.
	alt := []telemetryPoint{
		{Timestamp: tcStart(), Value: 10}, {Timestamp: tcStart().Add(time.Hour), Value: 20}, {Timestamp: tcStart().Add(3 * time.Hour), Value: 40},
		{Timestamp: day2, Value: 90}, {Timestamp: day2.Add(time.Hour), Value: 100},
	}
	f := &tcFake{series: map[string][]telemetryPoint{tcSoc: soc, tcAltStbd: alt}}
	d := tcDeps(f, tcVessel(), "metric")
	args := fmt.Sprintf(`{"series":[{"role":"house_bank_soc"},{"role":"starboard_alternator_current"}],%s,"view":"xy","x":0,"y":1,
		"compare":[{"label":"Before","from":"2026-10-12T00:00:00Z","to":"2026-10-13T00:00:00Z"},{"label":"After","from":"2026-10-13T00:00:00Z","to":"2026-10-14T00:00:00Z"}]}`, tcRange)
	res, err := tcRun(t, d, args)
	if err != nil {
		t.Fatal(err)
	}
	var m chartModelResult
	_ = json.Unmarshal([]byte(res), &m)
	before, after := m.XY.Ranges[0], m.XY.Ranges[1]
	if before.Range != "Before" || before.Buckets != 4 || before.Paired != 3 {
		t.Errorf("before: %+v", before)
	}
	if after.Range != "After" || after.Paired != 2 {
		t.Errorf("after: %+v", after)
	}
	// Union x is 20..100: edges at 10-point steps of 10 (8 bins of width 10).
	if len(before.Bins) != 3 || before.Bins[0].XFrom != 20 || before.Bins[0].XTo != 30 {
		t.Errorf("before bins (empty bins omitted, edges from the union): %+v", before.Bins)
	}
	if len(after.Bins) != 1 || after.Bins[0].N != 2 || after.Bins[0].XTo != 100 || after.Bins[0].MeanY != 95 {
		t.Errorf("after bins: %+v", after.Bins)
	}
}

func TestShowTelemetryChart_ComparisonRangesAreInThePayloadInTheBoatsTimezone(t *testing.T) {
	f := &tcFake{series: map[string][]telemetryPoint{tcAltStbd: tcHourly(tcStart(), 72, func(int) float64 { return 30 })}}
	d := tcDeps(f, tcVessel(), "metric")
	args := fmt.Sprintf(`{"series":[{"role":"starboard_alternator_current","label":"Stbd alt"}],%s,"title":"Alternator before and after",
		"compare":[{"label":"Before regulator change","from":"2026-10-12T00:00:00Z","to":"2026-10-13T00:00:00Z"},{"label":"After","from":"2026-10-14T00:00:00Z","to":"2026-10-15T00:00:00Z"}]}`, tcRange)
	res, err := tcRun(t, d, args)
	if err != nil {
		t.Fatal(err)
	}
	p := tcPayload(t, d, res)
	if p.Title != "Alternator before and after" || p.View != "time" || p.XY != nil || p.Timezone != "Etc/GMT-10" {
		t.Errorf("payload header: %+v", p)
	}
	if p.Series[0].Label != "Stbd alt" || p.Series[0].Unit != "A" || p.Series[0].Axis != "left" || p.Series[0].Path != tcAltStbd {
		t.Errorf("series: %+v", p.Series[0])
	}
	if len(p.Compare) != 2 || p.Compare[0].Label != "Before regulator change" || p.Compare[1].From != "2026-10-14T10:00:00+10:00" {
		t.Errorf("compare: %+v", p.Compare)
	}
	if p.Range.From != "2026-10-12T10:00:00+10:00" || p.Range.To != "2026-10-19T10:00:00+10:00" {
		t.Errorf("range: %+v", p.Range)
	}
	var m chartModelResult
	_ = json.Unmarshal([]byte(res), &m)
	if len(m.Series[0].Ranges) != 2 || m.Series[0].Ranges[1].Range != "After" {
		t.Errorf("summary should be per comparison range: %+v", m.Series[0].Ranges)
	}
}

func TestShowTelemetryChart_EmptyCompareIsAnArrayAndToIsClampedAtNow(t *testing.T) {
	f := &tcFake{series: map[string][]telemetryPoint{tcAltStbd: tcHourly(tcStart(), 24, func(int) float64 { return 30 })}}
	d := tcDeps(f, tcVessel(), "metric")
	res, err := tcRun(t, d, `{"series":[{"role":"starboard_alternator_current"}],"from":"2026-10-12T00:00:00Z","to":"2026-10-19T12:02:00Z"}`)
	if err != nil {
		t.Fatal(err)
	}
	cards := d.takeCards(res)
	if !strings.Contains(string(cards[0].Payload), `"compare":[]`) {
		t.Errorf("compare must be an empty array: %s", cards[0].Payload)
	}
	var p telemetryChartPayload
	_ = json.Unmarshal(cards[0].Payload, &p)
	if p.Range.To != "2026-10-19T22:00:00+10:00" {
		t.Errorf("to should be clamped at now (12:00Z): %s", p.Range.To)
	}
}

func TestShowTelemetryChart_OneCardAndAShortResultThatTellsTheModelNotToTabulate(t *testing.T) {
	f := &tcFake{series: map[string][]telemetryPoint{
		tcSoc:     tcHourly(tcStart(), 168, func(i int) float64 { return 0.3 + 0.001*float64(i) }),
		tcAltStbd: tcHourly(tcStart(), 168, func(i int) float64 { return float64(i % 40) }),
	}}
	d := tcDeps(f, tcVessel(), "metric")
	res, err := tcRun(t, d, fmt.Sprintf(`{"series":[{"role":"house_bank_soc"},{"role":"starboard_alternator_current"}],%s}`, tcRange))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(res, "A chart card is attached; describe what it shows and what changed — do not list the numbers as a table.") {
		t.Errorf("instruction missing: %s", res)
	}
	if len(res) > assistantMaxToolResultChars {
		t.Errorf("result %d chars is over the cap", len(res))
	}
	if cards := d.takeCards(res); len(cards) != 1 {
		t.Fatalf("cards = %d", len(cards))
	}
	if cards := d.takeCards(res); len(cards) != 0 {
		t.Error("the card must be handed out once")
	}
}

func TestShowTelemetryChart_TheLargestSummaryFitsTheCap(t *testing.T) {
	f := &tcFake{series: map[string][]telemetryPoint{
		tcSoc:                             tcHourly(tcStart(), 168, func(i int) float64 { return 0.3 + 0.004*float64(i%100) }),
		tcAltStbd:                         tcHourly(tcStart(), 168, func(i int) float64 { return float64(i % 40) }),
		"electrical.alternator.2.current": tcHourly(tcStart(), 168, func(i int) float64 { return float64(i % 30) }),
		"electrical.batteries.1.current":  tcHourly(tcStart(), 168, func(i int) float64 { return float64(i%50) - 20 }),
	}}
	var compare []string
	for i := 0; i < 3; i++ {
		compare = append(compare, fmt.Sprintf(`{"label":%q,"from":"2026-10-%02dT00:00:00Z","to":"2026-10-%02dT00:00:00Z"}`, strings.Repeat("r", chartMaxLabelLen-1)+fmt.Sprint(i), 12+i, 13+i))
	}
	long := strings.Repeat("l", chartMaxLabelLen)
	d := tcDeps(f, tcVessel(), "metric")
	res, err := tcRun(t, d, fmt.Sprintf(`{"series":[{"role":"house_bank_soc","label":%q},{"role":"starboard_alternator_current","label":%q},{"role":"port_alternator_current","label":%q},{"role":"house_bank_current","label":%q}],%s,"view":"xy","x":0,"y":1,"compare":[%s]}`,
		long, long, long, long, tcRange, strings.Join(compare, ",")))
	if err != nil {
		t.Fatal(err)
	}
	if len(res) > assistantMaxToolResultChars || !strings.Contains(res, "do not list the numbers") {
		t.Errorf("len %d: %s", len(res), res)
	}
}

func TestChartModelResult_ShrinkDropsDetailLastFirstAndEventuallyStops(t *testing.T) {
	m := &chartModelResult{Instruction: chartInstruction}
	one, two := 1.0, 2.0
	for i := 0; i < 2; i++ {
		m.Series = append(m.Series, chartModelSeries{Label: "s", Ranges: []chartModelRange{{Range: "r", Min: &one, Mean: &two, Max: &two, Buckets: 3}}})
	}
	m.XY = &chartModelXY{Ranges: []chartModelXYRange{{Range: "r", Bins: []chartModelBin{{N: 1}}}}}
	n := 0
	for m.shrink() {
		n++
		if n > 100 {
			t.Fatal("shrink never finishes")
		}
	}
	if n == 0 || m.Series[1].Ranges[0].Min != nil || len(m.XY.Ranges[0].Bins) != 0 {
		t.Errorf("shrink did not trim: n=%d %+v", n, m)
	}
	if m.Series[0].Ranges[0].Mean == nil {
		t.Error("the mean is never dropped")
	}
}

func TestBuildInfluxPathSourcesFlux(t *testing.T) {
	q, err := buildInfluxPathSourcesFlux("b", "value", "electrical.alternator.12.current", tcStart(), tcNow)
	if err != nil {
		t.Fatal(err)
	}
	for _, w := range []string{`r._measurement == "electrical.alternator.12.current"`, `distinct(column: "source")`, "2026-10-12T00:00:00Z"} {
		if !strings.Contains(q, w) {
			t.Errorf("flux %q missing %q", q, w)
		}
	}
	if _, err := buildInfluxPathSourcesFlux("b", "value", "a\"; drop", tcStart(), tcNow); err == nil {
		t.Log("quote in path is escaped by fluxStringLiteral")
	}
}

func TestShowTelemetryChart_DeclaredFlowUnitConvertsToLitresPerHour(t *testing.T) {
	f := &tcFake{series: map[string][]telemetryPoint{
		"propulsion.port.fuel.rate": tcHourly(tcStart(), 3, func(int) float64 { return 1e-6 }), // 3.6 L/h
	}}
	d := tcDeps(f, tcVessel(), "metric")
	res, err := tcRun(t, d, fmt.Sprintf(`{"series":[{"path":"propulsion.port.fuel.rate"}],%s}`, tcRange))
	if err != nil {
		t.Fatal(err)
	}
	var m chartModelResult
	_ = json.Unmarshal([]byte(res), &m)
	if m.Series[0].Unit != "L/h" || math.Abs(*m.Series[0].Ranges[0].Mean-3.6) > 0.01 {
		t.Errorf("unit %q %+v", m.Series[0].Unit, m.Series[0].Ranges[0])
	}
}

func TestShowTelemetryChart_UndeclaredUnitIsNeitherGuessedNorConverted(t *testing.T) {
	// Named like a temperature, but the boat declares no unit: raw values.
	f := &tcFake{series: map[string][]telemetryPoint{
		"environment.inside.temperature": tcHourly(tcStart(), 3, func(int) float64 { return 295 }),
	}}
	d := tcDeps(f, tcVessel(), "metric")
	res, err := tcRun(t, d, fmt.Sprintf(`{"series":[{"path":"environment.inside.temperature"}],%s}`, tcRange))
	if err != nil {
		t.Fatal(err)
	}
	var m chartModelResult
	_ = json.Unmarshal([]byte(res), &m)
	p := tcPayload(t, d, res)
	if p.Series[0].Unit != "" || !strings.HasSuffix(p.Series[0].Label, " (unit not reported)") {
		t.Errorf("card series %+v", p.Series[0])
	}
	if m.Series[0].Unit != "" || !strings.HasSuffix(m.Series[0].Label, " (unit not reported)") || math.Abs(*m.Series[0].Ranges[0].Mean-295) > 0.01 {
		t.Errorf("model series %+v %+v", m.Series[0], m.Series[0].Ranges[0])
	}
}

func TestShowTelemetryChart_EachUndeclaredSeriesIsItsOwnAxisGroup(t *testing.T) {
	f := &tcFake{series: map[string][]telemetryPoint{
		"a.b": tcHourly(tcStart(), 3, func(int) float64 { return 1 }),
		"c.d": tcHourly(tcStart(), 3, func(int) float64 { return 2 }),
		"e.f": tcHourly(tcStart(), 3, func(int) float64 { return 2 }),
		tcSoc: tcHourly(tcStart(), 3, func(int) float64 { return 0.5 }),
	}}
	d := tcDeps(f, tcVessel(), "metric")
	// Two undeclared series are two groups: left and right.
	res, err := tcRun(t, d, fmt.Sprintf(`{"series":[{"path":"a.b"},{"path":"c.d"}],%s}`, tcRange))
	if err != nil {
		t.Fatal(err)
	}
	p := tcPayload(t, d, res)
	if p.Series[0].Axis != "left" || p.Series[1].Axis != "right" {
		t.Errorf("axes = %s %s, want left right", p.Series[0].Axis, p.Series[1].Axis)
	}
	// A third group, declared or not, is refused.
	d = tcDeps(f, tcVessel(), "metric")
	if _, err := tcRun(t, d, fmt.Sprintf(`{"series":[{"path":"a.b"},{"role":"house_bank_soc"},{"path":"c.d"}],%s}`, tcRange)); err == nil || !strings.Contains(err.Error(), "two") {
		t.Fatalf("a third group must be refused: %v", err)
	}
	d = tcDeps(f, tcVessel(), "metric")
	if _, err := tcRun(t, d, fmt.Sprintf(`{"series":[{"path":"a.b"},{"path":"c.d"},{"path":"e.f"}],%s}`, tcRange)); err == nil || !strings.Contains(err.Error(), "two") {
		t.Fatalf("a third undeclared series must be refused: %v", err)
	}
}

func TestShowTelemetryChart_PayloadCarriesTheDeclaredSIUnit(t *testing.T) {
	f := &tcFake{series: map[string][]telemetryPoint{
		tcRPMPort: tcHourly(tcStart(), 3, func(int) float64 { return 20 }),
		"a.b":     tcHourly(tcStart(), 3, func(int) float64 { return 1 }),
	}}
	d := tcDeps(f, tcVessel(), "metric")
	res, err := tcRun(t, d, fmt.Sprintf(`{"series":[{"role":"engine_rpm","engine":"port"},{"path":"a.b"}],%s}`, tcRange))
	if err != nil {
		t.Fatal(err)
	}
	p := tcPayload(t, d, res)
	if p.Series[0].SIUnit != "Hz" || p.Series[0].Unit != "rpm" || p.Series[1].SIUnit != "" {
		t.Errorf("series %+v %+v", p.Series[0], p.Series[1])
	}
}

func TestShowTelemetryChart_MultiSourceCheckIsPerDrawnRange(t *testing.T) {
	// The second device logged only between the two compare ranges.
	f := &tcFake{
		series: map[string][]telemetryPoint{tcAltStbd: tcHourly(tcStart(), 24*10, func(int) float64 { return 30 })},
		sourcesIn: func(_ string, from, to time.Time) []string {
			gapFrom, gapTo := tcStart().AddDate(0, 0, 3), tcStart().AddDate(0, 0, 4)
			if from.Before(gapTo) && to.After(gapFrom) {
				return []string{"venus.x", "can0.12"}
			}
			return []string{"venus.x"}
		},
	}
	d := tcDeps(f, tcVessel(), "metric")
	args := `{"series":[{"role":"starboard_alternator_current"}],` + tcRange + `,"compare":[` +
		`{"label":"Before","from":"2026-10-12T00:00:00Z","to":"2026-10-13T00:00:00Z"},` +
		`{"label":"After","from":"2026-10-18T00:00:00Z","to":"2026-10-19T00:00:00Z"}]}`
	if _, err := tcRun(t, d, args); err != nil {
		t.Fatalf("the second device is outside every drawn range: %v", err)
	}
	// A range that does include it is still refused.
	d = tcDeps(f, tcVessel(), "metric")
	args = `{"series":[{"role":"starboard_alternator_current"}],` + tcRange + `,"compare":[` +
		`{"label":"Before","from":"2026-10-12T00:00:00Z","to":"2026-10-16T00:00:00Z"}]}`
	if _, err := tcRun(t, d, args); err == nil || !strings.Contains(err.Error(), "can0.12") {
		t.Fatalf("want refusal naming can0.12: %v", err)
	}
}

func TestShowTelemetryChart_HistoryReadsAreConcurrentBoundedAndOrderStable(t *testing.T) {
	paths := []string{"a.one", "b.two", "c.three", "d.four"}
	f := &tcFake{series: map[string][]telemetryPoint{}}
	for i, p := range paths {
		f.series[p] = tcHourly(tcStart(), 24*7, func(h int) float64 { return float64(i*100 + h) })
	}
	var inFlight, peak int32
	var mu sync.Mutex
	f.onStat = func(_ context.Context, _ string, _ time.Time) {
		mu.Lock()
		inFlight++
		if int(inFlight) > int(peak) {
			peak = inFlight
		}
		mu.Unlock()
		time.Sleep(5 * time.Millisecond)
		mu.Lock()
		inFlight--
		mu.Unlock()
	}
	args := `{"series":[{"path":"a.one"},{"path":"b.two"}],` + tcRange + `,"compare":[` + tcCmp("A") + `,` + tcCmp("B") + `,` + tcCmp("C") + `]}`
	var first string
	for i := 0; i < 5; i++ {
		d := tcDeps(f, tcVessel(), "metric")
		res, err := tcRun(t, d, args)
		if err != nil {
			t.Fatal(err)
		}
		if i == 0 {
			first = res
		} else if res != first {
			t.Fatalf("run %d differs:\n%s\n%s", i, res, first)
		}
	}
	// 2 series x 3 ranges = 6 jobs, 3 stat calls each, all independent: they overlap.
	if peak < 4 {
		t.Errorf("peak concurrent stat calls = %d, reads should overlap", peak)
	}
	if peak > 8*3 {
		t.Errorf("peak concurrent stat calls = %d, jobs are bounded at 8", peak)
	}
}

func TestShowTelemetryChart_FirstErrorIsFirstByOrderNotByTime(t *testing.T) {
	f := &tcFake{series: map[string][]telemetryPoint{}}
	d := tcDeps(f, tcVessel(), "metric")
	d.influxPathHistoryStat = func(_ context.Context, path, _ string, _, _ time.Time, _, _ string) ([]telemetryPoint, error) {
		if path == "a.one" {
			time.Sleep(30 * time.Millisecond)
			return nil, fmt.Errorf("boom-a")
		}
		return nil, fmt.Errorf("boom-b")
	}
	_, err := tcRun(t, d, fmt.Sprintf(`{"series":[{"path":"a.one"},{"path":"b.two"}],%s}`, tcRange))
	if err == nil || !strings.Contains(err.Error(), "boom-a") {
		t.Fatalf("want the first error by series order, got %v", err)
	}
}

func TestShowTelemetryChart_CancelStopsFurtherReads(t *testing.T) {
	f := &tcFake{series: map[string][]telemetryPoint{}}
	var mu sync.Mutex
	started := map[string]bool{}
	f.onStat = func(ctx context.Context, path string, start time.Time) {
		mu.Lock()
		started[path+start.String()] = true
		mu.Unlock()
		<-ctx.Done() // every read hangs until the call is cancelled
	}
	d := tcDeps(f, tcVessel(), "metric")
	d.siUnit = func(string) string { return "A" }
	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		time.Sleep(50 * time.Millisecond)
		cancel()
	}()
	// 4 series x 3 ranges = 12 jobs, bounded at 8.
	cmp := tcCmp("A") + `,` + `{"label":"B","from":"2026-10-14T00:00:00Z","to":"2026-10-15T00:00:00Z"},{"label":"C","from":"2026-10-16T00:00:00Z","to":"2026-10-17T00:00:00Z"}`
	args := `{"series":[{"path":"a.one"},{"path":"b.two"},{"path":"c.three"},{"path":"d.four"}],` + tcRange + `,"compare":[` + cmp + `]}`
	_, err := d.execute(ctx, "show_telemetry_chart", json.RawMessage(args))
	if err == nil || !strings.Contains(err.Error(), "context canceled") {
		t.Fatalf("want a cancellation error, got %v", err)
	}
	mu.Lock()
	defer mu.Unlock()
	if len(started) > 8 {
		t.Errorf("%d reads started; after cancel no further read may start (bound is 8)", len(started))
	}
}

func TestShowTelemetryChart_LabelWithTheUnitNoteStaysWithinTheLimit(t *testing.T) {
	f := &tcFake{series: map[string][]telemetryPoint{"a.b": tcHourly(tcStart(), 3, func(int) float64 { return 1 })}}
	d := tcDeps(f, tcVessel(), "metric")
	long := strings.Repeat("x", chartMaxLabelLen) // fits alone, not with the note
	if _, err := tcRun(t, d, fmt.Sprintf(`{"series":[{"path":"a.b","label":%q}],%s}`, long, tcRange)); err == nil || !strings.Contains(err.Error(), "label") {
		t.Fatalf("want a label-length error: %v", err)
	}
	d = tcDeps(f, tcVessel(), "metric")
	short := strings.Repeat("x", chartMaxLabelLen-len(" (unit not reported)"))
	res, err := tcRun(t, d, fmt.Sprintf(`{"series":[{"path":"a.b","label":%q}],%s}`, short, tcRange))
	if err != nil {
		t.Fatal(err)
	}
	if l := len([]rune(tcPayload(t, d, res).Series[0].Label)); l > chartMaxLabelLen {
		t.Errorf("stored label is %d runes", l)
	}
}

func TestShowTelemetryChart_EngineWithoutAnInstanceSaysSo(t *testing.T) {
	v := tcVessel()
	v.Engines = []vesselEngineSetting{{Instance: "", Name: "Port"}}
	d := tcDeps(&tcFake{}, v, "metric")
	_, err := tcRun(t, d, fmt.Sprintf(`{"series":[{"role":"engine_rpm","engine":"Port"}],%s}`, tcRange))
	want := `engine "Port" has no instance assigned; set it in Settings → SignalK → Roles`
	if err == nil || !strings.Contains(err.Error(), want) || strings.Contains(err.Error(), "not one of") {
		t.Fatalf("got %v, want %q", err, want)
	}
}
