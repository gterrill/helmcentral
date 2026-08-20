package main

import (
	"errors"
	"fmt"
	"net/http"
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

// signalKNotificationsAPIPath is SignalK 2.x's Notifications API, which manages
// alerts by notification id rather than by writing their paths. Its two actions
// are defined in terms of the method array: silence removes "sound",
// acknowledge removes both "sound" and "visual".
const signalKNotificationsAPIPath = "/signalk/v2/api/notifications"

const (
	notificationActionSilence     = "silence"
	notificationActionAcknowledge = "acknowledge"
)

// Refusals, distinguished from a server failure so the handler can answer 409
// (the request was understood and declined) rather than 502 (SignalK could not
// carry it out).
var (
	errNotificationNotLive    = errors.New("no live notification at that path")
	errNotificationEmergency  = errors.New("a SignalK emergency cannot be silenced or acknowledged")
	errNotificationNotAllowed = errors.New("SignalK does not offer that action for this notification")
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

// notificationValueIsLive reports whether a notification value is actually
// raised. normal is the cleared state, and an unknown state is not something to
// raise a klaxon over.
//
// Shared with the publish path (signalk_publish.go), which confirms a write by
// reading it back: if the two disagreed about what "cleared" means, publishing
// would report failures for clears the server had accepted. signalk-server does
// not delete a cleared notification — it keeps the key and normalises it to
// state "normal" — so absence is not the test, liveness is.
func notificationValueIsLive(value map[string]any) bool {
	state, _ := value["state"].(string)
	state = strings.TrimSpace(state)

	_, known := alarmStateRank[state]
	return known && state != alarmStateNormal
}

func notificationStatus(value map[string]any, prefix []string) (alarmStatus, bool) {
	if !notificationValueIsLive(value) {
		return alarmStatus{}, false
	}
	state := strings.TrimSpace(value["state"].(string))

	path := strings.Join(prefix, ".")
	message, _ := value["message"].(string)
	if strings.TrimSpace(message) == "" {
		message = path
	}

	acknowledged, silenced := notificationAlertState(value)
	canAcknowledge, canSilence := notificationCapabilities(value)

	// Silencing stops the sound; the alarm is still demanding attention, so
	// only acknowledging moves it out of the active phase.
	phase := alarmPhaseActive
	if acknowledged {
		phase = alarmPhaseAcknowledged
	}

	// ADR 0038's rule outranks the server's flags, and an action already taken
	// is not offered again.
	//
	// Acknowledging subsumes silencing — it removes the visual alert as well as
	// the sound — so an acknowledged alarm is silenced whatever the server's
	// own flag says, and offers no Silence button. SignalK leaves canSilence
	// true and silenced false after an acknowledge, so this cannot be read off
	// the flags alone.
	emergency := state == alarmStateEmergency
	silenced = silenced || acknowledged

	return alarmStatus{
		// Namespaced so an inbound notification can never collide with a
		// locally configured rule id.
		RuleID:         notificationsRoot + ":" + path,
		Label:          path,
		Path:           notificationsRoot + "." + path,
		Phase:          phase,
		State:          state,
		Message:        message,
		Silenced:       silenced,
		CanSilence:     canSilence && !silenced && !emergency,
		CanAcknowledge: canAcknowledge && !acknowledged && !emergency,
	}, true
}

// notificationAlertState reports whether a notification has been acknowledged
// or silenced.
//
// The Notifications API keeps explicit flags, and they are authoritative. A
// producer that writes the tree directly carries none, so its method array is
// read instead — which agrees by construction, because the API defines its own
// actions as removing exactly those methods.
//
// An absent method array is not silence. It is a producer that never said how
// to alert, and reading it as acknowledged would swallow every alarm it raises.
func notificationAlertState(value map[string]any) (acknowledged, silenced bool) {
	if status, ok := value["status"].(map[string]any); ok {
		acknowledged, _ = status["acknowledged"].(bool)
		silenced, _ = status["silenced"].(bool)
		return acknowledged, silenced
	}

	methods, declared := notificationMethods(value)
	if !declared {
		return false, false
	}
	sounding := slices.Contains(methods, notificationMethodSound)
	visible := slices.Contains(methods, notificationMethodVisual)
	return !sounding && !visible, !sounding
}

// notificationCapabilities reports which actions Helmcentral can actually
// invoke for a notification.
//
// Both are POSTs to the notification's own id through the Notifications API, so
// a producer that writes the tree directly — no id, no status — offers neither.
// Reporting an action that has nowhere to go would put a button on screen whose
// only possible outcome is an error.
func notificationCapabilities(value map[string]any) (canAcknowledge, canSilence bool) {
	if id, _ := value["id"].(string); id == "" {
		return false, false
	}
	status, ok := value["status"].(map[string]any)
	if !ok {
		return false, false
	}
	canAcknowledge, _ = status["canAcknowledge"].(bool)
	canSilence, _ = status["canSilence"].(bool)
	return canAcknowledge, canSilence
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

// actOnSignalKNotification invokes one of the SignalK Notifications API's alert
// actions — silence or acknowledge — on a notification raised by another
// producer (ADR 0038).
//
// The action goes to the notification's own id, not to its path. Writing the
// path was the original mistake: notifications have no PUT handler, so the
// server answered 404, and even a successful write would have been Helmcentral
// reimplementing by hand what the API does properly — the server edits the
// method array and maintains the status flags itself.
//
// The server is also the record. One of these has no engine state to mutate,
// and the next poll rebuilds it from the tree, so an acknowledgement held only
// in Helmcentral would be erased a second later and would leave every other
// consumer — buzzer plugin, MFD — still sounding.
func actOnSignalKNotification(snapshot *signalKSnapshot, path, action string, now time.Time, post func(id, action string) error) (alarmStatus, error) {
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

	allowed := status.CanAcknowledge
	if action == notificationActionSilence {
		allowed = status.CanSilence
	}
	if !allowed {
		return alarmStatus{}, fmt.Errorf("%w: %s.%s cannot be %sd", errNotificationNotAllowed, notificationsRoot, path, action)
	}

	id, _ := value["id"].(string)
	if err := post(id, action); err != nil {
		return alarmStatus{}, fmt.Errorf("failed to %s the notification through SignalK: %w", action, err)
	}

	// The state just committed, not a re-read: the change has to travel back
	// through the delta stream before the snapshot reflects it.
	status.Silenced = true
	status.CanSilence = false
	if action == notificationActionAcknowledge {
		status.Phase = alarmPhaseAcknowledged
		status.AckedAt = now
		status.CanAcknowledge = false
	}
	return status, nil
}

// postSignalKNotificationAction calls one of the Notifications API's actions,
// authenticating with Helmcentral's own service account like every other
// outbound write (ADR 0040).
func postSignalKNotificationAction(id, action string) error {
	settingsPath := getEnv("SETTINGS_FILE", "../settings.yaml")
	address, port, err := loadSignalKSettings(settingsPath)
	if err != nil {
		return fmt.Errorf("could not read the SignalK connection settings: %w", err)
	}

	return signalkRequestJSONWithAuth(
		buildSignalKURL(address, port), settingsPath,
		signalKNotificationsAPIPath+"/"+id+"/"+action,
		http.MethodPost, nil,
	)
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
