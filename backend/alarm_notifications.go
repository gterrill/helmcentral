package main

import (
	"errors"
	"fmt"
	"slices"
	"sort"
	"strings"
	"time"
)

// notificationsRoot is the SignalK subtree other producers raise alarms into.
const notificationsRoot = "notifications"

// The methods SignalK notifications use to ask for attention. There is no
// "acknowledged" state in the spec: silencing an alarm is expressed by dropping
// "sound" from its method array, leaving the notification live and visible.
const (
	notificationMethodVisual = "visual"
	notificationMethodSound  = "sound"
)

// Refusals to acknowledge, distinguished from an upstream write failure so the
// handler can answer 409 (the request was understood and declined) rather than
// 502 (SignalK could not be written to).
var (
	errNotificationNotLive   = errors.New("no live notification at that path")
	errNotificationEmergency = errors.New("a SignalK emergency cannot be silenced")
)

// signalKNotifications surfaces alarms raised by anything else on the bus —
// Victron GX, N2K devices, other SignalK plugins — as alarm statuses.
//
// This is what adopting SignalK's notification vocabulary buys (ADR 0038):
// no per-source integration, no translation layer. Every producer already
// speaks it, so consuming the tree the delta stream already carries is the
// whole implementation.
func signalKNotifications(snapshot *signalKSnapshot) []alarmStatus {
	tree := snapshot.selfTree()
	if tree == nil {
		return nil
	}

	root, ok := tree[notificationsRoot].(map[string]any)
	if !ok {
		return nil
	}

	var out []alarmStatus
	collectSignalKNotifications(root, nil, &out)

	sort.Slice(out, func(i, j int) bool { return out[i].Path < out[j].Path })
	return out
}

func collectSignalKNotifications(node map[string]any, prefix []string, out *[]alarmStatus) {
	// A notification leaf is a node whose "value" is an object carrying a
	// state. Anything else at this level is a branch to keep descending.
	if value, ok := node["value"].(map[string]any); ok {
		if status, ok := notificationStatus(value, prefix); ok {
			*out = append(*out, status)
		}
		return
	}

	for key, child := range node {
		asMap, ok := child.(map[string]any)
		if !ok {
			continue
		}
		collectSignalKNotifications(asMap, append(append([]string{}, prefix...), key), out)
	}
}

func notificationStatus(value map[string]any, prefix []string) (alarmStatus, bool) {
	state, _ := value["state"].(string)
	state = strings.TrimSpace(state)

	// normal is the cleared state, and an unknown state is not something to
	// raise a klaxon over.
	if _, known := alarmStateRank[state]; !known || state == alarmStateNormal {
		return alarmStatus{}, false
	}

	path := strings.Join(prefix, ".")
	message, _ := value["message"].(string)
	if strings.TrimSpace(message) == "" {
		message = path
	}

	// A notification with no sound left in its method array has been silenced —
	// by us, by an MFD, or by another client. That is the only acknowledgement
	// record these alarms have: they are rebuilt from the tree on every poll,
	// so nothing local could survive the next one.
	//
	// An absent method array is not silence. It is a producer that never said
	// how to alert, and reading it as acknowledged would swallow every alarm
	// such a producer raises.
	phase := alarmPhaseActive
	if methods, declared := notificationMethods(value); declared && !slices.Contains(methods, notificationMethodSound) {
		phase = alarmPhaseAcknowledged
	}

	return alarmStatus{
		// Namespaced so an inbound notification can never collide with a
		// locally configured rule id.
		RuleID:  notificationsRoot + ":" + path,
		Label:   path,
		Path:    notificationsRoot + "." + path,
		Phase:   phase,
		State:   state,
		Message: message,
	}, true
}

// notificationMethods reads a notification's method array, reporting whether
// the producer declared one at all.
func notificationMethods(value map[string]any) ([]string, bool) {
	raw, ok := value["method"].([]any)
	if !ok {
		return nil, false
	}

	methods := make([]string, 0, len(raw))
	for _, entry := range raw {
		if method, ok := entry.(string); ok {
			methods = append(methods, method)
		}
	}
	return methods, true
}

// notificationRuleIDPath unwraps the namespaced id signalKNotifications hands
// out ("notifications:navigation.arrivalCircleEntered") back into the SignalK
// path under the notifications root, reporting whether the id was bus-sourced
// at all. A locally configured rule id never matches.
func notificationRuleIDPath(ruleID string) (string, bool) {
	path, ok := strings.CutPrefix(ruleID, notificationsRoot+":")
	if !ok || path == "" {
		return "", false
	}
	return path, true
}

// acknowledgeSignalKNotification silences a notification raised by another
// producer by writing it back to the bus with "sound" dropped from its method
// array — the spec's only expression of acknowledgement (ADR 0038).
//
// The bus is the record: unlike a rule alarm, one of these has no engine state
// to mutate, and the next poll rebuilds it from the tree. An ack that lived
// only in Helmcentral would be erased a second later, and would leave every
// other consumer — buzzer plugin, MFD — still sounding.
func acknowledgeSignalKNotification(snapshot *signalKSnapshot, path string, now time.Time, put func(string, any) error) (alarmStatus, error) {
	value, ok := notificationValueAt(snapshot, path)
	if !ok {
		return alarmStatus{}, fmt.Errorf("%w: %s.%s", errNotificationNotLive, notificationsRoot, path)
	}

	status, live := notificationStatus(value, strings.Split(path, "."))
	if !live {
		return alarmStatus{}, fmt.Errorf("%w: %s.%s", errNotificationNotLive, notificationsRoot, path)
	}
	if status.State == alarmStateEmergency {
		return alarmStatus{}, fmt.Errorf("%w: %s.%s", errNotificationEmergency, notificationsRoot, path)
	}

	if err := put(notificationsRoot+"."+path, silencedNotificationValue(value)); err != nil {
		return alarmStatus{}, fmt.Errorf("failed to write the acknowledgement back to SignalK: %w", err)
	}

	status.Phase = alarmPhaseAcknowledged
	status.AckedAt = now
	return status, nil
}

// silencedNotificationValue copies a notification with "sound" removed. The
// whole object is copied because a SignalK PUT replaces it: dropping fields the
// producer set would corrupt its own notification on the way through.
func silencedNotificationValue(value map[string]any) map[string]any {
	silenced := make(map[string]any, len(value))
	for key, entry := range value {
		silenced[key] = entry
	}

	methods, declared := notificationMethods(value)
	if !declared {
		// Match what Helmcentral publishes for its own alarms, minus the sound:
		// still shown, no longer heard.
		methods = []string{notificationMethodVisual, notificationMethodSound}
	}

	remaining := make([]string, 0, len(methods))
	for _, method := range methods {
		if method != notificationMethodSound {
			remaining = append(remaining, method)
		}
	}
	silenced["method"] = remaining
	return silenced
}

// notificationValueAt walks the snapshot to one notification leaf's value.
func notificationValueAt(snapshot *signalKSnapshot, path string) (map[string]any, bool) {
	node := snapshot.selfTree()
	if node == nil {
		return nil, false
	}

	for _, segment := range append([]string{notificationsRoot}, strings.Split(path, ".")...) {
		child, ok := node[segment].(map[string]any)
		if !ok {
			return nil, false
		}
		node = child
	}

	value, ok := node["value"].(map[string]any)
	return value, ok
}
