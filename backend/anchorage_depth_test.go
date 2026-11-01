package main

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
)

// The stay in anchorageArgs runs 04:00Z to 22:00Z on 2 November. The stub tide
// has its lowest water inside it at 0.8 m (14:00Z) and its highest at 3.2 m
// (20:00Z); the forecast's strongest gust is 19 kts, which the rode planner
// plans at as the top of the 15 to 20 kt band, 20 kts: 7:1 by the ratio method.

// anchorageBoat is the boat the checks read: a 1.4 m draft, 13 m long, with
// generic anchor settings (not any real boat's).
func anchorageBoat() anchorageBoatSettings {
	return anchorageBoatSettings{
		BowRollerHeightM: 1.5, ChainOnboardM: 40, ScopeMethod: "ratio", GPSFromBowM: 3, LOAM: 0, MinClearanceAtLowM: 0.5,
	}
}

func depthDeps(t *testing.T) (assistantToolDeps, *proposalEnv) {
	t.Helper()
	d, _ := anchorageDeps(t, butterflyBayFeatures(t))
	env := newProposalEnv(t)
	d.documents = func() *documentStore { return env.docs }
	d.vesselState = func() (vesselStateData, error) { return vesselStateData{DraftM: 1.4, LengthOverallM: 13}, nil }
	boat := anchorageBoat()
	d.anchorSettings = func() (anchorageBoatSettings, error) { return boat, nil }
	return d, env
}

func anchorageWithDepth(depth string) string {
	return strings.TrimSuffix(anchorageArgs, "}") + `,"depth_m":` + depth + "}"
}

func TestAnchorageDepth_OperatorFigureGivesTheChecks(t *testing.T) {
	d, _ := depthDeps(t)
	raw, res := runAnchorage(t, d, anchorageWithDepth("4"))
	dp := res.Depth
	if !dp.Known || dp.DepthM == nil || *dp.DepthM != 4 || dp.Source != depthFromOperator || dp.Label != "4.0 m at datum, from you" {
		t.Fatalf("depth = %+v", dp)
	}

	// Water at the lowest low: 4 + 0.8 = 4.8; under a 1.4 m draft that leaves 3.4.
	c := dp.Clearance
	if c.Status != checkOK || *c.DepthAtLowM != 4.8 || *c.DraftM != 1.4 || *c.ClearanceM != 3.4 || *c.WantedM != 0.5 {
		t.Errorf("clearance = %+v", c)
	}
	if !strings.Contains(c.Text, "low water") || !strings.Contains(c.Text, "3.4 m under a 1.4 m draft") {
		t.Errorf("clearance text = %q", c.Text)
	}

	// Rode at the highest water: 4 + 3.2 = 7.2; plus 1.5 at the roller is 8.7;
	// the ratio method at 20 kts is 7:1, so 60.9 m against 40 m aboard.
	r := dp.Rode
	if r.Status != "short" || *r.DepthAtHighM != 7.2 || *r.RodeNeededM != 60.9 || *r.ShortByM != 20.9 || *r.ChainAboardM != 40 || *r.WindKts != 20 || r.Method != "ratio" {
		t.Errorf("rode = %+v", r)
	}
	if !strings.Contains(r.Text, "SHORT by 21 m") {
		t.Errorf("rode text = %q", r.Text)
	}

	// Swing: the rode is short, so the 40 m of chain + 3 m bow to GPS + 13 m from SignalK.
	w := dp.Swing
	if w.Status != "computed" || *w.RadiusM != 56 || *w.LoaM != 13 {
		t.Errorf("swing = %+v", w)
	}

	if !strings.Contains(res.Reading, "4.0 m at datum, from you") {
		t.Errorf("the reading must say which figure was used: %q", res.Reading)
	}
	if len(raw) > assistantMaxToolResultChars {
		t.Errorf("result %d chars exceeds the cap", len(raw))
	}

	cardMsg := d.takeCards(raw)[0]
	card := decodeAnchorageCard(t, cardMsg)
	if card.Depth.Swing.RadiusM == nil || *card.Depth.Swing.RadiusM != 56 {
		t.Errorf("card depth = %+v", card.Depth)
	}
	// The card words its own lines: the model's sentences and label are not in it.
	var onWire map[string]json.RawMessage
	if err := json.Unmarshal(cardMsg.Payload, &onWire); err != nil || strings.Contains(string(onWire["depth"]), `"text"`) || strings.Contains(string(onWire["depth"]), `"label"`) {
		t.Errorf("card depth on the wire carries text or label: %s", onWire["depth"])
	}
}

