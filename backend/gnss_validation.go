package main

import (
	"fmt"
	"log"
	"math"
	"strings"
	"sync"
	"time"
)

const (
	gnssTrustedHDOPThreshold     = 2.5
	gnssCriticalHDOPThreshold    = 4.0
	gnssDegradedMaxAge           = 10 * time.Second
	gnssCriticalMaxAge           = 30 * time.Second
	gnssDepthJumpMeters          = 1.5
	gnssDepthJumpWindow          = 10 * time.Second
	gnssDegradedJumpKts          = 6.0
	gnssCriticalJumpKts          = 12.0
	gnssRecoverySamples          = 5
	gnssRecoveryMinDuration      = 15 * time.Second
	gnssSpoofingUniformSNRStdDev = 1.5
	gnssSpoofingUniformSNRAvg    = 30.0
	gnssJammingMaxSNRThreshold   = 20.0
)

type gnssPositionValidation struct {
	QualityIndicator int
	HDOP             float64
	Satellites       int
	Status           string
	Reason           string
	Trusted          bool
	Critical         bool
}

type gnssObservedSample struct {
	// HasPosition is false when the tick carried no fix (Latitude and
	// Longitude are then meaningless): such a tick is never compared for a
	// jump and never becomes the last sample.
	HasPosition    bool
	Latitude       float64
	Longitude      float64
	DepthMeters    float64
	Navigation     string
	EnginesRunning bool
	ObservedAt     time.Time
	HasObservedAt  bool
}

type gnssHeuristicState struct {
	lastSample      *gnssObservedSample
	criticalLatched bool
	criticalSince   time.Time
	recoveryCount   int
}

var gnssValidationMu sync.Mutex
var lastGNSSValidationStatus string
var lastGNSSValidationReason string
var lastTrustedGNSSLatitude float64
var lastTrustedGNSSLongitude float64
var lastTrustedGNSSPositionValid bool
var gnssHeuristic gnssHeuristicState

func parseGNSSPositionValidation(t telemetryReader) gnssPositionValidation {
	qualityIndicator := parseGNSSQualityIndicator(t)
	hdop := parseGNSSHDOP(t)
	snrs := parseGNSSSatellitesSNR(t)
	status, reason := classifyGNSSPositionValidation(qualityIndicator, hdop, snrs)
	return gnssPositionValidation{
		QualityIndicator: qualityIndicator,
		HDOP:             hdop,
		Satellites:       gnssSatelliteCount(t),
		Status:           status,
		Reason:           reason,
		Trusted:          status == "trusted",
		Critical:         status == "critical",
	}
}

// criticalGNSSValidation builds a critical validation result for situations
// where there's no SignalK payload to run the normal quality/HDOP/heuristic
// checks against at all — e.g. the connection to SignalK itself failed, or it
// isn't configured. It explicitly engages the same recovery-hysteresis latch
// applyGNSSRecoveryHysteresis uses for a bad-fix critical event, so a
// reconnect still needs a stable run of trusted samples before the alarm
// clears, rather than clearing instantly on the first good packet back.
func criticalGNSSValidation(reason string, now time.Time) gnssPositionValidation {
	gnssValidationMu.Lock()
	gnssHeuristic.criticalLatched = true
	gnssHeuristic.criticalSince = now
	gnssHeuristic.recoveryCount = 0
	gnssValidationMu.Unlock()

	return gnssPositionValidation{
		QualityIndicator: -1,
		HDOP:             -1,
		Satellites:       -1,
		Status:           "critical",
		Reason:           reason,
		Trusted:          false,
		Critical:         true,
	}
}

