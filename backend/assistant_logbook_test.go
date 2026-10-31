package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"math"
	"os"
	"strings"
	"testing"
	"time"
)

// The fixture is trimmed from GET /api/passages and /api/cruises on the boat
// (10 of the 64 passages, 2026-10-28), so its shapes are the real ones:
// blank place names, a passage with no fuel figure, a rating, and a stay
// whose arrival and next departure carry different names.
type logbookFixture struct {
	Cruises  []cruiseRecord `json:"cruises"`
	Passages []struct {
		passageRecord
		Stats    passageStats `json:"stats"`
		StartLon *float64     `json:"start_lon"`
		StartLat *float64     `json:"start_lat"`
	} `json:"passages"`
}

func loadLogbookFixture(t *testing.T) (*passageStore, logbookFixture) {
	t.Helper()
	raw, err := os.ReadFile("testdata/passages/logbook_real_shape.json")
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	var fx logbookFixture
	if err := json.Unmarshal(raw, &fx); err != nil {
		t.Fatalf("parse fixture: %v", err)
	}
	s := newTestPassageStore(t)
	for _, c := range fx.Cruises {
		if _, err := s.InsertCruise(c); err != nil {
			t.Fatalf("insert cruise: %v", err)
		}
	}
	for _, p := range fx.Passages {
		if _, err := s.InsertPassage(p.passageRecord); err != nil {
			t.Fatalf("insert passage: %v", err)
		}
		buf, _ := json.Marshal(passageFigures{Stats: p.Stats, StartLon: p.StartLon, StartLat: p.StartLat})
		if _, err := s.db.Exec(`UPDATE passages SET stats_json = ? WHERE id = ?`, string(buf), p.ID); err != nil {
			t.Fatalf("store stats: %v", err)
		}
	}
	return s, fx
}

// logbookDeps wires the logbook and fails the test if anything reaches for
// the telemetry: with the logbook as the source there is no fallback to it.
func logbookDeps(t *testing.T, s *passageStore) assistantToolDeps {
	t.Helper()
	return assistantToolDeps{
		now:        func() time.Time { return time.Date(2026, 10, 28, 12, 0, 0, 0, time.UTC) },
		passageLog: func() *passageStore { return s },
		influxRange: func(string, time.Time, time.Time, string) ([]telemetryPoint, error) {
			t.Fatal("the passage tools must not read telemetry")
			return nil, nil
		},
	}
}

type listedPassage struct {
	Start         string   `json:"start"`
	End           string   `json:"end"`
	StartLocal    string   `json:"start_local"`
	EndLocal      string   `json:"end_local"`
	From          string   `json:"from"`
	To            string   `json:"to"`
	Cruise        string   `json:"cruise"`
	Rating        string   `json:"rating"`
	DistanceNm    float64  `json:"distance_nm"`
	UnderwayHours float64  `json:"underway_hours"`
	FuelBurnedL   *float64 `json:"fuel_burned_l"`
}

type listResult struct {
	Status       string          `json:"status"`
	Found        int             `json:"found"`
	Returned     int             `json:"returned"`
	LogbookFirst string          `json:"logbook_first_departure"`
	LogbookLast  string          `json:"logbook_last_arrival"`
	Note         string          `json:"note"`
	Passages     []listedPassage `json:"passages"`
	UnderWayNow  *struct {
		Start      string  `json:"start"`
		StartLocal string  `json:"start_local"`
		DistanceNm float64 `json:"distance_nm_so_far"`
	} `json:"under_way_now"`
}

func runTool(t *testing.T, d assistantToolDeps, name, args string, into any) string {
	t.Helper()
	out, _, err := d.execute(context.Background(), name, json.RawMessage(args))
	if err != nil {
		t.Fatalf("%s: %v", name, err)
	}
	if err := json.Unmarshal([]byte(out), into); err != nil {
		t.Fatalf("%s json: %v\n%s", name, err, out)
	}
	return out
}

