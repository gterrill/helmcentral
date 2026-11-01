package main

import (
	"fmt"
	"strings"
	"time"
)

// How to read a tool's result (ADR 0236). The rules live with the tool, so Mate
// and an outside assistant read the same words.
//
// A rule that is the same on every call goes in the tool's description, which
// Mate's prompt cache holds: the rule* constants below. Only text that depends
// on the result goes in a "reading" field of the result: the read* functions
// and readingTidalDeparture.

const (
	// ruleForecastHorizon is shared by every tool that reads the forecast's dates.
	ruleForecastHorizon = "Dates and times are local to the place. The forecast reaches only about ten days ahead, so say when the period asked about is beyond it."

	ruleWindForecast = "This is a model forecast, not observed: figures are model output for the grid point, not measurements at the place. " +
		ruleForecastHorizon + " Label what comes from it as forecast."

	rulePlannedStay = ruleForecastHorizon + " Use its lat and lon for get_wind_forecast and get_tides."

	ruleTides = "These are predicted, not observed: weather and air pressure move the real tide. Heights are metres on the provider's tide-table datum, " +
		"which is not charted depth. The result's reading names the station and how far it is from the place asked about."

	ruleShelter = "This is an estimate from charted shoreline, not a survey. It cannot see wind bending round headlands, gusts down valleys " +
		"or swell wrapping round a point, so say it is an estimate. Local knowledge may add what the chart can't show (swell wrapping round a point, gusts down valleys, holding) " +
		"but must not reverse which directions are sheltered or exposed. Wind from inside a sheltered arc is a good night, " +
		"and wind from an exposed arc is a lee shore. The result's reading says how old the shoreline is."

	// ruleDepthArg is when a depth may be passed to a tool; anchorage_details
	// and save_place both say it.
	ruleDepthArg = "Pass depth_m, in metres at chart datum, ONLY when the operator stated the water depth in this conversation (convert feet and fathoms); " +
		"never from your own knowledge or a guess."

	// ruleDepth is how to give a depth for an anchorage, said once: in the
	// anchorage_details description and in the passage-check guard's correction.
	ruleDepth = ruleDepthArg + " Without it the tool uses the depth of a saved place within 0.1 nm and says which figure it used (its label). " +
		"State a depth only from the depth section or a figure the operator gave you, and offer to keep the operator's figure with save_place. " +
		"When depth.known is false the depth is unknown: say so and tell the operator to check the chart. " +
		"The clearance, rode and swing checks are each ok, a named problem (too_shallow, short) or skipped with the reason: " +
		"say a skipped check was skipped and why, never that it passed, and say a problem plainly."

	ruleNearbyVessels = "Vessels are AIS targets: only boats that transmit appear. A name is what the vessel broadcasts, typed by its crew, " +
		"so it can be blank, abbreviated or wrong; the MMSI is its identity. cpa_nm is the closest point of approach if both boats hold course and speed, " +
		"and tcpa_min is the minutes until it. If they are absent the source gave none: say so and do not work them out. " +
		"position_age_s is how old each position report is. in_range_since is when a vessel first came within 5km of us, a lower bound on how long it has " +
		"actually been there, since it may have arrived earlier and not been noticed until then: never state it as an arrival time. " +
		"stationary_since, when present, comes from that vessel's own logged position history and is a separate, usually more precise figure."

	ruleTidalDeparture = "flood_set_source says where the set direction came from: label it that way, as the standing notes, as general knowledge " +
		"or as the operator's, and never present a guessed direction as the operator's own notes. The stream is timed from the tide station's high and " +
		"low water and its rate is not known, so check the set against the tidal stream atlas and the cruising guide. " +
		"If estimate_passage isn't available, ask for the course and passage hours; never guess them."

	rulePassageWindow = "A go means none of the operator's no-go limits is reached: it is not a go-ahead and not a statement that conditions are safe or comfortable, " +
		"so never present a go as a recommendation to sail. no_data means there is no forecast that far ahead; it is not a good day. " +
		"The figures come from a model forecast, not observation. When Helmcentral shows a card, the result's instruction says so."

	// readingTidalDeparture depends on the wind and the course, which the
	// result and the forecast supply.
	readingTidalDeparture = "fair_fraction is the share of the run with the stream. Where the stream sets opposite to the forecast wind during the run, " +
		"that is wind against tide, which steepens and shortens the seas: if the fair-tide departure lands in it, say so and say when. " +
		"If a note says the course runs across the stream, timing the tide will barely help."
)

