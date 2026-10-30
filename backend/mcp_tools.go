package main

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"sync"
)

// What an outside assistant may be offered (ADR 0231). Every Mate tool has one
// decision here, with its reason; a tool added to the registry without one
// fails TestMCPPolicy_EveryMateToolHasADecision.
//
// The decision is enforced on the server, not hidden in Settings: a tool that
// is never offered is not built into the MCP server for any token, whatever
// that token's stored list says, and a call to it is refused as unknown.

type mcpToolStatus string

const (
	mcpDefaultOn  mcpToolStatus = "default on"
	mcpOfferedOff mcpToolStatus = "offered off"
	mcpNever      mcpToolStatus = "never"
)

// mcpBoatContextTool is the one tool that exists only on the MCP surface.
const mcpBoatContextTool = "get_boat_context"

type mcpToolPolicy struct {
	Name   string
	Plain  string // what Settings calls it
	Status mcpToolStatus
	Reason string
	// Description replaces the registry's description when set. Extra is added
	// after it. Either carries the wording Mate's prompt would otherwise supply.
	Description string
	Extra       string
}

// mcpPassageDecision is why the passage tools are offered but start off.
const mcpPassageDecision = "Helps decide a passage, so it starts off and the operator opts in per token."

const (
	extraReadFigures = "Figures are in the operator's units: quote the display value and display_unit as given. "
)

