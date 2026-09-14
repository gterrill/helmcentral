package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/coder/websocket"
	"github.com/labstack/echo/v4"
)

// radarSpokeUpstreamPathTemplate is mayara-server's own WebSocket endpoint
// for one radar's spoke stream, dialled directly against mayara-server
// rather than through the SignalK connection: unlike
// mayaraCapabilitiesAPIPathTemplate (radar_capabilities.go) and
// mayaraTargetsAPIPathTemplate (radar_source.go), which are the SignalK
// plugin's REST proxy, the plugin returns 404 for spokes (see the plan's
// "Why the backend relays rather than the browser connecting direct"). %s is
// the radar id, e.g. "fur6424A".
const radarSpokeUpstreamPathTemplate = "/signalk/v2/api/vessels/self/radars/%s/spokes"

// radarSpokeReadLimit bounds one upstream WebSocket message. Measured spoke
// frames are ~232KB (testdata/mayara/spokes-fur6424A.md); this mirrors
// signalKStreamReadLimit's reasoning (signalk_stream.go) of leaving headroom
// well beyond the largest frame observed rather than trimming to it exactly.
const radarSpokeReadLimit = 4 << 20

// radarSpokeClientBufferFrames is how many frames a single browser client's
// fan-out channel holds before this relay starts dropping frames for that
// client specifically. See radarSpokeRelay.broadcast for why dropping,
// rather than blocking or growing the buffer, is the deliberate choice here.
const radarSpokeClientBufferFrames = 4

// radarSpokeWriteTimeout bounds each frame write to a browser client. Without
// it a client that goes silent -- asleep, backgrounded, a dead radio -- but
// never closes its socket holds this handler's goroutine, its slot in
// relay.clients, and therefore the shared upstream mayara connection open
// until the OS's own TCP retransmission timeout gives up, which can be
// minutes. Named per the perf audit rather than inlined so the budget is
// visible at a glance; serveSpokeClient takes it as a parameter (rather than
// reading the const directly) so a test can shrink it without waiting out
// the real 5s.
const radarSpokeWriteTimeout = 5 * time.Second

// radarSpokeClient is one connected browser's fan-out channel. frames is
// buffered rather than unbounded so a client that stops reading costs this
// process a small, fixed amount of memory rather than an unbounded amount;
// see radarSpokeRelay.broadcast for the drop policy once it fills.
type radarSpokeClient struct {
	frames chan []byte

	// warnedDrop is set on the first frame dropped for this client, so the
	// log gets exactly one line per client per bad patch rather than one per
	// dropped frame — a client stuck behind a slow tablet radio would
	// otherwise spam the log at the full upstream frame rate. Only ever
	// touched under radarSpokeRelay.mu.
	warnedDrop bool
}

// radarSpokeRelay holds one upstream connection to mayara-server for one
// radar id, fanned out to every connected browser client for that id.
// Ref-counted by radarSpokeRelayRegistry: the upstream opens on the first
// client and closes when the last leaves, mirroring the singleton
// discipline in frontend/src/hooks/use-telemetry-stream.ts.
type radarSpokeRelay struct {
	radarID string

	mu      sync.Mutex
	clients map[*radarSpokeClient]struct{}

	// refs counts callers between acquire and release -- handler
	// invocations, not connected clients. A handler that has acquired the
	// relay but not yet reached addClient (still inside websocket.Accept's
	// handshake) must keep it alive even though it holds no entry in clients
	// yet. Sizing teardown off len(clients) instead let a second handler's
	// failed Accept for the same radar id tear down a relay the first
	// handler had already acquired but not yet joined, orphaning it on a
	// dead upstream until its browser reconnected. Touched only under
	// radarSpokeRelayRegistry.mu, same discipline as cancel below.
	refs int

	// cancel and done are set by radarSpokeRelayRegistry.acquire when the
	// first reference arrives, and used by release to tear the upstream
	// connection down synchronously once the last reference drops: release
	// waits on done before returning, so a caller that reconnects
	// immediately after always gets a fresh upstream rather than racing the
	// old one's teardown.
	cancel context.CancelFunc
	done   chan struct{}

	minBackoff time.Duration
	maxBackoff time.Duration
	now        func() time.Time

	// resolveURL is re-read on every (re)connect attempt, so a settings
	// change takes effect on the next attempt without requiring a restart —
	// the same pattern signalKStreamClient.resolveURLs uses. It returns an
	// explicit error rather than a URL built from an empty host when the
	// mayara address is unconfigured: an optional integration that isn't
	// configured must be visibly unavailable, never a dial against nothing
	// (AGENTS.md fallback policy).
	resolveURL func() (string, error)
}

