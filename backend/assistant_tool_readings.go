package main

import "fmt"

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
		"or swell wrapping round a point, so say it is an estimate and let local knowledge correct it. Wind from inside a sheltered arc is a good night, " +
		"and wind from an exposed arc is a lee shore. The result's reading says how old the shoreline is."

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