func TestListPassages_ReadsTheLogbook(t *testing.T) {
	s, fx := loadLogbookFixture(t)
	var res listResult
	runTool(t, logbookDeps(t, s), "list_passages", `{}`, &res)

	if res.Found != 10 || res.Returned != 10 || len(res.Passages) != 10 {
		t.Fatalf("want 10 passages, got found %d returned %d", res.Found, res.Returned)
	}
	// Newest first, with the recorded names, rating and cruise.
	first := res.Passages[0]
	if first.From != "Dumbell Island" || first.To != "Haslewood Island" {
		t.Errorf("newest passage: %+v", first)
	}
	if first.FuelBurnedL == nil || math.Abs(*first.FuelBurnedL-35.9) > 0.05 {
		t.Errorf("fuel: %v", first.FuelBurnedL)
	}
	last := res.Passages[9]
	if last.From != "Pittwater" || last.To != "Newcastle Cruising Yacht Club" || last.Cruise != "NSW" {
		t.Errorf("oldest passage: %+v", last)
	}
	if last.FuelBurnedL != nil {
		t.Errorf("an unrecorded fuel figure must stay null, got %v", *last.FuelBurnedL)
	}
	rated := 0
	for _, p := range res.Passages {
		if p.Rating == "Comfortable" {
			rated++
		}
	}
	if rated != 1 {
		t.Errorf("want the one recorded rating, got %d", rated)
	}
	// Times are also given in the zone at the passage's own start position
	// (ADR 0230), the way the Passages page shows them.
	if first.StartLocal != "2026-10-27 10:16 AEST (UTC+10)" || first.EndLocal != "2026-10-27 11:14 AEST (UTC+10)" {
		t.Errorf("local time: %q to %q", first.StartLocal, first.EndLocal)
	}
	if !strings.HasPrefix(res.LogbookFirst, "2026-02-28T") || !strings.HasPrefix(res.LogbookLast, "2026-10-27T") {
		t.Errorf("logbook span: %q to %q", res.LogbookFirst, res.LogbookLast)
	}
	// A year before the telemetry window: no 60-day ceiling.
	if _, err := time.Parse(time.RFC3339, last.Start); err != nil {
		t.Errorf("start not RFC3339: %v", err)
	}
	_ = fx
}

func TestListPassages_RangeLimitAndHidden(t *testing.T) {
	s, fx := loadLogbookFixture(t)
	hide := fx.Passages[0].ID // the oldest
	if _, err := s.db.Exec(`UPDATE passages SET hidden = 1 WHERE id = ?`, hide); err != nil {
		t.Fatal(err)
	}
	d := logbookDeps(t, s)
	tests := []struct {
		name       string
		args       string
		found, ret int
		newestFrom string
	}{
		{"hidden passages are not in the logbook", `{}`, 9, 9, "Dumbell Island"},
		{"from date", `{"from":"2026-10-01"}`, 4, 4, "Dumbell Island"},
		{"a bare date is the boat's day: Hayman left 22:13Z on the 9th, the 10th at +10", `{"from":"2026-09-01","to":"2026-09-09"}`, 0, 0, ""},
		{"to date is inclusive of that local day", `{"from":"2026-09-01","to":"2026-09-10"}`, 1, 1, "Hayman Island"},
		{"Hamilton left 23:03Z on the 13th, the 14th at +10", `{"from":"2026-09-01","to":"2026-09-13"}`, 1, 1, "Hayman Island"},
		{"limit keeps the newest", `{"limit":2}`, 9, 2, "Dumbell Island"},
		{"RFC3339 bound", `{"from":"2026-10-24T00:00:00Z","to":"2026-10-24T23:59:59Z"}`, 1, 1, "Fairey Reef #2"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var res listResult
			runTool(t, d, "list_passages", tc.args, &res)
			if res.Found != tc.found || res.Returned != tc.ret {
				t.Fatalf("found %d returned %d, want %d and %d", res.Found, res.Returned, tc.found, tc.ret)
			}
			if tc.ret == 0 {
				return
			}
			if res.Passages[0].From != tc.newestFrom {
				t.Errorf("newest from %q, want %q", res.Passages[0].From, tc.newestFrom)
			}
		})
	}
}

