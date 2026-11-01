package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"sort"
	"time"
)

// The marina_details export of a POI plugin: what OpenStreetMap records about
// the marina charted at a position and the features around it. It is optional
// like place_name_at, and a plugin without it (google-places) gets a clear
// error, never an empty answer, since an empty answer would read as a marina
// with nothing in it.

// Bounds on what one answer may carry, so a busy harbour cannot flood Mate's
// context. A list cut to its bound sets Truncated; shapes cut to theirs set
// GeometryTruncated instead, since the model never reads geometry.
const (
	marinaMaxFacilities    = 60
	marinaMaxMarks         = 40
	marinaMaxAshore        = 30
	marinaMaxArms          = 60
	marinaMaxPontoons      = 200
	marinaMaxOutlineShapes = 20
	marinaMaxShapePoints   = 300 // the osm-overpass plugin thins every shape to this
	marinaMaxFacts         = 12
	marinaTextRunes        = 80
	marinaLongTextRunes    = 200
	marinaMaxLimitM        = 1000.0
)

// marinaDetailsProvider is implemented by a place-names provider whose plugin
// exports marina_details.
type marinaDetailsProvider interface {
	MarinaDetails(lat, lon float64) (marinaDetails, error)
}

// errNoMarinaDetailsExport is what a plugin without the export answers with.
var errNoMarinaDetailsExport = errors.New("no marina_details export")

// marinaDetails is the guest's answer, as decoded once. The wire types embed
// the types Mate sees and add the geometry a map draws, so the model-facing
// result is built from them without copying field by field.
type marinaDetails struct {
	Marina        marinaRecordWire     `json:"marina"`
	Facilities    []marinaFacilityWire `json:"facilities"`
	EntranceMarks []marinaMarkWire     `json:"entrance_marks"`
	Ashore        []marinaAshoreWire   `json:"ashore"`
	Truncated     bool                 `json:"truncated"`
	// GeometryTruncated says the map's shapes were cut to their bounds. It is
	// the card's to carry; the model reads no geometry and is not told.
	GeometryTruncated bool `json:"-"`
	// DataAsOf is when the OpenStreetMap data behind the answer was current,
	// an RFC 3339 time, or empty when the server did not say.
	DataAsOf string `json:"data_as_of"`
}

// marinaRecordWire is the marina's own record. Outline and Pontoons are drawn
// on a map by the host and are never part of what Mate is told: the model
// needs names, kinds and facts, and coordinates would only cost it context.
type marinaRecordWire struct {
	marinaContact
	marinaChartedLimits
	OSM          string          `json:"osm"`
	Name         string          `json:"name"`
	Lat          float64         `json:"lat"`
	Lon          float64         `json:"lon"`
	Berths       *int            `json:"berths"`
	Fee          string          `json:"fee"`
	PowerSupply  string          `json:"power_supply"`
	OpeningHours string          `json:"opening_hours"`
	UnreadTags   []string        `json:"unread_tags"`
	TaggedAbsent []string        `json:"tagged_absent"`
	PontoonArms  []string        `json:"pontoon_arms"`
	Outline      []marinaShape   `json:"outline"`
	Pontoons     []marinaPontoon `json:"pontoons"`
}

// marinaShape is a point, a line or a closed polygon as [lat, lon] pairs.
type marinaShape struct {
	Kind   string       `json:"kind"`
	Points [][2]float64 `json:"points"`
}

type marinaPontoon struct {
	Name    string      `json:"name"`
	Private bool        `json:"private"`
	Shape   marinaShape `json:"shape"`
}

// marinaFacilityWire is a facility with one shape per mapped feature counted.
type marinaFacilityWire struct {
	marinaFacility
	Geometry []marinaShape `json:"geometry"`
}

type marinaMarkWire struct {
	marinaMark
	Geometry []marinaShape `json:"geometry"`
}

// marinaAshoreWire adds the position that places the service on the map.
type marinaAshoreWire struct {
	marinaAshore
	Lat float64 `json:"lat"`
	Lon float64 `json:"lon"`
}

