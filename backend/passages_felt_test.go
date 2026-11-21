package main

import (
	"context"
	"encoding/json"
	"math"
	"strings"
	"testing"
	"time"
)

// feltRun is the one-hour run at 6 kn from instrumentedRun: a rest point, then
// 60 one-minute legs. Leg k ends at pts[k]; every leg is under way.
func feltRun() []recordedTrackPoint {
	pts := instrumentedRun()
	return pts
}

// setSway gives the legs from..to (point indexes, inclusive) a roll and pitch RMS.
func setSway(pts []recordedTrackPoint, from, to int, roll, pitch float64) {
	for i := from; i <= to; i++ {
		pts[i].RollRMSDeg, pts[i].PitchRMSDeg = pfloat(roll), pfloat(pitch)
	}
}

func TestFeltIsTheThirdQuartileOfFifteenMinuteSpells(t *testing.T) {
	pts := feltRun()
	// Four spells of 15 under-way minutes: sway 1, 2, 3 and 4 degrees.
	setSway(pts, 1, 15, 1, 0)
	setSway(pts, 16, 30, 2, 0)
	setSway(pts, 31, 45, 3, 0)
	setSway(pts, 46, 60, 4, 0)
	s := computePassageStats(pts)

	if len(s.FeltSpells) != 4 {
		t.Fatalf("spells %d, want 4: %+v", len(s.FeltSpells), s.FeltSpells)
	}
	for i, want := range []float64{1, 2, 3, 4} {
		if !approx(s.FeltSpells[i].SwayDeg, want, 1e-9) {
			t.Errorf("spell %d sway %v, want %v", i, s.FeltSpells[i].SwayDeg, want)
		}
	}
	if s.FeltSpells[0].Start != pts[0].At.Unix() || s.FeltSpells[0].End != pts[15].At.Unix() {
		t.Errorf("first spell runs %d..%d, want %d..%d", s.FeltSpells[0].Start, s.FeltSpells[0].End, pts[0].At.Unix(), pts[15].At.Unix())
	}
	// 75th percentile by linear interpolation: 3 + 0.25 * (4 - 3).
	if s.FeltDeg == nil || !approx(*s.FeltDeg, 3.25, 1e-9) {
		t.Errorf("felt %v, want 3.25", s.FeltDeg)
	}
	if s.FeltReason != "" {
		t.Errorf("a figure carries no reason: %q", s.FeltReason)
	}
}

func TestSpellSwayIsRootOfMeanRollSquaredPlusMeanPitchSquared(t *testing.T) {
	pts := feltRun()
	setSway(pts, 1, 15, 3, 4)
	// Alternating roll 2 and 4: mean square 10, not (mean 3)^2 = 9.
	for i := 16; i <= 30; i++ {
		r := 2.0
		if i%2 == 0 {
			r = 4
		}
		pts[i].RollRMSDeg, pts[i].PitchRMSDeg = pfloat(r), pfloat(0)
	}
	s := computePassageStats(pts)
	if len(s.FeltSpells) < 2 {
		t.Fatalf("spells: %+v", s.FeltSpells)
	}
	if !approx(s.FeltSpells[0].SwayDeg, 5, 1e-9) {
		t.Errorf("3 and 4 should give 5, got %v", s.FeltSpells[0].SwayDeg)
	}
	if want := math.Sqrt((4*7 + 16*8) / 15.0); !approx(s.FeltSpells[1].SwayDeg, want, 1e-9) {
		t.Errorf("sway %v, want %v", s.FeltSpells[1].SwayDeg, want)
	}
}

func TestFeltIgnoresTheFirstPointAndTooShortASpell(t *testing.T) {
	b := newTrackBuilder()
	b.run(time.Minute, time.Minute, 0)
	b.run(33*time.Minute, time.Minute, 6) // two spells and a 3-minute tail
	pts := b.pts
	setSway(pts, 1, 30, 2, 0)
	setSway(pts, 31, 33, 9, 0) // the tail is under five minutes
	pts[0].RollRMSDeg, pts[0].PitchRMSDeg = pfloat(50), pfloat(50)
	s := computePassageStats(pts)
	if len(s.FeltSpells) != 2 {
		t.Fatalf("spells %d, want 2: %+v", len(s.FeltSpells), s.FeltSpells)
	}
	if s.FeltDeg == nil || !approx(*s.FeltDeg, 2, 1e-9) {
		t.Errorf("felt %v, want 2", s.FeltDeg)
	}
}

