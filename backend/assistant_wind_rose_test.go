package main

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"strings"
	"testing"
	"time"
)

func TestWindRoseSectorOfCentresNorthOnSectorZero(t *testing.T) {
	cases := []struct {
		deg     float64
		sectors int
		want    int
	}{
		{0, 16, 0},
		{348.75, 16, 0}, // the lower edge of the north sector
		{11.24, 16, 0},
		{11.25, 16, 1}, // the upper edge belongs to the next sector
		{90, 16, 4},
		{180, 16, 8},
		{359.9, 16, 0},
		{45, 8, 1},
		{22.4, 8, 0},
		{22.5, 8, 1},
		{67.4, 8, 1},
		{67.5, 8, 2},
		{5.7, 32, 1},
	}
	for _, c := range cases {
		if got := windRoseSectorOf(c.deg, c.sectors); got != c.want {
			t.Errorf("%v deg in %d sectors = %d, want %d", c.deg, c.sectors, got, c.want)
		}
	}
}

func TestWindRoseBinOfUsesHalfOpenEdges(t *testing.T) {
	edges := []float64{5, 10, 15}
	cases := []struct {
		speed float64
		want  int
	}{{0, 0}, {4.99, 0}, {5, 1}, {9.99, 1}, {10, 2}, {15, 3}, {60, 3}}
	for _, c := range cases {
		if got := windRoseBinOf(c.speed, edges); got != c.want {
			t.Errorf("%v kn = bin %d, want %d", c.speed, got, c.want)
		}
	}
}

func TestBuildWindRoseCountsSamplesIntoSectorsBinsCalmAndVariable(t *testing.T) {
	deg := func(d float64) *float64 { return &d }
	samples := []windRoseSample{
		{Dir: deg(90), Speed: 7},  // E, 5-10
		{Dir: deg(92), Speed: 12}, // E, 10-15
		{Dir: deg(180), Speed: 30},
		{Dir: deg(270), Speed: 0.4}, // calm: counted apart, whatever the direction
		{Dir: nil, Speed: 8},        // no mean direction
		{Dir: nil, Speed: 0.2},      // calm and no direction: calm
	}
	tally := buildWindRose(samples, 16, []float64{5, 10, 15, 20, 25}, 1)
	if tally.Samples != 6 || tally.Calm != 2 || tally.Variable != 1 {
		t.Errorf("tally = %+v", tally)
	}
	if tally.Counts[4][1] != 1 || tally.Counts[4][2] != 1 || tally.Counts[8][5] != 1 {
		t.Errorf("counts = %v", tally.Counts)
	}
	total := 0
	for _, row := range tally.Counts {
		for _, n := range row {
			total += n
		}
	}
	if total != 3 {
		t.Errorf("binned %d, want 3 (calm and variable are counted apart)", total)
	}
}

func TestWindRoseBucketWidthKeepsTheSampleCountBounded(t *testing.T) {
	cases := []struct {
		span time.Duration
		want time.Duration
	}{
		{24 * time.Hour, time.Minute},
		{3 * 24 * time.Hour, 5 * time.Minute},
		{7 * 24 * time.Hour, 10 * time.Minute},
		{60 * 24 * time.Hour, time.Hour},
	}
	for _, c := range cases {
		_, got := windRoseBucketWidth(c.span)
		if got != c.want {
			t.Errorf("%v: width %v, want %v", c.span, got, c.want)
		}
		if int(c.span/got) > windRoseMaxBuckets {
			t.Errorf("%v at %v makes %d samples, over %d", c.span, got, int(c.span/got), windRoseMaxBuckets)
		}
	}
}

// wrFake serves the wind: direction in radians, speed in m/s, one point per
// ten minutes from tcStart.
type wrFake struct {
	n       int
	dirDeg  func(i int) float64
	speedMS func(i int) float64
}

