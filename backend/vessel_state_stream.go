package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/labstack/echo/v4"
)

const (
	// Resolution of the stream loop. Each emitter has its own interval, which
	// must be a multiple of this to be honoured precisely.
	telemetryStreamTick = 1 * time.Second

	// Without traffic an idle SSE connection looks dead to proxies and gets
	// reaped, so send a comment line when nothing has changed for a while.
	telemetryStreamKeepalive = 15 * time.Second
)

// streamEmitter is one named event type on the shared stream.
type streamEmitter struct {
	event    string
	interval time.Duration
	build    func() map[string]any

	last    string
	nextDue time.Time
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
		{event: "vessel-state", interval: 1 * time.Second, build: buildVesselStatePayload},
		{event: "autopilot", interval: 1 * time.Second, build: buildAutopilotPayload},
		{event: "alarms", interval: 2 * time.Second, build: buildAlarmsPayload},
		{event: "gauge-values", interval: 1 * time.Second, build: buildGaugeValuesPayload},
		{event: "electrical-state", interval: 5 * time.Second, build: buildElectricalStatePayload},
		{event: "nearby-vessels", interval: 5 * time.Second, build: buildNearbyVesselsPayload},
		{event: "solar-state", interval: 10 * time.Second, build: buildSolarStatePayload},
		{event: "tanks-state", interval: 10 * time.Second, build: buildTanksStatePayload},
	}
}

// telemetryStream pushes vessel telemetry to the browser over Server-Sent Events.
//
// SSE rather than a WebSocket: the traffic is strictly one-way, EventSource
// gives reconnection and Last-Event-ID resumption for free, and it survives the
// authenticating reverse proxy the README recommends for remote access.
//
// Events are named so one connection carries every payload type — the browser
// opens a single stream rather than one per hook.
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
	ticker := time.NewTicker(telemetryStreamTick)
	defer ticker.Stop()

	emitters := telemetryEmitters()
	lastWrite := time.Now()

	// A write error means the client is gone; that is an ordinary end to the
	// request, not a failure worth returning to Echo.
	pump := func(now time.Time) bool {
		wrote := false

		for _, emitter := range emitters {
			if now.Before(emitter.nextDue) {
				continue
			}
			emitter.nextDue = now.Add(emitter.interval)

			encoded, err := json.Marshal(emitter.build())
			if err != nil {
				continue
			}

			payload := string(encoded)
			if payload == emitter.last {
				continue
			}
			emitter.last = payload

			if _, err := fmt.Fprintf(response, "event: %s\ndata: %s\n\n", emitter.event, payload); err != nil {
				return false
			}
			wrote = true
		}

		if wrote {
			response.Flush()
			lastWrite = now
			return true
		}

		if now.Sub(lastWrite) >= telemetryStreamKeepalive {
			if _, err := fmt.Fprint(response, ": keepalive\n\n"); err != nil {
				return false
			}
			response.Flush()
			lastWrite = now
		}

		return true
	}

	if !pump(time.Now()) {
		return nil
	}

	for {
		select {
		case <-ctx.Done():
			return nil
		case now := <-ticker.C:
			if !pump(now) {
				return nil
			}
		}
	}
}
