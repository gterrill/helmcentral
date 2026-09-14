package main

import (
	"bufio"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/labstack/echo/v4"
)

// freshTelemetryHubForTest swaps globalTelemetryHub for a brand new instance
// (no subscribers, no cached frames, no nextDue schedule carried over),
// mirroring withGlobalSnapshot's swap-and-restore pattern. Without this,
// tests in this file would share one hub for the lifetime of the test
// binary: a subscriber left over from a previous test (still tearing down
// asynchronously) would make Subscribe() think the hub is already running
// and seed the new test's client with the previous test's cached frames
// instead of a guaranteed-fresh build, and per-emitter-interval assertions
// like "tanks-state fires exactly once in this 4s window" would see whatever
// the previous test's hub happened to be mid-cycle on.
func freshTelemetryHubForTest(t *testing.T) {
	t.Helper()
	original := globalTelemetryHub
	globalTelemetryHub = newTelemetryHub()
	t.Cleanup(func() { globalTelemetryHub = original })
}

// streamTestServer serves only the SSE route, against an empty snapshot so
// buildVesselStatePayload resolves immediately.
func streamTestServer(t *testing.T) *httptest.Server {
	t.Helper()
	freshTelemetryHubForTest(t)
	withGlobalSnapshot(t, newSignalKSnapshot())
	// An empty snapshot drives GNSS validation critical, and that state latches
	// in module-level globals until several good samples clear it.
	resetGNSSPositionValidationState()
	t.Cleanup(resetGNSSPositionValidationState)

	e := echo.New()
	e.GET("/api/stream", telemetryStream)

	server := httptest.NewServer(e)
	t.Cleanup(server.Close)
	return server
}

func TestTelemetryStreamSetsServerSentEventHeaders(t *testing.T) {
	server := streamTestServer(t)

	request, err := http.NewRequest(http.MethodGet, server.URL+"/api/stream", nil)
	if err != nil {
		t.Fatalf("building request: %v", err)
	}

	response, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Fatalf("GET /api/stream: %v", err)
	}
	defer response.Body.Close()

	if got := response.Header.Get("Content-Type"); !strings.HasPrefix(got, "text/event-stream") {
		t.Fatalf("Content-Type: got %q, want text/event-stream", got)
	}
	if got := response.Header.Get("Cache-Control"); got != "no-cache" {
		t.Fatalf("Cache-Control: got %q, want no-cache", got)
	}
	// Proxy buffering would hold events back and defeat the whole point.
	if got := response.Header.Get("X-Accel-Buffering"); got != "no" {
		t.Fatalf("X-Accel-Buffering: got %q, want no", got)
	}
}

// The hub builds every due event of a tick concurrently (one goroutine per
// event, deliberately — see streamEmitter.buildMu's doc comment) so a slow
// build cannot delay the others, which means events no longer necessarily
// arrive in telemetryEmitters' list order the way a single connection's own
// serial pump() used to guarantee. This reads frames until vessel-state
// turns up rather than assuming it is first.
func TestTelemetryStreamEmitsNamedVesselStateEvent(t *testing.T) {
	server := streamTestServer(t)

	response, err := http.Get(server.URL + "/api/stream")
	if err != nil {
		t.Fatalf("GET /api/stream: %v", err)
	}
	defer response.Body.Close()

	type frame struct {
		event string
		data  string
	}
	frames := make(chan frame, 16)

	go func() {
		scanner := bufio.NewScanner(response.Body)
		event := ""
		for scanner.Scan() {
			line := scanner.Text()
			switch {
			case strings.HasPrefix(line, "event: "):
				event = strings.TrimPrefix(line, "event: ")
			case strings.HasPrefix(line, "data: "):
				frames <- frame{event: event, data: strings.TrimPrefix(line, "data: ")}
			}
		}
	}()

	deadline := time.After(5 * time.Second)
	for {
		select {
		case got := <-frames:
			if got.event != "vessel-state" {
				continue
			}
			// The stream must carry the same shape as GET /api/vessel-state.
			for _, key := range []string{`"depth"`, `"latitude"`, `"source"`, `"gnss_validation_state"`} {
				if !strings.Contains(got.data, key) {
					t.Fatalf("event payload missing %s: %s", key, got.data)
				}
			}
			return
		case <-deadline:
			t.Fatalf("no vessel-state event within 5s")
		}
	}
}

