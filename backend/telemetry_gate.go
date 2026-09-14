package main

import (
	"encoding/json"
	"fmt"
	"log"
	"math"
	"strings"
	"time"
)

/*
backend-perf-audit.md Tier 1 #4: on the boat every /api/stream frame differed
from the last, so telemetryHub's change gate (vessel_state_stream.go) never
skipped a broadcast. The culprits are all volatile-but-uninteresting fields —
a `datetime` restamped with the build time, and ages that tick by 0.1s every
build — sitting alongside the real telemetry in the same payload.

The frontend must not change (frontend/src/lib/staleness.ts deliberately
computes ages against the vessel clock, not the browser's, because the wall
kiosk's own clock cannot be trusted), so every sent payload still carries a
real `datetime` and real ages. What changes here is only the *comparison*
the hub's gate makes: each gated event normalises a copy of its payload —
dropping repeated clocks and banding ages into the coarser steps the UI
actually shows — and gates on that, while still broadcasting the real,
un-normalised payload whenever the normalised key changes.
*/

// telemetryFreshAfterSeconds mirrors STALE_AFTER_SECONDS in
// frontend/src/lib/staleness.ts. Below it, isStale reports false and the UI
// never renders the age at all, so a fresh age ticking from 1s to 119s is
// invisible to the operator; letting that tick alone defeat the change gate
// would mean resending an unchanged screen just because a number nobody sees
// moved. Every fresh age therefore bands to one value, and a value crossing
// this threshold still forces a send, because that crossing is exactly when
// the UI starts showing something.
const telemetryFreshAfterSeconds = 120

// telemetryDaySeconds is a day, for banding a stale age down to the whole
// days formatDataAge (frontend/src/lib/staleness.ts) displays once an age
// has been stale for that long.
const telemetryDaySeconds = 24 * 60 * 60

// bandAge collapses ageSeconds into the coarsest value that still changes
// exactly when formatDataAge's displayed label would change:
//
//   - A negative sentinel ("no timestamp at all", ADR 0068) passes through
//     unchanged — it is not a duration to band, it is a distinct absence.
//   - 0 to telemetryFreshAfterSeconds inclusive bands to a single "fresh"
//     value: isStale is false throughout this range, so the UI shows no age
//     text at all and every value in it is indistinguishable to the operator.
//   - Above that and under a day, it bands to the whole minute — formatDataAge
//     shows minutes (bare, or as the minutes part of "Xh Ym") throughout this
//     range, ticking once a minute.
//   - A day or more bands to the whole day, matching formatDataAge's "Nd".
func bandAge(ageSeconds float64) float64 {
	if ageSeconds < 0 {
		return ageSeconds
	}
	if ageSeconds <= telemetryFreshAfterSeconds {
		return 0
	}
	if ageSeconds < telemetryDaySeconds {
		return math.Floor(ageSeconds/60) * 60
	}
	return math.Floor(ageSeconds/telemetryDaySeconds) * telemetryDaySeconds
}

// telemetryGateKeyFor decodes encoded (an event's already-marshalled real
// payload) back into a generic JSON tree, normalises it, and re-encodes that
// as the gate's comparison key. It never mutates or re-sends encoded itself —
// buildAndBroadcast keeps that as the real payload regardless of what this
// returns.
//
// bandAgeSeconds controls whether a bare "age_seconds" key (as opposed to
// one already caught by the "_age_s" suffix rule below) gets banded: radar
// targets' per-target age_seconds is parsed by the frontend but never
// rendered (confirmed by grep across frontend/src), so it bands like any
// other age; nearby-vessels' per-vessel age_seconds is rendered on every row
// ("12s ago", nearby-vessels-tile.tsx) and must stay exact, so that event
// passes false.
//
// extraDropKeys names additional top-level fields to drop beyond the
// "datetime" every gated event drops automatically — used by solar-state for
// trend_24h_total, whose last bucket's time is not a value the frontend
// reads at all (see solarStateGateKey).
func telemetryGateKeyFor(encoded []byte, bandAgeSeconds bool, extraDropKeys ...string) string {
	var generic any
	if err := json.Unmarshal(encoded, &generic); err != nil {
		// A payload the hub cannot even decode back for comparison is not
		// one to silently treat as unchanged: that would hide a real change
		// behind a broken comparison (AGENTS.md fallback policy). Failing
		// toward "always send" is the safe direction — it can cost an extra
		// frame, never a missed one.
		log.Printf("telemetry hub: gate key decode failed, forcing a send: %v", err)
		return fmt.Sprintf("gate-key-error-%d", time.Now().UnixNano())
	}

	drop := map[string]bool{"datetime": true}
	for _, key := range extraDropKeys {
		drop[key] = true
	}

	normalized := normalizeTelemetryAges(generic, drop, bandAgeSeconds)
	out, err := json.Marshal(normalized)
	if err != nil {
		log.Printf("telemetry hub: gate key encode failed, forcing a send: %v", err)
		return fmt.Sprintf("gate-key-error-%d", time.Now().UnixNano())
	}
	return string(out)
}

