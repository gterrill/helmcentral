package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"sync"
	"time"

	"github.com/labstack/echo/v4"
)

// telemetryStreamTick is the resolution of the hub's driving loop. Each
// event has its own interval, which must be a multiple of this to be
// honoured precisely. A var rather than a const so tests can shrink it to
// drive many simulated ticks quickly instead of waiting on real wall-clock
// time for a hub built with short, test-only event intervals.
var telemetryStreamTick = 1 * time.Second

const (
	// Without traffic an idle SSE connection looks dead to proxies and gets
	// reaped, so send a comment line when nothing has changed for a while.
	telemetryStreamKeepalive = 15 * time.Second

	// telemetryHubSubscriberBuffer is how many pending frames a subscriber's
	// channel can hold before the hub drops it rather than block. 9 events
	// on this hub, so a burst where every one of them is due on the same
	// tick still fits with headroom; a subscriber that can't keep up with
	// that is genuinely stuck (a slow network write, a dead browser tab),
	// not just briefly behind.
	telemetryHubSubscriberBuffer = 32
)

// streamEmitter is one named event type the hub builds and broadcasts.
type streamEmitter struct {
	event    string
	interval time.Duration
	build    func() map[string]any

	// gateKey optionally normalises this event's already-marshalled payload
	// into a comparison key used only to decide whether to broadcast — the
	// hub still sends the real, un-normalised payload every time it does
	// broadcast. nil means the gate compares the raw encoded payload itself,
	// unchanged from before this field existed: correct for heartbeat, which
	// must always send regardless of any comparison, and for events the
	// audit did not find carrying a volatile clock or age field (autopilot,
	// alarms). See telemetry_gate.go for what the real gate keys drop and
	// band, and why.
	gateKey func(encoded []byte) string

	// alwaysSend bypasses the change gate entirely: this event broadcasts
	// on every build it's due for, regardless of what gateKey/payload
	// comparison would otherwise say. Only heartbeat sets it. gateKey's own
	// doc comment used to claim a nil gateKey was enough for heartbeat,
	// reasoning that its RFC3339Nano timestamp always differs between
	// builds - but two builds can format to the identical nanosecond
	// string (a coarser system clock, or two builds landing in the same
	// tick under load), and when they do, comparing the raw payload like
	// any other ungated event wrongly suppresses that heartbeat. This is
	// what made TestTelemetryHeartbeatIsObservableAndPeriodic intermittent
	// (backend perf audit Tier 3): a must-always-send guarantee should not
	// depend on incidental uniqueness of a formatted string.
	alwaysSend bool

	// nextDue is owned by the hub's single driving goroutine (tick) only —
	// nothing else reads or writes it, so it needs no lock of its own.
	nextDue time.Time

	// buildMu is TryLock'd around build() so a build that overruns its own
	// interval (a slow Influx query, say) can never overlap with itself: the
	// hub just skips dispatching a new one for that event until the running
	// one finishes, rather than piling up concurrent queries against
	// whatever is already slow.
	buildMu sync.Mutex
}

// dueAt reports whether e should fire at now, advancing nextDue to the next
// slot at or after now when it does.
//
// nextDue advances from its own previous value, not from now: anchoring to
// now (the original implementation's nextDue = now.Add(interval)) lets any
// per-tick scheduling jitter permanently shift the schedule forward by
// however late that particular now happened to be, and a tick landing with
// less jitter than the one before it then arrives before that shifted
// nextDue and gets skipped — measured on the boat as 1s events actually
// firing every ~2s (gaps of 0.85, 2.00, 2.01, 2.00, 1.00s). Advancing from
// nextDue's own prior value keeps the schedule on a fixed grid that ordinary
// tick jitter cannot drift. A slot missed outright (the process paused for
// longer than one interval) is skipped forward to the next slot at or after
// now rather than fired in a catch-up burst.
func (e *streamEmitter) dueAt(now time.Time) bool {
	if now.Before(e.nextDue) {
		return false
	}
	if e.nextDue.IsZero() {
		e.nextDue = now
	}
	for !e.nextDue.After(now) {
		e.nextDue = e.nextDue.Add(e.interval)
	}
	return true
}