func TestTimeAtRestIsNotSpellTime(t *testing.T) {
	b := newTrackBuilder()
	b.run(time.Minute, time.Minute, 0)
	b.run(20*time.Minute, time.Minute, 6)
	b.run(40*time.Minute, time.Minute, 0) // an hour of nothing is not a spell
	b.run(10*time.Minute, time.Minute, 6)
	pts := b.pts
	for i := range pts {
		pts[i].RollRMSDeg, pts[i].PitchRMSDeg = pfloat(1), pfloat(0)
	}
	s := computePassageStats(pts)
	var total int64
	for _, sp := range s.FeltSpells {
		total += sp.End - sp.Start
	}
	if total > 31*60 {
		t.Errorf("spells cover %d s, more than the 30 under-way minutes: %+v", total, s.FeltSpells)
	}
}

func TestFeltSaysWhyWhenThereIsNoFigure(t *testing.T) {
	none := computePassageStats(feltRun())
	if none.FeltDeg != nil || none.FeltReason == "" || !strings.Contains(none.FeltReason, "No motion") {
		t.Errorf("no motion: %v %q", none.FeltDeg, none.FeltReason)
	}
	if none.FeltSpells == nil {
		t.Error("spells must be an empty list, not null")
	}
	pts := feltRun()
	for i := range pts {
		pts[i].Source = trackSourceImported
	}
	imp := computePassageStats(pts)
	if imp.FeltDeg != nil || !strings.Contains(imp.FeltReason, "imported") {
		t.Errorf("imported: %v %q", imp.FeltDeg, imp.FeltReason)
	}
	// Motion for only four minutes under way is too little to judge.
	short := feltRun()
	setSway(short, 1, 4, 1, 1)
	got := computePassageStats(short)
	if got.FeltDeg != nil || !strings.Contains(got.FeltReason, "five minutes") {
		t.Errorf("short: %v %q", got.FeltDeg, got.FeltReason)
	}
}

func TestSnapAndPoundingMaximaWithTheirTimes(t *testing.T) {
	pts := feltRun()
	pts[20].RollAccelPeakDps2 = pfloat(50)
	pts[30].RollAccelPeakDps2 = pfloat(80)
	pts[0].RollAccelPeakDps2 = pfloat(500) // before the passage
	pts[40].VertAccelPeakMs2 = pfloat(2 * gravityMPerS2)
	pts[10].VertAccelPeakMs2 = pfloat(gravityMPerS2)
	s := computePassageStats(pts)
	if s.MaxRollAccelDps2 == nil || *s.MaxRollAccelDps2 != 80 || s.MaxRollAccelAt == nil || *s.MaxRollAccelAt != pts[30].At.Unix() {
		t.Errorf("roll acceleration %v at %v", s.MaxRollAccelDps2, s.MaxRollAccelAt)
	}
	if s.MaxPoundingG == nil || !approx(*s.MaxPoundingG, 2, 1e-9) || s.MaxPoundingAt == nil || *s.MaxPoundingAt != pts[40].At.Unix() {
		t.Errorf("pounding %v at %v", s.MaxPoundingG, s.MaxPoundingAt)
	}
	if s.SnapReason != "" || s.PoundingReason != "" {
		t.Errorf("figures carry no reason: %q %q", s.SnapReason, s.PoundingReason)
	}
}

// feltBlockOf is the felt block a passage page would show for these points.
func feltBlockOf(pts []recordedTrackPoint) passageFelt {
	return buildPassageFelt(computePassageStats(pts), passageSidewaysG{}, comfortBands{}, nil)
}

func TestSnapPresentAndNoHeaveSaysNoHeave(t *testing.T) {
	noHeave := feltRun()
	noHeave[5].RollAccelPeakDps2 = pfloat(10)
	noHeave[5].MotionRateHz = pfloat(10)
	f := feltBlockOf(noHeave)
	if f.Snap.Value == nil || f.Pounding.ValueG != nil || !strings.Contains(f.Pounding.Reason, "No heave was recorded") {
		t.Errorf("no heave: %v %v %q", f.Snap.Value, f.Pounding.ValueG, f.Pounding.Reason)
	}
}

