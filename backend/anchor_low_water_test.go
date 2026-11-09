package main

import (
	"encoding/json"
	"errors"
	"math"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/labstack/echo/v4"
)

// The Anchor Watch low-water clearance (ADR 0135): the depth at the boat's
// position, projected to the next low tide, against the water the operator
// wants under the keel. These tests are the rules the browser's own
// computeLowWaterClearance used to hold; the server now holds them alone.

// lwNow is the instant the fixtures are built around: the tide reading was
// taken 15 minutes before it and the next low is 20 minutes after it.
var lwNow = time.Date(2026, 9, 26, 14, 0, 0, 0, time.UTC)

func lwTide(mutate func(*tideTodayResponse)) *tideTodayResponse {
	t := &tideTodayResponse{
		Datetime:            "2026-09-26T13:45:00Z",
		CurrentTideHeightFt: 3,
		TideDirection:       "Falling",
		HighTideTime:        "2026-09-26T20:00:00Z",
		HighTideHeightFt:    5,
		LowTideTime:         "2026-09-26T14:20:00Z",
		LowTideHeightFt:     1,
		StationName:         "Test Station",
		Provider:            "test",
	}
	if mutate != nil {
		mutate(t)
	}
	return t
}

func lwInput(mutate func(*lowWaterInput)) lowWaterInput {
	in := lowWaterInput{DepthM: floatPtr(4), Tide: lwTide(nil), DraftM: floatPtr(1.2), MarginM: 0.5, Now: lwNow}
	if mutate != nil {
		mutate(&in)
	}
	return in
}

func lwFloat(t *testing.T, name string, got *float64, want, tol float64) {
	t.Helper()
	if got == nil || math.Abs(*got-want) > tol {
		t.Errorf("%s = %v, want %v", name, got, want)
	}
}

func TestLowWater_OKWhenTheClearanceMeetsTheMargin(t *testing.T) {
	// Fall = (3 - 1) / 3.28084 = 0.6096 m. Depth at low 3.3904, clearance 2.1904.
	got := computeLowWaterClearance(lwInput(nil))
	if got.Status != lowWaterOK || got.Reason != "" {
		t.Fatalf("got %+v", got)
	}
	lwFloat(t, "depth_now_m", got.DepthNowM, 4, 1e-9)
	lwFloat(t, "tide_fall_m", got.TideFallM, 0.6096, 1e-3)
	lwFloat(t, "depth_at_low_m", got.DepthAtLowM, 3.3904, 1e-3)
	lwFloat(t, "draft_m", got.DraftM, 1.2, 1e-9)
	lwFloat(t, "clearance_m", got.ClearanceM, 2.1904, 1e-3)
	lwFloat(t, "margin_m", got.MarginM, 0.5, 1e-9)
	if got.LowTideTime != "2026-09-26T14:20:00Z" {
		t.Errorf("low_tide_time = %q", got.LowTideTime)
	}
}

func TestLowWater_TooShallowUnderTheMargin(t *testing.T) {
	got := computeLowWaterClearance(lwInput(func(in *lowWaterInput) { in.DepthM = floatPtr(2) }))
	if got.Status != lowWaterTooShallow {
		t.Fatalf("got %+v", got)
	}
	lwFloat(t, "clearance_m", got.ClearanceM, 0.1904, 1e-3)
}

func TestLowWater_AClearanceExactlyAtTheMarginIsOK(t *testing.T) {
	fallM := (3.0 - 1.0) / metersToFeet
	got := computeLowWaterClearance(lwInput(func(in *lowWaterInput) { in.DepthM = floatPtr(0.5 + 1.2 + fallM) }))
	if got.Status != lowWaterOK {
		t.Fatalf("got %+v", got)
	}
	lwFloat(t, "clearance_m", got.ClearanceM, 0.5, 1e-6)
}