// applyGNSSHeuristics reads and mutates the package-level gnssHeuristic
// state (lastSample plus the recovery-hysteresis counters), so it takes
// gnssValidationMu for its whole body. This is safe against
// resolveGNSSPosition/resetGNSSPositionValidationState's own use of the same
// mutex because fetchSignalKVesselState and criticalVesselState (signalk.go)
// always call this function and resolveGNSSPosition one after another, never
// with one nested inside the other -- nesting would deadlock, since Go's
// sync.Mutex is not reentrant.
func applyGNSSHeuristics(validation gnssPositionValidation, sample gnssObservedSample, now time.Time) gnssPositionValidation {
	if now.IsZero() {
		now = time.Now().UTC()
	}

	gnssValidationMu.Lock()
	defer gnssValidationMu.Unlock()

	if !sample.HasObservedAt {
		validation = escalateValidation(validation, "critical", "gnss timestamp unavailable")
	} else {
		age := now.Sub(sample.ObservedAt)
		if age > gnssCriticalMaxAge {
			validation = escalateValidation(validation, "critical", fmt.Sprintf("gnss data stale (%ds)", int(age.Seconds())))
		} else if age > gnssDegradedMaxAge {
			validation = escalateValidation(validation, "degraded", fmt.Sprintf("gnss data aging (%ds)", int(age.Seconds())))
		}
	}

	if sample.HasPosition && gnssHeuristic.lastSample != nil && sample.HasObservedAt && gnssHeuristic.lastSample.HasObservedAt {
		dt := sample.ObservedAt.Sub(gnssHeuristic.lastSample.ObservedAt)
		if dt > 0 {
			distance := haversineMeters(
				gnssHeuristic.lastSample.Latitude,
				gnssHeuristic.lastSample.Longitude,
				sample.Latitude,
				sample.Longitude,
			)
			speedKts := (distance / dt.Seconds()) * metersPerSecondToKnots
			// Departure is plausible with a running main engine even before
			// Auto-state receives anchor-raised. Quality/age checks still apply.
			if isAnchoredOrMooredState(sample.Navigation) && !sample.EnginesRunning {
				if speedKts > gnssCriticalJumpKts {
					validation = escalateValidation(validation, "critical", fmt.Sprintf("position jump implies %.1f kts at anchor", speedKts))
				} else if speedKts > gnssDegradedJumpKts {
					validation = escalateValidation(validation, "degraded", fmt.Sprintf("position jump implies %.1f kts at anchor", speedKts))
				}

				if dt <= gnssDepthJumpWindow && sample.DepthMeters >= 0 && gnssHeuristic.lastSample.DepthMeters >= 0 {
					depthDelta := math.Abs(sample.DepthMeters - gnssHeuristic.lastSample.DepthMeters)
					if depthDelta > gnssDepthJumpMeters {
						if speedKts > gnssDegradedJumpKts {
							validation = escalateValidation(validation, "critical", fmt.Sprintf("depth jump %.1fm with implausible position jump", depthDelta))
						} else {
							validation = escalateValidation(validation, "degraded", fmt.Sprintf("depth jump %.1fm in %ds", depthDelta, int(dt.Seconds())))
						}
					}
				}
			}
		}
	}

	if sample.HasPosition && sample.Latitude >= -90 && sample.Latitude <= 90 && sample.Longitude >= -180 && sample.Longitude <= 180 {
		copySample := sample
		gnssHeuristic.lastSample = &copySample
	}

	validation = applyGNSSRecoveryHysteresis(validation, now)
	return validation
}

func escalateValidation(current gnssPositionValidation, target string, reason string) gnssPositionValidation {
	if target == "critical" {
		current.Status = "critical"
		current.Reason = reason
		current.Critical = true
		current.Trusted = false
		return current
	}
	if target == "degraded" && current.Status == "trusted" {
		current.Status = "degraded"
		current.Reason = reason
		current.Critical = false
		current.Trusted = false
	}
	return current
}

// applyGNSSRecoveryHysteresis mutates gnssHeuristic's latch/counter fields
// and must only be called with gnssValidationMu already held -- its one
// caller, applyGNSSHeuristics above, holds it for its whole body.
func applyGNSSRecoveryHysteresis(validation gnssPositionValidation, now time.Time) gnssPositionValidation {
	if validation.Status == "critical" {
		gnssHeuristic.criticalLatched = true
		gnssHeuristic.criticalSince = now
		gnssHeuristic.recoveryCount = 0
		return validation
	}

	if !gnssHeuristic.criticalLatched {
		gnssHeuristic.recoveryCount = 0
		return validation
	}

	if validation.Status == "trusted" {
		gnssHeuristic.recoveryCount++
		if gnssHeuristic.recoveryCount >= gnssRecoverySamples && now.Sub(gnssHeuristic.criticalSince) >= gnssRecoveryMinDuration {
			gnssHeuristic.criticalLatched = false
			gnssHeuristic.recoveryCount = 0
			return validation
		}
	} else {
		gnssHeuristic.recoveryCount = 0
	}

	validation.Status = "critical"
	validation.Reason = "waiting for stable GPS before clearing alarm"
	validation.Critical = true
	validation.Trusted = false
	return validation
}

