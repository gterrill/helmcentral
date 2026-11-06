package main

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"
)

// The 2026-11-06 route: out of Island Head Creek to the north-east for 1.4 nm,
// then south to Long Beach, Great Keppel. A northerly is on the bow for the
// creek mouth and astern for the rest.
func pwIslandHeadLeg() passageLegArgs {
	return passageLegArgs{
		Route: []passageRoutePoint{
			{Lat: -22.36904, Lon: 150.64467, Name: "Island Head Creek"},
			{Lat: -22.34911, Lon: 150.66005},
			{Lat: -23.18, Lon: 150.97, Name: "Long Beach"},
		},
		PassageHours: pwF(6),
	}
}

func TestAssessItinerary_CellSummarisesWindBySector(t *testing.T) {
	a := pwArgs([]string{"2026-10-20"}, "06:00", pwIslandHeadLeg())
	fc := pwUniform(10, 0, 12, nil, 0, pwDay(19, 0), pwDay(25, 23))
	c := pwCell0(t, pwAssess(t, a, pwLimitsAll(), fc))
	if c.State != "go" {
		t.Fatalf("state = %q, want go", c.State)
	}
	if want := "Wind from N 10 kn: astern 5.5 h, ahead 0.5 h (first 0.5 h)"; c.wind != want {
		t.Fatalf("wind summary = %q, want %q", c.wind, want)
	}
}

func TestAssessItinerary_WindSummaryRangesAndNamesTheStretch(t *testing.T) {
	// Wind backs from 348 to 031 and builds from 6 to 15 kn across the day.
	hours := map[int64]passageHour{}
	dirs := []float64{348, 355, 5, 12, 20, 31, 31, 31}
	for i, d := range dirs {
		ts := pwDay(20, 6+i)
		hours[ts.Unix()] = passageHour{WindMS: kn(6 + float64(i)*9/7), GustMS: kn(16), WindDirDeg: d}
	}
	fc := forecastSource{Provider: "wx", Fetch: func(_ context.Context, lat, lon float64) (passageForecast, error) {
		return passageForecast{Hours: hours}, nil
	}}
	a := pwArgs([]string{"2026-10-20"}, "06:00", pwIslandHeadLeg())
	c := pwCell0(t, pwAssess(t, a, pwLimitsAll(), fc))
	if !strings.HasPrefix(c.wind, "Wind from NNW–NNE 6–14 kn: ") {
		t.Fatalf("wind summary = %q", c.wind)
	}
}

func TestAssessItinerary_GustTextNamesTheWindDirection(t *testing.T) {
	a := pwArgs([]string{"2026-10-20"}, "06:00", pwNorthLeg())
	limits := passageLimits{Ahead: passageSectorLimits{WindKnNoGo: pwF(20)}, Abeam: passageSectorLimits{WindKnNoGo: pwF(30)}, Astern: passageSectorLimits{WindKnNoGo: pwF(35)}}
	c := pwCell0(t, pwAssess(t, a, limits, pwUniform(5, 0, 16, nil, 0, pwDay(19, 0), pwDay(25, 23))))
	if c.Limiting == nil || c.Limiting.Factor != "gust" {
		t.Fatalf("limiting = %+v, want gust", c.Limiting)
	}
	if want := "Gusts 16 kn from N, ahead · Alpha → Bravo"; c.Limiting.Text != want {
		t.Fatalf("text = %q, want %q", c.Limiting.Text, want)
	}
}

func TestPassageModelCellCarriesTheWindSummary(t *testing.T) {
	a := pwArgs([]string{"2026-10-20"}, "06:00", pwIslandHeadLeg())
	p := pwAssess(t, a, pwLimitsAll(), pwUniform(10, 0, 12, nil, 0, pwDay(19, 0), pwDay(25, 23)))
	raw, err := json.Marshal(newPassageModelResult(p, false))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), `"wind":"Wind from N 10 kn: astern 5.5 h, ahead 0.5 h (first 0.5 h)"`) {
		t.Fatalf("model result lacks the wind summary: %s", raw)
	}
	card, _ := json.Marshal(p)
	var decoded struct {
		Rows []struct {
			Cells []map[string]json.RawMessage `json:"cells"`
		} `json:"rows"`
	}
	if err := json.Unmarshal(card, &decoded); err != nil || len(decoded.Rows) == 0 || len(decoded.Rows[0].Cells) == 0 {
		t.Fatalf("card payload: %v %s", err, card)
	}
	if _, has := decoded.Rows[0].Cells[0]["wind"]; has {
		t.Fatalf("the card payload must not carry the summary: %s", card)
	}
}

