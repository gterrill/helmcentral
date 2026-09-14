package main

import (
	"encoding/json"
	"testing"
)

// TestBandAge_Boundaries pins down the band edges bandAge's doc comment
// describes, in terms of what an operator would actually see change:
// STALE_AFTER_SECONDS (frontend/src/lib/staleness.ts) at 120, then whatever
// formatDataAge would newly display above that.
func TestBandAge_Boundaries(t *testing.T) {
	cases := []struct {
		name string
		age  float64
		want float64
	}{
		{"unknown sentinel stays exact", -1, -1},
		{"zero is fresh", 0, 0},
		{"just under the fresh ceiling", 119.9, 0},
		{"exactly the fresh ceiling stays fresh", 120, 0},
		{"just over the ceiling bands to its own minute", 120.1, 120},
		{"same minute as just-over stays banded together", 179, 120},
		{"next minute bands separately", 180, 180},
		{"an hour-ish, still whole minutes", 3599, 3540},
		{"just under a day, still whole minutes", 86399, 86340},
		{"exactly a day bands to whole days", 86400, 86400},
		{"two days", 172800, 172800},
		{"partway through the third day floors down", 200000, 172800},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := bandAge(tc.age); got != tc.want {
				t.Fatalf("bandAge(%v) = %v, want %v", tc.age, got, tc.want)
			}
		})
	}
}

// vesselStatePayload builds a literal matching buildVesselStatePayload's real
// field names (main.go), so these tests exercise the same key names
// vesselStateGateKey normalises in production rather than a stand-in shape
// that could quietly drift from it.
func vesselStatePayload(datetime string, depth, depthAge, positionAge, windAge float64) map[string]any {
	return map[string]any{
		"name":                       "Piko Rua",
		"datetime":                   datetime,
		"depth":                      depth,
		"depth_last_update_age_s":    depthAge,
		"latitude":                   -36.8,
		"longitude":                  174.7,
		"position_last_update_age_s": positionAge,
		"wind_speed_apparent_kts":    12.3,
		"wind_last_update_age_s":     windAge,
		"max_gust_kts":               map[string]any{"10m": 14.0, "1h": 18.0},
		"source":                     "signalk",
	}
}

func gateKeyOf(t *testing.T, gate func([]byte) string, payload map[string]any) string {
	t.Helper()
	encoded, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("marshalling test payload: %v", err)
	}
	return gate(encoded)
}

// TestVesselStateGateKey_SkipsDatetimeAndFreshAgeChurn is the vessel-state
// half of the audit's core complaint: on the boat, datetime and the three
// 0.1s-resolution ages changed on literally every frame, so the old
// whole-payload-string gate never skipped a broadcast. Two builds differing
// only in those fields, all still within the fresh band, must gate key
// identically.
func TestVesselStateGateKey_SkipsDatetimeAndFreshAgeChurn(t *testing.T) {
	a := vesselStatePayload("2026-09-15T08:00:00Z", 4.2, 1.0, 2.0, 0.5)
	b := vesselStatePayload("2026-09-15T08:00:01Z", 4.2, 45.0, 90.0, 118.0)

	keyA := gateKeyOf(t, vesselStateGateKey, a)
	keyB := gateKeyOf(t, vesselStateGateKey, b)
	if keyA != keyB {
		t.Fatalf("expected identical gate keys for datetime/fresh-age-only differences, got %q vs %q", keyA, keyB)
	}
}

// TestVesselStateGateKey_SendsOnRealValueChange proves the gate does not
// over-suppress: a genuine instrument change (depth) must still change the
// key, and depth itself must never be rounded away.
func TestVesselStateGateKey_SendsOnRealValueChange(t *testing.T) {
	a := vesselStatePayload("2026-09-15T08:00:00Z", 4.2, 1.0, 2.0, 0.5)
	b := vesselStatePayload("2026-09-15T08:00:01Z", 4.3, 1.1, 2.1, 0.6)

	keyA := gateKeyOf(t, vesselStateGateKey, a)
	keyB := gateKeyOf(t, vesselStateGateKey, b)
	if keyA == keyB {
		t.Fatalf("expected different gate keys once depth actually changed, got identical %q", keyA)
	}
}

