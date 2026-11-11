package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"math"
	"os"
	"strings"
	"sync"
	"testing"
	"time"
)

// A deterministic semidiurnal fixture: LW at 02:00Z, HW 6h12m later, and so
// on, alternating. LW0 02:00, HW0 08:12, LW1 14:24, HW1 20:36.
var tidalFixtureLW0 = time.Date(2026, 9, 12, 2, 0, 0, 0, time.UTC)

const tidalFixtureHalf = 6*time.Hour + 12*time.Minute

func tidalFixtureExtremes(from, count int) []tideExtremePoint {
	out := make([]tideExtremePoint, 0, count)
	for i := from; i < from+count; i++ {
		out = append(out, tideExtremePoint{
			Time:    tidalFixtureLW0.Add(time.Duration(i) * tidalFixtureHalf),
			HeightM: 1.0,
			High:    i%2 != 0, // even index = low water
		})
	}
	return out
}

func tidalDeps(extremes []tideExtremePoint) assistantToolDeps {
	station := tideStation{StationID: "S1", Name: "Test Bar", Lat: -27.0, Lon: 153.0, Timezone: "UTC"}
	provider := &stubTideProvider{id: "bom", stations: []tideStation{station}, result: tideChartResult{Extremes: extremes}}
	return assistantToolDeps{
		now:   func() time.Time { return tidalFixtureLW0 },
		tides: func() (tideProvider, string, error) { return provider, "bom", nil },
	}
}

func runTidalDeparture(t *testing.T, deps assistantToolDeps, args string) assistantTidalDepartureResult {
	t.Helper()
	raw, _, err := deps.execute(context.Background(), "plan_tidal_departure", json.RawMessage(args))
	if err != nil {
		t.Fatalf("plan_tidal_departure: %v", err)
	}
	var result assistantTidalDepartureResult
	if err := json.Unmarshal([]byte(raw), &result); err != nil {
		t.Fatalf("unmarshal: %v (raw %s)", err, raw)
	}
	return result
}

func tidalArgs(course, floodSet float64, passage float64, extra string) string {
	return fmt.Sprintf(`{"lat":-27.0,"lon":153.0,"course_deg":%g,"passage_hours":%g,"flood_set_deg":%g,"flood_set_source":"standing_notes"%s}`,
		course, passage, floodSet, withDefaultRoute(course, extra))
}

// withDefaultRoute adds a 12 nm route from lat/lon along course to extra unless
// it already names a route or asks for none (the marker noRouteMarker). The
// model is read along a route, so a test of the model path needs one.
const noRouteMarker = `"_no_route":true`

func withDefaultRoute(course float64, extra string) string {
	if strings.Contains(extra, `"route"`) {
		return extra
	}
	if strings.Contains(extra, noRouteMarker) {
		return strings.Replace(extra, `,`+noRouteMarker, ``, 1)
	}
	lat2, lon2 := destinationPoint(-27.0, 153.0, course*math.Pi/180, 12*metersPerNauticalMile)
	return extra + fmt.Sprintf(`,"route":[{"lat":-27.0,"lon":153.0},{"lat":%.5f,"lon":%.5f}]`, lat2, lon2)
}

// One full tidal cycle window starting at the first LW.
const tidalOneCycleWindow = `,"earliest":"2026-09-12T02:00:00Z","latest":"2026-09-12T15:00:00Z"`

func isoAt(t *testing.T, iso string) time.Time {
	t.Helper()
	ts, err := time.Parse(time.RFC3339, iso)
	if err != nil {
		t.Fatalf("parse %q: %v", iso, err)
	}
	return ts
}

func TestPlanTidalDeparture_FloodSettingWithCourseRanksLowWaterBestHighWaterWorst(t *testing.T) {
	deps := tidalDeps(tidalFixtureExtremes(-2, 40))
	// Southbound, flood sets south: depart at LW to ride the whole flood.
	res := runTidalDeparture(t, deps, tidalArgs(180, 180, 6.2, tidalOneCycleWindow))

	if len(res.Best) == 0 {
		t.Fatalf("expected best candidates")
	}
	if !strings.Contains(res.Reading, "wind against tide") || !strings.Contains(res.Reading, "say so and say when") {
		t.Errorf("plan_tidal_departure should say what to do about wind against tide, got %q", res.Reading)
	}
	if got := isoAt(t, res.Best[0].DepartureISO); !got.Equal(tidalFixtureLW0) {
		t.Errorf("expected best departure at LW %s, got %s", tidalFixtureLW0, got)
	}
	if res.Best[0].MeanAssist == nil || *res.Best[0].MeanAssist < 0.6 || *res.Best[0].MeanAssist > 0.66 {
		t.Errorf("expected mean_assist near 2/pi, got %v", res.Best[0].MeanAssist)
	}
	if res.Best[0].FairFraction < 0.95 {
		t.Errorf("expected nearly all of the run fair, got %v", res.Best[0].FairFraction)
	}
	if res.Best[0].Stream != "flood" {
		t.Errorf("expected stream %q, got %q", "flood", res.Best[0].Stream)
	}
	hw := tidalFixtureLW0.Add(tidalFixtureHalf)
	d := isoAt(t, res.Worst.DepartureISO).Sub(hw)
	if d < -15*time.Minute || d > 15*time.Minute {
		t.Errorf("expected worst departure within 15 min of HW %s, got %s", hw, res.Worst.DepartureISO)
	}
	if res.Worst.MeanAssist == nil || *res.Worst.MeanAssist > -0.55 {
		t.Errorf("expected strongly foul worst, got %v", res.Worst.MeanAssist)
	}
	if !strings.HasPrefix(res.Worst.Stream, "ebb") {
		t.Errorf("expected worst stream ebb, got %q", res.Worst.Stream)
	}
	if res.FloodSetSource != "standing_notes" || res.FloodSetDeg == nil || *res.FloodSetDeg != 180 || res.CourseDeg != 180 || res.RankingBasis != "tide_phase" {
		t.Errorf("expected inputs echoed, got %+v", res)
	}
	if res.Station.ID != "S1" || res.TimeBasis != "UTC" || res.Basis == "" {
		t.Errorf("expected station, time_basis and basis, got %+v", res)
	}
	if res.Note != "" {
		t.Errorf("expected no note for an along-stream course, got %q", res.Note)
	}
	if !strings.HasPrefix(res.Best[0].Departure, "Sat 12 Sep 2.00 AM") {
		t.Errorf("expected local departure label, got %q", res.Best[0].Departure)
	}
}

func TestPlanTidalDeparture_EbbSettingWithCourseFlipsTheRanking(t *testing.T) {
	deps := tidalDeps(tidalFixtureExtremes(-2, 40))
	// Flood sets north (0), course south: the ebb sets south, so HW is best.
	res := runTidalDeparture(t, deps, tidalArgs(180, 0, 6.2, tidalOneCycleWindow))

	hw := tidalFixtureLW0.Add(tidalFixtureHalf)
	d := isoAt(t, res.Best[0].DepartureISO).Sub(hw)
	if d < -15*time.Minute || d > 15*time.Minute {
		t.Errorf("expected best departure within 15 min of HW %s, got %s", hw, res.Best[0].DepartureISO)
	}
	if !strings.HasPrefix(res.Best[0].Stream, "ebb") {
		t.Errorf("expected stream starting with ebb, got %q", res.Best[0].Stream)
	}
	if got := isoAt(t, res.Worst.DepartureISO); !got.Equal(tidalFixtureLW0) {
		t.Errorf("expected worst departure at LW, got %s", got)
	}
}