func TestListPassages_EmptyAndMisses(t *testing.T) {
	empty := newTestPassageStore(t)
	var res listResult
	runTool(t, logbookDeps(t, empty), "list_passages", `{}`, &res)
	if res.Status != "empty_logbook" || res.Found != 0 || len(res.Passages) != 0 {
		t.Fatalf("empty logbook: %+v", res)
	}
	for _, want := range []string{"no passages", "not from the telemetry"} {
		if !strings.Contains(res.Note, want) {
			t.Errorf("note %q should say %q", res.Note, want)
		}
	}

	s, _ := loadLogbookFixture(t)
	runTool(t, logbookDeps(t, s), "list_passages", `{"from":"2025-01-01","to":"2025-12-31"}`, &res)
	if res.Status != "none_in_range" || res.Found != 0 || res.LogbookFirst == "" {
		t.Fatalf("range with none: %+v", res)
	}
}

func TestPassageTools_FailPlainly(t *testing.T) {
	s, _ := loadLogbookFixture(t)
	d := logbookDeps(t, s)
	for _, tool := range []string{"list_passages", "get_cruise_summary"} {
		for _, args := range []string{`{"from":"yesterday"}`, `{"to":"2026-13-40"}`, `{"from":"2026-10-02","to":"2026-10-01"}`} {
			if _, _, err := d.execute(context.Background(), tool, json.RawMessage(args)); err == nil {
				t.Errorf("%s %s: want an error", tool, args)
			}
		}
		none := assistantToolDeps{now: d.now}
		if _, _, err := none.execute(context.Background(), tool, nil); err == nil || !strings.Contains(err.Error(), "passage log is not running") {
			t.Errorf("%s with no store: %v", tool, err)
		}
		gone := assistantToolDeps{now: d.now, passageLog: func() *passageStore { return nil }}
		if _, _, err := gone.execute(context.Background(), tool, nil); err == nil {
			t.Errorf("%s with a nil store: want an error", tool)
		}
	}
}

type summaryStay struct {
	Place         string  `json:"place"`
	Arrived       string  `json:"arrived"`
	Departed      string  `json:"departed"`
	Days          float64 `json:"days"`
	NextDepartsAt string  `json:"next_departure_place"`
	HiddenBetween bool    `json:"hidden_passage_between"`
}

type summaryResult struct {
	Status        string  `json:"status"`
	PassageCount  int     `json:"passage_count"`
	FirstPlace    string  `json:"first_place"`
	LastPlace     string  `json:"last_place"`
	FirstDepart   string  `json:"first_departure"`
	LastArrival   string  `json:"last_arrival"`
	TotalNm       float64 `json:"total_distance_nm"`
	UnderwayHours float64 `json:"underway_hours"`
	Fuel          struct {
		BurnedL            float64 `json:"burned_l"`
		PassagesRecorded   int     `json:"passages_with_fuel"`
		PassagesUnrecorded int     `json:"passages_without_fuel"`
	} `json:"fuel"`
	Route   []string `json:"route"`
	Cruises []struct {
		Name     string `json:"name"`
		Passages int    `json:"passages"`
	} `json:"cruises"`
	LongestPassages []listedPassage `json:"longest_passages"`
	StaysTotal      int             `json:"stays_total"`
	Stays           []summaryStay   `json:"longest_stays"`
	Note            string          `json:"note"`
	UnderWayNow     *struct {
		DistanceNm float64 `json:"distance_nm_so_far"`
	} `json:"under_way_now"`
}

