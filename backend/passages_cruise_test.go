package main

import (
	"errors"
	"testing"
	"time"
)

func waterAt(h float64) float64 {
	switch {
	case h < 10:
		return 800 - 20*h
	case h < 12:
		return 600 + 150*(h-10)
	default:
		return 900 - 10*(h-12)
	}
}

// 10-minute points for 42 hours from passageEpoch (10:00 local at UTC+10).
func cruiseSpan() []recordedTrackPoint {
	var pts []recordedTrackPoint
	for i := 0; i <= 42*6; i++ {
		h := float64(i) / 6
		p := recordedTrackPoint{At: passageEpoch.Add(time.Duration(i) * 10 * time.Minute), Lat: -20, Lon: 149}
		w := waterAt(h)
		if i%2 == 0 {
			w += 2
		} else {
			w -= 2
		}
		p.WaterL = pfloat(w)
		p.GenState = pstr("stopped")
		if h >= 5 && h < 7 {
			p.GenState = pstr("running")
		}
		p.SoCPct = pfloat(100 - h)
		pts = append(pts, p)
	}
	return pts
}

func between(pts []recordedTrackPoint, from, to time.Time) []recordedTrackPoint {
	var out []recordedTrackPoint
	for _, p := range pts {
		if !p.At.Before(from) && !p.At.After(to) {
			out = append(out, p)
		}
	}
	return out
}

func cruiseFixture() ([]passageRecord, [][]recordedTrackPoint, []recordedTrackPoint) {
	span := cruiseSpan()
	p1 := passageRecord{Start: passageEpoch, End: passageEpoch.Add(2 * time.Hour)}
	p2 := passageRecord{Start: passageEpoch.Add(40 * time.Hour), End: passageEpoch.Add(42 * time.Hour)}
	return []passageRecord{p1, p2}, [][]recordedTrackPoint{between(span, p1.Start, p1.End), between(span, p2.Start, p2.End)}, span
}

func TestCruiseSpanNightsAndGeneratorHours(t *testing.T) {
	ps, per, span := cruiseFixture()
	st := computeCruiseStats(ps, per, span)
	if st.PassageCount != 2 || !st.Departure.Equal(passageEpoch) || !st.Arrival.Equal(passageEpoch.Add(42*time.Hour)) {
		t.Errorf("%+v", st)
	}
	if st.Nights != 2 {
		t.Errorf("nights %d, want 2 local midnights", st.Nights)
	}
	if st.GeneratorHours == nil || !approx(*st.GeneratorHours, 2, 0.01) {
		t.Errorf("generator hours %v, want 2 across the anchored span", st.GeneratorHours)
	}
	if st.LowestSoCPct == nil || !approx(*st.LowestSoCPct, 58, 0.01) {
		t.Errorf("lowest soc %v", st.LowestSoCPct)
	}
	if !approx(st.SpanSeconds, 42*3600, 1) {
		t.Errorf("span %v", st.SpanSeconds)
	}
}

func TestCruiseNightSoCMinAndDawn(t *testing.T) {
	ps, per, span := cruiseFixture()
	st := computeCruiseStats(ps, per, span)
	if len(st.NightSoC) == 0 {
		t.Fatal("no nights")
	}
	n := st.NightSoC[0]
	if n.MinSoCPct == nil || n.DawnSoCPct == nil {
		t.Fatalf("%+v", n)
	}
	// SoC falls one point per hour, so the minimum of the night is the dawn reading
	if !approx(*n.MinSoCPct, *n.DawnSoCPct, 0.2) {
		t.Errorf("min %v dawn %v", *n.MinSoCPct, *n.DawnSoCPct)
	}
	// sunrise near 05:50 local is about 19:50 UTC on the 14th, 19.8 h in
	if !approx(float64(n.Sunrise.Sub(passageEpoch))/float64(time.Hour), 19.8, 0.5) {
		t.Errorf("sunrise %v", n.Sunrise)
	}
}

