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

// angleDiffDeg is the smaller angle between two bearings, 0 to 180. It stays a
// float because relativeAngleDeg (assistant_geometry.go) rounds to whole
// degrees, which would let 25.4 degrees pass the inclusive 25.0 edge.
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

// nearbyFeatures names, ranks and measures what a provider returned from the
// boat's position: named features within 10 nm, nearest first. It reuses the
// Nearby tile's decoration, so distance, bearing and de-duplication agree.
func nearbyFeatures(raw []poiFeatureRaw, lat, lon float64) []inboxFeature {
	named := make([]poiFeatureRaw, 0, len(raw))
	for _, f := range raw {
		if f.Name = strings.TrimSpace(f.Name); f.Name != "" {
			named = append(named, f)
		}
	}
	ranked := decorateAndRankPOIFeatures(named, lat, lon, inboxFeatureRangeNm*metersPerNauticalMile, len(named))
	out := make([]inboxFeature, 0, len(ranked))
	for _, f := range ranked {
		out = append(out, inboxFeature{
			Name: f.Name, Category: f.Category,
			DistanceNm: f.DistanceM / metersPerNauticalMile, BearingDeg: f.BearingDeg,
		})
	}
	return out
}

// nearestInSector is the nearest feature that lies where the camera pointed:
// the first one in the sector of a list ranked nearest first (nearbyFeatures).
func nearestInSector(ranked []inboxFeature, cameraTrueDeg float64) (inboxFeature, bool) {
	for _, f := range ranked {
		if inSector(cameraTrueDeg, f.BearingDeg) {
			return f, true
		}
	}
	return inboxFeature{}, false
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

// bearingText is a bearing as the prompt gives it: whole degrees from 000 to
// 359, so -10 reads 350° and 359.6 reads 000°.
func bearingText(deg float64) string {
	return fmt.Sprintf("%03d°", int(math.Round(normalizeDegrees(deg)))%360)
}

// describe is the line Mate reads: name, distance, and the side of the course
// it lies on ("Cape Capricorn, 3.1 nm, 40° to port"). With no course it can
// only give the true bearing.
func (f inboxFeature) describe(courseDeg *float64) string {
	head := fmt.Sprintf("%s, %.1f nm", f.Name, f.DistanceNm)
	if courseDeg == nil {
		return fmt.Sprintf("%s, bearing %s true", head, bearingText(f.BearingDeg))
	}
	rel := relativeBearingDeg(*courseDeg, f.BearingDeg)
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

// inboxFetched is what the provider returned around a position. Truncated says
// it cut a category short, so the list may lack the very nearest feature.
type inboxFetched struct {
	Features  []poiFeatureRaw
	Truncated bool
}

// inboxFeaturesNear lists the named islands and bays around a position from the
// configured place-names provider: the same plugin that names places, asked for
// its island and bay categories out to 10 nm plus a margin. A var so tests can
// stand in. It fails rather than answering from nothing when the provider is
// missing or cannot list features.
var inboxFeaturesNear = func(lat, lon float64) (inboxFetched, error) {
	pn, id, err := placeNameProviderResolve()
	if err != nil {
		return inboxFetched{}, fmt.Errorf("place-names provider: %w", err)
	}
	poi, ok := pn.(poiProvider)
	if !ok {
		return inboxFetched{}, fmt.Errorf("place-names provider %q cannot list nearby features", id)
	}
	radiusM := int(math.Round((inboxFeatureRangeNm + inboxFeatureFetchPadNm) * metersPerNauticalMile))
	res, err := poi.FetchPOI(lat, lon, radiusM, inboxFeatureCategories, inboxFeatureLimit)
	if err != nil {
		return inboxFetched{}, fmt.Errorf("place-names provider %q: %w", id, firstLineErr(err))
	}
	if len(res.Truncated) > 0 {
		// The provider caps each category, so in a crowded archipelago the very
		// nearest feature may be missing from the list.
		log.Printf("inbox: nearby features near %.3f,%.3f cut short by %q for %v", lat, lon, id, res.Truncated)
	}
	return inboxFetched{Features: res.Features, Truncated: len(res.Truncated) > 0}, nil
}

// firstLineErr trims a provider error to its first line for the operator.
func firstLineErr(err error) error { return fmt.Errorf("%s", firstErrorLine(err)) }
