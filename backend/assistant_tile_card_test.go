package main

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"
)

var tileCardNow = time.Date(2026, 10, 29, 4, 3, 0, 0, time.UTC)

func tileCardDeps(state vesselStateData) assistantToolDeps {
	return assistantToolDeps{
		now:          func() time.Time { return tileCardNow },
		displayUnits: func() (string, error) { return "metric", nil },
		vesselState:  func() (vesselStateData, error) { return state, nil },
		tileTide: func(context.Context, time.Time) (tideTodayResponse, error) {
			return tideTodayResponse{
				Datetime: tileCardNow.Format(time.RFC3339), CurrentTideHeightFt: 6.56, TideDirection: "Rising",
				HighTideTime: "2026-10-29T04:12:00Z", HighTideHeightFt: 9.84,
				LowTideTime: "2026-10-29T10:30:00Z", LowTideHeightFt: 1.64,
				StationName: "Cape Upstart", Provider: "bom",
			}, nil
		},
		tileDepthTrend: func(_ context.Context, window string) depthTrendResponse {
			var pts []depthTrendPoint
			for i := 0; i < 300; i++ {
				pts = append(pts, depthTrendPoint{Time: tileCardNow.Add(time.Duration(i-300) * time.Minute), DepthM: 3 + float64(i)/300})
			}
			return depthTrendResponse{Points: pts, Since: "tide", TideType: "low", TideDepthM: 2.6}
		},
		cards: newAssistantCardSink(),
	}
}

type tileCardResult struct {
	Tile       string          `json:"tile"`
	CapturedAt string          `json:"captured_at"`
	Snapshot   json.RawMessage `json:"snapshot"`
	Display    map[string]any  `json:"display"`
	Missing    []string        `json:"missing"`
	Note       string          `json:"note"`
}

func tileCardRun(t *testing.T, d assistantToolDeps, args string) (string, tileCardResult) {
	t.Helper()
	out, err := d.execute(context.Background(), "show_tile", json.RawMessage(args))
	if err != nil {
		t.Fatalf("show_tile: %v", err)
	}
	var res tileCardResult
	if err := json.Unmarshal([]byte(out), &res); err != nil {
		t.Fatalf("decode: %v\n%s", err, out)
	}
	return out, res
}

