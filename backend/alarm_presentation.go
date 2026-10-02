package main

import (
	"regexp"
	"strconv"
	"strings"
	"time"
	"unicode"
)

// alarmRadarTarget is the live radar figures for the target behind a
// guard-zone notification. Computed on every read from the radar target
// store, never frozen at raise (same contract as alarmStatus.Encounter), and
// absent whenever the target cannot be found rather than guessed.
type alarmRadarTarget struct {
	BearingRad  float64  `json:"bearing_rad"` // true, radians (ADR 0062)
	RangeM      float64  `json:"range_m"`
	CpaM        *float64 `json:"cpa_m,omitempty"`
	TcpaSeconds *float64 `json:"tcpa_seconds,omitempty"`
}

var (
	radarGuardZonePath = regexp.MustCompile(`^radar\.([^.]+)\.guardZone\.(\d+)$`)
	radarTargetInText  = regexp.MustCompile(`(?i)\btarget (\d+)\b`)
	camelBoundary      = regexp.MustCompile(`([a-z0-9])([A-Z])`)
	trailingPunct      = regexp.MustCompile(`[.!?]+$`)
	// The AIS prioritizer's target message (ADR 0057): "<NAME> - CPA WARNING".
	collisionTargetMessage = regexp.MustCompile(`^(.+?)\s+-\s+CPA\s+(WARNING|ALARM)$`)
)

// writtenTitles names the bus sources common enough to deserve better than
// the path turned into words.
var writtenTitles = map[string]string{
	"navigation.anchor": "Anchor Alarm",
}

// presentNotification turns a bus notification (path without the
// "notifications." prefix, plus the producer's message) into the short event
// name and the situation sentence an operator reads. The title is never a
// path, device id or plugin name; the body never carries internal ids and is
// empty when the producer said nothing beyond the path.
func presentNotification(path, message string) (title, body string) {
	if m := radarGuardZonePath.FindStringSubmatch(path); m != nil {
		return "Radar Guard Zone " + m[2], "Target in guard zone " + m[2] + "."
	}

	message = strings.TrimSpace(message)
	if path == "navigation.closestApproach" {
		// Only the self tree reaches here: target contexts go through
		// presentCollision. Under self the plugin uses this path for its own
		// GPS fault (ADR 0057).
		return "Collision Watch Fault", sentence(message, path)
	}

	title, ok := writtenTitles[path]
	if !ok {
		title = humanisePath(path)
	}
	return title, sentence(message, path)
}

// presentCollision words an AIS target's collision alarm. It is always a
// collision risk; the vessel's name is lifted out of the plugin's
// "<NAME> - CPA WARNING" when the message has that shape, and otherwise the
// message stands as it is.
func presentCollision(message string) (title, body string) {
	message = strings.TrimSpace(message)
	if m := collisionTargetMessage.FindStringSubmatch(message); m != nil {
		return "Collision Risk", m[1] + " inside the CPA limit."
	}
	return "Collision Risk", sentence(message, "navigation.closestApproach")
}

// needsRadarTargets reports whether any status is a guard zone, so the radar
// store is only copied when there is a figure to look up.
func needsRadarTargets(statuses []alarmStatus) bool {
	for _, status := range statuses {
		if radarGuardZonePath.MatchString(status.Label) {
			return true
		}
	}
	return false
}

// sentence is the producer's message with its punctuation normalised to one
// full stop, or "" when it says nothing beyond the path.
func sentence(message, path string) string {
	if message == "" || message == path {
		return ""
	}
	message = strings.TrimSpace(trailingPunct.ReplaceAllString(message, ""))
	if message == "" {
		return ""
	}
	return message + "."
}

func humanisePath(path string) string {
	var words []string
	for _, segment := range strings.Split(path, ".") {
		for _, word := range strings.Fields(camelBoundary.ReplaceAllString(segment, "$1 $2")) {
			runes := []rune(word)
			runes[0] = unicode.ToUpper(runes[0])
			words = append(words, string(runes))
		}
	}
	return strings.Join(words, " ")
}

// radarFiguresFor finds the live radar target behind a guard-zone
// notification. nil unless the path is a guard zone, the message names a
// target id and the store currently holds that target.
func radarFiguresFor(path, message string, targets []radarTarget) *alarmRadarTarget {
	pm := radarGuardZonePath.FindStringSubmatch(path)
	tm := radarTargetInText.FindStringSubmatch(message)
	if pm == nil || tm == nil {
		return nil
	}
	id, err := strconv.ParseUint(tm[1], 10, 64)
	if err != nil {
		return nil
	}
	key := radarTargetKey(pm[1], id)
	for _, t := range targets {
		if radarTargetKey(t.RadarID, t.TargetID) == key {
			return &alarmRadarTarget{BearingRad: t.BearingRad, RangeM: t.RangeM, CpaM: t.CpaM, TcpaSeconds: t.TcpaSeconds}
		}
	}
	return nil
}

// presentNotificationStatus rewrites a bus notification status (whose Label
// is still the bare path) for display. Path and RuleID are untouched, so
// everything keyed on them keeps working. Apply it once, last.
func presentNotificationStatus(status alarmStatus, targets []radarTarget, _ time.Time) alarmStatus {
	path := status.Label
	status.RadarTarget = radarFiguresFor(path, status.Message, targets)
	status.Label, status.Message = presentNotification(path, status.Message)
	return status
}
