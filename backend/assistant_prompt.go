package main

import (
	"fmt"
	"strings"
	"time"
)

// This file builds the onboard assistant's system prompt (ADR 0093) from
// live vessel context, so the model always reasons from the boat's actual
// position, heading, warnings and configured providers rather than from
// nothing at all. collectAssistantPromptContext gathers that context once
// per turn; buildAssistantSystemPrompt is a pure function of it, kept
// separate so prompt wording can be tested without SignalK, settings, or
// the forecast warnings fetcher.

// assistantPromptContext is every fact the system prompt is built from.
// Live vessel fields (Latitude/Longitude/HeadingTrue/SpeedOverGroundKts/
// WindSpeedApparentKts/WindAngleApparentDeg) carry vesselStateData's own -1
// "unknown" sentinel (signalk.go's fetchSignalKVesselState) rather than a
// fabricated reading when a fetch fails - buildAssistantSystemPrompt prints
// "unknown" for exactly that sentinel and never lets the literal "-1"
// appear in the rendered prompt.
type assistantPromptContext struct {
	Now time.Time

	VesselName string
	BoatModel  string
	// LOAM is anchor.loa_m from settings, 0 when not entered (settings.go's
	// own convention for this field - not a -1 sentinel).
	LOAM float64
	// HullType is anchor.hull_type from settings (signalk.go's
	// isSupportedHullType), rendered into the identity line by
	// assistantHullTypePhrase. Blank when settings carries no recognised
	// value, in which case the identity line omits the hull phrase entirely
	// rather than guess.
	HullType string

	Latitude  float64
	Longitude float64
	// PlaceName is the anchor-watch pinned name when a watch is active and
	// has resolved one (it wins over the roaming tick's cache - see
	// collectAssistantPromptContext), otherwise the roaming
	// getCurrentPlaceName() value. Empty when neither is known.
	PlaceName string

	HeadingTrue          float64
	SpeedOverGroundKts   float64
	WindSpeedApparentKts float64
	WindAngleApparentDeg float64
	WindSide             string

	// WarningOK is false when the forecast warnings fetcher
	// (forecast_warnings_fetcher.go) has never landed a reading at all -
	// distinct from WarningLevel==0, which is a genuine "no warning in
	// force" result.
	WarningOK        bool
	WarningLevel     int
	WarningSurf      bool
	WarningFetchedAt time.Time

	WeatherProvider string
	WaveProvider    string
	TideProvider    string
	TideStationName string

	// Notes are the operator's standing notes (settings.yaml's
	// assistant.notes), injected into every system prompt verbatim - this is
	// where anchorage exposure knowledge and rules like "queenfish bite best
	// on a rising tide" live (ADR 0093).
	Notes string

	// ManualPages is globalManual (assistant_manual.go) at the time this
	// context was collected - the embedded operator manual's index, listed
	// in the prompt so the model knows what read_manual can return before
	// calling it. Empty on a build with no manual staged, in which case the
	// prompt simply omits the index line.
	ManualPages []manualPage

	// Spoken and Screen are turn-scoped: postAssistantMessageHandler sets
	// them straight from that one POST's body, after calling
	// collectAssistantPromptContext, rather than reading them from settings
	// or any persisted state - they describe this question, not the boat.
	// Spoken is true when the operator asked by voice (mate-voice-assistant
	// plan phase 1) and wants a reply that can be read aloud. Screen names
	// what the operator was looking at when they asked (phase "App-wide
	// voice").
	Spoken bool
	Screen assistantScreenContext
}

// assistantScreenContext is what the frontend was showing when the operator
// asked - the panel id, its settings section (only meaningful when
// Panel=="settings"), and the dashboard page name (only meaningful when
// Panel==""). See postAssistantMessageHandler's bind struct, which decodes
// this from the POST body's "screen" field.
type assistantScreenContext struct {
	Panel   string `json:"panel"`
	Section string `json:"section"`
	Page    string `json:"page"`
}