// telemetryEmitters lists what the stream carries and how often each payload is
// re-evaluated. Intervals are deliberately uneven: depth, wind and heading move
// continuously, whereas tank levels and daily solar totals barely move at all,
// and every build re-reads settings from disk. Emission is change-gated on top,
// so these are sampling ceilings rather than event rates.
//
// Weather, tide and place-name are deliberately absent — they are external API
// data behind long TTL caches, not vessel telemetry, and pushing them at this
// cadence would burn work to resend identical cached payloads.
func telemetryEmitters() []*streamEmitter {
	return []*streamEmitter{
		{event: "vessel-state", interval: 1 * time.Second, build: buildVesselStatePayload, gateKey: vesselStateGateKey},
		{event: "autopilot", interval: 1 * time.Second, build: buildAutopilotPayload},
		{event: "alarms", interval: 2 * time.Second, build: buildAlarmsPayload},
		{event: "gauge-values", interval: 1 * time.Second, build: buildGaugeValuesPayload, gateKey: gaugeValuesGateKey},
		{event: "electrical-state", interval: 5 * time.Second, build: buildElectricalStatePayload, gateKey: electricalStateGateKey},
		{event: "nearby-vessels", interval: 5 * time.Second, build: buildNearbyVesselsPayload, gateKey: nearbyVesselsGateKey},
		{event: "radar-targets", interval: 2 * time.Second, build: buildRadarTargetsPayload, gateKey: radarTargetsGateKey},
		{event: "solar-state", interval: 10 * time.Second, build: buildSolarStatePayload, gateKey: solarStateGateKey},
		{event: "tanks-state", interval: 10 * time.Second, build: buildTanksStatePayload, gateKey: tanksStateGateKey},
		// Unlike SSE comment keepalives, this is observable in EventSource
		// JavaScript, and must prove liveness on a quiet boat where nothing
		// else is changing - alwaysSend is what actually guarantees that
		// (see its doc comment on streamEmitter above); the timestamp is
		// just payload content for the client to look at, not what gets it
		// past the gate.
		{event: "heartbeat", interval: telemetryStreamKeepalive, alwaysSend: true, build: func() map[string]any {
			return map[string]any{"timestamp": time.Now().UTC().Format(time.RFC3339Nano)}
		}},
	}
}

// telemetryHubFrame is one encoded event, ready to write straight onto an
// SSE connection.
type telemetryHubFrame struct {
	event   string
	payload string
}

// telemetryHubSubscriber is one connection's mailbox. frames is only ever
// sent to by the hub and only ever received from by the owning handler
// goroutine; done is closed exactly once (removeSubscriber's sync.Once) when
// the subscriber is removed, whether that's the client disconnecting or the
// hub dropping it for a full buffer — it is never sent on, only closed, so
// it is always safe to select on from any goroutine without risking a
// send-on-closed-channel panic against frames.
type telemetryHubSubscriber struct {
	frames chan telemetryHubFrame
	done   chan struct{}

	removeOnce sync.Once
}

// telemetryHub builds each stream event once per interval and fans the
// result out to every subscriber, replacing the previous design where
// telemetryStream built its own full set of emitters per connection — N
// connections meant N times the work for identical output.
type telemetryHub struct {
	events []*streamEmitter

	// mu guards subscribers, cancel and stopped. Held only briefly —
	// adding/removing a subscriber and copying the subscriber set to
	// broadcast — never while a build() call is running.
	mu          sync.Mutex
	subscribers map[*telemetryHubSubscriber]struct{}
	cancel      context.CancelFunc
	// stopped is closed by run() once it (and every build it dispatched,
	// even one still in flight when the last subscriber left) has actually
	// exited. removeSubscriber waits on it after calling cancel, so by the
	// time Unsubscribe/dropSubscriber returns, nothing from this hub is
	// still touching global state — load-bearing for tests that swap
	// globals like globalSignalKSnapshot the moment a test ends.
	stopped chan struct{}

	// framesMu guards frames and gateKeys. Written by the driving goroutine's
	// per-event build dispatches (one at a time per event, serialised by
	// that event's own buildMu) and read by Subscribe to seed a new
	// subscriber; kept across stop/start cycles so a subscriber arriving
	// into an already-running hub always gets something immediately rather
	// than racing the next tick.
	//
	// frames holds the latest built payload per event, updated on every
	// build regardless of whether that build's gate key changed — it seeds
	// new subscribers, and a new subscriber must see this build's real
	// ages, not whatever a build several intervals ago last broadcast
	// (which is all a "only update on change" cache would hold once the
	// gate is doing its job and most builds go unbroadcast).
	//
	// gateKeys holds the last gate key each event broadcast on, which is
	// what buildAndBroadcast actually compares against to decide whether to
	// broadcast — a separate value from the frame itself specifically so
	// that comparison can be normalised (telemetry_gate.go) while frames
	// keeps carrying the real, un-normalised payload.
	framesMu sync.RWMutex
	frames   map[string]string
	gateKeys map[string]string
}