// normalizeTelemetryAges walks the full JSON tree (not just top-level keys —
// solar's per-controller and tanks' per-tank ages sit inside a nested array)
// dropping every key in drop and banding every age field via bandAge:
//
//   - any map key ending in "_age_s" (last_update_age_s,
//     depth_last_update_age_s, fuel_volume_age_s, and so on, at any depth),
//   - every value of a map keyed "ages" (gauge-values' per-path age map,
//     whose keys are SignalK paths, not field names ending in "_age_s"),
//   - "age_seconds", but only when bandAgeSeconds says to (see
//     telemetryGateKeyFor's doc comment).
//
// Live instrument values (position, wind, depth, gust, and so on) are never
// touched here: they pass through byte-for-byte, so a real change to any of
// them still changes the key.
func normalizeTelemetryAges(node any, drop map[string]bool, bandAgeSeconds bool) any {
	switch v := node.(type) {
	case map[string]any:
		result := make(map[string]any, len(v))
		for key, value := range v {
			if drop[key] {
				continue
			}
			if key == "ages" {
				if ages, ok := value.(map[string]any); ok {
					result[key] = bandAgeMap(ages)
					continue
				}
			}
			if strings.HasSuffix(key, "_age_s") {
				if age, ok := value.(float64); ok {
					result[key] = bandAge(age)
					continue
				}
			}
			if key == "age_seconds" && bandAgeSeconds {
				if age, ok := value.(float64); ok {
					result[key] = bandAge(age)
					continue
				}
			}
			result[key] = normalizeTelemetryAges(value, drop, bandAgeSeconds)
		}
		return result
	case []any:
		result := make([]any, len(v))
		for i, item := range v {
			result[i] = normalizeTelemetryAges(item, drop, bandAgeSeconds)
		}
		return result
	default:
		return v
	}
}

// bandAgeMap bands every value of a gauge-values "ages" map. Its keys are
// arbitrary SignalK paths, not field names, so they can never match the
// "_age_s" suffix rule above — this is the special case that catches them.
func bandAgeMap(ages map[string]any) map[string]any {
	result := make(map[string]any, len(ages))
	for path, value := range ages {
		if age, ok := value.(float64); ok {
			result[path] = bandAge(age)
			continue
		}
		result[path] = value
	}
	return result
}

// The gate key functions below are what telemetryEmitters() (vessel_state_
// stream.go) wires to each gated event. Every one of them defers to
// telemetryGateKeyFor with the drop/band choices that event's own payload
// shape needs; see backend-perf-audit.md Tier 1 #4 for which fields on which
// events were measured changing every frame.

// vesselStateGateKey covers main.go's buildVesselStatePayload: datetime, plus
// depth/position/wind's own last_update_age_s fields.
func vesselStateGateKey(encoded []byte) string {
	return telemetryGateKeyFor(encoded, false)
}

// gaugeValuesGateKey covers signalk_paths.go's buildGaugeValuesPayload: no
// datetime field at all, but every bound path's age in the "ages" map, which
// the special case in normalizeTelemetryAges catches by the map's own key
// rather than by suffix.
func gaugeValuesGateKey(encoded []byte) string {
	return telemetryGateKeyFor(encoded, false)
}

// electricalStateGateKey covers main.go's buildElectricalStatePayload:
// datetime and the feed-level last_update_age_s.
func electricalStateGateKey(encoded []byte) string {
	return telemetryGateKeyFor(encoded, false)
}

// tanksStateGateKey covers main.go's buildTanksStatePayload: datetime, the
// feed-level last_update_age_s, fuel_volume_age_s and fuel_derived_age_s, and
// — via the generic "_age_s" suffix rule, not a special case here — each
// tank's own last_update_age_s nested inside the "tanks" array.
func tanksStateGateKey(encoded []byte) string {
	return telemetryGateKeyFor(encoded, false)
}

// nearbyVesselsGateKey covers main.go's buildNearbyVesselsPayload: datetime
// and the feed-level last_update_age_s. bandAgeSeconds is false because each
// vessel's own age_seconds is displayed on every row ("12s ago",
// nearby-vessels-tile.tsx) and must stay exact so a row that actually ticks
// over still sends.
func nearbyVesselsGateKey(encoded []byte) string {
	return telemetryGateKeyFor(encoded, false)
}

// radarTargetsGateKey covers radar_source.go's buildRadarTargetsPayload:
// datetime, and — unlike nearby-vessels — each target's own age_seconds,
// which the frontend parses (use-radar-targets.ts) but never renders
// (confirmed by grep across frontend/src's radar-targets-tile.tsx), so
// bandAgeSeconds is true here.
func radarTargetsGateKey(encoded []byte) string {
	return telemetryGateKeyFor(encoded, true)
}

// solarStateGateKey covers main.go's buildSolarStatePayload: datetime, the
// feed-level and per-controller last_update_age_s, and trend_24h_total as a
// whole.
//
// trend_24h_total's own volatility is not an age field bandAge helps with:
// its last (still-filling) bucket's "time" gets re-stamped close to the
// query's own `now` on every Influx refresh (queryInfluxSolarTrend24h's
// aggregateWindow, influx.go), because that partial window's timestamp is
// derived from the query's stop bound rather than from when the bucket
// actually started. grep across frontend/src turns up no reference to
// trend_24h_total or any camelCase form of it at all — nothing renders it —
// so rather than teach the gate key about Influx's windowing behaviour, this
// drops the whole field, the same way "datetime" is dropped.
func solarStateGateKey(encoded []byte) string {
	return telemetryGateKeyFor(encoded, false, "trend_24h_total")
}