func TestCruiseDawnAbsentWhenNoFreshSoC(t *testing.T) {
	ps, per, span := cruiseFixture()
	for i := range span {
		if h := span[i].At.Sub(passageEpoch).Hours(); h > 16 && h < 22 {
			span[i].SoCPct = nil
		}
	}
	st := computeCruiseStats(ps, per, span)
	if st.NightSoC[0].DawnSoCPct != nil {
		t.Errorf("dawn %v: an old reading must not stand in", *st.NightSoC[0].DawnSoCPct)
	}
}

func TestCruiseWaterUsedAndTakenOnWithDeadband(t *testing.T) {
	ps, per, span := cruiseFixture()
	st := computeCruiseStats(ps, per, span)
	if len(st.WaterRises) != 1 {
		t.Fatalf("rises %d: %+v", len(st.WaterRises), st.WaterRises)
	}
	r := st.WaterRises[0]
	if !approx(r.Litres, 300, 10) {
		t.Errorf("rise %.0f L", r.Litres)
	}
	if h := r.Start.Sub(passageEpoch).Hours(); !approx(h, 10, 0.5) {
		t.Errorf("rise starts at %.1f h", h)
	}
	if st.WaterTakenOnL == nil || !approx(*st.WaterTakenOnL, 300, 10) {
		t.Errorf("taken on %v", st.WaterTakenOnL)
	}
	// falls: 800->600 then 900->(900-300)=600
	if st.WaterUsedL == nil || !approx(*st.WaterUsedL, 200+300, 20) {
		t.Errorf("used %v", st.WaterUsedL)
	}
}

func TestCruiseWindUnderwayAndAtAnchorAreSeparate(t *testing.T) {
	ps, per, span := cruiseFixture()
	for i := range span {
		in := !span[i].At.After(ps[0].End) || !span[i].At.Before(ps[1].Start)
		w, g := 5.0, 8.0 // at anchor
		if in {
			w, g = 15, 25
		}
		span[i].TWSKts, span[i].GustKts = pfloat(w), pfloat(g)
	}
	for k := range per {
		for i := range per[k] {
			per[k][i].TWSKts, per[k][i].GustKts = pfloat(15), pfloat(25)
		}
	}
	st := computeCruiseStats(ps, per, span)
	if st.UnderwayTWSMeanKts == nil || *st.UnderwayTWSMeanKts != 15 || st.UnderwayMaxGustKts == nil || *st.UnderwayMaxGustKts != 25 {
		t.Errorf("underway %v %v", st.UnderwayTWSMeanKts, st.UnderwayMaxGustKts)
	}
	if st.AnchorTWSMeanKts == nil || *st.AnchorTWSMeanKts != 5 || st.AnchorMaxGustKts == nil || *st.AnchorMaxGustKts != 8 {
		t.Errorf("anchor %v %v", st.AnchorTWSMeanKts, st.AnchorMaxGustKts)
	}
}

func TestCruiseWindAbsentWhereNoReadings(t *testing.T) {
	ps, per, span := cruiseFixture()
	for k := range per {
		for i := range per[k] {
			per[k][i].TWSKts, per[k][i].GustKts = pfloat(12), pfloat(20)
		}
	}
	for i := range span {
		span[i].TWSKts, span[i].GustKts = nil, nil
	}
	st := computeCruiseStats(ps, per, span)
	if st.UnderwayTWSMeanKts == nil || st.AnchorTWSMeanKts != nil || st.AnchorMaxGustKts != nil {
		t.Errorf("%+v", st)
	}
}

func TestCruiseNoWaterOrGeneratorReadingsMeansAbsent(t *testing.T) {
	ps, per, span := cruiseFixture()
	for i := range span {
		span[i].WaterL, span[i].GenState, span[i].SoCPct, span[i].TWSKts = nil, nil, nil, nil
	}
	st := computeCruiseStats(ps, per, span)
	if st.GeneratorHours != nil || st.WaterUsedL != nil || st.WaterTakenOnL != nil || st.LowestSoCPct != nil || st.UnderwayTWSMeanKts != nil || st.AnchorTWSMeanKts != nil || st.FuelBurnedL != nil {
		t.Errorf("%+v", st)
	}
	if len(st.WaterRises) != 0 {
		t.Error("rises")
	}
}

