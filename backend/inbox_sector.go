package main

import (
	"fmt"
	"log"
	"math"
	"strings"
)

// The bearing sector and the nearby named land features (ADR 0282). A photo
// that carries the direction the phone faced is titled from the nearest named
// island or bay within +/-25 degrees of it, out to 10 nautical miles. The same
// features, five of them with distance and side of the bow, go to the vision
// model when Mate is set up.

const (
	// inboxSectorHalfWidthDeg is how far either side of the camera's bearing a
	// feature may lie and still count as where the camera pointed.
	inboxSectorHalfWidthDeg = 25.0
	// inboxFeatureRangeNm is how far out a feature is offered.
	inboxFeatureRangeNm = 10.0
	// inboxFeatureCount is how many of the nearest features Mate is shown.
	inboxFeatureCount = 5
	// inboxFeatureFetchPadNm widens the fetch around a cell's centre so every
	// photo taken inside the cell still has its full 10 nm.
	inboxFeatureFetchPadNm = 1.0
	// inboxFeatureLimit is the courtesy cap handed to the provider.
	inboxFeatureLimit = 100
)

// inboxFeatureCategories are the POI categories that name land a skipper
// would recognise from the water. The fixed category list has no headland or
// point (docs/reference/poi-categories.md), so a headland is only offered
// where it is mapped as a bay or island.
var inboxFeatureCategories = []string{"island", "bay"}

// inboxFeature is a named land feature seen from the boat's position.
type inboxFeature struct {
	Name     string
	Category string
	// DistanceNm from the boat; BearingDeg is the true bearing from the boat.
	DistanceNm float64
	BearingDeg float64
	// InSector says the feature lies in the direction the camera pointed.
	InSector bool
}

// angleDiffDeg is the smaller angle between two bearings, 0 to 180.
func angleDiffDeg(a, b float64) float64 {
	d := math.Mod(math.Abs(a-b), 360)
	if d > 180 {
		d = 360 - d
	}
	return d
}

// inSector says a feature at featureDeg lies within +/-25 degrees of the
// camera's true bearing. The edges count as inside.
func inSector(cameraTrueDeg, featureDeg float64) bool {
	return angleDiffDeg(cameraTrueDeg, featureDeg) <= inboxSectorHalfWidthDeg
}

// cameraBearingTrue turns the direction the phone recorded into degrees true.
// The reference is "T" or "M" (empty is true). A magnetic bearing needs the
// magnetic variation, east positive, to become true.
//
// Nothing in the box records variation yet: the track keeps position, speed
// and course over ground only, and Helmcentral reads no variation from
// SignalK. So the handlers pass nil, and a magnetic bearing is read as if it
// were true, an error of the local variation (a few degrees on this coast)
// that sits well inside the 25 degree half-width. The parameter is here so a
// variation source can be wired in without touching the maths (ADR 0282).
func cameraBearingTrue(bearingDeg float64, ref string, variationDeg *float64) float64 {
	if ref != "M" || variationDeg == nil {
		return bearingDeg
	}
	return math.Mod(math.Mod(bearingDeg+*variationDeg, 360)+360, 360)
}

// nearbyFeatures names, ranks and measures what a provider returned from the
// boat's position: named features within 10 nm, nearest first. It reuses the
// Nearby tile's decoration, so distance, bearing and de-duplication agree.
func nearbyFeatures(raw []poiFeatureRaw, lat, lon float64) []inboxFeature {
	named := make([]poiFeatureRaw, 0, len(raw))
	for _, f := range raw {
		if strings.TrimSpace(f.Name) != "" {
			named = append(named, f)
		}
	}
	ranked := decorateAndRankPOIFeatures(named, lat, lon, inboxFeatureRangeNm*metersPerNauticalMile, len(named))
	out := make([]inboxFeature, 0, len(ranked))
	for _, f := range ranked {
		out = append(out, inboxFeature{
			Name: strings.TrimSpace(f.Name), Category: f.Category,
			DistanceNm: f.DistanceM / metersPerNauticalMile, BearingDeg: f.BearingDeg,
		})
	}
	return out
}

// nearestInSector is the nearest feature that lies where the camera pointed.
func nearestInSector(features []inboxFeature, cameraTrueDeg float64) (inboxFeature, bool) {
	var best inboxFeature
	found := false
	for _, f := range features {
		if f.DistanceNm > inboxFeatureRangeNm || !inSector(cameraTrueDeg, f.BearingDeg) {
			continue
		}
		if !found || f.DistanceNm < best.DistanceNm {
			best, found = f, true
		}
	}
	return best, found
}

// markSector returns the features with InSector set for the camera's bearing.
func markSector(features []inboxFeature, cameraTrueDeg float64) []inboxFeature {
	out := make([]inboxFeature, len(features))
	for i, f := range features {
		f.InSector = inSector(cameraTrueDeg, f.BearingDeg)
		out[i] = f
	}
	return out
}

// describe is the line Mate reads: name, distance, and the side of the bow it
// lies on ("Cape Capricorn, 3.1 nm, 40° to port"). With no heading it can only
// give the true bearing.
func (f inboxFeature) describe(headingDeg *float64) string {
	head := fmt.Sprintf("%s, %.1f nm", f.Name, f.DistanceNm)
	if headingDeg == nil {
		return fmt.Sprintf("%s, bearing %03d° true", head, int(math.Round(f.BearingDeg))%360)
	}
	rel := relativeBearingDeg(*headingDeg, f.BearingDeg)
	switch {
	case rel == 0:
		return head + ", dead ahead"
	case rel == 180:
		return head + ", dead astern"
	case rel < 180:
		return fmt.Sprintf("%s, %d° to starboard", head, rel)
	default:
		return fmt.Sprintf("%s, %d° to port", head, 360-rel)
	}
}

// inboxFeaturesNear lists the named islands and bays around a position from the
// configured place-names provider: the same plugin that names places, asked for
// its island and bay categories out to 10 nm plus a margin. A var so tests can
// stand in. It fails rather than answering from nothing when the provider is
// missing or cannot list features.
var inboxFeaturesNear = func(lat, lon float64) ([]poiFeatureRaw, error) {
	pn, id, err := placeNameProviderResolve()
	if err != nil {
		return nil, fmt.Errorf("place-names provider: %w", err)
	}
	poi, ok := pn.(poiProvider)
	if !ok {
		return nil, fmt.Errorf("place-names provider %q cannot list nearby features", id)
	}
	radiusM := int(math.Round((inboxFeatureRangeNm + inboxFeatureFetchPadNm) * metersPerNauticalMile))
	res, err := poi.FetchPOI(lat, lon, radiusM, inboxFeatureCategories, inboxFeatureLimit)
	if err != nil {
		return nil, fmt.Errorf("place-names provider %q: %w", id, firstLineErr(err))
	}
	if len(res.Truncated) > 0 {
		// The provider caps each category, so in a crowded archipelago the very
		// nearest feature may be missing from the list.
		log.Printf("inbox: nearby features near %.3f,%.3f cut short by %q for %v", lat, lon, id, res.Truncated)
	}
	return res.Features, nil
}

// firstLineErr trims a provider error to its first line for the operator.
func firstLineErr(err error) error { return fmt.Errorf("%s", firstErrorLine(err)) }
