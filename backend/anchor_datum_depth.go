package main

import (
	"encoding/json"
	"log"
	"math"
	"os"
	"strings"
)

// maxTransducerDepthM bounds anchor.transducer_depth_m: a transducer sits
// under the hull, not at the keel of a ship.
const maxTransducerDepthM = 5.0

// Where a watch's planning depth came from. Empty means it cannot be known.
const (
	planningDepthFromSounder  = "sounder"
	planningDepthFromOperator = "operator"
)

// computeDatumDepth is the water under the anchor at chart datum. A sounder
// figure is read below the transducer, so the transducer's depth under the
// waterline is added; a figure the operator typed is taken as depth below the
// surface and needs no transducer. Either way the tide height at the moment the
// depth was read is taken off. It returns nil and a reason an operator can act
// on when any input is missing, or when the depth's source is not known; it
// never substitutes a value. An unknown input is a planning depth of 0 or less,
// a transducer depth of 0, or a nil tide height. A tide height of any size,
// negative included, is a reading.
//
// tideDatum is what the tide height was measured from when it was captured
// (ADR 0273). Only a height above chart datum can be taken off a depth to give
// a depth at chart datum; one from mean sea level cannot, so it gives no depth
// and says why. Empty means chart datum, which is what every watch stored
// before the field existed holds.
func computeDatumDepth(planningDepthM float64, planningTideHeightFt *float64, source string, transducerDepthM float64, tideDatum string) (*float64, string) {
	corrects, why := tideCorrectsChartDepth(tideDatum)
	switch {
	case planningDepthM <= 0 || math.IsNaN(planningDepthM):
		return nil, "No depth was recorded for this watch."
	case source != planningDepthFromSounder && source != planningDepthFromOperator:
		return nil, "This watch does not record whether its depth came from the sounder or typed in. Enter the depth again in the rode planner."
	case !corrects:
		return nil, strings.ToUpper(why[:1]) + why[1:] + "."
	case planningTideHeightFt == nil || !finite(*planningTideHeightFt):
		return nil, "The tide height at the drop is unknown."
	}
	v := planningDepthM - *planningTideHeightFt/metersToFeet
	if source == planningDepthFromSounder {
		if transducerDepthM <= 0 || math.IsNaN(transducerDepthM) {
			return nil, "Set the transducer depth in Settings → Anchor Watch."
		}
		v += transducerDepthM
	}
	return &v, ""
}

// optionalFloat is a JSON number that can be told apart from null and from
// absent: Present is true for either of the first two, and Value is nil for
// null. A request can then say "no tide height" without a stand-in number.
type optionalFloat struct {
	Present bool
	Value   *float64
}

func (o *optionalFloat) UnmarshalJSON(b []byte) error {
	o.Present = true
	if string(b) == "null" {
		o.Value = nil
		return nil
	}
	var f float64
	if err := json.Unmarshal(b, &f); err != nil {
		return err
	}
	o.Value = &f
	return nil
}

// maxPlanningTideFt bounds a tide height the watch will store: no tide, surge
// included, comes near it.
const maxPlanningTideFt = 150.0

// checkPlanningTide validates a request's planning tide height against the
// datum it says it was measured from, and returns the datum to store:
// chart_datum or mean_sea_level when a height is given, and "" when it is not.
// A height is a reading whatever its sign, on either datum, so it only has to be a sensible number: a minus
// tide below chart datum is real. A nil height is none. A datum with no height
// to go with it is not stored.
func checkPlanningTide(heightFt *float64, datum *string) (stored string, msg string) {
	d := ""
	if datum != nil {
		d = *datum
	}
	switch d {
	case "", tideDatumChart:
	case tideDatumMeanSeaLevel:
		stored = tideDatumMeanSeaLevel
	default:
		return "", "planning_tide_datum must be chart_datum or mean_sea_level"
	}
	if heightFt == nil {
		return "", ""
	}
	if !finite(*heightFt) || math.Abs(*heightFt) > maxPlanningTideFt {
		return "", "planning_tide_height_ft must be a height within 150 ft either side of the datum"
	}
	// A height always carries its datum when stored; see dropLegacyTideMarker.
	if stored == "" {
		stored = tideDatumChart
	}
	return stored, ""
}

// transducerDepthSetting reads anchor.transducer_depth_m from settings.yaml,
// following the settings cache without copying the document or building the
// whole settings payload: the anchor watch is polled constantly.
func transducerDepthSetting(settingsPath string) (float64, error) {
	settings, err := readSettingsShared(settingsPath)
	if err != nil {
		return 0, err
	}
	anchor, _ := settings["anchor"].(map[string]any)
	return coerceFloat(anchor["transducer_depth_m"]), nil
}

// readSettingsShared is readSettings without the deep copy: the caller must
// only read the returned map. A cache miss parses and caches as readSettings does.
func readSettingsShared(settingsPath string) (map[string]any, error) {
	if info, err := os.Stat(settingsPath); err == nil {
		if shared, ok := sharedCachedSettings(settingsPath, info); ok {
			return shared, nil
		}
	}
	return readSettings(settingsPath)
}

// withDatumDepth adds planning_depth_source, datum_depth_m (null when it cannot
// be worked out) and datum_depth_reason (empty when it can) to an anchor watch
// response. A settings file that cannot be read is a reason, not a quiet zero.
func withDatumDepth(resp map[string]any, aw *anchorWatchData) map[string]any {
	resp["planning_depth_source"] = aw.PlanningDepthSource
	resp["planning_tide_datum"] = aw.PlanningTideDatum
	transducerM, err := transducerDepthSetting(getEnv("SETTINGS_FILE", "../settings.yaml"))
	if err != nil {
		log.Printf("anchor watch: reading the transducer depth from settings failed: %v", err)
		resp["datum_depth_m"] = nil
		resp["datum_depth_reason"] = "Settings could not be read, so the transducer depth is unknown."
		return resp
	}
	depth, reason := computeDatumDepth(aw.PlanningDepthM, aw.PlanningTideHeightFt, aw.PlanningDepthSource, transducerM, aw.PlanningTideDatum)
	if depth == nil {
		resp["datum_depth_m"] = nil
	} else {
		resp["datum_depth_m"] = math.Round(*depth*1000) / 1000
	}
	resp["datum_depth_reason"] = reason
	return resp
}