// TestVesselStateGateKey_SendsWhenAgeCrossesStaleThreshold guards the case
// the design specifically calls out: an age sitting at "3s" is invisible to
// the operator, but the moment it crosses STALE_AFTER_SECONDS the UI starts
// showing it, so that crossing must still change the key even though it
// looks like "just an age changing" the same way the fresh churn above does
// not.
func TestVesselStateGateKey_SendsWhenAgeCrossesStaleThreshold(t *testing.T) {
	a := vesselStatePayload("2026-09-15T08:00:00Z", 4.2, 119.0, 2.0, 0.5)
	b := vesselStatePayload("2026-09-15T08:00:01Z", 4.2, 121.0, 2.0, 0.5)

	keyA := gateKeyOf(t, vesselStateGateKey, a)
	keyB := gateKeyOf(t, vesselStateGateKey, b)
	if keyA == keyB {
		t.Fatalf("expected different gate keys once depth's age crossed the stale threshold, got identical %q", keyA)
	}
}

// TestVesselStateGateKey_SendsWhenStaleAgeCrossesAMinuteBoundary covers the
// stale side: formatDataAge's minute label ticks over even while already
// stale, and that tick must still reach the client.
func TestVesselStateGateKey_SendsWhenStaleAgeCrossesAMinuteBoundary(t *testing.T) {
	a := vesselStatePayload("2026-09-15T08:00:00Z", 4.2, 179.0, 2.0, 0.5)
	b := vesselStatePayload("2026-09-15T08:00:01Z", 4.2, 180.0, 2.0, 0.5)

	keyA := gateKeyOf(t, vesselStateGateKey, a)
	keyB := gateKeyOf(t, vesselStateGateKey, b)
	if keyA == keyB {
		t.Fatalf("expected different gate keys once a stale age crossed a minute boundary (179s -> 180s), got identical %q", keyA)
	}

	// And within the same minute, no such churn: 175s and 179s both display
	// as "2m" (formatDataAge), so they must gate key identically.
	c := vesselStatePayload("2026-09-15T08:00:02Z", 4.2, 175.0, 2.0, 0.5)
	keyC := gateKeyOf(t, vesselStateGateKey, c)
	if keyA != keyC {
		t.Fatalf("expected identical gate keys within the same stale minute band, got %q vs %q", keyA, keyC)
	}
}

// gaugeValuesPayload mirrors buildGaugeValuesPayload's actual shape
// (signalk_paths.go): a "values" map alongside an "ages" map keyed by the
// same SignalK paths.
func gaugeValuesPayload(voltage float64, voltageAge float64) map[string]any {
	return map[string]any{
		"values": map[string]any{
			"electrical.batteries.0.voltage": voltage,
			"tanks.fuel.0.currentLevel":      0.62,
		},
		"ages": map[string]float64{
			"electrical.batteries.0.voltage": voltageAge,
			"tanks.fuel.0.currentLevel":      30.0,
		},
	}
}

// TestGaugeValuesGateKey_SkipsFreshAgeChurnAcrossTheAgesMap proves the
// "ages" map special case: gauge-values carries no top-level datetime at
// all, so every bit of its churn on the boat was this map's per-path ages
// ticking by 0.1s each build (55 KB/min measured). Two builds whose ages
// differ only within the fresh band, with values unchanged, must gate key
// identically.
func TestGaugeValuesGateKey_SkipsFreshAgeChurnAcrossTheAgesMap(t *testing.T) {
	a := gaugeValuesPayload(12.6, 1.0)
	b := gaugeValuesPayload(12.6, 90.0)

	keyA := gateKeyOf(t, gaugeValuesGateKey, a)
	keyB := gateKeyOf(t, gaugeValuesGateKey, b)
	if keyA != keyB {
		t.Fatalf("expected identical gate keys for fresh-age-only churn in the ages map, got %q vs %q", keyA, keyB)
	}
}