// Judged as displayed, as anchorage_details does: 0.46 m reads as 0.5 m and
// meets a 0.5 m margin, where 0.44 m reads as 0.4 m and does not.
func TestLowWater_TheClearanceIsJudgedAsDisplayed(t *testing.T) {
	fallM := (3.0 - 1.0) / metersToFeet
	for _, tc := range []struct {
		clearance float64
		want      string
	}{{0.46, lowWaterOK}, {0.44, lowWaterTooShallow}, {0.45, lowWaterOK}} {
		got := computeLowWaterClearance(lwInput(func(in *lowWaterInput) { in.DepthM = floatPtr(tc.clearance + 1.2 + fallM) }))
		if got.Status != tc.want {
			t.Errorf("clearance %.2f: status %q, want %q", tc.clearance, got.Status, tc.want)
		}
		// The figure itself is not rounded: Mate and the page round it for display.
		lwFloat(t, "clearance_m", got.ClearanceM, tc.clearance, 1e-9)
	}
}

func TestLowWater_FallClampsToZeroWhenAlreadyBelowTheNextLow(t *testing.T) {
	got := computeLowWaterClearance(lwInput(func(in *lowWaterInput) {
		in.Tide = lwTide(func(t *tideTodayResponse) { t.CurrentTideHeightFt, t.LowTideHeightFt = 0.5, 1 })
	}))
	if got.Status != lowWaterOK {
		t.Fatalf("got %+v", got)
	}
	lwFloat(t, "tide_fall_m", got.TideFallM, 0, 1e-9)
	lwFloat(t, "depth_at_low_m", got.DepthAtLowM, 4, 1e-6)
	lwFloat(t, "clearance_m", got.ClearanceM, 2.8, 1e-6)
}

func TestLowWater_UnknownNamesWhatIsMissing(t *testing.T) {
	passedLow := lwTide(func(t *tideTodayResponse) { t.LowTideTime = "2026-09-26T13:00:00Z" })
	cases := []struct {
		name   string
		mutate func(*lowWaterInput)
		want   string
	}{
		{"no depth", func(in *lowWaterInput) { in.DepthM = nil }, lowWaterNoDepth},
		{"depth sentinel -1", func(in *lowWaterInput) { in.DepthM = floatPtr(-1) }, lowWaterNoDepth},
		{"no draft", func(in *lowWaterInput) { in.DraftM = nil }, lowWaterNoDraft},
		{"draft sentinel -1", func(in *lowWaterInput) { in.DraftM = floatPtr(-1) }, lowWaterNoDraft},
		{"zero draft", func(in *lowWaterInput) { in.DraftM = floatPtr(0) }, lowWaterNoDraft},
		{"no tide", func(in *lowWaterInput) { in.Tide = nil }, lowWaterNoTide},
		{"current height is the -1 sentinel", func(in *lowWaterInput) {
			in.Tide = lwTide(func(t *tideTodayResponse) { t.CurrentTideHeightFt = -1 })
		}, lowWaterNoTide},
		{"next low height is the -1 sentinel", func(in *lowWaterInput) {
			in.Tide = lwTide(func(t *tideTodayResponse) { t.LowTideHeightFt = -1 })
		}, lowWaterNoTide},
		{"next low is exactly now", func(in *lowWaterInput) {
			in.Tide = lwTide(func(t *tideTodayResponse) { t.LowTideTime = lwNow.Format(time.RFC3339) })
		}, lowWaterTideStale},
		{"next low already passed", func(in *lowWaterInput) {
			in.Tide = lwTide(func(t *tideTodayResponse) { t.LowTideTime = "2026-09-26T13:00:00Z" })
		}, lowWaterTideStale},
		{"low tide time does not parse", func(in *lowWaterInput) {
			in.Tide = lwTide(func(t *tideTodayResponse) { t.LowTideTime = "not-a-date" })
		}, lowWaterTideStale},
		{"no depth is reported ahead of a stale tide", func(in *lowWaterInput) { in.DepthM, in.Tide = nil, passedLow }, lowWaterNoDepth},
		{"no tide station", func(in *lowWaterInput) { in.Tide, in.TideReason = nil, lowWaterNoTideStation }, lowWaterNoTideStation},
		{"tide unreachable", func(in *lowWaterInput) { in.Tide, in.TideReason = nil, lowWaterTideUnreachable }, lowWaterTideUnreachable},
	}
	for _, tc := range cases {
		got := computeLowWaterClearance(lwInput(tc.mutate))
		if got.Status != lowWaterUnknown || got.Reason != tc.want {
			t.Errorf("%s: got status %q reason %q, want unknown/%q", tc.name, got.Status, got.Reason, tc.want)
		}
		if got.ClearanceM != nil || got.DepthAtLowM != nil || got.LowTideTime != "" {
			t.Errorf("%s: an unknown result carries a verdict: %+v", tc.name, got)
		}
	}
}