func TestPlanTidalDeparture_SlackOffsetShiftsTheBestTime(t *testing.T) {
	deps := tidalDeps(tidalFixtureExtremes(-2, 40))
	res := runTidalDeparture(t, deps, tidalArgs(180, 180, 6.2, tidalOneCycleWindow+`,"slack_offset_min":60`))
	want := tidalFixtureLW0.Add(time.Hour)
	if got := isoAt(t, res.Best[0].DepartureISO); !got.Equal(want) {
		t.Errorf("expected best departure %s with a +60 min lag, got %s", want, got)
	}
	if res.SlackOffsetMin != 60 {
		t.Errorf("expected slack_offset_min echoed, got %d", res.SlackOffsetMin)
	}

	early := runTidalDeparture(t, deps, tidalArgs(180, 180, 6.2, tidalOneCycleWindow+`,"slack_offset_min":-60`))
	// The window starts at LW0; the turn leads by 60 min, so LW0-60 is out of
	// range and the best in-window time is the next LW less 60 min.
	wantEarly := tidalFixtureLW0.Add(2 * tidalFixtureHalf).Add(-time.Hour)
	d := isoAt(t, early.Best[0].DepartureISO).Sub(wantEarly)
	if d < -15*time.Minute || d > 15*time.Minute {
		t.Errorf("expected best departure near %s with a -60 min lead, got %s", wantEarly, early.Best[0].DepartureISO)
	}
}

func TestPlanTidalDeparture_SlackOffsetIsClamped(t *testing.T) {
	deps := tidalDeps(tidalFixtureExtremes(-2, 40))
	res := runTidalDeparture(t, deps, tidalArgs(180, 180, 6.2, tidalOneCycleWindow+`,"slack_offset_min":600`))
	if res.SlackOffsetMin != 180 {
		t.Errorf("expected slack_offset_min clamped to 180, got %d", res.SlackOffsetMin)
	}
}

func TestPlanTidalDeparture_CrossStreamCourseCarriesNote(t *testing.T) {
	deps := tidalDeps(tidalFixtureExtremes(-2, 40))
	res := runTidalDeparture(t, deps, tidalArgs(90, 180, 6.2, tidalOneCycleWindow))
	if !strings.Contains(res.Note, "across") {
		t.Errorf("expected a cross-stream note, got %q", res.Note)
	}
	if len(res.Best) == 0 {
		t.Errorf("expected candidates to be computed anyway")
	}
	// A course a little off the stream still carries no note.
	res = runTidalDeparture(t, deps, tidalArgs(150, 180, 6.2, tidalOneCycleWindow))
	if res.Note != "" {
		t.Errorf("expected no note at 30 degrees off the stream, got %q", res.Note)
	}
}

func TestPlanTidalDeparture_CandidatesAreSeparatedAndSortedBestFirst(t *testing.T) {
	deps := tidalDeps(tidalFixtureExtremes(-2, 40))
	res := runTidalDeparture(t, deps, tidalArgs(180, 180, 3, `,"earliest":"2026-09-12T02:00:00Z","latest":"2026-09-14T02:00:00Z"`))
	if len(res.Best) != 3 {
		t.Fatalf("expected 3 candidates over two days, got %d", len(res.Best))
	}
	for i := range res.Best {
		if i > 0 && *res.Best[i].MeanAssist > *res.Best[i-1].MeanAssist {
			t.Errorf("expected best first, got %v then %v", *res.Best[i-1].MeanAssist, *res.Best[i].MeanAssist)
		}
		for j := i + 1; j < len(res.Best); j++ {
			gap := isoAt(t, res.Best[i].DepartureISO).Sub(isoAt(t, res.Best[j].DepartureISO))
			if gap < 0 {
				gap = -gap
			}
			if gap < 2*time.Hour {
				t.Errorf("candidates %d and %d only %s apart", i, j, gap)
			}
		}
		dep := isoAt(t, res.Best[i].DepartureISO)
		arr := isoAt(t, res.Best[i].ArrivalISO)
		if arr.Sub(dep) != 3*time.Hour {
			t.Errorf("expected arrival 3h after departure, got %s", arr.Sub(dep))
		}
	}
}

func TestPlanTidalDeparture_PhasesCrossedAreNamedInOrder(t *testing.T) {
	deps := tidalDeps(tidalFixtureExtremes(-2, 40))
	// 05:00Z for 6.2 h spans the end of the flood (to 08:12) then the ebb.
	res := runTidalDeparture(t, deps, tidalArgs(180, 180, 6.2, `,"earliest":"2026-09-12T05:00:00Z","latest":"2026-09-12T05:15:00Z"`))
	got := res.Best[0].Stream
	if got != "flood then ebb" {
		t.Errorf("expected %q, got %q", "flood then ebb", got)
	}
	if len(res.Best[0].Phases) != 2 || res.Best[0].Phases[0].Phase != "flood" || res.Best[0].Phases[1].Phase != "ebb" {
		t.Errorf("expected two phases flood, ebb, got %+v", res.Best[0].Phases)
	}
}

func TestPlanTidalDeparture_DefaultWindowIsNextTwentyFourHours(t *testing.T) {
	deps := tidalDeps(tidalFixtureExtremes(-2, 40))
	res := runTidalDeparture(t, deps, tidalArgs(180, 180, 3, ""))
	now := tidalFixtureLW0.Add(time.Hour)
	for _, c := range append(append([]assistantTidalCandidate{}, res.Best...), res.Worst) {
		dep := isoAt(t, c.DepartureISO)
		if dep.Before(now) || dep.After(now.Add(24*time.Hour)) {
			t.Errorf("departure %s outside the default window", dep)
		}
	}
}

func TestPlanTidalDeparture_FailsFast(t *testing.T) {
	good := tidalFixtureExtremes(-2, 40)
	cases := []struct {
		name string
		deps assistantToolDeps
		args string
		want string
	}{
		{
			name: "no provider",
			deps: assistantToolDeps{now: time.Now, tides: func() (tideProvider, string, error) { return nil, "", fmt.Errorf("no tide provider is selected") }},
			args: tidalArgs(180, 180, 6, ""),
			want: "no tide provider",
		},
		{
			name: "no stations",
			deps: assistantToolDeps{now: time.Now, tides: func() (tideProvider, string, error) {
				return &stubTideProvider{id: "bom"}, "bom", nil
			}},
			args: tidalArgs(180, 180, 6, ""),
			want: "no stations",
		},
		{
			name: "extremes start too late",
			deps: tidalDeps(tidalFixtureExtremes(3, 20)),
			args: tidalArgs(180, 180, 6, tidalOneCycleWindow),
			want: "before",
		},
		{
			name: "extremes end too early",
			deps: tidalDeps(tidalFixtureExtremes(-2, 6)),
			args: tidalArgs(180, 180, 6, tidalOneCycleWindow),
			want: "after",
		},
		{
			name: "no extremes",
			deps: tidalDeps(nil),
			args: tidalArgs(180, 180, 6, tidalOneCycleWindow),
			want: "extremes",
		},
		{
			name: "latest not after earliest",
			deps: tidalDeps(good),
			args: tidalArgs(180, 180, 6, `,"earliest":"2026-09-12T05:00:00Z","latest":"2026-09-12T05:00:00Z"`),
			want: "latest",
		},
		{
			name: "passage hours zero",
			deps: tidalDeps(good),
			args: tidalArgs(180, 180, 0, ""),
			want: "passage_hours",
		},
		{
			name: "passage under a minute",
			deps: tidalDeps(good),
			args: tidalArgs(180, 180, 0.005, ""),
			want: "passage_hours",
		},
		{
			name: "passage hours too long",
			deps: tidalDeps(good),
			args: tidalArgs(180, 180, 30, ""),
			want: "passage_hours",
		},
		{
			name: "missing flood set",
			deps: tidalDeps(good),
			args: `{"lat":-27,"lon":153,"course_deg":180,"passage_hours":6,"flood_set_source":"operator"}`,
			want: "flood_set_deg",
		},
		{
			name: "bad source",
			deps: tidalDeps(good),
			args: `{"lat":-27,"lon":153,"course_deg":180,"passage_hours":6,"flood_set_deg":180,"flood_set_source":"guess"}`,
			want: "flood_set_source",
		},
		{
			name: "missing course",
			deps: tidalDeps(good),
			args: `{"lat":-27,"lon":153,"passage_hours":6,"flood_set_deg":180,"flood_set_source":"operator"}`,
			want: "course_deg",
		},
		{
			name: "bad earliest",
			deps: tidalDeps(good),
			args: tidalArgs(180, 180, 6, `,"earliest":"tomorrow"`),
			want: "earliest",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, _, err := tc.deps.execute(context.Background(), "plan_tidal_departure", json.RawMessage(tc.args))
			if err == nil {
				t.Fatalf("expected an error")
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Errorf("expected error to mention %q, got %q", tc.want, err.Error())
			}
		})
	}
}

