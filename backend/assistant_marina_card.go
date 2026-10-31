package main

import "math"

// The marina card (ADR 0240): what marina_details found, drawn for the
// operator. It is a snapshot like the passage window: it stores the figures and
// the geometry the tool read, and a reopened thread draws the same card without
// asking the map server again. The model reads the compact text result; the
// card carries the full set, including the shapes the model never sees.

const marinaCardKind = "marina"

// marinaCardInstruction is what the model is told when the operator will see
// the card, so it does not read the same facilities back as a table.
const marinaCardInstruction = "A marina card with a map of the berths and facilities is shown under your reply, so do not list the facilities or arms back as a table."

// The card's facility, mark and ashore rows have the fields of the ones Mate
// reads, in the same order, so a conversion copies them. Only their JSON
// differs: the card keeps empty fields, where Mate's copy leaves them out.
type marinaCardFacility struct {
	Kind  string            `json:"kind"`
	Name  string            `json:"name"`
	Count int               `json:"count"`
	Facts map[string]string `json:"facts"`
}

type marinaCardMark struct {
	Kind   string `json:"kind"`
	Side   string `json:"side"`
	Colour string `json:"colour"`
	Name   string `json:"name"`
	Count  int    `json:"count"`
}

type marinaCardAshore struct {
	Kind         string `json:"kind"`
	Name         string `json:"name"`
	OpeningHours string `json:"opening_hours"`
	DistanceM    int    `json:"distance_m"`
}

type marinaCardLimit struct {
	Particular    string   `json:"particular"`
	Status        string   `json:"status"`
	BoatM         *float64 `json:"boat_m"`
	ChartedLimitM *float64 `json:"charted_limit_m"`
}

type marinaCardContact struct {
	Website string `json:"website"`
	Phone   string `json:"phone"`
	VHF     string `json:"vhf"`
	Email   string `json:"email"`
}

// marinaCardPoint is a facility or a service ashore as a place on the map.
type marinaCardPoint struct {
	Kind string  `json:"kind"`
	Name string  `json:"name"`
	Lat  float64 `json:"lat"`
	Lon  float64 `json:"lon"`
}

type marinaCardGeometry struct {
	Outline    []marinaShape     `json:"outline"`
	Pontoons   []marinaPontoon   `json:"pontoons"`
	Facilities []marinaCardPoint `json:"facilities"`
	Ashore     []marinaCardPoint `json:"ashore"`
}

// marinaCardSavedDocument is a document of the operator's tagged with the
// marina's name, which the card links to.
type marinaCardSavedDocument struct {
	ID    string `json:"id"`
	Title string `json:"title"`
}

type marinaCardPayload struct {
	Name             string               `json:"name"`
	OSM              string               `json:"osm"`
	Lat              float64              `json:"lat"`
	Lon              float64              `json:"lon"`
	Berths           *int                 `json:"berths"`
	Fee              string               `json:"fee"`
	PowerSupply      string               `json:"power_supply"`
	OpeningHours     string               `json:"opening_hours"`
	Contact          marinaCardContact    `json:"contact"`
	ChartedLimits    marinaChartedLimits  `json:"charted_limits"`
	PontoonArms      []string             `json:"pontoon_arms"`
	Facilities       []marinaCardFacility `json:"facilities"`
	EntranceMarks    []marinaCardMark     `json:"entrance_marks"`
	Ashore           []marinaCardAshore   `json:"ashore"`
	VesselLimitCheck []marinaCardLimit    `json:"vessel_limit_check"`
	// DataAsOf is the OpenStreetMap data time the plugin reported, "" when it
	// reported none. It is never filled from the clock.
	DataAsOf string `json:"data_as_of"`
	// Truncated says the plugin cut a list to its bound, so the card may not
	// show everything mapped.
	Truncated bool `json:"truncated"`
	// GeometryTruncated says the map's shapes were simplified or cut.
	GeometryTruncated bool               `json:"geometry_truncated"`
	Geometry          marinaCardGeometry `json:"geometry"`
	// SavedDocuments are the operator's documents linked to this marina by tag.
	SavedDocuments []marinaCardSavedDocument `json:"saved_documents"`
}

// shapeCentre is the place a shape stands for on the map: a point as it is,
// a line by the point half-way along its length (a vertex would be an end of a
// two-node way), a polygon by the mean of its corners.
func shapeCentre(sh marinaShape) (lat, lon float64) {
	switch sh.Kind {
	case "line":
		return lineMidpoint(sh.Points)
	case "polygon":
		corners := sh.Points[:len(sh.Points)-1] // the last repeats the first
		for _, p := range corners {
			lat, lon = lat+p[0], lon+p[1]
		}
		n := float64(len(corners))
		return lat / n, lon / n
	default:
		return sh.Points[0][0], sh.Points[0][1]
	}
}

