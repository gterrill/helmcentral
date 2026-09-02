package main

import (
	"bufio"
	"bytes"
	"compress/gzip"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/labstack/echo/v4"
)

// mayaraSpokeStub is a mayara-server spoke WebSocket endpoint standing in for
// the real one, in the shape of streamStub (signalk_stream_test.go): onConn
// is handed the connection index (1 for the first connection) so reconnect
// tests can behave differently on each attempt, and a counter records how
// many upstream connections were actually opened, which is how the
// single-upstream-per-radar contract gets asserted.
type mayaraSpokeStub struct {
	server *httptest.Server
	mu     sync.Mutex
	count  int
}

func newMayaraSpokeStub(onConn func(ctx context.Context, c *websocket.Conn, connIndex int)) *mayaraSpokeStub {
	stub := &mayaraSpokeStub{}
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

func (s *mayaraSpokeStub) url() string { return s.server.URL }

func (s *mayaraSpokeStub) connections() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.count
}

func (s *mayaraSpokeStub) close() { s.server.Close() }

// mayaraSettingsFileForServer writes a temp settings.yaml whose mayara block
// points at serverURL. buildMayaraURL accepts a full http:// URL as address
// directly, mirroring settingsFileForServer's use of buildSignalKURL
// (tracks_test.go).
func mayaraSettingsFileForServer(t *testing.T, serverURL string) string {
	t.Helper()
	content := []byte("mayara:\n  address: " + serverURL + "\n  port: 0\n")
	dir := t.TempDir()
	path := dir + "/settings.yaml"
	if err := os.WriteFile(path, content, 0o644); err != nil {
		t.Fatalf("could not write temp settings: %v", err)
	}
	return path
}

// newSpokeRelayTestServer wires radarSpokeRelayHandler onto a real listener:
// the WebSocket upgrade needs an actual hijackable connection, which
// httptest.NewRecorder cannot provide.
func newSpokeRelayTestServer(t *testing.T) string {
	t.Helper()
	e := echo.New()
	e.GET("/api/radar/spokes", radarSpokeRelayHandler)
	srv := httptest.NewServer(e)
	t.Cleanup(srv.Close)
	return "ws://" + strings.TrimPrefix(srv.URL, "http://")
}

// spokeRelayCount reports how many radar ids currently have a live relay,
// so teardown/reopen tests can observe the registry emptying out.
func spokeRelayCount() int {
	globalRadarSpokeRelays.mu.Lock()
	defer globalRadarSpokeRelays.mu.Unlock()
	return len(globalRadarSpokeRelays.relays)
}

// loadFirstSpokeFrame decodes the first captured frame from
// testdata/mayara/spokes-fur6424A.jsonl.gz: gzipped, one JSON record per
// WebSocket message ({"t":...,"b64":...}), per the Phase 0 capture notes
// (testdata/mayara/spokes-fur6424A.md). Used so the verbatim-relay test
// exercises a real frame rather than a hand-authored one.
func loadFirstSpokeFrame(t *testing.T) []byte {
	t.Helper()
	f, err := os.Open("testdata/mayara/spokes-fur6424A.jsonl.gz")
	if err != nil {
		t.Fatalf("opening spoke fixture: %v", err)
	}
	defer f.Close()

	gz, err := gzip.NewReader(f)
	if err != nil {
		t.Fatalf("opening gzip reader: %v", err)
	}
	defer gz.Close()

	scanner := bufio.NewScanner(gz)
	// A captured frame is roughly 232KB, base64-inflated to ~310KB; the
	// scanner's default 64KB buffer is too small for a single line of that.
	scanner.Buffer(make([]byte, 0, 64*1024), 1<<20)
	if !scanner.Scan() {
		t.Fatalf("spoke fixture has no records")
	}

	var record struct {
		T   string `json:"t"`
		B64 string `json:"b64"`
	}
	if err := json.Unmarshal(scanner.Bytes(), &record); err != nil {
		t.Fatalf("decoding fixture record: %v", err)
	}

	frame, err := base64.StdEncoding.DecodeString(record.B64)
	if err != nil {
		t.Fatalf("decoding base64 frame: %v", err)
	}
	if len(frame) == 0 {
		t.Fatalf("decoded fixture frame is empty")
	}
	return frame
}

