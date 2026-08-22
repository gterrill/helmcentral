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

// streamStub is a SignalK stream server standing in for a real one. onConn is
// handed the connection index (1 for the first connection) so reconnect tests
// can behave differently on each attempt.
type streamStub struct {
	server *httptest.Server
	mu     sync.Mutex
	count  int
}

func newStreamStub(onConn func(ctx context.Context, c *websocket.Conn, connIndex int)) *streamStub {
	stub := &streamStub{}
	stub.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := websocket.Accept(w, r, &websocket.AcceptOptions{InsecureSkipVerify: true})
		if err != nil {
			return
		}
		defer conn.CloseNow()

		stub.mu.Lock()
		stub.count++
		index := stub.count
		stub.mu.Unlock()

		onConn(r.Context(), conn, index)
	}))
	return stub
}

func (s *streamStub) wsURL() string {
	return "ws://" + strings.TrimPrefix(s.server.URL, "http://")
}

func (s *streamStub) connections() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.count
}

func (s *streamStub) close() { s.server.Close() }

func writeFrame(ctx context.Context, c *websocket.Conn, payload string) error {
	return c.Write(ctx, websocket.MessageText, []byte(payload))
}

const depthDeltaFrame = `{"context":"vessels.self","updates":[{"timestamp":"2026-08-12T10:00:00.000Z","$source":"n2k.1","values":[{"path":"environment.depth.belowTransducer","value":2.5}]}]}`

// helloFrame is what a SignalK server sends first: server metadata with no
// updates array at all.
const helloFrame = `{"name":"test server","version":"1.0.4","self":"vessels.urn:mrn:signalk:uuid:abc","roles":["master","main"]}`

// waitFor polls until cond holds, failing the test on timeout. Polling beats a
// fixed sleep here: the client applies deltas on its own goroutine.
func waitFor(t *testing.T, timeout time.Duration, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatalf("timed out after %s waiting for %s", timeout, what)
}

func testStreamClient(snapshot *signalKSnapshot) *signalKStreamClient {
	client := newSignalKStreamClient(snapshot, "")
	client.minBackoff = time.Millisecond
	client.maxBackoff = 5 * time.Millisecond
	return client
}

func snapshotDepth(snapshot *signalKSnapshot) float64 {
	tree := snapshot.treeFor("vessels.self")
	if tree == nil {
		return -1
	}
	return lookupNumber(tree, "environment", "depth", "belowTransducer", "value")
}

func TestBuildSignalKStreamURLMapsSchemeAndDisablesImplicitSubscription(t *testing.T) {
	cases := []struct {
		name    string
		address string
		port    int
		want    string
	}{
		{"bare host and port", "100.103.214.1", 3000, "ws://100.103.214.1:3000/signalk/v1/stream?subscribe=none"},
		{"http prefixed address", "http://boat.local:3000", 0, "ws://boat.local:3000/signalk/v1/stream?subscribe=none"},
		{"https prefixed address becomes wss", "https://boat.local", 0, "wss://boat.local/signalk/v1/stream?subscribe=none"},
	}

	for _, tc := range cases {
		got := buildSignalKStreamURL(tc.address, tc.port)
		if got != tc.want {
			t.Fatalf("%s: got %q, want %q", tc.name, got, tc.want)
		}
	}
}

func TestStreamClientAppliesDeltaToSnapshot(t *testing.T) {
	stub := newStreamStub(func(ctx context.Context, c *websocket.Conn, _ int) {
		_ = writeFrame(ctx, c, depthDeltaFrame)
		<-ctx.Done()
	})
	defer stub.close()

	snapshot := newSignalKSnapshot()
	client := testStreamClient(snapshot)

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	go client.connectOnce(ctx, stub.wsURL(), "")

	waitFor(t, 2*time.Second, "depth delta to land", func() bool {
		return snapshotDepth(snapshot) == 2.5
	})
}

// The hello frame carries no updates. Treating it as an error would mean the
// client never survives its own handshake.
func TestStreamClientIgnoresHelloFrameWithoutUpdates(t *testing.T) {
	stub := newStreamStub(func(ctx context.Context, c *websocket.Conn, _ int) {
		_ = writeFrame(ctx, c, helloFrame)
		_ = writeFrame(ctx, c, depthDeltaFrame)
		<-ctx.Done()
	})
	defer stub.close()

	snapshot := newSignalKSnapshot()
	client := testStreamClient(snapshot)

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	go client.connectOnce(ctx, stub.wsURL(), "")

	waitFor(t, 2*time.Second, "delta after hello frame to land", func() bool {
		return snapshotDepth(snapshot) == 2.5
	})
}