func TestCruiseGeneratorGapIsNotCounted(t *testing.T) {
	pts := []recordedTrackPoint{
		{At: passageEpoch, GenState: pstr("running")},
		{At: passageEpoch.Add(time.Hour), GenState: pstr("stopped")}, // 1 h hole: unknown
		{At: passageEpoch.Add(time.Hour + 5*time.Minute), GenState: pstr("stopped")},
	}
	if h := generatorHours(pts); h == nil || *h != 0 {
		t.Errorf("%v", h)
	}
}

func cruiseWithRise(t *testing.T) (*passageStore, cruiseRecord) {
	t.Helper()
	s := newTestPassageStore(t)
	appendAll(t, s, cruiseSpan())
	c, _ := s.InsertCruise(cruiseRecord{Name: "C"})
	for _, w := range [][2]time.Duration{{0, 2 * time.Hour}, {40 * time.Hour, 42 * time.Hour}} {
		r := seedPassage(t, s, passageEpoch.Add(w[0]), passageEpoch.Add(w[1]))
		s.PatchPassage(r.ID, passagePatch{CruiseID: &c.ID, CruiseIDSet: true})
	}
	return s, c
}

func stubMarina(t *testing.T, f func(lat, lon float64) (bool, error)) *int {
	t.Helper()
	calls := 0
	prev := marinaNear
	marinaNear = func(lat, lon float64) (bool, error) { calls++; return f(lat, lon) }
	t.Cleanup(func() { marinaNear = prev })
	return &calls
}

func TestCruiseStatsReadsStoredValuesAndNeverLooksUp(t *testing.T) {
	s, c := cruiseWithRise(t)
	calls := stubMarina(t, func(float64, float64) (bool, error) { return true, nil })
	_, _, st, err := s.CruiseStats(c.ID)
	if err != nil || len(st.WaterRises) != 1 {
		t.Fatalf("%v %+v", err, st.WaterRises)
	}
	if *calls != 0 {
		t.Errorf("GET looked up a marina %d times", *calls)
	}
	if r := st.WaterRises[0]; r.SuggestedLabel != nil || r.LookedUpAt != nil || r.SuggestionStatus != "pending" {
		t.Errorf("nothing stored yet: %+v", r)
	}
}

func TestMarinaWorkerStoresSuggestionOnceAndOperatorLabelsSeparately(t *testing.T) {
	s, c := cruiseWithRise(t)
	calls := stubMarina(t, func(float64, float64) (bool, error) { return true, nil })
	if err := s.LookUpNewWaterRises(); err != nil {
		t.Fatal(err)
	}
	if err := s.LookUpNewWaterRises(); err != nil {
		t.Fatal(err)
	}
	if *calls != 1 {
		t.Errorf("lookups %d, want 1: a stored rise is not looked up again", *calls)
	}
	_, _, st, _ := s.CruiseStats(c.ID)
	r := st.WaterRises[0]
	if r.SuggestedLabel == nil || *r.SuggestedLabel != "Dock fill" || r.LookedUpAt == nil || r.Label != nil || r.SuggestionStatus != "found" {
		t.Fatalf("%+v", r)
	}

	if err := s.SetWaterRiseLabel(c.ID, r.Start, "Watermaker"); err != nil {
		t.Fatal(err)
	}
	_, _, st, _ = s.CruiseStats(c.ID)
	if st.WaterRises[0].Label == nil || *st.WaterRises[0].Label != "Watermaker" {
		t.Errorf("label not stored: %+v", st.WaterRises[0])
	}
	if err := s.SetWaterRiseLabel(c.ID, r.Start, "Rain"); !errors.Is(err, errPassageInvalid) {
		t.Errorf("bad label: %v", err)
	}
	s.SetWaterRiseLabel(c.ID, r.Start, "")
	_, _, st, _ = s.CruiseStats(c.ID)
	if st.WaterRises[0].Label != nil {
		t.Error("label should be cleared")
	}
}