// collectAssistantPromptContext reads settings once via readSettings +
// buildSettingsPayload, the live vessel state, the anchor watch's pinned
// place name (winning over the roaming one), and the forecast warnings
// slot. It is deliberately thin: every rendering decision (sentinel ->
// "unknown", provider id -> "not configured", etc.) lives in
// buildAssistantSystemPrompt so that function can be tested by constructing
// this struct directly, with no SignalK connection or settings file
// required.
//
// A failed settings read or vessel state fetch is never masked with a
// plausible-looking value (AGENTS.md's fallback policy): the affected
// fields simply stay at their zero value / sentinel default, which
// buildAssistantSystemPrompt already renders as absent.
func collectAssistantPromptContext(settingsPath string, now time.Time) assistantPromptContext {
	pc := assistantPromptContext{
		Now:                  now,
		Latitude:             -1,
		Longitude:            -1,
		HeadingTrue:          -1,
		SpeedOverGroundKts:   -1,
		WindSpeedApparentKts: -1,
		WindAngleApparentDeg: -1,
	}

	if settings, err := readSettings(settingsPath); err == nil {
		payload := buildSettingsPayload(settings)
		pc.BoatModel = payload.Boat.Model
		pc.LOAM = payload.Anchor.LOAM
		pc.HullType = payload.Anchor.HullType
		pc.WeatherProvider = payload.UI.WeatherProvider
		pc.WaveProvider = payload.UI.WaveProvider
		pc.TideProvider = payload.UI.TideProvider
		pc.TideStationName = payload.UI.TideStationName
		pc.Notes = payload.Assistant.Notes
	}

	if state, err := fetchSignalKVesselState(); err == nil {
		pc.VesselName = state.Name
		pc.Latitude = state.Latitude
		pc.Longitude = state.Longitude
		pc.HeadingTrue = state.HeadingTrue
		pc.SpeedOverGroundKts = state.SpeedOverGroundKts
		pc.WindSpeedApparentKts = state.WindSpeedApparentKts
		pc.WindAngleApparentDeg = state.WindAngleApparentDeg
		pc.WindSide = state.WindSide
	}

	// The pinned anchor-watch name wins over the roaming tick's cache while
	// a watch is active and has resolved one - matching placeName's own
	// precedence (place_name.go's placeName handler and updateTickPlaceName).
	anchorWatchMu.RLock()
	aw := anchorWatchState
	anchorWatchMu.RUnlock()
	if aw != nil && aw.PlaceName != "" {
		pc.PlaceName = aw.PlaceName
	} else {
		pc.PlaceName = getCurrentPlaceName()
	}

	if reading, ok := globalForecastWarningsSlot.get(); ok {
		pc.WarningOK = true
		pc.WarningLevel = reading.WindLevel
		pc.WarningSurf = reading.Surf
		pc.WarningFetchedAt = reading.FetchedAt
	}

	pc.ManualPages = globalManual

	return pc
}

// assistantTimeZoneLabel names pc.Longitude's derived fixed-offset zone the
// way an operator would say it ("UTC+10"), rather than the IANA-style id
// vesselLocalTimezoneName (weather_tide.go) hands to a weather plugin. A
// zero offset - including the one a -1 sentinel longitude rounds to -
// collapses to plain "UTC" rather than the technically-accurate but
// misleading "UTC+0".
func assistantTimeZoneLabel(longitude float64) string {
	label := vesselLocalLocation(longitude).String()
	if label == "UTC+0" {
		return "UTC"
	}
	return label
}

// assistantWarningLevelLabel names a wind-ladder level (see
// forecastWindWarningLevelFor, forecast_warnings_fetcher.go) for the prompt.
func assistantWarningLevelLabel(level int) string {
	switch level {
	case 1:
		return "a wind advisory/watch"
	case 2:
		return "a gale warning"
	case 3:
		return "a storm/hurricane warning"
	default:
		return "an unrecognised wind warning"
	}
}

