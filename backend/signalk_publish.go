package main

import (
	"context"
	"encoding/json"
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

const (
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
	settingsPath := getEnv("SETTINGS_FILE", "../settings.yaml")
	address, port, err := loadSignalKSettings(settingsPath)
	if err != nil {
		return fmt.Errorf("could not read the SignalK connection settings: %w", err)
	}

	httpBaseURL := buildSignalKURL(address, port)
	username, password := loadSignalKCredentials(settingsPath)
	token, err := acquireSignalKToken(httpBaseURL, username, password)
	if err != nil {
		return fmt.Errorf("could not authenticate to publish %s: %w", path, err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), signalKPublishConfirmWindow+5*time.Second)
	defer cancel()

	if err := sendSignalKDelta(ctx, buildSignalKPublishURL(address, port), token, path, value); err != nil {
		return fmt.Errorf("could not publish %s to SignalK: %w", path, err)
	}

	return confirmSignalKNotification(ctx, httpBaseURL, token, path, value != nil)
}

// sendSignalKDelta opens a publish connection, writes one delta, and closes it.
// A separate connection from the read stream deliberately: the stream is what
// keeps the whole dashboard alive, and a publish must not be able to disturb it.
func sendSignalKDelta(ctx context.Context, streamURL, token, path string, value any) error {
	options := &websocket.DialOptions{}
	if token != "" {
		options.HTTPHeader = http.Header{"Authorization": []string{"Bearer " + token}}
	}

	conn, _, err := websocket.Dial(ctx, streamURL, options)
	if err != nil {
		return err
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
	deadline := time.Now().Add(signalKPublishConfirmWindow)

	for {
		live, err := signalKNotificationLive(ctx, httpBaseURL, token, path)
		if err == nil && live == raised {
			return nil
		}

		if time.Now().After(deadline) {
			if err != nil {
				return fmt.Errorf("published %s but could not confirm it: %w", path, err)
			}
			if raised {
				return fmt.Errorf("SignalK did not accept the raise of %s: the delta was sent but the notification is not live in the server's model", path)
			}
			return fmt.Errorf("SignalK did not accept the clear of %s: the delta was sent but the notification is still live in the server's model", path)
		}

		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(signalKPublishConfirmInterval):
		}
	}
}

// signalKNotificationLive reports whether the server's model currently carries
// a raised notification at a dotted SignalK path.
func signalKNotificationLive(ctx context.Context, httpBaseURL, token, path string) (bool, error) {
	url := strings.TrimRight(httpBaseURL, "/") + signalKSelfAPIPath + "/" + strings.ReplaceAll(path, ".", "/")

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return false, err
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}

	response, err := (&http.Client{Timeout: 5 * time.Second}).Do(req)
	if err != nil {
		return false, err
	}
	defer response.Body.Close()
	body, _ := io.ReadAll(response.Body)

	if response.StatusCode == http.StatusNotFound {
		return false, nil
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return false, fmt.Errorf("signalk returned status %d: %s", response.StatusCode, string(body))
	}

	var payload struct {
		Value map[string]any `json:"value"`
	}
	if err := json.Unmarshal(body, &payload); err != nil {
		return false, fmt.Errorf("signalk returned an unreadable body for %s: %w", path, err)
	}
	if payload.Value == nil {
		return false, nil
	}
	// The same rule the read path applies, so publishing and reading cannot
	// disagree about what counts as cleared.
	return notificationValueIsLive(payload.Value), nil
}
