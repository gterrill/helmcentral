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

func TestCruiseNoWaterOrGeneratorReadingsMeansAbsent(t *testing.T) {
	ps, per, span := cruiseFixture()
	for i := range span {
		span[i].WaterL, span[i].GenState, span[i].SoCPct, span[i].TWSKts = nil, nil, nil, nil
	}
	st := computeCruiseStats(ps, per, span)
	if st.GeneratorHours != nil || st.WaterUsedL != nil || st.WaterTakenOnL != nil || st.LowestSoCPct != nil || st.TWSMeanKts != nil || st.FuelBurnedL != nil {
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

func TestCruiseStoreLabelsAndMarinaSuggestion(t *testing.T) {
	s := newTestPassageStore(t)
	span := cruiseSpan()
	appendAll(t, s, span)
	c, _ := s.InsertCruise(cruiseRecord{Name: "C"})
	for _, w := range [][2]time.Duration{{0, 2 * time.Hour}, {40 * time.Hour, 42 * time.Hour}} {
		r := seedPassage(t, s, passageEpoch.Add(w[0]), passageEpoch.Add(w[1]))
		s.PatchPassage(r.ID, passagePatch{CruiseID: &c.ID, CruiseIDSet: true})
	}

	prev := marinaNear
	defer func() { marinaNear = prev }()
	marinaCacheMu.Lock()
	marinaCache = map[string]bool{}
	marinaCacheMu.Unlock()

	marinaNear = func(lat, lon float64) (bool, error) { return true, nil }
	_, _, st, err := s.CruiseStats(c.ID)
	if err != nil || len(st.WaterRises) != 1 {
		t.Fatalf("%v %+v", err, st.WaterRises)
	}
	if r := st.WaterRises[0]; r.SuggestedLabel == nil || *r.SuggestedLabel != "Dock fill" || r.Label != nil {
		t.Errorf("%+v", r)
	}

	if err := s.SetWaterRiseLabel(c.ID, st.WaterRises[0].Start, "Watermaker"); err != nil {
		t.Fatal(err)
	}
	_, _, st, _ = s.CruiseStats(c.ID)
	if st.WaterRises[0].Label == nil || *st.WaterRises[0].Label != "Watermaker" {
		t.Errorf("label not stored: %+v", st.WaterRises[0])
	}
	if err := s.SetWaterRiseLabel(c.ID, st.WaterRises[0].Start, "Rain"); !errors.Is(err, errPassageInvalid) {
		t.Errorf("bad label: %v", err)
	}
	s.SetWaterRiseLabel(c.ID, st.WaterRises[0].Start, "")
	_, _, st, _ = s.CruiseStats(c.ID)
	if st.WaterRises[0].Label != nil {
		t.Error("label should be cleared")
	}
}

func TestCruiseMarinaLookupFailureLeavesNoSuggestion(t *testing.T) {
	s := newTestPassageStore(t)
	appendAll(t, s, cruiseSpan())
	c, _ := s.InsertCruise(cruiseRecord{Name: "C"})
	for _, w := range [][2]time.Duration{{0, 2 * time.Hour}, {40 * time.Hour, 42 * time.Hour}} {
		r := seedPassage(t, s, passageEpoch.Add(w[0]), passageEpoch.Add(w[1]))
		s.PatchPassage(r.ID, passagePatch{CruiseID: &c.ID, CruiseIDSet: true})
	}
	prev := marinaNear
	defer func() { marinaNear = prev }()
	marinaCacheMu.Lock()
	marinaCache = map[string]bool{}
	marinaCacheMu.Unlock()
	marinaNear = func(lat, lon float64) (bool, error) { return false, errors.New("overpass unreachable") }
	_, _, st, err := s.CruiseStats(c.ID)
	if err != nil {
		t.Fatal(err)
	}
	if st.WaterRises[0].SuggestedLabel != nil {
		t.Error("an unreachable lookup must not guess")
	}
	marinaNear = func(lat, lon float64) (bool, error) { return false, nil }
	marinaCacheMu.Lock()
	marinaCache = map[string]bool{}
	marinaCacheMu.Unlock()
	_, _, st, _ = s.CruiseStats(c.ID)
	if st.WaterRises[0].SuggestedLabel != nil {
		t.Error("no marina nearby means no suggestion, not a Watermaker guess")
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