// assistantHullTypePhrase names anchor.hull_type for the identity line,
// covering every value isSupportedHullType (signalk.go) accepts: "power
// catamaran", "sailing catamaran", "power monohull", "sailing monohull". A
// blank or unrecognised value (an empty settings field, or a settings.yaml
// this binary predates) returns "" so buildAssistantSystemPrompt can omit
// the phrase entirely rather than print a guess about the vessel's hull.
func assistantHullTypePhrase(hullType string) string {
	switch strings.TrimSpace(hullType) {
	case "power_cat":
		return "power catamaran"
	case "sail_cat":
		return "sailing catamaran"
	case "power_mono":
		return "power monohull"
	case "sail_mono":
		return "sailing monohull"
	default:
		return ""
	}
}

// assistantPanelLabels names the dashboard panels the "app-wide voice"
// screen-context feature can identify precisely (mate-voice-assistant plan).
// A panel id outside this map is printed verbatim by assistantScreenSentence
// rather than dropped - a newly added panel this map hasn't caught up with
// still tells the model something, just not a pretty label.
var assistantPanelLabels = map[string]string{
	"forecast":     "Forecast",
	"routes":       "Routes",
	"charts":       "Charts",
	"radar":        "Radar",
	"anchor-watch": "Anchor Watch",
	"alarms":       "Alarms",
	"assistant":    "Mate",
}

// assistantScreenSentence renders one sentence naming what the operator was
// looking at when they asked (screen.Panel/Section/Page, set for this turn
// only by postAssistantMessageHandler from the POST body's "screen" field),
// or "" when every field is blank - the ordinary case, which must leave the
// prompt byte-for-byte what it was before this feature existed.
func assistantScreenSentence(screen assistantScreenContext) string {
	panel := strings.TrimSpace(screen.Panel)
	section := strings.TrimSpace(screen.Section)
	page := strings.TrimSpace(screen.Page)

	if panel == "" && section == "" && page == "" {
		return ""
	}

	if panel == "" {
		if page != "" {
			return fmt.Sprintf("The operator is looking at the dashboard page named %s.\n\n", page)
		}
		// A section with neither a panel nor a page named is not a shape the
		// frontend is expected to send, but it still names something rather
		// than silently dropping it.
		return fmt.Sprintf("The operator is looking at the Settings panel (section %s).\n\n", section)
	}

	label, known := assistantPanelLabels[panel]
	switch {
	case panel == "settings" && section != "":
		label = fmt.Sprintf("Settings (section %s)", section)
	case panel == "settings":
		label = "Settings"
	case !known:
		label = panel
	}
	return fmt.Sprintf("The operator is looking at the %s panel.\n\n", label)
}

// providerLabelOrNotConfigured names a configured provider id, or says so
// plainly when settings carries none - never a guessed default, since the
// model needs to know when it's working with nothing rather than a real
// provider.
func providerLabelOrNotConfigured(id string) string {
	if id = strings.TrimSpace(id); id == "" {
		return "not configured"
	}
	return id
}

