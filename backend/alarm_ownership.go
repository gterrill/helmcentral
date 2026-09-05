package main

// helmcentralOwnsPath reports whether path -- a bare SignalK path, with no
// "notifications." root -- is one Helmcentral itself writes onto the bus.
//
// There are two routes to ownership. A path under the derived-value namespace
// (derivedPathPrefix, derived_paths.go) is owned outright: nothing but
// Helmcentral ever publishes there, so a live notification under it is an
// echo of a Helmcentral alarm whether or not the rule that raised it is still
// configured on THIS instance -- exactly the case for a notification a
// different Helmcentral install left on the shared boat bus. Anything else is
// owned only if an enabled rule on this engine currently watches that exact
// path: a disabled rule has retracted nothing already on the bus, but this
// engine no longer claims the path, so a live notification there may
// legitimately be some other producer's.
func helmcentralOwnsPath(path string, rules []alarmRule) bool {
	if isDerivedPath(path) {
		return true
	}
	for _, rule := range rules {
		if rule.Enabled && rule.Path == path {
			return true
		}
	}
	return false
}

// helmcentralOwnershipPredicate builds the ownership check from the same rule
// set evaluateAlarmsOnce evaluates every tick -- stored rules plus the ones
// derived from gauge zones (ADR 0050) -- so "owned" never drifts from what
// this engine is actually watching.
func helmcentralOwnershipPredicate() func(path string) bool {
	rules := append(listAlarmRules(), zoneDerivedAlarmRules()...)
	return func(path string) bool {
		return helmcentralOwnsPath(path, rules)
	}
}