func validShape(sh marinaShape) bool {
	switch sh.Kind {
	case "point":
		if len(sh.Points) != 1 {
			return false
		}
	case "line":
		if len(sh.Points) < 2 {
			return false
		}
	case "polygon":
		// A polygon is closed: its last point repeats its first.
		if len(sh.Points) < 4 || sh.Points[0] != sh.Points[len(sh.Points)-1] {
			return false
		}
	default:
		return false
	}
	if len(sh.Points) > marinaMaxShapePoints {
		return false
	}
	for _, p := range sh.Points {
		if !validLatLon(p[0], p[1]) {
			return false
		}
	}
	return true
}

func validShapes(list []marinaShape) bool {
	for _, sh := range list {
		if !validShape(sh) {
			return false
		}
	}
	return true
}

// The facility, mark and ashore types are what Mate sees; the wire types
// above add the geometry to them.
type marinaFacility struct {
	Kind  string            `json:"kind"`
	Name  string            `json:"name,omitempty"`
	Count int               `json:"count"`
	Facts map[string]string `json:"facts,omitempty"`
}

type marinaMark struct {
	Kind   string `json:"kind"`
	Side   string `json:"side"`
	Colour string `json:"colour,omitempty"`
	Name   string `json:"name,omitempty"`
	Count  int    `json:"count"`
}

type marinaAshore struct {
	Kind         string `json:"kind"`
	Name         string `json:"name"`
	OpeningHours string `json:"opening_hours,omitempty"`
	DistanceM    int    `json:"distance_m"`
}

func cleanList(in []string, max int, truncated *bool) []string {
	out := make([]string, 0, len(in))
	for _, s := range in {
		if s = sanitizeMapText(s, marinaTextRunes); s != "" {
			out = append(out, s)
		}
	}
	if len(out) > max {
		out, *truncated = out[:max], true
	}
	return out
}

