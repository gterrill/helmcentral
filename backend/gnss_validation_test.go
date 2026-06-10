package main

import "testing"

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
