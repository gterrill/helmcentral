package main

import (
	"fmt"
	"math"
	"strings"
	"time"
)

// The depth section of anchorage_details: the water at the anchorage, where the
// figure came from, and what it means for the stay. A depth the boat can stand
// behind comes from one of two places only: the operator said it in this
// conversation (the depth_m argument) or it is the depth of a saved place within
// a tenth of a nautical mile. Mate's own knowledge is never a source. With no
// depth the checks are listed as skipped, with the reason, so that nothing reads
// as passed that was not looked at.
//
// The rode is checked by the ratio method only. The catenary method needs the
// sea state and the seabed for a stay that has not begun, and Helmcentral has
// neither, so an operator whose scope method is catenary gets the rode check
// skipped, with the reason, and the swing check with it.

const (
	// anchorageSavedPlaceRadiusM is how near a saved place must be for its depth
	// to stand for the anchorage: a tenth of a nautical mile.
	anchorageSavedPlaceRadiusM = 0.1 * metersPerNauticalMile

	depthFromOperator   = "operator"
	depthFromSavedPlace = "saved_place"
	depthFromNowhere    = "none"

	checkOK      = "ok"
	checkSkipped = "skipped"

	// rodeCatenaryUnavailable is why a scope method of catenary skips the rode
	// check. It is worded for the operator.
	rodeCatenaryUnavailable = "the scope method in Settings is catenary, which needs the sea state and the seabed at the anchorage, and Helmcentral does not know them for a stay that has not begun; set the method to ratio to have the rode checked"
)

// anchorageBoatSettings is the part of the Anchor settings the checks read.
type anchorageBoatSettings struct {
	BowRollerHeightM   float64
	ChainOnboardM      float64
	ScopeMethod        string
	GPSFromBowM        float64
	LOAM               float64
	MinClearanceAtLowM float64
}

type anchorageCheck struct {
	// Status is checkOK or checkSkipped, or the check's own failing word:
	// "too_shallow" for the keel, "short" for the chain, "computed" for the swing.
	Status        string `json:"status"`
	SkippedReason string `json:"skipped_reason"`
	// Text is the check in one sentence, for the model's copy only. The card
	// words its lines itself, in the operator's units, so forCard clears it.
	Text string `json:"text,omitempty"`
}

// skippedCheck is every check that did not run: the reason, and the sentence
// the model reads it as.
func skippedCheck(reason string) anchorageCheck {
	return anchorageCheck{Status: checkSkipped, SkippedReason: reason, Text: "Skipped: " + reason + "."}
}

type anchorageClearanceCheck struct {
	anchorageCheck
	LowestWaterAt string   `json:"lowest_water_at"`
	LowestWaterM  *float64 `json:"lowest_water_height_m"`
	DepthAtLowM   *float64 `json:"depth_at_low_m"`
	DraftM        *float64 `json:"draft_m"`
	ClearanceM    *float64 `json:"clearance_m"`
	WantedM       *float64 `json:"wanted_m"`
}

type anchorageRodeCheck struct {
	anchorageCheck
	HighestWaterAt string   `json:"highest_water_at"`
	HighestWaterM  *float64 `json:"highest_water_height_m"`
	DepthAtHighM   *float64 `json:"depth_at_high_m"`
	Method         string   `json:"method"`
	WindKts        *float64 `json:"wind_kts"`
	RodeNeededM    *float64 `json:"rode_needed_m"`
	ScopeRatio     *float64 `json:"scope_ratio"`
	ChainAboardM   *float64 `json:"chain_onboard_m"`
	ShortByM       *float64 `json:"short_by_m"`
	// WindCheckedUntil is when the forecast ends, RFC 3339, when it ends before
	// the stay does: the wind was checked only up to then. Empty when the
	// forecast covers the whole stay.
	WindCheckedUntil string `json:"wind_checked_until"`
}

