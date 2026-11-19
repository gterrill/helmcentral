package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/coder/websocket"
)

// Helmcentral publishes onto the bus as a producer, over the delta stream every
// other SignalK producer uses. subscribe=none because this connection only
// writes — the read stream (signalk_stream.go) is what feeds the snapshot.
const signalKPublishStreamPath = "/signalk/v1/stream?subscribe=none"

// signalKPublishSourceLabel names Helmcentral on the bus, so an operator
// looking at a notification's $source can see which producer raised it.
const signalKPublishSourceLabel = "helmcentral"

// isOwnPublishSource reports whether a $source on the read stream is
// Helmcentral's own publisher: the label alone, or the label with the
// server's "XX" suffix. Any other suffix ("helmcentral.115") is a real device
// on the network that happens to share the name.
func isOwnPublishSource(ref string) bool {
	return ref == signalKPublishSourceLabel || ref == signalKPublishSourceLabel+".XX"
}

var (
	// A publish is confirmed by reading the value back out of the server's own
	// model. Ingestion is asynchronous, so this polls briefly rather than
	// reading once and calling a race a failure.
	signalKPublishConfirmWindow   = 3 * time.Second
	signalKPublishConfirmInterval = 250 * time.Millisecond
)

func buildSignalKPublishURL(address string, port int) string {
	base := buildSignalKURL(address, port)

	switch {
	case strings.HasPrefix(base, "https://"):
		base = "wss://" + strings.TrimPrefix(base, "https://")
	case strings.HasPrefix(base, "http://"):
		base = "ws://" + strings.TrimPrefix(base, "http://")
	}

	return base + signalKPublishStreamPath
}

// publishSignalKNotification raises or clears one of Helmcentral's own alarms
// on the bus, so a buzzer plugin or an MFD reacts without knowing Helmcentral
// exists (ADR 0038 §1). A nil value clears the notification.
//
// This is a delta, not a REST write. Notifications have no PUT handler — the
// server answers 404 for one — and publishing a value you did not compute is
// what the delta stream is for. Every other producer on the bus does the same.
//
// The publish is then confirmed by reading the value back. A WebSocket write
// succeeds locally whether or not the server accepts what it carried, so
// returning nil on the write alone would report a delivery that never happened.
// That is exactly how the previous REST implementation went unnoticed: it had
// never once reached the bus.
func publishSignalKNotification(path string, value any) error {
	return publishSignalKValue(path, value, func(ctx context.Context, base, token string) error {
		return confirmSignalKNotification(ctx, base, token, path, value != nil)
	})
}

// publishSignalKValue shares the authenticated delta transport, but leaves
// read-back semantics to the producer (notifications normalize null).
func publishSignalKValue(path string, value any, confirm func(context.Context, string, string) error) error {
	settingsPath := getEnv("SETTINGS_FILE", "../settings.yaml")
	address, port, err := loadSignalKSettings(settingsPath)
	if err != nil {
		return fmt.Errorf("could not read the SignalK connection settings: %w", err)
	}

	httpBaseURL := buildSignalKURL(address, port)
	username, password, err := loadSignalKCredentials(settingsPath)
	if err != nil {
		return fmt.Errorf("could not read the SignalK credentials: %w", err)
	}
	token, err := acquireSignalKToken(httpBaseURL, username, password)
	if err != nil {
		return fmt.Errorf("could not authenticate to publish %s: %w", path, err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), signalKPublishConfirmWindow+5*time.Second)
	defer cancel()

	if err := sendSignalKDelta(ctx, buildSignalKPublishURL(address, port), token, path, value); err != nil {
		return fmt.Errorf("could not publish %s to SignalK: %w", path, err)
	}

	return confirm(ctx, httpBaseURL, token)
}

