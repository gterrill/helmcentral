package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"math"
	"sort"
	"strings"
	"time"
)

// show_tile (ADR 0229): a dashboard tile in Mate's answer. The tool reads the
// same figures the tile reads from its live stores, hands them to the model as
// text, and attaches them as a card. The card is the live tile while its reply
// is the newest and the tile frozen on this snapshot afterwards, so the
// numbers beside the prose never move under it.

const (
	// tileTrendWindow is the depth trend window the Depth and Tide tile asks for.
	tileTrendWindow = "3h"
	// tileTrendMaxPoints thins the trend stored with the card. The sparkline
	// is a few hundred pixels wide, and the stored snapshot is read back by
	// the model.
	tileTrendMaxPoints = 48
)

// mateTilePayload is the stored card: which tile, when it was read, and the
// figures the tile reads. Snapshot is the tile's own shape, so the frontend
// can hand it to the tile in place of its live stores.
type mateTilePayload struct {
	Tile       string          `json:"tile"`
	CapturedAt string          `json:"captured_at"`
	Snapshot   json.RawMessage `json:"snapshot"`
}

// mateTileResult is what the model reads: the card's snapshot, the figures in
// the operator's units, and the readings that could not be taken.
type mateTileResult struct {
	Tile        string          `json:"tile"`
	CapturedAt  string          `json:"captured_at"`
	Snapshot    json.RawMessage `json:"snapshot"`
	Display     map[string]any  `json:"display"`
	Missing     []string        `json:"missing"`
	Note        string          `json:"note"`
	Instruction string          `json:"instruction"`
}

const mateTileInstruction = "A card showing this tile is attached to your reply. Answer in words first and quote the key figure, " +
	"using the display figures. Do not read the card out as a list, and do not fill in a missing reading."

// mateTileSpec is one tile Mate may show. Adding a tile to the table is the
// whole change on the server.
type mateTileSpec struct {
	// title is the tile's name as the operator reads it.
	title string
	// snapshot reads the figures the tile reads. It returns them in the
	// tile's own shape, the figures for the model in the operator's units, a
	// line for every reading that is missing, and an error only when the tile
	// cannot be read at all.
	snapshot func(ctx context.Context, d assistantToolDeps, now time.Time, system string) (snap any, display map[string]any, missing []string, note string, err error)
	// unwired names the dependencies this tile reads that d does not have.
	unwired func(d assistantToolDeps) []string
	// note is what the model needs to know about the snapshot's units.
	note string
}

// mateTileTable is every tile show_tile can draw. The list is short on
// purpose: a tile belongs here when the server can read every figure it shows
// and showing it changes nothing on the boat.
var mateTileTable = map[string]mateTileSpec{
	"depth-tide": {
		title:    "Depth & Tide",
		snapshot: depthTideSnapshot,
		unwired:  depthTideUnwired,
		note:     "depth_m is metres. Tide heights are feet, as the tide service reports them. depth_trend is the last three hours, oldest first.",
	},
}