type anchorageSwingCheck struct {
	anchorageCheck
	RadiusM    *float64 `json:"radius_m"`
	RodeM      *float64 `json:"rode_m"`
	BowOffsetM *float64 `json:"bow_offset_m"`
	LoaM       *float64 `json:"loa_m"`
	// AllChainOut is true when the rode check came out short: the swing is
	// then for all the chain aboard, not the rode needed.
	AllChainOut bool `json:"all_chain_out"`
}

// anchorageDepthSection is what the tool result and the card both carry. Every
// key is always present, except the model-only wording (see forCard).
type anchorageDepthSection struct {
	// Error is set when the saved places could not be read; the depth is then
	// unknown and the checks are skipped, naming it.
	Error  string   `json:"error"`
	Known  bool     `json:"known"`
	DepthM *float64 `json:"depth_m"`
	// Source is depthFromOperator, depthFromSavedPlace or depthFromNowhere.
	Source string `json:"source"`
	// SavedPlace, SavedSource and SavedDate describe the saved place a depth
	// came from; SavedSource is "charted" or "sounded".
	SavedPlace  string `json:"saved_place"`
	SavedSource string `json:"saved_source"`
	SavedDate   string `json:"saved_date"`
	// Label says which figure was used and where it came from, for the
	// model's copy only.
	Label     string                  `json:"label,omitempty"`
	Note      string                  `json:"note"`
	Clearance anchorageClearanceCheck `json:"clearance"`
	Rode      anchorageRodeCheck      `json:"rode"`
	Swing     anchorageSwingCheck     `json:"swing"`
}

// forCard is the section without the wording written for the model: the card
// words its own lines from the figures, in the operator's units.
func (s anchorageDepthSection) forCard() anchorageDepthSection {
	s.Label = ""
	s.Clearance.Text, s.Rode.Text, s.Swing.Text = "", "", ""
	return s
}

// anchorageStayWater is the lowest or highest water the boat sees in the stay.
type anchorageStayWater struct {
	at      time.Time
	heightM float64
	// what is "low water", "high water", "arrival" or "leaving".
	what string
}

func skippedChecks(s *anchorageDepthSection, reason string) {
	c := skippedCheck(reason)
	s.Clearance.anchorageCheck, s.Rode.anchorageCheck, s.Swing.anchorageCheck = c, c, c
}

// resolveAnchorageDepth picks the depth for an anchorage: the operator's
// figure when Mate passed one, else the depth of the nearest saved place within
// a tenth of a nautical mile that has one, else none.
func (d assistantToolDeps) resolveAnchorageDepth(lat, lon float64, given *float64) anchorageDepthSection {
	s := anchorageDepthSection{Source: depthFromNowhere}
	if given != nil {
		s.Known, s.DepthM, s.Source = true, given, depthFromOperator
		s.Label = fmt.Sprintf("%.1f m at datum, from you", *given)
		return s
	}
	store, err := d.recordStore("anchorage_details")
	if err != nil {
		s.Error = "Depth unavailable: " + firstErrorLine(err)
		return s
	}
	places, err := readSavedPlacesNear(store, lat, lon, anchorageSavedPlaceRadiusM)
	if err != nil {
		s.Error = "Depth unavailable: " + firstErrorLine(err)
		return s
	}
	// The nearest place with a depth supplies it; a nearer place without one
	// does not shadow it.
	var nearest, nearestWithDepth *savedPlace
	nearestM, nearestDepthM := math.Inf(1), math.Inf(1)
	for i := range places {
		m := haversineMeters(lat, lon, places[i].Lat, places[i].Lon)
		if m > anchorageSavedPlaceRadiusM {
			continue
		}
		if m < nearestM {
			nearest, nearestM = &places[i], m
		}
		if places[i].DepthM != nil && m < nearestDepthM {
			nearestWithDepth, nearestDepthM = &places[i], m
		}
	}
	switch {
	case nearest == nil:
		s.Note = "No saved place within 0.1 nm has a depth, and none was given."
	case nearestWithDepth == nil:
		s.Note = fmt.Sprintf("The saved place %s is %d m away but has no depth saved.", nearest.Name, int(math.Round(nearestM)))
	default:
		p := nearestWithDepth
		s.Known, s.DepthM, s.Source = true, p.DepthM, depthFromSavedPlace
		s.SavedPlace, s.SavedSource, s.SavedDate = p.Name, p.DepthSource, p.DepthDate
		s.Label = fmt.Sprintf("%.1f m at datum, from saved place %s (%s %s)", *p.DepthM, p.Name, p.DepthSource, p.DepthDate)
	}
	return s
}

