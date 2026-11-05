package main

import (
	"fmt"
	"testing"
	"time"
)

// The anchor is at -20.0, 149.0 and the boat lies 1 km north of it, so range
// from the boat and distance from the anchor differ. Ten decoys sit within
// about 150 m of the boat (far from the anchor). NEAR is 333 m from the anchor
// and 667 m from the boat, so it ranks 11th by boat range. FAR is 1.8 km from
// the anchor and 778 m from the boat, so it ranks 12th and is outside 500 m.
const (
	feedBoatLat = -19.991
	feedNearLat = -19.997
	feedFarLat  = -19.984
)

func seedFeedScene(t *testing.T) {
	t.Helper()
	seedSelfTree(t, trustedSignalKPayload(feedBoatLat, aisTestAnchorLon))
	now := time.Now().UTC()
	add := func(mmsi, name string, lat float64) {
		id := "urn:mrn:imo:mmsi:" + mmsi
		globalSignalKSnapshot.contexts[vesselContextPrefix+id] = map[string]any{
			"name": name, "mmsi": mmsi,
			"navigation": map[string]any{"position": map[string]any{"value": map[string]any{"latitude": lat, "longitude": aisTestAnchorLon}}},
		}
		globalSignalKSnapshot.pathSeen[vesselContextPrefix+id+"|navigation.position"] = now
	}
	for i := 0; i < 10; i++ {
		add(fmt.Sprintf("2000000%02d", i), fmt.Sprintf("DECOY %d", i), feedBoatLat+0.0005+float64(i)*0.0001)
	}
	add("300000001", "NEAR", feedNearLat)
	add("300000002", "FAR", feedFarLat)
}

func feedNames(payload map[string]any) []string {
	var names []string
	for _, v := range payload["vessels"].([]nearbyVessel) {
		names = append(names, v.Name)
	}
	return names
}

func hasName(names []string, want string) bool {
	for _, n := range names {
		if n == want {
			return true
		}
	}
	return false
}

func TestNearbyFeedWithoutAWatchKeepsTheTenNearest(t *testing.T) {
	seedFeedScene(t)
	names := feedNames(buildNearbyVesselsPayload())
	if len(names) != nearbyVesselsDefaultLimit || hasName(names, "NEAR") || hasName(names, "FAR") {
		t.Fatalf("with no watch the feed is the 10 nearest the boat, got %v", names)
	}
}

func TestNearbyFeedDuringAWatchAddsEveryBoatWithin500mOfTheAnchor(t *testing.T) {
	anchorTrailEnv(t)
	dropWatch(t)
	seedFeedScene(t)
	payload := buildNearbyVesselsPayload()
	names := feedNames(payload)
	if !hasName(names, "NEAR") {
		t.Fatalf("a boat 333 m from the anchor must be in the feed during a watch, got %v", names)
	}
	if hasName(names, "FAR") {
		t.Fatalf("a boat outside 500 m ranked 12th must stay out, got %v", names)
	}
	if len(names) != nearbyVesselsDefaultLimit+1 {
		t.Fatalf("expected the 10 nearest plus NEAR, got %d: %v", len(names), names)
	}
	// Still in range order.
	vessels := payload["vessels"].([]nearbyVessel)
	for i := 1; i < len(vessels); i++ {
		if vessels[i].RangeM < vessels[i-1].RangeM {
			t.Fatalf("feed is not in range order: %v", names)
		}
	}
	if names[len(names)-1] != "NEAR" {
		t.Fatalf("NEAR ranks last by boat range, got %v", names)
	}
}

func TestNearbyFeedVesselsIsAPureSelection(t *testing.T) {
	all := make([]nearbyVessel, 0, 13)
	for i := 0; i < 13; i++ {
		all = append(all, nearbyVessel{Name: fmt.Sprintf("V%d", i), RangeM: float64(i), Lat: -19.9, Lon: aisTestAnchorLon})
	}
	all[11].Lat = aisInsideLat // within 500 m of the anchor, 12th by range
	if got := nearbyFeedVessels(all, nil); len(got) != nearbyVesselsDefaultLimit {
		t.Fatalf("no watch: want the first 10, got %d", len(got))
	}
	watch := &anchorWatchData{Lat: aisTestAnchorLat, Lon: aisTestAnchorLon}
	got := nearbyFeedVessels(all, watch)
	if len(got) != 11 || got[10].Name != "V11" {
		t.Fatalf("watch: want the first 10 plus V11, got %+v", got)
	}
	short := all[:4]
	if got := nearbyFeedVessels(short, watch); len(got) != 4 {
		t.Fatalf("a short list is returned whole, got %d", len(got))
	}
}