var mcpToolPolicies = []mcpToolPolicy{
	// Offered, on by default.
	{Name: "get_vessel_status", Plain: "Vessel status", Status: mcpDefaultOn,
		Reason: "One snapshot built from the operator's roles and items. Its result carries units and missing reasons itself.",
		Extra: "Answer a status question from this alone and do not choose SignalK paths of your own. " + extraReadFigures +
			"house_bank is the boat's house battery bank, chosen by the operator. A field with a missing reason has no reading: " +
			"give the reason in a few words in place of the figure, and never estimate it or quote an older reading. " +
			"For each tank, warn_percent and alarm_percent are the levels at which its alarms fire. " +
			"Observations are questions to put to the operator, never causes."},
	{Name: "get_active_alarms", Plain: "Active alarms", Status: mcpDefaultOn,
		Reason: "Titles and bodies are worded on the server for the watchkeeper. The investigate hint names tools that are also on offer.",
		Extra: "Report each alarm as worded; do not reword its title or body. An empty list means nothing is alarming now. " +
			"If times carry a timezone_note they are UTC, not boat time. Each alarm's investigate list says what to read next: " +
			"an entry that is a SignalK path goes to get_last_recorded (path_prefix) and get_path_history (path). " +
			"If it is empty the alarm has no sensor of its own to read, so say that rather than guessing a path. " +
			"Never use the alarm's own path field, and never a path guessed from the device name on the card."},
	{Name: "get_alarm_context", Plain: "Alarm log and history", Status: mcpDefaultOn,
		Reason: "Reads the alarm log (raised and cleared history). Evidence wording moved into the description.",
		Extra: "Explain from what it returns: the reading against the threshold in the operator's units, and the clear point and dwell where the rule has them. " +
			"Keep the value at raise apart from the live value now and say which one you quote. A large step or a long flat run in the history " +
			"points at the sensor, and a gradual change does not. If occurrences carries a pattern, the alarm keeps going off. " +
			"Never invent a cause the figures do not show. It describes an alarm and never clears or changes one."},
	{Name: "get_path_history", Plain: "Path history", Status: mcpDefaultOn,
		Reason: "Recorded history of one reading. Needs InfluxDB and says so when it is not configured.",
		Extra: "Use only a path the boat reported (from get_boat_context, get_last_recorded or an alarm's investigate list); never guess a path. " +
			"It returns what was recorded and nothing else. Quote a mean direction as the tool gives it and never average angles yourself."},
	{Name: "get_last_recorded", Plain: "Last recorded readings", Status: mcpDefaultOn,
		Reason: "Finds the last recorded point per source. Result is self-describing.",
		Extra:  "Use it to find paths and when a reading last arrived. Never guess a path: take it from this result, get_boat_context or an alarm's investigate list."},
	{Name: "list_passages", Plain: "Passages", Status: mcpDefaultOn,
		Reason: "Reads the Passages logbook. The wording about what the logbook is moved into the description.",
		Extra: "The Passages logbook is the record of the passages, however long ago they were. Do not count passages from logged speed. " +
			"Take places only from the records returned, and never guess the direction of travel or what region a place is in. " +
			"Say what the logbook lacks instead of filling it in. If it has nothing in the dates asked about, say so."},
	{Name: "get_cruise_summary", Plain: "Cruise summary", Status: mcpDefaultOn,
		Reason: "Totals from the Passages logbook. The wording about what the logbook is moved into the description.",
		Extra: "Call this first for a season, a cruise or a trip. The Passages logbook is the record of the passages, however long ago. " +
			"Quote its totals rather than adding them up yourself. Take places only from the records returned, and never guess direction or region. " +
			"Say what the logbook lacks instead of filling it in."},
	{Name: "list_maintenance", Plain: "Maintenance due", Status: mcpDefaultOn,
		Reason: "Status worked out on the server. The date used is the boat's local date, taken from the boat's time zone.",
		Extra:  "The status is worked out against the boat's local date. If the boat has never had a position fix, the call fails rather than guess a date."},
	{Name: "get_maintenance_log", Plain: "Maintenance log", Status: mcpDefaultOn,
		Reason: "Reads the log. Self-describing."},
	{Name: "find_equipment", Plain: "Equipment", Status: mcpDefaultOn,
		Reason: "Gives the equipment ids the two maintenance tools need. Reads the equipment registry only."},
	{Name: "get_vessel_particulars", Plain: "Boat particulars", Status: mcpDefaultOn,
		Reason: "Length, beam, builder, hull and registration. The owner's name, phone, email, insurer, policy number and storm delegate are removed from the result, not just from the description.",
		Description: "Read what Helmcentral has on record about the boat: length overall and beam, builder, model, year, hull details, " +
			"registration, home marina and berth, plus the boat's name and live length and draft from instruments when they are connected. " +
			"Each value says where it came from. Anything not on record is listed as absent: never fill it from memory or guess. " +
			"The owner's contact and insurance details are never part of this result."},
	{Name: mcpBoatContextTool, Plain: "Boat context", Status: mcpDefaultOn,
		Reason: "Generated from the same readers as get_vessel_status. Tells an outside assistant how this boat is set up, in what units and in what time zone.",
		Description: "Read how this boat is set up, generated live from Helmcentral: the equipment items and roles (which battery is the house bank, " +
			"which engines and tanks there are), the units the operator reads and the units the boat reports in, the boat's particulars " +
			"(no contact details), the alarm rules, the tank warn and alarm levels and the boat's time zone. Call it first when you are unsure " +
			"which item a question means."},
	{Name: "get_wind_forecast", Plain: "Wind and wave forecast", Status: mcpDefaultOn,
		Reason: "Read-only forecast data, useful on its own. On by default."},
	{Name: "get_tides", Plain: "Tides", Status: mcpDefaultOn,
		Reason: "Read-only tide predictions, useful on its own. On by default."},
	{Name: "get_nearby_vessels", Plain: "Nearby vessels", Status: mcpDefaultOn,
		Reason: "Read-only view of AIS targets from the boat's own receiver. On by default."},
	{Name: "get_planned_stay", Plain: "Planned stay", Status: mcpDefaultOn,
		Reason: "Read-only: the stay the operator set on the Forecast page. On by default."},
	{Name: "route_distance", Plain: "Distance by water", Status: mcpDefaultOn,
		Reason: "Read-only routing over the offline coastline. On by default."},
	{Name: "anchorage_shelter", Plain: "Anchorage shelter", Status: mcpDefaultOn,
		Reason: "Read-only shelter estimate from the offline coastline. On by default."},
	{Name: "find_places", Plain: "Place names", Status: mcpDefaultOn,
		Reason: "Read-only place-name lookup. On by default."},
	{Name: "read_help", Plain: "Helmcentral help", Status: mcpDefaultOn,
		Reason: "Read-only help pages built into Helmcentral. On by default."},

	// Offered, off until the operator switches them on.
	{Name: "check_signalk_paths", Plain: "Live SignalK paths", Status: mcpOfferedOff,
		Reason: "Raw paths and sources. Useful for diagnosis, but it invites path guessing, so it starts off.",
		Extra:  "Use only paths this tool returns; never guess one. Values are in the units the boat declares, not the operator's display units."},
	{Name: "list_alarm_rules", Plain: "Alarm rules", Status: mcpOfferedOff,
		Reason: "Lists what the boat monitors. The description is rewritten so it does not invite proposals, which are never offered.",
		Description: "List every alarm rule the boat is watching: id, label, path, op (above, below, equal, notEqual, stale), value and clear_value " +
			"in the operator's unit (display_unit), severity (state), dwell_seconds, enabled, and origin (own, built-in, or derived from tank " +
			"levels or a gauge zone). Use it to say what the boat monitors. It is read-only: you cannot change a rule."},
	{Name: "estimate_passage", Plain: "Passage estimate", Status: mcpOfferedOff,
		Reason: mcpPassageDecision},
	{Name: "plan_tidal_departure", Plain: "Tidal departure", Status: mcpOfferedOff,
		Reason: mcpPassageDecision},
	{Name: "assess_passage_window", Plain: "Passage window", Status: mcpOfferedOff,
		Reason: mcpPassageDecision},

	// Never offered.
	{Name: "search_conversations", Plain: "", Status: mcpNever, Reason: "Mate's chat history is the operator's, not an outside assistant's."},
	{Name: "read_conversation", Plain: "", Status: mcpNever, Reason: "Mate's chat history is the operator's, not an outside assistant's."},
	{Name: "propose_changes", Plain: "", Status: mcpNever, Reason: "Writes and proposals are out of scope: this server is read-only."},
	{Name: "fill_form", Plain: "", Status: mcpNever, Reason: "Drives Helmcentral's own forms; writes."},
	{Name: "inspect_form", Plain: "", Status: mcpNever, Reason: "Reads a form on the operator's screen, which an outside assistant has no screen for."},
	{Name: "start_watch", Plain: "", Status: mcpNever, Reason: "Creates a watch: a write."},
	{Name: "run_watch_now", Plain: "", Status: mcpNever, Reason: "Runs a watch's model review, which spends the OpenRouter key."},
	{Name: "list_watches", Plain: "", Status: mcpNever, Reason: "Watches and their reports are Mate's; they hold Mate's own conversation."},
	{Name: "get_watch_runs", Plain: "", Status: mcpNever, Reason: "Watch reports are written by Mate's model; not offered this cycle."},
	{Name: "search_web", Plain: "", Status: mcpNever, Reason: "Calls OpenRouter and spends the operator's key."},
	{Name: "search_documents", Plain: "", Status: mcpNever, Reason: "Semantic search embeds the query through OpenRouter and spends the key, and what documents may leave the boat is undecided."},
	{Name: "read_document", Plain: "", Status: mcpNever, Reason: "What documents may leave the boat is undecided."},
	{Name: "list_records", Plain: "", Status: mcpNever, Reason: "Reaches every record type, the vessel record among them, and so the owner's contact details."},
	{Name: "get_record", Plain: "", Status: mcpNever, Reason: "Reaches every record type, the vessel record among them, and so the owner's contact details."},
	{Name: "describe_record_type", Plain: "", Status: mcpNever, Reason: "Exists to prepare proposals, which are never offered."},
	{Name: "show_tile", Plain: "", Status: mcpNever, Reason: "Draws a card in Mate's chat; the result is a picture an outside assistant cannot show."},
	{Name: "show_telemetry_chart", Plain: "", Status: mcpNever, Reason: "Draws a card in Mate's chat; use get_path_history for the figures."},
	{Name: "show_wind_rose", Plain: "", Status: mcpNever, Reason: "Draws a card in Mate's chat."},
}