func newRadarSpokeRelay(radarID, settingsPath string) *radarSpokeRelay {
	return &radarSpokeRelay{
		radarID:    radarID,
		clients:    map[*radarSpokeClient]struct{}{},
		done:       make(chan struct{}),
		minBackoff: defaultStreamMinBackoff,
		maxBackoff: defaultStreamMaxBackoff,
		now:        func() time.Time { return time.Now().UTC() },
		resolveURL: func() (string, error) {
			address, port, err := loadMayaraSettings(settingsPath)
			if err != nil {
				return "", fmt.Errorf("radar spoke relay: settings unreadable: %w", err)
			}
			if address == "" {
				return "", errors.New("radar spoke relay: mayara address is not configured")
			}
			return buildRadarSpokeURL(address, port, radarID), nil
		},
	}
}

// addClient registers a new browser client and returns its fan-out handle.
func (r *radarSpokeRelay) addClient() *radarSpokeClient {
	client := &radarSpokeClient{frames: make(chan []byte, radarSpokeClientBufferFrames)}
	r.mu.Lock()
	r.clients[client] = struct{}{}
	r.mu.Unlock()
	return client
}

func (r *radarSpokeRelay) removeClient(client *radarSpokeClient) {
	r.mu.Lock()
	delete(r.clients, client)
	r.mu.Unlock()
}

// broadcast fans one upstream frame out to every connected client.
//
// This is the deliberate design decision the plan calls out, not an
// oversight: each client's channel is small and non-blocking. If a client's
// buffer is already full — a tablet whose radio, CPU or a busy render loop
// can't keep up — the frame is dropped for that client alone rather than
// blocking this loop (which would stall every other client on the slowest
// one) or growing the channel without bound (a memory leak on a boat
// computer). Spokes are independent: the browser accumulates them into a
// persistent buffer keyed by bearing, so a dropped frame costs one sector of
// one sweep and the next antenna revolution repaints it. A tablet that can't
// keep up degrades to a lower effective frame rate, which on a rotating
// radar picture is barely perceptible. This is also why there is no Go
// protobuf dependency here: decimating server-side would need one, and
// dropping under backpressure gets the same practical outcome for free
// (testdata/mayara/spokes-fur6424A.md, "What this means for the relay").
func (r *radarSpokeRelay) broadcast(frame []byte) {
	r.mu.Lock()
	defer r.mu.Unlock()

	for client := range r.clients {
		select {
		case client.frames <- frame:
		default:
			if !client.warnedDrop {
				client.warnedDrop = true
				log.Printf("radar spoke relay: radar %s client buffer full, dropping frames for it until it catches up", r.radarID)
			}
		}
	}
}

// run blocks until ctx is cancelled, reconnecting to mayara with exponential
// backoff. Every attempt is logged: the fallback policy requires retry
// behaviour to be loud rather than silently sitting on an unreachable radar
// server. Mirrors signalKStreamClient.run (signalk_stream.go) closely.
func (r *radarSpokeRelay) run(ctx context.Context) {
	defer close(r.done)

	backoff := r.minBackoff

	for {
		if ctx.Err() != nil {
			return
		}

		spokeURL, err := r.resolveURL()
		if err != nil {
			log.Printf("radar spoke relay: radar %s cannot resolve upstream URL: %v; retrying in %s", r.radarID, err, backoff)
		} else {
			connectedAt := r.now()
			err = r.connectOnce(ctx, spokeURL)
			if ctx.Err() != nil {
				return
			}
			if r.now().Sub(connectedAt) >= streamStableDuration {
				backoff = r.minBackoff
			}
			log.Printf("radar spoke relay: radar %s upstream %s disconnected (%v); reconnecting in %s", r.radarID, spokeURL, err, backoff)
		}

		select {
		case <-ctx.Done():
			return
		case <-time.After(backoff):
		}

		backoff *= 2
		if backoff > r.maxBackoff {
			backoff = r.maxBackoff
		}
	}
}

// connectOnce dials mayara's spoke endpoint and pumps frames into broadcast
// until the connection fails or ctx is cancelled. It always returns a
// non-nil error describing why it stopped.
func (r *radarSpokeRelay) connectOnce(ctx context.Context, spokeURL string) error {
	conn, _, err := websocket.Dial(ctx, spokeURL, nil)
	if err != nil {
		return err
	}
	defer conn.CloseNow()
	conn.SetReadLimit(radarSpokeReadLimit)

	for {
		_, data, err := conn.Read(ctx)
		if err != nil {
			return err
		}
		r.broadcast(data)
	}
}