func TestDescribeAssistantToolCall_PlanTidalDeparture(t *testing.T) {
	got := describeAssistantToolCall("plan_tidal_departure", json.RawMessage(`{"lat":-20.1,"lon":149.1}`))
	if !strings.Contains(got, "tide") {
		t.Errorf("expected a tide status line, got %q", got)
	}
}

func TestAssistantToolDefinitions_PlanTidalDepartureTakesAnOptionalSetAndSource(t *testing.T) {
	for _, tool := range assistantToolDefinitions(assistantToolDeps{}) {
		if tool.Function.Name != "plan_tidal_departure" {
			continue
		}
		params := string(tool.Function.Parameters)
		for _, want := range []string{"flood_set_deg", "flood_set_source", "standing_notes", "general_knowledge", "operator", "course_deg", "passage_hours", "slack_offset_min", "route"} {
			if !strings.Contains(params, want) {
				t.Errorf("expected parameters to mention %q, got %s", want, params)
			}
		}
		var schema struct {
			Required []string `json:"required"`
		}
		if err := json.Unmarshal(tool.Function.Parameters, &schema); err != nil {
			t.Fatal(err)
		}
		for _, r := range schema.Required {
			if r == "flood_set_deg" || r == "flood_set_source" {
				t.Errorf("%s must be optional: the model ranks without it where it covers the run", r)
			}
		}
		return
	}
	t.Fatal("plan_tidal_departure tool definition not found")
}

// stubCurrentsWaveProvider is a wave provider with the currents capability. The
// current at a point comes from at(lat, lon) hour by hour.
type stubCurrentsWaveProvider struct {
	stubWaveProvider
	unsupported bool
	at          func(lat, lon float64, t time.Time) (v, set float64, ok bool)
	from        time.Time
	hours       int

	delay time.Duration
	fail  error

	mu       sync.Mutex
	calls    [][]currentsPoint
	lastDays int
}

func (s *stubCurrentsWaveProvider) FetchCurrents(ctx context.Context, points []currentsPoint, days int) (currentsBundle, error) {
	if err := ctx.Err(); err != nil {
		return currentsBundle{}, err
	}
	s.mu.Lock()
	s.calls = append(s.calls, append([]currentsPoint(nil), points...))
	s.lastDays = days
	s.mu.Unlock()
	if s.unsupported {
		return currentsBundle{}, fmt.Errorf("plugin %q: %w", "open-meteo-marine", errNoCurrentsExport)
	}
	if s.delay > 0 {
		select {
		case <-time.After(s.delay):
		case <-ctx.Done():
			return currentsBundle{}, ctx.Err()
		}
	}
	if s.fail != nil {
		return currentsBundle{}, s.fail
	}
	var out currentsBundle
	for _, pt := range points {
		var series currentPointSeries
		for i := 0; i < s.hours; i++ {
			ts := s.from.Add(time.Duration(i) * time.Hour)
			h := currentHourPoint{Time: ts}
			if v, set, ok := s.at(pt.Lat, pt.Lon, ts); ok {
				h.VelocityMS, h.SetDeg = &v, &set
			}
			series.Hourly = append(series.Hourly, h)
		}
		out.Points = append(out.Points, series)
	}
	return out, nil
}

func tidalDepsWithProvider(p waveProvider) assistantToolDeps {
	deps := tidalDeps(tidalFixtureExtremes(-2, 40))
	deps.waves = func() (waveProvider, string, error) { return p, "open-meteo-marine", nil }
	return deps
}

// steadyCurrent is 0.5 m/s toward set everywhere, all the time.
func steadyCurrent(set float64) *stubCurrentsWaveProvider {
	return &stubCurrentsWaveProvider{
		stubWaveProvider: stubWaveProvider{id: "open-meteo-marine"},
		from:             time.Date(2026, 9, 11, 0, 0, 0, 0, time.UTC),
		hours:            120,
		at:               func(lat, lon float64, t time.Time) (float64, float64, bool) { return 0.5, set, true },
	}
}

func tidalModelArgs(course float64, passage float64, extra string) string {
	return fmt.Sprintf(`{"lat":-27.0,"lon":153.0,"course_deg":%g,"passage_hours":%g%s}`, course, passage, withDefaultRoute(course, extra))
}

const tidalEarlyWindow = `,"earliest":"2026-09-12T02:00:00Z","latest":"2026-09-12T02:15:00Z"`

func TestPlanTidalDeparture_RanksOnTheModelledCurrentWhereItCoversTheRun(t *testing.T) {
	// The current sets south (180) hard from 06:00Z and north before it, the
	// reverse of any tide phase: a boat heading south is helped only after 06:00Z.
	p := steadyCurrent(0)
	p.at = func(lat, lon float64, ts time.Time) (float64, float64, bool) {
		if ts.Before(time.Date(2026, 9, 12, 6, 0, 0, 0, time.UTC)) {
			return 0.5, 0, true
		}
		return 0.5, 180, true
	}
	deps := tidalDepsWithProvider(p)
	res := runTidalDeparture(t, deps, tidalModelArgs(180, 3, `,"earliest":"2026-09-12T00:00:00Z","latest":"2026-09-12T12:00:00Z"`))
	if res.RankingBasis != "modelled_current" || res.ModelledCurrentMissing != "" {
		t.Fatalf("expected modelled_current, got %q (%s)", res.RankingBasis, res.ModelledCurrentMissing)
	}
	best := res.Best[0]
	if got := isoAt(t, best.DepartureISO); got.Before(time.Date(2026, 9, 12, 5, 55, 0, 0, time.UTC)) {
		t.Errorf("a southbound run should be best once the current sets south, got %s", got)
	}
	if best.MeanAssist != nil {
		t.Errorf("mean_assist is the tide-phase score and has no place under modelled_current")
	}
	if best.MeanAlongCourseKn == nil || *best.MeanAlongCourseKn < 0.9 || best.FairFraction < 0.99 {
		t.Errorf("expected about 0.97 kn with the boat and all of the run fair, got %+v", best)
	}
	if res.Worst.MeanAlongCourseKn == nil || *res.Worst.MeanAlongCourseKn > -0.9 || res.Worst.FairFraction > 0.01 {
		t.Errorf("expected the worst run against the current, got %+v", res.Worst)
	}
	if res.FloodSetDeg != nil || res.FloodSetSource != "" {
		t.Errorf("no flood set is derived or used under modelled_current: %+v", res)
	}
}

func TestPlanTidalDeparture_ModelledCurrentIgnoresASuppliedSetAndSaysSo(t *testing.T) {
	deps := tidalDepsWithProvider(steadyCurrent(150))
	res := runTidalDeparture(t, deps, tidalArgs(150, 330, 3, tidalEarlyWindow))
	if res.RankingBasis != "modelled_current" {
		t.Fatalf("got %q", res.RankingBasis)
	}
	if res.FloodSetDeg != nil || !strings.Contains(res.Note, "not used") {
		t.Errorf("the unused direction should be said, got set %v note %q", res.FloodSetDeg, res.Note)
	}
}

func TestPlanTidalDeparture_AlongCourseStreamHasTheRightSign(t *testing.T) {
	deps := tidalDepsWithProvider(steadyCurrent(150))
	const kn = 0.5 * 1.943844

	with := runTidalDeparture(t, deps, tidalModelArgs(150, 3, tidalEarlyWindow)).Best[0]
	if with.MeanAlongCourseKn == nil || math.Abs(*with.MeanAlongCourseKn-kn) > 0.05 || math.Abs(*with.PeakAlongCourseKn-kn) > 0.05 {
		t.Errorf("a current along the course should carry about %.2f kn, got %+v", kn, with)
	}
	against := runTidalDeparture(t, deps, tidalModelArgs(330, 3, tidalEarlyWindow)).Best[0]
	if against.MeanAlongCourseKn == nil || math.Abs(*against.MeanAlongCourseKn+kn) > 0.05 || *against.PeakAlongCourseKn > -kn+0.05 {
		t.Errorf("the same current against the course should read about -%.2f kn, got %+v", kn, against)
	}
	across := runTidalDeparture(t, deps, tidalModelArgs(60, 3, tidalEarlyWindow)).Best[0]
	if across.MeanAlongCourseKn == nil || math.Abs(*across.MeanAlongCourseKn) > 0.05 {
		t.Errorf("a course square to the current should read about 0, got %+v", across)
	}
}