func mcpPolicyFor(name string) (mcpToolPolicy, bool) {
	for _, p := range mcpToolPolicies {
		if p.Name == name {
			return p, true
		}
	}
	return mcpToolPolicy{}, false
}

// mcpOffered says whether a tool can be offered to any token at all.
func mcpOffered(name string) bool {
	p, ok := mcpPolicyFor(name)
	return ok && p.Status != mcpNever
}

// mcpDefaultTools is the curated list a new token starts with.
func mcpDefaultTools() []string {
	var out []string
	for _, p := range mcpToolPolicies {
		if p.Status == mcpDefaultOn {
			out = append(out, p.Name)
		}
	}
	return out
}

// mcpToolDef is one tool as an outside assistant sees it.
type mcpToolDef struct {
	Name        string
	Plain       string
	Description string
	Schema      json.RawMessage
	DefaultOn   bool
	Run         func(ctx context.Context, d assistantToolDeps, args json.RawMessage) (string, error)
}

var (
	mcpToolDefsOnce  sync.Once
	mcpToolDefsBuilt []mcpToolDef
)

// mcpToolDefs is every tool that can be offered to some token, by name. The
// registry and the policy are static, so it is built once. Callers must not
// change the slice.
func mcpToolDefs() []mcpToolDef {
	mcpToolDefsOnce.Do(func() { mcpToolDefsBuilt = buildMCPToolDefs() })
	return mcpToolDefsBuilt
}

