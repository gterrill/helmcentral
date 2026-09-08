package main

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"time"
)

// notificationSyncInterval is how often the notifications subtree is
// reconciled against the server's REST tree (ADR 0086). Nothing else ever
// re-reads the server: the snapshot is fed only by the delta stream, so a
// notification the server has forgotten -- a restart, a clear the
// instant+minPeriod stream dropped, its 60-120s clean() sweep -- stays live
// in Helmcentral until that exact path happens to change again. Half a
// minute is the cost of catching that without hammering the server.
const notificationSyncInterval = 30 * time.Second

// notificationSyncer periodically GETs each held vessel context's
// notifications tree and reconciles it against the snapshot
// (signalKSnapshot.reconcileNotifications), so a notification the server no
// longer carries leaves Helmcentral's copy too.
type notificationSyncer struct {
	snapshot *signalKSnapshot
	interval time.Duration

	// fetch reads GET /signalk/v1/api/vessels/<vesselID>/notifications. found
	// is false when the server answered 404 (no such context, or no
	// notifications under it). Any other non-2xx status is an error carrying
	// the status and body verbatim. Injectable so tests never reach the
	// network.
	fetch func(vesselID string) (tree map[string]any, found bool, err error)

	lastRun time.Time

	// failing tracks an outage across ticks so the log carries one line for
	// the whole streak, not one every 30s the server stays down, and one
	// "reachable again" line when it recovers.
	failing bool
}

func newNotificationSyncer(snapshot *signalKSnapshot) *notificationSyncer {
	return &notificationSyncer{
		snapshot: snapshot,
		interval: notificationSyncInterval,
		fetch:    fetchSignalKNotificationsTree,
	}
}

// invalidate forces the next check to run regardless of how recently the
// last one did -- used after a failed bus action (alarm_service.go) so the
// stale card leaves the list on the next tick rather than waiting out the
// rest of the interval.
func (y *notificationSyncer) invalidate() {
	y.lastRun = time.Time{}
}

// check re-reads the self context plus every other vessel context currently
// holding a live notification, and reconciles each against the snapshot.
//
// It returns nothing: unlike globalBusNotificationWatcher, this is not
// itself an alarm source. It only corrects the tree the watcher reads;
// wiring it to run immediately before the watcher (evaluateAlarmsOnce,
// alarm_service.go) is what turns a correction into a clear on the same tick.
func (y *notificationSyncer) check(now time.Time) {
	if y.interval <= 0 {
		y.interval = notificationSyncInterval
	}
	if now.Sub(y.lastRun) < y.interval {
		return
	}
	y.lastRun = now

	self := y.snapshot.selfContext()
	if self == "" {
		// No hello yet: there is nothing to reconcile against.
		return
	}

	contexts := []string{self}
	var others []string
	for _, ctx := range y.snapshot.knownContexts() {
		if ctx == self {
			continue
		}
		notifications, ok := y.snapshot.treeFor(ctx)[notificationsRoot].(map[string]any)
		if !ok {
			continue
		}
		var live []alarmStatus
		collectSignalKNotifications(notifications, nil, &live)
		if len(live) == 0 {
			continue
		}
		others = append(others, ctx)
	}
	sort.Strings(others)
	contexts = append(contexts, others...)

	for _, ctx := range contexts {
		vesselID := "self"
		if ctx != self {
			vesselID = strings.TrimPrefix(ctx, vesselContextPrefix)
		}

		before := y.snapshot.treeFor(ctx)
		readStartedAt := now

		tree, found, err := y.fetch(vesselID)
		if err != nil {
			if !y.failing {
				log.Printf("notification sync: %v", err)
			}
			y.failing = true
			// The remaining contexts would each wait out the client timeout
			// against a server that is down; nothing is gained by trying them.
			return
		}
		if y.failing {
			log.Printf("notification sync: signalk reachable again")
			y.failing = false
		}

		var server map[string]any
		if found {
			server = tree
		}

		changed := y.snapshot.reconcileNotifications(ctx, server, readStartedAt)
		if len(changed) == 0 {
			continue
		}

		after := y.snapshot.treeFor(ctx)
		for _, path := range changed {
			wasLive, _ := notificationLeafLiveAt(before, path)
			nowLive, present := notificationLeafLiveAt(after, path)
			if !present {
				log.Printf("notification sync: %s notifications.%s corrected against the server (removed, was live: %v)", ctx, path, wasLive)
				continue
			}
			log.Printf("notification sync: %s notifications.%s corrected against the server (was live: %v, now live: %v)", ctx, path, wasLive, nowLive)
		}
	}
}

// notificationLeafLiveAt reports whether the notification leaf at a dotted
// path (relative to "notifications") within a context tree is present and
// live, for the sync's own logging -- it needs both facts about a leaf that
// reconcileNotifications has already applied and can no longer be asked for.
func notificationLeafLiveAt(tree map[string]any, path string) (live, present bool) {
	node, ok := tree[notificationsRoot].(map[string]any)
	if !ok {
		return false, false
	}

	segments := strings.Split(path, ".")
	for i, segment := range segments {
		child, ok := node[segment].(map[string]any)
		if !ok {
			return false, false
		}
		if i < len(segments)-1 {
			node = child
			continue
		}
		if _, hasValue := child["value"]; !hasValue {
			return false, false
		}
		return leafNotificationIsLive(child), true
	}
	return false, false
}

// fetchSignalKNotificationsTree GETs one vessel's notifications subtree,
// following the postSignalKNotificationAction idiom (alarm_notifications.go).
func fetchSignalKNotificationsTree(vesselID string) (map[string]any, bool, error) {
	settingsPath := getEnv("SETTINGS_FILE", "../settings.yaml")
	address, port, err := loadSignalKSettings(settingsPath)
	if err != nil {
		return nil, false, fmt.Errorf("could not read the SignalK connection settings: %w", err)
	}

	path := "/signalk/v1/api/vessels/" + url.PathEscape(vesselID) + "/notifications"
	status, body, err := signalkRequestJSONWithAuthBody(
		buildSignalKURL(address, port), settingsPath,
		path, http.MethodGet, nil,
	)
	if err != nil {
		return nil, false, err
	}
	if status == http.StatusNotFound {
		return nil, false, nil
	}
	if status < 200 || status >= 300 {
		return nil, false, fmt.Errorf("signalk returned status %d: %s", status, string(body))
	}

	var tree map[string]any
	if err := json.Unmarshal(body, &tree); err != nil {
		return nil, false, fmt.Errorf("could not decode the notifications tree: %w", err)
	}
	return tree, true, nil
}