// One corrupt frame must not cost the whole connection — reconnecting drops
// every path back to unseen for the backoff duration.
func TestStreamClientSurvivesMalformedFrame(t *testing.T) {
	stub := newStreamStub(func(ctx context.Context, c *websocket.Conn, _ int) {
		_ = writeFrame(ctx, c, "{not json at all")
		_ = writeFrame(ctx, c, depthDeltaFrame)
		<-ctx.Done()
	})
	defer stub.close()

	snapshot := newSignalKSnapshot()
	client := testStreamClient(snapshot)

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	go client.connectOnce(ctx, stub.wsURL(), "")

	waitFor(t, 2*time.Second, "delta after malformed frame to land", func() bool {
		return snapshotDepth(snapshot) == 2.5
	})

	if stub.connections() != 1 {
		t.Fatalf("malformed frame should not have forced a reconnect: got %d connections, want 1", stub.connections())
	}
}

func TestStreamClientReportsConnectedThenDisconnected(t *testing.T) {
	release := make(chan struct{})
	stub := newStreamStub(func(ctx context.Context, c *websocket.Conn, _ int) {
		<-release
		_ = c.Close(websocket.StatusNormalClosure, "done")
	})
	defer stub.close()

	snapshot := newSignalKSnapshot()
	client := testStreamClient(snapshot)

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	go client.connectOnce(ctx, stub.wsURL(), "")

	waitFor(t, 2*time.Second, "connected to be reported", func() bool {
		connected, _ := snapshot.status()
		return connected
	})

	close(release)

	waitFor(t, 2*time.Second, "disconnect to be reported", func() bool {
		connected, _ := snapshot.status()
		return !connected
	})
}

func TestStreamClientReconnectsAfterServerClosesConnection(t *testing.T) {
	stub := newStreamStub(func(ctx context.Context, c *websocket.Conn, index int) {
		if index == 1 {
			_ = c.Close(websocket.StatusNormalClosure, "first connection closes")
			return
		}
		_ = writeFrame(ctx, c, depthDeltaFrame)
		<-ctx.Done()
	})
	defer stub.close()

	snapshot := newSignalKSnapshot()
	client := testStreamClient(snapshot)
	client.resolveURLs = func() (string, string) { return stub.wsURL(), "" }

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	go client.run(ctx)

	waitFor(t, 4*time.Second, "delta on the reconnected session to land", func() bool {
		return snapshotDepth(snapshot) == 2.5
	})

	if stub.connections() < 2 {
		t.Fatalf("client should have reconnected: got %d connections, want >= 2", stub.connections())
	}
}

func TestStreamClientRunReturnsPromptlyOnContextCancel(t *testing.T) {
	stub := newStreamStub(func(ctx context.Context, c *websocket.Conn, _ int) {
		<-ctx.Done()
	})
	defer stub.close()

	snapshot := newSignalKSnapshot()
	client := testStreamClient(snapshot)
	client.resolveURLs = func() (string, string) { return stub.wsURL(), "" }

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		client.run(ctx)
		close(done)
	}()

	waitFor(t, 2*time.Second, "initial connection", func() bool {
		return stub.connections() >= 1
	})

	cancel()

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatalf("run did not return within 2s of context cancellation")
	}
}

func TestStreamClientCapturesSelfContextFromHelloFrame(t *testing.T) {
	stub := newStreamStub(func(ctx context.Context, c *websocket.Conn, _ int) {
		_ = writeFrame(ctx, c, helloFrame)
		_ = writeFrame(ctx, c, `{"context":"vessels.urn:mrn:signalk:uuid:abc","updates":[{"values":[{"path":"environment.depth.belowTransducer","value":4.5}]}]}`)
		<-ctx.Done()
	})
	defer stub.close()

	snapshot := newSignalKSnapshot()
	client := testStreamClient(snapshot)

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	go client.connectOnce(ctx, stub.wsURL(), "")

	waitFor(t, 2*time.Second, "self context and delta to land", func() bool {
		tree := snapshot.selfTree()
		return tree != nil && lookupNumber(tree, "environment", "depth", "belowTransducer", "value") == 4.5
	})
}