func (f wrFake) stat(_ context.Context, path, _ string, _, _ time.Time, _, fn string) ([]telemetryPoint, error) {
	var out []telemetryPoint
	for i := 0; i < f.n; i++ {
		ts := tcStart().Add(time.Duration(i) * 10 * time.Minute)
		switch {
		case path == windSpeedTruePath && fn == "mean":
			out = append(out, telemetryPoint{Timestamp: ts, Value: f.speedMS(i)})
		case path == windSpeedTruePath && fn == "max":
			out = append(out, telemetryPoint{Timestamp: ts, Value: f.speedMS(i) + 2})
		case path == windDirectionTruePath && fn == "sin_mean":
			out = append(out, telemetryPoint{Timestamp: ts, Value: math.Sin(rad(f.dirDeg(i)))})
		case path == windDirectionTruePath && fn == "cos_mean":
			out = append(out, telemetryPoint{Timestamp: ts, Value: math.Cos(rad(f.dirDeg(i)))})
		}
	}
	return out, nil
}

func wrDeps(f wrFake) assistantToolDeps {
	d := tcDeps(&tcFake{}, tcVessel(), "metric")
	d.influxPathHistoryStat = f.stat
	return d
}

const knMS = 0.514444

func wrRun(t *testing.T, d assistantToolDeps, args string) (string, error) {
	t.Helper()
	return d.execute(context.Background(), "show_wind_rose", json.RawMessage(args))
}