// ── bands ────────────────────────────────────────────────────────────────

func rated(rating string, felts ...float64) []ratedFelt {
	out := make([]ratedFelt, len(felts))
	for i, f := range felts {
		out[i] = ratedFelt{Rating: rating, FeltDeg: f}
	}
	return out
}

func joinRated(parts ...[]ratedFelt) []ratedFelt {
	var out []ratedFelt
	for _, p := range parts {
		out = append(out, p...)
	}
	return out
}

// bandOf names the band a figure falls in and the note for a figure outside the rated range.
func bandOf(b comfortBands, deg float64) (string, string) {
	band, note := feltBandFor(b, deg)
	if band == nil {
		return "", note
	}
	return *band, note
}

func TestBandsNeedThreeRatedPassagesAndTwoClasses(t *testing.T) {
	two := learnComfortBands(joinRated(rated("comfortable", 0.5), rated("lively", 2)))
	if two.Learned || two.RatedCount != 2 || !strings.Contains(two.Reason, "2 rated") {
		t.Errorf("two rated: %+v", two)
	}
	oneClass := learnComfortBands(rated("comfortable", 0.4, 0.8, 1.2, 1.5))
	if oneClass.Learned || oneClass.RatedCount != 4 || oneClass.Reason == "" {
		t.Errorf("one class: %+v", oneClass)
	}
	none := learnComfortBands(nil)
	if none.Learned || none.RatedCount != 0 || !strings.Contains(none.Reason, "0 rated") {
		t.Errorf("none: %+v", none)
	}
}

func TestBandsIgnoreUnratedAndUnknownRatings(t *testing.T) {
	b := learnComfortBands(joinRated(rated("", 1, 2, 3), rated("meh", 4), rated("comfortable", 0.5, 1), rated("lively", 2.5)))
	if !b.Learned || b.RatedCount != 3 {
		t.Errorf("%+v", b)
	}
}

func TestBandEdgeSitsMidwayBetweenTheTwoClasses(t *testing.T) {
	b := learnComfortBands(joinRated(rated("comfortable", 0.2, 0.5, 1.5), rated("lively", 2.1, 2.5)))
	if !b.Learned || b.LivelyFrom == nil || !approx(*b.LivelyFrom, 1.8, 1e-9) {
		t.Fatalf("edge %+v", b)
	}
	if b.RoughFrom != nil {
		t.Errorf("no Shouldn't-have-gone rating, no Rough edge: %v", *b.RoughFrom)
	}
	if b.RatedCount != 5 {
		t.Errorf("rated %d", b.RatedCount)
	}
	if want := "Learned from 5 rated passages: Lively from 1.8°."; b.Summary != want {
		t.Errorf("summary %q, want %q", b.Summary, want)
	}
	cases := []struct {
		deg  float64
		band string
		note string
	}{
		{0.3, "comfortable", ""},
		{1.79, "comfortable", ""},
		{1.8, "lively", ""},
		{2.5, "lively", ""},
		{3.0, "lively", "rougher than any rated passage"},
	}
	for _, c := range cases {
		band, note := bandOf(b, c.deg)
		if band != c.band || note != c.note {
			t.Errorf("%.2f: %q %q, want %q %q", c.deg, band, note, c.band, c.note)
		}
	}
}

func TestOverlappingClassesSplitAtTheMidpointOfTheirMedians(t *testing.T) {
	// Comfortable runs up to 2.4 and Lively starts at 2.0: they overlap.
	b := learnComfortBands(joinRated(rated("comfortable", 0.2, 1.0, 2.4), rated("lively", 2.0, 3.0)))
	if b.LivelyFrom == nil || !approx(*b.LivelyFrom, (1.0+2.5)/2, 1e-9) {
		t.Errorf("edge %+v", b.LivelyFrom)
	}
}

