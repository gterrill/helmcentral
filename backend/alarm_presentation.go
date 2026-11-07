package main

import (
	"regexp"
	"strings"
	"unicode"
	"unicode/utf8"
)

var (
	// The radar plugin's guard-zone notification, still on the bus for Helmcam
	// (ADR 0270). Worded without the radar's device key or the target id.
	radarGuardZonePath = regexp.MustCompile(`^radar\.[^.]+\.guardZone\.(\d+)$`)
	camelBoundary      = regexp.MustCompile(`([a-z0-9])([A-Z])`)
	trailingPunct      = regexp.MustCompile(`[.!?]+$`)
	// The AIS prioritizer's target message (ADR 0057): "<NAME> - CPA WARNING".
	collisionTargetMessage = regexp.MustCompile(`^(.+?)\s+-\s+CPA\s+(WARNING|ALARM)$`)
)

// writtenTitles names the bus sources common enough to deserve better than
// the path turned into words.
var writtenTitles = map[string]string{
	"navigation.anchor": "Anchor Alarm",
	// The plotter's route alarms (alarm_presentation_navigation.go).
	"perpendicularPassed":  "Waypoint Passed",
	"arrivalCircleEntered": "Arriving at Waypoint",
}

// presentNotification turns a bus notification (path without the
// "notifications." prefix, plus the producer's message) into the short event
// name and the situation sentence an operator reads. The title is never a
// path, device id or plugin name; the body never carries internal ids and is
// empty when the producer said nothing beyond the path.
func presentNotification(path, message string) (title, body string) {
	if m := radarGuardZonePath.FindStringSubmatch(path); m != nil {
		return "Radar Guard Zone " + m[1], "Target in guard zone " + m[1] + "."
	}

	message = strings.TrimSpace(message)
	if path == "navigation.closestApproach" {
		// Only the self tree reaches here: target contexts go through
		// presentCollision. Under self the plugin uses this path for its own
		// GPS fault (ADR 0057).
		return "Collision Watch Fault", sentence(message, path)
	}

	if strings.HasPrefix(path, distressReceivedPrefix) {
		// The key carries a timestamp and the beacon's MMSI; neither belongs
		// on the card. The producer's sentence says where and what.
		if strings.HasPrefix(path, distressAISPrefix) {
			return "AIS Distress", sentence(message, path)
		}
		return "Distress Received", sentence(message, path)
	}

	if title, body, ok := presentNavigationNotification(path); ok {
		return title, body
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
		return "Collision Risk", m[1] + collisionBodySuffix
	}
	return "Collision Risk", sentence(message, "navigation.closestApproach")
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

// presentNotificationStatus rewrites a bus notification status (whose Label
// is still the bare path) for display. Path and RuleID are untouched, so
// everything keyed on them keeps working. Apply it once, last.
func presentNotificationStatus(status alarmStatus) alarmStatus {
	path := status.Label
	status.Label, status.Message = presentNotification(path, status.Message)
	return status
}

// collisionBodySuffix ends the sentence presentCollision writes for a named
// target. collisionTargetName reads it back, so the two share this constant.
const collisionBodySuffix = " inside the CPA limit."

// alarmSourceKind says what kind of thing raises the alarms under one rule id.
type alarmSourceKind string

// alarmSourceAISClosestApproach is an AIS target's collision alarm.
const alarmSourceAISClosestApproach alarmSourceKind = "ais_closest_approach"

// Label is the kind in the operator's words.
func (k alarmSourceKind) Label() string {
	if k == alarmSourceAISClosestApproach {
		return "AIS closest approach"
	}
	return ""
}

// alarmSourceKindOf reads the kind from a rule id, "" when it has none. A
// vessel's collision alarm is the notifications: prefix, the collision branch,
// the vessel separator and the target's urn.
func alarmSourceKindOf(ruleID string) alarmSourceKind {
	branch, vesselID := splitNotificationVessel(ruleID)
	if vesselID != "" && branch == "notifications:"+strings.Join(collisionNotificationBranch, ".") {
		return alarmSourceAISClosestApproach
	}
	return ""
}

var collisionUnnamedTarget = regexp.MustCompile(`^<?\d+>?$`)

// collisionTargetName is the vessel a presented collision message names, in
// title case when the AIS name was all capitals. ok is false for a message that
// names nobody: a bare or bracketed MMSI is not a name.
func collisionTargetName(message string) (name string, ok bool) {
	name, found := strings.CutSuffix(strings.TrimSpace(message), collisionBodySuffix)
	name = strings.TrimSpace(name)
	if !found || name == "" || collisionUnnamedTarget.MatchString(name) {
		return "", false
	}
	if name == strings.ToUpper(name) {
		words := strings.Fields(strings.ToLower(name))
		for i, w := range words {
			words[i] = upperFirstLetter(w)
		}
		name = strings.Join(words, " ")
	}
	return name, true
}

// upperFirstLetter capitalises the first letter of a word, skipping any digits
// or punctuation before it.
func upperFirstLetter(w string) string {
	for i, r := range w {
		if unicode.IsLetter(r) {
			return w[:i] + string(unicode.ToUpper(r)) + w[i+utf8.RuneLen(r):]
		}
	}
	return w
}