// lineMidpoint is the point at half the line's length. Lengths are measured on
// a plane with longitude scaled by the cosine of the line's mean latitude,
// which is exact enough at the size of a marina.
func lineMidpoint(pts [][2]float64) (lat, lon float64) {
	var meanLat float64
	for _, p := range pts {
		meanLat += p[0]
	}
	k := math.Cos(meanLat / float64(len(pts)) * math.Pi / 180)
	seg := func(a, b [2]float64) float64 { return math.Hypot(b[0]-a[0], (b[1]-a[1])*k) }
	var total float64
	for i := 1; i < len(pts); i++ {
		total += seg(pts[i-1], pts[i])
	}
	if total == 0 {
		return pts[0][0], pts[0][1]
	}
	half := total / 2
	for i := 1; i < len(pts); i++ {
		l := seg(pts[i-1], pts[i])
		if l >= half {
			t := (half) / l
			return pts[i-1][0] + t*(pts[i][0]-pts[i-1][0]), pts[i-1][1] + t*(pts[i][1]-pts[i-1][1])
		}
		half -= l
	}
	last := pts[len(pts)-1]
	return last[0], last[1]
}

// buildMarinaCard keeps everything the plugin returned, un-shrunk. A facility
// the marina declares about itself (its own fuel tags, say) arrives with no
// geometry: it gets no dot, but still a row.
func buildMarinaCard(found marinaDetails, checks []marinaLimitCheck, saved []marinaSavedDocument) marinaCardPayload {
	m := found.Marina
	card := marinaCardPayload{
		SavedDocuments: make([]marinaCardSavedDocument, 0, len(saved)),
		Name:           m.Name, OSM: m.OSM, Lat: m.Lat, Lon: m.Lon, Berths: m.Berths,
		Fee: m.Fee, PowerSupply: m.PowerSupply, OpeningHours: m.OpeningHours,
		Contact:           marinaCardContact{Website: m.Website, Phone: m.Phone, VHF: m.VHF, Email: m.Email},
		ChartedLimits:     m.marinaChartedLimits,
		PontoonArms:       append([]string{}, m.PontoonArms...),
		Facilities:        make([]marinaCardFacility, 0, len(found.Facilities)),
		EntranceMarks:     make([]marinaCardMark, 0, len(found.EntranceMarks)),
		Ashore:            make([]marinaCardAshore, 0, len(found.Ashore)),
		DataAsOf:          found.DataAsOf,
		Truncated:         found.Truncated,
		GeometryTruncated: found.GeometryTruncated,
		Geometry: marinaCardGeometry{
			Outline:    append([]marinaShape{}, m.Outline...),
			Pontoons:   append([]marinaPontoon{}, m.Pontoons...),
			Facilities: []marinaCardPoint{},
			Ashore:     make([]marinaCardPoint, 0, len(found.Ashore)),
		},
	}
	for _, f := range found.Facilities {
		row := marinaCardFacility(f.marinaFacility)
		if row.Facts == nil {
			row.Facts = map[string]string{}
		}
		card.Facilities = append(card.Facilities, row)
		for _, sh := range f.Geometry {
			lat, lon := shapeCentre(sh)
			card.Geometry.Facilities = append(card.Geometry.Facilities, marinaCardPoint{Kind: f.Kind, Name: f.Name, Lat: lat, Lon: lon})
		}
	}
	for _, k := range found.EntranceMarks {
		card.EntranceMarks = append(card.EntranceMarks, marinaCardMark(k.marinaMark))
	}
	for _, a := range found.Ashore {
		card.Ashore = append(card.Ashore, marinaCardAshore(a.marinaAshore))
		card.Geometry.Ashore = append(card.Geometry.Ashore, marinaCardPoint{Kind: a.Kind, Name: a.Name, Lat: a.Lat, Lon: a.Lon})
	}
	for _, s := range saved {
		card.SavedDocuments = append(card.SavedDocuments, marinaCardSavedDocument{ID: s.DocumentID, Title: s.Title})
	}
	for _, c := range checks {
		card.VesselLimitCheck = append(card.VesselLimitCheck, marinaCardLimit{Particular: c.Particular, Status: c.Status, BoatM: c.BoatM, ChartedLimitM: c.ChartedLimitM})
	}
	if card.VesselLimitCheck == nil {
		card.VesselLimitCheck = []marinaCardLimit{}
	}
	return card
}