// A real spring low can read below -1 ft on some datums; only the sentinel
// itself, exactly -1, means "not published".
func TestLowWater_NegativeHeightsAreRealData(t *testing.T) {
	cases := []struct {
		name         string
		current, low float64
		fallFt       float64
	}{
		{"negative next low", 3, -0.4, 3.4},
		{"negative current and low", -0.4, -0.9, 0.5},
		{"next low below -1 ft", 3, -1.2, 4.2},
		{"current and low below -1 ft", -1.5, -1.9, 0.4},
	}
	for _, tc := range cases {
		got := computeLowWaterClearance(lwInput(func(in *lowWaterInput) {
			in.Tide = lwTide(func(t *tideTodayResponse) { t.CurrentTideHeightFt, t.LowTideHeightFt = tc.current, tc.low })
		}))
		if got.Status == lowWaterUnknown {
			t.Errorf("%s: unknown (%s)", tc.name, got.Reason)
			continue
		}
		lwFloat(t, tc.name+" depth_at_low_m", got.DepthAtLowM, 4-tc.fallFt/metersToFeet, 1e-3)
	}
}

// The tide is built fresh for every request, so a reading has no age rule: only
// a low that has already passed makes it stale.
func TestLowWater_ATideStampedLongAgoIsNotStaleWhileItsLowIsAhead(t *testing.T) {
	got := computeLowWaterClearance(lwInput(func(in *lowWaterInput) {
		in.Tide = lwTide(func(t *tideTodayResponse) { t.Datetime = "2026-09-25T00:00:00Z" })
	}))
	if got.Status == lowWaterUnknown {
		t.Errorf("got %+v", got)
	}
}

// A clearance below zero is too shallow whatever the margin, however it rounds.
func TestLowWater_AClearanceBelowZeroIsTooShallowWhateverTheMargin(t *testing.T) {
	fallM := (3.0 - 1.0) / metersToFeet
	got := computeLowWaterClearance(lwInput(func(in *lowWaterInput) {
		in.DepthM = floatPtr(-0.04 + 1.2 + fallM)
		in.MarginM = 0
	}))
	if got.Status != lowWaterTooShallow {
		t.Errorf("clearance -0.04 with margin 0: %+v", got)
	}
}

func TestLowWater_UnknownKeepsTheInputsItDidHave(t *testing.T) {
	got := computeLowWaterClearance(lwInput(func(in *lowWaterInput) { in.Tide = nil }))
	lwFloat(t, "depth_now_m", got.DepthNowM, 4, 1e-9)
	lwFloat(t, "draft_m", got.DraftM, 1.2, 1e-9)
	lwFloat(t, "margin_m", got.MarginM, 0.5, 1e-9)
}

func TestLowWater_WireShape(t *testing.T) {
	raw, err := json.Marshal(computeLowWaterClearance(lwInput(nil)))
	if err != nil {
		t.Fatal(err)
	}
	var m map[string]any
	if err := json.Unmarshal(raw, &m); err != nil {
		t.Fatal(err)
	}
	for _, k := range []string{"status", "depth_now_m", "tide_fall_m", "depth_at_low_m", "draft_m", "clearance_m", "margin_m", "low_tide_time"} {
		if _, ok := m[k]; !ok {
			t.Errorf("missing %q in %s", k, raw)
		}
	}
	if _, ok := m["reason"]; ok {
		t.Errorf("a resolved result carries a reason: %s", raw)
	}

	raw, _ = json.Marshal(computeLowWaterClearance(lwInput(func(in *lowWaterInput) { in.DraftM = nil })))
	m = map[string]any{}
	_ = json.Unmarshal(raw, &m)
	if m["status"] != "unknown" || m["reason"] != "no_draft" {
		t.Errorf("unknown shape: %s", raw)
	}
	if _, ok := m["clearance_m"]; ok {
		t.Errorf("unknown carries a clearance: %s", raw)
	}
}

