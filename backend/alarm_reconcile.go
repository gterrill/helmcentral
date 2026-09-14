package main

import (
	"log"
	"time"
)

// busEchoReconcileRetry bounds how often a clear is retried for the same
// path. A clear that "succeeded" locally but never settled on the bus -- the
// write raced a reconnect, the server was briefly unreachable -- would
// otherwise be retried every evaluator tick; five minutes gives a genuine
// failure room to resolve itself without hammering the bus over it.
const busEchoReconcileRetry = 5 * time.Minute

// busEchoReconciler clears a ghost: a notification live on the SignalK bus at
// a path Helmcentral owns, that this engine is not currently holding live.
// The case this repo has actually hit is a different Helmcentral instance's
// echo left on the shared boat bus, still showing after this instance's rule
// for that path changed or the instance itself changed; the same logic
// equally covers a local rule that raised, then was deleted or renamed while
// its notification was still live.
//
// It mirrors streamWatchdog.reconcile (alarm_watchdog.go) in spirit -- both
// repair a published state that no longer matches this engine's -- but that
// one repairs a single hardcoded path, while the set of paths Helmcentral
// owns changes with the rule set, so this walks every live notification on
// the bus each tick instead.
type busEchoReconciler struct {
	snapshot *signalKSnapshot

	// publish and transportEnabled are injected exactly like
	// signalKNotifyTransport's own put func (alarm_notify.go), so tests never
	// touch the real bus.
	publish          func(path string, value any) error
	transportEnabled func() bool

	lastAttempt    map[string]time.Time
	warnedDisabled map[string]bool
}

func newBusEchoReconciler(snapshot *signalKSnapshot) *busEchoReconciler {
	return &busEchoReconciler{
		snapshot:         snapshot,
		publish:          publishSignalKNotification,
		transportEnabled: func() bool { return getAlarmTransports().SignalK.Enabled },
		lastAttempt:      map[string]time.Time{},
		warnedDisabled:   map[string]bool{},
	}
}

// check walks the live notifications on the bus and clears every ghost: a
// notification whose bare path owned reports true, that is not among
// liveEnginePaths (the Path values of globalAlarmEngine.active()).
func (r *busEchoReconciler) check(now time.Time, owned func(path string) bool, liveEnginePaths map[string]bool) {
	if r.lastAttempt == nil {
		r.lastAttempt = map[string]time.Time{}
	}
	if r.warnedDisabled == nil {
		r.warnedDisabled = map[string]bool{}
	}

	// nodeAt copies only the notifications branch, not the whole self tree
	// selfTree() would -- this runs every alarm tick (backend-perf-audit.md
	// Tier 1 #2).
	root := r.snapshot.nodeAt(notificationsRoot)
	if root == nil {
		return
	}

	// Reuses the same tree walk signalKNotifications does (alarm_notifications.go)
	// rather than a second implementation of it.
	var statuses []alarmStatus
	collectSignalKNotifications(root, nil, &statuses)

	for _, status := range statuses {
		// notificationStatus sets Label to exactly this bare path.
		path := status.Label
		if !owned(path) || liveEnginePaths[path] {
			continue
		}
		r.clearGhost(now, path)
	}
}

func (r *busEchoReconciler) clearGhost(now time.Time, path string) {
	if !r.transportEnabled() {
		// The dev laptop must never write to the boat's bus -- that is the
		// whole reason the transport switch exists -- so a disabled transport
		// means this ghost is only ever reported. Logged once per path so a
		// ghost that persists for days does not fill the log with the same
		// line on every tick.
		if !r.warnedDisabled[path] {
			r.warnedDisabled[path] = true
			log.Printf("owned notification %s is live on the bus but the SignalK transport is disabled, so it cannot be cleared from here", path)
		}
		return
	}

	if last, tried := r.lastAttempt[path]; tried && now.Sub(last) < busEchoReconcileRetry {
		return
	}
	r.lastAttempt[path] = now

	if err := r.publish(notificationsRoot+"."+path, nil); err != nil {
		log.Printf("could not clear ghost notification %s: %v", path, err)
		return
	}
	log.Printf("cleared ghost notification %s: live on the bus, not held by this engine", path)
}
