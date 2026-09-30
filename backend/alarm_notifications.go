package main

import (
	"errors"
	"fmt"
	"log"
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
//
// owned excludes a path Helmcentral itself writes into (alarm_ownership.go).
// Without it, Helmcentral's own rule alarms — published onto this same tree
// by signalKNotifyTransport (signalk_publish.go) so a buzzer or MFD can react
// — read back as a second, foreign-looking alarm, and a path this instance no
// longer has a rule for but another Helmcentral instance still published to
// shows up as a ghost with no engine status behind it at all.
func signalKNotifications(snapshot *signalKSnapshot, owned func(path string) bool, now time.Time) []alarmStatus {
	// nodeAt copies only the notifications branch, not the whole self tree
	// selfTree() would -- this runs once per activeAlarms() call (the alarms
	// SSE event, the REST handler, the heartbeat) and unitForAlarmPath below
	// used to cost a second whole-tree copy per live notification on top of
	// it (backend-perf-audit.md Tier 1 #2).
	root := snapshot.nodeAt(notificationsRoot)
	if root == nil {
		return nil
	}

	var out []alarmStatus
	collectSignalKNotifications(root, nil, &out)

	live := out[:0]
	for _, status := range out {
		// Label is set to exactly the bare path by notificationStatus below,
		// which is what owned expects.
		if owned(status.Label) {
			continue
		}
		// ADR 0144: a leaf whose own "sentence" field says it came from a
		// repeating NMEA 0183 message (signalk-server's parser re-asserts
		// arrivalCircleEntered/perpendicularPassed on every APB sentence and
		// only clears them when a later one arrives with the flag unset) is
		// set aside once that sentence has gone quiet for too long, rather
		// than surfaced as though the condition were still being reported
		// right now. root is already a copy this call holds, so this is a
		// cheap in-memory walk, not another snapshot lock/copy.
		if leaf := notificationLeafAt(root, status.Label); leaf != nil {
			if stale, _, _, _ := nmeaNotificationIsStale(snapshot, leaf, now); stale {
				continue
			}
		}
		// Label is also the real SignalK data path the notification is
		// about -- Path is "notifications."+Label, which carries no meta of
		// its own -- so the unit lookup is keyed off Label, derived-aware for
		// the same reason a rule alarm's is (a Helmcentral echo of one of its
		// own derived-path rules can turn up here before ownership filters it
		// out on some other instance).
		status.Unit = unitForAlarmPath(snapshot, status.Label)
		live = append(live, status)
	}
	out = live

	sort.Slice(out, func(i, j int) bool { return out[i].Path < out[j].Path })
	return out
}

// nmeaNotificationStaleAfter is how long an NMEA 0183 sentence may go unseen
// -- or, never having been seen at all, how long this process may go on
// listening without seeing it -- before a live notification that names it
// stops being surfaced (ADR 0144).
//
// signalk-server's NMEA 0183 parser raises notifications like
// arrivalCircleEntered and perpendicularPassed from APB sentences and
// re-asserts them on every repeat -- typically once a second while the
// plotter is actively steering the route -- clearing one only when a later
// sentence arrives with the flag unset. There is no timeout of its own: stop
// the route on the plotter, or lose the NMEA 0183 feed, and the sentence
// simply stops arriving, so the notification stays "alarm" on the SignalK
// tree forever (the boat's own arrivalCircleEntered, stuck live since the APB
// feed stopped at 2026-09-28T00:52:49Z). Five minutes is far longer than any
// real gap between repeats while short enough that an abandoned route stops
// paging within a few minutes rather than staying live for days.
const nmeaNotificationStaleAfter = 5 * time.Minute

// nmeaRepeatingRouteSentences are the only sentences the stale check applies
// to: route sentences the plotter re-sends every second or so while it steers
// a route, so their silence means the route has stopped. A one-shot sentence
// (a DSC distress call, say) raises its notification once and never repeats,
// and timing that out would hide an alarm that still stands.
var nmeaRepeatingRouteSentences = map[string]bool{"APB": true, "RMB": true}

// nmeaNotificationIsStale reports whether a live notification leaf, whose own
// "sentence" field names an NMEA 0183 sentence type, should be treated as
// stale -- and if so, what to log about it.
//
// Staleness is measured by snapshot.lastSentenceSeen(sentence), the receive
// time of the latest delta update that actually carried fresh vessel data for
// that sentence, never by the leaf's own "timestamp". The leaf's timestamp
// cannot be trusted for this: the SignalK Notifications API re-emits a
// notification's leaf -- with the ORIGINAL sentence and source object, and a
// BRAND NEW timestamp -- whenever another client acknowledges or silences it,
// or the periodic REST reconcile re-copies it from the server (ADR 0086).
// That is exactly what made the boat's stuck arrivalCircleEntered leaf read
// 02:43:31Z, nearly two hours after its APB feed actually died at
// 00:52:49Z: an acknowledge in between refreshed the timestamp with no new
// APB sentence ever arriving. Reading the leaf's own timestamp as "last seen"
// would make a dead alarm look fresh again on every such touch -- silently
// re-raising and re-dispatching it -- and, the other way, would hide a
// genuinely live alarm after five minutes of APB updates that all happen to
// avoid this exact leaf, since notification-only updates change nothing
// about the underlying route.
//
// A "sentence" this process has never once seen for real (everSeen false) is
// not automatically stale: it is given until listeningSince()+
// nmeaNotificationStaleAfter before its absence counts as evidence of
// anything, rather than declaring every notification stale the instant the
// process starts. This is what clears a notification left behind by a
// restart -- the REST reconcile brings the old leaf straight back with its
// old sentence and old timestamp (ADR 0086), but a freshly started process
// has recorded no sighting of that sentence at all, and five minutes of
// listening with nothing repeating is itself the evidence the route is gone.
func nmeaNotificationIsStale(snapshot *signalKSnapshot, leaf map[string]any, now time.Time) (stale bool, sentence string, lastSeen time.Time, everSeen bool) {
	sentence, _ = leaf["sentence"].(string)
	if !nmeaRepeatingRouteSentences[sentence] {
		return false, sentence, time.Time{}, false
	}

	if seen, ok := snapshot.lastSentenceSeen(sentence); ok {
		return now.Sub(seen) > nmeaNotificationStaleAfter, sentence, seen, true
	}

	listenSince := snapshot.listeningSince()
	if listenSince.IsZero() {
		// No delta has ever been applied at all: there is no clock to measure
		// a grace period against yet, so nothing here can honestly be called
		// stale.
		return false, sentence, time.Time{}, false
	}
	return now.Sub(listenSince) > nmeaNotificationStaleAfter, sentence, time.Time{}, false
}

// notificationLeafAt walks an already-fetched notifications subtree (as
// snapshot.nodeAt(notificationsRoot) returns it) to the leaf at a dotted
// label -- the same string notificationStatus sets as Label -- returning nil
// if any segment along the way is missing. A pure map walk over a copy the
// caller already holds, so a second lookup for one label costs nothing beyond
// signalKNotifications' own tree fetch.
func notificationLeafAt(root map[string]any, label string) map[string]any {
	node := root
	for _, segment := range strings.Split(label, ".") {
		if node == nil {
			return nil
		}
		child, ok := node[segment].(map[string]any)
		if !ok {
			return nil
		}
		node = child
	}
	return node
}

// nmeaStaleInfo is what logStaleNMEATransitions needs to describe why a
// notification was set aside: the sentence it came from and when the delta
// stream last carried a real update for that sentence, if ever.
type nmeaStaleInfo struct {
	Sentence string
	LastSeen time.Time
	EverSeen bool
}

// staleNMEANotificationLabels returns every currently-live (per SignalK's own
// state) self notification label that signalKNotifications is excluding
// because its leaf has gone stale (nmeaNotificationIsStale), keyed by label.
//
// Kept separate from signalKNotifications, which every alarm list already
// reads several times a second, so a caller that needs to tell "genuinely
// cleared" apart from "still alarm on the bus, just set aside" -- only
// busNotificationWatcher's own once-per-transition diagnostic log needs that
// -- does not have to change what every other consumer gets back.
func staleNMEANotificationLabels(snapshot *signalKSnapshot, root map[string]any, now time.Time) map[string]nmeaStaleInfo {
	if root == nil {
		return nil
	}

	var out []alarmStatus
	collectSignalKNotifications(root, nil, &out)

	stale := map[string]nmeaStaleInfo{}
	for _, status := range out {
		leaf := notificationLeafAt(root, status.Label)
		if leaf == nil {
			continue
		}
		if isStale, sentence, lastSeen, everSeen := nmeaNotificationIsStale(snapshot, leaf, now); isStale {
			stale[status.Label] = nmeaStaleInfo{Sentence: sentence, LastSeen: lastSeen, EverSeen: everSeen}
		}
	}
	return stale
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

	var ackedAt time.Time
	if acknowledged {
		ackedAt = notificationAcknowledgedAt(value, path)
	}

	return alarmStatus{
		// Namespaced so an inbound notification can never collide with a
		// locally configured rule id.
		RuleID:         notificationsRoot + ":" + path,
		Label:          path,
		Path:           notificationsRoot + "." + path,
		Phase:          phase,
		State:          state,
		Message:        message,
		AckedAt:        ackedAt,
		Silenced:       silenced,
		CanSilence:     canSilence && !silenced && !emergency,
		CanAcknowledge: canAcknowledge && !acknowledged && !emergency,
	}, true
}

// notificationAcknowledgedAt reads status.acknowledgedAt, which SignalK 2.31
// and later stamps when an alarm is acknowledged, from any client, and removes
// when the alarm clears or worsens. Older servers omit it, leaving the time
// zero. A stamp that does not parse is logged and left zero rather than
// replaced with a guess.
func notificationAcknowledgedAt(value map[string]any, path string) time.Time {
	status, ok := value["status"].(map[string]any)
	if !ok {
		return time.Time{}
	}
	raw, present := status["acknowledgedAt"]
	if !present {
		return time.Time{}
	}
	text, _ := raw.(string)
	at, err := time.Parse(time.RFC3339Nano, text)
	if err != nil {
		log.Printf("alarm notifications: %s has an unreadable status.acknowledgedAt %v", path, raw)
		return time.Time{}
	}
	return at.UTC()
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

	branch, vesselID := splitNotificationVessel(path)
	status, live := notificationStatus(value, strings.Split(branch, "."))
	if !live {
		return alarmStatus{}, fmt.Errorf("%w: %s.%s", errNotificationNotLive, notificationsRoot, path)
	}
	// Restored so the status handed back matches the rule id the caller acted
	// on. Without it the frontend cannot reconcile the two.
	if vesselID != "" {
		status.RuleID += notificationVesselSeparator + vesselID
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
	// through the delta stream before the snapshot reflects it. now stands in
	// for the server's acknowledgedAt until that arrives, and the next status
	// read replaces it with the server's own stamp.
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
	// A collision notification is raised on the AIS target's own context rather
	// than on self, and carries that vessel in its rule id (ADR 0057). Every
	// other notification comes back with an empty vessel and reads self.
	branch, vesselID := splitNotificationVessel(path)

	node := snapshot.selfTree()
	if vesselID != "" {
		node = snapshot.treeFor(vesselContextPrefix + vesselID)
	}
	if node == nil {
		return nil, false
	}

	for _, segment := range append([]string{notificationsRoot}, strings.Split(branch, ".")...) {
		child, ok := node[segment].(map[string]any)
		if !ok {
			return nil, false
		}
		node = child
	}

	value, ok := node["value"].(map[string]any)
	return value, ok
}