func tallyOf(segs int, sectors ...string) *passageWindTally {
	depart := pwDay(20, 6)
	w := newPassageWindTally(float64(len(sectors)), segs)
	for i, sec := range sectors {
		w.add(depart.Add(time.Duration(i)*time.Hour+30*time.Minute), depart, depart.Add(time.Duration(len(sectors))*time.Hour), 0, 10, sec, "A → B")
	}
	return w
}

func TestWindSummaryStretchOnlyWhenContiguous(t *testing.T) {
	got := tallyOf(2, "ahead", "astern", "astern", "ahead", "astern", "astern").summary("Leg")
	if want := "Wind from N 10 kn: astern 4.0 h, ahead 2.0 h (in 2 stretches)"; got != want {
		t.Fatalf("summary = %q, want %q", got, want)
	}
	got = tallyOf(1, "astern", "astern", "astern", "astern", "ahead", "ahead").summary("Leg")
	if !strings.Contains(got, "ahead 2.0 h (last 2.0 h)") {
		t.Fatalf("summary = %q", got)
	}
}

func TestPassageModelTrimOrder(t *testing.T) {
	cell := func(state string) passageModelCell {
		return passageModelCell{State: state, Why: "Gusts", Times: "6→12", Wind: "Wind N"}
	}
	r := passageModelRow{Cells: []passageModelCell{cell("go"), cell("no_go")}}
	r.trim(0)
	if r.Cells[0].Why != "" || r.Cells[1].Why == "" || r.Cells[0].Wind == "" {
		t.Fatalf("level 0 drops only go cells' reasons: %+v", r.Cells)
	}
	r.trim(1)
	if r.Cells[0].Wind != "" || r.Cells[1].Wind == "" {
		t.Fatalf("level 1 drops go cells' summaries only: %+v", r.Cells)
	}
	r.trim(2)
	if r.Cells[1].Wind != "" || r.Cells[1].Why == "" {
		t.Fatalf("level 2 drops the other summaries and keeps the deciding reason: %+v", r.Cells)
	}
	r.trim(3)
	if r.Cells[1].Why != "" || r.Cells[1].Times == "" {
		t.Fatalf("level 3 drops every reason: %+v", r.Cells)
	}
}

func TestCompassRange(t *testing.T) {
	for _, tc := range []struct {
		dirs []float64
		want string
	}{
		{[]float64{10, 10, 10}, "N"},
		{[]float64{348, 31}, "NNW–NNE"},
		{[]float64{0, 120, 240}, "variable"},
		{[]float64{0, 170, 250}, "variable"},
		{[]float64{0, 90}, "N–E"},
		{[]float64{0, 180}, "variable"},
		{nil, "calm"},
	} {
		if got := compassRange(tc.dirs); got != tc.want {
			t.Errorf("%v = %q, want %q", tc.dirs, got, tc.want)
		}
	}
}

func TestWindSummaryStatesMissingHours(t *testing.T) {
	depart := pwDay(20, 6)
	arrive := depart.Add(6 * time.Hour)
	w := newPassageWindTally(6, 1)
	for h := 0; h < 6; h++ {
		at := depart.Add(time.Duration(h)*time.Hour + 30*time.Minute)
		if h >= 2 && h <= 3 {
			w.gap(at, depart, arrive)
			continue
		}
		sec := "astern"
		if h == 0 {
			sec = "ahead"
		}
		w.add(at, depart, arrive, 0, 10, sec, "A → B")
	}
	got := w.summary("Leg")
	if !strings.Contains(got, "no forecast 2.0 h") || !strings.Contains(got, "astern 3.0 h") || !strings.Contains(got, "ahead 1.0 h (first 1.0 h)") {
		t.Fatalf("summary = %q", got)
	}
}