// sendSignalKDelta opens a publish connection, writes one delta, and closes it.
// A separate connection from the read stream deliberately: the stream is what
// keeps the whole dashboard alive, and a publish must not be able to disturb it.
func sendSignalKDelta(ctx context.Context, streamURL, token, path string, value any) error {
	options := &websocket.DialOptions{}
	if token != "" {
		options.HTTPHeader = http.Header{"Authorization": []string{"Bearer " + token}}
	}

	conn, response, err := websocket.Dial(ctx, streamURL, options)
	if err != nil {
		return withDialStatus(err, response)
	}
	defer conn.CloseNow()

	// Context is left unset so the server files this under the connection's own
	// vessel. Naming one explicitly risks publishing onto another boat.
	payload, err := json.Marshal(signalKDelta{
		Updates: []signalKUpdate{{
			Source:    map[string]any{"label": signalKPublishSourceLabel},
			Timestamp: time.Now().UTC().Format(time.RFC3339),
			Values:    []signalKValue{{Path: path, Value: value}},
		}},
	})
	if err != nil {
		return err
	}

	if err := conn.Write(ctx, websocket.MessageText, payload); err != nil {
		return err
	}
	return conn.Close(websocket.StatusNormalClosure, "")
}

// confirmSignalKNotification reads the notification back out of the server's
// model until it reflects what was published, or the window expires.
//
// The test is liveness, not presence. signalk-server does not delete a cleared
// notification: its notifications API keeps the key and normalises it to state
// "normal". Confirming a clear by absence therefore fails against every real
// server, however well it works against a stub that deletes.
func confirmSignalKNotification(ctx context.Context, httpBaseURL, token, path string, raised bool) error {
	return confirmSignalKPublish(ctx, path,
		func() (bool, error) {
			live, err := signalKNotificationLive(ctx, httpBaseURL, token, path)
			return live == raised, err
		},
		func() string {
			if raised {
				return fmt.Sprintf("SignalK did not accept the raise of %s: the delta was sent but the notification is not live in the server's model", path)
			}
			return fmt.Sprintf("SignalK did not accept the clear of %s: the delta was sent but the notification is still live in the server's model", path)
		})
}

// confirmSignalKPublish polls matches until it reports true or the confirm
// window expires. rejected words the failure when the read worked but the
// server's model does not hold what was published.
func confirmSignalKPublish(ctx context.Context, path string, matches func() (bool, error), rejected func() string) error {
	deadline := time.Now().Add(signalKPublishConfirmWindow)

	for {
		ok, err := matches()
		if err == nil && ok {
			return nil
		}

		if time.Now().After(deadline) {
			if err != nil {
				return fmt.Errorf("published %s but could not confirm it: %w", path, err)
			}
			return errors.New(rejected())
		}

		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(signalKPublishConfirmInterval):
		}
	}
}

// signalKSelfLeaf reads one leaf of the self vessel over REST at a dotted path
// and returns its raw JSON value. found is false on a 404 or a missing value.
func signalKSelfLeaf(ctx context.Context, httpBaseURL, token, path string) (value json.RawMessage, found bool, err error) {
	url := strings.TrimRight(httpBaseURL, "/") + signalKSelfAPIPath + "/" + strings.ReplaceAll(path, ".", "/")

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, false, err
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}

	response, err := (&http.Client{Timeout: 5 * time.Second}).Do(req)
	if err != nil {
		return nil, false, err
	}
	defer response.Body.Close()
	body, _ := io.ReadAll(response.Body)

	if response.StatusCode == http.StatusNotFound {
		return nil, false, nil
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return nil, false, fmt.Errorf("signalk returned status %d: %s", response.StatusCode, string(body))
	}

	var payload struct {
		Value json.RawMessage `json:"value"`
	}
	if err := json.Unmarshal(body, &payload); err != nil {
		return nil, false, fmt.Errorf("signalk returned an unreadable body for %s: %w", path, err)
	}
	return payload.Value, len(payload.Value) > 0, nil
}

// signalKNotificationLive reports whether the server's model currently carries
// a raised notification at a dotted SignalK path.
func signalKNotificationLive(ctx context.Context, httpBaseURL, token, path string) (bool, error) {
	raw, found, err := signalKSelfLeaf(ctx, httpBaseURL, token, path)
	if err != nil || !found {
		return false, err
	}
	var value map[string]any
	if err := json.Unmarshal(raw, &value); err != nil {
		return false, fmt.Errorf("signalk returned an unreadable body for %s: %w", path, err)
	}
	if value == nil {
		return false, nil
	}
	// The same rule the read path applies, so publishing and reading cannot
	// disagree about what counts as cleared.
	return notificationValueIsLive(value), nil
}