// dialSpokeClient dials radar/spokes for radarID and raises the client's own
// read limit to match the server's, since captured frames are well over
// coder/websocket's 32KiB default.
func dialSpokeClient(t *testing.T, ctx context.Context, wsURL, radarID string) *websocket.Conn {
	t.Helper()
	conn, _, err := websocket.Dial(ctx, wsURL+"/api/radar/spokes?radar="+radarID, nil)
	if err != nil {
		t.Fatalf("dialing radar %s: %v", radarID, err)
	}
	conn.SetReadLimit(radarSpokeReadLimit)
	return conn
}

func TestRadarSpokeRelaySingleClientReceivesFramesVerbatim(t *testing.T) {
	frame := []byte("single-client-frame-payload")
	stub := newMayaraSpokeStub(func(ctx context.Context, c *websocket.Conn, _ int) {
		_ = c.Write(ctx, websocket.MessageBinary, frame)
		<-ctx.Done()
	})
	defer stub.close()
	t.Setenv("SETTINGS_FILE", mayaraSettingsFileForServer(t, stub.url()))
	wsURL := newSpokeRelayTestServer(t)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	conn := dialSpokeClient(t, ctx, wsURL, "single-client")
	defer conn.CloseNow()

	_, got, err := conn.Read(ctx)
	if err != nil {
		t.Fatalf("reading relayed frame: %v", err)
	}
	if !bytes.Equal(got, frame) {
		t.Fatalf("relayed frame = %q, want %q", got, frame)
	}
}

func TestRadarSpokeRelayTwoClientsShareOneUpstreamConnection(t *testing.T) {
	frame := []byte("shared-upstream-frame")
	stub := newMayaraSpokeStub(func(ctx context.Context, c *websocket.Conn, _ int) {
		ticker := time.NewTicker(5 * time.Millisecond)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				if err := c.Write(ctx, websocket.MessageBinary, frame); err != nil {
					return
				}
			}
		}
	})
	defer stub.close()
	t.Setenv("SETTINGS_FILE", mayaraSettingsFileForServer(t, stub.url()))
	wsURL := newSpokeRelayTestServer(t)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	conn1 := dialSpokeClient(t, ctx, wsURL, "two-clients")
	defer conn1.CloseNow()
	conn2 := dialSpokeClient(t, ctx, wsURL, "two-clients")
	defer conn2.CloseNow()

	got1, got2 := make(chan struct{}), make(chan struct{})
	go func() {
		if _, _, err := conn1.Read(ctx); err == nil {
			close(got1)
		}
	}()
	go func() {
		if _, _, err := conn2.Read(ctx); err == nil {
			close(got2)
		}
	}()

	for name, ch := range map[string]chan struct{}{"client 1": got1, "client 2": got2} {
		select {
		case <-ch:
		case <-time.After(3 * time.Second):
			t.Fatalf("%s never received a frame", name)
		}
	}

	if got := stub.connections(); got != 1 {
		t.Fatalf("upstream connections = %d, want 1 (two browser clients for the same radar must share one upstream)", got)
	}
}