func TestGetCruiseSummary_WholeLogbook(t *testing.T) {
	s, fx := loadLogbookFixture(t)
	var res summaryResult
	runTool(t, logbookDeps(t, s), "get_cruise_summary", `{}`, &res)

	var wantNm, wantFuel float64
	withFuel := 0
	for _, p := range fx.Passages {
		wantNm += p.Stats.DistanceNM
		if p.Stats.FuelBurnedL != nil {
			wantFuel += *p.Stats.FuelBurnedL
			withFuel++
		}
	}
	if res.PassageCount != 10 || math.Abs(res.TotalNm-round1(wantNm)) > 0.11 {
		t.Errorf("count %d, distance %v want %v", res.PassageCount, res.TotalNm, wantNm)
	}
	if res.FirstPlace != "Pittwater" || res.LastPlace != "Haslewood Island" {
		t.Errorf("places from the records: %q to %q", res.FirstPlace, res.LastPlace)
	}
	if !strings.HasPrefix(res.FirstDepart, "2026-02-28T") || !strings.HasPrefix(res.LastArrival, "2026-10-27T") {
		t.Errorf("span: %q to %q", res.FirstDepart, res.LastArrival)
	}
	// Fuel is summed only over the passages that recorded it, and says so.
	if res.Fuel.PassagesRecorded != withFuel || res.Fuel.PassagesUnrecorded != 10-withFuel || math.Abs(res.Fuel.BurnedL-round1(wantFuel)) > 0.11 {
		t.Errorf("fuel: %+v want %v over %d", res.Fuel, wantFuel, withFuel)
	}
	if res.UnderwayHours <= 0 {
		t.Errorf("underway hours: %v", res.UnderwayHours)
	}
	if len(res.Route) == 0 || res.Route[0] != "Pittwater" || res.Route[len(res.Route)-1] != "Haslewood Island" {
		t.Errorf("route: %v", res.Route)
	}
	if len(res.LongestPassages) == 0 || res.LongestPassages[0].To != "Darley Reef" {
		t.Errorf("longest passage: %+v", res.LongestPassages)
	}
	cruises := map[string]int{}
	for _, c := range res.Cruises {
		cruises[c.Name] = c.Passages
	}
	if cruises["NSW"] != 2 || cruises["Coral Sea"] != 2 || cruises["GBR Southbound"] != 3 {
		t.Errorf("cruises: %v", cruises)
	}

	// Nine gaps. The longest is the three months at K'gari.
	if res.StaysTotal != 9 || len(res.Stays) == 0 {
		t.Fatalf("stays: total %d, listed %d", res.StaysTotal, len(res.Stays))
	}
	top := res.Stays[0]
	if top.Place != "K'gari" || math.Abs(top.Days-96.9) > 0.1 {
		t.Errorf("longest stay: %+v", top)
	}
	// The boat left from a differently named place: the tool reports both
	// names and does not decide which is right.
	if top.NextDepartsAt != "Hayman Island" {
		t.Errorf("departure place: %q", top.NextDepartsAt)
	}
	if res.Stays[1].Place != "Cromartys Bay" || res.Stays[1].NextDepartsAt != "Goat Island" {
		t.Errorf("second stay: %+v", res.Stays[1])
	}
	for i := 1; i < len(res.Stays); i++ {
		if res.Stays[i].Days > res.Stays[i-1].Days {
			t.Errorf("stays must run longest first: %+v", res.Stays)
		}
	}
}

func TestGetCruiseSummary_RangeAndGaps(t *testing.T) {
	s, _ := loadLogbookFixture(t)
	d := logbookDeps(t, s)
	tests := []struct {
		name       string
		args       string
		count      int
		first, end string
		stays      int
	}{
		{"october", `{"from":"2026-10-01"}`, 4, "Breakwater Marina", "Haslewood Island", 3},
		{"two passages one gap", `{"from":"2026-02-01","to":"2026-03-31"}`, 2, "Pittwater", "Cromartys Bay", 1},
		{"single passage has no stay", `{"from":"2026-10-27","to":"2026-10-27"}`, 1, "Dumbell Island", "Haslewood Island", 0},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var res summaryResult
			runTool(t, d, "get_cruise_summary", tc.args, &res)
			if res.PassageCount != tc.count || res.FirstPlace != tc.first || res.LastPlace != tc.end || res.StaysTotal != tc.stays {
				t.Fatalf("got %d %q %q stays %d, want %d %q %q stays %d", res.PassageCount, res.FirstPlace, res.LastPlace, res.StaysTotal, tc.count, tc.first, tc.end, tc.stays)
			}
		})
	}
}

func TestGetCruiseSummary_BlankNamesAreNotInvented(t *testing.T) {
	s, _ := loadLogbookFixture(t)
	var res summaryResult
	runTool(t, logbookDeps(t, s), "get_cruise_summary", `{"from":"2026-06-01","to":"2026-06-30"}`, &res)
	if res.PassageCount != 2 {
		t.Fatalf("count: %d", res.PassageCount)
	}
	// Goat Island -> (blank) then (blank) -> K'gari: the stay between has no
	// recorded place, and the tool leaves it blank.
	if len(res.Stays) != 1 || res.Stays[0].Place != "" || res.Stays[0].NextDepartsAt != "" {
		t.Errorf("stay: %+v", res.Stays)
	}
	for _, p := range res.Route {
		if p == "" {
			t.Errorf("an unnamed place must not appear in the route: %v", res.Route)
		}
	}
	if !strings.Contains(res.Note, "no place name") || !strings.Contains(res.Note, "stay") {
		t.Errorf("note should say some places are unnamed: %q", res.Note)
	}
}

