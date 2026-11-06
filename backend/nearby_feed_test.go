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
	if got, _ := nearbyFeedVessels(all, nil); len(got) != nearbyVesselsDefaultLimit {
		t.Fatalf("no watch: want the first 10, got %d", len(got))
	}
	watch := &anchorWatchData{Lat: aisTestAnchorLat, Lon: aisTestAnchorLon}
	got, _ := nearbyFeedVessels(all, watch)
	if len(got) != 11 || got[10].Name != "V11" {
		t.Fatalf("watch: want the first 10 plus V11, got %+v", got)
	}
	short := all[:4]
	if got, _ := nearbyFeedVessels(short, watch); len(got) != 4 {
		t.Fatalf("a short list is returned whole, got %d", len(got))
	}
}

// A crowded anchorage: 45 boats within 500 m of the anchor, all further from
// the boat than the ten decoys. The feed holds the nearest nearbyFeedCap of
// them and says how many it left out.
func TestNearbyFeedCapsTheBoatsNearTheAnchorAndSaysHowManyWereLeftOut(t *testing.T) {
	anchorTrailEnv(t)
	dropWatch(t)
	seedFeedScene(t)
	now := time.Now().UTC()
	for i := 0; i < 45; i++ {
		id := fmt.Sprintf("urn:mrn:imo:mmsi:4000000%02d", i)
		globalSignalKSnapshot.contexts[vesselContextPrefix+id] = map[string]any{
			"name": fmt.Sprintf("CROWD %02d", i), "mmsi": fmt.Sprintf("4000000%02d", i),
			"navigation": map[string]any{"position": map[string]any{"value": map[string]any{"latitude": -19.9965 - float64(i)*0.00005, "longitude": aisTestAnchorLon}}},
		}
		globalSignalKSnapshot.pathSeen[vesselContextPrefix+id+"|navigation.position"] = now
	}
	payload := buildNearbyVesselsPayload()
	vessels := payload["vessels"].([]nearbyVessel)
	if len(vessels) != nearbyFeedCap {
		t.Fatalf("the feed holds at most %d boats, got %d", nearbyFeedCap, len(vessels))
	}
	// 10 decoys + NEAR + 45 crowd = 56 qualify; FAR is outside 500 m.
	if got := payload["capped_count"]; got != 56-nearbyFeedCap {
		t.Fatalf("capped_count = %v, want %d", got, 56-nearbyFeedCap)
	}
	for i := 1; i < len(vessels); i++ {
		if vessels[i].RangeM < vessels[i-1].RangeM {
			t.Fatalf("the capped feed must stay nearest first")
		}
	}
}

func TestNearbyFeedReportsNothingCappedWhenEveryoneFits(t *testing.T) {
	seedFeedScene(t)
	if got := buildNearbyVesselsPayload()["capped_count"]; got != 0 {
		t.Fatalf("capped_count = %v, want 0", got)
	}
}

func TestNearbyFeedSelectionCapsOnlyTheWatchExtras(t *testing.T) {
	all := make([]nearbyVessel, 0, 100)
	for i := 0; i < 100; i++ {
		all = append(all, nearbyVessel{Name: fmt.Sprintf("V%d", i), RangeM: float64(i), Lat: aisInsideLat, Lon: aisTestAnchorLon})
	}
	watch := &anchorWatchData{Lat: aisTestAnchorLat, Lon: aisTestAnchorLon}
	feed, capped := nearbyFeedVessels(all, watch)
	if len(feed) != nearbyFeedCap || capped != 100-nearbyFeedCap || feed[nearbyFeedCap-1].Name != fmt.Sprintf("V%d", nearbyFeedCap-1) {
		t.Fatalf("want the nearest %d and %d capped, got %d and %d", nearbyFeedCap, 100-nearbyFeedCap, len(feed), capped)
	}
	if feed, capped = nearbyFeedVessels(all, nil); len(feed) != nearbyVesselsDefaultLimit || capped != 0 {
		t.Fatalf("with no watch the feed is the 10 nearest and nothing counts as capped, got %d and %d", len(feed), capped)
	}
}

func positionDelta(context, timestamp string) signalKDelta {
	return signalKDelta{Context: context, Updates: []signalKUpdate{{
		Timestamp: timestamp,
		Values:    []signalKValue{{Path: "navigation.position", Value: map[string]any{"latitude": -20.0, "longitude": 149.0}}},
	}}}
}

// ais_heard_age_s is the feed-health signal: how long since ANY other vessel
// reported a live position, in range or not, so a silent receiver is told apart
// from an empty sea.
func TestNearbyPayloadReportsHowLongSinceAnyAISPositionWasHeard(t *testing.T) {
	seedSelfTree(t, trustedSignalKPayload(feedBoatLat, aisTestAnchorLon))
	now := time.Now().UTC()
	// 100 km away would be out of range, but it was heard.
	globalSignalKSnapshot.applyDelta(positionDelta(vesselContextPrefix+"urn:mrn:imo:mmsi:500000001", now.Add(-90*time.Second).Format(time.RFC3339Nano)), now.Add(-90*time.Second))
	age, ok := buildNearbyVesselsPayload()["ais_heard_age_s"].(float64)
	if !ok || age < 89 || age > 95 {
		t.Fatalf("ais_heard_age_s = %v, want about 90", age)
	}
}

func TestNearbyPayloadSaysNoAISHeardWithMinusOne(t *testing.T) {
	seedSelfTree(t, trustedSignalKPayload(feedBoatLat, aisTestAnchorLon))
	if got := buildNearbyVesselsPayload()["ais_heard_age_s"]; got != -1.0 {
		t.Fatalf("ais_heard_age_s = %v, want -1", got)
	}
}