func TestPlanTidalDeparture_RouteSetsTheCoursePerLegAndTheSamplePoints(t *testing.T) {
	// The water sets east (90) along the southern leg and north (0) up the
	// eastern one. A route heading east then north is helped on both legs. Taking
	// course_deg 90 for the whole run would leave the second leg with no help.
	p := steadyCurrent(0)
	p.at = func(lat, lon float64, ts time.Time) (float64, float64, bool) {
		if lat > -26.99 {
			return 0.5, 0, true
		}
		return 0.5, 90, true
	}
	deps := tidalDepsWithProvider(p)
	route := `,"route":[{"lat":-27.0,"lon":153.0},{"lat":-27.0,"lon":153.4},{"lat":-26.6,"lon":153.4}]`
	res := runTidalDeparture(t, deps, tidalModelArgs(90, 4, tidalEarlyWindow+route))
	if res.RankingBasis != "modelled_current" || res.RoutePoints != 3 {
		t.Fatalf("got basis %q, route points %d", res.RankingBasis, res.RoutePoints)
	}
	c := res.Best[0]
	if c.MeanAlongCourseKn == nil || *c.MeanAlongCourseKn < 0.85 {
		t.Errorf("the current runs with the boat on both legs, got %+v", c)
	}
	// Without the route the model is not used at all, so nothing is read.
	single := runTidalDeparture(t, deps, tidalArgs(90, 90, 4, tidalEarlyWindow+`,`+noRouteMarker))
	if single.RankingBasis != "tide_phase" {
		t.Errorf("no route means no model, got %q", single.RankingBasis)
	}
	// One request carried the whole route, and each distinct cell was sent once.
	// The call with no route made no request at all.
	if len(p.calls) != 1 {
		t.Fatalf("expected one FetchCurrents call per tool call that has a route, got %d", len(p.calls))
	}
	cells := map[tidalGridPoint]int{}
	for _, pt := range p.calls[0] {
		cells[roundToTidalGrid(pt.Lat, pt.Lon)]++
	}
	for cell, n := range cells {
		if n != 1 {
			t.Errorf("cell %v sent %d times", cell, n)
		}
	}
	if len(p.calls[0]) < 3 {
		t.Errorf("a route of several cells expected, got %d points", len(p.calls[0]))
	}
}

func TestPlanTidalDeparture_NoModelAndNoFloodSetErrorsNamingBothGaps(t *testing.T) {
	cases := map[string]assistantToolDeps{
		"no wave provider":        tidalDeps(tidalFixtureExtremes(-2, 40)),
		"plugin without currents": tidalDepsWithProvider(&stubWaveProvider{id: "open-meteo-marine"}),
		"plugin export missing":   tidalDepsWithProvider(&stubCurrentsWaveProvider{stubWaveProvider: stubWaveProvider{id: "open-meteo-marine"}, unsupported: true}),
	}
	want := map[string]string{
		"no wave provider":        "wave provider",
		"plugin without currents": "install the updated open-meteo-marine plugin",
		"plugin export missing":   "install the updated open-meteo-marine plugin",
	}
	for name, deps := range cases {
		t.Run(name, func(t *testing.T) {
			_, _, err := deps.execute(context.Background(), "plan_tidal_departure", json.RawMessage(tidalModelArgs(150, 3, tidalOneCycleWindow)))
			if err == nil {
				t.Fatal("expected an error, never an invented direction")
			}
			if !strings.Contains(err.Error(), want[name]) || !strings.Contains(err.Error(), "flood_set_deg") {
				t.Errorf("expected both gaps named (%q and flood_set_deg), got %q", want[name], err.Error())
			}
		})
	}
}

func TestPlanTidalDeparture_SuppliedSetWithoutModelStillRanksAndSaysWhy(t *testing.T) {
	deps := tidalDeps(tidalFixtureExtremes(-2, 40)) // no wave provider
	res := runTidalDeparture(t, deps, tidalArgs(180, 180, 3, tidalEarlyWindow))
	if res.RankingBasis != "tide_phase" || !strings.Contains(res.ModelledCurrentMissing, "wave provider") {
		t.Errorf("expected tide_phase with the reason, got %q %q", res.RankingBasis, res.ModelledCurrentMissing)
	}
	c := res.Best[0]
	if c.MeanAlongCourseKn != nil || c.PeakAlongCourseKn != nil {
		t.Errorf("missing currents must not read as 0: %+v", c)
	}
	if c.AlongCourseMissing != "" {
		t.Errorf("modelled_current_missing is the one reason, not repeated on every candidate: %q", c.AlongCourseMissing)
	}
}

func TestPlanTidalDeparture_RouteAndSourceRules(t *testing.T) {
	deps := tidalDepsWithProvider(steadyCurrent(150))
	cases := map[string]string{
		"one route point":            `{"lat":-27,"lon":153,"course_deg":150,"passage_hours":3,"route":[{"lat":-27,"lon":153}]}`,
		"bad route point":            `{"lat":-27,"lon":153,"course_deg":150,"passage_hours":3,"route":[{"lat":-27,"lon":153},{"lat":95,"lon":153}]}`,
		"open_meteo_model is gone":   `{"lat":-27,"lon":153,"course_deg":150,"passage_hours":3,"flood_set_deg":150,"flood_set_source":"open_meteo_model"}`,
		"notes source without a set": `{"lat":-27,"lon":153,"course_deg":150,"passage_hours":3,"flood_set_source":"standing_notes"}`,
	}
	for name, args := range cases {
		t.Run(name, func(t *testing.T) {
			_, _, err := deps.execute(context.Background(), "plan_tidal_departure", json.RawMessage(args))
			if err == nil {
				t.Fatal("expected an error")
			}
		})
	}
}

func TestPlanTidalDeparture_AFailedCurrentsRequestIsAnErrorNotAFallback(t *testing.T) {
	p := steadyCurrent(150)
	p.fail = fmt.Errorf("upstream returned 500")
	deps := tidalDepsWithProvider(p)
	// A failed request is an error to Mate, never a switch to tide phase, even
	// with a flood direction given and the fallback on.
	_, _, err := deps.execute(context.Background(), "plan_tidal_departure", json.RawMessage(tidalArgs(150, 150, 3, tidalEarlyWindow)))
	if err == nil || !strings.Contains(err.Error(), "500") || !strings.Contains(err.Error(), "current") {
		t.Errorf("expected the failed request reported as an error, got %v", err)
	}
}

func TestPlanTidalDeparture_ACancelledCallStopsWaitingAndReturnsTheContextError(t *testing.T) {
	p := steadyCurrent(150)
	p.delay = 5 * time.Second
	deps := tidalDepsWithProvider(p)
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	start := time.Now()
	_, _, err := deps.execute(ctx, "plan_tidal_departure", json.RawMessage(tidalArgs(150, 150, 3, tidalEarlyWindow)))
	if err == nil || !strings.Contains(err.Error(), "deadline") {
		t.Fatalf("expected the context error, got %v", err)
	}
	if time.Since(start) > 2*time.Second {
		t.Errorf("the call kept waiting after its context was done")
	}
}

