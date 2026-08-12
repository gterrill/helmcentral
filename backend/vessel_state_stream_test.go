package main

import (
	"bufio"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/labstack/echo/v4"
)

// streamTestServer serves only the SSE route, against an empty snapshot so
// buildVesselStatePayload resolves immediately.
func streamTestServer(t *testing.T) *httptest.Server {
	t.Helper()
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
	frames := make(chan frame, 1)

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
				return
			}
		}
	}()

	select {
	case got := <-frames:
		if got.event != "vessel-state" {
			t.Fatalf("event name: got %q, want %q", got.event, "vessel-state")
		}
		// The stream must carry the same shape as GET /api/vessel-state.
		for _, key := range []string{`"depth"`, `"latitude"`, `"source"`, `"gnss_validation_state"`} {
			if !strings.Contains(got.data, key) {
				t.Fatalf("event payload missing %s: %s", key, got.data)
			}
		}
	case <-time.After(5 * time.Second):
		t.Fatalf("no vessel-state event within 5s")
	}
}

// Each emitter has its own interval so slow-moving payloads are not rebuilt and
// resent at the cadence depth and wind need. A slow event appearing once per
// tick would mean the per-emitter schedule is being ignored.
func TestTelemetryStreamHonoursPerEmitterIntervals(t *testing.T) {
	server := streamTestServer(t)

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
		// vessel-state carries a per-second datetime, so it should keep flowing.
		if seen["vessel-state"] < 2 {
			t.Fatalf("vessel-state (1s interval) in a 4s window: got %d, want at least 2", seen["vessel-state"])
		}
	case <-time.After(10 * time.Second):
		t.Fatalf("reader did not finish")
	}
}
