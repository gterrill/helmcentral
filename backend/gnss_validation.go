package main

import (
	"fmt"
	"log"
	"math"
	"strings"
	"sync"
)

const (
	gnssTrustedHDOPThreshold  = 2.5
	gnssCriticalHDOPThreshold = 4.0
)

type gnssPositionValidation struct {
	QualityIndicator int
	HDOP             float64
	Status           string
	Reason           string
	Trusted          bool
	Critical         bool
}

var gnssValidationMu sync.Mutex
var lastGNSSValidationStatus string
var lastGNSSValidationReason string
var lastTrustedGNSSLatitude float64
var lastTrustedGNSSLongitude float64
var lastTrustedGNSSPositionValid bool

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
		[]string{"navigation", "position", "value", "quality"},
		[]string{"navigation", "position", "value", "qualityIndicator"},
		[]string{"navigation", "position", "quality", "value"},
		[]string{"navigation", "position", "qualityIndicator", "value"},
		[]string{"navigation", "gnss", "quality", "value"},
		[]string{"navigation", "gnss", "method", "value"},
		[]string{"navigation", "gnss", "positionFixIndicator", "value"},
		[]string{"navigation", "gnss", "positionFix", "value"},
		[]string{"navigation", "gnss", "fixQuality", "value"},
	)
	if numeric >= 0 {
		return int(math.Round(numeric))
	}

	text := firstNonEmptyString(
		lookupString(payload, "navigation", "position", "value", "quality"),
		lookupString(payload, "navigation", "position", "value", "qualityIndicator"),
		lookupString(payload, "navigation", "position", "quality", "value"),
		lookupString(payload, "navigation", "position", "qualityIndicator", "value"),
		lookupString(payload, "navigation", "gnss", "quality", "value"),
		lookupString(payload, "navigation", "gnss", "method", "value"),
		lookupString(payload, "navigation", "gnss", "positionFixIndicator", "value"),
		lookupString(payload, "navigation", "gnss", "positionFix", "value"),
		lookupString(payload, "navigation", "gnss", "fixQuality", "value"),
	)
	if text == "" {
		return -1
	}

	return parseGNSSQualityLabel(text)
}

func parseGNSSHDOP(payload map[string]any) float64 {
	hdop := lookupFirstNumber(payload,
		[]string{"navigation", "position", "value", "hdop"},
		[]string{"navigation", "position", "hdop", "value"},
		[]string{"navigation", "gnss", "hdop", "value"},
		[]string{"navigation", "gnss", "horizontalDilutionOfPrecision", "value"},
		[]string{"navigation", "gnss", "dilutionOfPrecision", "value"},
		[]string{"navigation", "gnss", "positionDilutionOfPrecision", "value"},
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
}