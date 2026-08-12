package main

import (
	"fmt"
)

// globalSignalKSnapshot is fed by the delta stream client (ADR 0037). It is the
// only source of vessel data; there is no REST read path.
var globalSignalKSnapshot = newSignalKSnapshot()

// signalKSelfPayload returns the self vessel tree in the same nested shape the
// SignalK REST API returns, reassembled from the delta stream.
//
// There is deliberately no fallback. A stream carrying no data is an outage and
// must surface as one (AGENTS.md fallback policy); callers route the error into
// criticalVesselState, which freezes position at the last trusted fix so the
// anchor alarm cannot mistake an outage for the vessel moving.
func signalKSelfPayload() (map[string]any, error) {
	tree := globalSignalKSnapshot.selfTree()
	if tree == nil {
		return nil, fmt.Errorf("signalk unreachable: stream has no self vessel data")
	}
	return tree, nil
}

// signalKVesselsPayload returns every known vessel tree, keyed by bare vessel id
// to match what GET /signalk/v1/api/vessels used to return.
func signalKVesselsPayload() (map[string]any, error) {
	vessels := globalSignalKSnapshot.vesselsTree()
	if len(vessels) == 0 {
		return nil, fmt.Errorf("signalk unreachable: stream has no vessel data")
	}
	return vessels, nil
}
