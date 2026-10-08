package main

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
)

// get_vessel_particulars: what Helmcentral knows about the boat and its
// owner, for filling in paperwork. Every figure carries its source, and what
// is not known is listed as absent so Mate asks the operator instead of
// guessing.

const (
	vesselSourceStored = "stored in Helmcentral (Settings, Vessel)"
	vesselSourceLive   = "live instrument data"
)

type assistantVesselValue struct {
	Field  string `json:"field"`
	Label  string `json:"label"`
	Value  any    `json:"value"`
	Source string `json:"source"`
}

type assistantVesselAbsent struct {
	Field string `json:"field"`
	Label string `json:"label"`
}

type assistantVesselParticularsResult struct {
	Present []assistantVesselValue  `json:"present"`
	Absent  []assistantVesselAbsent `json:"absent"`
	Notes   []string                `json:"notes,omitempty"`
}

func assistantVesselParticularsToolDefinition() openRouterTool {
	return openRouterTool{
		Type: "function",
		Function: openRouterFunctionDef{
			Name: "get_vessel_particulars",
			Description: "Read what Helmcentral has on record about the boat and its owner: owner name, phone and email, insurer " +
				"and policy number, home marina and berth, the storm delegate, length overall and beam, builder, model, year, hull " +
				"details, registration; plus the boat's name and length overall and draft from live instruments when they are " +
				"connected. Each value says where it came from. Anything not on record is listed as absent: never fill it from " +
				"memory or guess it, ask the operator. Use it before filling in any form.",
			Parameters: json.RawMessage(`{"type": "object", "properties": {}}`),
		},
	}
}

type vesselParticularsLabel struct{ field, label string }

// The order is the order Mate sees them in: owner and insurance first, since
// that is what forms ask for.
var vesselParticularsLabels = []vesselParticularsLabel{
	{"owner_name", "Owner name"}, {"owner_phone", "Owner phone"}, {"owner_email", "Owner email"},
	{"insurer", "Insurer"}, {"policy_number", "Policy number"},
	{"home_marina", "Home marina"}, {"berth", "Berth"}, {"storm_delegate", "Storm delegate"},
	{"loa_m", "Length overall (m)"}, {"beam_m", "Beam (m)"},
	{"builder", "Builder"}, {"model", "Model"}, {"year", "Year built"}, {"hin", "Hull ID (HIN)"},
	{"flag", "Flag"}, {"hailing_port", "Hailing port"}, {"hull_type", "Hull type"}, {"hull_material", "Hull material"},
	{"displacement_kg", "Displacement (kg)"}, {"registration", "Registration"}, {"imo", "IMO number"},
	{"epirb_id", "EPIRB beacon ID"}, {"shore_power", "Shore power"}, {"system_voltage", "System voltage"},
	{"date_acquired", "Date acquired"},
}

func (d assistantToolDeps) executeGetVesselParticulars(ctx context.Context, _ json.RawMessage) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	store, err := d.recordStore("get_vessel_particulars")
	if err != nil {
		return "", err
	}
	v, err := store.GetVesselParticulars()
	if err != nil {
		return "", fmt.Errorf("get_vessel_particulars: %w", err)
	}
	snap := vesselParticularsSnapshot(v)

	var res assistantVesselParticularsResult
	res.Present, res.Absent = []assistantVesselValue{}, []assistantVesselAbsent{}
	for _, l := range vesselParticularsLabels {
		val := snap.Fields[l.field]
		if val == nil || val == "" {
			continue
		}
		res.Present = append(res.Present, assistantVesselValue{Field: l.field, Label: l.label, Value: val, Source: vesselSourceStored})
	}

	// Live figures come from the boat's instruments, and are reported next to
	// the stored ones, never in place of them.
	var liveName string
	var liveLOA, liveDraft float64 = -1, -1
	if d.vesselState == nil {
		res.Notes = append(res.Notes, "Live instrument data is not available to this tool, so the boat's name and live length and draft are unknown.")
	} else if st, serr := d.vesselState(); serr != nil {
		res.Notes = append(res.Notes, fmt.Sprintf("Live instrument data could not be read (%v), so the boat's name and live length and draft are unknown.", serr))
	} else {
		liveName, liveLOA, liveDraft = strings.TrimSpace(st.Name), st.LengthOverallM, st.DraftM
	}
	if liveName != "" {
		res.Present = append(res.Present, assistantVesselValue{Field: "vessel_name", Label: "Boat name", Value: liveName, Source: vesselSourceLive})
	}
	if liveLOA > 0 {
		res.Present = append(res.Present, assistantVesselValue{Field: "loa_m", Label: "Length overall (m)", Value: liveLOA, Source: vesselSourceLive + " (design length)"})
		if v.LOAM != nil && *v.LOAM != liveLOA {
			res.Notes = append(res.Notes, fmt.Sprintf("Length overall differs: %.1f m stored, %.1f m from live instruments. Ask the operator which the form should carry.", *v.LOAM, liveLOA))
		}
	}
	if liveDraft > 0 {
		res.Present = append(res.Present, assistantVesselValue{Field: "draft_m", Label: "Draft (m)", Value: liveDraft, Source: vesselSourceLive + " (maximum draft)"})
	}

	have := map[string]bool{}
	for _, p := range res.Present {
		have[p.Field] = true
	}
	for _, l := range vesselParticularsLabels {
		if !have[l.field] {
			res.Absent = append(res.Absent, assistantVesselAbsent{Field: l.field, Label: l.label})
		}
	}
	for _, extra := range []assistantVesselAbsent{{"vessel_name", "Boat name"}, {"draft_m", "Draft (m)"}} {
		if !have[extra.Field] {
			res.Absent = append(res.Absent, extra)
		}
	}
	return capToolResultJSON(&res, nil)
}