func TestAnchorageDepth_TooShallowAtLowWaterIsSaidPlainly(t *testing.T) {
	d, _ := depthDeps(t)
	_, res := runAnchorage(t, d, anchorageWithDepth("1"))
	c := res.Depth.Clearance
	// 1.0 + 0.8 - 1.4 = 0.4, under the 0.5 wanted.
	if c.Status != "too_shallow" || *c.ClearanceM != 0.4 || !strings.Contains(c.Text, "LESS than the 0.5 m wanted") {
		t.Errorf("clearance = %+v", c)
	}
}

func TestAnchorageDepth_EnoughChainIsOK(t *testing.T) {
	d, _ := depthDeps(t)
	boat := anchorageBoat()
	boat.ChainOnboardM = 80
	d.anchorSettings = func() (anchorageBoatSettings, error) { return boat, nil }
	_, res := runAnchorage(t, d, anchorageWithDepth("4"))
	if r := res.Depth.Rode; r.Status != checkOK || r.ShortByM != nil || !strings.Contains(r.Text, "enough") {
		t.Errorf("rode = %+v", r)
	}
}

func TestAnchorageDepth_CatenaryMethodSkipsTheRodeAndTheSwingAndSaysWhy(t *testing.T) {
	d, _ := depthDeps(t)
	boat := anchorageBoat()
	boat.ScopeMethod, boat.ChainOnboardM = "catenary", 100
	d.anchorSettings = func() (anchorageBoatSettings, error) { return boat, nil }
	raw, res := runAnchorage(t, d, anchorageWithDepth("4"))
	r := res.Depth.Rode
	if r.Status != checkSkipped || r.RodeNeededM != nil {
		t.Fatalf("rode = %+v", r)
	}
	for _, want := range []string{"catenary", "sea state", "seabed", "ratio"} {
		if !strings.Contains(r.SkippedReason, want) {
			t.Errorf("rode reason %q lacks %q", r.SkippedReason, want)
		}
	}
	if w := res.Depth.Swing; w.Status != checkSkipped || w.SkippedReason != r.SkippedReason || w.RadiusM != nil {
		t.Errorf("swing = %+v: it needs the rode", w)
	}
	if res.Depth.Clearance.Status != checkOK {
		t.Errorf("clearance does not need the rode: %+v", res.Depth.Clearance)
	}
	cardJSON := string(d.takeCards(raw)[0].Payload)
	for _, banned := range []string{"assumptions", "calm", "sand"} {
		if strings.Contains(raw, banned) || strings.Contains(cardJSON, banned) {
			t.Errorf("no result may carry %q", banned)
		}
	}
}

func TestAnchorageDepth_OperatorFigureBeatsTheSavedPlace(t *testing.T) {
	d, env := depthDeps(t)
	createDepthPlace(t, env, "Thomas Island South", -20.5446, 149.118933, depthOf(9), "charted", "2026-10-01")
	_, res := runAnchorage(t, d, anchorageWithDepth("4"))
	if res.Depth.Source != depthFromOperator || *res.Depth.DepthM != 4 {
		t.Errorf("depth = %+v", res.Depth)
	}
}

func TestAnchorageDepth_SavedPlaceWithinATenthOfAMileSuppliesIt(t *testing.T) {
	d, env := depthDeps(t)
	// About 110 m north of the anchorage.
	createDepthPlace(t, env, "Thomas Island South", -20.5436, 149.118933, depthOf(5), "charted", "2026-10-01")
	_, res := runAnchorage(t, d, anchorageArgs)
	dp := res.Depth
	if dp.Source != depthFromSavedPlace || *dp.DepthM != 5 || dp.SavedPlace != "Thomas Island South" || dp.SavedSource != "charted" || dp.SavedDate != "2026-10-01" {
		t.Fatalf("depth = %+v", dp)
	}
	if dp.Label != "5.0 m at datum, from saved place Thomas Island South (charted 2026-10-01)" {
		t.Errorf("label = %q", dp.Label)
	}
	if dp.Clearance.Status != checkOK {
		t.Errorf("clearance = %+v", dp.Clearance)
	}
}

