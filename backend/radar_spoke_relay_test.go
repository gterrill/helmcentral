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

	full, ok := relay.addClient()
	if !ok {
		t.Fatalf("addClient: expected success under the cap")
	}
	for i := 0; i < radarSpokeClientBufferFrames; i++ {
		full.frames <- []byte("filler")
	}

	open, ok := relay.addClient()
	if !ok {
		t.Fatalf("addClient: expected success under the cap")
	}

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

// TestRadarSpokeRelayAddClientRejectsBeyondMaxClients reproduces K-5: with
// no cap, an unbounded number of browser clients could watch one radar's
// spoke stream, each retaining up to radarSpokeClientBufferFrames buffered
// frames -- ten idle tabs alone is already 160MB of frames nobody is
// looking at (radarSpokeRelayMaxClients's own doc comment). The
// radarSpokeRelayMaxClients-th client is accepted; the next must be
// refused, and releasing a slot must let a new client back in.
func TestRadarSpokeRelayAddClientRejectsBeyondMaxClients(t *testing.T) {
	relay := newRadarSpokeRelay("unit-capacity", "")

	var clients []*radarSpokeClient
	for i := 0; i < radarSpokeRelayMaxClients; i++ {
		client, ok := relay.addClient()
		if !ok {
			t.Fatalf("client %d: expected addClient to succeed under radarSpokeRelayMaxClients=%d", i, radarSpokeRelayMaxClients)
		}
		clients = append(clients, client)
	}

	if _, ok := relay.addClient(); ok {
		t.Fatalf("expected addClient to refuse a client beyond radarSpokeRelayMaxClients=%d", radarSpokeRelayMaxClients)
	}

	// Freeing a slot must let a new client back in -- the cap is on
	// concurrent clients, not a one-shot budget.
	relay.removeClient(clients[0])
	if _, ok := relay.addClient(); !ok {
		t.Fatalf("expected addClient to succeed again once a slot was freed")
	}
}

