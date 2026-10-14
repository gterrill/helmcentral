package main

import (
	"fmt"
)

// globalSignalKSnapshot is fed by the delta stream client (ADR 0037). It is the
// only source of vessel data; there is no REST read path.
var globalSignalKSnapshot = newSignalKSnapshot()

// errSignalKNoSelf is every own-boat fetcher's answer while the self vessel's
// telemetry view has no tree (telemetryView.HasSelf), i.e. before the
// stream's hello frame has named the self vessel or after it has carried no
// data.
//
// There is deliberately no fallback. A stream carrying no data is an outage and
// must surface as one (AGENTS.md fallback policy); fetchSignalKVesselState
// routes the error into criticalVesselState, which freezes position at the
// last trusted fix so the anchor alarm cannot mistake an outage for the
// vessel moving.
var errSignalKNoSelf = fmt.Errorf("signalk unreachable: stream has no self vessel data")

// signalKVesselsPayload returns every known vessel tree, keyed by bare vessel id
// to match what GET /signalk/v1/api/vessels used to return.
func signalKVesselsPayload() (map[string]any, error) {
	vessels := globalSignalKSnapshot.vesselsTree()
	if len(vessels) == 0 {
		return nil, fmt.Errorf("signalk unreachable: stream has no vessel data")
	}
	return vessels, nil
}