// Each emitter has its own interval so slow-moving payloads are not rebuilt and
// resent at the cadence depth and wind need. A slow event appearing once per
// tick would mean the per-emitter schedule is being ignored.
// TestTelemetryStreamHonoursPerEmitterIntervals proves interval-honouring
// end-to-end through the real handler and hub. It cannot use vessel-state's
// own 1s cadence to do that any more: this test's bare snapshot gives
// buildVesselStatePayload nothing to actually change, so once gated
// (Tier 1 #4) it correctly stops rebroadcasting after its first frame — the
// same thing it now does on a quiet boat, which is the whole point of the
// fix. A synthetic ungated event with a value that genuinely changes every
// build stands in for "a 1s emitter that should keep flowing" instead.
func TestTelemetryStreamHonoursPerEmitterIntervals(t *testing.T) {
	server := streamTestServer(t)

	var fastCalls int32
	globalTelemetryHub.events = append(globalTelemetryHub.events, &streamEmitter{
		event:    "test-fast",
		interval: 1 * time.Second,
		build: func() map[string]any {
			return map[string]any{"n": atomic.AddInt32(&fastCalls, 1)}
		},
	})

	response, err := http.Get(server.URL + "/api/stream")
	if err != nil {
		t.Fatalf("GET /api/stream: %v", err)
	}
	defer response.Body.Close()

	counts := make(chan map[string]int, 1)
	go func() {
		scanner := bufio.NewScanner(response.Body)
		seen := map[string]int{}
		deadline := time.Now().Add(4 * time.Second)
		for scanner.Scan() && time.Now().Before(deadline) {
			line := scanner.Text()
			if strings.HasPrefix(line, "event: ") {
				seen[strings.TrimPrefix(line, "event: ")]++
			}
		}
		counts <- seen
	}()

	select {
	case seen := <-counts:
		// tanks-state is on a 10s interval, so within a 4s window it can only
		// have fired its initial emission.
		if seen["tanks-state"] != 1 {
			t.Fatalf("tanks-state (10s interval) in a 4s window: got %d, want 1", seen["tanks-state"])
		}
		// The synthetic event's value genuinely changes every build, so
		// nothing gates it and it should keep flowing at its 1s cadence.
		if seen["test-fast"] < 2 {
			t.Fatalf("test-fast (1s interval, ungated, always changing) in a 4s window: got %d, want at least 2", seen["test-fast"])
		}
		// vessel-state's own content is static here, so past its first
		// frame the gate should now correctly hold it back.
		if seen["vessel-state"] > 1 {
			t.Fatalf("vessel-state in a 4s window with nothing real changing: got %d, want at most 1 (the gate should suppress the rest)", seen["vessel-state"])
		}
	case <-time.After(10 * time.Second):
		t.Fatalf("reader did not finish")
	}
}

// TestTelemetryEmittersIncludeRadarTargets guards the one-line wiring in
// telemetryEmitters (ADR 0062 phase 3): radar targets move roughly once per
// antenna rotation (1.25-2.5s, see radarTargetMaxAge in radar_store.go), so
// 2s keeps up without forcing the slower AIS nearby-vessels emitter's
// resend cadence onto a much busier payload.
func TestTelemetryEmittersIncludeRadarTargets(t *testing.T) {
	for _, emitter := range telemetryEmitters() {
		if emitter.event != "radar-targets" {
			continue
		}
		if emitter.interval != 2*time.Second {
			t.Fatalf("radar-targets interval = %v, want 2s", emitter.interval)
		}
		return
	}
	t.Fatal("telemetryEmitters() does not include a radar-targets emitter")
}

func TestTelemetryHeartbeatIsObservableAndPeriodic(t *testing.T) {
	for _, emitter := range telemetryEmitters() {
		if emitter.event != "heartbeat" {
			continue
		}
		if emitter.interval != 15*time.Second {
			t.Fatalf("heartbeat cadence: %v", emitter.interval)
		}
		first := emitter.build()["timestamp"]
		if first == nil || emitter.build()["timestamp"] == first {
			t.Fatal("heartbeat must carry fresh data so change gating cannot suppress it")
		}
		return
	}
	t.Fatal("missing observable heartbeat (SSE comments cannot drive the browser watchdog)")
}

// ── buildAutopilotPayload ───────────────────────────────────────────────────

func autopilotDelta(context string, now time.Time, values map[string]any) signalKDelta {
	vals := make([]signalKValue, 0, len(values))
	for path, value := range values {
		vals = append(vals, signalKValue{Path: path, Value: value})
	}
	return signalKDelta{
		Context: context,
		Updates: []signalKUpdate{{
			Timestamp: now.Format(time.RFC3339),
			SourceRef: "autopilot-provider.0",
			Values:    vals,
		}},
	}
}

// Absence is not a value: a stream with no steering.autopilot.* at all must
// report present:false, never a synthesized disengaged pilot (mirrors the
// alarm engine's and gauge-values' treatment of missing paths).
func TestBuildAutopilotPayload_AbsentWhenNoAutopilotOnStream(t *testing.T) {
	snapshot := newSignalKSnapshot()
	snapshot.applyDelta(depthDelta("vessels.self", 2.0), time.Now())
	snapshot.setSelfContext("vessels.self")
	withGlobalSnapshot(t, snapshot)

	payload := buildAutopilotPayload()
	if present, _ := payload["present"].(bool); present {
		t.Fatalf("expected present:false, got %+v", payload)
	}
	if _, hasEngaged := payload["engaged"]; hasEngaged {
		t.Fatalf("must not synthesize an engaged field when absent, got %+v", payload)
	}
}