func buildMCPToolDefs() []mcpToolDef {
	var defs []mcpToolDef
	for _, p := range mcpToolPolicies {
		if p.Status == mcpNever {
			continue
		}
		def := mcpToolDef{Name: p.Name, Plain: p.Plain, DefaultOn: p.Status == mcpDefaultOn}
		if p.Name == mcpBoatContextTool {
			def.Description = p.Description
			def.Schema = json.RawMessage(`{"type":"object","properties":{}}`)
			def.Run = func(ctx context.Context, d assistantToolDeps, _ json.RawMessage) (string, error) {
				if err := ctx.Err(); err != nil {
					return "", err
				}
				return buildBoatContext(d, mcpEnabledFrom(ctx))
			}
			defs = append(defs, def)
			continue
		}
		mt := lookupMateTool(p.Name)
		if mt == nil {
			// Guarded by TestMCPPolicy_EveryMateToolHasADecision.
			panic(fmt.Sprintf("outside assistant policy names %q, which is not a Mate tool", p.Name))
		}
		def.Description = mt.function.Description
		if p.Description != "" {
			def.Description = p.Description
		}
		if p.Extra != "" {
			def.Description += " " + p.Extra
		}
		def.Schema = mt.function.Parameters
		if len(def.Schema) == 0 {
			def.Schema = json.RawMessage(`{"type":"object","properties":{}}`)
		}
		def.Run = mcpRunMateTool(mt, p.Name)
		defs = append(defs, def)
	}
	sort.Slice(defs, func(i, j int) bool { return defs[i].Name < defs[j].Name })
	return defs
}

func mcpRunMateTool(mt *mateTool, name string) func(context.Context, assistantToolDeps, json.RawMessage) (string, error) {
	return func(ctx context.Context, d assistantToolDeps, args json.RawMessage) (string, error) {
		out, err := mt.execute(d, ctx, args)
		if err != nil {
			return "", err
		}
		if name == "get_vessel_particulars" {
			return stripParticularsContacts(out)
		}
		return out, nil
	}
}

func mcpToolDefByName(name string) *mcpToolDef {
	for _, d := range mcpToolDefs() {
		if d.Name == name {
			d := d
			return &d
		}
	}
	return nil
}

// mcpToolsForList is the tools a token may use: its stored list, narrowed to
// what can be offered at all, in name order. A stored name that is never
// offered is ignored.
func mcpToolsForList(stored []string) []mcpToolDef {
	on := map[string]bool{}
	for _, n := range stored {
		on[n] = true
	}
	var out []mcpToolDef
	for _, d := range mcpToolDefs() {
		if on[d.Name] {
			out = append(out, d)
		}
	}
	return out
}

// The particulars fields that are the owner's contact and insurance details.
// They are removed from the result itself, so no description wording is all
// that stands between them and an outside assistant.
var mcpContactFields = map[string]bool{
	"owner_name": true, "owner_phone": true, "owner_email": true,
	"insurer": true, "policy_number": true, "storm_delegate": true,
}

func stripParticularsContacts(raw string) (string, error) {
	var res assistantVesselParticularsResult
	if err := json.Unmarshal([]byte(raw), &res); err != nil {
		return "", fmt.Errorf("get_vessel_particulars: read result to remove contact details: %w", err)
	}
	present := res.Present[:0]
	for _, v := range res.Present {
		if !mcpContactFields[v.Field] {
			present = append(present, v)
		}
	}
	absent := res.Absent[:0]
	for _, v := range res.Absent {
		if !mcpContactFields[v.Field] {
			absent = append(absent, v)
		}
	}
	res.Present, res.Absent = present, absent
	return capToolResultJSON(&res, nil)
}

type mcpEnabledKey struct{}

// mcpEnabledCtx carries the calling token's tool list to the tools that draw
// on other tools' data (the boat context).
func mcpEnabledCtx(ctx context.Context, on func(string) bool) context.Context {
	return context.WithValue(ctx, mcpEnabledKey{}, on)
}

// mcpEnabledFrom is the token's tool test. A call that carries none has no
// switchable tool on, so nothing that depends on one is shown.
func mcpEnabledFrom(ctx context.Context) func(string) bool {
	if on, ok := ctx.Value(mcpEnabledKey{}).(func(string) bool); ok {
		return on
	}
	return func(string) bool { return false }
}