func newTelemetryHub() *telemetryHub {
	return &telemetryHub{
		events:      telemetryEmitters(),
		subscribers: make(map[*telemetryHubSubscriber]struct{}),
		frames:      make(map[string]string),
		gateKeys:    make(map[string]string),
	}
}

// globalTelemetryHub is the production hub every /api/stream connection
// subscribes to. Tests that need isolation from one another swap this out
// for a fresh instance (see freshTelemetryHubForTest in
// vessel_state_stream_test.go), the same pattern withGlobalSnapshot uses for
// globalSignalKSnapshot.
var globalTelemetryHub = newTelemetryHub()

// Subscribe attaches a new subscriber. If the hub has no other subscribers
// right now, it starts a fresh driving goroutine, which immediately runs one
// build pass before its first tick — so this call never seeds a subscriber
// with a stale frame left over from before the hub was last idle; it either
// gets a genuinely fresh build (hub was stopped) or the actually-current
// cached frame (hub was already running).
func (h *telemetryHub) Subscribe() *telemetryHubSubscriber {
	s := &telemetryHubSubscriber{
		frames: make(chan telemetryHubFrame, telemetryHubSubscriberBuffer),
		done:   make(chan struct{}),
	}

	h.mu.Lock()
	wasRunning := h.cancel != nil
	h.subscribers[s] = struct{}{}
	pendingStop := h.stopped
	h.mu.Unlock()

	if wasRunning {
		h.seed(s)
		return s
	}

	h.startFreshRun(pendingStop)
	return s
}

// startFreshRun brings up a new driving goroutine from a stopped state,
// resetting every event's nextDue and the whole frames cache first — so its
// first pass unconditionally builds and broadcasts every event, rather than
// skipping ones whose nextDue is still in the future from the previous run,
// or having the change gate suppress ones whose rebuilt payload happens to
// equal what's still cached from it. Without that reset, a client that
// unsubscribes and immediately resubscribes (a kiosk page reload does this
// within milliseconds) could wait out an event's full interval, or forever
// if its value never changes, before hearing from it again.
//
// pendingStop, if non-nil, is the previous run's completion signal
// (h.stopped as it was when this subscriber was added). It can be non-nil
// and still open here: dropSubscriber never waits for a stop it requests
// (see its doc comment), so a run it stopped can still be winding down —
// finishing its last build, calling wg.Done, returning from run() — when a
// new Subscribe call arrives. Waiting for it here, before touching
// h.events[*].nextDue or h.frames, is what keeps that winding-down
// goroutine and this fresh one from touching that shared state at the same
// time. This must happen without holding h.mu: the winding-down run's own
// build goroutines need it briefly (broadcast's subscriber-list copy) to
// finish up and let pendingStop close at all.
func (h *telemetryHub) startFreshRun(pendingStop chan struct{}) {
	if pendingStop != nil {
		<-pendingStop
	}

	h.mu.Lock()
	if h.cancel != nil {
		// Another Subscribe call already won the race to start a fresh run
		// while this one was waiting above. That run's first pass still
		// covers every current subscriber (this one included — it was
		// added to h.subscribers before either call got here), so there is
		// nothing left to do.
		h.mu.Unlock()
		return
	}

	for _, e := range h.events {
		e.nextDue = time.Time{}
	}
	h.framesMu.Lock()
	h.frames = make(map[string]string)
	h.gateKeys = make(map[string]string)
	h.framesMu.Unlock()

	ctx, cancel := context.WithCancel(context.Background())
	stopped := make(chan struct{})
	h.cancel = cancel
	h.stopped = stopped
	h.mu.Unlock()

	go func() {
		h.run(ctx)
		close(stopped)
	}()
}

// seed queues whatever the hub already has cached for s, in event-list order,
// dropping (not blocking) if a live broadcast has filled the buffer first —
// a brand new subscriber's channel has 9 empty slots against 9 events, so
// that only happens under real concurrent load, and a live frame arriving
// instead of a cached one is a fine substitute.
func (h *telemetryHub) seed(s *telemetryHubSubscriber) {
	h.framesMu.RLock()
	defer h.framesMu.RUnlock()

	for _, e := range h.events {
		payload, ok := h.frames[e.event]
		if !ok {
			continue
		}
		select {
		case s.frames <- telemetryHubFrame{event: e.event, payload: payload}:
		default:
		}
	}
}