func TestAnchorageDepth_SoundedFiguresSaySo(t *testing.T) {
	d, env := depthDeps(t)
	createDepthPlace(t, env, "Spot", -20.5446, 149.118933, depthOf(3.2), "sounded", "2026-10-20")
	_, res := runAnchorage(t, d, anchorageArgs)
	if !strings.Contains(res.Depth.Label, "(sounded 2026-10-20)") {
		t.Errorf("label = %q", res.Depth.Label)
	}
}

func TestAnchorageDepth_TheNearestSavedPlaceWinsAndASavedPlaceTooFarDoesNot(t *testing.T) {
	d, env := depthDeps(t)
	// 300 m away is outside 0.1 nm (185.2 m).
	createDepthPlace(t, env, "Far Spot", -20.5419, 149.118933, depthOf(5), "charted", "2026-10-01")
	_, res := runAnchorage(t, d, anchorageArgs)
	if res.Depth.Known || res.Depth.Source != depthFromNowhere {
		t.Fatalf("a saved place outside 0.1 nm must not supply a depth: %+v", res.Depth)
	}

	// Inside the circle: the only place has no depth, so the note names it.
	createDepthPlace(t, env, "Near Spot", -20.5445, 149.118933, nil, "", "")
	_, res = runAnchorage(t, d, anchorageArgs)
	if res.Depth.Known || !strings.Contains(res.Depth.Note, "Near Spot") || !strings.Contains(res.Depth.Note, "no depth") {
		t.Errorf("depth = %+v", res.Depth)
	}
}

func TestAnchorageDepth_TheNearestPlaceWithADepthSuppliesIt(t *testing.T) {
	d, env := depthDeps(t)
	// A nearer place with no depth does not shadow a farther one that has one.
	createDepthPlace(t, env, "Beach", -20.5445, 149.118933, nil, "", "")
	createDepthPlace(t, env, "Butterfly Bay", -20.5436, 149.118933, depthOf(4), "charted", "2026-10-01")
	_, res := runAnchorage(t, d, anchorageArgs)
	if !res.Depth.Known || res.Depth.Source != depthFromSavedPlace || *res.Depth.DepthM != 4 || res.Depth.SavedPlace != "Butterfly Bay" {
		t.Errorf("depth = %+v", res.Depth)
	}
}

func TestAnchorageDepth_ClearanceIsJudgedAsDisplayed(t *testing.T) {
	d, _ := depthDeps(t)
	// 1.06 + 0.8 - 1.4 = 0.46 m, which reads as the 0.5 m wanted.
	_, res := runAnchorage(t, d, anchorageWithDepth("1.06"))
	c := res.Depth.Clearance
	if c.Status != checkOK || !strings.Contains(c.Text, "at least the 0.5 m wanted") {
		t.Errorf("clearance = %+v", c)
	}
}

func TestAnchorageDepth_TheStrongestHourlyGustPlansTheRode(t *testing.T) {
	d, _ := depthDeps(t)
	wx := anchorageWeather()
	// 05:00Z is between the 3-hourly card rows (04, 07, 10, ...): 15 m/s is 29 kts.
	wx.bundle.Hourly[5].WindGustMS = 15
	d.weather = func() (weatherProvider, string, error) { return wx, "wx", nil }
	_, res := runAnchorage(t, d, anchorageWithDepth("4"))
	if r := res.Depth.Rode; r.WindKts == nil || *r.WindKts != 30 {
		t.Errorf("rode = %+v: the 05:00Z gust of 29 kts plans at 30", r)
	}
	// A gust outside the stay does not count.
	wx = anchorageWeather()
	wx.bundle.Hourly[3].WindGustMS = 15
	d.weather = func() (weatherProvider, string, error) { return wx, "wx", nil }
	_, res = runAnchorage(t, d, anchorageWithDepth("4"))
	if r := res.Depth.Rode; r.WindKts == nil || *r.WindKts != 20 || r.WindCheckedUntil != "" {
		t.Errorf("rode = %+v", r)
	}
}