func isAnchoredOrMooredState(value string) bool {
	normalized := strings.ToLower(strings.TrimSpace(value))
	normalized = strings.ReplaceAll(normalized, "_", "")
	normalized = strings.ReplaceAll(normalized, "-", "")
	normalized = strings.ReplaceAll(normalized, " ", "")
	return normalized == "anchored" || normalized == "moored" || normalized == "atanchor"
}

func classifyGNSSPositionValidation(qualityIndicator int, hdop float64, snrs []float64) (string, string) {
	switch {
	case qualityIndicator <= 0:
		return "critical", "quality indicator reports no fix"
	case hdop < 0:
		return "critical", "hdop unavailable"
	case hdop > gnssCriticalHDOPThreshold:
		return "critical", fmt.Sprintf("hdop %.1f exceeds %.1f", hdop, gnssCriticalHDOPThreshold)
	}

	if len(snrs) >= 4 {
		sum := 0.0
		maxSnr := 0.0
		for _, snr := range snrs {
			sum += snr
			if snr > maxSnr {
				maxSnr = snr
			}
		}
		avg := sum / float64(len(snrs))

		variance := 0.0
		for _, snr := range snrs {
			variance += (snr - avg) * (snr - avg)
		}
		stddev := math.Sqrt(variance / float64(len(snrs)))

		if stddev < gnssSpoofingUniformSNRStdDev && avg > gnssSpoofingUniformSNRAvg {
			return "critical", fmt.Sprintf("suspected spoofing: uniform snr (stddev %.1f, avg %.1f)", stddev, avg)
		}

		if maxSnr < gnssJammingMaxSNRThreshold {
			return "critical", fmt.Sprintf("suspected jamming: max snr only %.1f", maxSnr)
		}
	}

	switch {
	case hdop <= gnssTrustedHDOPThreshold:
		return "trusted", ""
	default:
		return "degraded", fmt.Sprintf("hdop %.1f above trusted threshold %.1f", hdop, gnssTrustedHDOPThreshold)
	}
}

// parseGNSSSatellitesSNR reads the per-satellite SNR figures this boat's
// server publishes at navigation.gnss.satellites, as an array or a map of
// satellite objects, either as a leaf's value or bare (a REST-shaped tree).
// nil when there is none, or the path holds a plain count.
func parseGNSSSatellitesSNR(t telemetryReader) []float64 {
	const path = "navigation.gnss.satellites"

	var snrs []float64
	raw, ok := t.Value(path)
	if !ok {
		// A bare map of satellite objects is a branch, not a value.
		for _, id := range t.Children(path) {
			if snr, ok := firstNumber(t, path+"."+id+".snr", path+"."+id+".SNR"); ok && snr > 0 {
				snrs = append(snrs, snr)
			}
		}
		return snrs
	}

	add := func(rawSat any) {
		satMap, ok := rawSat.(map[string]any)
		if !ok {
			return
		}
		if snr, ok := satelliteSNR(satMap); ok && snr > 0 {
			snrs = append(snrs, snr)
		}
	}

	switch satellites := raw.(type) {
	case []any:
		for _, rawSat := range satellites {
			add(rawSat)
		}
	case map[string]any:
		for _, rawSat := range satellites {
			add(rawSat)
		}
	}

	return snrs
}

// satelliteSNR reads one satellite object's SNR, published as "snr" or "SNR".
func satelliteSNR(sat map[string]any) (float64, bool) {
	for _, key := range []string{"snr", "SNR"} {
		if snr, ok := numericTelemetryValue(sat[key]); ok {
			return snr, true
		}
	}
	return 0, false
}

// gnssSatelliteCount reports how many satellites are contributing to the
// current GNSS fix. Most SignalK servers publish navigation.gnss.satellites
// as a plain count; this boat's server instead publishes a per-satellite
// SNR map/array at that path, so fall back to counting those entries when
// the plain-number form isn't present. Returns -1 when neither form is
// available, distinguishing "unknown" from a genuine zero.
func gnssSatelliteCount(t telemetryReader) int {
	if count, ok := leafNumber(t, "navigation.gnss.satellites"); ok && count >= 0 {
		return int(count)
	}
	if snrs := parseGNSSSatellitesSNR(t); snrs != nil {
		return len(snrs)
	}
	return -1
}