func TestRadarSpokeRelayUpstreamClosesOnLastClientAndReopensForNext(t *testing.T) {
	stub := newMayaraSpokeStub(func(ctx context.Context, c *websocket.Conn, _ int) {
		<-ctx.Done()
	})
	defer stub.close()
	t.Setenv("SETTINGS_FILE", mayaraSettingsFileForServer(t, stub.url()))
	wsURL := newSpokeRelayTestServer(t)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	conn1 := dialSpokeClient(t, ctx, wsURL, "reopen")
	waitFor(t, 2*time.Second, "first upstream connection", func() bool {
		return stub.connections() == 1
	})

	conn1.CloseNow()
	waitFor(t, 2*time.Second, "relay to tear down once its last client leaves", func() bool {
		return spokeRelayCount() == 0
	})

	conn2 := dialSpokeClient(t, ctx, wsURL, "reopen")
	defer conn2.CloseNow()
	waitFor(t, 2*time.Second, "a later client to open a fresh upstream connection", func() bool {
		return stub.connections() == 2
	})
}

// TestRadarSpokeRelaySlowClientDoesNotBlockFastClient is the backpressure
// contract and the most important test in this file: a client that stops
// reading must degrade to a lower frame rate for itself only, never stall
// the fan-out for every other client. The stub writes continuously; the slow
// client dials once and then never reads again, while the fast client keeps
// draining its socket and must keep making progress well within the
// deadline.
func TestRadarSpokeRelaySlowClientDoesNotBlockFastClient(t *testing.T) {
	frame := bytes.Repeat([]byte{0xAA}, 1024)
	stop := make(chan struct{})
	stub := newMayaraSpokeStub(func(ctx context.Context, c *websocket.Conn, _ int) {
		ticker := time.NewTicker(2 * time.Millisecond)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-stop:
				return
			case <-ticker.C:
				if err := c.Write(ctx, websocket.MessageBinary, frame); err != nil {
					return
				}
			}
		}
	})
	defer stub.close()
	defer close(stop)
	t.Setenv("SETTINGS_FILE", mayaraSettingsFileForServer(t, stub.url()))
	wsURL := newSpokeRelayTestServer(t)

	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()

	fast := dialSpokeClient(t, ctx, wsURL, "backpressure")
	defer fast.CloseNow()
	slow := dialSpokeClient(t, ctx, wsURL, "backpressure")
	defer slow.CloseNow()
	// slow never reads again past this point.

	const wantFrames = 100
	var fastCount int64
	done := make(chan struct{})
	go func() {
		defer close(done)
		for {
			if _, _, err := fast.Read(ctx); err != nil {
				return
			}
			if atomic.AddInt64(&fastCount, 1) >= wantFrames {
				return
			}
		}
	}()

	select {
	case <-done:
	case <-time.After(6 * time.Second):
		t.Fatalf("fast client only received %d/%d frames before timing out; a slow client must not block it", atomic.LoadInt64(&fastCount), wantFrames)
	}
}

// TestRadarSpokeRelayBroadcastDropsForFullClientWithoutBlocking is the unit-
// level half of the backpressure contract: with one client's buffer already
// full, broadcasting a frame must return immediately (not block on that
// client) and must leave the full client's buffer exactly as it was, while
// an open client still receives the frame normally.
func TestRadarSpokeRelayBroadcastDropsForFullClientWithoutBlocking(t *testing.T) {
	relay := newRadarSpokeRelay("unit-backpressure", "")

	full := relay.addClient()
	for i := 0; i < radarSpokeClientBufferFrames; i++ {
		full.frames <- []byte("filler")
	}

	open := relay.addClient()

	done := make(chan struct{})
	go func() {
		relay.broadcast([]byte("frame"))
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("broadcast blocked on a full client buffer instead of dropping the frame for it")
	}

	select {
	case got := <-open.frames:
		if string(got) != "frame" {
			t.Fatalf("open client received %q, want %q", got, "frame")
		}
	default:
		t.Fatal("open client never received the frame that was broadcast alongside the full one")
	}

	if n := len(full.frames); n != radarSpokeClientBufferFrames {
		t.Fatalf("full client's buffer length = %d, want unchanged at %d (the frame must be dropped, not appended)", n, radarSpokeClientBufferFrames)
	}
}