func TestAnchorageDepth_ForecastEndingBeforeTheStaySaysHowFarTheWindWasChecked(t *testing.T) {
	d, _ := depthDeps(t)
	wx := anchorageWeather()
	wx.bundle.Hourly = wx.bundle.Hourly[:14] // to 13:00Z; the stay runs to 22:00Z
	d.weather = func() (weatherProvider, string, error) { return wx, "wx", nil }
	raw, res := runAnchorage(t, d, anchorageWithDepth("4"))
	r := res.Depth.Rode
	if r.Status != "short" || r.WindCheckedUntil != "2026-11-02T23:00:00+10:00" {
		t.Fatalf("rode = %+v", r)
	}
	if !strings.Contains(r.Text, "only checked up to") {
		t.Errorf("rode text = %q", r.Text)
	}
	if card := string(d.takeCards(raw)[0].Payload); !strings.Contains(card, `"wind_checked_until":"2026-11-02T23:00:00+10:00"`) {
		t.Errorf("card lacks wind_checked_until")
	}
}

func TestAnchorageDepth_ShortRodeSwingsOnTheChainAboard(t *testing.T) {
	d, _ := depthDeps(t)
	_, res := runAnchorage(t, d, anchorageWithDepth("4"))
	w := res.Depth.Swing
	// 40 m of chain + 3 m bow to GPS + 13 m boat = 56 m, not the 60.9 m wanted.
	if w.Status != "computed" || !w.AllChainOut || *w.RadiusM != 56 || *w.RodeM != 40 {
		t.Fatalf("swing = %+v", w)
	}
	if !strings.Contains(w.Text, "all 40 m of chain out") || !strings.Contains(w.Text, "short") {
		t.Errorf("swing text = %q", w.Text)
	}
	// Enough chain: the swing is for the rode needed.
	boat := anchorageBoat()
	boat.ChainOnboardM = 80
	d.anchorSettings = func() (anchorageBoatSettings, error) { return boat, nil }
	_, res = runAnchorage(t, d, anchorageWithDepth("4"))
	if w := res.Depth.Swing; w.AllChainOut || *w.RadiusM != 76.9 {
		t.Errorf("swing = %+v", w)
	}
}

func TestAnchorageDepth_NoBowRollerHeightSkipsTheRodeOnce(t *testing.T) {
	d, _ := depthDeps(t)
	boat := anchorageBoat()
	boat.BowRollerHeightM = 0
	d.anchorSettings = func() (anchorageBoatSettings, error) { return boat, nil }
	_, res := runAnchorage(t, d, anchorageWithDepth("4"))
	if r := res.Depth.Rode; r.Status != checkSkipped || r.SkippedReason != "the bow roller height is not set in Settings" {
		t.Errorf("rode = %+v", r)
	}
}

func TestAnchorageDepth_NoDepthSkipsTheChecksAndSaysSo(t *testing.T) {
	d, _ := depthDeps(t)
	raw, res := runAnchorage(t, d, anchorageArgs)
	dp := res.Depth
	if dp.Known || dp.DepthM != nil || dp.Source != depthFromNowhere {
		t.Fatalf("depth = %+v", dp)
	}
	for name, ck := range map[string]anchorageCheck{"clearance": dp.Clearance.anchorageCheck, "rode": dp.Rode.anchorageCheck, "swing": dp.Swing.anchorageCheck} {
		if ck.Status != checkSkipped || !strings.Contains(ck.SkippedReason, "depth is unknown") {
			t.Errorf("%s = %+v", name, ck)
		}
	}
	if !strings.Contains(dp.Note, "check the chart") || !strings.Contains(res.Reading, "depth is unknown") {
		t.Errorf("note %q reading %q", dp.Note, res.Reading)
	}
	// The card carries the same section, every key present.
	card := string(d.takeCards(raw)[0].Payload)
	for _, key := range []string{`"depth":{`, `"known":false`, `"depth_m":null`, `"clearance":{`, `"rode":{`, `"swing":{`, `"radius_m":null`} {
		if !strings.Contains(card, key) {
			t.Errorf("card lacks %s", key)
		}
	}
}

