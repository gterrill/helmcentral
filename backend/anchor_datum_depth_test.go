package main

import (
	"math"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/labstack/echo/v4"
)

func TestComputeDatumDepth(t *testing.T) {
	cases := []struct {
		name                        string
		depthM, tideFt, transducerM float64
		source                      string
		wantM                       float64
		wantReasonContains          string
	}{
		{name: "sounder adds transducer, takes off tide", depthM: 4.0, tideFt: 3.2808399, transducerM: 0.5, source: planningDepthFromSounder, wantM: 3.5},
		{name: "zero tide is a known tide", depthM: 4.0, tideFt: 0, transducerM: 0.5, source: planningDepthFromSounder, wantM: 4.5},
		{name: "negative tide is a real reading", depthM: 4.0, tideFt: -0.4, transducerM: 0.5, source: planningDepthFromSounder, wantM: 4.0 + 0.5 + 0.4/metersToFeet},
		{name: "operator figure takes no transducer offset", depthM: 4.0, tideFt: 3.2808399, transducerM: 0, source: planningDepthFromOperator, wantM: 3.0},
		{name: "operator figure ignores a set transducer", depthM: 4.0, tideFt: 0, transducerM: 0.5, source: planningDepthFromOperator, wantM: 4.0},
		{name: "sounder, transducer unset", depthM: 4.0, tideFt: 2, transducerM: 0, source: planningDepthFromSounder, wantReasonContains: "Settings → Anchor Watch"},
		{name: "no planning depth", depthM: -1, tideFt: 2, transducerM: 0.5, source: planningDepthFromSounder, wantReasonContains: "depth was recorded"},
		{name: "legacy zero planning depth", depthM: 0, tideFt: 2, transducerM: 0.5, source: "", wantReasonContains: "depth was recorded"},
		{name: "tide unknown", depthM: 4.0, tideFt: -1, transducerM: 0.5, source: planningDepthFromSounder, wantReasonContains: "tide"},
		{name: "source unknown is not guessed", depthM: 4.0, tideFt: 2, transducerM: 0.5, source: "", wantReasonContains: "sounder or typed"},
		{name: "sounder wins over transducer when depth missing", depthM: -1, tideFt: -1, transducerM: 0, source: planningDepthFromSounder, wantReasonContains: "depth was recorded"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, reason := computeDatumDepth(tc.depthM, tc.tideFt, tc.source, tc.transducerM)
			if tc.wantReasonContains != "" {
				if got != nil || !strings.Contains(reason, tc.wantReasonContains) {
					t.Fatalf("want no value and reason containing %q, got %v %q", tc.wantReasonContains, got, reason)
				}
				return
			}
			if got == nil || reason != "" || math.Abs(*got-tc.wantM) > 0.001 {
				t.Fatalf("want %v, got %v %q", tc.wantM, got, reason)
			}
		})
	}
}

func TestPlanningDepthSourceFollowsTheWriter(t *testing.T) {
	anchorTestEnv(t, 0)
	resetAnchorWatchState(t)

	code, resp := postAnchorWatch(t, map[string]any{"lat": -21.1, "lon": 149.2, "planning_depth_m": 4.0, "planning_tide_height_ft": 1.0})
	if code != http.StatusOK || resp["planning_depth_source"] != "sounder" {
		t.Fatalf("a depth sent with the drop is the sounder's: %d %+v", code, resp)
	}
	code, resp = postAnchorWatch(t, map[string]any{"lat": -21.1001, "lon": 149.2})
	if code != http.StatusOK || resp["planning_depth_source"] != "sounder" {
		t.Fatalf("a reposition keeps the source: %d %+v", code, resp)
	}
	code, resp = patchAnchorWatchForTest(t, map[string]any{"sea_state": "calm"})
	if code != http.StatusOK || resp["planning_depth_source"] != "sounder" {
		t.Fatalf("an unrelated patch keeps the source: %d %+v", code, resp)
	}
	code, resp = patchAnchorWatchForTest(t, map[string]any{"planning_depth_m": 5.0, "planning_tide_height_ft": 1.0})
	if code != http.StatusOK || resp["planning_depth_source"] != "operator" {
		t.Fatalf("a typed depth is the operator's: %d %+v", code, resp)
	}
	code, resp = patchAnchorWatchForTest(t, map[string]any{"planning_depth_m": -1})
	if code != http.StatusOK || resp["planning_depth_source"] != "" {
		t.Fatalf("a cleared depth has no source: %d %+v", code, resp)
	}
}