func parseGNSSQualityIndicator(t telemetryReader) int {
	if numeric, ok := leafNumber(t, "navigation.gnss.methodQuality"); ok && numeric >= 0 {
		return int(math.Round(numeric))
	}

	text, _ := t.Sample("navigation.gnss.methodQuality").Value.(string)
	if text == "" {
		return -1
	}

	return parseGNSSQualityLabel(text)
}

func parseGNSSHDOP(t telemetryReader) float64 {
	if hdop, ok := firstLeafNumber(t,
		"navigation.gnss.horizontalDilution",
		"navigation.gnss.positionDilution",
	); ok && hdop >= 0 {
		return hdop
	}

	return -1
}

func parseGNSSQualityLabel(value string) int {
	normalized := strings.ToLower(strings.TrimSpace(value))
	normalized = strings.NewReplacer("-", "", "_", "", " ", "", ".", "").Replace(normalized)

	switch normalized {
	case "", "invalid", "nofix", "none", "unavailable":
		return 0
	case "gnssfix", "gpsfix":
		return 1
	case "dgnssfix", "dgpsfix":
		return 2
	case "gps", "standalone", "autonomous", "sps":
		return 1
	case "dgps", "differential", "rtcm", "sbas", "waas":
		return 2
	case "rtkfixed", "fixedrtk", "fix":
		return 4
	case "rtkfloat", "floatrtk", "float":
		return 5
	case "simulation", "sim", "simulated":
		return 8
	default:
		if normalized == "manual" || normalized == "estimated" {
			return 1
		}
		return 0
	}
}

// resolveGNSSPosition is the trust gate (ADR 0037): a trusted fix is shown
// and remembered; anything else, a tick with no fix at all included
// (hasPosition false), holds the last trusted fix. -1,-1 means no fix has
// ever been trusted.
func resolveGNSSPosition(latitude, longitude float64, hasPosition bool, validation gnssPositionValidation) (float64, float64) {
	gnssValidationMu.Lock()
	defer gnssValidationMu.Unlock()

	if validation.Status != lastGNSSValidationStatus || validation.Reason != lastGNSSValidationReason {
		switch validation.Status {
		case "critical":
			log.Printf("WARNING: GNSS validation critical: %s", validation.Reason)
		case "degraded":
			log.Printf("WARNING: GNSS validation degraded: %s", validation.Reason)
		case "trusted":
			if lastGNSSValidationStatus == "critical" || lastGNSSValidationStatus == "degraded" {
				log.Printf("GNSS validation recovered: quality=%d hdop=%.2f", validation.QualityIndicator, validation.HDOP)
			}
		}
		lastGNSSValidationStatus = validation.Status
		lastGNSSValidationReason = validation.Reason
	}

	if hasPosition && validation.Trusted && latitude >= -90 && latitude <= 90 && longitude >= -180 && longitude <= 180 {
		lastTrustedGNSSLatitude = latitude
		lastTrustedGNSSLongitude = longitude
		lastTrustedGNSSPositionValid = true
		return latitude, longitude
	}

	if lastTrustedGNSSPositionValid {
		return lastTrustedGNSSLatitude, lastTrustedGNSSLongitude
	}

	return -1, -1
}

// gnssPositionCritical reports whether the latest validation was critical, so
// a raw position read straight from the delta stream is not to be trusted.
func gnssPositionCritical() bool {
	gnssValidationMu.Lock()
	defer gnssValidationMu.Unlock()
	return lastGNSSValidationStatus == "critical"
}

// trustedVesselPosition is the last trusted fix the vessel state stream
// publishes; ok is false while none has been trusted.
func trustedVesselPosition() (lat, lon float64, ok bool) {
	gnssValidationMu.Lock()
	defer gnssValidationMu.Unlock()
	if lastTrustedGNSSPositionValid {
		return lastTrustedGNSSLatitude, lastTrustedGNSSLongitude, true
	}
	return 0, 0, false
}

func resetGNSSPositionValidationState() {
	gnssValidationMu.Lock()
	defer gnssValidationMu.Unlock()

	lastGNSSValidationStatus = ""
	lastGNSSValidationReason = ""
	lastTrustedGNSSLatitude = 0
	lastTrustedGNSSLongitude = 0
	lastTrustedGNSSPositionValid = false
	gnssHeuristic = gnssHeuristicState{}
}
