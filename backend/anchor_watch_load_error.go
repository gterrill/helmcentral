package main

import "time"

// anchorWatchLoadErr holds the message GET /api/anchor-watch reports while
// the persisted anchor watch cannot be trusted — set by
// recordAnchorWatchLoadFailure, cleared by clearAnchorWatchLoadFailure.
// Guarded by anchorWatchMu (anchor.go), the same lock as anchorWatchState:
// getAnchorWatch reads the two together, and a load failure always leaves
// anchorWatchState nil, so the pair can never disagree about whether a watch
// is active.
var anchorWatchLoadErr string

const (
	anchorWatchStateRuleID = "anchor-watch-state"
	anchorWatchStateLabel  = "Anchor watch state"

	// anchorWatchStatePath is Helmcentral's own, alongside collisionProfilePath
	// (collision_profile.go) and watchdogPath (alarm_watchdog.go): nothing else
	// on the bus writes here, and the "helmcentral." prefix makes it owned
	// outright by isDerivedPath (alarm_ownership.go) whether or not any stored
	// rule happens to name it.
	anchorWatchStatePath = "notifications.helmcentral.anchorWatch"
)

// recordAnchorWatchLoadFailure puts the anchor watch into its explicit error
// state after a corrupt or invalid anchor_watch.json (loadAnchorWatch
// already refuses to install one as an active watch). Unlike every other
// store main.go opens at startup, this one must not call log.Fatalf: every
// other alarm this backend runs depends on the process staying up, and one
// damaged file is not a reason to take all of them down. Instead:
//
//   - GET /api/anchor-watch reports err (which already names the file path —
//     see loadAnchorWatch) instead of an invented or empty watch.
//   - A "Anchor watch state unreadable" warning is raised through the same
//     alarm/notification mechanism the collision-profile syncer and the
//     stream watchdog use for their own static, non-rule-driven warnings,
//     rather than a log line only someone tailing the console would see.
//
// The bad file itself is left on disk, untouched, by this function. Recovery
// is the operator's: dropping a new anchor overwrites it (setAnchorWatch
// always writes atomically) and an explicit Raise removes it
// (raiseAnchorWatch always does) — both call clearAnchorWatchLoadFailure once
// they succeed.
func recordAnchorWatchLoadFailure(err error) {
	anchorWatchMu.Lock()
	anchorWatchLoadErr = err.Error()
	anchorWatchMu.Unlock()

	recordAlarmEvent(anchorWatchStateEvent(alarmEventRaised, alarmStateWarn,
		"Anchor watch state unreadable: "+err.Error()), time.Now().UTC())
}

// clearAnchorWatchLoadFailure retracts a previously recorded load failure, if
// there was one. Called after anything that leaves the anchor watch file in
// a known-good state — a fresh Drop (which overwrites it) or a Raise (which
// removes it) — never automatically and never on a plain successful startup
// load, so an ordinary Drop/Raise with nothing ever wrong does not spam the
// alarm log with a resolution to a problem that never existed.
func clearAnchorWatchLoadFailure() {
	anchorWatchMu.Lock()
	had := anchorWatchLoadErr != ""
	anchorWatchLoadErr = ""
	anchorWatchMu.Unlock()

	if had {
		recordAlarmEvent(anchorWatchStateEvent(alarmEventCleared, alarmStateNormal, ""), time.Now().UTC())
	}
}

// anchorWatchStateEvent raises or clears Helmcentral's own "the persisted
// anchor watch could not be read" warning — the same static, non-rule-driven
// shape collisionProfileEvent (collision_profile.go) uses for its own.
func anchorWatchStateEvent(kind, state, message string) alarmEvent {
	phase := alarmPhaseActive
	if kind == alarmEventCleared {
		phase = alarmPhaseNormal
	}

	status := alarmStatus{
		RuleID:  anchorWatchStateRuleID,
		Label:   anchorWatchStateLabel,
		Path:    anchorWatchStatePath,
		Phase:   phase,
		State:   state,
		Message: message,
	}
	return alarmEvent{
		Kind: kind,
		Rule: alarmRule{
			ID:      anchorWatchStateRuleID,
			Enabled: true,
			Label:   anchorWatchStateLabel,
			Path:    anchorWatchStatePath,
			State:   state,
		},
		Status: status,
		Source: alarmSourceRule,
	}
}