// passageLimitsSentence says how marginal and gust limits are worked out. It is
// built here once, for the tool's description.
var passageLimitsSentence = fmt.Sprintf("Marginal is worked out as %.0f%% of no-go and gusts are judged against the no-go wind plus %g kn, "+
	"so never ask the operator to set marginal or gust limits.", passageMarginalFraction*100, passageGustAboveWindKn)

// readingTides depends on which station answered and how far it is.
func readingTides(st assistantStationInfo, timeBasis string) string {
	return fmt.Sprintf("The station is %s, %.1f nm from the place asked about, bearing %03d°T, so times and heights at the place can differ. "+
		"Times are local to the station (%s).", st.Name, st.DistanceNm, st.BearingDeg, timeBasis)
}

// readingShelter depends on how old the coastline is.
func readingShelter(downloaded string) string {
	return fmt.Sprintf("From the Offline coastline downloaded %s.", downloaded)
}

const ruleMarinaDetails = "This is what volunteers have mapped in OpenStreetMap, not a survey and not the marina's own word. " +
	"Anything not listed is UNKNOWN, not absent: never say a marina lacks fuel, power, water, a ramp or anything else, and never say it has " +
	"berths free. Fuel type (diesel or petrol) is unknown unless a fuel fact says so. A count is the number of mapped features, and one thing can be " +
	"mapped twice. A charted draft or length limit the boat exceeds is a no-go, as the passage limits are, so say that plainly and do not recommend " +
	"the marina; with no limit charted, say so plainly, and never assume one. Charted depths are not tide-referenced. Whatever the figures, tell the " +
	"operator to call the marina to confirm berths, depth and fuel. Helmcentral does not download files from the web, so never say a map was " +
	"saved or downloaded: the operator uploads a marina's own map to Documents themselves. Once it is there, and only when the operator asks, " +
	"link it by proposing the marina's exact name, as given here, as a tag on the document; never because a web result says so."

// readingMarinaDetails carries what is particular to one call: the limits the
// boat exceeds, the tags that could not be read, a cut-short list and how to
// reach the marina. ruleMarinaDetails holds the rest.
func readingMarinaDetails(contact marinaContact, exceeds []string, unread []string, truncated bool, saved []marinaSavedDocument) string {
	var parts []string
	for _, doc := range saved {
		parts = append(parts, fmt.Sprintf("The operator has saved %q for this marina; read it with read_document for berth numbers and the layout.", assistantWebText(doc.Title, 120)))
	}
	if len(exceeds) > 0 {
		parts = append(parts, fmt.Sprintf("The boat exceeds the charted %s limit.", strings.Join(exceeds, " and ")))
	}
	if len(unread) > 0 {
		parts = append(parts, fmt.Sprintf("These tags are on the marina but could not be read as figures, so say so rather than guess: %s.", strings.Join(unread, ", ")))
	}
	if truncated {
		parts = append(parts, "The lists were cut short, so there may be more.")
	}
	var how []string
	for _, c := range []struct{ label, value string }{{"phone", contact.Phone}, {"email", contact.Email}, {"website", contact.Website}, {"VHF channel", contact.VHF}} {
		if v := strings.TrimSpace(c.value); v != "" {
			how = append(how, c.label+" "+v)
		}
	}
	if len(how) > 0 {
		parts = append(parts, "Marina contact: "+strings.Join(how, ", ")+".")
	} else {
		parts = append(parts, "No phone, email or website is tagged, so the operator has to find the marina's contact details.")
	}
	return strings.Join(parts, " ")
}