// End-to-end: a delta arriving over a real WebSocket reaches the fetcher that
// every telemetry handler calls, with the fetcher's signature unchanged.
func TestStreamDeltaReachesVesselStateFetcher(t *testing.T) {

	stub := newStreamStub(func(ctx context.Context, c *websocket.Conn, _ int) {
		_ = writeFrame(ctx, c, helloFrame)
		_ = writeFrame(ctx, c, `{"context":"vessels.urn:mrn:signalk:uuid:abc","updates":[{"values":[{"path":"environment.depth.belowTransducer","value":8.75}]}]}`)
		<-ctx.Done()
	})
	defer stub.close()

	snapshot := newSignalKSnapshot()
	withGlobalSnapshot(t, snapshot)
	// GNSS validation latches critical across calls in module-level state.
	resetGNSSPositionValidationState()
	t.Cleanup(resetGNSSPositionValidationState)

	client := testStreamClient(snapshot)
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	go client.connectOnce(ctx, stub.wsURL(), "")

	waitFor(t, 2*time.Second, "vessel state fetcher to read the streamed depth", func() bool {
		state, err := fetchSignalKVesselState()
		return err == nil && state.Depth == 8.75
	})
}

// The server pushes far faster than anything here consumes: measured against
// the live boat, subscribe=all delivers ~333 frames/s, with
// navigation.headingMagnetic alone arriving at 40Hz. Every one of those frames
// costs a websocket read and a json.Unmarshal — together 70% of the backend's
// CPU — and is then overwritten before it is read, because the alarm evaluator
// ticks at 1s, anchor drag at 5s, and the SSE telemetry stream at 1s. Nothing
// in the process consumes faster than 1Hz.
//
// So the client subscribes explicitly and rate-limits to 1Hz per path, rather
// than taking the firehose and throwing most of it away.
func TestStreamClientSubscribesWithRateLimit(t *testing.T) {
	received := make(chan string, 1)
	stub := newStreamStub(func(ctx context.Context, c *websocket.Conn, _ int) {
		_, data, err := c.Read(ctx)
		if err != nil {
			return
		}
		select {
		case received <- string(data):
		default:
		}
		<-ctx.Done()
	})
	defer stub.close()

	client := testStreamClient(newSignalKSnapshot())
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	go client.connectOnce(ctx, stub.wsURL(), "")

	var raw string
	select {
	case raw = <-received:
	case <-time.After(2 * time.Second):
		t.Fatal("client never sent a subscription message")
	}

	var msg struct {
		Context   string `json:"context"`
		Subscribe []struct {
			Path      string `json:"path"`
			Policy    string `json:"policy"`
			MinPeriod int    `json:"minPeriod"`
		} `json:"subscribe"`
	}
	if err := json.Unmarshal([]byte(raw), &msg); err != nil {
		t.Fatalf("subscription message is not valid JSON: %v (%q)", err, raw)
	}

	// vessels.*, not vessels.self: nearby-vessels reads other vessels'
	// contexts. They are only 2.3% of frame volume, so narrowing the context
	// would cost that feature everything and save almost nothing.
	if msg.Context != "vessels.*" {
		t.Fatalf("context = %q, want vessels.* so other vessels still arrive", msg.Context)
	}
	if len(msg.Subscribe) != 1 {
		t.Fatalf("want exactly one subscription entry, got %d", len(msg.Subscribe))
	}
	entry := msg.Subscribe[0]
	// path "*" keeps every path, including any the operator has mapped to a
	// widget in settings.yaml. Only the rate is capped, never the coverage.
	if entry.Path != "*" {
		t.Fatalf("path = %q, want * so no configured path is dropped", entry.Path)
	}
	if entry.Policy != "instant" {
		t.Fatalf("policy = %q, want instant so changes still arrive promptly", entry.Policy)
	}
	if entry.MinPeriod != 1000 {
		t.Fatalf("minPeriod = %d, want 1000 to match the 1s consumption rate", entry.MinPeriod)
	}
}

// subscribe=none is what makes the explicit subscription authoritative; left
// at all, the server firehoses regardless of what we then ask for.
func TestBuildSignalKStreamURLDisablesImplicitSubscription(t *testing.T) {
	got := buildSignalKStreamURL("boat.local", 3000)
	if !strings.Contains(got, "subscribe=none") {
		t.Fatalf("stream URL must disable implicit subscription, got %q", got)
	}
}
