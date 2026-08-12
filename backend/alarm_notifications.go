package main

import (
	"sort"
	"strings"
)

// notificationsRoot is the SignalK subtree other producers raise alarms into.
const notificationsRoot = "notifications"

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

	return alarmStatus{
		// Namespaced so an inbound notification can never collide with a
		// locally configured rule id.
		RuleID:  notificationsRoot + ":" + path,
		Label:   path,
		Path:    notificationsRoot + "." + path,
		Phase:   alarmPhaseActive,
		State:   state,
		Message: message,
	}, true
}