func TestMarinaWorkerFailureIsStoredAsFailedAndRetriedAfterSixHours(t *testing.T) {
	s, c := cruiseWithRise(t)
	now := time.Now()
	calls := stubMarina(t, func(float64, float64) (bool, error) { return false, errors.New("overpass unreachable") })
	s.LookUpNewWaterRisesAt(now)
	s.LookUpNewWaterRisesAt(now.Add(5 * time.Hour))
	if *calls != 1 {
		t.Errorf("a failed lookup was retried inside six hours: %d calls", *calls)
	}
	_, _, st, _ := s.CruiseStats(c.ID)
	r := st.WaterRises[0]
	if r.SuggestionStatus != "failed" || r.SuggestedLabel != nil || r.LookedUpAt == nil || r.SuggestionError == "" {
		t.Fatalf("a failure must read as failed, not as none: %+v", r)
	}

	// six hours on the job tries again, and a good answer replaces the failure
	*calls = 0
	marinaNear = func(float64, float64) (bool, error) { *calls++; return true, nil }
	s.LookUpNewWaterRisesAt(now.Add(7 * time.Hour))
	if *calls != 1 {
		t.Errorf("retry calls %d", *calls)
	}
	_, _, st, _ = s.CruiseStats(c.ID)
	if r := st.WaterRises[0]; r.SuggestionStatus != "found" || r.SuggestedLabel == nil || r.SuggestionError != "" {
		t.Errorf("%+v", r)
	}
}

func TestMarinaWorkerNoMarinaMeansNoSuggestion(t *testing.T) {
	s, c := cruiseWithRise(t)
	stubMarina(t, func(float64, float64) (bool, error) { return false, nil })
	s.LookUpNewWaterRises()
	_, _, st, _ := s.CruiseStats(c.ID)
	r := st.WaterRises[0]
	if r.SuggestedLabel != nil || r.LookedUpAt == nil || r.SuggestionStatus != "none" {
		t.Errorf("no marina nearby is a final none, not a Watermaker guess: %+v", r)
	}
	calls := stubMarina(t, func(float64, float64) (bool, error) { return true, nil })
	s.LookUpNewWaterRisesAt(time.Now().Add(48 * time.Hour))
	if *calls != 0 {
		t.Error("none is final and is not looked up again")
	}
}

func TestDeleteCruiseRemovesLabelsKeepsTrackPoints(t *testing.T) {
	s := newTestPassageStore(t)
	c, _ := s.InsertCruise(cruiseRecord{Name: "C"})
	s.SetWaterRiseLabel(c.ID, passageEpoch, "Dock fill")
	if err := s.DeleteCruise(c.ID); err != nil {
		t.Fatal(err)
	}
	var n int
	s.db.QueryRow(`SELECT COUNT(*) FROM water_rise_labels`).Scan(&n)
	if n != 0 {
		t.Errorf("labels %d", n)
	}
}

// Review finding 9: a label belongs to the rise it was given to, not to the
// exact millisecond the zig-zag filter happened to start it on.
func TestWaterLabelAndSuggestionSurviveARiseThatMoved(t *testing.T) {
	s, c := cruiseWithRise(t)
	stubMarina(t, func(float64, float64) (bool, error) { return true, nil })
	s.LookUpNewWaterRises()
	_, _, st, _ := s.CruiseStats(c.ID)
	r := st.WaterRises[0]
	if err := s.SetWaterRiseLabel(c.ID, r.Start, "Watermaker"); err != nil {
		t.Fatal(err)
	}

	// the filter now starts this rise 20 minutes later and ends it 10 earlier
	moved := r.Start.Add(20 * time.Minute).UnixMilli()
	movedEnd := r.End.Add(-10 * time.Minute).UnixMilli()
	for _, q := range []string{
		`UPDATE water_rise_labels SET at_ms = ?, end_ms = ?`,
		`UPDATE water_rise_suggestions SET at_ms = ?, end_ms = ?`,
	} {
		if _, err := s.db.Exec(q, moved, movedEnd); err != nil {
			t.Fatal(err)
		}
	}
	_, _, st, _ = s.CruiseStats(c.ID)
	got := st.WaterRises[0]
	if got.Label == nil || *got.Label != "Watermaker" {
		t.Errorf("label orphaned: %+v", got)
	}
	if got.SuggestionStatus != "found" {
		t.Errorf("suggestion orphaned: %+v", got)
	}
}