func TestRoughNeedsAShouldntHaveGoneRating(t *testing.T) {
	b := learnComfortBands(joinRated(rated("comfortable", 0.5), rated("lively", 2.0, 2.2), rated("shouldnt_have_gone", 4.0)))
	if !b.Learned || b.RoughFrom == nil || !approx(*b.RoughFrom, 3.1, 1e-9) {
		t.Fatalf("%+v", b)
	}
	if band, _ := bandOf(b, 3.1); band != "rough" {
		t.Errorf("3.1 is %q", band)
	}
	if band, note := bandOf(b, 2.9); band != "lively" || note != "" {
		t.Errorf("2.9 is %q %q", band, note)
	}
	if band, _ := bandOf(b, 1.0); band != "comfortable" {
		t.Errorf("1.0 is %q", band)
	}
}

func TestBandsWithoutAComfortableRatingNameNothingBelowLively(t *testing.T) {
	b := learnComfortBands(joinRated(rated("lively", 2.0, 2.2), rated("shouldnt_have_gone", 4.0)))
	if !b.Learned || b.LivelyFrom != nil || b.RoughFrom == nil {
		t.Fatalf("%+v", b)
	}
	if band, note := bandOf(b, 0.3); band != "lively" || note != "gentler than any rated passage" {
		t.Errorf("0.3: %q %q", band, note)
	}
	if band, note := bandOf(b, 2.1); band != "lively" || note != "" {
		t.Errorf("2.1: %q %q", band, note)
	}
}

func TestComfortableAndShouldntHaveGoneAloneLearnNothing(t *testing.T) {
	b := learnComfortBands(joinRated(rated("comfortable", 0.5, 0.8), rated("shouldnt_have_gone", 4.0)))
	if b.Learned || !strings.Contains(b.Reason, "neighbouring") {
		t.Errorf("%+v", b)
	}
	if band, _ := bandOf(b, 1); band != "" {
		t.Errorf("unlearned bands name no band, got %q", band)
	}
}

// ── API ──────────────────────────────────────────────────────────────────

// feltPassages stores three passages of one hour: two rated, one not.
func feltPassages(t *testing.T) (*passageStore, []passageRecord) {
	t.Helper()
	store := withPassageStore(t)
	var recs []passageRecord
	levels := []float64{0.5, 2.5, 1.0}
	ratings := []string{"comfortable", "lively", ""}
	for i, lvl := range levels {
		b := newTrackBuilder()
		b.t = passageEpoch.Add(time.Duration(i) * 24 * time.Hour)
		b.run(time.Minute, time.Minute, 0)
		b.run(time.Hour, time.Minute, 6)
		setSway(b.pts, 1, 60, lvl, 0)
		if i == 1 {
			b.pts[30].RollAccelPeakDps2 = pfloat(90)
			b.pts[30].MotionRateHz = pfloat(10)
			b.pts[40].VertAccelPeakMs2 = pfloat(1.5 * gravityMPerS2)
		}
		appendAll(t, store, b.pts)
		rec, err := store.InsertPassage(passageRecord{Start: b.pts[0].At, End: b.pts[60].At, Title: "p", Rating: ratings[i]})
		if err != nil {
			t.Fatal(err)
		}
		recs = append(recs, rec)
	}
	// As after the start-up pass: every passage has its figures and felt column.
	if _, failed := recomputeStalePassages(context.Background(), store, store.PassageFiguresFor); failed != 0 {
		t.Fatal("could not work out the figures")
	}
	return store, recs
}