// TestRadarSpokeRelayHandlerRefusesBeyondMaxClients is the end-to-end half
// of K-5: the HTTP handler itself must refuse the client beyond the cap with
// a clear error, the same "visibly unavailable, not silently dropped" shape
// as TestRadarSpokeRelayHandlerRefusesWhenMayaraAddressUnset uses for an
// unconfigured mayara address.
func TestRadarSpokeRelayHandlerRefusesBeyondMaxClients(t *testing.T) {
	stub := newMayaraSpokeStub(func(ctx context.Context, c *websocket.Conn, _ int) {
		<-ctx.Done()
	})
	defer stub.close()
	t.Setenv("SETTINGS_FILE", mayaraSettingsFileForServer(t, stub.url()))
	wsURL := newSpokeRelayTestServer(t)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	var conns []*websocket.Conn
	for i := 0; i < radarSpokeRelayMaxClients; i++ {
		conns = append(conns, dialSpokeClient(t, ctx, wsURL, "at-capacity"))
	}
	defer func() {
		for _, conn := range conns {
			conn.CloseNow()
		}
	}()

	if _, _, err := websocket.Dial(ctx, wsURL+"/api/radar/spokes?radar=at-capacity", nil); err == nil {
		t.Fatalf("expected the client beyond radarSpokeRelayMaxClients=%d to be refused", radarSpokeRelayMaxClients)
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

// TestRadarSpokeRelayNegotiatesCompressionWhenClientOffersItAndFramesStayIdentical
// is the compression half of backend perf audit #5. websocket.Dial with nil
// options (every other test in this file) offers no permessage-deflate
// extension at all, so the default DialOptions{} zero value -- and every
// existing test above -- stays exactly as uncompressed as before this fix;
// only a client that explicitly asks, as this one does, exercises the new
// path.
func TestRadarSpokeRelayNegotiatesCompressionWhenClientOffersItAndFramesStayIdentical(t *testing.T) {
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

	conn, resp, err := websocket.Dial(ctx, wsURL+"/api/radar/spokes?radar=compressed", &websocket.DialOptions{
		CompressionMode: websocket.CompressionContextTakeover,
	})
	if err != nil {
		t.Fatalf("dialing radar compressed: %v", err)
	}
	defer conn.CloseNow()
	conn.SetReadLimit(radarSpokeReadLimit)

	if ext := resp.Header.Get("Sec-WebSocket-Extensions"); !strings.Contains(ext, "permessage-deflate") {
		t.Fatalf("expected the relay to negotiate permessage-deflate when the client offers it, got Sec-WebSocket-Extensions=%q", ext)
	}

	_, got, err := conn.Read(ctx)
	if err != nil {
		t.Fatalf("reading relayed frame: %v", err)
	}
	if !bytes.Equal(got, frame) {
		t.Fatalf("relayed frame diverged under compression: got %d bytes, want %d bytes", len(got), len(frame))
	}
}

// TestServeSpokeClientEndsAWriteThatExceedsTheTimeout is the write-timeout
// half of #5: a client that stops reading without closing must not hold the
// write, and therefore this goroutine, open past writeTimeout. Exercised
// directly against serveSpokeClient (bypassing the relay/registry) with a
// short timeout, since forcing an OS socket buffer to actually stall on a
// real 5s budget would make this test as slow as the bug it guards against.
// The frame is large and the peer never reads at all, so the write is
// guaranteed to still be in flight when writeTimeout fires.
func TestServeSpokeClientEndsAWriteThatExceedsTheTimeout(t *testing.T) {
	e := echo.New()
	var serverConn *websocket.Conn
	accepted := make(chan struct{})
	e.GET("/spoke", func(c echo.Context) error {
		conn, err := websocket.Accept(c.Response().Writer, c.Request(), nil)
		if err != nil {
			return err
		}
		serverConn = conn
		close(accepted)
		<-c.Request().Context().Done()
		return nil
	})
	srv := httptest.NewServer(e)
	defer srv.Close()

	dialCtx, dialCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer dialCancel()
	clientConn, _, err := websocket.Dial(dialCtx, "ws://"+strings.TrimPrefix(srv.URL, "http://")+"/spoke", nil)
	if err != nil {
		t.Fatalf("dialing test server: %v", err)
	}
	defer clientConn.CloseNow()
	// The client deliberately never calls Read again from here on.

	select {
	case <-accepted:
	case <-time.After(2 * time.Second):
		t.Fatal("server never accepted the connection")
	}

	client := &radarSpokeClient{frames: make(chan []byte, 1)}
	// Large and entirely undrained: with nothing reading the socket, this
	// write cannot complete within the short timeout below regardless of the
	// platform's default buffer sizes.
	client.frames <- bytes.Repeat([]byte{0xAA}, 16<<20)

	done := make(chan struct{})
	go func() {
		serveSpokeClient(context.Background(), serverConn, client, 100*time.Millisecond)
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("serveSpokeClient did not return after its write exceeded the timeout; the client, and the upstream it holds open, would be stuck")
	}
}

// TestRadarSpokeRelayRegistry_AcquireReleaseInterleavingDoesNotOrphanARelay
// is the failing-test-first regression guard for the refcount bug (backend
// perf audit #5): handler A acquires, then handler B acquires the same
// relay and releases before ever calling addClient (its Accept failed).
// Sizing teardown off len(relay.clients) tore the relay down here even
// though handler A, still mid-handshake, had not joined yet and was still
// holding it -- A's eventual client would then be attached to a relay whose
// run() had already exited, receiving nothing until its browser reconnected.
func TestRadarSpokeRelayRegistry_AcquireReleaseInterleavingDoesNotOrphanARelay(t *testing.T) {
	frame := []byte("interleave-frame")
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
	settingsPath := mayaraSettingsFileForServer(t, stub.url())

	const radarID = "interleave-radar"

	// Handler A acquires and is still mid-handshake (has not called
	// addClient yet).
	relayA := globalRadarSpokeRelays.acquire(radarID, settingsPath)
	t.Cleanup(func() { globalRadarSpokeRelays.release(radarID) })

	// Handler B acquires the same relay, then its Accept fails and it
	// releases immediately -- also without ever calling addClient.
	relayB := globalRadarSpokeRelays.acquire(radarID, settingsPath)
	if relayB != relayA {
		t.Fatalf("acquire for the same radar id must return the same relay instance")
	}
	globalRadarSpokeRelays.release(radarID)

	if n := spokeRelayCount(); n != 1 {
		t.Fatalf("relay count = %d, want 1: handler A's acquire is still outstanding and must keep the relay alive", n)
	}

	waitFor(t, 2*time.Second, "the upstream to stay connected while A still holds a reference", func() bool {
		return stub.connections() == 1
	})

	// Handler A now finishes its handshake and joins as a client. On the
	// buggy len(clients)==0 teardown this relay would already be gone from
	// the registry (and its run() exited), so this client would receive
	// nothing until it reconnected.
	client, ok := relayA.addClient()
	if !ok {
		t.Fatalf("addClient: expected success under the cap")
	}
	defer relayA.removeClient(client)

	select {
	case frame := <-client.frames:
		_ = frame
	case <-time.After(2 * time.Second):
		t.Fatal("handler A's client never received a frame from the relay it had acquired before B's premature release")
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
