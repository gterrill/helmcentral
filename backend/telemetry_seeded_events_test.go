package main

import (
	"os"
	"regexp"
	"sort"
	"strings"
	"testing"
)

// The browser's stream (frontend/src/hooks/use-telemetry-stream.ts) listens
// for every event the hub seeds a connection with, so it can remember the
// payload for a store that starts late (ADR 0218). Its SEEDED_EVENTS list is
// the client's one declaration of those names; this test fails when it and
// telemetryEmitters() drift apart. The heartbeat is not data and is handled
// on its own by the stream.
func TestFrontendSeededEventsMatchTheHub(t *testing.T) {
	src, err := os.ReadFile("../frontend/src/hooks/use-telemetry-stream.ts")
	if err != nil {
		t.Fatalf("read the frontend stream module: %v", err)
	}
	block := regexp.MustCompile(`(?s)const SEEDED_EVENTS = \[(.*?)\] as const`).FindSubmatch(src)
	if block == nil {
		t.Fatal("SEEDED_EVENTS list not found in use-telemetry-stream.ts")
	}
	var client []string
	for _, m := range regexp.MustCompile(`'([^']+)'`).FindAllSubmatch(block[1], -1) {
		client = append(client, string(m[1]))
	}

	var hub []string
	for _, e := range telemetryEmitters() {
		if e.event == "heartbeat" {
			continue
		}
		hub = append(hub, e.event)
	}

	sort.Strings(client)
	sort.Strings(hub)
	if strings.Join(client, ",") != strings.Join(hub, ",") {
		t.Fatalf("SEEDED_EVENTS in use-telemetry-stream.ts differs from telemetryEmitters():\n client: %v\n hub:    %v", client, hub)
	}
}