// TestGaugeValuesGateKey_SendsOnRealValueChange is the value-side guard: a
// gauge's actual reading is never rounded, so any real change must still
// change the key.
func TestGaugeValuesGateKey_SendsOnRealValueChange(t *testing.T) {
	a := gaugeValuesPayload(12.6, 1.0)
	b := gaugeValuesPayload(12.7, 1.0)

	keyA := gateKeyOf(t, gaugeValuesGateKey, a)
	keyB := gateKeyOf(t, gaugeValuesGateKey, b)
	if keyA == keyB {
		t.Fatalf("expected different gate keys once a gauge value actually changed, got identical %q", keyA)
	}
}

// nearbyVesselsPayload mirrors buildNearbyVesselsPayload's shape (main.go):
// a feed-level last_update_age_s alongside a "vessels" array whose entries
// each carry their own age_seconds.
func nearbyVesselsPayload(datetime string, feedAge float64, vesselAge int) map[string]any {
	return map[string]any{
		"datetime": datetime,
		"source":   "signalk",
		"vessels": []map[string]any{
			{"id": "urn:mrn:imo:mmsi:512345", "name": "Contact", "age_seconds": vesselAge, "range_m": 900.0},
		},
		"last_update_age_s": feedAge,
	}
}

// TestNearbyVesselsGateKey_LeavesPerVesselAgeSecondsExact is the case the
// design calls out by name: nearby-vessels-tile.tsx renders each row's
// age_seconds as "12s ago", so — unlike radar's identically-named field —
// it must never be banded away, even though it is well within what would
// otherwise be the fresh band.
func TestNearbyVesselsGateKey_LeavesPerVesselAgeSecondsExact(t *testing.T) {
	a := nearbyVesselsPayload("2026-09-15T08:00:00Z", 5.0, 12)
	b := nearbyVesselsPayload("2026-09-15T08:00:05Z", 5.0, 17)

	keyA := gateKeyOf(t, nearbyVesselsGateKey, a)
	keyB := gateKeyOf(t, nearbyVesselsGateKey, b)
	if keyA == keyB {
		t.Fatalf("expected different gate keys when a displayed per-vessel age_seconds changed, got identical %q", keyA)
	}
}

// TestNearbyVesselsGateKey_SkipsDatetimeAndFreshFeedAgeChurn checks the
// feed-level field, which is not displayed per row and so should behave like
// vessel-state's ages: fresh-band churn alone must not defeat the gate.
func TestNearbyVesselsGateKey_SkipsDatetimeAndFreshFeedAgeChurn(t *testing.T) {
	a := nearbyVesselsPayload("2026-09-15T08:00:00Z", 1.0, 12)
	b := nearbyVesselsPayload("2026-09-15T08:00:05Z", 90.0, 12)

	keyA := gateKeyOf(t, nearbyVesselsGateKey, a)
	keyB := gateKeyOf(t, nearbyVesselsGateKey, b)
	if keyA != keyB {
		t.Fatalf("expected identical gate keys for datetime/fresh-feed-age-only differences, got %q vs %q", keyA, keyB)
	}
}

// radarTargetsPayload mirrors buildRadarTargetsPayload's shape
// (radar_source.go): a "targets" array whose entries carry their own
// age_seconds, confirmed by grep to be parsed but never rendered by the
// frontend.
func radarTargetsPayload(datetime string, bearingRad float64, targetAge int) map[string]any {
	return map[string]any{
		"datetime": datetime,
		"source":   "mayara",
		"radars":   []map[string]any{{"id": "radar-0", "name": "Radar", "transmitting": true}},
		"targets": []map[string]any{
			{"id": "radar-0:1", "bearing_rad": bearingRad, "range_m": 500.0, "age_seconds": targetAge},
		},
	}
}

