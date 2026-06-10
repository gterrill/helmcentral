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
	gnssTrustedHDOPThreshold  = 2.5
	gnssCriticalHDOPThreshold = 4.0
	gnssDegradedMaxAge        = 10 * time.Second
	gnssCriticalMaxAge        = 30 * time.Second
	gnssDepthJumpMeters       = 1.5
	gnssDepthJumpWindow       = 10 * time.Second
	gnssDegradedJumpKts       = 6.0
	gnssCriticalJumpKts       = 12.0
	gnssRecoverySamples       = 5
	gnssRecoveryMinDuration   = 15 * time.Second
)

type gnssPositionValidation struct {
	QualityIndicator int
	HDOP             float64
	Status           string
	Reason           string
	Trusted          bool
	Critical         bool
}

type gnssObservedSample struct {
	Latitude      float64
	Longitude     float64
	DepthMeters   float64
	Navigation    string
	ObservedAt    time.Time
	HasObservedAt bool
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

func parseGNSSPositionValidation(payload map[string]any) gnssPositionValidation {
	qualityIndicator := parseGNSSQualityIndicator(payload)
	hdop := parseGNSSHDOP(payload)
	status, reason := classifyGNSSPositionValidation(qualityIndicator, hdop)
	return gnssPositionValidation{
		QualityIndicator: qualityIndicator,
		HDOP:             hdop,
		Status:           status,
		Reason:           reason,
		Trusted:          status == "trusted",
		Critical:         status == "critical",
	}
}

func applyGNSSHeuristics(validation gnssPositionValidation, sample gnssObservedSample, now time.Time) gnssPositionValidation {
	if now.IsZero() {
		now = time.Now().UTC()
	}

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

	if gnssHeuristic.lastSample != nil && sample.HasObservedAt && gnssHeuristic.lastSample.HasObservedAt {
		dt := sample.ObservedAt.Sub(gnssHeuristic.lastSample.ObservedAt)
		if dt > 0 {
			distance := haversineMeters(
				gnssHeuristic.lastSample.Latitude,
				gnssHeuristic.lastSample.Longitude,
				sample.Latitude,
				sample.Longitude,
			)
			speedKts := (distance / dt.Seconds()) * metersPerSecondToKnots
			if isAnchoredOrMooredState(sample.Navigation) {
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

	if sample.Latitude >= -90 && sample.Latitude <= 90 && sample.Longitude >= -180 && sample.Longitude <= 180 {
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
	validation.Reason = "awaiting trusted recovery hysteresis"
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

func classifyGNSSPositionValidation(qualityIndicator int, hdop float64) (string, string) {
	switch {
	case qualityIndicator <= 0:
		return "critical", "quality indicator reports no fix"
	case hdop < 0:
		return "critical", "hdop unavailable"
	case hdop > gnssCriticalHDOPThreshold:
		return "critical", fmt.Sprintf("hdop %.1f exceeds %.1f", hdop, gnssCriticalHDOPThreshold)
	case hdop <= gnssTrustedHDOPThreshold:
		return "trusted", ""
	default:
		return "degraded", fmt.Sprintf("hdop %.1f above trusted threshold %.1f", hdop, gnssTrustedHDOPThreshold)
	}
}

func parseGNSSQualityIndicator(payload map[string]any) int {
	numeric := lookupFirstNumber(payload,
		[]string{"navigation", "gnss", "methodQuality", "value"},
	)
	if numeric >= 0 {
		return int(math.Round(numeric))
	}

	text := firstNonEmptyString(
		lookupString(payload, "navigation", "gnss", "methodQuality", "value"),
	)
	if text == "" {
		return -1
	}

	return parseGNSSQualityLabel(text)
}

func parseGNSSHDOP(payload map[string]any) float64 {
	hdop := lookupFirstNumber(payload,
		[]string{"navigation", "gnss", "horizontalDilution", "value"},
		[]string{"navigation", "gnss", "positionDilution", "value"},
	)
	if hdop >= 0 {
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

func resolveGNSSPosition(latitude, longitude float64, validation gnssPositionValidation) (float64, float64) {
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

	if validation.Trusted && latitude >= -90 && latitude <= 90 && longitude >= -180 && longitude <= 180 {
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