func TestAnchorageDepth_NoDraftSkipsOnlyTheClearance(t *testing.T) {
	d, _ := depthDeps(t)
	d.vesselState = func() (vesselStateData, error) { return vesselStateData{DraftM: -1, LengthOverallM: 13}, nil }
	_, res := runAnchorage(t, d, anchorageWithDepth("4"))
	c := res.Depth.Clearance
	if c.Status != checkSkipped || c.SkippedReason != "the boat's draft isn't reported" || c.ClearanceM != nil {
		t.Errorf("clearance = %+v", c)
	}
	if res.Depth.Rode.Status != "short" || res.Depth.Swing.Status != "computed" {
		t.Errorf("rode %+v swing %+v: the draft has nothing to do with them", res.Depth.Rode, res.Depth.Swing)
	}
}

func TestAnchorageDepth_UnreadableVesselStateIsNotAnAssumedDraft(t *testing.T) {
	d, _ := depthDeps(t)
	d.vesselState = func() (vesselStateData, error) { return vesselStateData{}, errors.New("signalk down") }
	_, res := runAnchorage(t, d, anchorageWithDepth("4"))
	if c := res.Depth.Clearance; c.Status != checkSkipped || !strings.Contains(c.SkippedReason, "could not be read") {
		t.Errorf("clearance = %+v", c)
	}
	if w := res.Depth.Swing; w.Status != checkSkipped || !strings.Contains(w.SkippedReason, "length overall") {
		t.Errorf("swing = %+v", w)
	}
}

func TestAnchorageDepth_TideFailureSkipsTheChecksThatNeedIt(t *testing.T) {
	d, _ := depthDeps(t)
	d.tides = nil
	_, res := runAnchorage(t, d, anchorageWithDepth("4"))
	if res.Tide.Error == "" {
		t.Fatal("tide should have failed")
	}
	for name, ck := range map[string]anchorageCheck{"clearance": res.Depth.Clearance.anchorageCheck, "rode": res.Depth.Rode.anchorageCheck, "swing": res.Depth.Swing.anchorageCheck} {
		if ck.Status != checkSkipped || !strings.Contains(ck.SkippedReason, "tide") {
			t.Errorf("%s = %+v", name, ck)
		}
	}
}

func TestAnchorageDepth_AnUnsetSettingSkipsTheRodeAndNamesIt(t *testing.T) {
	d, _ := depthDeps(t)
	boat := anchorageBoat()
	boat.ChainOnboardM = 0
	d.anchorSettings = func() (anchorageBoatSettings, error) { return boat, nil }
	_, res := runAnchorage(t, d, anchorageWithDepth("4"))
	if r := res.Depth.Rode; r.Status != checkSkipped || !strings.Contains(r.SkippedReason, "chain aboard") {
		t.Errorf("rode = %+v", r)
	}
	if w := res.Depth.Swing; w.Status != checkSkipped || !strings.Contains(w.SkippedReason, "chain aboard") {
		t.Errorf("swing = %+v", w)
	}
	if res.Depth.Clearance.Status != checkOK {
		t.Errorf("clearance does not need the chain: %+v", res.Depth.Clearance)
	}
}

func TestAnchorageDepth_UnreadableSettingsAreNamed(t *testing.T) {
	d, _ := depthDeps(t)
	d.anchorSettings = nil
	_, res := runAnchorage(t, d, anchorageWithDepth("4"))
	for name, ck := range map[string]anchorageCheck{"clearance": res.Depth.Clearance.anchorageCheck, "rode": res.Depth.Rode.anchorageCheck} {
		if ck.Status != checkSkipped || !strings.Contains(ck.SkippedReason, "anchor settings") {
			t.Errorf("%s = %+v", name, ck)
		}
	}
}