// radarSpokeRelayRegistry ref-counts one radarSpokeRelay per radar id: the
// upstream connection opens on the first browser client for that id and
// closes on the last, so N tablets watching the same radar cost mayara one
// connection rather than N.
type radarSpokeRelayRegistry struct {
	mu     sync.Mutex
	relays map[string]*radarSpokeRelay
}

func newRadarSpokeRelayRegistry() *radarSpokeRelayRegistry {
	return &radarSpokeRelayRegistry{relays: map[string]*radarSpokeRelay{}}
}

// globalRadarSpokeRelays is the process-wide registry, alongside
// globalRadarTargetStore and globalSignalKSnapshot (main.go).
var globalRadarSpokeRelays = newRadarSpokeRelayRegistry()

// acquire returns the shared relay for radarID, starting its upstream
// connection if this is the first caller, and counts one reference against
// it. Must be paired with exactly one release call, however the caller's own
// handling of the relay (Accept, addClient, the serve loop) turns out.
func (reg *radarSpokeRelayRegistry) acquire(radarID, settingsPath string) *radarSpokeRelay {
	reg.mu.Lock()
	defer reg.mu.Unlock()

	relay, ok := reg.relays[radarID]
	if !ok {
		relay = newRadarSpokeRelay(radarID, settingsPath)
		reg.relays[radarID] = relay
	}
	relay.refs++

	// cancel is nil only for a relay that has never had its upstream started
	// — a freshly created one, since release always deletes a relay from the
	// map once refs returns to zero, before it can be reused. Gating on
	// cancel alone (rather than also inspecting relay.clients, which
	// addClient/removeClient mutate under relay.mu instead of reg.mu) keeps
	// this whole decision serialized by reg.mu without a second lock.
	if relay.cancel == nil {
		ctx, cancel := context.WithCancel(context.Background())
		relay.cancel = cancel
		go relay.run(ctx)
	}

	return relay
}

// release drops one reference to radarID's relay. When the caller releasing
// is the last reference, the upstream connection is torn down synchronously
// — bounded by the same context-cancel-closes-the-read behaviour
// signalk_stream.go already relies on — and the relay is removed from the
// registry so a later caller builds a fresh one rather than reusing a
// half-torn-down instance.
//
// Reference counted rather than sized off len(relay.clients): a handler
// acquires before it ever calls addClient (websocket.Accept can still fail,
// or simply take a while, in between), so a concurrent handler for the same
// radar id releasing in that window must not tear down a relay this one is
// still holding.
func (reg *radarSpokeRelayRegistry) release(radarID string) {
	reg.mu.Lock()
	defer reg.mu.Unlock()

	relay, ok := reg.relays[radarID]
	if !ok {
		return
	}

	relay.refs--
	if relay.refs > 0 {
		return
	}

	delete(reg.relays, radarID)
	if relay.cancel != nil {
		relay.cancel()
		<-relay.done
	}
}

// loadMayaraSettings reads the mayara-server address and port from
// settings.yaml (main.go's settingsPayload.Mayara), mirroring
// loadSignalKSettings. Unlike loadSignalKSettings, a blank address is
// returned blank rather than defaulted to a host: there is no sane address
// to guess for a radar, and a blank address is exactly how the radar picture
// overlay reports itself as unconfigured (normalizeSettingsPayload,
// signalk.go).
func loadMayaraSettings(settingsPath string) (address string, port int, err error) {
	settings, err := readSettings(settingsPath)
	if err != nil {
		return "", 0, err
	}

	mayaraMap, ok := settings["mayara"].(map[string]any)
	if !ok {
		return "", defaultMayaraPort, nil
	}

	address = strings.TrimSpace(coerceString(mayaraMap["address"]))
	port = coercePort(mayaraMap["port"])
	if port <= 0 || port > 65535 {
		port = defaultMayaraPort
	}

	return address, port, nil
}

// buildMayaraURL mirrors buildSignalKURL (signalk.go): an address already
// carrying a scheme is used as-is (trimmed of a trailing slash), otherwise
// it is paired with port over plain http.
func buildMayaraURL(address string, port int) string {
	trimmed := strings.TrimSpace(address)

	for _, prefix := range []string{"http://", "https://", "ws://", "wss://"} {
		if strings.HasPrefix(trimmed, prefix) {
			return strings.TrimRight(trimmed, "/")
		}
	}

	if port <= 0 || port > 65535 {
		port = defaultMayaraPort
	}
	return fmt.Sprintf("http://%s:%d", trimmed, port)
}

