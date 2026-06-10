package main

import (
	"testing"
	"time"
)

func TestParseGNSSPositionValidationFromSignalKMethodQualityAndHDOP(t *testing.T) {
	payload := map[string]any{
		"navigation": map[string]any{
			"gnss": map[string]any{
				"methodQuality": map[string]any{
					"value": "GNSS Fix",
				},
				"horizontalDilution": map[string]any{
					"value": 1.7,
				},
			},
		},
	}

	validation := parseGNSSPositionValidation(payload)
	if validation.Status != "trusted" {
		t.Fatalf("expected trusted validation, got %q", validation.Status)
	}
	if validation.QualityIndicator != 1 {
		t.Fatalf("expected quality 1, got %d", validation.QualityIndicator)
	}
	if validation.HDOP != 1.7 {
		t.Fatalf("expected hdop 1.7, got %.1f", validation.HDOP)
	}
}

func TestParseGNSSPositionValidationFromSignalKDGNSSFixLabel(t *testing.T) {
	payload := map[string]any{
		"navigation": map[string]any{
			"gnss": map[string]any{
				"methodQuality": map[string]any{
					"value": "DGNSS fix",
				},
				"positionDilution": map[string]any{
					"value": 2.2,
				},
			},
		},
	}

	validation := parseGNSSPositionValidation(payload)
	if validation.Status != "trusted" {
		t.Fatalf("expected trusted validation, got %q", validation.Status)
	}
	if validation.QualityIndicator != 2 {
		t.Fatalf("expected DGNSS quality 2, got %d", validation.QualityIndicator)
	}
}

func TestResolveGNSSPositionFreezesLastGoodFixOnCritical(t *testing.T) {
	resetGNSSPositionValidationState()
	t.Cleanup(resetGNSSPositionValidationState)

	trusted := gnssPositionValidation{QualityIndicator: 1, HDOP: 1.3, Status: "trusted", Trusted: true}
	lat, lon := resolveGNSSPosition(-25.2939, 152.9103, trusted)
	if lat != -25.2939 || lon != 152.9103 {
		t.Fatalf("expected trusted fix to pass through, got %.4f %.4f", lat, lon)
	}

	critical := gnssPositionValidation{QualityIndicator: 0, HDOP: 4.8, Status: "critical", Critical: true, Reason: "quality indicator reports no fix"}
	lat, lon = resolveGNSSPosition(-25.9, 152.3, critical)
	if lat != -25.2939 || lon != 152.9103 {
		t.Fatalf("expected critical fix to freeze last good position, got %.4f %.4f", lat, lon)
	}
}

func TestApplyGNSSHeuristicsCriticalOnStaleTimestamp(t *testing.T) {
	resetGNSSPositionValidationState()
	t.Cleanup(resetGNSSPositionValidationState)

	now := time.Date(2026, 6, 11, 0, 0, 0, 0, time.UTC)
	validation := gnssPositionValidation{QualityIndicator: 1, HDOP: 0.9, Status: "trusted", Trusted: true}
	updated := applyGNSSHeuristics(validation, gnssObservedSample{
		Latitude:      -25.2939,
		Longitude:     152.9103,
		DepthMeters:   4.0,
		Navigation:    "anchored",
		ObservedAt:    now.Add(-45 * time.Second),
		HasObservedAt: true,
	}, now)

	if updated.Status != "critical" {
		t.Fatalf("expected stale sample to be critical, got %q", updated.Status)
	}
}

func TestApplyGNSSHeuristicsCriticalOnImplausibleAnchorJump(t *testing.T) {
	resetGNSSPositionValidationState()
	t.Cleanup(resetGNSSPositionValidationState)

	now := time.Date(2026, 6, 11, 0, 0, 0, 0, time.UTC)
	base := gnssPositionValidation{QualityIndicator: 1, HDOP: 0.9, Status: "trusted", Trusted: true}

	_ = applyGNSSHeuristics(base, gnssObservedSample{
		Latitude:      -25.2939,
		Longitude:     152.9103,
		DepthMeters:   4.0,
		Navigation:    "anchored",
		ObservedAt:    now,
		HasObservedAt: true,
	}, now)

	updated := applyGNSSHeuristics(base, gnssObservedSample{
		Latitude:      -25.2939,
		Longitude:     152.9203,
		DepthMeters:   4.1,
		Navigation:    "anchored",
		ObservedAt:    now.Add(5 * time.Second),
		HasObservedAt: true,
	}, now.Add(5*time.Second))

	if updated.Status != "critical" {
		t.Fatalf("expected jump-speed heuristic to be critical, got %q", updated.Status)
	}
}

func TestApplyGNSSHeuristicsDegradedOnDepthJump(t *testing.T) {
	resetGNSSPositionValidationState()
	t.Cleanup(resetGNSSPositionValidationState)

	now := time.Date(2026, 6, 11, 0, 0, 0, 0, time.UTC)
	base := gnssPositionValidation{QualityIndicator: 1, HDOP: 0.9, Status: "trusted", Trusted: true}

	_ = applyGNSSHeuristics(base, gnssObservedSample{
		Latitude:      -25.2939,
		Longitude:     152.9103,
		DepthMeters:   4.0,
		Navigation:    "anchored",
		ObservedAt:    now,
		HasObservedAt: true,
	}, now)

	updated := applyGNSSHeuristics(base, gnssObservedSample{
		Latitude:      -25.29391,
		Longitude:     152.91031,
		DepthMeters:   6.2,
		Navigation:    "anchored",
		ObservedAt:    now.Add(5 * time.Second),
		HasObservedAt: true,
	}, now.Add(5*time.Second))

	if updated.Status != "degraded" {
		t.Fatalf("expected depth jump heuristic to be degraded, got %q", updated.Status)
	}
}

func TestApplyGNSSHeuristicsHysteresisRequiresRecoverySamples(t *testing.T) {
	resetGNSSPositionValidationState()
	t.Cleanup(resetGNSSPositionValidationState)

	now := time.Date(2026, 6, 11, 0, 0, 0, 0, time.UTC)
	critical := gnssPositionValidation{QualityIndicator: 0, HDOP: 5.0, Status: "critical", Critical: true}
	trusted := gnssPositionValidation{QualityIndicator: 1, HDOP: 0.8, Status: "trusted", Trusted: true}

	_ = applyGNSSHeuristics(critical, gnssObservedSample{Latitude: -25.2939, Longitude: 152.9103, Navigation: "anchored", ObservedAt: now, HasObservedAt: true}, now)

	for i := 1; i <= 4; i++ {
		updated := applyGNSSHeuristics(trusted, gnssObservedSample{
			Latitude:      -25.2939,
			Longitude:     152.9103,
			Navigation:    "anchored",
			ObservedAt:    now.Add(time.Duration(i) * 4 * time.Second),
			HasObservedAt: true,
		}, now.Add(time.Duration(i)*4*time.Second))
		if updated.Status != "critical" {
			t.Fatalf("expected hysteresis to remain critical at sample %d, got %q", i, updated.Status)
		}
	}

	final := applyGNSSHeuristics(trusted, gnssObservedSample{
		Latitude:      -25.2939,
		Longitude:     152.9103,
		Navigation:    "anchored",
		ObservedAt:    now.Add(20 * time.Second),
		HasObservedAt: true,
	}, now.Add(20*time.Second))

	if final.Status != "trusted" {
		t.Fatalf("expected hysteresis recovery to trusted, got %q", final.Status)
	}
}
