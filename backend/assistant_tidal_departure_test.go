package main

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
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
	raw, err := deps.execute(context.Background(), "plan_tidal_departure", json.RawMessage(args))
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
	if got := isoAt(t, res.Best[0].DepartureISO); !got.Equal(tidalFixtureLW0) {
		t.Errorf("expected best departure at LW %s, got %s", tidalFixtureLW0, got)
	}
	if res.Best[0].MeanAssist < 0.6 || res.Best[0].MeanAssist > 0.66 {
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
	if res.Worst.MeanAssist > -0.55 {
		t.Errorf("expected strongly foul worst, got %v", res.Worst.MeanAssist)
	}
	if !strings.HasPrefix(res.Worst.Stream, "ebb") {
		t.Errorf("expected worst stream ebb, got %q", res.Worst.Stream)
	}
	if res.FloodSetSource != "standing_notes" || res.FloodSetDeg != 180 || res.CourseDeg != 180 {
		t.Errorf("expected inputs echoed, got %+v", res)
	}
	if res.Station.ID != "S1" || res.TimeBasis != "UTC" || res.Basis == "" {
		t.Errorf("expected station, time_basis and basis, got %+v", res)
	}
	if res.Note != "" {
		t.Errorf("expected no note for an along-stream course, got %q", res.Note)
	}
	if !strings.HasPrefix(res.Best[0].Departure, "Sat 12 Sep 02:00") {
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
		if i > 0 && res.Best[i].MeanAssist > res.Best[i-1].MeanAssist {
			t.Errorf("expected best first, got %v then %v", res.Best[i-1].MeanAssist, res.Best[i].MeanAssist)
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
			_, err := tc.deps.execute(context.Background(), "plan_tidal_departure", json.RawMessage(tc.args))
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

func TestAssistantToolDefinitions_PlanTidalDepartureRequiresSetAndSource(t *testing.T) {
	for _, tool := range assistantToolDefinitions() {
		if tool.Function.Name != "plan_tidal_departure" {
			continue
		}
		params := string(tool.Function.Parameters)
		for _, want := range []string{"flood_set_deg", "flood_set_source", "standing_notes", "general_knowledge", "operator", "course_deg", "passage_hours", "slack_offset_min"} {
			if !strings.Contains(params, want) {
				t.Errorf("expected parameters to mention %q, got %s", want, params)
			}
		}
		return
	}
	t.Fatal("plan_tidal_departure tool definition not found")
}
