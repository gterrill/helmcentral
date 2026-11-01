package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"time"
)

// The anchorage_features export of a POI plugin (ADR 0244): what OpenStreetMap
// records within a nautical mile of a position that bears on anchoring there.
// Optional like marina_details. A plugin without it gets a named error from the
// tool's features section, never an empty list, since an empty list reads as
// "nothing there".

// Bounds on one answer. A list cut to its bound sets Truncated.
const (
	anchorageMaxRestricted = 50 + 20 // seamark areas plus marine protected areas
	anchorageMaxAreas      = 50      // anchorage areas, and seabed areas, each
	anchorageMaxMoorings   = 200
	anchorageMaxRadiusM    = 1852 * 2 // a distance the plugin could not report from a 1 nm search
	anchorageMaxVesselM    = 1000.0
)

// anchorageFeaturesProvider is implemented by a place-names provider whose
// plugin exports anchorage_features.
type anchorageFeaturesProvider interface {
	AnchorageFeatures(lat, lon float64) (anchorageFeatures, error)
}

// errNoAnchorageFeaturesExport is what a plugin without the export answers with.
var errNoAnchorageFeaturesExport = errors.New("no anchorage_features export")

// The model-facing record types. The wire types below embed them and add the
// geometry a map draws, which the model never reads.
type anchorageRestrictedArea struct {
	OSM    string `json:"osm"`
	Name   string `json:"name"`
	Source string `json:"source"` // "seamark" or "protected_area"
	// Restriction is the S-57 restriction as mapped (anchoring_prohibited),
	// or for a protected area its protection title.
	Restriction string `json:"restriction"`
	Category    string `json:"category"`
	// DistanceM is nil for a protected area: only its boundary was within a
	// nautical mile, and its centre says nothing about that.
	DistanceM *int `json:"distance_m"`
}

type anchorageMooringRecord struct {
	OSM             string   `json:"osm"`
	Name            string   `json:"name"`
	Ref             string   `json:"ref"`
	VesselSizeM     *float64 `json:"vessel_size_m"`
	Class           string   `json:"class"`
	DetailsUntagged bool     `json:"details_untagged"`
	DistanceM       int      `json:"distance_m"`
}

type anchorageAreaRecord struct {
	OSM       string `json:"osm"`
	Name      string `json:"name"`
	Category  string `json:"category"`
	DistanceM int    `json:"distance_m"`
}

type anchorageSeabedRecord struct {
	OSM       string `json:"osm"`
	Name      string `json:"name"`
	Surface   string `json:"surface"`
	DistanceM int    `json:"distance_m"`
}

type anchorageBayRecord struct {
	OSM       string `json:"osm"`
	Name      string `json:"name"`
	Contains  bool   `json:"contains"`
	DistanceM int    `json:"distance_m"`
}

type anchorageRestrictedWire struct {
	anchorageRestrictedArea
	Geometry []marinaShape `json:"geometry"`
}

type anchorageMooringWire struct {
	anchorageMooringRecord
	Lat float64 `json:"lat"`
	Lon float64 `json:"lon"`
}

type anchorageAreaWire struct {
	anchorageAreaRecord
	Geometry []marinaShape `json:"geometry"`
}

type anchorageSeabedWire struct {
	anchorageSeabedRecord
	Geometry []marinaShape `json:"geometry"`
}

// anchorageFeatures is the guest's answer, decoded once.
type anchorageFeatures struct {
	RestrictedAreas []anchorageRestrictedWire `json:"restricted_areas"`
	Moorings        []anchorageMooringWire    `json:"moorings"`
	AnchorageAreas  []anchorageAreaWire       `json:"anchorage_areas"`
	SeabedAreas     []anchorageSeabedWire     `json:"seabed_areas"`
	Bay             *anchorageBayRecord       `json:"bay"`
	Truncated       bool                      `json:"truncated"`
	// DataAsOf is Overpass's data time, an RFC 3339 time, or empty.
	DataAsOf string `json:"data_as_of"`
}