// buildRadarSpokeURL builds the ws(s):// URL for radarID's spoke stream,
// mirroring buildSignalKStreamURL's http(s)->ws(s) scheme mapping
// (signalk_stream.go).
func buildRadarSpokeURL(address string, port int, radarID string) string {
	base := buildMayaraURL(address, port)

	switch {
	case strings.HasPrefix(base, "https://"):
		base = "wss://" + strings.TrimPrefix(base, "https://")
	case strings.HasPrefix(base, "http://"):
		base = "ws://" + strings.TrimPrefix(base, "http://")
	}

	return base + fmt.Sprintf(radarSpokeUpstreamPathTemplate, radarID)
}

// radarSpokeRelayHandler backs GET /api/radar/spokes?radar=<id> (tierRead),
// upgraded to WebSocket. It refuses immediately, before any dial or
// registry entry, when the mayara address is unconfigured: an optional
// integration that isn't set up must be visibly unavailable rather than
// silently degraded (AGENTS.md fallback policy).
func radarSpokeRelayHandler(c echo.Context) error {
	radarID := c.QueryParam("radar")
	if radarID == "" {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "radar query parameter is required"})
	}

	settingsPath := getEnv("SETTINGS_FILE", "../settings.yaml")

	address, _, err := loadMayaraSettings(settingsPath)
	if err != nil {
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": fmt.Sprintf("reading mayara settings: %v", err)})
	}
	if address == "" {
		return c.JSON(http.StatusServiceUnavailable, map[string]string{"error": "mayara address is not configured; set it under Settings > Mayara before enabling the radar overlay"})
	}

	relay := globalRadarSpokeRelays.acquire(radarID, settingsPath)
	defer globalRadarSpokeRelays.release(radarID)

	// CompressionContextTakeover is only negotiated if the browser's own
	// handshake offers permessage-deflate (selectDeflate, coder/websocket's
	// accept.go) -- a client that doesn't gets exactly today's uncompressed
	// behaviour. Spoke frames gzip roughly 45:1 (testdata/mayara's capture
	// notes), against a fixed cost of a 32KB sliding window plus a 1.2MB
	// flate.Writer per connection, which at the handful of concurrent radar
	// viewers this relay ever serves is a good trade for cutting 2-6GB/h on
	// a phone's LTE link down by over an order of magnitude. Every other
	// Accept behaviour (origin checking, in particular) is unchanged: a zero
	// AcceptOptions with only CompressionMode set behaves identically to nil
	// otherwise.
	conn, err := websocket.Accept(c.Response().Writer, c.Request(), &websocket.AcceptOptions{
		CompressionMode: websocket.CompressionContextTakeover,
	})
	if err != nil {
		return err
	}
	defer conn.CloseNow()

	// This handler only ever pushes to the browser; nothing it needs to read
	// back. CloseRead hands off reading to coder/websocket itself, which
	// answers pings and cancels the returned context the moment the client
	// goes away — the correct way to detect a browser disconnect on a
	// send-only connection.
	ctx := conn.CloseRead(context.Background())

	client := relay.addClient()
	defer relay.removeClient(client)

	serveSpokeClient(ctx, conn, client, radarSpokeWriteTimeout)
	return nil
}

// serveSpokeClient pumps frames from client.frames to conn until ctx is
// cancelled (the browser disconnected), the channel closes, or a single
// write exceeds writeTimeout. That last case is what radarSpokeWriteTimeout
// exists for: a client that goes silent without closing must not hold this
// goroutine, and so the relay's reference on it and mayara's upstream
// connection, open indefinitely. writeTimeout is a parameter rather than the
// const read directly so a test can shrink it instead of waiting out the
// real budget.
func serveSpokeClient(ctx context.Context, conn *websocket.Conn, client *radarSpokeClient, writeTimeout time.Duration) {
	for {
		select {
		case <-ctx.Done():
			return
		case frame, ok := <-client.frames:
			if !ok {
				return
			}
			writeCtx, cancel := context.WithTimeout(ctx, writeTimeout)
			err := conn.Write(writeCtx, websocket.MessageBinary, frame)
			cancel()
			if err != nil {
				return
			}
		}
	}
}
