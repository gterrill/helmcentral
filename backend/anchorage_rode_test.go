package main

import (
	"encoding/json"
	"math"
	"os"
	"path/filepath"
	"testing"
)

// The vectors are shared with frontend/src/lib/rode-plan.vectors.test.ts. Their
// expected values come from the TypeScript reference.
type rodeVectorMethod struct {
	RecommendedRodeM  float64 `json:"recommendedRodeM"`
	ScopeRatio        float64 `json:"scopeRatio"`
	UnavailableReason *string `json:"unavailableReason"`
}

type rodeVectorFile struct {
	Tolerance float64 `json:"tolerance"`
	Cases     []struct {
		Name  string `json:"name"`
		Input struct {
			rodePlanInput
			LoaM       *float64 `json:"loaM"`
			BowOffsetM float64  `json:"bowOffsetM"`
		} `json:"input"`
		Expected struct {
			Ratio            rodeVectorMethod `json:"ratio"`
			RatioSwingRadius *float64         `json:"ratioSwingRadiusM"`
		} `json:"expected"`
	} `json:"cases"`
	WindBands []struct {
		Kts     *float64 `json:"kts"`
		PlanKts *float64 `json:"planKts"`
	} `json:"windBands"`
	Loa []struct {
		SettingsLoaM         float64  `json:"settingsLoaM"`
		VesselLengthOverallM *float64 `json:"vesselLengthOverallM"`
		LoaM                 *float64 `json:"loaM"`
	} `json:"loa"`
}

func rodeNear(a, b, tol float64) bool {
	return math.Abs(a-b) <= tol*math.Max(1, math.Max(math.Abs(a), math.Abs(b)))
}

func rodeNearPtr(got, want *float64, tol float64) bool {
	if got == nil || want == nil {
		return got == nil && want == nil
	}
	return rodeNear(*got, *want, tol)
}

func checkRodeMethod(t *testing.T, name string, got rodeMethodResult, want rodeVectorMethod, tol float64) {
	t.Helper()
	reason := ""
	if want.UnavailableReason != nil {
		reason = *want.UnavailableReason
	}
	if got.UnavailableReason != reason {
		t.Errorf("%s: unavailable reason %q, want %q", name, got.UnavailableReason, reason)
		return
	}
	if reason == "" && (!rodeNear(got.RecommendedRodeM, want.RecommendedRodeM, tol) || !rodeNear(got.ScopeRatio, want.ScopeRatio, tol)) {
		t.Errorf("%s: rode %v scope %v, want %v %v", name, got.RecommendedRodeM, got.ScopeRatio, want.RecommendedRodeM, want.ScopeRatio)
	}
}

func TestRodePlanMatchesTheTypeScriptReference(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join("testdata", "rode_plan_vectors.json"))
	if err != nil {
		t.Fatal(err)
	}
	var v rodeVectorFile
	if err := json.Unmarshal(raw, &v); err != nil {
		t.Fatal(err)
	}
	if len(v.Cases) < 8 || v.Tolerance <= 0 {
		t.Fatalf("vector file has %d cases and tolerance %g", len(v.Cases), v.Tolerance)
	}
	for _, c := range v.Cases {
		got := rodeRatioMethod(c.Input.rodePlanInput)
		checkRodeMethod(t, c.Name+" ratio", got, c.Expected.Ratio, v.Tolerance)

		// The swing is only asked of a rode that exists and a length that is known.
		var swing *float64
		if got.UnavailableReason == "" && c.Input.LoaM != nil {
			s := computeSwingRadiusM(got.RecommendedRodeM, c.Input.BowOffsetM, *c.Input.LoaM)
			swing = &s
		}
		if !rodeNearPtr(swing, c.Expected.RatioSwingRadius, v.Tolerance) {
			t.Errorf("%s: swing %v, want %v", c.Name, swing, c.Expected.RatioSwingRadius)
		}
	}
}

func TestRodeWindBandsMatchTheTypeScriptReference(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join("testdata", "rode_plan_vectors.json"))
	if err != nil {
		t.Fatal(err)
	}
	var v rodeVectorFile
	if err := json.Unmarshal(raw, &v); err != nil {
		t.Fatal(err)
	}
	for _, b := range v.WindBands {
		if b.Kts == nil {
			continue // the reference answers null for no reading; the Go caller never asks
		}
		if got := rodePlanWindKts(*b.Kts); b.PlanKts == nil || got != *b.PlanKts {
			t.Errorf("wind %v kts plans at %v, want %v", *b.Kts, got, b.PlanKts)
		}
	}
	for _, l := range v.Loa {
		var got *float64
		if m, ok := resolveLoaM(l.SettingsLoaM, l.VesselLengthOverallM); ok {
			got = &m
		}
		if !rodeNearPtr(got, l.LoaM, v.Tolerance) {
			t.Errorf("loa settings %v vessel %v = %v, want %v", l.SettingsLoaM, l.VesselLengthOverallM, got, l.LoaM)
		}
	}
}