func wrCard(t *testing.T, d assistantToolDeps, res string) windRosePayload {
	t.Helper()
	cards := d.takeCards(res)
	if len(cards) != 1 || cards[0].Kind != "wind_rose" {
		t.Fatalf("cards = %+v", cards)
	}
	var p windRosePayload
	if err := json.Unmarshal(cards[0].Payload, &p); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestShowWindRoseBuildsASnapshotCardInTheOperatorsUnits(t *testing.T) {
	// 72 samples: the first 36 from the east at 12 kn, the rest from the south
	// at 22 kn; one of them a calm.
	f := wrFake{
		n:       72,
		dirDeg:  func(i int) float64 { return map[bool]float64{true: 90, false: 180}[i < 36] },
		speedMS: func(i int) float64 { return map[bool]float64{true: 12 * knMS, false: 22 * knMS}[i < 36] },
	}
	d := wrDeps(f)
	res, err := wrRun(t, d, fmt.Sprintf(`{"from":"2026-10-12T00:00:00Z","to":"2026-10-12T12:00:00Z"}`))
	if err != nil {
		t.Fatal(err)
	}
	p := wrCard(t, d, res)
	if p.Sectors != 16 || p.SpeedUnit != "kn" || p.Reference != "true" {
		t.Errorf("card = %+v", p)
	}
	if len(p.Bins) != 6 || p.Bins[0].From != 0 || p.Bins[0].To == nil || *p.Bins[0].To != 5 || p.Bins[5].To != nil {
		t.Errorf("bins = %+v, want 0-5 ... 25+ in knots", p.Bins)
	}
	if len(p.Counts) != 16 || len(p.Counts[0]) != 6 {
		t.Fatalf("counts shape %d x %d", len(p.Counts), len(p.Counts[0]))
	}
	if p.Counts[4][2] != 36 || p.Counts[8][4] != 36 {
		t.Errorf("counts = %v, want 36 from the east at 10-15 kn and 36 from the south at 20-25 kn", p.Counts)
	}
	if p.Samples != 72 || p.Calm != 0 || p.Variable != 0 || p.CalmBelow != 1 {
		t.Errorf("tally = %+v", p)
	}
	// The fake's bucket max is the mean plus 2 m/s: 22 kn + 3.89 kn.
	if p.Strongest == nil || math.Abs(p.Strongest.Speed-25.89) > 0.05 {
		t.Errorf("strongest = %+v, want the 25.9 kn gust", p.Strongest)
	}
	if p.Range.From == "" || p.Timezone == "" || p.Title == "" || p.BucketMinutes <= 0 {
		t.Errorf("card = %+v", p)
	}

	var m windRoseModelResult
	if err := json.Unmarshal([]byte(res), &m); err != nil {
		t.Fatal(err)
	}
	if len(m.Sectors) == 0 || m.Sectors[0].Compass != "E" || math.Abs(m.Sectors[0].Percent-50) > 0.1 {
		t.Errorf("model summary = %+v, want E first at 50%%", m.Sectors)
	}
	if !strings.Contains(m.Instruction, "do not") {
		t.Errorf("instruction = %q", m.Instruction)
	}
	if strings.Contains(res, "environment.wind") {
		t.Errorf("a result must not show raw paths: %s", res)
	}
}

func TestShowWindRoseHonoursSectorsAndSpeedBins(t *testing.T) {
	f := wrFake{n: 12, dirDeg: func(int) float64 { return 45 }, speedMS: func(int) float64 { return 12 * knMS }}
	d := wrDeps(f)
	res, err := wrRun(t, d, `{"from":"2026-10-12T00:00:00Z","to":"2026-10-12T04:00:00Z","sectors":8,"speed_bins":[10,20]}`)
	if err != nil {
		t.Fatal(err)
	}
	p := wrCard(t, d, res)
	if p.Sectors != 8 || len(p.Bins) != 3 || p.Counts[1][1] != 12 {
		t.Errorf("card = %+v", p)
	}
}

func TestShowWindRoseCountsCalmAndAWindWithNoMeanDirectionApart(t *testing.T) {
	f := wrFake{
		n: 4,
		// Sample 0 is calm, 1 has a direction, 2 and 3 are fine too.
		dirDeg:  func(int) float64 { return 90 },
		speedMS: func(i int) float64 { return []float64{0.2, 8, 8, 8}[i] * knMS },
	}
	d := wrDeps(f)
	res, err := wrRun(t, d, `{"from":"2026-10-12T00:00:00Z","to":"2026-10-12T01:00:00Z"}`)
	if err != nil {
		t.Fatal(err)
	}
	p := wrCard(t, d, res)
	if p.Samples != 4 || p.Calm != 1 || p.Counts[4][1] != 3 {
		t.Errorf("card = %+v", p)
	}
}

func TestShowWindRoseRefusesWhatItCannotChartTruthfully(t *testing.T) {
	good := wrFake{n: 6, dirDeg: func(int) float64 { return 90 }, speedMS: func(int) float64 { return 5 }}
	cases := []struct {
		name string
		args string
		mut  func(d *assistantToolDeps)
		want []string
	}{
		{"bad sectors", `{"from":"2026-10-12T00:00:00Z","to":"2026-10-12T01:00:00Z","sectors":12}`, nil, []string{"sectors"}},
		{"bins out of order", `{"from":"2026-10-12T00:00:00Z","to":"2026-10-12T01:00:00Z","speed_bins":[10,5]}`, nil, []string{"speed_bins"}},
		{"too many bins", `{"from":"2026-10-12T00:00:00Z","to":"2026-10-12T01:00:00Z","speed_bins":[1,2,3,4,5,6,7,8,9]}`, nil, []string{"speed_bins"}},
		{"range too long", `{"from":"2026-07-01T00:00:00Z","to":"2026-10-12T00:00:00Z"}`, nil, []string{"days"}},
		{"to before from", `{"from":"2026-10-12T02:00:00Z","to":"2026-10-12T01:00:00Z"}`, nil, []string{"after"}},
		{"bad time", `{"from":"yesterday","to":"2026-10-12T01:00:00Z"}`, nil, []string{"RFC3339"}},
		{"direction not declared", `{"from":"2026-10-12T00:00:00Z","to":"2026-10-12T01:00:00Z"}`,
			func(d *assistantToolDeps) {
				d.siUnit = func(p string) string { return map[string]string{windSpeedTruePath: "m/s"}[p] }
			}, []string{"direction", "unit"}},
		{"speed not metres per second", `{"from":"2026-10-12T00:00:00Z","to":"2026-10-12T01:00:00Z"}`,
			func(d *assistantToolDeps) {
				d.siUnit = func(p string) string {
					return map[string]string{windSpeedTruePath: "knots", windDirectionTruePath: "rad"}[p]
				}
			}, []string{"speed", "m/s"}},
		{"two direction sources", `{"from":"2026-10-12T00:00:00Z","to":"2026-10-12T01:00:00Z"}`,
			func(d *assistantToolDeps) {
				d.influxPathSources = func(_ context.Context, p string, _, _ time.Time) ([]string, error) {
					if p == windDirectionTruePath {
						return []string{"a", "b"}, nil
					}
					return nil, nil
				}
			}, []string{"direction_source", "a", "b"}},
		{"nothing logged", `{"from":"2026-10-12T00:00:00Z","to":"2026-10-12T01:00:00Z"}`,
			func(d *assistantToolDeps) {
				d.influxPathHistoryStat = func(context.Context, string, string, time.Time, time.Time, string, string) ([]telemetryPoint, error) {
					return nil, nil
				}
			}, []string{"no logged"}},
		{"no cards", `{"from":"2026-10-12T00:00:00Z","to":"2026-10-12T01:00:00Z"}`,
			func(d *assistantToolDeps) { d.cards = nil }, []string{"cards"}},
	}
	for _, c := range cases {
		d := wrDeps(good)
		d.siUnit = func(p string) string { return tcDeclared[p] }
		if c.mut != nil {
			c.mut(&d)
		}
		_, err := wrRun(t, d, c.args)
		if err == nil {
			t.Errorf("%s: expected an error", c.name)
			continue
		}
		for _, w := range c.want {
			if !strings.Contains(err.Error(), w) {
				t.Errorf("%s: error %q should contain %q", c.name, err, w)
			}
		}
		if d.cards != nil {
			if got := d.cards.take(""); got != nil {
				t.Errorf("%s: a refused call made a card", c.name)
			}
		}
	}
}

// The golden fixture is what the frontend decodes. It must be exactly the shape
// the Go structs write, field for field, so it cannot drift from the server.
func TestMateEventFixtureCardsMatchTheGoPayloadStructs(t *testing.T) {
	ev := mateFixtureMessage(t)
	seen := map[string]int{}
	for _, c := range ev.Message.Cards {
		dec := json.NewDecoder(strings.NewReader(string(c.Payload)))
		dec.DisallowUnknownFields()
		var err error
		switch c.Kind {
		case "wind_rose":
			var p windRosePayload
			err = dec.Decode(&p)
			if err == nil && (len(p.Counts) != p.Sectors || len(p.Counts[0]) != len(p.Bins)) {
				t.Errorf("wind_rose counts are %d x %d for %d sectors and %d bins", len(p.Counts), len(p.Counts[0]), p.Sectors, len(p.Bins))
			}
		case "telemetry_chart":
			var p telemetryChartPayload
			err = dec.Decode(&p)
		default:
			continue
		}
		if err != nil {
			t.Errorf("%s card in message_source.json: %v", c.Kind, err)
		}
		seen[c.Kind]++
	}
	if seen["wind_rose"] != 1 || seen["telemetry_chart"] != 2 {
		t.Errorf("fixture cards = %v, want one wind_rose and two telemetry_chart", seen)
	}
}

func TestShowWindRoseIsRegisteredWithItsDescription(t *testing.T) {
	tool := lookupMateTool("show_wind_rose")
	if tool == nil {
		t.Fatal("show_wind_rose is not in the registry")
	}
	for _, w := range []string{"wind rose", "coming from", "card"} {
		if !strings.Contains(tool.function.Description, w) {
			t.Errorf("description should mention %q: %s", w, tool.function.Description)
		}
	}
}