func TestBuildAutopilotPayload_AbsentOnEmptySnapshot(t *testing.T) {
	withGlobalSnapshot(t, newSignalKSnapshot())

	payload := buildAutopilotPayload()
	if present, _ := payload["present"].(bool); present {
		t.Fatalf("expected present:false on an empty snapshot, got %+v", payload)
	}
}

func TestBuildAutopilotPayload_ReportsLiveStateFromDeltaStream(t *testing.T) {
	snapshot := newSignalKSnapshot()
	snapshot.applyDelta(autopilotDelta("vessels.self", time.Now(), map[string]any{
		"steering.autopilot.engaged":          true,
		"steering.autopilot.state":            "auto",
		"steering.autopilot.mode":             "compass",
		"steering.autopilot.target":           135.5,
		"steering.autopilot.availableActions": []any{"disengage", "tack"},
	}), time.Now())
	snapshot.setSelfContext("vessels.self")
	withGlobalSnapshot(t, snapshot)

	payload := buildAutopilotPayload()
	if present, _ := payload["present"].(bool); !present {
		t.Fatalf("expected present:true, got %+v", payload)
	}
	if engaged, _ := payload["engaged"].(bool); !engaged {
		t.Fatalf("expected engaged:true, got %+v", payload)
	}
	if payload["state"] != "auto" || payload["mode"] != "compass" {
		t.Fatalf("expected state/mode to reflect the delta stream, got %+v", payload)
	}
	if payload["target"] != 135.5 {
		t.Fatalf("expected target 135.5, got %v", payload["target"])
	}
	actions, ok := payload["available_actions"].([]string)
	if !ok || len(actions) != 2 || actions[0] != "disengage" || actions[1] != "tack" {
		t.Fatalf("expected available_actions [disengage tack], got %+v", payload["available_actions"])
	}
	if stale, _ := payload["stale"].(bool); stale {
		t.Fatalf("expected stale:false for a just-received update, got %+v", payload)
	}
}

// Stale state must be visible: if steering.autopilot.* goes quiet while the
// tile last knew it was engaged, the payload must say stale rather than let
// the frontend keep trusting a frozen "engaged: true".
func TestBuildAutopilotPayload_StaleWhenSteeringDataGoesQuiet(t *testing.T) {
	snapshot := newSignalKSnapshot()
	longAgo := time.Now().Add(-30 * time.Second)
	snapshot.applyDelta(autopilotDelta("vessels.self", longAgo, map[string]any{
		"steering.autopilot.engaged": true,
		"steering.autopilot.state":   "auto",
	}), longAgo)
	snapshot.setSelfContext("vessels.self")
	withGlobalSnapshot(t, snapshot)

	payload := buildAutopilotPayload()
	if present, _ := payload["present"].(bool); !present {
		t.Fatalf("expected present:true (last known pilot), got %+v", payload)
	}
	if stale, _ := payload["stale"].(bool); !stale {
		t.Fatalf("expected stale:true once steering.autopilot.* has gone quiet, got %+v", payload)
	}
	// Stale must not be reported as disengaged — that's still a claim about
	// steering the payload cannot back up once the source has gone quiet.
	if engaged, _ := payload["engaged"].(bool); !engaged {
		t.Fatalf("expected last-known engaged:true to still be reported (as stale, not silently flipped), got %+v", payload)
	}
}

func TestTelemetryStreamEmitsAutopilotEvent(t *testing.T) {
	server := streamTestServer(t)

	response, err := http.Get(server.URL + "/api/stream")
	if err != nil {
		t.Fatalf("GET /api/stream: %v", err)
	}
	defer response.Body.Close()

	type frame struct {
		event string
		data  string
	}
	frames := make(chan frame, 8)

	go func() {
		scanner := bufio.NewScanner(response.Body)
		event := ""
		for scanner.Scan() {
			line := scanner.Text()
			switch {
			case strings.HasPrefix(line, "event: "):
				event = strings.TrimPrefix(line, "event: ")
			case strings.HasPrefix(line, "data: "):
				frames <- frame{event: event, data: strings.TrimPrefix(line, "data: ")}
			}
		}
	}()

	deadline := time.After(5 * time.Second)
	for {
		select {
		case got := <-frames:
			if got.event == "autopilot" {
				if !strings.Contains(got.data, `"present":false`) {
					t.Fatalf("expected present:false on an empty snapshot, got %s", got.data)
				}
				return
			}
		case <-deadline:
			t.Fatalf("no autopilot event within 5s")
		}
	}
}
