package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/coder/websocket"
)

// subscribe=none, then an explicit subscription sent on connect (see
// streamSubscription). The server's built-in modes are all-or-nothing on rate:
// subscribe=all measured at ~333 frames/s against the live boat, with
// navigation.headingMagnetic alone at 40Hz. Every frame costs a websocket read
// and a json.Unmarshal — together 70% of this process's CPU — and is then
// overwritten before anything reads it, because nothing here consumes faster
// than 1Hz (alarm evaluator 1s, anchor drag 5s, SSE telemetry stream 1s).
// Asking for what we actually use is the only way to say that.
const signalKStreamPath = "/signalk/v1/stream?subscribe=none"

// streamSubscription is sent immediately after the handshake. It caps the rate
// without narrowing coverage:
//
//   - context vessels.* keeps other vessels, which nearby-vessels needs. They
//     are only ~2.3% of frame volume, so narrowing to vessels.self would cost
//     that feature everything and save almost nothing.
//   - path "*" keeps every path, including any the operator has mapped to a
//     widget in settings.yaml. Only the rate is capped, never the coverage.
//   - policy instant with minPeriod still delivers a change as soon as it
//     happens, just no more than once per second per path. policy fixed was
//     measured slower to no benefit (210 frames/s against instant's 159).
const streamSubscribeMinPeriodMS = 1000

type signalKSubscribeEntry struct {
	Path      string `json:"path"`
	Policy    string `json:"policy"`
	MinPeriod int    `json:"minPeriod"`
}

type signalKSubscribeMessage struct {
	Context   string                  `json:"context"`
	Subscribe []signalKSubscribeEntry `json:"subscribe"`
}

func streamSubscription() signalKSubscribeMessage {
	return signalKSubscribeMessage{
		Context: "vessels.*",
		Subscribe: []signalKSubscribeEntry{{
			Path:      "*",
			Policy:    "instant",
			MinPeriod: streamSubscribeMinPeriodMS,
		}},
	}
}

const (
	defaultStreamMinBackoff = 1 * time.Second
	defaultStreamMaxBackoff = 30 * time.Second

	// A connection that survived this long is treated as evidence the endpoint
	// is healthy, so the next drop retries promptly instead of inheriting the
	// backoff grown by an earlier outage.
	streamStableDuration = 10 * time.Second
)

func buildSignalKStreamURL(address string, port int) string {
	base := buildSignalKURL(address, port)

	switch {
	case strings.HasPrefix(base, "https://"):
		base = "wss://" + strings.TrimPrefix(base, "https://")
	case strings.HasPrefix(base, "http://"):
		base = "ws://" + strings.TrimPrefix(base, "http://")
	}

	return base + signalKStreamPath
}

// signalKStreamClient keeps a signalKSnapshot fed from the SignalK delta
// stream, reconnecting for as long as its context lives.
type signalKStreamClient struct {
	snapshot     *signalKSnapshot
	settingsPath string

	minBackoff time.Duration
	maxBackoff time.Duration
	now        func() time.Time

	// Injectable so tests can point the client at a stub without writing
	// settings, and drive the reauth path without a live SignalK server.
	resolveURLs     func() (streamURL string, httpBaseURL string)
	acquireToken    func(httpBaseURL string) (string, error)
	invalidateToken func()
}

func newSignalKStreamClient(snapshot *signalKSnapshot, settingsPath string) *signalKStreamClient {
	return &signalKStreamClient{
		snapshot:     snapshot,
		settingsPath: settingsPath,
		minBackoff:   defaultStreamMinBackoff,
		maxBackoff:   defaultStreamMaxBackoff,
		now:          func() time.Time { return time.Now().UTC() },
		resolveURLs: func() (string, string) {
			address, port, err := loadSignalKSettings(settingsPath)
			if err != nil {
				log.Printf("signalk stream: settings unreadable, falling back to defaults: %v", err)
			}
			return buildSignalKStreamURL(address, port), buildSignalKURL(address, port)
		},
		acquireToken: func(httpBaseURL string) (string, error) {
			username, password := loadSignalKCredentials(settingsPath)
			return acquireSignalKToken(httpBaseURL, username, password)
		},
		invalidateToken: invalidateSignalKToken,
	}
}