func mateTileIDs() []string {
	ids := make([]string, 0, len(mateTileTable))
	for id := range mateTileTable {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	return ids
}

// depthTideSnapshot reads what LiveDepthTideTile reads: the depth, its age and
// the navigation state from the vessel state, the tide from the tide service
// and the three hour depth trend.
func depthTideUnwired(d assistantToolDeps) []string {
	var out []string
	if d.vesselState == nil {
		out = append(out, "vesselState")
	}
	if d.tileTide == nil {
		out = append(out, "tileTide")
	}
	if d.tileDepthTrend == nil {
		out = append(out, "tileDepthTrend")
	}
	return out
}

func depthTideSnapshot(ctx context.Context, d assistantToolDeps, now time.Time, system string) (any, map[string]any, []string, string, error) {
	var missing []string
	display := map[string]any{}
	snap := map[string]any{"depth_m": nil, "depth_age_s": nil, "navigation_state": nil, "tide": nil}

	state, err := d.vesselState()
	switch {
	case err != nil:
		missing = append(missing, "depth: the vessel state could not be read: "+err.Error())
	default:
		if state.Status != "" {
			snap["navigation_state"] = state.Status
		}
		if state.DepthLastUpdateAge >= 0 {
			snap["depth_age_s"] = roundTo(state.DepthLastUpdateAge, 1)
		}
		if state.Depth < 0 {
			missing = append(missing, "depth: no depth reading")
		} else {
			snap["depth_m"] = roundTo(state.Depth, 4)
			label, conv, cerr := chartDisplayFor("environment.depth.belowTransducer", "m", system)
			if cerr != nil {
				return nil, nil, nil, "", fmt.Errorf("show_tile: %w", cerr)
			}
			display["depth"] = fmt.Sprintf("%s %s", fixed(conv(state.Depth), 1), label)
			if age, known := secondsAge(state.DepthLastUpdateAge); known && age > vesselStatusFreshAge {
				missing = append(missing, fmt.Sprintf("depth: the reading is %d s old and the tile marks it stale", int(age.Seconds())))
			}
		}
	}

	if err := ctx.Err(); err != nil {
		return nil, nil, nil, "", err
	}
	// Clock times are boat time, from the position the app derives the vessel's
	// zone from elsewhere. Without a usable position the model gets the
	// instants and no clock time, rather than a zone guessed.
	var loc *time.Location
	zoneLabel := ""
	zoneNote := "No usable position, so tide times are the instants as given (UTC offset in the string) and carry no boat clock time."
	if err == nil && hasUsableVesselPosition(state.Latitude, state.Longitude) {
		loc, zoneLabel = vesselLocalLocation(state.Longitude), assistantTimeZoneLabel(state.Longitude)
		zoneNote = "Tide times in display are boat time (" + zoneLabel + "); the snapshot keeps the tide service's own strings."
	}

	tide, terr := d.tileTide(ctx, now)
	if terr != nil {
		if cerr := ctx.Err(); cerr != nil {
			return nil, nil, nil, "", cerr
		}
		log.Printf("show_tile: depth-tide: tide unavailable: %v", terr)
		missing = append(missing, "tide: "+tideUnavailableSentence(terr))
	} else {
		snap["tide"] = tide
		unit, perFt, decimals := "ft", 1.0, 1
		if system != "imperial" {
			unit, perFt, decimals = "m", 1/metersToFeet, 2
		}
		// The tile draws a dash for any current height under zero, so a
		// negative height is a missing reading here too.
		if tide.CurrentTideHeightFt >= 0 {
			display["tide_now"] = fmt.Sprintf("%s %s %s", fixed(tide.CurrentTideHeightFt*perFt, decimals), unit, strings.ToLower(tide.TideDirection))
		} else {
			missing = append(missing, "tide: no current tide height")
		}
		for _, e := range []struct {
			key, when string
			ft        float64
		}{
			{"next_high", tide.HighTideTime, tide.HighTideHeightFt},
			{"next_low", tide.LowTideTime, tide.LowTideHeightFt},
		} {
			if e.when == "" || e.ft == noTideExtremeHeightFt {
				missing = append(missing, "tide: no "+strings.TrimPrefix(e.key, "next_")+" tide to come")
				continue
			}
			at, perr := time.Parse(time.RFC3339, e.when)
			if perr != nil {
				return nil, nil, nil, "", fmt.Errorf("show_tile: tide %s time %q: %w", e.key, e.when, perr)
			}
			when := at.UTC().Format(time.RFC3339)
			if loc != nil {
				when = at.In(loc).Format("15:04") + " (" + zoneLabel + ")"
			}
			display[e.key] = fmt.Sprintf("%s %s at %s", fixed(e.ft*perFt, decimals), unit, when)
		}
	}

	if err := ctx.Err(); err != nil {
		return nil, nil, nil, "", err
	}
	trend := d.tileDepthTrend(ctx, tileTrendWindow)
	trend.Points = thinDepthTrend(trend.Points, tileTrendMaxPoints)
	if trend.Points == nil {
		// A card with no trend still decodes: the list is empty, not null.
		trend.Points = []depthTrendPoint{}
	}
	snap["depth_trend"] = trend
	return snap, display, missing, zoneNote, nil
}

// tideUnavailableSentence says why the tide is missing in the operator's words.
// The raw error carries settings keys and README advice, so it goes to the log.
func tideUnavailableSentence(err error) string {
	var te *tideTodayError
	if errors.As(err, &te) {
		switch te.reason {
		case tideNotSetUp:
			return "no tide station is set up"
		case tideUnreachable:
			return "the tide service could not be reached"
		case tideSettingsUnreadable:
			return "the tide settings could not be read"
		}
	}
	return "the tide could not be read"
}

func fixed(v float64, decimals int) string { return fmt.Sprintf("%.*f", decimals, v) }

// thinDepthTrend keeps at most max points, evenly spaced, always the newest.
func thinDepthTrend(pts []depthTrendPoint, max int) []depthTrendPoint {
	if len(pts) <= max {
		return pts
	}
	out := make([]depthTrendPoint, 0, max)
	step := float64(len(pts)-1) / float64(max-1)
	for i := 0; i < max; i++ {
		out = append(out, pts[int(math.Round(float64(i)*step))])
	}
	return out
}

func (d assistantToolDeps) executeShowTile(ctx context.Context, args json.RawMessage) (string, error) {
	const tool = "show_tile"
	var in struct {
		Tile string `json:"tile"`
	}
	if err := json.Unmarshal(args, &in); err != nil {
		return "", fmt.Errorf("%s: invalid arguments: %w", tool, err)
	}
	spec, ok := mateTileTable[in.Tile]
	if !ok {
		return "", fmt.Errorf("%s: tile %q cannot be shown; the tiles on offer are %s", tool, in.Tile, strings.Join(mateTileIDs(), ", "))
	}
	if d.displayUnits == nil || d.cards == nil || d.now == nil {
		return "", fmt.Errorf("%s: the card sink, clock or units are not wired", tool)
	}
	if missing := spec.unwired(d); len(missing) > 0 {
		return "", fmt.Errorf("%s: the %s tile needs %s, which are not wired", tool, in.Tile, strings.Join(missing, ", "))
	}
	if err := ctx.Err(); err != nil {
		return "", err
	}
	now := d.now().UTC()
	system, err := d.displayUnits()
	if err != nil {
		return "", fmt.Errorf("%s: %w", tool, err)
	}
	snap, display, missing, zoneNote, err := spec.snapshot(ctx, d, now, system)
	if err != nil {
		return "", err
	}
	raw, err := json.Marshal(snap)
	if err != nil {
		return "", fmt.Errorf("%s: %w", tool, err)
	}
	captured := now.Format(time.RFC3339)
	card, err := json.Marshal(mateTilePayload{Tile: in.Tile, CapturedAt: captured, Snapshot: raw})
	if err != nil {
		return "", fmt.Errorf("%s: %w", tool, err)
	}
	if missing == nil {
		missing = []string{}
	}
	text, err := capToolResultJSON(mateTileResult{
		Tile: in.Tile, CapturedAt: captured, Snapshot: raw, Display: display, Missing: missing,
		Note: strings.TrimSpace(spec.note + " " + zoneNote), Instruction: mateTileInstruction,
	}, func() bool { return false })
	if err != nil {
		return "", err
	}
	d.cards.put(text, mateCard{Kind: "tile", Payload: card})
	return text, nil
}

func mateTileParameters() json.RawMessage {
	ids, _ := json.Marshal(mateTileIDs())
	return json.RawMessage(`{
		"type": "object",
		"properties": {
			"tile": {"type": "string", "enum": ` + string(ids) + `, "description": "The dashboard tile to show."}
		},
		"required": ["tile"]
	}`)
}

var mateToolShowTile = mateTool{
	name: "show_tile",
	function: openRouterFunctionDef{
		Description: "Show a dashboard tile in your answer: the tile as the operator knows it, read now. Use it when the " +
			"picture adds something, such as \"what's our depth?\" or \"what's the tide doing?\", with the depth-tide tile. " +
			"Not for diagnostics (\"is the sounder working?\"): answer those from the readings. The card is attached to " +
			"your reply and you get the same figures it holds, with the operator's units in display. Answer in words " +
			"first and quote the key figure. If a reading is missing the tool says which, and you say so too.",
		Parameters: mateTileParameters(),
	},
	execute: func(d assistantToolDeps, ctx context.Context, args json.RawMessage) (string, error) {
		return d.executeShowTile(ctx, args)
	},
	describe: func(args json.RawMessage) string {
		var in struct {
			Tile string `json:"tile"`
		}
		if json.Unmarshal(args, &in) == nil {
			if spec, ok := mateTileTable[in.Tile]; ok {
				return "Reading the " + spec.title + " tile…"
			}
		}
		return "Reading a tile…"
	},
}
