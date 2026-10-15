package main

import (
	"sort"
	"strings"
	"sync"
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

	validation := parseGNSSPositionValidation(telemetryViewFromTree(payload, time.Time{}))
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

	validation := parseGNSSPositionValidation(telemetryViewFromTree(payload, time.Time{}))
	if validation.Status != "trusted" {
		t.Fatalf("expected trusted validation, got %q", validation.Status)
	}
	if validation.QualityIndicator != 2 {
		t.Fatalf("expected DGNSS quality 2, got %d", validation.QualityIndicator)
	}
}

func TestGnssSatelliteCount_FromPlainSignalKNumber(t *testing.T) {
	payload := map[string]any{
		"navigation": map[string]any{
			"gnss": map[string]any{
				"satellites": map[string]any{
					"value": 9.0,
				},
			},
		},
	}

	if count := gnssSatelliteCount(telemetryViewFromTree(payload, time.Time{})); count != 9 {
		t.Fatalf("expected 9 satellites, got %d", count)
	}
}

func TestGnssSatelliteCount_FromPerSatelliteArray(t *testing.T) {
	payload := map[string]any{
		"navigation": map[string]any{
			"gnss": map[string]any{
				"satellites": map[string]any{
					"value": []any{
						map[string]any{"SNR": 38.0},
						map[string]any{"snr": 41.5},
						map[string]any{"snr": 36.0},
					},
				},
			},
		},
	}

	if count := gnssSatelliteCount(telemetryViewFromTree(payload, time.Time{})); count != 3 {
		t.Fatalf("expected 3 satellites, got %d", count)
	}
}

func TestGnssSatelliteCount_MinusOneWhenUnavailable(t *testing.T) {
	if count := gnssSatelliteCount(telemetryViewFromTree(map[string]any{}, time.Time{})); count != -1 {
		t.Fatalf("expected -1 for missing satellite data, got %d", count)
	}
}

func TestParseGNSSPositionValidationIncludesSatelliteCount(t *testing.T) {
	payload := map[string]any{
		"navigation": map[string]any{
			"gnss": map[string]any{
				"methodQuality": map[string]any{
					"value": "GNSS Fix",
				},
				"horizontalDilution": map[string]any{
					"value": 1.7,
				},
				"satellites": map[string]any{
					"value": 11.0,
				},
			},
		},
	}

	validation := parseGNSSPositionValidation(telemetryViewFromTree(payload, time.Time{}))
	if validation.Satellites != 11 {
		t.Fatalf("expected 11 satellites, got %d", validation.Satellites)
	}
}

func TestParseGNSSPositionValidationSpoofingUniformSNR(t *testing.T) {
	payload := map[string]any{
		"navigation": map[string]any{
			"gnss": map[string]any{
				"methodQuality": map[string]any{
					"value": "GNSS Fix",
				},
				"horizontalDilution": map[string]any{
					"value": 1.2,
				},
				"satellites": map[string]any{
					"value": map[string]any{
						"1": map[string]any{"SNR": 45.0},
						"2": map[string]any{"SNR": 44.5},
						"3": map[string]any{"snr": 45.1},
						"4": map[string]any{"snr": 45.0},
						"5": map[string]any{"snr": 44.9},
					},
				},
			},
		},
	}

	validation := parseGNSSPositionValidation(telemetryViewFromTree(payload, time.Time{}))
	if validation.Status != "critical" {
		t.Fatalf("expected critical validation for spoofing, got %q", validation.Status)
	}
	if validation.Reason == "" {
		t.Fatalf("expected reason for critical status")
	}
}

func TestParseGNSSPositionValidationJammingLowMaxSNR(t *testing.T) {
	payload := map[string]any{
		"navigation": map[string]any{
			"gnss": map[string]any{
				"methodQuality": map[string]any{
					"value": "GNSS Fix",
				},
				"horizontalDilution": map[string]any{
					"value": 1.5,
				},
				"satellites": map[string]any{
					"value": []any{
						map[string]any{"SNR": 18.0},
						map[string]any{"snr": 12.0},
						map[string]any{"SNR": 15.0},
						map[string]any{"snr": 19.5},
					},
				},
			},
		},
	}

	validation := parseGNSSPositionValidation(telemetryViewFromTree(payload, time.Time{}))
	if validation.Status != "critical" {
		t.Fatalf("expected critical validation for jamming, got %q", validation.Status)
	}
	if validation.Reason == "" {
		t.Fatalf("expected reason for critical status")
	}
}