// The currents request is in GMT with past_days=1, so the series runs from
// 00:00 UTC yesterday to 23:00 UTC on the last forecast day. Whatever the time of
// day now falls in, the days asked for must leave the last sample bracketed.
func TestTidalCurrentDays_AlwaysBracketTheLastSample(t *testing.T) {
	for minute := 0; minute < 24*60; minute += 15 {
		now := time.Date(2026, 9, 12, 0, 0, 0, 0, time.UTC).Add(time.Duration(minute) * time.Minute)
		for _, ahead := range []time.Duration{0, 30 * time.Minute, 23*time.Hour + 30*time.Minute, 24 * time.Hour, 47*time.Hour + 30*time.Minute, 5 * 24 * time.Hour} {
			need := now.Add(ahead)
			days := tidalCurrentDays(now, need)
			midnight := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC)
			lastHour := midnight.Add(time.Duration(days)*24*time.Hour - time.Hour)
			if lastHour.Before(need) {
				t.Fatalf("now %s ahead %s: %d days end at %s, before %s", now, ahead, days, lastHour, need)
			}
		}
	}
}

func TestPlanTidalDeparture_ForecastDaysCoverALateEvening(t *testing.T) {
	p := steadyCurrent(150)
	deps := tidalDepsWithProvider(p)
	// The run ends at 23:30 UTC, an hour short of two whole days from now.
	runTidalDeparture(t, deps, tidalArgs(150, 150, 3, `,"earliest":"2026-09-12T03:00:00Z","latest":"2026-09-13T20:30:00Z"`))
	if p.lastDays != 7 {
		t.Errorf("a run ending two days on is asked for in the 7 day bucket, asked for %d", p.lastDays)
	}
}

func TestTidalSamples_ZeroLengthLegsTakeTheNextLegsCourse(t *testing.T) {
	// The route repeats its first point, then runs east.
	route := []assistantLatLon{{Lat: -27, Lon: 153}, {Lat: -27, Lon: 153}, {Lat: -27, Lon: 153.5}}
	samples, err := tidalSamples(route, -27, 153, 0, 12)
	if err != nil {
		t.Fatal(err)
	}
	for i, s := range samples {
		if math.Abs(s.course-90) > 1 {
			t.Fatalf("sample %d course %v, expected the east leg's 90", i, s.course)
		}
	}
}

func TestPlanTidalDeparture_ARouteOfOnePositionIsAnInputError(t *testing.T) {
	deps := tidalDepsWithProvider(steadyCurrent(150))
	args := tidalModelArgs(150, 3, tidalEarlyWindow+`,"route":[{"lat":-27,"lon":153},{"lat":-27,"lon":153}]`)
	_, _, err := deps.execute(context.Background(), "plan_tidal_departure", json.RawMessage(args))
	if err == nil || !strings.Contains(err.Error(), "route") {
		t.Fatalf("expected a route error, got %v", err)
	}
}

func TestPlanTidalDeparture_SendsTheFirstSamplePositionInEachCellNotTheCentre(t *testing.T) {
	p := steadyCurrent(150)
	deps := tidalDepsWithProvider(p)
	// -27.02, 153.01 is in a cell whose centre is elsewhere: the request carries
	// the sample's own position, which a snapped centre could put on land.
	route := `,"route":[{"lat":-27.02,"lon":153.01},{"lat":-27.02,"lon":153.30}]`
	runTidalDeparture(t, deps, tidalModelArgs(90, 3, tidalEarlyWindow+route))
	if len(p.calls) != 1 {
		t.Fatalf("expected one call, got %d", len(p.calls))
	}
	first := p.calls[0][0]
	if math.Abs(first.Lat+27.02) > 1e-9 || math.Abs(first.Lon-153.01) > 1e-9 {
		t.Errorf("expected the first sample's own position, got %+v", first)
	}
}

func TestTidalSamples_CarryTheirOwnPositionAndCell(t *testing.T) {
	route := []assistantLatLon{{Lat: -27.03, Lon: 153.02}, {Lat: -27.03, Lon: 153.30}}
	samples, err := tidalSamples(route, -27, 153, 0, 6)
	if err != nil {
		t.Fatal(err)
	}
	if samples[0].pos != route[0] {
		t.Errorf("first sample is at the route start, got %+v", samples[0].pos)
	}
	if samples[0].cell != roundToTidalGrid(-27.03, 153.02) {
		t.Errorf("cell is the rounded position, got %+v", samples[0].cell)
	}
}

func TestTidalSamples_InterpolateAcrossTheAntimeridian(t *testing.T) {
	route := []assistantLatLon{{Lat: -17, Lon: 179.8}, {Lat: -17, Lon: -179.8}}
	samples, err := tidalSamples(route, -17, 179.8, 0, 2)
	if err != nil {
		t.Fatal(err)
	}
	mid := samples[1].pos
	if math.Abs(math.Abs(mid.Lon)-180) > 0.01 {
		t.Errorf("the middle of a run across 180 is near 180, got %v", mid.Lon)
	}
	if math.Abs(samples[1].course-90) > 1 {
		t.Errorf("heading east across the antimeridian is 90, got %v", samples[1].course)
	}
}

func TestTidalSamples_ARouteWithNoLengthIsAnError(t *testing.T) {
	route := []assistantLatLon{{Lat: -27, Lon: 153}, {Lat: -27, Lon: 153}}
	if _, err := tidalSamples(route, -27, 153, 0, 6); err == nil {
		t.Fatal("expected an error")
	}
}

func TestPlanTidalDeparture_ModelledCurrentNeedsNoTideDataAndShowsNoPhases(t *testing.T) {
	p := steadyCurrent(150)
	deps := tidalDepsWithProvider(p)
	deps.tides = func() (tideProvider, string, error) { return nil, "", fmt.Errorf("no tide provider is selected") }
	res := runTidalDeparture(t, deps, tidalModelArgs(150, 3, `,"earliest":"2026-09-12T02:00:00Z","latest":"2026-09-12T05:00:00Z"`))
	if res.RankingBasis != "modelled_current" {
		t.Fatalf("expected modelled_current, got %q", res.RankingBasis)
	}
	if res.Station != nil || res.Provider != "" {
		t.Errorf("no tide station was used, so none is reported: %+v", res.Station)
	}
	if res.TimeBasis == "" {
		t.Errorf("times still need a stated zone")
	}
	for _, c := range append(append([]assistantTidalCandidate{}, res.Best...), res.Worst) {
		if c.Stream != "" || len(c.Phases) != 0 {
			t.Errorf("stream and phases come from the tide, so they appear only under tide_phase: %+v", c)
		}
		if c.Departure == "" {
			t.Errorf("departure label missing: %+v", c)
		}
	}
	raw, _, _ := deps.execute(context.Background(), "plan_tidal_departure", json.RawMessage(tidalModelArgs(150, 3, tidalEarlyWindow)))
	if strings.Contains(raw, `"phases"`) || strings.Contains(raw, `"stream"`) {
		t.Errorf("the keys must be absent from the result: %s", raw)
	}
}

func TestPlanTidalDeparture_RouteCapIsAnErrorNotASplit(t *testing.T) {
	p := steadyCurrent(150)
	deps := tidalDepsWithProvider(p)
	// Fifteen degrees of latitude is 180 cells of 1/12 degree, over the cap of 100.
	route := `,"route":[{"lat":-27.0,"lon":153.0},{"lat":-42.0,"lon":153.0}]`
	_, _, err := deps.execute(context.Background(), "plan_tidal_departure", json.RawMessage(tidalModelArgs(180, 24, tidalEarlyWindow+route)))
	if err == nil || !strings.Contains(err.Error(), "100") {
		t.Fatalf("expected the cap named, got %v", err)
	}
	if len(p.calls) != 0 {
		t.Errorf("the route must not be split into several requests")
	}
}

// ---- coverage at the ends, no route, route start, wording, grid ----

// coverageStub is a steady 0.5 m/s toward 150 that the model lacks (null) where
// lacking(lat, lon) says so.
func coverageStub(lacking func(lat, lon float64) bool) *stubCurrentsWaveProvider {
	p := steadyCurrent(150)
	p.at = func(lat, lon float64, ts time.Time) (float64, float64, bool) {
		return 0.5, 150, !lacking(lat, lon)
	}
	return p
}