func TestPassageDetailCarriesTheFeltFigureBandSpellsSnapAndPounding(t *testing.T) {
	_, recs := feltPassages(t)
	withHelmHeight(t, nil)
	// Two rated passages: bands are not learned yet.
	code, body := getJSON(t, getPassageHandler, "/api/passages/x", recs[1].ID)
	if code != 200 {
		t.Fatal(code, body)
	}
	felt := body["passage"].(map[string]any)["felt"].(map[string]any)
	if v, _ := felt["deg"].(float64); !approx(v, 2.5, 1e-9) {
		t.Errorf("deg %v", felt["deg"])
	}
	if felt["band"] != nil {
		t.Errorf("not learned: %v", felt["band"])
	}
	bands := felt["bands"].(map[string]any)
	if bands["learned"] != false || bands["rated_count"].(float64) != 2 || bands["reason"] == "" {
		t.Errorf("bands %v", bands)
	}
	if spells := felt["spells"].([]any); len(spells) != 4 {
		t.Errorf("spells %v", spells)
	}
	snap := felt["snap"].(map[string]any)
	if snap["kind"] != "roll_accel" || snap["value"].(float64) != 90 || snap["at"] == nil {
		t.Errorf("without a helm height snap is the roll acceleration: %v", snap)
	}
	if n, _ := snap["note"].(string); n == "" {
		t.Errorf("the missing helm height should be mentioned: %v", snap)
	}
	pound := felt["pounding"].(map[string]any)
	if v, _ := pound["value_g"].(float64); !approx(v, 1.5, 1e-9) || pound["at"] == nil {
		t.Errorf("pounding %v", pound)
	}

	// With a helm height, snap is the sideways g at the helm.
	withHelmHeight(t, pfloat(3))
	_, body = getJSON(t, getPassageHandler, "/api/passages/x", recs[1].ID)
	snap = body["passage"].(map[string]any)["felt"].(map[string]any)["snap"].(map[string]any)
	want := 3 * (90 * math.Pi / 180) / gravityMPerS2
	if snap["kind"] != "sideways_g" || !approx(snap["value"].(float64), want, 1e-9) {
		t.Errorf("snap %v, want sideways g %v", snap, want)
	}
}

func TestPassageDetailNamesTheBandOnceThreeAreRated(t *testing.T) {
	store, recs := feltPassages(t)
	withHelmHeight(t, nil)
	// Rate the third passage Comfortable: three rated, two classes.
	if _, err := store.PatchPassage(recs[2].ID, passagePatch{Rating: pstr("comfortable")}); err != nil {
		t.Fatal(err)
	}
	_, body := getJSON(t, getPassageHandler, "/api/passages/x", recs[1].ID)
	felt := body["passage"].(map[string]any)["felt"].(map[string]any)
	if felt["band"] != "lively" {
		t.Errorf("band %v: %v", felt["band"], felt)
	}
	bands := felt["bands"].(map[string]any)
	if bands["learned"] != true || bands["lively_from_deg"] == nil || bands["rough_from_deg"] != nil {
		t.Errorf("bands %v", bands)
	}
	for _, s := range felt["spells"].([]any) {
		if s.(map[string]any)["band"] != "lively" {
			t.Errorf("spell %v", s)
		}
	}
	// The operator's rating is untouched.
	if r := body["passage"].(map[string]any)["rating"]; r != "lively" {
		t.Errorf("rating %v", r)
	}
}

func TestPassageListCarriesTheBandBesideEachRowAndStaysLean(t *testing.T) {
	store, recs := feltPassages(t)
	if _, err := store.PatchPassage(recs[2].ID, passagePatch{Rating: pstr("comfortable")}); err != nil {
		t.Fatal(err)
	}
	_, body := getJSON(t, listPassagesHandler, "/api/passages", "")
	if _, has := body["comfort_bands"]; has {
		t.Errorf("the list carries no bands of its own: %v", body["comfort_bands"])
	}
	for _, p := range body["passages"].([]any) {
		stats := p.(map[string]any)["stats"].(map[string]any)
		for _, k := range []string{"felt_spells", "max_roll_accel_dps2", "max_pounding_g", "snap_reason", "pounding_reason", "motion_gap", "heave_recorded"} {
			if _, has := stats[k]; has {
				t.Errorf("list stats carry %s", k)
			}
		}
	}
	byID := map[string]map[string]any{}
	for _, p := range body["passages"].([]any) {
		m := p.(map[string]any)
		byID[m["id"].(string)] = m
	}
	if got := byID[recs[1].ID]["felt_band"]; got != "lively" {
		t.Errorf("felt band %v", got)
	}
	if got := byID[recs[0].ID]["felt_band"]; got != "comfortable" {
		t.Errorf("felt band %v", got)
	}
}