// With no fuel figure on any passage in the range, the total is null, not 0.
func TestGetCruiseSummary_NoFuelRecordedIsNull(t *testing.T) {
	s, _ := loadLogbookFixture(t)
	var res struct {
		Fuel struct {
			BurnedL *float64 `json:"burned_l"`
			With    int      `json:"passages_with_fuel"`
		} `json:"fuel"`
	}
	runTool(t, logbookDeps(t, s), "get_cruise_summary", `{"from":"2026-02-01","to":"2026-03-31"}`, &res)
	if res.Fuel.With != 0 || res.Fuel.BurnedL != nil {
		t.Errorf("fuel: %+v", res.Fuel)
	}
}

func TestGetCruiseSummary_Empty(t *testing.T) {
	var res summaryResult
	runTool(t, logbookDeps(t, newTestPassageStore(t)), "get_cruise_summary", `{}`, &res)
	if res.Status != "empty_logbook" || res.PassageCount != 0 || !strings.Contains(res.Note, "not from the telemetry") {
		t.Fatalf("empty logbook: %+v", res)
	}
	s, _ := loadLogbookFixture(t)
	runTool(t, logbookDeps(t, s), "get_cruise_summary", `{"from":"2025-01-01","to":"2025-02-01"}`, &res)
	if res.Status != "none_in_range" || res.PassageCount != 0 {
		t.Fatalf("range with none: %+v", res)
	}
}

func TestPassageTools_OverlapMakesNoNegativeStay(t *testing.T) {
	s := newTestPassageStore(t)
	t0 := time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC)
	for _, p := range []passageRecord{
		{Start: t0, End: t0.Add(4 * time.Hour), FromName: "A", ToName: "B"},
		{Start: t0.Add(3 * time.Hour), End: t0.Add(8 * time.Hour), FromName: "B", ToName: "C"},
	} {
		if _, err := s.InsertPassage(p); err != nil {
			t.Fatal(err)
		}
	}
	var res summaryResult
	runTool(t, logbookDeps(t, s), "get_cruise_summary", `{}`, &res)
	if res.StaysTotal != 0 || res.PassageCount != 2 {
		t.Errorf("%+v", res)
	}
}

func TestPassageTools_Registered(t *testing.T) {
	for _, name := range []string{"list_passages", "get_cruise_summary"} {
		if lookupMateTool(name) == nil {
			t.Errorf("%s is not in the registry", name)
		}
	}
	d := lookupMateTool("list_passages").function.Description
	for _, want := range []string{"Passages logbook", "get_path_history"} {
		if !strings.Contains(d, want) {
			t.Errorf("list_passages description should mention %q", want)
		}
	}
	if strings.Contains(d, "InfluxDB") || strings.Contains(d, "speed over ground") {
		t.Errorf("list_passages must no longer describe the telemetry: %s", d)
	}
}

var _ = sql.ErrNoRows

func TestPassageTools_RatingIsTheLabelOnThePage(t *testing.T) {
	s := newTestPassageStore(t)
	t0 := time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC)
	for i, r := range []string{"comfortable", "lively", "shouldnt_have_gone", ""} {
		at := t0.Add(time.Duration(i) * 24 * time.Hour)
		if _, err := s.InsertPassage(passageRecord{Start: at, End: at.Add(time.Hour), Rating: r}); err != nil {
			t.Fatal(err)
		}
	}
	var res listResult
	runTool(t, logbookDeps(t, s), "list_passages", `{}`, &res)
	var got []string
	for _, p := range res.Passages {
		got = append(got, p.Rating)
	}
	if strings.Join(got, "|") != "|Shouldn't have gone|Lively|Comfortable" {
		t.Errorf("ratings: %q", got)
	}
}

