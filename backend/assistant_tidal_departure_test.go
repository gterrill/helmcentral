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
		now:   func() time.Time { return tidalFixtureLW0.Add(time.Hour) },
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
		course, passage, floodSet, extra)
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
	fail  func(lat, lon float64) error

	mu       sync.Mutex
	calls    []string
	persists int
	lastDays int
}

func (s *stubCurrentsWaveProvider) SupportsCurrents() bool { return !s.unsupported }
func (s *stubCurrentsWaveProvider) PersistCurrents() {
	s.mu.Lock()
	s.persists++
	s.mu.Unlock()
}
func (s *stubCurrentsWaveProvider) FetchCurrents(ctx context.Context, lat, lon float64, days int) (currentsBundle, error) {
	if err := ctx.Err(); err != nil {
		return currentsBundle{}, err
	}
	s.mu.Lock()
	s.calls = append(s.calls, fmt.Sprintf("%.2f,%.2f", lat, lon))
	s.lastDays = days
	s.mu.Unlock()
	if s.delay > 0 {
		select {
		case <-time.After(s.delay):
		case <-ctx.Done():
			return currentsBundle{}, ctx.Err()
		}
	}
	if s.fail != nil {
		return currentsBundle{}, s.fail(lat, lon)
	}
	var out currentsBundle
	for i := 0; i < s.hours; i++ {
		ts := s.from.Add(time.Duration(i) * time.Hour)
		h := currentHourPoint{Time: ts}
		if v, set, ok := s.at(lat, lon, ts); ok {
			h.VelocityMS, h.SetDeg = &v, &set
		}
		out.Hourly = append(out.Hourly, h)
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
	return fmt.Sprintf(`{"lat":-27.0,"lon":153.0,"course_deg":%g,"passage_hours":%g%s}`, course, passage, extra)
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
	// Without the route, the single point sees only the eastward current.
	single := runTidalDeparture(t, deps, tidalModelArgs(90, 4, tidalEarlyWindow)).Best[0]
	if single.MeanAlongCourseKn == nil || *single.MeanAlongCourseKn < 0.85 {
		t.Errorf("single point, got %+v", single)
	}
	// Each distinct 0.05 degree point was fetched once, however many samples
	// fall on it.
	seen := map[string]int{}
	for _, c := range p.calls {
		seen[c]++
	}
	for pt, n := range seen {
		if n > 2 { // once per run above (route and single-point calls)
			t.Errorf("point %s fetched %d times", pt, n)
		}
	}
	if len(p.calls) > 2*(len(seen)) {
		t.Errorf("too many fetches: %v", p.calls)
	}
}

func TestPlanTidalDeparture_FallsBackToTidePhaseNamingWhatLackedData(t *testing.T) {
	// The model has nothing east of 153.1E, where the route goes.
	p := steadyCurrent(150)
	p.at = func(lat, lon float64, ts time.Time) (float64, float64, bool) {
		return 0.5, 150, lon < 153.1
	}
	deps := tidalDepsWithProvider(p)
	route := `,"route":[{"lat":-27.0,"lon":153.0},{"lat":-27.0,"lon":153.4}]`
	res := runTidalDeparture(t, deps, tidalArgs(90, 90, 3, tidalEarlyWindow+route))
	if res.RankingBasis != "tide_phase" {
		t.Fatalf("expected tide_phase, got %q", res.RankingBasis)
	}
	if !strings.Contains(res.ModelledCurrentMissing, "153.") || !strings.Contains(res.ModelledCurrentMissing, "no current") {
		t.Errorf("expected the missing point named, got %q", res.ModelledCurrentMissing)
	}
	if res.FloodSetDeg == nil || *res.FloodSetDeg != 90 || res.FloodSetSource != "standing_notes" {
		t.Errorf("the supplied set is the basis now: %+v", res)
	}
	for _, c := range append(append([]assistantTidalCandidate{}, res.Best...), res.Worst) {
		if c.MeanAssist == nil {
			t.Errorf("every candidate uses the same basis: %+v", c)
		}
		if c.AlongCourseMissing == "" || c.MeanAlongCourseKn != nil {
			t.Errorf("a run the model misses reports missing, not 0: %+v", c)
		}
	}
}

func TestPlanTidalDeparture_OneUncoveredCandidateMeansNoCandidateUsesTheModel(t *testing.T) {
	// The model covers up to 05:00Z. The window runs departures from 02:00Z to
	// 03:00Z with a 3 hour passage, so the late departures run past it.
	p := steadyCurrent(150)
	p.hours = 29 // 2026-09-11T00:00Z .. 2026-09-12T04:00Z
	deps := tidalDepsWithProvider(p)
	res := runTidalDeparture(t, deps, tidalArgs(150, 150, 3, `,"earliest":"2026-09-12T02:00:00Z","latest":"2026-09-12T03:00:00Z"`))
	if res.RankingBasis != "tide_phase" {
		t.Fatalf("expected tide_phase for the whole result, got %q", res.RankingBasis)
	}
	if !strings.Contains(res.ModelledCurrentMissing, "no current") {
		t.Errorf("expected the reason, got %q", res.ModelledCurrentMissing)
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
	if c.MeanAlongCourseKn != nil || c.PeakAlongCourseKn != nil || c.AlongCourseMissing == "" {
		t.Errorf("missing currents must not read as 0: %+v", c)
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

func TestPlanTidalDeparture_AFailedPointFetchNamesThePointAndFallsBack(t *testing.T) {
	p := steadyCurrent(150)
	p.fail = func(lat, lon float64) error { return fmt.Errorf("upstream returned 500") }
	deps := tidalDepsWithProvider(p)
	res := runTidalDeparture(t, deps, tidalArgs(150, 150, 3, tidalEarlyWindow))
	if res.RankingBasis != "tide_phase" || !strings.Contains(res.ModelledCurrentMissing, "current fetch for") || !strings.Contains(res.ModelledCurrentMissing, "500") {
		t.Errorf("expected the failed fetch reported, got %q %q", res.RankingBasis, res.ModelledCurrentMissing)
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

func TestPlanTidalDeparture_PersistsTheCurrentsCacheOncePerCall(t *testing.T) {
	p := steadyCurrent(150)
	deps := tidalDepsWithProvider(p)
	route := `,"route":[{"lat":-27.0,"lon":153.0},{"lat":-27.0,"lon":153.6}]`
	runTidalDeparture(t, deps, tidalModelArgs(90, 4, tidalEarlyWindow+route))
	if p.persists != 1 {
		t.Errorf("expected one cache write for the whole call, got %d", p.persists)
	}
}

// Open-Meteo's hourly series runs from 00:00 local today to 23:00 local on the
// last day. Whatever the zone and the time of day now falls in, the days asked
// for must leave the last sample bracketed by an hour at or after it.
func TestTidalCurrentDays_AlwaysBracketTheLastSample(t *testing.T) {
	for zone := -12; zone <= 14; zone++ {
		loc := time.FixedZone("z", zone*3600)
		for minute := 0; minute < 24*60; minute += 30 {
			now := time.Date(2026, 9, 12, 0, 0, 0, 0, loc).Add(time.Duration(minute) * time.Minute)
			for _, ahead := range []time.Duration{0, 30 * time.Minute, 23*time.Hour + 30*time.Minute, 24 * time.Hour, 47*time.Hour + 30*time.Minute, 5 * 24 * time.Hour} {
				need := now.Add(ahead)
				days := tidalCurrentDays(now, need)
				midnight := time.Date(now.In(loc).Year(), now.In(loc).Month(), now.In(loc).Day(), 0, 0, 0, 0, loc)
				lastHour := midnight.Add(time.Duration(days)*24*time.Hour - time.Hour)
				if lastHour.Before(need) {
					t.Fatalf("zone %+d now %s ahead %s: %d days end at %s, before %s", zone, now, ahead, days, lastHour, need)
				}
			}
		}
	}
}

func TestPlanTidalDeparture_ForecastDaysCoverALateEvening(t *testing.T) {
	p := steadyCurrent(150)
	deps := tidalDepsWithProvider(p)
	// The run ends at 23:30 UTC, an hour short of two whole days from now.
	runTidalDeparture(t, deps, tidalArgs(150, 150, 3, `,"earliest":"2026-09-12T03:00:00Z","latest":"2026-09-13T20:30:00Z"`))
	if p.lastDays < 4 {
		t.Errorf("a run that ends at 23:30 two days on needs more than 3 days of forecast, asked for %d", p.lastDays)
	}
}

func TestTidalSamples_ZeroLengthLegsTakeTheNextLegsCourse(t *testing.T) {
	// The route repeats its first point, then runs east.
	route := []assistantLatLon{{Lat: -27, Lon: 153}, {Lat: -27, Lon: 153}, {Lat: -27, Lon: 153.5}}
	for i, s := range tidalSamples(route, -27, 153, 0, 12) {
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