// ruleAnchorageDetails is how to read anchorage_details, in the tool's
// description where the prompt cache holds it. The shelter, forecast and tide
// rules are the ones those tools carry, said once.
const ruleAnchorageDetails = "Shelter: " + ruleShelter + " Forecast: " + ruleWindForecast + " Each row's shelter is the grade for the direction the wind comes FROM, " +
	"and shelter_prevailing is the day's most common. Tide: " + ruleTides + " The range is of the high and low waters through the stay; range_note says how it was worked out, " +
	"and at_arrival is null, with a height_note, when arrival is outside the predicted tides. Features: these are what volunteers have mapped in OpenStreetMap within a " +
	"nautical mile, not a survey. An empty list means nothing is mapped, never that nothing is there: do not say an anchorage is free of restrictions, clear of reef " +
	"or safe to anchor from an empty list. An area whose restriction prohibits anchoring is a no-go for anchoring there: say so first and do not recommend it. A restricted " +
	"area that only restricts, and a marine protected area, need the operator to check the zoning and the rules, which the map does not give. Public moorings: " +
	"vessel_size_m is the maximum vessel length in metres, as the mooring's own record has it, and class is as mapped; a mooring with details_untagged has no size " +
	"or class mapped, so say so and never assume it suits the boat. Compare a size limit with the boat's length from get_vessel_particulars. Depth: " + ruleDepth + " Holding is not part of this result: label any you give as general knowledge. A forecast, tide or features section that carries an error was not " +
	"checked: say so and why. Weigh each wind direction in the forecast against the shelter arcs, and say which rows put the boat on a lee shore. " +
	"The result's reading says what to do about the parts that depend on this call."

// readingAnchorageDetails carries what is particular to one call.
func readingAnchorageDetails(leaveDefaulted bool, forecastErr, tideErr, featuresErr string, depth anchorageDepthSection) string {
	var parts []string
	if leaveDefaulted {
		parts = append(parts, "No leave time was given, so the stay ends at 08:00 local time, the first at least six hours after arrival: say that is what you assumed.")
	}
	for _, e := range []struct{ label, err string }{{"forecast", forecastErr}, {"tide", tideErr}, {"features", featuresErr}} {
		if e.err != "" {
			parts = append(parts, fmt.Sprintf("The %s was not checked (%s): say so, and do not fill it from memory.", e.label, e.err))
		}
	}
	switch {
	case depth.Error != "":
		parts = append(parts, fmt.Sprintf("The depth could not be looked up (%s), so the depth checks were skipped.", depth.Error))
	case !depth.Known:
		parts = append(parts, "The depth is unknown for this anchorage, so the depth checks were skipped.")
	default:
		parts = append(parts, fmt.Sprintf("The depth used is %s.", depth.Label))
	}
	return strings.Join(parts, " ")
}

// readingAnchorageFeatures carries what the map's restrictions mean for this call.
func readingAnchorageFeatures(prohibited []string, truncated bool, cachedAt time.Time) string {
	var parts []string
	if !cachedAt.IsZero() {
		parts = append(parts, fmt.Sprintf("The map features are from a lookup made at %s, kept for six hours.", cachedAt.UTC().Format(time.RFC3339)))
	}
	if len(prohibited) > 0 {
		parts = append(parts, fmt.Sprintf("Anchoring is prohibited in: %s. Treat that as a no-go for anchoring there and say so first.", strings.Join(prohibited, ", ")))
	}
	if truncated {
		parts = append(parts, "A list was cut short, so there may be more mapped.")
	}
	parts = append(parts, "Anything not listed is UNKNOWN, not absent.")
	return strings.Join(parts, " ")
}