func TestPassageWithNoMotionHasNoBandAndAReason(t *testing.T) {
	store := withPassageStore(t)
	withHelmHeight(t, nil)
	b := simpleTrip(2 * time.Hour)
	appendAll(t, store, b.pts)
	rec, _ := store.InsertPassage(passageRecord{Start: b.pts[2].At, End: b.pts[360].At, Title: "t"})
	_, body := getJSON(t, getPassageHandler, "/api/passages/x", rec.ID)
	felt := body["passage"].(map[string]any)["felt"].(map[string]any)
	if felt["deg"] != nil || felt["band"] != nil || felt["reason"] == "" {
		t.Errorf("felt %v", felt)
	}
	if snap := felt["snap"].(map[string]any); snap["value"] != nil || snap["reason"] == "" {
		t.Errorf("snap %v", snap)
	}
}

func TestStoredFiguresFromAnOlderVersionAreWorkedOutAgain(t *testing.T) {
	store, recs := feltPassages(t)
	old, _ := json.Marshal(map[string]any{"version": 2, "stats": passageStats{}, "start_lat": -20.0, "start_lon": 149.0, "timezone": "Australia/Brisbane"})
	if _, err := store.db.Exec(`UPDATE passages SET stats_json = ? WHERE id = ?`, string(old), recs[1].ID); err != nil {
		t.Fatal(err)
	}
	f, err := store.PassageFiguresFor(recs[1])
	if err != nil {
		t.Fatal(err)
	}
	if f.Version != passageFiguresVersion || f.Stats.FeltDeg == nil {
		t.Errorf("not recomputed: version %d felt %v", f.Version, f.Stats.FeltDeg)
	}
}

func TestPassageDetailSendsSpellsSnapAndPoundingOnlyUnderFelt(t *testing.T) {
	_, recs := feltPassages(t)
	withHelmHeight(t, nil)
	_, body := getJSON(t, getPassageHandler, "/api/passages/x", recs[1].ID)
	stats := body["passage"].(map[string]any)["stats"].(map[string]any)
	for _, k := range []string{"felt_spells", "max_roll_accel_dps2", "max_roll_accel_at", "max_pounding_g", "max_pounding_at", "snap_reason", "pounding_reason", "motion_gap", "heave_recorded"} {
		if _, has := stats[k]; has {
			t.Errorf("stats repeat %s, which belongs under felt", k)
		}
	}
}

func TestComfortBandsReadOnlyTheRatingAndTheFeltColumn(t *testing.T) {
	store, recs := feltPassages(t)
	if _, err := store.PatchPassage(recs[2].ID, passagePatch{Rating: pstr("comfortable")}); err != nil {
		t.Fatal(err)
	}
	// Only the list works the passages out; the bands read the column as it stands.
	list, err := store.ListPassageSummaries()
	if err != nil {
		t.Fatal(err)
	}
	// One passage stored before the figures were kept, with no felt column: the
	// bands count the two that have one and neither scan nor recompute the third.
	old, _ := json.Marshal(map[string]any{"version": 2, "stats": passageStats{}})
	if _, err := store.db.Exec(`UPDATE passages SET stats_json = ?, felt_deg = NULL WHERE id = ?`, string(old), recs[0].ID); err != nil {
		t.Fatal(err)
	}
	got, err := store.ComfortBands()
	if err != nil {
		t.Fatal(err)
	}
	if got.Learned || got.RatedCount != 2 {
		t.Fatalf("bands %+v: want only the two with a felt column counted", got)
	}
	if raw, _ := storedFigures(t, store, recs[0].ID); raw.String != string(old) {
		t.Errorf("ComfortBands rewrote a row: %s", raw.String)
	}
	// The list brings the passage up to date first, so its own bands count it.
	list, err = store.ListPassageSummaries()
	if err != nil {
		t.Fatal(err)
	}
	for _, p := range list {
		if p.Stats.FeltDeg != nil && p.FeltBand == nil {
			t.Errorf("list row %s has a figure and no band though the bands are learned", p.ID)
		}
	}
}

// ── review fixes ─────────────────────────────────────────────────────────

func TestOverlappingRatingsThatCrossDrawNoEdges(t *testing.T) {
	b := learnComfortBands(joinRated(rated("comfortable", 1, 5), rated("lively", 2, 3), rated("shouldnt_have_gone", 2.5)))
	if b.Learned || b.LivelyFrom != nil || b.RoughFrom != nil || b.Summary != "" {
		t.Fatalf("crossing edges were drawn: %+v", b)
	}
	if !strings.Contains(b.Reason, "overlap") {
		t.Errorf("reason %q", b.Reason)
	}
	if b.RatedCount != 5 {
		t.Errorf("rated %d", b.RatedCount)
	}
}

