package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
)

// save_place: Mate offers to keep a named position the operator gave. Like
// propose_changes it writes nothing; the operator sees the same Apply card.
// A name that is already saved proposes a move and the card shows both
// positions.

type assistantSavePlaceArgs struct {
	Name string   `json:"name"`
	Lat  *float64 `json:"lat"`
	Lon  *float64 `json:"lon"`
	Note string   `json:"note"`
}

var mateToolSavePlace = mateTool{
	name: "save_place",
	function: openRouterFunctionDef{
		Description: "Offer to save a named position the operator gave you, so find_places knows it in later chats. " +
			"Use it when the operator states coordinates for a named spot that find_places did not know. " +
			"This changes NOTHING: the operator sees a card under your reply and taps Apply or Dismiss, so tell them to tap " +
			"Apply and never say it is saved. Give lat and lon as signed decimal degrees (south and west negative): " +
			"convert 20°32.676' S 149°07.136' E to -20.5446 and 149.118933 (keep at least six decimals). If the name is already saved this proposes moving it " +
			"and the card shows the old and new position.",
		Parameters: json.RawMessage(`{
					"type": "object",
					"properties": {
						"name": {"type": "string", "description": "The operator's name for the spot, as they said it, e.g. \"Thomas Island South\"."},
						"lat": {"type": "number", "description": "Latitude in decimal degrees, south negative."},
						"lon": {"type": "number", "description": "Longitude in decimal degrees, west negative."},
						"note": {"type": "string", "description": "Optional short note, e.g. what the spot is for."}
					},
					"required": ["name", "lat", "lon"]
				}`),
	},
	execute: plainTool(assistantToolDeps.executeSavePlace),
	describe: func(args json.RawMessage) string {
		var a assistantSavePlaceArgs
		if json.Unmarshal(args, &a) == nil {
			if name := strings.TrimSpace(a.Name); name != "" {
				return fmt.Sprintf("Preparing to save %s…", name)
			}
		}
		return "Preparing to save a place…"
	},
	collect: collectProposalFromToolResult,
}

func (d assistantToolDeps) executeSavePlace(ctx context.Context, raw json.RawMessage) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	store, err := d.recordStore("save_place")
	if err != nil {
		return "", err
	}
	var args assistantSavePlaceArgs
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&args); err != nil {
		return "", fmt.Errorf("parse save_place arguments: %w", err)
	}
	if args.Lat == nil || args.Lon == nil {
		return "", errors.New("save_place: lat and lon are required, as signed decimal degrees")
	}
	candidate := savedPlace{Name: args.Name, Lat: *args.Lat, Lon: *args.Lon, Note: args.Note}
	if err := validateSavedPlace(&candidate); err != nil {
		return "", fmt.Errorf("save_place: %w", err)
	}

	existing, err := findSavedPlaceByName(store, candidate.Name)
	if err != nil {
		return "", fmt.Errorf("save_place: %w", err)
	}

	op := changeOp{Type: recordTypeSavedPlace, Action: changeCreate, Fields: fieldSet{
		"name": candidate.Name, "lat": candidate.Lat, "lon": candidate.Lon,
	}}
	if candidate.Note != "" {
		op.Fields["note"] = candidate.Note
	}
	if existing != nil {
		// A move or a new note, never a rename: the stored name stays as the
		// operator wrote it, however Mate spelled it.
		op.Action, op.ID, op.BaseVersion = changeUpdate, existing.ID, savedPlaceVersion(*existing)
		op.Fields = fieldSet{}
		if candidate.Lat != existing.Lat || candidate.Lon != existing.Lon {
			op.Fields["lat"], op.Fields["lon"] = candidate.Lat, candidate.Lon
		}
		if candidate.Note != "" && candidate.Note != existing.Note {
			op.Fields["note"] = candidate.Note
		}
		if len(op.Fields) == 0 {
			return fmt.Sprintf(`{"already_saved":true,"message":%q}`, fmt.Sprintf("%s is already saved at %s. Nothing to apply.", existing.Name, formatSavedPlacePosition(existing.Lat, existing.Lon))), nil
		}
	}
	body, err := json.Marshal(assistantProposeArgs{Operations: []changeOp{op}})
	if err != nil {
		return "", fmt.Errorf("save_place: %w", err)
	}
	out, err := d.executeProposeChanges(ctx, body)
	if err != nil {
		return "", fmt.Errorf("save_place: %w", err)
	}
	return out, nil
}