// tideStayWaters finds the lowest and highest water in the stay: the high and
// low waters inside it, and the heights at arrival and at leaving, which can
// be the extremes when the stay sits between two waters. nil when the
// prediction gives no height inside the stay.
func tideStayWaters(sorted []tideExtremePoint, stay anchorageStay) (low, high *anchorageStayWater) {
	var cands []anchorageStayWater
	for _, e := range sorted {
		if !e.Time.Before(stay.arrive) && !e.Time.After(stay.leave) {
			what := "low water"
			if e.High {
				what = "high water"
			}
			cands = append(cands, anchorageStayWater{at: e.Time, heightM: e.HeightM, what: what})
		}
	}
	if n := len(sorted); n > 0 {
		covered := func(t time.Time) bool { return !t.Before(sorted[0].Time) && t.Before(sorted[n-1].Time) }
		if covered(stay.arrive) {
			h, _ := interpolateTideNow(sorted, stay.arrive)
			cands = append(cands, anchorageStayWater{at: stay.arrive, heightM: h, what: "arrival"})
		}
		if covered(stay.leave) {
			h, _ := interpolateTideNow(sorted, stay.leave)
			cands = append(cands, anchorageStayWater{at: stay.leave, heightM: h, what: "leaving"})
		}
	}
	for i := range cands {
		if low == nil || cands[i].heightM < low.heightM {
			low = &cands[i]
		}
		if high == nil || cands[i].heightM > high.heightM {
			high = &cands[i]
		}
	}
	return low, high
}