// The last place and the last arrival come from one passage, the one that
// finished last, even when a passage that started later finished earlier.
func TestGetCruiseSummary_LastPlaceAndArrivalAgree(t *testing.T) {
	s := newTestPassageStore(t)
	t0 := time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC)
	for _, p := range []passageRecord{
		{Start: t0, End: t0.Add(10 * time.Hour), FromName: "A", ToName: "B"},
		{Start: t0.Add(time.Hour), End: t0.Add(5 * time.Hour), FromName: "C", ToName: "D"},
	} {
		if _, err := s.InsertPassage(p); err != nil {
			t.Fatal(err)
		}
	}
	var res summaryResult
	runTool(t, logbookDeps(t, s), "get_cruise_summary", `{}`, &res)
	if res.LastPlace != "B" || res.LastArrival != "2026-05-01T10:00:00Z" {
		t.Errorf("last: %q at %q", res.LastPlace, res.LastArrival)
	}
}

// A hidden passage is left out of the listing and the totals, but it was a
// move of the boat, so a stay it splits is flagged and not silently merged.
func TestGetCruiseSummary_HiddenPassageFlagsTheStay(t *testing.T) {
	s := newTestPassageStore(t)
	t0 := time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC)
	for _, p := range []passageRecord{
		{Start: t0, End: t0.Add(time.Hour), FromName: "A", ToName: "B"},
		{Start: t0.Add(48 * time.Hour), End: t0.Add(49 * time.Hour), FromName: "B", ToName: "B2", Hidden: true},
		{Start: t0.Add(96 * time.Hour), End: t0.Add(97 * time.Hour), FromName: "B2", ToName: "C"},
		{Start: t0.Add(200 * time.Hour), End: t0.Add(201 * time.Hour), FromName: "C", ToName: "D"},
	} {
		if _, err := s.InsertPassage(p); err != nil {
			t.Fatal(err)
		}
	}
	var res summaryResult
	runTool(t, logbookDeps(t, s), "get_cruise_summary", `{}`, &res)
	if res.PassageCount != 3 {
		t.Fatalf("hidden passages stay out of the count: %d", res.PassageCount)
	}
	flagged := 0
	for _, st := range res.Stays {
		if st.HiddenBetween {
			flagged++
		}
	}
	if res.StaysTotal != 2 || flagged != 1 {
		t.Errorf("stays %d, flagged %d: %+v", res.StaysTotal, flagged, res.Stays)
	}
}

// A passage under way is not in the logbook until it ends. The tools report
// it from the track log, apart from the totals, and say it is not finished.
func TestPassageTools_ReportThePassageUnderWay(t *testing.T) {
	s := newTestPassageStore(t)
	b := newTrackBuilder().run(90*time.Minute, time.Minute, 6)
	for _, p := range b.pts {
		if err := s.AppendTrackPoint(p); err != nil {
			t.Fatal(err)
		}
	}
	d := logbookDeps(t, s)
	var res listResult
	runTool(t, d, "list_passages", `{}`, &res)
	if res.UnderWayNow == nil || res.UnderWayNow.DistanceNm < 5 || res.UnderWayNow.StartLocal == "" {
		t.Fatalf("under way: %+v", res.UnderWayNow)
	}
	if res.Found != 0 || !strings.Contains(res.Note, "under way") {
		t.Errorf("found %d, note %q", res.Found, res.Note)
	}
	var sum summaryResult
	runTool(t, d, "get_cruise_summary", `{}`, &sum)
	if sum.UnderWayNow == nil || sum.PassageCount != 0 {
		t.Errorf("summary: %+v", sum)
	}

	// Once nothing is under way, there is no such field.
	quiet := newTestPassageStore(t)
	var none listResult
	runTool(t, logbookDeps(t, quiet), "list_passages", `{}`, &none)
	if none.UnderWayNow != nil {
		t.Errorf("under way with an empty track log: %+v", none.UnderWayNow)
	}
}