func TestLowWater_BuildReadsTheBoatTheTideAndTheMargin(t *testing.T) {
	state := vesselStateData{Depth: 4, DepthLastUpdateAge: 2, DraftM: 1.2}
	tide := func() (tideTodayResponse, error) { return *lwTide(nil), nil }
	margin := func() (float64, error) { return 0.5, nil }
	got, err := buildLowWater(state, tide, margin, nil, lwNow)
	if err != nil || got.Status != lowWaterOK {
		t.Fatalf("got %+v, %v", got, err)
	}

	// A depth that has stopped updating is no depth, on get_vessel_status's own
	// limit (120 s, as the page's STALE_AFTER_SECONDS): 120 s still counts, 121 s
	// does not, and an unknown age is no depth.
	edge := state
	edge.DepthLastUpdateAge = 120
	if got, err = buildLowWater(edge, tide, margin, nil, lwNow); err != nil || got.Status != lowWaterOK {
		t.Errorf("120 s old depth: got %+v, %v", got, err)
	}
	edge.DepthLastUpdateAge = 121
	if got, err = buildLowWater(edge, tide, margin, nil, lwNow); err != nil || got.Status != lowWaterUnknown || got.Reason != lowWaterNoDepth {
		t.Errorf("121 s old depth: got %+v, %v", got, err)
	}
	edge.DepthLastUpdateAge = -1
	if got, err = buildLowWater(edge, tide, margin, nil, lwNow); err != nil || got.Status != lowWaterUnknown || got.Reason != lowWaterNoDepth {
		t.Errorf("unknown depth age: got %+v, %v", got, err)
	}

	// The -1 sentinels never pass as readings.
	none := state
	none.Depth, none.DraftM = -1, -1
	got, _ = buildLowWater(none, tide, margin, nil, lwNow)
	if got.Reason != lowWaterNoDepth {
		t.Errorf("no depth: got %+v", got)
	}
}

func TestLowWater_BuildSaysWhyTheTideIsMissingAndFailsOnUnreadableSettings(t *testing.T) {
	state := vesselStateData{Depth: 4, DepthLastUpdateAge: 2, DraftM: 1.2}
	margin := func() (float64, error) { return 0.5, nil }

	noStation := func() (tideTodayResponse, error) {
		return tideTodayResponse{}, &tideTodayError{status: 502, reason: tideNotSetUp, message: "no tide provider configured"}
	}
	got, err := buildLowWater(state, noStation, margin, nil, lwNow)
	if err != nil || got.Status != lowWaterUnknown || got.Reason != lowWaterNoTideStation || got.Detail != "no tide station is set up" {
		t.Errorf("no station: got %+v, %v", got, err)
	}

	down := func() (tideTodayResponse, error) {
		return tideTodayResponse{}, &tideTodayError{status: 502, reason: tideUnreachable, message: "provider failed"}
	}
	got, err = buildLowWater(state, down, margin, nil, lwNow)
	if err != nil || got.Reason != lowWaterTideUnreachable || got.Detail != "the tide service could not be reached" {
		t.Errorf("unreachable: got %+v, %v", got, err)
	}

	badMargin := func() (float64, error) { return 0, errors.New("settings are unreadable") }
	if _, err := buildLowWater(state, func() (tideTodayResponse, error) { return *lwTide(nil), nil }, badMargin, nil, lwNow); err == nil {
		t.Error("an unreadable margin setting must be an error, not a default")
	}
}

func serveLowWaterForTest(t *testing.T, state func() (vesselStateData, error), watch bool, margin func() (float64, error), tideCalls *int) (int, map[string]any) {
	t.Helper()
	tide := func(now time.Time) (tideTodayResponse, error) {
		*tideCalls++
		r := *lwTide(nil)
		r.Datetime = now.Add(-5 * time.Minute).Format(time.RFC3339)
		r.LowTideTime = now.Add(time.Hour).Format(time.RFC3339)
		return r, nil
	}
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/anchor-watch/low-water", nil)
	if err := serveAnchorLowWater(echo.New().NewContext(req, rec), state, func() bool { return watch }, tide, margin, nil); err != nil {
		t.Fatal(err)
	}
	var body map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode %q: %v", rec.Body.String(), err)
	}
	return rec.Code, body
}