func TestOldPassageWithRollPeaksSaysItHasNoFeltFigure(t *testing.T) {
	pts := feltRun()
	for i := 1; i <= 60; i++ {
		pts[i].RollPeakDeg = pfloat(5)
	}
	s := computePassageStats(pts)
	if s.FeltDeg != nil || strings.Contains(s.FeltReason, "No motion was recorded") || strings.Contains(s.FeltReason, "{figure}") ||
		!strings.Contains(s.FeltReason, "roll peaks but no felt figure") {
		t.Errorf("reason %q", s.FeltReason)
	}
}

func TestRollOnlyFeedIsNotCalledTooShort(t *testing.T) {
	pts := feltRun()
	for i := 1; i <= 60; i++ {
		pts[i].RollRMSDeg = pfloat(1)
	}
	s := computePassageStats(pts)
	if s.FeltDeg != nil || strings.Contains(s.FeltReason, "five minutes") || !strings.Contains(s.FeltReason, "Only roll was recorded") {
		t.Errorf("roll only: %q", s.FeltReason)
	}
	pts = feltRun()
	for i := 1; i <= 60; i++ {
		pts[i].PitchRMSDeg = pfloat(1)
	}
	s = computePassageStats(pts)
	if !strings.Contains(s.FeltReason, "Only pitch was recorded") {
		t.Errorf("pitch only: %q", s.FeltReason)
	}
}

func TestSpellSwayWeighsEachLegByItsDuration(t *testing.T) {
	b := newTrackBuilder()
	b.run(time.Minute, time.Minute, 0)
	b.run(5*time.Minute, time.Minute, 6)   // pts 1..5, one-minute legs
	b.run(9*time.Minute, 3*time.Minute, 6) // pts 6..8, three-minute legs
	setSway(b.pts, 1, 5, 1, 0)
	setSway(b.pts, 6, 8, 3, 0)
	s := computePassageStats(b.pts)
	if len(s.FeltSpells) != 1 {
		t.Fatalf("spells %+v", s.FeltSpells)
	}
	want := math.Sqrt((5*60*1.0 + 9*60*9.0) / (14 * 60))
	if !approx(s.FeltSpells[0].SwayDeg, want, 1e-9) {
		t.Errorf("sway %v, want %v", s.FeltSpells[0].SwayDeg, want)
	}
}

func TestOldFiguresWithNoTrackPointsAreStoredAtTheCurrentVersion(t *testing.T) {
	store := withPassageStore(t)
	rec, err := store.InsertPassage(passageRecord{Start: passageEpoch, End: passageEpoch.Add(time.Hour), Title: "empty"})
	if err != nil {
		t.Fatal(err)
	}
	old, _ := json.Marshal(map[string]any{"version": 2, "stats": passageStats{}, "start_lon": 149.0})
	if _, err := store.db.Exec(`UPDATE passages SET stats_json = ? WHERE id = ?`, string(old), rec.ID); err != nil {
		t.Fatal(err)
	}
	if recomputed, failed := recomputeStalePassages(context.Background(), store, store.PassageFiguresFor); recomputed != 1 || failed != 0 {
		t.Fatalf("recomputed %d failed %d, want 1 and 0", recomputed, failed)
	}
	if ids, err := store.staleFigureIDs(); err != nil || len(ids) != 0 {
		t.Fatalf("%v still older than the current version after one pass (%v)", ids, err)
	}
	// And the passage GET path no longer rescans it.
	var raw string
	if err := store.db.QueryRow(`SELECT stats_json FROM passages WHERE id = ?`, rec.ID).Scan(&raw); err != nil {
		t.Fatal(err)
	}
	f, err := store.PassageFiguresFor(rec)
	if err != nil {
		t.Fatal(err)
	}
	var after string
	if err := store.db.QueryRow(`SELECT stats_json FROM passages WHERE id = ?`, rec.ID).Scan(&after); err != nil {
		t.Fatal(err)
	}
	if after != raw || f.Version != passageFiguresVersion {
		t.Errorf("read rewrote the row: version %d\n%s\n%s", f.Version, raw, after)
	}
}