// A 40 nm route from -27, 153 heading 150: the first and last 3 nm are river,
// marina or approach water where the model is null.
func longRouteArgs(extra string) string {
	lat2, lon2 := destinationPoint(-27.0, 153.0, 150*math.Pi/180, 40*metersPerNauticalMile)
	route := fmt.Sprintf(`,"route":[{"lat":-27.0,"lon":153.0},{"lat":%.5f,"lon":%.5f}]`, lat2, lon2)
	return tidalModelArgs(150, 8, tidalEarlyWindow+route+extra)
}

func distNmFromStart(lat, lon float64) float64 {
	return haversineMeters(-27.0, 153.0, lat, lon) / metersPerNauticalMile
}

func TestPlanTidalDeparture_AWaveProviderConfigurationFaultIsAnErrorNotAFallback(t *testing.T) {
	deps := tidalDeps(tidalFixtureExtremes(-2, 40))
	deps.waves = func() (waveProvider, string, error) {
		return nil, "", fmt.Errorf("unknown wave provider configured: %q (is the plugin installed in plugins/waves?)", "gone")
	}
	_, _, err := deps.execute(context.Background(), "plan_tidal_departure", json.RawMessage(tidalArgs(150, 150, 3, tidalEarlyWindow)))
	if err == nil || !strings.Contains(err.Error(), "unknown wave provider") {
		t.Fatalf("a configuration fault must be reported, got %v", err)
	}
}

func TestPlanTidalDeparture_NoRouteMeansNoModel(t *testing.T) {
	p := steadyCurrent(150)
	deps := tidalDepsWithProvider(p)
	res := runTidalDeparture(t, deps, tidalArgs(150, 150, 3, tidalEarlyWindow+`,`+noRouteMarker))
	if res.RankingBasis != "tide_phase" || !strings.Contains(res.ModelledCurrentMissing, "no route was given") {
		t.Errorf("expected tide_phase with the reason, got %q %q", res.RankingBasis, res.ModelledCurrentMissing)
	}
	if len(p.calls) != 0 {
		t.Errorf("one cell is not the whole run, so nothing is requested, got %d calls", len(p.calls))
	}
	// And with no direction either, both gaps are named.
	_, _, err := deps.execute(context.Background(), "plan_tidal_departure", json.RawMessage(`{"lat":-27,"lon":153,"course_deg":150,"passage_hours":3}`))
	if err == nil || !strings.Contains(err.Error(), "no route was given") || !strings.Contains(err.Error(), "flood_set_deg") {
		t.Errorf("expected both gaps named, got %v", err)
	}
}

// Open-Meteo's ocean model cells have their centres at k/12 + 1/24 degrees (live
// responses: -24.958336 152.62502, -24.958336 153.62502, -25.458336 153.79167).
func TestRoundToTidalGrid_UsesTheModelsCellCentres(t *testing.T) {
	cases := []struct{ lat, lon, wantLat, wantLon float64 }{
		{-25.0, 152.6, -24.958333, 152.625},
		{-25.0, 153.6, -24.958333, 153.625},
		{-25.5, 153.8, -25.458333, 153.791667},
	}
	for _, c := range cases {
		g := roundToTidalGrid(c.lat, c.lon)
		if math.Abs(g.lat-c.wantLat) > 1e-4 || math.Abs(g.lon-c.wantLon) > 1e-4 {
			t.Errorf("(%v, %v): got (%v, %v), want (%v, %v)", c.lat, c.lon, g.lat, g.lon, c.wantLat, c.wantLon)
		}
	}
	// A point is in the same cell as its own centre.
	g := roundToTidalGrid(-25.0, 152.6)
	if roundToTidalGrid(g.lat, g.lon) != g {
		t.Errorf("a cell centre must round to itself")
	}
}

// ---- review fixes: zone lookup, note wording, past departures, leg courses ----

func TestPlanTidalDeparture_TidePhaseDoesNotLookUpTheDeparturePositionsZone(t *testing.T) {
	calls := 0
	old := tidalZoneAt
	tidalZoneAt = func(lat, lon float64) (placeZone, error) {
		calls++
		return placeZone{}, fmt.Errorf("zone index unavailable")
	}
	defer func() { tidalZoneAt = old }()

	// No route: the tide-phase method, which times everything in the station's zone.
	deps := tidalDepsWithProvider(steadyCurrent(150))
	res := runTidalDeparture(t, deps, tidalArgs(150, 150, 3, tidalEarlyWindow+`,`+noRouteMarker))
	if res.RankingBasis != "tide_phase" {
		t.Fatalf("expected tide_phase, got %q", res.RankingBasis)
	}
	if calls != 0 {
		t.Errorf("tide_phase must not look up the departure position's zone, got %d lookups", calls)
	}

	// The model path does need the zone, and a failed lookup is an error there.
	_, _, err := deps.execute(context.Background(), "plan_tidal_departure", json.RawMessage(tidalModelArgs(150, 3, tidalEarlyWindow)))
	if err == nil || !strings.Contains(err.Error(), "time zone") {
		t.Errorf("a failed zone lookup on the model path must be an error, got %v", err)
	}
}

func TestPlanTidalDeparture_AWindowStartingInThePastIsClampedToNow(t *testing.T) {
	// now is 03:00Z on 12 Sep and the series starts 00:00Z on 11 Sep, so a window
	// opening 30 hours ago would reach a candidate before the series starts.
	deps := tidalDepsWithProvider(steadyCurrent(150))
	now := deps.now()
	window := fmt.Sprintf(`,"earliest":%q,"latest":%q`, now.Add(-30*time.Hour).Format(time.RFC3339), now.Add(6*time.Hour).Format(time.RFC3339))
	res := runTidalDeparture(t, deps, tidalModelArgs(150, 3, window))
	if res.RankingBasis != "modelled_current" {
		t.Fatalf("a past earliest must not push a candidate outside the series, got %q (%s)", res.RankingBasis, res.ModelledCurrentMissing)
	}
	for _, c := range append(append([]assistantTidalCandidate{}, res.Best...), res.Worst) {
		if isoAt(t, c.DepartureISO).Before(now) {
			t.Errorf("no departure may be in the past, got %s", c.DepartureISO)
		}
	}

	// A window wholly in the past leaves nothing to rank.
	past := fmt.Sprintf(`,"earliest":%q,"latest":%q`, now.Add(-30*time.Hour).Format(time.RFC3339), now.Add(-2*time.Hour).Format(time.RFC3339))
	if _, _, err := deps.execute(context.Background(), "plan_tidal_departure", json.RawMessage(tidalModelArgs(150, 3, past))); err == nil {
		t.Errorf("a window wholly in the past must be an error")
	}
}

func TestPlanTidalDeparture_TidePhaseFallbackUsesEachLegsCourseOnARoute(t *testing.T) {
	// East for 6 nm then north for 6 nm, with a single course_deg of 90 and a flood
	// that sets north: the first leg is square to the stream and the second is
	// straight along it, so some departures are helped or hindered. Using the one
	// course_deg of 90 would score every departure 0.
	p := steadyCurrent(150)
	p.at = func(lat, lon float64, ts time.Time) (float64, float64, bool) { return 0, 0, false } // the model has nothing
	deps := tidalDepsWithProvider(p)
	lat2, lon2 := destinationPoint(-27.0, 153.0, 90*math.Pi/180, 6*metersPerNauticalMile)
	lat3, lon3 := destinationPoint(lat2, lon2, 0, 6*metersPerNauticalMile)
	route := fmt.Sprintf(`,"route":[{"lat":-27.0,"lon":153.0},{"lat":%.5f,"lon":%.5f},{"lat":%.5f,"lon":%.5f}]`, lat2, lon2, lat3, lon3)
	res := runTidalDeparture(t, deps, tidalArgs(90, 0, 3, tidalOneCycleWindow+route))
	if res.RankingBasis != "tide_phase" {
		t.Fatalf("setup: expected tide_phase, got %q", res.RankingBasis)
	}
	if math.Abs(*res.Best[0].MeanAssist) < 0.05 {
		t.Errorf("the north leg runs along the flood, so the score cannot be 0: %+v", res.Best[0])
	}
	if strings.Contains(res.Note, "across the stream") {
		t.Errorf("a route with a leg along the stream is not across it: %q", res.Note)
	}
}