// TestRadarTargetsGateKey_BandsPerTargetAgeSeconds is the counterpart to the
// nearby-vessels test above: since the frontend never renders radar's
// age_seconds, it should band like any other age, not stay exact.
func TestRadarTargetsGateKey_BandsPerTargetAgeSeconds(t *testing.T) {
	a := radarTargetsPayload("2026-09-15T08:00:00Z", 1.2, 1)
	b := radarTargetsPayload("2026-09-15T08:00:02Z", 1.2, 45)

	keyA := gateKeyOf(t, radarTargetsGateKey, a)
	keyB := gateKeyOf(t, radarTargetsGateKey, b)
	if keyA != keyB {
		t.Fatalf("expected identical gate keys for datetime/fresh-target-age-only differences, got %q vs %q", keyA, keyB)
	}

	c := radarTargetsPayload("2026-09-15T08:00:02Z", 1.4, 1)
	keyC := gateKeyOf(t, radarTargetsGateKey, c)
	if keyA == keyC {
		t.Fatalf("expected different gate keys once bearing_rad actually changed, got identical %q", keyA)
	}
}

// tanksStatePayload mirrors buildTanksStatePayload's shape (main.go): a
// feed-level last_update_age_s, fuel_volume_age_s and fuel_derived_age_s,
// plus per-tank last_update_age_s nested in the "tanks" array — the nested
// case the generic "_age_s" suffix rule (not a special case) must catch.
func tanksStatePayload(datetime string, tankLevel, tankAge, feedAge, fuelVolAge, fuelDerivedAge float64) map[string]any {
	return map[string]any{
		"datetime": datetime,
		"source":   "signalk",
		"tanks": []map[string]any{
			{"id": "fuel.0", "label": "Fuel", "level_percent": tankLevel, "last_update_age_s": tankAge},
		},
		"last_update_age_s":    feedAge,
		"fuel_volume_m3":       0.4,
		"fuel_volume_age_s":    fuelVolAge,
		"fuel_time_to_empty_s": 36000.0,
		"fuel_range_m":         185000.0,
		"fuel_derived_age_s":   fuelDerivedAge,
	}
}

// TestTanksStateGateKey_BandsNestedPerTankAge is the nested case: a tank's
// own age lives inside the "tanks" array, not at the top level, so this only
// passes if normalizeTelemetryAges actually recurses rather than only
// looking at the payload's top-level keys.
func TestTanksStateGateKey_BandsNestedPerTankAge(t *testing.T) {
	a := tanksStatePayload("2026-09-15T08:00:00Z", 61.0, 1.0, 1.0, 1.0, 1.0)
	b := tanksStatePayload("2026-09-15T08:00:10Z", 61.0, 90.0, 90.0, 90.0, 90.0)

	keyA := gateKeyOf(t, tanksStateGateKey, a)
	keyB := gateKeyOf(t, tanksStateGateKey, b)
	if keyA != keyB {
		t.Fatalf("expected identical gate keys for datetime/fresh-age-only differences (including the nested per-tank age), got %q vs %q", keyA, keyB)
	}

	c := tanksStatePayload("2026-09-15T08:00:10Z", 60.5, 1.0, 1.0, 1.0, 1.0)
	keyC := gateKeyOf(t, tanksStateGateKey, c)
	if keyA == keyC {
		t.Fatalf("expected different gate keys once level_percent actually changed, got identical %q", keyA)
	}
}