// decodeMarinaDetails reads the guest's JSON strictly. Every text in it comes
// from a map anyone can edit, so it is cleaned here, once, before it reaches
// Mate. A malformed answer is an error, not a thinner result.
func decodeMarinaDetails(pluginID string, raw []byte) (marinaDetails, error) {
	var d marinaDetails
	if err := json.Unmarshal(raw, &d); err != nil {
		return marinaDetails{}, fmt.Errorf("plugin %q: unparseable marina_details JSON: %w", pluginID, err)
	}
	bad := func(format string, args ...any) (marinaDetails, error) {
		return marinaDetails{}, fmt.Errorf("plugin %q: marina_details returned %s", pluginID, fmt.Sprintf(format, args...))
	}

	if d.DataAsOf != "" {
		if _, err := time.Parse(time.RFC3339, d.DataAsOf); err != nil {
			return bad("a data date %q that is not an RFC 3339 time", sanitizeMapText(d.DataAsOf, marinaTextRunes))
		}
	}

	m := &d.Marina
	if m.OSM = sanitizeMapText(m.OSM, marinaTextRunes); m.OSM == "" {
		return bad("a marina with no OpenStreetMap id")
	}
	if !validLatLon(m.Lat, m.Lon) {
		return bad("a marina at %g, %g, which is not a position", m.Lat, m.Lon)
	}
	if m.Berths != nil && *m.Berths < 0 {
		return bad("a negative berth count")
	}
	for name, v := range map[string]*float64{"max_draught_m": m.MaxDraughtM, "max_length_m": m.MaxLengthM, "depth_m": m.DepthM} {
		if v != nil && (math.IsNaN(*v) || *v <= 0 || *v > marinaMaxLimitM) {
			return bad("%s %g, which is not a plausible figure", name, *v)
		}
	}
	m.Name = sanitizeMapText(m.Name, marinaTextRunes)
	for _, f := range []*string{&m.Fee, &m.PowerSupply, &m.Phone, &m.Email, &m.VHF, &m.OpeningHours} {
		*f = sanitizeMapText(*f, marinaTextRunes)
	}
	m.Website = sanitizeMapText(m.Website, marinaLongTextRunes)
	m.UnreadTags = cleanList(m.UnreadTags, marinaMaxFacts, &d.Truncated)
	m.TaggedAbsent = cleanList(m.TaggedAbsent, marinaMaxFacts, &d.Truncated)
	m.PontoonArms = cleanList(m.PontoonArms, marinaMaxArms, &d.Truncated)

	for i := range d.Facilities {
		f := &d.Facilities[i]
		if f.Kind = sanitizeMapText(f.Kind, marinaTextRunes); f.Kind == "" || f.Count < 1 {
			return bad("a facility with no kind or no count")
		}
		f.Name = sanitizeMapText(f.Name, marinaTextRunes)
		f.Facts = cleanFacts(f.Facts, &d.Truncated)
		if !validShapes(f.Geometry) {
			return bad("a facility with an invalid shape")
		}
	}
	if len(d.Facilities) > marinaMaxFacilities {
		d.Facilities, d.Truncated = d.Facilities[:marinaMaxFacilities], true
	}
	for i := range d.EntranceMarks {
		k := &d.EntranceMarks[i]
		if k.Kind = sanitizeMapText(k.Kind, marinaTextRunes); k.Kind == "" || k.Count < 1 {
			return bad("an entrance mark with no kind or no count")
		}
		k.Side = sanitizeMapText(k.Side, marinaTextRunes)
		k.Colour = sanitizeMapText(k.Colour, marinaTextRunes)
		k.Name = sanitizeMapText(k.Name, marinaTextRunes)
		if !validShapes(k.Geometry) {
			return bad("an entrance mark with an invalid shape")
		}
	}
	if len(d.EntranceMarks) > marinaMaxMarks {
		d.EntranceMarks, d.Truncated = d.EntranceMarks[:marinaMaxMarks], true
	}
	for i := range d.Ashore {
		a := &d.Ashore[i]
		a.Kind = sanitizeMapText(a.Kind, marinaTextRunes)
		a.Name = sanitizeMapText(a.Name, marinaTextRunes)
		if a.Kind == "" || a.Name == "" || a.DistanceM < 0 {
			return bad("a service ashore with no kind, no name or a negative distance")
		}
		a.OpeningHours = sanitizeMapText(a.OpeningHours, marinaTextRunes)
		if !validLatLon(a.Lat, a.Lon) {
			return bad("a service ashore at an impossible position")
		}
	}
	if len(d.Ashore) > marinaMaxAshore {
		d.Ashore, d.Truncated = d.Ashore[:marinaMaxAshore], true
	}
	if !validShapes(m.Outline) {
		return bad("a marina outline that is not a valid shape")
	}
	if len(m.Outline) > marinaMaxOutlineShapes {
		m.Outline, d.GeometryTruncated = m.Outline[:marinaMaxOutlineShapes], true
	}
	for i := range m.Pontoons {
		p := &m.Pontoons[i]
		if !validShape(p.Shape) {
			return bad("a pontoon that is not a valid shape")
		}
		p.Name = sanitizeMapText(p.Name, marinaTextRunes)
	}
	if len(m.Pontoons) > marinaMaxPontoons {
		m.Pontoons, d.GeometryTruncated = m.Pontoons[:marinaMaxPontoons], true
	}
	return d, nil
}

func cleanFacts(in map[string]string, truncated *bool) map[string]string {
	if len(in) == 0 {
		return nil
	}
	keys := make([]string, 0, len(in))
	for k := range in {
		keys = append(keys, k)
	}
	sort.Strings(keys) // which facts survive the cut must not depend on map order
	out := make(map[string]string, len(in))
	for _, k := range keys {
		k, v := sanitizeMapText(k, marinaTextRunes), sanitizeMapText(in[k], marinaTextRunes)
		if k == "" || v == "" {
			continue
		}
		if len(out) >= marinaMaxFacts {
			*truncated = true
			break
		}
		out[k] = v
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// wasmLatLonInput is the input of an optional export that takes a position.
type wasmLatLonInput struct {
	Lat float64 `json:"lat"`
	Lon float64 `json:"lon"`
}

// MarinaDetails calls the guest's marina_details export. The guest's own error
// text (no marina charted within 500 m, a rate limit, a runtime error from the
// map server) comes back as the error.
func (p *wasmPOIProvider) MarinaDetails(lat, lon float64) (marinaDetails, error) {
	return callOptionalExport(p, "marina_details", p.SupportsMarinaDetails(), errNoMarinaDetailsExport,
		wasmLatLonInput{Lat: lat, Lon: lon}, decodeMarinaDetails)
}
