package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"
)

const signalKAnchorPositionPath = "navigation.anchor.position"

type signalKAnchorPosition struct {
	Latitude  float64 `json:"latitude"`
	Longitude float64 `json:"longitude"`
}

// A missing path is NOT a raised anchor: Auto-state requires a null delta
// to release its anchored latch. Do not reuse notification-clear semantics.
func publishSignalKAnchorPosition(watch *anchorWatchData) error {
	var value any
	var position *signalKAnchorPosition
	if watch != nil {
		position = &signalKAnchorPosition{Latitude: watch.Lat, Longitude: watch.Lon}
		value = position
	}
	return publishSignalKValue(signalKAnchorPositionPath, value, func(ctx context.Context, base, token string) error {
		deadline := time.Now().Add(signalKPublishConfirmWindow)
		for {
			if anchorPositionMatches(ctx, base, token, position) {
				return nil
			}
			if time.Now().After(deadline) {
				return fmt.Errorf("SignalK did not confirm %s", signalKAnchorPositionPath)
			}
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(signalKPublishConfirmInterval):
			}
		}
	})
}

func anchorPositionMatches(ctx context.Context, base, token string, expected *signalKAnchorPosition) bool {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, base+signalKSelfAPIPath+"/navigation/anchor/position", nil)
	if err != nil {
		return false
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	res, err := (&http.Client{Timeout: 5 * time.Second}).Do(req)
	if err != nil {
		return false
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return false
	}
	var node struct {
		Value json.RawMessage `json:"value"`
	}
	if json.NewDecoder(res.Body).Decode(&node) != nil || len(node.Value) == 0 {
		return false
	}
	if expected == nil {
		return string(node.Value) == "null"
	}
	var actual struct {
		Latitude  *float64 `json:"latitude"`
		Longitude *float64 `json:"longitude"`
	}
	if json.Unmarshal(node.Value, &actual) != nil {
		return false
	}
	return actual.Latitude != nil && actual.Longitude != nil && *actual.Latitude == expected.Latitude && *actual.Longitude == expected.Longitude
}