// TestRadarSpokeRelayReconnectsWithBackoffAfterUpstreamDrop mirrors
// TestStreamClientReconnectsAfterServerClosesConnection (signalk_stream_test.go):
// the relay's upstream connection is dialled and pumped directly via run(),
// bypassing the HTTP handler, with a millisecond-scale backoff so the test
// stays fast.
func TestRadarSpokeRelayReconnectsWithBackoffAfterUpstreamDrop(t *testing.T) {
	stub := newMayaraSpokeStub(func(ctx context.Context, c *websocket.Conn, index int) {
		if index == 1 {
			_ = c.Close(websocket.StatusNormalClosure, "first connection closes")
			return
		}
		<-ctx.Done()
	})
	defer stub.close()

	relay := newRadarSpokeRelay("reconnect-radar", "")
	relay.minBackoff = time.Millisecond
	relay.maxBackoff = 5 * time.Millisecond
	spokeURL := "ws://" + strings.TrimPrefix(stub.url(), "http://") + fmt.Sprintf(radarSpokeUpstreamPathTemplate, "reconnect-radar")
	relay.resolveURL = func() (string, error) { return spokeURL, nil }

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	go relay.run(ctx)

	waitFor(t, 4*time.Second, "the relay to reconnect after the first connection closes", func() bool {
		return stub.connections() >= 2
	})
}

// TestRadarSpokeRelayHandlerRefusesWhenMayaraAddressUnset pins the fallback
// policy (AGENTS.md): an unset mayara address must be refused with a clear
// error, never a silent dial against an empty host. No relay may even be
// created, since that is what "no dial attempted" means here.
func TestRadarSpokeRelayHandlerRefusesWhenMayaraAddressUnset(t *testing.T) {
	dir := t.TempDir()
	path := dir + "/settings.yaml"
	if err := os.WriteFile(path, []byte("mayara:\n  address: \"\"\n  port: 6502\n"), 0o644); err != nil {
		t.Fatalf("writing settings: %v", err)
	}
	t.Setenv("SETTINGS_FILE", path)

	e := echo.New()
	req := httptest.NewRequest(http.MethodGet, "/api/radar/spokes?radar=unset-addr", nil)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)

	if err := radarSpokeRelayHandler(c); err != nil {
		t.Fatalf("handler returned error: %v", err)
	}

	if rec.Code == http.StatusSwitchingProtocols {
		t.Fatalf("handler upgraded to a websocket despite an unconfigured mayara address")
	}
	if rec.Code < 400 {
		t.Fatalf("status = %d, want a client/server error status", rec.Code)
	}
	if !strings.Contains(strings.ToLower(rec.Body.String()), "mayara") {
		t.Fatalf("error body does not mention mayara: %s", rec.Body.String())
	}

	if n := spokeRelayCount(); n != 0 {
		t.Fatalf("relay count = %d, want 0: a relay must never be created (and therefore no dial attempted) for an unconfigured radar", n)
	}
}

func TestRadarSpokeRelayRelaysCapturedFrameVerbatim(t *testing.T) {
	frame := loadFirstSpokeFrame(t)

	stub := newMayaraSpokeStub(func(ctx context.Context, c *websocket.Conn, _ int) {
		_ = c.Write(ctx, websocket.MessageBinary, frame)
		<-ctx.Done()
	})
	defer stub.close()
	t.Setenv("SETTINGS_FILE", mayaraSettingsFileForServer(t, stub.url()))
	wsURL := newSpokeRelayTestServer(t)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	conn := dialSpokeClient(t, ctx, wsURL, "verbatim")
	defer conn.CloseNow()

	_, got, err := conn.Read(ctx)
	if err != nil {
		t.Fatalf("reading relayed frame: %v", err)
	}
	if !bytes.Equal(got, frame) {
		t.Fatalf("relayed frame diverged from the captured fixture: got %d bytes, want %d bytes", len(got), len(frame))
	}
}