// ---- the model always ranks where it has data; tide_phase only with none ----

// longRoute is a 40 nm route heading 150 from -27, 153.
func longRoute() string {
	lat2, lon2 := destinationPoint(-27.0, 153.0, 150*math.Pi/180, 40*metersPerNauticalMile)
	return fmt.Sprintf(`,"route":[{"lat":-27.0,"lon":153.0},{"lat":%.5f,"lon":%.5f}]`, lat2, lon2)
}

func TestPlanTidalDeparture_NullEndsAndMiddleStillRankOnTheModelAndAreListed(t *testing.T) {
	const kn = 0.5 * 1.943844
	endLat, endLon := destinationPoint(-27.0, 153.0, 150*math.Pi/180, 40*metersPerNauticalMile)
	startCell, endCell := roundToTidalGrid(-27.0, 153.0), roundToTidalGrid(endLat, endLon)
	// The model lacks the first cell, the last cell, and 16 to 24 nm in the middle.
	p := coverageStub(func(lat, lon float64) bool {
		c := roundToTidalGrid(lat, lon)
		d := distNmFromStart(lat, lon)
		return c == startCell || c == endCell || (d > 16 && d < 24)
	})
	res := runTidalDeparture(t, tidalDepsWithProvider(p), tidalModelArgs(150, 8, tidalEarlyWindow+longRoute()))
	if res.RankingBasis != "modelled_current" {
		t.Fatalf("a partly covered run is still ranked on the model, got %q (%s)", res.RankingBasis, res.ModelledCurrentMissing)
	}
	c := res.Best[0]
	if c.CoveredFraction == nil || *c.CoveredFraction < 0.5 || *c.CoveredFraction > 0.95 {
		t.Fatalf("expected a covered fraction between 0.5 and 0.95, got %+v", c.CoveredFraction)
	}
	if len(c.Uncovered) != 3 {
		t.Fatalf("expected the start, middle and end stretches, got %+v", c.Uncovered)
	}
	if c.Uncovered[0].FromNm != 0 || c.Uncovered[1].FromNm < 10 || c.Uncovered[1].ToNm > 30 || c.Uncovered[2].ToNm < 39 {
		t.Errorf("stretches in the wrong places: %+v", c.Uncovered)
	}
	if c.MeanAlongCourseKn == nil || math.Abs(*c.MeanAlongCourseKn-kn) > 0.05 {
		t.Errorf("the mean is over the covered samples only (about %.2f kn): %+v", kn, c)
	}
	if c.FairFraction < 0.99 {
		t.Errorf("every covered sample is fair, got %v", c.FairFraction)
	}
}

func TestUncoveredStretches_EachSampleCoversHalfASpacingEitherSideClampedThenMerged(t *testing.T) {
	dist := []float64{0, 2, 4, 6, 8, 10}
	cases := []struct {
		name    string
		covered []bool
		want    []assistantUncoveredStretch
	}{
		{"merged and single", []bool{false, false, true, false, true, true}, []assistantUncoveredStretch{{FromNm: 0, ToNm: 3}, {FromNm: 5, ToNm: 7}}},
		{"last sample only", []bool{true, true, true, true, true, false}, []assistantUncoveredStretch{{FromNm: 9, ToNm: 10}}},
		{"first sample only", []bool{false, true, true, true, true, true}, []assistantUncoveredStretch{{FromNm: 0, ToNm: 1}}},
		{"all covered", []bool{true, true, true, true, true, true}, nil},
	}
	for _, tc := range cases {
		got := uncoveredStretches(dist, tc.covered, 10)
		if len(got) != len(tc.want) {
			t.Errorf("%s: got %+v, want %+v", tc.name, got, tc.want)
			continue
		}
		for i := range got {
			if got[i] != tc.want[i] {
				t.Errorf("%s: got %+v, want %+v", tc.name, got, tc.want)
			}
		}
	}
}

func TestPlanTidalDeparture_HoursThePastTheSeriesAreUncoveredNotASwitch(t *testing.T) {
	// The series stops at 04:00Z. A 3 hour run leaving at 02:00Z is partly past it
	// and one leaving at 04:15Z or later is wholly past it.
	p := steadyCurrent(150)
	p.hours = 29
	deps := tidalDepsWithProvider(p)
	res := runTidalDeparture(t, deps, tidalModelArgs(150, 3, `,"earliest":"2026-09-12T02:00:00Z","latest":"2026-09-12T06:00:00Z"`))
	if res.RankingBasis != "modelled_current" {
		t.Fatalf("hours past the series are uncovered samples, not a switch to the tide: %q", res.RankingBasis)
	}
	all := append(append([]assistantTidalCandidate{}, res.Best...), res.Worst)
	seenBare := false
	for _, c := range all {
		bare := c.MeanAlongCourseKn == nil
		if bare {
			seenBare = true
			if c.AlongCourseMissing == "" || (c.CoveredFraction != nil && *c.CoveredFraction != 0) {
				t.Errorf("a candidate with no data says why and covers none: %+v", c)
			}
		} else if seenBare && c.DepartureISO != res.Worst.DepartureISO {
			t.Errorf("a candidate with no data ranks last, found one with figures after it: %+v", all)
		}
	}
	if res.Best[0].MeanAlongCourseKn == nil || res.Best[0].CoveredFraction == nil || *res.Best[0].CoveredFraction < 0.6 {
		t.Errorf("the best candidate should have most of its run covered: %+v", res.Best[0])
	}
}

func TestPlanTidalDeparture_TidePhaseOnlyWhenTheModelHasNoDataAtAll(t *testing.T) {
	var buf bytes.Buffer
	log.SetOutput(&buf)
	t.Cleanup(func() { log.SetOutput(os.Stderr) })
	p := steadyCurrent(150)
	p.at = func(lat, lon float64, ts time.Time) (float64, float64, bool) { return 0, 0, false }
	deps := tidalDepsWithProvider(p)
	res := runTidalDeparture(t, deps, tidalArgs(150, 150, 3, tidalEarlyWindow))
	if res.RankingBasis != "tide_phase" || res.ModelledCurrentMissing == "" {
		t.Fatalf("every cell null should use tide_phase with a reason, got %q %q", res.RankingBasis, res.ModelledCurrentMissing)
	}
	if !strings.Contains(buf.String(), "tide_phase used") || !strings.Contains(buf.String(), "no current") {
		t.Errorf("expected a log line with the reason, got %q", buf.String())
	}
	_, _, err := deps.execute(context.Background(), "plan_tidal_departure", json.RawMessage(tidalModelArgs(150, 3, tidalEarlyWindow)))
	if err == nil || !strings.Contains(err.Error(), "no current") || !strings.Contains(err.Error(), "flood_set_deg") {
		t.Errorf("expected both gaps named, got %v", err)
	}
}

func TestPlanTidalDeparture_NoWavePluginMeansNoModelDataButABadSettingIsAnError(t *testing.T) {
	deps := tidalDeps(tidalFixtureExtremes(-2, 40))
	deps.waves = func() (waveProvider, string, error) {
		return nil, "", fmt.Errorf("unknown wave provider configured: %q: %w", "open-meteo-marine", errNoWaveProviderInstalled)
	}
	res := runTidalDeparture(t, deps, tidalArgs(150, 150, 3, tidalEarlyWindow))
	if res.RankingBasis != "tide_phase" || !strings.Contains(res.ModelledCurrentMissing, "wave") {
		t.Errorf("no wave plugin installed is no model data, got %q %q", res.RankingBasis, res.ModelledCurrentMissing)
	}
}