func TestStoredWatchWithoutSourceIsUnknown(t *testing.T) {
	anchorTestEnv(t, 0)
	resetAnchorWatchState(t)
	postAnchorWatch(t, map[string]any{"lat": -21.1, "lon": 149.2, "planning_depth_m": 4.0, "planning_tide_height_ft": 1.0})
	anchorWatchMu.Lock()
	anchorWatchState.PlanningDepthSource = ""
	anchorWatchMu.Unlock()
	e := echo.New()
	rec := httptest.NewRecorder()
	if err := getAnchorWatch(e.NewContext(httptest.NewRequest(http.MethodGet, "/api/anchor-watch", nil), rec)); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(rec.Body.String(), `"datum_depth_m":null`) || !strings.Contains(rec.Body.String(), "sounder or typed") {
		t.Fatalf("legacy watch should say the source is unknown, got %s", rec.Body.String())
	}
}

func TestDatumDepthSaysWhenSettingsCannotBeRead(t *testing.T) {
	settingsPath := anchorTestEnv(t, 0)
	resetAnchorWatchState(t)
	postAnchorWatch(t, map[string]any{"lat": -21.1, "lon": 149.2, "planning_depth_m": 4.0, "planning_tide_height_ft": 1.0})
	if err := os.WriteFile(settingsPath, []byte("anchor: [unclosed"), 0o644); err != nil {
		t.Fatal(err)
	}
	e := echo.New()
	rec := httptest.NewRecorder()
	if err := getAnchorWatch(e.NewContext(httptest.NewRequest(http.MethodGet, "/api/anchor-watch", nil), rec)); err != nil {
		t.Fatal(err)
	}
	body := rec.Body.String()
	if !strings.Contains(body, "Settings could not be read, so the transducer depth is unknown.") || strings.Contains(body, "yaml") {
		t.Fatalf("want an operator sentence, got %s", body)
	}
}

func TestGetAnchorWatchCarriesDatumDepth(t *testing.T) {
	settingsPath := anchorTestEnv(t, 0)
	resetAnchorWatchState(t)

	code, resp := postAnchorWatch(t, map[string]any{
		"lat": -21.1, "lon": 149.2, "planning_depth_m": 4.0, "planning_tide_height_ft": 0.0,
	})
	if code != http.StatusOK {
		t.Fatalf("post: %d %+v", code, resp)
	}
	if resp["datum_depth_m"] != nil || !strings.Contains(resp["datum_depth_reason"].(string), "Settings → Anchor Watch") {
		t.Fatalf("unset transducer should explain, got %+v", resp)
	}

	body, _ := os.ReadFile(settingsPath)
	if err := os.WriteFile(settingsPath, append(body, []byte("  transducer_depth_m: 0.6\n")...), 0o644); err != nil {
		t.Fatal(err)
	}
	e := echo.New()
	rec := httptest.NewRecorder()
	if err := getAnchorWatch(e.NewContext(httptest.NewRequest(http.MethodGet, "/api/anchor-watch", nil), rec)); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(rec.Body.String(), `"datum_depth_m":4.6`) || !strings.Contains(rec.Body.String(), `"datum_depth_reason":""`) {
		t.Fatalf("want datum depth 4.6, got %s", rec.Body.String())
	}
}

func TestValidateSettingsChangeTransducerDepth(t *testing.T) {
	for _, bad := range []float64{-0.1, 5.1, math.NaN(), math.Inf(1)} {
		var next settingsPayload
		next.Anchor.TransducerDepthM = bad
		if err := validateSettingsChange(settingsPayload{}, next); err == nil || err.Field != "anchor.transducer_depth_m" {
			t.Fatalf("%v should be refused, got %+v", bad, err)
		}
	}
	for _, ok := range []float64{0, 0.4, 5} {
		var next settingsPayload
		next.Anchor.TransducerDepthM = ok
		if err := validateSettingsChange(settingsPayload{}, next); err != nil {
			t.Fatalf("%v should pass, got %+v", ok, err)
		}
	}
}

func TestTransducerDepthSettingRoundTrips(t *testing.T) {
	var req settingsPayload
	req.Anchor.TransducerDepthM = 0.45
	if got := normalizeSettingsPayload(req).Anchor.TransducerDepthM; got != 0.45 {
		t.Fatalf("normalize changed it to %v", got)
	}
	p := buildSettingsPayload(map[string]any{"anchor": map[string]any{"transducer_depth_m": 0.45}})
	if p.Anchor.TransducerDepthM != 0.45 {
		t.Fatalf("build read %v", p.Anchor.TransducerDepthM)
	}
	if buildSettingsPayload(map[string]any{}).Anchor.TransducerDepthM != 0 {
		t.Fatal("unset must read as 0")
	}
}