func tileCardPayload(t *testing.T, d assistantToolDeps, out string) mateTilePayload {
	t.Helper()
	cards := d.takeCards(out)
	if len(cards) != 1 || cards[0].Kind != "tile" {
		t.Fatalf("cards = %+v", cards)
	}
	var p mateTilePayload
	if err := json.Unmarshal(cards[0].Payload, &p); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestShowTileDepthTideCardsTheSnapshotTheModelReads(t *testing.T) {
	d := tileCardDeps(vesselStateData{Status: "anchored", Depth: 3.3, DepthLastUpdateAge: 2, Latitude: -20, Longitude: 148})
	out, res := tileCardRun(t, d, `{"tile":"depth-tide"}`)
	p := tileCardPayload(t, d, out)

	if p.Tile != "depth-tide" || p.CapturedAt != tileCardNow.Format(time.RFC3339) {
		t.Errorf("payload = %+v", p)
	}
	if res.Tile != "depth-tide" || len(res.Missing) != 0 {
		t.Errorf("result = %+v", res)
	}
	// The model reads exactly the numbers the frozen card holds.
	var cardSnap, modelSnap map[string]any
	if err := json.Unmarshal(p.Snapshot, &cardSnap); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(res.Snapshot, &modelSnap); err != nil {
		t.Fatal(err)
	}
	a, _ := json.Marshal(cardSnap)
	b, _ := json.Marshal(modelSnap)
	if string(a) != string(b) {
		t.Errorf("card and result snapshots differ:\n%s\n%s", a, b)
	}
	if cardSnap["depth_m"] != 3.3 || cardSnap["navigation_state"] != "anchored" || cardSnap["depth_age_s"] != 2.0 {
		t.Errorf("vessel fields = %v", cardSnap)
	}
	tide := cardSnap["tide"].(map[string]any)
	if tide["current_tide_height_ft"] != 6.56 || tide["tide_direction"] != "Rising" || tide["high_tide_time"] != "2026-10-29T04:12:00Z" || tide["station_name"] != "Cape Upstart" {
		t.Errorf("tide = %v", tide)
	}
	trend := cardSnap["depth_trend"].(map[string]any)
	pts := trend["points"].([]any)
	if len(pts) == 0 || len(pts) > tileTrendMaxPoints {
		t.Errorf("trend points = %d, want 1..%d", len(pts), tileTrendMaxPoints)
	}
	if trend["since"] != "tide" || trend["tide_type"] != "low" || trend["tide_depth_m"] != 2.6 {
		t.Errorf("trend = %v", trend)
	}
	last := pts[len(pts)-1].(map[string]any)
	if last["depth_m"] != 3+299.0/300 {
		t.Errorf("the newest trend point must survive thinning, got %v", last)
	}
	// Figures for the model in the operator's units.
	// Times are boat time (148E is UTC+10), whatever offset the provider wrote.
	if res.Display["next_high"] != "3.00 m at 14:12 (UTC+10)" || res.Display["next_low"] != "0.50 m at 20:30 (UTC+10)" {
		t.Errorf("tide extremes = %v / %v", res.Display["next_high"], res.Display["next_low"])
	}
	if res.Display["depth"] != "3.3 m" || res.Display["tide_now"] != "2.00 m rising" {
		t.Errorf("display = %v", res.Display)
	}
}

func TestShowTileDepthTideSaysWhichReadingIsMissing(t *testing.T) {
	d := tileCardDeps(vesselStateData{Status: "anchored", Depth: -1, DepthLastUpdateAge: -1})
	d.tileTide = func(context.Context, time.Time) (tideTodayResponse, error) {
		return tideTodayResponse{}, &tideTodayError{status: 502, reason: tideNotSetUp, message: "no tide provider configured — set ui.tide_provider in Settings (see README)"}
	}
	out, res := tileCardRun(t, d, `{"tile":"depth-tide"}`)
	p := tileCardPayload(t, d, out)

	var snap map[string]any
	if err := json.Unmarshal(p.Snapshot, &snap); err != nil {
		t.Fatal(err)
	}
	if v, ok := snap["depth_m"]; !ok || v != nil {
		t.Errorf("depth_m = %v (present %v), want an explicit null", v, ok)
	}
	if v, ok := snap["tide"]; !ok || v != nil {
		t.Errorf("tide = %v (present %v), want an explicit null", v, ok)
	}
	if len(res.Missing) != 2 {
		t.Fatalf("missing = %v", res.Missing)
	}
	all := strings.Join(res.Missing, "|")
	if !strings.Contains(all, "depth") || !strings.Contains(all, "no depth reading") || !strings.Contains(all, "no tide station is set up") || strings.Contains(all, "README") || strings.Contains(all, "ui.tide_provider") {
		t.Errorf("missing does not state the gaps: %v", res.Missing)
	}
	if _, ok := res.Display["depth"]; ok {
		t.Errorf("a missing depth must have no display figure: %v", res.Display)
	}
}

func TestShowTileRefusesATileOutsideTheTable(t *testing.T) {
	d := tileCardDeps(vesselStateData{Depth: 3})
	for _, args := range []string{`{"tile":"generator"}`, `{"tile":"wind"}`, `{}`} {
		_, err := d.execute(context.Background(), "show_tile", json.RawMessage(args))
		if err == nil || !strings.Contains(err.Error(), "depth-tide") {
			t.Errorf("%s: err = %v, want one naming the tiles on offer", args, err)
		}
	}
	if got := d.takeCards(""); got != nil {
		t.Errorf("a refused call left a card: %v", got)
	}
}

func TestShowTileSchemaOffersEveryTableRow(t *testing.T) {
	var schema struct {
		Properties struct {
			Tile struct {
				Enum []string `json:"enum"`
			} `json:"tile"`
		} `json:"properties"`
	}
	if err := json.Unmarshal(mateToolShowTile.function.Parameters, &schema); err != nil {
		t.Fatal(err)
	}
	if len(schema.Properties.Tile.Enum) != len(mateTileTable) {
		t.Errorf("enum %v does not match the table", schema.Properties.Tile.Enum)
	}
	for _, id := range schema.Properties.Tile.Enum {
		if _, ok := mateTileTable[id]; !ok {
			t.Errorf("enum names %q, which has no table row", id)
		}
	}
}

func TestShowTileEmptyTrendIsAnEmptyListNotNull(t *testing.T) {
	d := tileCardDeps(vesselStateData{Status: "anchored", Depth: 3.3, DepthLastUpdateAge: 2, Latitude: -20, Longitude: 148})
	d.tileDepthTrend = func(context.Context, string) depthTrendResponse { return depthTrendResponse{Since: "window"} }
	out, _ := tileCardRun(t, d, `{"tile":"depth-tide"}`)
	p := tileCardPayload(t, d, out)
	if !strings.Contains(string(p.Snapshot), `"points":[]`) {
		t.Errorf("snapshot = %s, want an empty points list", p.Snapshot)
	}
}

func TestShowTileCurrentTideBelowZeroReadsAsMissingLikeTheTile(t *testing.T) {
	d := tileCardDeps(vesselStateData{Status: "anchored", Depth: 3.3, DepthLastUpdateAge: 2, Latitude: -20, Longitude: 148})
	good := d.tileTide
	d.tileTide = func(ctx context.Context, now time.Time) (tideTodayResponse, error) {
		r, err := good(ctx, now)
		r.CurrentTideHeightFt = -0.5
		return r, err
	}
	_, res := tileCardRun(t, d, `{"tile":"depth-tide"}`)
	if _, ok := res.Display["tide_now"]; ok {
		t.Errorf("the tile draws a dash for a current height under zero, so display must not give one: %v", res.Display)
	}
	if !strings.Contains(strings.Join(res.Missing, "|"), "no current tide height") {
		t.Errorf("missing = %v", res.Missing)
	}
}

func TestShowTileWithoutAPositionGivesInstantsNotClockTimes(t *testing.T) {
	d := tileCardDeps(vesselStateData{Status: "anchored", Depth: 3.3, DepthLastUpdateAge: 2, Latitude: -1, Longitude: -1})
	_, res := tileCardRun(t, d, `{"tile":"depth-tide"}`)
	if res.Display["next_high"] != "3.00 m at 2026-10-29T04:12:00Z" {
		t.Errorf("next_high = %v, want the instant and no guessed clock time", res.Display["next_high"])
	}
	if !strings.Contains(res.Note, "no usable position") && !strings.Contains(res.Note, "UTC") {
		t.Errorf("note = %q should say why there are no clock times", res.Note)
	}
}

func TestShowTileStopsWhenTheTurnIsCancelled(t *testing.T) {
	d := tileCardDeps(vesselStateData{Status: "anchored", Depth: 3.3, DepthLastUpdateAge: 2, Latitude: -20, Longitude: 148})
	ctx, cancel := context.WithCancel(context.Background())
	inner := d.vesselState
	d.vesselState = func() (vesselStateData, error) { cancel(); return inner() }
	d.tileTide = func(context.Context, time.Time) (tideTodayResponse, error) {
		t.Error("the tide was read after the turn was cancelled")
		return tideTodayResponse{}, nil
	}
	if _, err := d.execute(ctx, "show_tile", json.RawMessage(`{"tile":"depth-tide"}`)); !errors.Is(err, context.Canceled) {
		t.Errorf("err = %v, want context.Canceled", err)
	}
	if got := d.takeCards(""); got != nil {
		t.Errorf("a cancelled call left a card: %v", got)
	}
}

func TestShowTileNamesTheDependencyThatIsNotWired(t *testing.T) {
	d := tileCardDeps(vesselStateData{Depth: 3})
	d.tileTide = nil
	_, err := d.execute(context.Background(), "show_tile", json.RawMessage(`{"tile":"depth-tide"}`))
	if err == nil || !strings.Contains(err.Error(), "depth-tide") || !strings.Contains(err.Error(), "tileTide") {
		t.Errorf("err = %v, want one naming depth-tide and tileTide", err)
	}
}

func TestShowTileSaysWhenTheTideServiceCannotBeReached(t *testing.T) {
	d := tileCardDeps(vesselStateData{Status: "anchored", Depth: 3.3, DepthLastUpdateAge: 2, Latitude: -20, Longitude: 148})
	d.tileTide = func(context.Context, time.Time) (tideTodayResponse, error) {
		return tideTodayResponse{}, &tideTodayError{status: 502, reason: tideUnreachable, message: `tide provider "bom" unavailable: dial tcp: i/o timeout`}
	}
	_, res := tileCardRun(t, d, `{"tile":"depth-tide"}`)
	all := strings.Join(res.Missing, "|")
	if !strings.Contains(all, "the tide service could not be reached") || strings.Contains(all, "dial tcp") {
		t.Errorf("missing = %v", res.Missing)
	}
}