func TestAnchorageDepth_UnknownLengthSkipsOnlyTheSwing(t *testing.T) {
	d, _ := depthDeps(t)
	d.vesselState = func() (vesselStateData, error) { return vesselStateData{DraftM: 1.4, LengthOverallM: -1}, nil }
	_, res := runAnchorage(t, d, anchorageWithDepth("4"))
	if w := res.Depth.Swing; w.Status != checkSkipped || !strings.Contains(w.SkippedReason, "length overall") {
		t.Errorf("swing = %+v", w)
	}
	if res.Depth.Rode.Status != "short" {
		t.Errorf("rode = %+v", res.Depth.Rode)
	}
	// The setting wins over SignalK when it is set.
	boat := anchorageBoat()
	boat.LOAM = 15
	d.anchorSettings = func() (anchorageBoatSettings, error) { return boat, nil }
	_, res = runAnchorage(t, d, anchorageWithDepth("4"))
	if *res.Depth.Swing.RadiusM != 58 { // 40 m of chain + 3 + 15
		t.Errorf("swing = %+v", res.Depth.Swing)
	}
}

func TestAnchorageDepth_StayShorterThanATideUsesTheHeightsAtItsEnds(t *testing.T) {
	d, _ := depthDeps(t)
	// 06:00Z to 08:00Z is wholly on the rising tide, ending at the 08:00Z high, so
	// the only low in it is the height at arrival.
	args := `{"lat":-20.5446,"lon":149.118933,"arrive":"2026-11-02T16:00:00+10:00","leave":"2026-11-02T18:00:00+10:00","depth_m":4}`
	_, res := runAnchorage(t, d, args)
	c := res.Depth.Clearance
	if c.Status != checkOK || c.LowestWaterM == nil {
		t.Fatalf("clearance = %+v", c)
	}
	if !strings.Contains(c.Text, "arrival") {
		t.Errorf("the lowest water should be the height at arrival: %q", c.Text)
	}
	if *c.LowestWaterM >= 3.0 {
		t.Errorf("lowest water %v should be below the high", *c.LowestWaterM)
	}
}

func TestAnchorageDepth_ABadDepthArgumentIsRefused(t *testing.T) {
	d, _ := depthDeps(t)
	for _, v := range []string{"5000", "-50"} {
		_, _, err := d.execute(context.Background(), "anchorage_details", json.RawMessage(anchorageWithDepth(v)))
		if err == nil || !strings.Contains(err.Error(), "depth_m") {
			t.Errorf("depth %s: err = %v", v, err)
		}
	}
}

func TestAnchorageDepth_SavedPlacesUnavailableIsASectionError(t *testing.T) {
	d, _ := depthDeps(t)
	d.documents = nil
	_, res := runAnchorage(t, d, anchorageArgs)
	if res.Depth.Error == "" || res.Depth.Known {
		t.Fatalf("depth = %+v", res.Depth)
	}
	if ck := res.Depth.Clearance; ck.Status != checkSkipped || !strings.Contains(ck.SkippedReason, "Depth unavailable") {
		t.Errorf("clearance = %+v", ck)
	}
	// An operator's figure needs no lookup.
	_, res = runAnchorage(t, d, anchorageWithDepth("4"))
	if res.Depth.Error != "" || !res.Depth.Known {
		t.Errorf("depth = %+v", res.Depth)
	}
}

func TestAnchorageDepth_TheToolSaysWhenToPassADepth(t *testing.T) {
	desc := mateToolAnchorageDetails.function.Description
	if !strings.Contains(desc, "ONLY when the operator stated") || !strings.Contains(desc, "never from your own knowledge") {
		t.Errorf("description must restrict depth_m: %q", desc)
	}
	var schema struct {
		Properties map[string]struct {
			Description string `json:"description"`
		} `json:"properties"`
		Required []string `json:"required"`
	}
	if err := json.Unmarshal(mateToolAnchorageDetails.function.Parameters, &schema); err != nil {
		t.Fatal(err)
	}
	if _, ok := schema.Properties["depth_m"]; !ok || strings.Join(schema.Required, ",") != "lat,lon,arrive" {
		t.Errorf("schema = %+v", schema)
	}
}
