package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/coder/websocket"
)

// publishStub is a stand-in SignalK server: it accepts a delta-stream
// WebSocket, records what is written to it, and serves the REST model that
// confirmation reads back.
type publishStub struct {
	mu      sync.Mutex
	frames  [][]byte
	authHdr string
	model   string
	modelSt int
	ingest  bool // whether a published delta becomes visible in the model
	server  *httptest.Server
}

func newPublishStub(t *testing.T) *publishStub {
	t.Helper()
	stub := &publishStub{modelSt: http.StatusNotFound, model: "", ingest: true}

	stub.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/signalk/v1/stream") {
			stub.mu.Lock()
			stub.authHdr = r.Header.Get("Authorization")
			stub.mu.Unlock()

			conn, err := websocket.Accept(w, r, nil)
			if err != nil {
				return
			}
			defer conn.CloseNow()

			ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
			defer cancel()
			for {
				_, data, err := conn.Read(ctx)
				if err != nil {
					return
				}
				stub.mu.Lock()
				stub.frames = append(stub.frames, data)
				if stub.ingest {
					stub.applyLocked(data)
				}
				stub.mu.Unlock()
			}
		}

		if r.URL.Path == "/signalk/v1/auth/login" {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"token":"service-jwt","timeToLive":86400}`))
			return
		}

		stub.mu.Lock()
		status, body := stub.modelSt, stub.model
		stub.mu.Unlock()
		w.WriteHeader(status)
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(stub.server.Close)
	return stub
}

// applyLocked mirrors what a real server does with an accepted delta: the
// value becomes readable at the path it names.
func (s *publishStub) applyLocked(frame []byte) {
	var delta signalKDelta
	if err := json.Unmarshal(frame, &delta); err != nil {
		return
	}
	for _, update := range delta.Updates {
		for _, value := range update.Values {
			if value.Value == nil {
				// What a real signalk-server does with a null notification: the
				// key is NOT removed. The notifications API normalises it to
				// state "normal" with an empty method, which is SignalK's
				// cleared state. Verified against signalk-server 2.24.0 — an
				// earlier version of this stub deleted the key instead, and the
				// resulting test passed while clears failed on a real server.
				encoded, _ := json.Marshal(map[string]any{
					"value": map[string]any{"state": "normal", "method": []any{}, "message": ""},
				})
				s.modelSt, s.model = http.StatusOK, string(encoded)
				continue
			}
			encoded, _ := json.Marshal(map[string]any{"value": value.Value})
			s.modelSt, s.model = http.StatusOK, string(encoded)
		}
	}
}

func (s *publishStub) captured() [][]byte {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([][]byte(nil), s.frames...)
}

func (s *publishStub) settings(t *testing.T) string {
	t.Helper()
	return settingsFileForServer(t, s.server.URL)
}

func withServiceAccount(t *testing.T, stub *publishStub) {
	t.Helper()
	invalidateSignalKToken()
	t.Cleanup(invalidateSignalKToken)
	t.Setenv("SIGNALK_USERNAME", "helmcentral-service")
	t.Setenv("SIGNALK_PASSWORD", "service-secret")
	t.Setenv("SETTINGS_FILE", stub.settings(t))
}

// Helmcentral publishes its own alarms as a producer on the bus, the way every
// other SignalK producer does. The REST write path it used before reached no
// endpoint the server serves.
func TestPublishSignalKNotificationSendsADelta(t *testing.T) {
	stub := newPublishStub(t)
	withServiceAccount(t, stub)

	value := map[string]any{"state": "alarm", "message": "House bank low", "method": []string{"visual", "sound"}}
	if err := publishSignalKNotification("notifications.electrical.batteries.house.voltage", value); err != nil {
		t.Fatalf("publishSignalKNotification: %v", err)
	}

	frames := stub.captured()
	if len(frames) != 1 {
		t.Fatalf("expected exactly one delta, got %d", len(frames))
	}

	var delta signalKDelta
	if err := json.Unmarshal(frames[0], &delta); err != nil {
		t.Fatalf("the frame must be a delta: %v (%s)", err, frames[0])
	}
	if len(delta.Updates) != 1 || len(delta.Updates[0].Values) != 1 {
		t.Fatalf("expected one update carrying one value, got %+v", delta)
	}
	got := delta.Updates[0].Values[0]
	if got.Path != "notifications.electrical.batteries.house.voltage" {
		t.Fatalf("path: got %q", got.Path)
	}
	if _, ok := got.Value.(map[string]any); !ok {
		t.Fatalf("value: expected the notification object, got %T", got.Value)
	}

	// Context is left unset so the server files it under the connection's own
	// vessel; a wrong explicit context would publish onto another boat.
	//
	// Asserted on the raw bytes, not the decoded struct: an absent key and an
	// empty string both decode to "", and the difference is the entire bug this
	// guards. signalk-server silently drops a delta carrying "context": "",
	// which is what marshalling signalKDelta produced before its Context field
	// gained omitempty — the delta was accepted by the socket and discarded by
	// the server, which is exactly the silent failure this transport is built
	// to make impossible.
	var wire map[string]any
	if err := json.Unmarshal(frames[0], &wire); err != nil {
		t.Fatalf("decode frame: %v", err)
	}
	if _, present := wire["context"]; present {
		t.Fatalf(`the delta must omit "context" entirely, not send it empty; got %s`, frames[0])
	}
	if _, present := wire["self"]; present {
		t.Fatalf(`"self" is a server-to-client field and must not be published; got %s`, frames[0])
	}
}

func TestPublishSignalKNotificationAuthenticatesAsTheServiceAccount(t *testing.T) {
	stub := newPublishStub(t)
	withServiceAccount(t, stub)

	if err := publishSignalKNotification("notifications.test", map[string]any{"state": "alert"}); err != nil {
		t.Fatalf("publishSignalKNotification: %v", err)
	}

	stub.mu.Lock()
	auth := stub.authHdr
	stub.mu.Unlock()
	if auth == "" || !strings.HasPrefix(auth, "Bearer ") {
		t.Fatalf("the publish connection must carry the service account's token, got %q", auth)
	}
}

// The regression this whole change exists to prevent. A WebSocket write
// succeeds locally whether or not the server accepts the delta, so publishing
// that only wrote and returned nil reported success it had not achieved — which
// is how the old transport went its entire life delivering nothing.
func TestPublishSignalKNotificationFailsWhenTheServerDoesNotIngestIt(t *testing.T) {
	stub := newPublishStub(t)
	stub.mu.Lock()
	stub.ingest = false
	stub.mu.Unlock()
	withServiceAccount(t, stub)

	err := publishSignalKNotification("notifications.test", map[string]any{"state": "alert"})
	if err == nil {
		t.Fatalf("a delta the server never accepted must not report success")
	}
	if !strings.Contains(err.Error(), "notifications.test") {
		t.Fatalf("the error should name the path that failed, got: %v", err)
	}
}

// SignalK clears a notification by writing null, so confirmation for a clear is
// the path going away rather than arriving.
// Clearing is confirmed by the notification no longer being live, not by its
// key disappearing. signalk-server keeps the key and normalises it to state
// "normal" — demanding absence fails against every real server.
func TestPublishSignalKNotificationConfirmsAClear(t *testing.T) {
	stub := newPublishStub(t)
	withServiceAccount(t, stub)

	if err := publishSignalKNotification("notifications.test", map[string]any{"state": "alert"}); err != nil {
		t.Fatalf("raise: %v", err)
	}
	if err := publishSignalKNotification("notifications.test", nil); err != nil {
		t.Fatalf("clear: %v", err)
	}
}

// A raise is only confirmed once the notification is actually live. A server
// that answers with a cleared notification has not accepted the raise, and
// reporting success there would be the silent delivery failure again.
func TestPublishSignalKNotificationRejectsARaiseThatReadsBackCleared(t *testing.T) {
	stub := newPublishStub(t)
	stub.mu.Lock()
	stub.ingest = false
	stub.modelSt = http.StatusOK
	stub.model = `{"value":{"state":"normal","method":[],"message":""}}`
	stub.mu.Unlock()
	withServiceAccount(t, stub)

	err := publishSignalKNotification("notifications.test", map[string]any{"state": "alarm"})
	if err == nil {
		t.Fatalf("a notification that reads back as cleared has not been raised")
	}
}