// Unsubscribe detaches s, stopping the hub's driving goroutine if s was the
// last subscriber, and waiting for that stop to actually complete before
// returning. Only Unsubscribe may wait like this: it runs on the handler's
// own goroutine, never inside a build dispatch.
func (h *telemetryHub) Unsubscribe(s *telemetryHubSubscriber) {
	h.removeSubscriber(s, true, false)
}

// dropSubscriber removes a subscriber the hub itself gave up on because its
// buffer was full, closing s.done so its handler's read loop wakes up and
// returns — ending the SSE response so the browser's EventSource reconnects
// and gets a fresh subscription rather than one silently stalled forever.
//
// This must never wait for the hub to stop, unlike Unsubscribe. deliver (and
// so dropSubscriber) runs inside a build goroutine that run()'s WaitGroup is
// tracking. If dropping the last subscriber waited here for that same
// WaitGroup's Wait() to return, it would deadlock: run() cannot finish
// Wait() until this goroutine's buildAndBroadcast returns, and this
// goroutine cannot return (so its deferred buildMu.Unlock and wg.Done never
// run) while it is blocked waiting for run() to finish. That would also
// leave the event's buildMu permanently locked, so it could never build
// again even for a later subscriber. Not waiting here is safe: stopping the
// hub is still requested (stop()), just not confirmed before this returns —
// startFreshRun (Subscribe) is what actually needs that confirmation, and it
// waits on h.stopped itself, off this goroutine.
func (h *telemetryHub) dropSubscriber(s *telemetryHubSubscriber) {
	h.removeSubscriber(s, false, true)
}

// removeSubscriber detaches s. If s was the last subscriber, it asks the
// driving goroutine to stop; wait controls whether this call blocks until
// that stop is confirmed complete (Unsubscribe: yes: dropSubscriber: never,
// see its doc comment). h.stopped is deliberately left set (not nil'd)
// here even when wait is true and the wait completes — Subscribe's
// startFreshRun is the one place that channel gets consumed and replaced,
// so a subsequent Subscribe can always find it and wait on it too if a drop
// elsewhere left a run still winding down.
func (h *telemetryHub) removeSubscriber(s *telemetryHubSubscriber, wait bool, logDrop bool) {
	s.removeOnce.Do(func() {
		close(s.done)

		h.mu.Lock()
		delete(h.subscribers, s)
		remaining := len(h.subscribers)
		var stop context.CancelFunc
		var stopped chan struct{}
		if remaining == 0 {
			stop = h.cancel
			stopped = h.stopped
			h.cancel = nil
		}
		h.mu.Unlock()

		if stop != nil {
			stop()
			if wait {
				// Wait for the driving goroutine, and anything it already
				// dispatched, to actually finish before this call returns.
				// Stopping only happens on the rare 1-subscriber-to-0
				// transition, so this is a bounded wait (at most however
				// long the slowest in-flight build takes), not a cost every
				// unsubscribe pays.
				<-stopped
			}
		}
		if logDrop {
			log.Printf("telemetry stream: dropping a subscriber whose buffer filled up rather than stall the rest of the stream")
		}
	})
}

// deliver sends frame to s without ever blocking the caller: a full buffer
// means s is dropped instead.
func (h *telemetryHub) deliver(s *telemetryHubSubscriber, frame telemetryHubFrame) {
	select {
	case s.frames <- frame:
	case <-s.done:
		// Already on its way out (removed by something else concurrently);
		// nothing to deliver to.
	default:
		h.dropSubscriber(s)
	}
}

// broadcast copies the current subscriber set under mu (briefly — no build
// or delivery happens while holding it) and then delivers outside the lock,
// so a slow or stuck subscriber can never hold up Subscribe/Unsubscribe.
func (h *telemetryHub) broadcast(frame telemetryHubFrame) {
	h.mu.Lock()
	subs := make([]*telemetryHubSubscriber, 0, len(h.subscribers))
	for s := range h.subscribers {
		subs = append(subs, s)
	}
	h.mu.Unlock()

	for _, s := range subs {
		h.deliver(s, frame)
	}
}

// run is the hub's driving goroutine: one immediate pass (so the first
// subscriber never waits out a full tick for its first frames), then one
// pass per telemetryStreamTick until ctx is cancelled (the last subscriber
// left). wg tracks every build dispatched from any tick so run can wait for
// all of them to actually finish before returning — without that, a build
// dispatched moments before the last subscriber leaves could still be
// running (and still touching whatever global state it reads) well after
// removeSubscriber's caller thinks the hub has stopped.
//
// Once ctx is cancelled, no further tick may run, even if a ticker value is
// also sitting ready in the same select — select picks pseudo-randomly
// between two ready cases, so without the explicit check below a tick could
// still slip in after cancellation. That matters because startFreshRun
// (Subscribe) treats h.stopped closing as its signal that nothing is left
// touching h.events[*].nextDue or h.frames before it resets them; a
// just-cancelled run ticking one more time would race that reset.
func (h *telemetryHub) run(ctx context.Context) {
	var wg sync.WaitGroup
	h.tick(time.Now(), &wg)

	ticker := time.NewTicker(telemetryStreamTick)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			wg.Wait()
			return
		case now := <-ticker.C:
			select {
			case <-ctx.Done():
				wg.Wait()
				return
			default:
			}
			h.tick(now, &wg)
		}
	}
}