// solarStatePayload mirrors buildSolarStatePayload's shape (main.go): a
// feed-level last_update_age_s, a "controllers" array with its own nested
// last_update_age_s per controller, and trend_24h_total, whose last point's
// time solarStateGateKey drops entirely (see its doc comment).
func solarStatePayload(datetime string, currentW, feedAge, controllerAge float64, lastTrendTime string) map[string]any {
	return map[string]any{
		"datetime":          datetime,
		"source":            "signalk",
		"current_w":         currentW,
		"today_kwh":         4.2,
		"peak_today_w":      620.0,
		"last_update_age_s": feedAge,
		"controllers": []map[string]any{
			{"id": "0", "label": "MPPT 1", "current_w": currentW, "last_update_age_s": controllerAge},
		},
		"trend_24h_total": []map[string]any{
			{"time": "2026-09-15T07:45:00Z", "total_w": 400.0},
			{"time": lastTrendTime, "total_w": 410.0},
		},
	}
}

// TestSolarStateGateKey_DropsTrendTotalEntirely checks the field the audit
// singled out: the last bucket's time changes on every Influx refresh
// regardless of whether solar output actually changed, and the frontend
// never reads trend_24h_total at all (confirmed by grep), so it must not
// affect the gate key even when nothing else about the payload differs.
func TestSolarStateGateKey_DropsTrendTotalEntirely(t *testing.T) {
	a := solarStatePayload("2026-09-15T08:00:00Z", 300.0, 1.0, 1.0, "2026-09-15T08:00:00Z")
	b := solarStatePayload("2026-09-15T08:00:10Z", 300.0, 90.0, 90.0, "2026-09-15T08:00:10Z")

	keyA := gateKeyOf(t, solarStateGateKey, a)
	keyB := gateKeyOf(t, solarStateGateKey, b)
	if keyA != keyB {
		t.Fatalf("expected identical gate keys for datetime/age/trend-time-only differences, got %q vs %q", keyA, keyB)
	}
}

// TestSolarStateGateKey_SendsOnRealPowerChange guards against
// TestSolarStateGateKey_DropsTrendTotalEntirely accidentally passing because
// the key function drops everything: a real change to current_w must still
// change the key.
func TestSolarStateGateKey_SendsOnRealPowerChange(t *testing.T) {
	a := solarStatePayload("2026-09-15T08:00:00Z", 300.0, 1.0, 1.0, "2026-09-15T08:00:00Z")
	b := solarStatePayload("2026-09-15T08:00:10Z", 305.0, 1.0, 1.0, "2026-09-15T08:00:00Z")

	keyA := gateKeyOf(t, solarStateGateKey, a)
	keyB := gateKeyOf(t, solarStateGateKey, b)
	if keyA == keyB {
		t.Fatalf("expected different gate keys once current_w actually changed, got identical %q", keyA)
	}
}

// electricalStatePayload mirrors buildElectricalStatePayload's shape
// (main.go).
func electricalStatePayload(datetime string, socPercent, feedAge float64) map[string]any {
	return map[string]any{
		"datetime":            datetime,
		"last_update_age_s":   feedAge,
		"battery_soc_percent": socPercent,
		"charging_power_w":    120.0,
		"source":              "signalk",
	}
}

// TestElectricalStateGateKey_SkipsDatetimeAndFreshAgeChurn is the same
// datetime/fresh-age pattern as vessel-state, for the payload the audit
// separately named.
func TestElectricalStateGateKey_SkipsDatetimeAndFreshAgeChurn(t *testing.T) {
	a := electricalStatePayload("2026-09-15T08:00:00Z", 87.0, 1.0)
	b := electricalStatePayload("2026-09-15T08:00:05Z", 87.0, 90.0)

	keyA := gateKeyOf(t, electricalStateGateKey, a)
	keyB := gateKeyOf(t, electricalStateGateKey, b)
	if keyA != keyB {
		t.Fatalf("expected identical gate keys for datetime/fresh-age-only differences, got %q vs %q", keyA, keyB)
	}

	c := electricalStatePayload("2026-09-15T08:00:05Z", 86.5, 90.0)
	keyC := gateKeyOf(t, electricalStateGateKey, c)
	if keyA == keyC {
		t.Fatalf("expected different gate keys once battery_soc_percent actually changed, got identical %q", keyA)
	}
}