func TestWindSummaryTinyHoursReadAsAnEnd(t *testing.T) {
	depart := pwDay(20, 6).Add(29 * time.Minute) // the 06:00 stamp keeps 1 minute
	arrive := depart.Add(3 * time.Hour)
	w := newPassageWindTally(3, 1)
	w.add(pwDay(20, 6), depart, arrive, 0, 16, "ahead", "A → B")
	for h := 7; h <= 9; h++ {
		w.add(pwDay(20, h), depart, arrive, 180, 10, "astern", "A → B")
	}
	if got := w.summary("Leg"); !strings.Contains(got, "ahead at departure") || strings.Contains(got, "ahead 0.0") {
		t.Fatalf("summary = %q", got)
	}
}

func TestWindSummaryEdgeAtBothEnds(t *testing.T) {
	depart := pwDay(20, 6).Add(40 * time.Minute)
	arrive := depart.Add(90 * time.Minute)
	w := newPassageWindTally(1.5, 1)
	w.add(pwDay(20, 6), depart, arrive, 0, 16, "ahead", "A → B")
	w.add(pwDay(20, 7), depart, arrive, 180, 10, "astern", "A → B")
	w.add(pwDay(20, 9), depart, arrive, 0, 16, "ahead", "A → B")
	if got := w.summary("Leg"); !strings.Contains(got, "ahead at departure and arrival") {
		t.Fatalf("summary = %q", got)
	}
}

func TestWindSummaryCalmHoursKeepOutOfTheDirection(t *testing.T) {
	depart := pwDay(20, 6)
	arrive := depart.Add(2 * time.Hour)
	w := newPassageWindTally(2, 1)
	w.add(depart.Add(30*time.Minute), depart, arrive, 200, 0, "astern", "A → B")
	w.add(depart.Add(90*time.Minute), depart, arrive, 20, 8, "ahead", "A → B")
	if got := w.summary("Leg"); got != "Wind from NNE 0–8 kn: ahead 1.0 h, calm 1.0 h" {
		t.Fatalf("summary = %q", got)
	}
}

func TestWindSummaryStretchRangeUsesWindowEdges(t *testing.T) {
	got := tallyOf(1, "astern", "ahead", "ahead", "astern", "astern", "astern").summary("Leg")
	if !strings.Contains(got, "ahead 2.0 h (1.0–3.0 h in)") {
		t.Fatalf("summary = %q", got)
	}
}

func TestWindSummaryMissingHourBreaksARun(t *testing.T) {
	depart := pwDay(20, 6)
	w := newPassageWindTally(6, 1)
	for i, sec := range []string{"astern", "ahead", "gap", "ahead", "astern", "astern"} {
		if sec == "gap" {
			w.gap(depart.Add(time.Duration(i)*time.Hour+30*time.Minute), depart, depart.Add(6*time.Hour))
			continue
		}
		w.add(depart.Add(time.Duration(i)*time.Hour+30*time.Minute), depart, depart.Add(6*time.Hour), 0, 10, sec, "A → B")
	}
	if got := w.summary("Leg"); !strings.Contains(got, "ahead 2.0 h (in 2 stretches)") {
		t.Fatalf("summary = %q", got)
	}
}