// buildAssistantSystemPrompt renders pc into the assistant's system prompt,
// in a fixed section order (ADR 0093): identity, local time, position,
// live heading/speed/wind, marine warnings, configured providers, tool-use
// guidance, and the operator's standing notes. Every live field that might
// carry vesselStateData's -1 "unknown" sentinel is guarded before
// formatting, so the literal string "-1" can never appear in the output -
// a model reasoning over "-1 kts" or "-1 degrees" as if it were a real
// reading would be worse than no prompt at all.
func buildAssistantSystemPrompt(pc assistantPromptContext) string {
	var b strings.Builder

	// 1. Identity.
	vesselLabel := strings.TrimSpace(pc.VesselName)
	if vesselLabel == "" {
		vesselLabel = "the vessel"
	}
	boatModel := strings.TrimSpace(pc.BoatModel)
	if boatModel == "" {
		boatModel = "unknown model"
	}
	loaLabel := "unknown"
	if pc.LOAM > 0 {
		loaLabel = fmt.Sprintf("%.1fm", pc.LOAM)
	}
	if hullPhrase := assistantHullTypePhrase(pc.HullType); hullPhrase != "" {
		fmt.Fprintf(&b, "You are Mate, the onboard passage-planning assistant aboard %s, a %s %s (LOA %s). The crew address you as Mate.\n\n", vesselLabel, boatModel, hullPhrase, loaLabel)
	} else {
		fmt.Fprintf(&b, "You are Mate, the onboard passage-planning assistant aboard %s, a %s (LOA %s). The crew address you as Mate.\n\n", vesselLabel, boatModel, loaLabel)
	}

	// 2. Local time.
	loc := vesselLocalLocation(pc.Longitude)
	fmt.Fprintf(&b, "Local time: %s (%s, derived from the vessel's longitude). Give every time in this zone unless a tool result states a different zone.\n\n",
		pc.Now.In(loc).Format("Monday 2 January 2006 15:04"), assistantTimeZoneLabel(pc.Longitude))

	// 3. Position + place name.
	if hasUsableVesselPosition(pc.Latitude, pc.Longitude) {
		if place := strings.TrimSpace(pc.PlaceName); place != "" {
			fmt.Fprintf(&b, "Position: %.4f, %.4f (near %s).\n\n", pc.Latitude, pc.Longitude, place)
		} else {
			fmt.Fprintf(&b, "Position: %.4f, %.4f.\n\n", pc.Latitude, pc.Longitude)
		}
	} else {
		b.WriteString("Position unknown: ask the operator for the vessel's position or a place name to plan around.\n\n")
	}

	// 4. Live heading / speed / apparent wind.
	headingLabel := "unknown"
	if pc.HeadingTrue >= 0 {
		headingLabel = fmt.Sprintf("%.0f° true", pc.HeadingTrue)
	}
	sogLabel := "unknown"
	if pc.SpeedOverGroundKts >= 0 {
		sogLabel = fmt.Sprintf("%.1f kts", pc.SpeedOverGroundKts)
	}
	windLabel := "unknown"
	if pc.WindSpeedApparentKts >= 0 && pc.WindAngleApparentDeg >= 0 {
		if side := strings.TrimSpace(pc.WindSide); side != "" {
			windLabel = fmt.Sprintf("%.0f kts at %.0f° (%s)", pc.WindSpeedApparentKts, pc.WindAngleApparentDeg, side)
		} else {
			windLabel = fmt.Sprintf("%.0f kts at %.0f°", pc.WindSpeedApparentKts, pc.WindAngleApparentDeg)
		}
	}
	fmt.Fprintf(&b, "Heading: %s. Speed over ground: %s. Apparent wind: %s.\n\n", headingLabel, sogLabel, windLabel)

	// 5. Marine warnings.
	switch {
	case !pc.WarningOK:
		b.WriteString("Marine warnings: the forecast warnings feed has not reported yet.\n\n")
	case pc.WarningLevel <= 0:
		fmt.Fprintf(&b, "Marine warnings: no official marine warning as of %s.\n\n", pc.WarningFetchedAt.In(loc).Format("15:04"))
	default:
		line := fmt.Sprintf("Marine warnings: %s in force (wind level %d).", assistantWarningLevelLabel(pc.WarningLevel), pc.WarningLevel)
		if pc.WarningSurf {
			line += " A surf advisory is also in force."
		}
		b.WriteString(line)
		b.WriteString("\n\n")
	}

	// 6. Configured providers and tide station.
	tideStation := strings.TrimSpace(pc.TideStationName)
	if tideStation == "" {
		tideStation = "none configured"
	}
	fmt.Fprintf(&b, "Weather provider: %s. Wave provider: %s. Tide provider: %s. Configured tide station: %s.\n\n",
		providerLabelOrNotConfigured(pc.WeatherProvider), providerLabelOrNotConfigured(pc.WaveProvider),
		providerLabelOrNotConfigured(pc.TideProvider), tideStation)

	if indexLine := manualIndexLine(pc.ManualPages); indexLine != "" {
		b.WriteString(indexLine)
		b.WriteString("\n\n")
	}

	// 6a. Screen context: only present when the POST for this turn carried a
	// non-empty screen field (postAssistantMessageHandler), so a
	// text-composer question - the overwhelming majority - renders exactly
	// as it did before this field existed.
	if sentence := assistantScreenSentence(pc.Screen); sentence != "" {
		b.WriteString(sentence)
	}

	// 7. Tool-use guidance.
	b.WriteString("When the question is about Helmcentral itself, what a panel or chart shows or how to " +
		"configure it, call read_manual for the relevant page first and answer from it; when it is about the " +
		"sea, use the forecast, tide and passage tools as usual.\n\n")

	b.WriteString("To resolve a place, call find_places with the bare feature name (\"Bona Bay\", not \"Bona Bay, " +
		"Gloucester Island\"). If it is more than about 20 nautical miles from the vessel, or the lookup returns " +
		"nothing, resolve a nearby feature you can name (the island, the cape, the harbour) and retry with its " +
		"coordinates as near_lat/near_lon. If a name still will not resolve, say so and use the nearest resolved " +
		"feature's position instead, telling the operator that is what you did.\n\n" +

		"For every candidate anchorage under discussion, fetch both get_wind_forecast and get_tides. Fetch the " +
		"forecast once per position with enough days for the whole plan; do not re-fetch the same position " +
		"under a different name.\n\n" +

		"Shape an anchorage or passage answer as a pilotage briefing, not a forecast readout. For each anchorage: " +
		"the wind directions it is sheltered from and which would make it a lee shore, reasoned from the " +
		"coastline and the direction it faces, plus holding, depths, swinging room and hazards where you know " +
		"them. The forecast against that shelter: wind by time of day across the stay, any shift into an exposed " +
		"quadrant, sea state. Tides where they matter, including tidal streams through passages and narrows when " +
		"you know them, and where the standing notes make tide state decisive. For a passage: distance and " +
		"bearing, timing, approach hazards. Fallback anchorages nearby if conditions change. A recommendation, " +
		"with reasons, and what would change it.\n\n" +

		"Whenever the question names where the boat is leaving from and where it is going, or a route, the " +
		"answer must include a Passage section, even when the question is mainly about the anchorage. For it: " +
		"take the distance_nm and bearing_deg that find_places returns for the destination from the vessel's " +
		"position; call estimate_passage with that distance and the planned speed and report time, fuel burn " +
		"and fuel margin; pass that bearing as course_deg to get_wind_forecast for the destination and read " +
		"the wind and sea angle off the returned rel_wind and rel_wave labels (head, bow, beam, quarter, " +
		"following); do not work the angle out yourself. A power catamaran runs comfortably in a following " +
		"sea and slows and burns more into a head sea. Say which it is for this passage.\n\n" +

		"Label the source of every claim: what comes from today's forecast and tide data, and what comes from " +
		"general knowledge, which the operator must check against the chart and cruising guide. Never present " +
		"general knowledge as a measurement.\n\n" +

		"Treat the operator's standing notes below as the first source for local rules; where they conflict with " +
		"general knowledge, the notes win.\n\n" +

		"If a tool returns an error, say so plainly; do not guess or fabricate a plausible answer.\n\n" +

		"Use knots for wind speed, nautical miles for distance, and metres for wave height and depth; give every " +
		"time in the local zone.\n\n" +

		"Write like a delivery skipper briefing the owner before casting off. No exclamation marks, no opener " +
		"like \"Good!\", no closing verdict line like \"looks like a comfortable passage\". Let length follow the " +
		"question; use markdown headings and a comparison table when weighing two or more options.\n\n")

	// 7 continued: only present for this turn when the POST carried
	// spoken:true (postAssistantMessageHandler) - a voice question wants a
	// short read-aloud tail on top of the written briefing above, not
	// instead of it.
	if pc.Spoken {
		b.WriteString("The operator asked by voice. End the answer with a heading exactly `## Spoken summary` " +
			"followed by at most three sentences that can be read aloud: the recommendation and the one number " +
			"that matters. Everything above that heading is the written briefing as usual.\n\n")
	}

	// 8. Operator standing notes, verbatim.
	notes := strings.TrimSpace(pc.Notes)
	if notes == "" {
		notes = "(none)"
	}
	fmt.Fprintf(&b, "Operator standing notes:\n%s", notes)

	return b.String()
}