// anchorageDepthChecks fills in the depth section for a resolved depth. Nothing
// is guessed: a missing input skips the check that needs it and says which.
func (d assistantToolDeps) anchorageDepthChecks(s anchorageDepthSection, tide anchorageTideOut, forecast anchorageForecastOut, stay anchorageStay) anchorageDepthSection {
	switch {
	case s.Error != "":
		skippedChecks(&s, "the depth is unknown ("+s.Error+")")
		return s
	case !s.Known:
		s.Note = joinSentences(s.Note, "The depth is unknown: check the chart. No depth was given, so the checks that need one were skipped.")
		skippedChecks(&s, "the depth is unknown")
		return s
	}
	depth := *s.DepthM
	loc := stay.zone.Loc
	at := func(t time.Time) string { return t.In(loc).Format(toolTimeLayout) }

	var settings anchorageBoatSettings
	settingsErr := ""
	if d.anchorSettings == nil {
		settingsErr = "the boat's anchor settings are not available"
	} else if v, err := d.anchorSettings(); err != nil {
		settingsErr = "the boat's anchor settings could not be read (" + firstErrorLine(err) + ")"
	} else {
		settings = v
	}

	// The tide's lowest and highest water in the stay.
	var low, high *anchorageStayWater
	tideErr := ""
	if tide.section.Error != "" {
		tideErr = "the tide section failed (" + tide.section.Error + ")"
	} else if low, high = tideStayWaters(tide.sorted, stay); low == nil {
		tideErr = "the tide gives no water height inside the stay"
	}
	tideCaveat := ""
	if tide.section.AnchorageTideBody != nil && tide.section.Note != "" {
		tideCaveat = " " + tide.section.Note + " A lower or higher water may follow."
	}

	// The boat's draft and length, read once.
	boat := d.marinaBoat()
	draftUnknown := "the boat's draft isn't reported"
	if boat.why != "" {
		draftUnknown = "the boat's draft is unknown: " + strings.TrimSuffix(boat.why, ".")
	}

	// Clearance at the lowest water.
	c := &s.Clearance
	switch {
	case tideErr != "":
		c.anchorageCheck = skippedCheck(tideErr)
	case settingsErr != "":
		c.anchorageCheck = skippedCheck(settingsErr)
	case boat.draftM == nil:
		c.anchorageCheck = skippedCheck(draftUnknown)
	default:
		draft := *boat.draftM
		depthAtLow := depth + low.heightM
		clearance := depthAtLow - draft
		c.LowestWaterAt, c.LowestWaterM = low.at.In(loc).Format(time.RFC3339), ptr2(low.heightM)
		c.DepthAtLowM, c.DraftM, c.ClearanceM, c.WantedM = ptr2(depthAtLow), ptr2(draft), ptr2(clearance), floatPtr(settings.MinClearanceAtLowM)
		c.Status = checkOK
		verdict := fmt.Sprintf("at least the %.1f m wanted", settings.MinClearanceAtLowM)
		// Judged as displayed: 0.46 m reads as 0.5 m and meets a 0.5 m wanted.
		if math.Round(clearance*10)/10 < settings.MinClearanceAtLowM {
			c.Status = "too_shallow"
			verdict = fmt.Sprintf("LESS than the %.1f m wanted", settings.MinClearanceAtLowM)
		}
		c.Text = fmt.Sprintf("At the lowest water in the stay (%s at %s, %.2f m) there is %.1f m of water, %.1f m under a %.1f m draft: %s.%s",
			low.what, at(low.at), low.heightM, depthAtLow, clearance, draft, verdict, tideCaveat)
	}

	// Rode at the highest water.
	r := &s.Rode
	var rodeNeeded *float64
	switch {
	case tideErr != "":
		r.anchorageCheck = skippedCheck(tideErr)
	case settingsErr != "":
		r.anchorageCheck = skippedCheck(settingsErr)
	default:
		rodeNeeded = fillRodeCheck(r, settings, forecast, depth, high, stay)
	}

	// The swing circle for that rode.
	w := &s.Swing
	switch {
	case rodeNeeded == nil:
		reason := "the rode needed could not be worked out"
		if r.SkippedReason != "" {
			reason = r.SkippedReason
		}
		w.anchorageCheck = skippedCheck(reason)
	default:
		loa, ok := resolveLoaM(settings.LOAM, boat.loaM)
		if !ok {
			w.anchorageCheck = skippedCheck("the boat's length overall is not set in Settings, reported by the boat's instruments or held in the vessel particulars")
			break
		}
		rode := *rodeNeeded
		w.Status = "computed"
		if r.Status == "short" {
			// The rode needed is not aboard: the boat swings on the chain it has.
			rode, w.AllChainOut = settings.ChainOnboardM, true
		}
		radius := computeSwingRadiusM(rode, settings.GPSFromBowM, loa)
		w.RadiusM, w.RodeM, w.BowOffsetM, w.LoaM = ptr1(radius), ptr1(rode), floatPtr(settings.GPSFromBowM), floatPtr(loa)
		if w.AllChainOut {
			w.Text = fmt.Sprintf("With all %.0f m of chain out, short of the %.0f m needed, the boat swings within about %.0f m of the anchor: %.1f m from bow to GPS and the boat's %.1f m length.",
				rode, *rodeNeeded, radius, settings.GPSFromBowM, loa)
		} else {
			w.Text = fmt.Sprintf("The boat swings within about %.0f m of the anchor: %.0f m of rode, %.1f m from bow to GPS and the boat's %.1f m length.",
				radius, rode, settings.GPSFromBowM, loa)
		}
	}
	return s
}