// tick dispatches a build for every event due at now. Each dispatch runs in
// its own goroutine specifically so one slow build (buildMu already
// prevents it overlapping itself) does not delay the others in the same
// tick, and so the driving loop is never itself blocked on a build — it
// only ever does the due-check/nextDue bookkeeping before moving on.
func (h *telemetryHub) tick(now time.Time, wg *sync.WaitGroup) {
	for _, e := range h.events {
		if !e.dueAt(now) {
			continue
		}
		wg.Add(1)
		go func(e *streamEmitter) {
			defer wg.Done()
			h.buildAndBroadcast(e)
		}(e)
	}
}

func (h *telemetryHub) buildAndBroadcast(e *streamEmitter) {
	if !e.buildMu.TryLock() {
		// A previous build for this exact event is still running (it
		// overran its own interval); skip this dispatch rather than run a
		// second one concurrently. The event just tries again next time
		// dueAt reports it due.
		return
	}
	defer e.buildMu.Unlock()

	encoded, err := json.Marshal(e.build())
	if err != nil {
		return
	}
	payload := string(encoded)

	key := payload
	if e.gateKey != nil {
		key = e.gateKey(encoded)
	}

	h.framesMu.Lock()
	// frames always takes this build's real payload, whether or not the
	// gate below decides to broadcast it — see the field's doc comment on
	// why a "changed" cache would go stale under gating.
	h.frames[e.event] = payload
	previousKey, had := h.gateKeys[e.event]
	changed := e.alwaysSend || !had || previousKey != key
	if changed {
		h.gateKeys[e.event] = key
	}
	h.framesMu.Unlock()

	if !changed {
		return
	}
	h.broadcast(telemetryHubFrame{event: e.event, payload: payload})
}

// telemetryStream pushes vessel telemetry to the browser over Server-Sent Events.
//
// SSE rather than a WebSocket: the traffic is strictly one-way, EventSource
// gives reconnection and Last-Event-ID resumption for free, and it survives the
// authenticating reverse proxy the README recommends for remote access.
//
// Events are named so one connection carries every payload type — the browser
// opens a single stream rather than one per hook. Every payload itself is
// built once per interval by globalTelemetryHub, not once per connection —
// this handler only subscribes and writes whatever it's handed.
func telemetryStream(c echo.Context) error {
	response := c.Response()
	header := response.Header()
	header.Set("Content-Type", "text/event-stream")
	header.Set("Cache-Control", "no-cache")
	header.Set("Connection", "keep-alive")
	// Defeats proxy response buffering, which otherwise withholds events until
	// a buffer fills — fatal for a stream whose whole point is latency.
	header.Set("X-Accel-Buffering", "no")
	response.WriteHeader(http.StatusOK)

	ctx := c.Request().Context()

	sub := globalTelemetryHub.Subscribe()
	defer globalTelemetryHub.Unsubscribe(sub)

	keepalive := time.NewTicker(telemetryStreamKeepalive)
	defer keepalive.Stop()
	lastWrite := time.Now()

	for {
		select {
		case <-ctx.Done():
			return nil
		case <-sub.done:
			// The hub dropped this subscriber (its buffer filled). Ending
			// the response lets the browser's EventSource reconnect and
			// subscribe fresh, same as any other end of stream.
			return nil
		case frame := <-sub.frames:
			if _, err := fmt.Fprintf(response, "event: %s\ndata: %s\n\n", frame.event, frame.payload); err != nil {
				// A write error means the client is gone; that is an
				// ordinary end to the request, not a failure worth
				// returning to Echo.
				return nil
			}
			response.Flush()
			lastWrite = time.Now()
		case now := <-keepalive.C:
			if now.Sub(lastWrite) < telemetryStreamKeepalive {
				continue
			}
			if _, err := fmt.Fprint(response, ": keepalive\n\n"); err != nil {
				return nil
			}
			response.Flush()
			lastWrite = now
		}
	}
}