func TestWaterLabelAnywhereInsideTheRiseFindsIt(t *testing.T) {
	s, c := cruiseWithRise(t)
	_, _, st, _ := s.CruiseStats(c.ID)
	r := st.WaterRises[0]
	mid := r.Start.Add(r.End.Sub(r.Start) / 2)
	if err := s.SetWaterRiseLabel(c.ID, mid, "Dock fill"); err != nil {
		t.Fatal(err)
	}
	_, _, st, _ = s.CruiseStats(c.ID)
	if l := st.WaterRises[0].Label; l == nil || *l != "Dock fill" {
		t.Errorf("%+v", st.WaterRises[0])
	}
	// clearing by any time inside it works too
	s.SetWaterRiseLabel(c.ID, r.Start, "")
	_, _, st, _ = s.CruiseStats(c.ID)
	if st.WaterRises[0].Label != nil {
		t.Error("not cleared")
	}
	if err := s.SetWaterRiseLabel(c.ID, passageEpoch.Add(30*time.Hour), "Dock fill"); !errors.Is(err, errPassageInvalid) {
		t.Errorf("a time that is in no rise: %v", err)
	}
}

func TestRiseMatchingPicksLargestOverlap(t *testing.T) {
	t0 := passageEpoch
	rises := []waterRise{{Start: t0, End: t0.Add(2 * time.Hour)}, {Start: t0.Add(3 * time.Hour), End: t0.Add(5 * time.Hour)}}
	rows := []riseRow{{At: t0.Add(90 * time.Minute), End: t0.Add(4 * time.Hour)}} // overlaps both; 30 min vs 60 min
	m := matchRiseRows(rises, rows)
	if _, ok := m[1]; !ok || len(m) != 1 {
		t.Errorf("%v: the row belongs to the rise it overlaps most", m)
	}
}

func TestWaterLabelSurvivesFilingAnotherPassage(t *testing.T) {
	s, c := cruiseWithRise(t)
	_, _, st, _ := s.CruiseStats(c.ID)
	if err := s.SetWaterRiseLabel(c.ID, st.WaterRises[0].Start, "Watermaker"); err != nil {
		t.Fatal(err)
	}
	// a later leg with the tanks topped right up widens the span and the deadband
	var more []recordedTrackPoint
	for i := 1; i <= 18*6; i++ {
		h := 42 + float64(i)/6
		more = append(more, recordedTrackPoint{At: passageEpoch.Add(time.Duration(42*60+i*10) * time.Minute), Lat: -20, Lon: 149, WaterL: pfloat(300 + 70*(h-42))})
	}
	appendAll(t, s, more)
	p := seedPassage(t, s, passageEpoch.Add(58*time.Hour), passageEpoch.Add(60*time.Hour))
	s.PatchPassage(p.ID, passagePatch{CruiseID: &c.ID, CruiseIDSet: true})

	_, _, st, _ = s.CruiseStats(c.ID)
	var labelled int
	for _, r := range st.WaterRises {
		if r.Label != nil && *r.Label == "Watermaker" {
			labelled++
			if h := r.Start.Sub(passageEpoch).Hours(); h > 12 {
				t.Errorf("the label moved to a different rise at %.1f h", h)
			}
		}
	}
	if labelled != 1 {
		t.Errorf("labelled rises %d, want the original one: %+v", labelled, st.WaterRises)
	}
}