// fillRodeCheck works out the rode for the highest water by the ratio method
// and sets the check. It returns the rode needed, nil when the check is
// skipped. A scope method of catenary is skipped, not approximated.
func fillRodeCheck(r *anchorageRodeCheck, settings anchorageBoatSettings, forecast anchorageForecastOut, depth float64, high *anchorageStayWater, stay anchorageStay) *float64 {
	skip := func(reason string) *float64 {
		r.anchorageCheck = skippedCheck(reason)
		return nil
	}
	r.Method = settings.ScopeMethod
	switch {
	case settings.ScopeMethod == "catenary":
		return skip(rodeCatenaryUnavailable)
	case settings.ChainOnboardM <= 0:
		return skip("the chain aboard is not set in Settings")
	case settings.BowRollerHeightM <= 0:
		// rodeRatioMethod has its own guard, worded as the browser planner words
		// it (the shared vectors pin that text); this one is for the operator.
		return skip("the bow roller height is not set in Settings")
	}
	if forecast.section.Error != "" {
		return skip("the forecast section failed (" + forecast.section.Error + "), so there is no wind to plan the rode for")
	}
	if forecast.stayMaxKts == nil {
		return skip("the forecast gives no wind for the stay")
	}
	planKts := rodePlanWindKts(*forecast.stayMaxKts)
	depthAtHigh := depth + high.heightM
	if depthAtHigh <= 0 {
		return skip(fmt.Sprintf("there is no water over the anchorage at the highest water (%.1f m)", depthAtHigh))
	}
	res := rodeRatioMethod(rodePlanInput{PlanningDepthM: depthAtHigh, BowRollerHeightM: settings.BowRollerHeightM, WindKts: &planKts})
	if res.UnavailableReason != "" {
		return skip("the rode planner cannot work it out: " + res.UnavailableReason)
	}

	needed := res.RecommendedRodeM
	checkedNote := ""
	if forecast.lastHour.Before(stay.leave) {
		r.WindCheckedUntil = forecast.lastHour.In(stay.zone.Loc).Format(time.RFC3339)
		checkedNote = fmt.Sprintf(" The forecast ends at %s, before the stay does, so the wind was only checked up to then.", forecast.lastHour.In(stay.zone.Loc).Format(toolTimeLayout))
	}
	r.HighestWaterAt, r.HighestWaterM = high.at.In(stay.zone.Loc).Format(time.RFC3339), ptr2(high.heightM)
	r.DepthAtHighM, r.WindKts = ptr2(depthAtHigh), floatPtr(planKts)
	r.RodeNeededM, r.ScopeRatio, r.ChainAboardM = ptr1(needed), ptr1(res.ScopeRatio), floatPtr(settings.ChainOnboardM)
	where := fmt.Sprintf("At the highest water in the stay (%s at %s, %.2f m) there is %.1f m of water, %.1f m from the bow roller",
		high.what, high.at.In(stay.zone.Loc).Format(toolTimeLayout), high.heightM, depthAtHigh, depthAtHigh+settings.BowRollerHeightM)
	basis := fmt.Sprintf("so about %.0f m of rode (%.1f:1) by the ratio method, planned at %.0f kts (the strongest forecast gust in the stay, rounded up to its band), against %.0f m of chain aboard",
		needed, res.ScopeRatio, planKts, settings.ChainOnboardM)
	if needed <= settings.ChainOnboardM {
		r.Status = checkOK
		r.Text = fmt.Sprintf("%s, %s: enough.%s", where, basis, checkedNote)
	} else {
		r.Status = "short"
		r.ShortByM = ptr1(needed - settings.ChainOnboardM)
		r.Text = fmt.Sprintf("%s, %s: SHORT by %.0f m.%s", where, basis, needed-settings.ChainOnboardM, checkedNote)
	}
	return &needed
}

// anchorSettingsFromSettingsFile reads the Anchor settings fresh on every call.
func anchorSettingsFromSettingsFile(settingsPath string) func() (anchorageBoatSettings, error) {
	return func() (anchorageBoatSettings, error) {
		settings, err := readSettings(settingsPath)
		if err != nil {
			return anchorageBoatSettings{}, fmt.Errorf("settings are unreadable: %w", err)
		}
		a := buildSettingsPayload(settings).Anchor
		return anchorageBoatSettings{
			BowRollerHeightM: a.BowRollerHeightM, ChainOnboardM: a.ChainOnboardM, ScopeMethod: a.ScopeMethod,
			GPSFromBowM: a.GPSFromBowM, LOAM: a.LOAM, MinClearanceAtLowM: a.MinClearanceAtLowM,
		}, nil
	}
}

func joinSentences(a, b string) string {
	if a == "" {
		return b
	}
	return a + " " + b
}