func TestResolveWaveProvider_BlankSettingWithNoPluginIsNotInstalledButANamedMissingOneIsNot(t *testing.T) {
	orig := waveProviderRegistry
	waveProviderRegistry = map[string]waveProvider{}
	t.Cleanup(func() { waveProviderRegistry = orig })
	settingsPath := writeTestSettings(t, "203.0.113.1", 3000) // ui.wave_provider is blank
	if _, _, err := resolveWaveProvider(settingsPath); !errors.Is(err, errNoWaveProviderInstalled) {
		t.Errorf("blank setting and nothing installed should be errNoWaveProviderInstalled, got %v", err)
	}
	settings, _ := readSettings(settingsPath)
	ui, _ := settings["ui"].(map[string]any)
	if ui == nil {
		ui = map[string]any{}
	}
	ui["wave_provider"] = "gone"
	settings["ui"] = ui
	if err := writeSettings(settingsPath, settings); err != nil {
		t.Fatal(err)
	}
	if _, _, err := resolveWaveProvider(settingsPath); err == nil || errors.Is(err, errNoWaveProviderInstalled) {
		t.Errorf("a named plugin that is missing is a configuration fault, got %v", err)
	}
}

func TestPlanTidalDeparture_RouteStartingAwayFromThePositionProceedsWithANote(t *testing.T) {
	deps := tidalDepsWithProvider(steadyCurrent(150))
	// 12 nm south of lat/lon: well beyond 2 nm, short of 20 nm.
	route := `,"route":[{"lat":-27.2,"lon":153.0},{"lat":-27.4,"lon":153.1}]`
	res := runTidalDeparture(t, deps, tidalModelArgs(150, 3, tidalEarlyWindow+route))
	if res.RankingBasis != "modelled_current" || !strings.Contains(res.Note, "12.0 nm") {
		t.Errorf("expected the route used and the distance named, got %q note %q", res.RankingBasis, res.Note)
	}
	far := `,"route":[{"lat":-28.0,"lon":153.0},{"lat":-28.2,"lon":153.1}]`
	_, _, err := deps.execute(context.Background(), "plan_tidal_departure", json.RawMessage(tidalModelArgs(150, 3, tidalEarlyWindow+far)))
	if err == nil || !strings.Contains(err.Error(), "20 nm") {
		t.Errorf("a route 60 nm away is a different passage, got %v", err)
	}
	near := `,"route":[{"lat":-27.02,"lon":153.0},{"lat":-27.2,"lon":153.1}]`
	if res := runTidalDeparture(t, deps, tidalModelArgs(150, 3, tidalEarlyWindow+near)); strings.Contains(res.Note, "nm from") {
		t.Errorf("within 2 nm needs no note, got %q", res.Note)
	}
}

func TestPlanTidalDeparture_DefaultLatestFollowsTheClampedEarliest(t *testing.T) {
	// The current grows with time, so the best departure is the last one tried.
	p := steadyCurrent(150)
	p.at = func(lat, lon float64, ts time.Time) (float64, float64, bool) {
		return 0.1 + 0.01*ts.Sub(p.from).Hours(), 150, true
	}
	deps := tidalDepsWithProvider(p)
	now := deps.now()
	res := runTidalDeparture(t, deps, tidalModelArgs(150, 3, fmt.Sprintf(`,"earliest":%q`, now.Add(-30*time.Hour).Format(time.RFC3339))))
	if got := isoAt(t, res.Best[0].DepartureISO); got.Before(now.Add(23 * time.Hour)) {
		t.Errorf("the default window should run 24 h from now, best (latest) departure %s", got)
	}
	past := fmt.Sprintf(`,"earliest":%q,"latest":%q`, now.Add(-30*time.Hour).Format(time.RFC3339), now.Add(-2*time.Hour).Format(time.RFC3339))
	_, _, err := deps.execute(context.Background(), "plan_tidal_departure", json.RawMessage(tidalModelArgs(150, 3, past)))
	if err == nil || !strings.Contains(err.Error(), "already past") {
		t.Errorf("a window wholly in the past needs clear wording, got %v", err)
	}
}

// ---- sixth review ----

func TestPlanTidalDeparture_NoDataDeparturesAreNeverBestOrWorst(t *testing.T) {
	p := steadyCurrent(150)
	p.hours = 29 // the series stops at 04:00Z
	res := runTidalDeparture(t, tidalDepsWithProvider(p), tidalModelArgs(150, 3, `,"earliest":"2026-09-12T02:00:00Z","latest":"2026-09-12T08:00:00Z"`))
	for _, c := range append(append([]assistantTidalCandidate{}, res.Best...), res.Worst) {
		if c.MeanAlongCourseKn == nil {
			t.Errorf("a departure with no data must not be best or worst: %+v", c)
		}
	}
}

// risingCurrent grows with the hour, so a later departure always has the higher
// mean, and the series ends at 08:00Z.
func risingCurrent() *stubCurrentsWaveProvider {
	p := steadyCurrent(150)
	p.hours = 33
	p.at = func(lat, lon float64, ts time.Time) (float64, float64, bool) {
		return 0.1 + 0.05*ts.Sub(p.from).Hours(), 150, true
	}
	return p
}

func TestPlanTidalDeparture_APartlyCoveredDepartureDoesNotOutrankAFullyCoveredOne(t *testing.T) {
	// 3 hour runs. 05:00Z is covered to the end; 07:00Z has a third of its run
	// covered but the highest mean, so only coverage can keep it from winning.
	res := runTidalDeparture(t, tidalDepsWithProvider(risingCurrent()), tidalModelArgs(150, 3, `,"earliest":"2026-09-12T02:00:00Z","latest":"2026-09-12T08:00:00Z"`))
	for _, c := range append(append([]assistantTidalCandidate{}, res.Best...), res.Worst) {
		if c.CoveredFraction == nil || *c.CoveredFraction < assistantTidalMinCoveredFraction {
			t.Errorf("only departures covered at least %.0f%% rank best or worst: %+v", assistantTidalMinCoveredFraction*100, c)
		}
	}
	if strings.Contains(res.Note, "mostly uncovered") {
		t.Errorf("full coverage exists, so no warning: %q", res.Note)
	}
}

func TestPlanTidalDeparture_EveryRunMostlyUncoveredStillRanksAndSaysSo(t *testing.T) {
	res := runTidalDeparture(t, tidalDepsWithProvider(risingCurrent()), tidalModelArgs(150, 3, `,"earliest":"2026-09-12T07:00:00Z","latest":"2026-09-12T07:45:00Z"`))
	if res.RankingBasis != "modelled_current" || len(res.Best) == 0 || res.Best[0].MeanAlongCourseKn == nil {
		t.Fatalf("departures with data still rank: %+v", res)
	}
	if !strings.Contains(res.Note, "mostly uncovered") {
		t.Errorf("expected the warning, got %q", res.Note)
	}
}

type shortBundleProvider struct{ *stubCurrentsWaveProvider }

func (s shortBundleProvider) FetchCurrents(ctx context.Context, points []currentsPoint, days int) (currentsBundle, error) {
	b, err := s.stubCurrentsWaveProvider.FetchCurrents(ctx, points, days)
	b.Points = b.Points[:len(b.Points)-1]
	return b, err
}

func TestPlanTidalDeparture_AnAnswerForTooFewPointsIsAnErrorNotAPanic(t *testing.T) {
	deps := tidalDepsWithProvider(shortBundleProvider{steadyCurrent(150)})
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("panicked: %v", r)
		}
	}()
	_, _, err := deps.execute(context.Background(), "plan_tidal_departure", json.RawMessage(tidalModelArgs(150, 3, tidalEarlyWindow)))
	if err == nil || !strings.Contains(err.Error(), "points") {
		t.Errorf("expected an error naming the point count, got %v", err)
	}
}

func TestNoModelDataReason_SummarisesEveryCell(t *testing.T) {
	a, b, c := tidalGridPoint{-27, 153}, tidalGridPoint{-27.1, 153}, tidalGridPoint{-27.2, 153}
	cur := tidalPointCurrents{series: map[tidalGridPoint]tidalCurrents{}, reason: map[tidalGridPoint]string{a: "null near shore", b: "null near shore", c: "plugin too old"}}
	got := noModelDataReason(cur, []tidalSample{{cell: a}, {cell: b}, {cell: c}})
	for _, want := range []string{"3 route cells", "null near shore", "plugin too old"} {
		if !strings.Contains(got, want) {
			t.Errorf("reason lacks %q: %s", want, got)
		}
	}
	if strings.Count(got, "null near shore") != 1 {
		t.Errorf("reasons are deduped: %s", got)
	}
}