// decodeAnchorageFeatures reads the guest's JSON strictly. Every text comes
// from a map anyone can edit, so it is cleaned once, here, before it reaches
// Mate. A malformed answer is an error, not a thinner result.
func decodeAnchorageFeatures(pluginID string, raw []byte) (anchorageFeatures, error) {
	var d anchorageFeatures
	if err := json.Unmarshal(raw, &d); err != nil {
		return anchorageFeatures{}, fmt.Errorf("plugin %q: unparseable anchorage_features JSON: %w", pluginID, err)
	}
	bad := func(format string, args ...any) (anchorageFeatures, error) {
		return anchorageFeatures{}, fmt.Errorf("plugin %q: anchorage_features returned %s", pluginID, fmt.Sprintf(format, args...))
	}
	if d.DataAsOf != "" {
		if _, err := time.Parse(time.RFC3339, d.DataAsOf); err != nil {
			return bad("a data date %q that is not an RFC 3339 time", sanitizeMapText(d.DataAsOf, marinaTextRunes))
		}
	}

	for i := range d.RestrictedAreas {
		r := &d.RestrictedAreas[i]
		if r.Source != "seamark" && r.Source != "protected_area" {
			return bad("a restricted area from the unknown source %q", sanitizeMapText(r.Source, marinaTextRunes))
		}
		if msg := checkAnchorageRecord("a restricted area", &r.OSM, &r.Name, r.DistanceM); msg != "" {
			return bad("%s", msg)
		}
		if r.Source == "seamark" && r.DistanceM == nil {
			return bad("a seamark restricted area with no distance")
		}
		r.Restriction = sanitizeMapText(r.Restriction, marinaTextRunes)
		r.Category = sanitizeMapText(r.Category, marinaTextRunes)
		if !validShapes(r.Geometry) {
			return bad("a restricted area with an invalid shape")
		}
	}
	d.RestrictedAreas = capFeatureList(d.RestrictedAreas, anchorageMaxRestricted, &d.Truncated)

	for i := range d.Moorings {
		m := &d.Moorings[i]
		if msg := checkAnchorageRecord("a mooring", &m.OSM, &m.Name, &m.DistanceM); msg != "" {
			return bad("%s", msg)
		}
		if !validLatLon(m.Lat, m.Lon) {
			return bad("a mooring at an impossible position")
		}
		if m.VesselSizeM != nil && (math.IsNaN(*m.VesselSizeM) || *m.VesselSizeM <= 0 || *m.VesselSizeM > anchorageMaxVesselM) {
			return bad("a mooring vessel size %g, which is not a plausible figure", *m.VesselSizeM)
		}
		m.Ref = sanitizeMapText(m.Ref, marinaTextRunes)
		m.Class = sanitizeMapText(m.Class, marinaTextRunes)
		// The flag is derived here, not trusted: untagged means nothing was read.
		m.DetailsUntagged = m.VesselSizeM == nil && m.Class == ""
	}
	d.Moorings = capFeatureList(d.Moorings, anchorageMaxMoorings, &d.Truncated)

	for i := range d.AnchorageAreas {
		a := &d.AnchorageAreas[i]
		if msg := checkAnchorageRecord("an anchorage area", &a.OSM, &a.Name, &a.DistanceM); msg != "" {
			return bad("%s", msg)
		}
		a.Category = sanitizeMapText(a.Category, marinaTextRunes)
		if !validShapes(a.Geometry) {
			return bad("an anchorage area with an invalid shape")
		}
	}
	d.AnchorageAreas = capFeatureList(d.AnchorageAreas, anchorageMaxAreas, &d.Truncated)

	for i := range d.SeabedAreas {
		s := &d.SeabedAreas[i]
		if msg := checkAnchorageRecord("a seabed area", &s.OSM, &s.Name, &s.DistanceM); msg != "" {
			return bad("%s", msg)
		}
		if s.Surface = sanitizeMapText(s.Surface, marinaTextRunes); s.Surface == "" {
			return bad("a seabed area with no surface")
		}
		if !validShapes(s.Geometry) {
			return bad("a seabed area with an invalid shape")
		}
	}
	d.SeabedAreas = capFeatureList(d.SeabedAreas, anchorageMaxAreas, &d.Truncated)

	if b := d.Bay; b != nil {
		if msg := checkAnchorageRecord("a bay", &b.OSM, &b.Name, &b.DistanceM); msg != "" {
			return bad("%s", msg)
		}
		if b.Name == "" {
			return bad("a bay with no name")
		}
	}
	return d, nil
}

// checkAnchorageRecord cleans a record's id and name and checks its id and
// distance, which every record kind carries. A nil distance is absent, which
// only a protected area may be. It returns what is wrong, or "".
func checkAnchorageRecord(what string, osm, name *string, distanceM *int) string {
	if *osm = sanitizeMapText(*osm, marinaTextRunes); *osm == "" {
		return what + " with no OpenStreetMap id"
	}
	if distanceM != nil && (*distanceM < 0 || *distanceM > anchorageMaxRadiusM) {
		return what + " at an impossible distance"
	}
	*name = sanitizeMapText(*name, marinaTextRunes)
	return ""
}

// capFeatureList cuts list to limit, setting truncated when it did.
func capFeatureList[T any](list []T, limit int, truncated *bool) []T {
	if len(list) > limit {
		*truncated = true
		return list[:limit]
	}
	return list
}

// AnchorageFeatures calls the guest's anchorage_features export, with the
// same config overlay and panic recovery as MarinaDetails.
func (p *wasmPOIProvider) AnchorageFeatures(lat, lon float64) (anchorageFeatures, error) {
	return callOptionalExport(p, "anchorage_features", p.SupportsAnchorageFeatures(), errNoAnchorageFeaturesExport,
		wasmLatLonInput{Lat: lat, Lon: lon}, decodeAnchorageFeatures)
}