// run blocks until ctx is cancelled, reconnecting with exponential backoff.
// Every attempt is logged: the fallback policy requires retry behaviour to be
// loud rather than silently papering over an unreachable server.
func (c *signalKStreamClient) run(ctx context.Context) {
	backoff := c.minBackoff

	for {
		if ctx.Err() != nil {
			return
		}

		streamURL, httpBaseURL := c.resolveURLs()

		connectedAt := c.now()
		err := c.connectOnce(ctx, streamURL, httpBaseURL)
		if ctx.Err() != nil {
			return
		}

		if c.now().Sub(connectedAt) >= streamStableDuration {
			backoff = c.minBackoff
		}

		log.Printf("signalk stream: %s disconnected (%v); reconnecting in %s", streamURL, err, backoff)

		select {
		case <-ctx.Done():
			return
		case <-time.After(backoff):
		}

		backoff *= 2
		if backoff > c.maxBackoff {
			backoff = c.maxBackoff
		}
	}
}

// connectOnce dials and pumps deltas into the snapshot until the connection
// fails or ctx is cancelled. It always returns a non-nil error describing why
// it stopped.
func (c *signalKStreamClient) connectOnce(ctx context.Context, streamURL, httpBaseURL string) error {
	conn, err := c.dial(ctx, streamURL, httpBaseURL)
	if err != nil {
		return err
	}
	defer conn.CloseNow()

	// Sent before marking connected: with subscribe=none the server sends
	// nothing until it arrives, so a failure here is a dead stream, not a
	// degraded one. Surface it rather than sitting silently on an idle socket
	// — the signalk-stream watchdog would eventually fire, but the reason
	// belongs in the log at the point it happened.
	subscription, err := json.Marshal(streamSubscription())
	if err != nil {
		return fmt.Errorf("signalk stream: encoding subscription: %w", err)
	}
	if err := conn.Write(ctx, websocket.MessageText, subscription); err != nil {
		return fmt.Errorf("signalk stream: sending subscription: %w", err)
	}

	c.snapshot.setConnected(true)
	defer c.snapshot.setConnected(false)

	for {
		_, data, err := conn.Read(ctx)
		if err != nil {
			return err
		}

		var delta signalKDelta
		if err := json.Unmarshal(data, &delta); err != nil {
			// Dropping the session over one corrupt frame would reset every
			// path to unseen for the whole backoff; the next frame usually parses.
			log.Printf("signalk stream: skipping unparseable frame: %v", err)
			continue
		}

		// The server's opening frame is metadata (name, version, self, roles)
		// with no updates at all. It is the only place the self context is
		// named, so it must be captured before the no-updates skip.
		if delta.Self != "" {
			c.snapshot.setSelfContext(delta.Self)
		}

		if len(delta.Updates) == 0 {
			continue
		}

		c.snapshot.applyDelta(delta, c.now())
	}
}

func (c *signalKStreamClient) dial(ctx context.Context, streamURL, httpBaseURL string) (*websocket.Conn, error) {
	token, err := c.acquireToken(httpBaseURL)
	if err != nil {
		log.Printf("signalk stream: token acquisition failed, dialling anonymously: %v", err)
	}

	conn, response, err := dialSignalKStream(ctx, streamURL, token)
	if err == nil {
		return conn, nil
	}

	// Mirrors the write paths' retry-once idiom (route_activation.go): a
	// rejected token is nearly always an expired one.
	if token == "" || response == nil || response.StatusCode != http.StatusUnauthorized {
		return nil, err
	}

	c.invalidateToken()
	token, tokenErr := c.acquireToken(httpBaseURL)
	if tokenErr != nil {
		return nil, fmt.Errorf("signalk stream reauth failed: %w", tokenErr)
	}

	conn, _, err = dialSignalKStream(ctx, streamURL, token)
	return conn, err
}

func dialSignalKStream(ctx context.Context, streamURL, token string) (*websocket.Conn, *http.Response, error) {
	options := &websocket.DialOptions{}
	if token != "" {
		options.HTTPHeader = http.Header{"Authorization": []string{"Bearer " + token}}
	}

	return websocket.Dial(ctx, streamURL, options)
}