// The summary mirrors the stamps the cell judges. A stamp with none of its hour
// in the leg (06:00 against a 06:40 departure, or against 06:30 where it only
// touches) stands for the conditions at departure.
func TestWindSummaryNamesTheSectorOfAnEdgeStamp(t *testing.T) {
	for _, depart := range []time.Time{pwDay(20, 6).Add(40 * time.Minute), pwDay(20, 6).Add(30 * time.Minute)} {
		arrive := depart.Add(3 * time.Hour)
		w := newPassageWindTally(3, 1)
		w.add(pwDay(20, 6), depart, arrive, 0, 16, "ahead", "A → B")
		for h := 7; h <= 9; h++ {
			w.add(pwDay(20, h), depart, arrive, 180, 10, "astern", "A → B")
		}
		got := w.summary("Leg")
		if !strings.Contains(got, "ahead at departure") || !strings.Contains(got, "10–16 kn") || !strings.Contains(got, "astern ") {
			t.Fatalf("depart %v: summary = %q", depart, got)
		}
	}
	// A stamp after arrival says so, and a sector with hours gets no edge note.
	depart, arrive := pwDay(20, 6), pwDay(20, 6).Add(90*time.Minute)
	w := newPassageWindTally(1.5, 1)
	w.add(pwDay(20, 6), depart, arrive, 0, 10, "astern", "A → B")
	w.add(pwDay(20, 7), depart, arrive, 0, 10, "astern", "A → B")
	w.add(pwDay(20, 8), depart, arrive, 90, 12, "abeam", "A → B")
	if got := w.summary("Leg"); !strings.Contains(got, "abeam at arrival") || strings.Contains(got, "astern at") {
		t.Fatalf("summary = %q", got)
	}
}

// A segment is named only when the leg has several and the run fills it.
func TestWindSummaryNamesASegmentOnlyWhenTheRunFillsIt(t *testing.T) {
	depart := pwDay(20, 6)
	arrive := depart.Add(4 * time.Hour)
	run := func(segs int, labels, sectors []string) string {
		w := newPassageWindTally(4, segs)
		for i, sec := range sectors {
			w.add(depart.Add(time.Duration(i)*time.Hour+30*time.Minute), depart, arrive, 0, 10, sec, labels[i])
		}
		return w.summary("Leg")
	}
	if got := run(2, []string{"A → B", "A → B", "B → C", "B → C"}, []string{"ahead", "astern", "astern", "astern"}); !strings.Contains(got, "(first 1.0 h)") {
		t.Errorf("a run filling half of A → B is placed by position: %q", got)
	}
	if got := run(2, []string{"A → B", "B → C", "B → C", "B → C"}, []string{"ahead", "astern", "astern", "astern"}); !strings.Contains(got, "(A → B)") {
		t.Errorf("a run filling A → B names it: %q", got)
	}
	if got := run(1, []string{"A → B", "A → B", "A → B", "A → B"}, []string{"ahead", "astern", "astern", "astern"}); !strings.Contains(got, "(first 1.0 h)") {
		t.Errorf("a one-segment leg is placed by position: %q", got)
	}
}

func TestWindSummaryMissingEndStampsReadAsEnds(t *testing.T) {
	depart := pwDay(20, 6).Add(40 * time.Minute)
	arrive := depart.Add(3 * time.Hour)
	w := newPassageWindTally(3, 1)
	w.gap(pwDay(20, 6), depart, arrive) // its hour ends before departure
	for h := 7; h <= 9; h++ {
		w.add(pwDay(20, h), depart, arrive, 0, 10, "ahead", "A → B")
	}
	got := w.summary("Leg")
	if !strings.Contains(got, "no forecast at departure") || strings.Contains(got, "0.0 h") {
		t.Fatalf("summary = %q", got)
	}
	w.gap(pwDay(20, 11), depart, arrive)
	if got := w.summary("Leg"); !strings.Contains(got, "no forecast at departure and arrival") {
		t.Fatalf("summary = %q", got)
	}
}

func TestWindSummaryCalmHourDoesNotBreakARun(t *testing.T) {
	depart := pwDay(20, 6)
	arrive := depart.Add(5 * time.Hour)
	w := newPassageWindTally(5, 1)
	for i, windKn := range []float64{10, 10, 0, 10, 10} {
		w.add(depart.Add(time.Duration(i)*time.Hour+30*time.Minute), depart, arrive, 0, windKn, "ahead", "A → B")
	}
	got := w.summary("Leg")
	if strings.Contains(got, "stretches") || !strings.Contains(got, "ahead 4.0 h") || !strings.Contains(got, "calm 1.0 h") {
		t.Fatalf("summary = %q", got)
	}
}