func TestResolveGNSSPositionFreezesLastGoodFixOnCritical(t *testing.T) {
	resetGNSSPositionValidationState()
	t.Cleanup(resetGNSSPositionValidationState)

	trusted := gnssPositionValidation{QualityIndicator: 1, HDOP: 1.3, Status: "trusted", Trusted: true}
	lat, lon := resolveGNSSPosition(-25.2939, 152.9103, true, trusted)
	if lat != -25.2939 || lon != 152.9103 {
		t.Fatalf("expected trusted fix to pass through, got %.4f %.4f", lat, lon)
	}

	critical := gnssPositionValidation{QualityIndicator: 0, HDOP: 4.8, Status: "critical", Critical: true, Reason: "quality indicator reports no fix"}
	lat, lon = resolveGNSSPosition(-25.9, 152.3, true, critical)
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
		HasPosition:   true,
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
		HasPosition:   true,
		Latitude:      -25.2939,
		Longitude:     152.9103,
		DepthMeters:   4.0,
		Navigation:    "anchored",
		ObservedAt:    now,
		HasObservedAt: true,
	}, now)

	updated := applyGNSSHeuristics(base, gnssObservedSample{
		HasPosition:   true,
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
		HasPosition:   true,
		Latitude:      -25.2939,
		Longitude:     152.9103,
		DepthMeters:   4.0,
		Navigation:    "anchored",
		ObservedAt:    now,
		HasObservedAt: true,
	}, now)

	updated := applyGNSSHeuristics(base, gnssObservedSample{
		HasPosition:   true,
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

func TestCriticalGNSSValidation_ReturnsCriticalResult(t *testing.T) {
	resetGNSSPositionValidationState()
	t.Cleanup(resetGNSSPositionValidationState)

	now := time.Date(2026, 6, 11, 0, 0, 0, 0, time.UTC)
	validation := criticalGNSSValidation("signalk unreachable: dial tcp: connection refused", now)

	if validation.Status != "critical" || !validation.Critical || validation.Trusted {
		t.Fatalf("expected critical/untrusted validation, got %+v", validation)
	}
	if validation.Reason == "" {
		t.Fatalf("expected a reason to be set")
	}
}

func TestCriticalGNSSValidation_EngagesRecoveryHysteresis(t *testing.T) {
	resetGNSSPositionValidationState()
	t.Cleanup(resetGNSSPositionValidationState)

	now := time.Date(2026, 6, 11, 0, 0, 0, 0, time.UTC)
	_ = criticalGNSSValidation("signalk unreachable", now)

	trusted := gnssPositionValidation{QualityIndicator: 1, HDOP: 0.8, Status: "trusted", Trusted: true}

	// A single good sample right after reconnecting should not instantly clear
	// the alarm — same hysteresis a degraded-fix critical event would apply.
	updated := applyGNSSHeuristics(trusted, gnssObservedSample{
		HasPosition:   true,
		Latitude:      -25.2939,
		Longitude:     152.9103,
		Navigation:    "anchored",
		ObservedAt:    now.Add(4 * time.Second),
		HasObservedAt: true,
	}, now.Add(4*time.Second))

	if updated.Status != "critical" {
		t.Fatalf("expected hysteresis to remain critical immediately after reconnect, got %q", updated.Status)
	}
}

func TestApplyGNSSHeuristicsHysteresisRequiresRecoverySamples(t *testing.T) {
	resetGNSSPositionValidationState()
	t.Cleanup(resetGNSSPositionValidationState)

	now := time.Date(2026, 6, 11, 0, 0, 0, 0, time.UTC)
	critical := gnssPositionValidation{QualityIndicator: 0, HDOP: 5.0, Status: "critical", Critical: true}
	trusted := gnssPositionValidation{QualityIndicator: 1, HDOP: 0.8, Status: "trusted", Trusted: true}

	_ = applyGNSSHeuristics(critical, gnssObservedSample{HasPosition: true, Latitude: -25.2939, Longitude: 152.9103, Navigation: "anchored", ObservedAt: now, HasObservedAt: true}, now)

	for i := 1; i <= 4; i++ {
		updated := applyGNSSHeuristics(trusted, gnssObservedSample{
			HasPosition:   true,
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
		HasPosition:   true,
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

// TestApplyGNSSHeuristics_ConcurrentCallsAreRaceFree guards the data race the
// audit flagged: applyGNSSHeuristics (and criticalGNSSValidation, which
// engages the same recovery-hysteresis latch) used to mutate the
// package-level gnssHeuristic state without holding gnssValidationMu, while
// resolveGNSSPosition and resetGNSSPositionValidationState did. In
// production this is reached from every fetchSignalKVesselState call --
// concurrently from /api/vessel-state requests and each stream client's own
// build -- so concurrent callers could make the recovery-hysteresis sample
// count advance faster than real samples arrived. Run with -race: this must
// fail on the unguarded code and pass once applyGNSSHeuristics/
// criticalGNSSValidation take gnssValidationMu for their mutations.
func TestApplyGNSSHeuristics_ConcurrentCallsAreRaceFree(t *testing.T) {
	resetGNSSPositionValidationState()
	t.Cleanup(resetGNSSPositionValidationState)

	trusted := gnssPositionValidation{QualityIndicator: 1, HDOP: 0.9, Status: "trusted", Trusted: true}
	base := time.Date(2026, 6, 11, 0, 0, 0, 0, time.UTC)

	var wg sync.WaitGroup
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			now := base.Add(time.Duration(n) * time.Millisecond)
			sample := gnssObservedSample{
				HasPosition:   true,
				Latitude:      -25.2939,
				Longitude:     152.9103,
				Navigation:    "anchored",
				ObservedAt:    now,
				HasObservedAt: true,
			}
			applyGNSSHeuristics(trusted, sample, now)
		}(i)
	}
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			criticalGNSSValidation("concurrent probe", base.Add(time.Duration(n)*time.Millisecond))
		}(i)
	}
	wg.Wait()
}

// A tick with no position (or half of one) between two real fixes must not
// become the "last sample" or the held fix: the next real fix at the same
// place is no jump, and the fix held through the gap is the original.
func TestVesselStateHoldsTheTrustedFixThroughATickWithNoPosition(t *testing.T) {
	resetGNSSPositionValidationState()
	t.Cleanup(resetGNSSPositionValidationState)

	now := time.Now().UTC()
	tick := func(datetime time.Time, position map[string]any) vesselStateData {
		navigation := map[string]any{
			"state":    map[string]any{"value": "anchored"},
			"datetime": map[string]any{"value": datetime.Format(time.RFC3339Nano)},
			"gnss": map[string]any{
				"methodQuality":      map[string]any{"value": "GNSS Fix"},
				"horizontalDilution": map[string]any{"value": 0.9},
			},
		}
		if position != nil {
			navigation["position"] = map[string]any{"value": position}
		}
		view := telemetryViewFromTree(map[string]any{"navigation": navigation}, now)
		return vesselStateFromTelemetry(view, vesselStateData{Status: "Unknown", Datetime: now, Depth: -1, Latitude: -1, Longitude: -1}, vesselView{})
	}
	fix := map[string]any{"latitude": -19.25, "longitude": 146.82}

	first := tick(now.Add(-500*time.Millisecond), fix)
	if first.GNSSCriticalAlert || first.Latitude != -19.25 || first.Longitude != 146.82 {
		t.Fatalf("first fix: expected a trusted fix at the boat, got %+v", first)
	}

	at := now.Add(-500 * time.Millisecond)
	for _, gapCase := range []struct {
		name     string
		position map[string]any
	}{{"no position", nil}, {"half a position", map[string]any{"latitude": -19.25}}} {
		name := gapCase.name
		at = at.Add(100 * time.Millisecond)
		gap := tick(at, gapCase.position)
		if gap.Latitude != -19.25 || gap.Longitude != 146.82 {
			t.Fatalf("%s: expected the trusted fix held, got (%v, %v)", name, gap.Latitude, gap.Longitude)
		}

		at = at.Add(100 * time.Millisecond)
		again := tick(at, fix)
		if again.GNSSCriticalAlert || strings.Contains(again.GNSSValidationReason, "jump") {
			t.Fatalf("%s: the next fix at the same place must not read as a jump, got %q (critical=%v)", name, again.GNSSValidationReason, again.GNSSCriticalAlert)
		}
		if again.Latitude != -19.25 || again.Longitude != 146.82 {
			t.Fatalf("%s: expected the fix at the boat, got (%v, %v)", name, again.Latitude, again.Longitude)
		}
	}
}

// navigation.gnss.satellites arrives three ways: wrapped in a leaf's value,
// or bare (a REST-shaped tree) as an array or a map of satellite objects.
func TestParseGNSSSatellitesSNRReadsWrappedAndBareForms(t *testing.T) {
	sats := []any{map[string]any{"snr": 31.0}, map[string]any{"SNR": 27.0}, map[string]any{"snr": 0.0}}
	satMap := map[string]any{"1": map[string]any{"snr": 31.0}, "2": map[string]any{"SNR": 27.0}}
	for name, satellites := range map[string]any{
		"wrapped array": map[string]any{"value": sats},
		"bare array":    sats,
		"bare map":      satMap,
	} {
		tree := map[string]any{"navigation": map[string]any{"gnss": map[string]any{"satellites": satellites}}}
		snrs := parseGNSSSatellitesSNR(telemetryViewFromTree(tree, time.Time{}))
		sort.Float64s(snrs)
		if len(snrs) != 2 || snrs[0] != 27 || snrs[1] != 31 {
			t.Errorf("%s: got %v, want [27 31]", name, snrs)
		}
	}
}