// A reconnect makes signalk-server replay its cached positions, each stamped
// with the time it was first written. They are not reports heard now.
func TestNearbyHeartbeatIgnoresPositionsReplayedOnReconnect(t *testing.T) {
	seedSelfTree(t, trustedSignalKPayload(feedBoatLat, aisTestAnchorLon))
	now := time.Now().UTC()
	old := now.Add(-3 * time.Hour).Format(time.RFC3339Nano)
	globalSignalKSnapshot.applyDelta(positionDelta(vesselContextPrefix+"urn:mrn:imo:mmsi:500000001", old), now)
	if got := buildNearbyVesselsPayload()["ais_heard_age_s"]; got != -1.0 {
		t.Fatalf("a replayed position must not count as heard, got %v", got)
	}
}

// Evicting a silent vessel's tree must not erase how long the receiver has
// been quiet: -1 means never heard since the process started.
func TestNearbyHeartbeatSurvivesEvictionOfTheSilentVessel(t *testing.T) {
	seedSelfTree(t, trustedSignalKPayload(feedBoatLat, aisTestAnchorLon))
	ctx := vesselContextPrefix + "urn:mrn:imo:mmsi:500000001"
	then := time.Now().UTC().Add(-2 * time.Hour)
	globalSignalKSnapshot.applyDelta(positionDelta(ctx, then.Format(time.RFC3339Nano)), then)
	globalSignalKSnapshot.evictStaleVesselContexts(time.Now().UTC())
	if _, still := globalSignalKSnapshot.contexts[ctx]; still {
		t.Fatalf("test setup: the context should have been evicted")
	}
	age, _ := buildNearbyVesselsPayload()["ais_heard_age_s"].(float64)
	if age < 7190 {
		t.Fatalf("ais_heard_age_s = %v, want about 7200 after eviction", age)
	}
}

// SignalK can publish this boat under a second context. Its reports are not
// AIS, so they must not keep the heartbeat alive.
func TestNearbyPayloadHeartbeatIgnoresThisBoatsOwnSecondContext(t *testing.T) {
	seedSelfTree(t, trustedSignalKPayload(feedBoatLat, aisTestAnchorLon))
	ctx := vesselContextPrefix + "urn:mrn:imo:mmsi:111"
	now := time.Now().UTC()
	globalSignalKSnapshot.contexts[ctx] = map[string]any{"name": "OWN BOAT"}
	globalSignalKSnapshot.applyDelta(positionDelta(ctx, now.Format(time.RFC3339Nano)), now)
	if got := aisHeardAge(now, []string{"own boat"}); got != -1 {
		t.Fatalf("aisHeardAge = %v, want -1: the only reports are this boat's own", got)
	}
}

// When the cap bites, it keeps the boats nearest the anchor, not the nearest
// the boat.
func TestNearbyFeedCapKeepsTheBoatsNearestTheAnchor(t *testing.T) {
	watch := &anchorWatchData{Lat: aisTestAnchorLat, Lon: aisTestAnchorLon}
	all := make([]nearbyVessel, 0, 60)
	for i := 0; i < 60; i++ {
		// All inside 500 m. Range from the boat rises with i, but the distance
		// from the anchor falls, so the last boats are the ones nearest the anchor.
		all = append(all, nearbyVessel{Name: fmt.Sprintf("V%02d", i), RangeM: float64(i), Lat: aisTestAnchorLat - 0.004 + float64(i)*0.00006, Lon: aisTestAnchorLon})
	}
	feed, capped := nearbyFeedVessels(all, watch)
	if len(feed) != nearbyFeedCap || capped != 60-nearbyFeedCap {
		t.Fatalf("want %d kept and %d capped, got %d and %d", nearbyFeedCap, 60-nearbyFeedCap, len(feed), capped)
	}
	names := map[string]bool{}
	for _, v := range feed {
		names[v.Name] = true
	}
	if !names["V59"] || names["V10"] {
		t.Fatalf("the cap must keep the boats nearest the anchor (V59) and drop far ones (V10), got %v", names)
	}
	for i := 1; i < len(feed); i++ {
		if feed[i].RangeM < feed[i-1].RangeM {
			t.Fatalf("feed must stay in range order")
		}
	}
}

func TestNearbyPayloadSendsTheCap(t *testing.T) {
	if got := buildNearbyVesselsPayload()["feed_cap"]; got != nearbyFeedCap {
		t.Fatalf("feed_cap = %v, want %d", got, nearbyFeedCap)
	}
}

// This boat's second context is evicted after an hour of silence. Its
// heartbeat entry must go with it, or the name check that excluded it is lost
// and its old reports count as AIS.
func TestNearbyHeartbeatForgetsThisBoatsSecondContextWhenItIsEvicted(t *testing.T) {
	seedSelfTree(t, trustedSignalKPayload(feedBoatLat, aisTestAnchorLon))
	globalSignalKSnapshot.contexts["vessels.self"]["name"] = "OWN BOAT"
	ctx := vesselContextPrefix + "urn:mrn:imo:mmsi:111"
	then := time.Now().UTC().Add(-2 * time.Hour)
	globalSignalKSnapshot.contexts[ctx] = map[string]any{"name": "Own Boat"}
	globalSignalKSnapshot.applyDelta(positionDelta(ctx, then.Format(time.RFC3339Nano)), then)
	globalSignalKSnapshot.evictStaleVesselContexts(time.Now().UTC())
	if got := aisHeardAge(time.Now().UTC(), nil); got != -1 {
		t.Fatalf("aisHeardAge = %v, want -1: the only report was this boat's own", got)
	}
}