func TestLowWater_Endpoint(t *testing.T) {
	good := func() (vesselStateData, error) {
		return vesselStateData{Status: "anchored", Depth: 2, DepthLastUpdateAge: 1, DraftM: 1.2}, nil
	}
	margin := func() (float64, error) { return 0.5, nil }
	calls := 0

	code, body := serveLowWaterForTest(t, good, false, margin, &calls)
	if code != http.StatusOK || body["status"] != "too_shallow" {
		t.Errorf("good: %d %v", code, body)
	}
	if at, _ := body["worked_out_at"].(string); at == "" {
		t.Errorf("the verdict carries no worked_out_at: %v", body)
	}

	code, body = serveLowWaterForTest(t, func() (vesselStateData, error) { return vesselStateData{}, errors.New("signalk down") }, false, margin, &calls)
	if code != http.StatusBadGateway || body["error"] == nil {
		t.Errorf("no boat state must be an error, not an unknown: %d %v", code, body)
	}

	code, body = serveLowWaterForTest(t, good, false, func() (float64, error) { return 0, errors.New("unreadable") }, &calls)
	if code != http.StatusInternalServerError || body["error"] == nil {
		t.Errorf("unreadable settings: %d %v", code, body)
	}
}

// The page and get_vessel_status share one gate: anchored or moored, or a
// watch set. With neither there is no anchorage to check and no tide is built.
func TestLowWater_EndpointIsShutWhenNotAnchoredAndNoWatch(t *testing.T) {
	underway := func() (vesselStateData, error) {
		return vesselStateData{Status: "motoring", Depth: 2, DepthLastUpdateAge: 1, DraftM: 1.2}, nil
	}
	margin := func() (float64, error) { return 0.5, nil }
	calls := 0
	code, body := serveLowWaterForTest(t, underway, false, margin, &calls)
	if code != http.StatusOK || body["status"] != "unknown" || body["reason"] != "not_anchored" || calls != 0 {
		t.Errorf("under way, no watch: %d %v, tide built %d times", code, body, calls)
	}
	if _, has := body["clearance_m"]; has {
		t.Errorf("a shut gate carries a verdict: %v", body)
	}
	// A set watch opens it whatever the navigation state says.
	code, body = serveLowWaterForTest(t, underway, true, margin, &calls)
	if code != http.StatusOK || body["status"] != "too_shallow" || calls != 1 {
		t.Errorf("under way, watch set: %d %v, tide built %d times", code, body, calls)
	}
}

func TestLowWater_TheGate(t *testing.T) {
	for _, tc := range []struct {
		status string
		watch  bool
		want   bool
	}{{"anchored", false, true}, {"moored", false, true}, {"At Anchor", false, true}, {"motoring", false, false}, {"", false, false}, {"sailing", true, true}} {
		if got := lowWaterApplies(vesselStateData{Status: tc.status}, tc.watch); got != tc.want {
			t.Errorf("status %q watch %v: %v, want %v", tc.status, tc.watch, got, tc.want)
		}
	}
}

func TestLowWater_RouteIsRegistered(t *testing.T) {
	for _, r := range anchorRoutes() {
		if r.Method == http.MethodGet && r.Path == "/api/anchor-watch/low-water" && r.Tier == tierRead {
			return
		}
	}
	t.Error("GET /api/anchor-watch/low-water is not in anchorRoutes")
}

func TestLowWater_BuildDoesNotReadTheTideWithoutADepthOrADraft(t *testing.T) {
	calls := 0
	tide := func() (tideTodayResponse, error) { calls++; return *lwTide(nil), nil }
	margin := func() (float64, error) { return 0.5, nil }
	for name, state := range map[string]vesselStateData{
		"no depth": {Depth: -1, DepthLastUpdateAge: 2, DraftM: 1.2},
		"no draft": {Depth: 4, DepthLastUpdateAge: 2, DraftM: -1},
	} {
		got, err := buildLowWater(state, tide, margin, nil, lwNow)
		if err != nil || got.Status != lowWaterUnknown {
			t.Errorf("%s: got %+v, %v", name, got, err)
		}
	}
	if calls != 0 {
		t.Errorf("the tide was built %d times with nothing to project", calls)
	}
}