func TestPassageTools_StopWhenCancelled(t *testing.T) {
	s, _ := loadLogbookFixture(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	for _, tool := range []string{"list_passages", "get_cruise_summary"} {
		if _, _, err := logbookDeps(t, s).execute(ctx, tool, nil); err == nil {
			t.Errorf("%s ran on a cancelled context", tool)
		}
	}
}

// A New South Wales passage in summer is on daylight saving: the zone is the
// one at its start position, not a whole-hour offset from longitude.
func TestListPassages_UsesTheZoneAtThePassageStart(t *testing.T) {
	// start_lat in the fixture is the named place's approximate latitude.
	s, _ := loadLogbookFixture(t)
	var res listResult
	runTool(t, logbookDeps(t, s), "list_passages", `{}`, &res)
	pittwater := res.Passages[9]
	// 2026-02-28 20:11 UTC is 07:11 on 1 March at AEDT (UTC+11).
	if pittwater.StartLocal != "2026-03-01 07:11 AEDT (UTC+11)" {
		t.Errorf("Pittwater start_local = %q", pittwater.StartLocal)
	}
	// A bare date is a day in that zone: 1 March finds it, 28 February does not.
	var on, before listResult
	runTool(t, logbookDeps(t, s), "list_passages", `{"from":"2026-03-01","to":"2026-03-01"}`, &on)
	runTool(t, logbookDeps(t, s), "list_passages", `{"from":"2026-02-28","to":"2026-02-28"}`, &before)
	if on.Found != 1 || on.Passages[0].From != "Pittwater" || before.Found != 0 {
		t.Errorf("1 March found %d, 28 February found %d", on.Found, before.Found)
	}
}

// A passage with no recorded position has no zone. It lists without local
// times and says why on its own row.
func TestListPassages_NoRecordedPositionHasNoZone(t *testing.T) {
	s := newTestPassageStore(t)
	t0 := time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC)
	if _, err := s.InsertPassage(passageRecord{Start: t0, End: t0.Add(time.Hour)}); err != nil {
		t.Fatal(err)
	}
	var res struct {
		Found    int `json:"found"`
		Passages []struct {
			StartLocal string `json:"start_local"`
			EndLocal   string `json:"end_local"`
			Note       string `json:"time_zone_note"`
		} `json:"passages"`
	}
	runTool(t, logbookDeps(t, s), "list_passages", `{}`, &res)
	if res.Found != 1 || res.Passages[0].StartLocal != "" || res.Passages[0].EndLocal != "" || !strings.Contains(res.Passages[0].Note, "time zone is not known") {
		t.Errorf("want one passage with no local times and a reason, got %+v", res.Passages)
	}
}

// A bare date asked about is a day on each passage's own clock. A passage with
// no zone far from that day cannot be in it, so it does not matter; one that
// could be is listed with a note that its zone is not known. Neither fails the
// call.
func TestListPassages_BareDateAndPassagesWithNoZone(t *testing.T) {
	s := newTestPassageStore(t)
	far := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)
	near := time.Date(2026, 3, 1, 5, 0, 0, 0, time.UTC)
	for _, p := range []passageRecord{
		{Start: far, End: far.Add(time.Hour), Title: "far"},
		{Start: near, End: near.Add(time.Hour), Title: "near"},
	} {
		if _, err := s.InsertPassage(p); err != nil {
			t.Fatal(err)
		}
	}
	var res struct {
		Found    int `json:"found"`
		Passages []struct {
			Title string `json:"title"`
			Note  string `json:"time_zone_note"`
		} `json:"passages"`
	}
	runTool(t, logbookDeps(t, s), "list_passages", `{"from":"2026-03-01","to":"2026-03-01"}`, &res)
	if res.Found != 1 || res.Passages[0].Title != "near" || !strings.Contains(res.Passages[0].Note, "time zone is not known") {
		t.Errorf("want only the near passage, with its note, got %+v", res)
	}
}

// The passage under way keeps its place in the logbook even when its zone
// cannot be worked out.
func TestOpenRunZone(t *testing.T) {
	start := time.Date(2026, 10, 27, 0, 0, 0, 0, time.UTC)
	z, reason := openRunZone([]recordedTrackPoint{{At: start, Lat: -20, Lon: 149}}, start)
	if z == nil || reason != "" {
		t.Fatalf("a located run has a zone, got %v %q", z, reason)
	}
	z, reason = openRunZone(nil, start)
	if z != nil || !strings.Contains(reason, "time zone is not known") {
		t.Fatalf("a run with no point has a reason, got %v %q", z, reason)
	}
	z, reason = openRunZone([]recordedTrackPoint{{At: start, Lat: 95, Lon: 10}}, start)
	if z != nil || !strings.Contains(reason, "time zone") {
		t.Fatalf("an unplaceable point has a reason, got %v %q", z, reason)
	}
}
