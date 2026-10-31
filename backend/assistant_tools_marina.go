package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
)

var mateToolMarinaDetails = mateTool{
	name: "marina_details",
	function: openRouterFunctionDef{
		Description: "Evaluate a marina from what OpenStreetMap records about it and the features around it: berth count, fee, power, " +
			"fuel, haul-out (travel lift), boatyard, launching ramp, water, pump-out, toilets and showers, the names of the pontoon arms, the " +
			"entrance marks (lateral beacons and buoys, with side and colour), contact details (phone, email, website, VHF), and shops and " +
			"services ashore within about a kilometre (supermarket, pharmacy, doctor, hospital, ATM, bank, laundry) with opening hours. " +
			"It also compares any charted maximum draught and maximum length with this boat's own draft and length overall, one line per " +
			"particular: ok, exceeds, not charted, or particular not set. Resolve the marina's name with find_places first and pass its " +
			"lat and lon; the position must be within 500 m of a charted marina or harbour, or it returns an error saying none is charted. " +
			"Use it when the operator is choosing or planning a marina stop. " + ruleMarinaDetails + " It needs a place-names plugin that " +
			"supports it; if it returns an error, tell the operator what it says rather than describing the marina from memory.",
		Parameters: json.RawMessage(`{
					"type": "object",
					"properties": {
						"lat": {
							"type": "number",
							"description": "Latitude of the marina in decimal degrees (south negative), from find_places."
						},
						"lon": {
							"type": "number",
							"description": "Longitude of the marina in decimal degrees (west negative), from find_places."
						}
					},
					"required": ["lat", "lon"]
				}`),
	},
	execute: assistantToolDeps.executeMarinaDetails,
	describe: func(args json.RawMessage) string {
		return "Looking up the marina…"
	},
}

type assistantMarinaDetailsArgs struct {
	Lat *float64 `json:"lat"`
	Lon *float64 `json:"lon"`
}

type marinaContact struct {
	Phone   string `json:"phone,omitempty"`
	Email   string `json:"email,omitempty"`
	Website string `json:"website,omitempty"`
	VHF     string `json:"vhf,omitempty"`
}

type marinaChartedLimits struct {
	MaxDraughtM *float64 `json:"max_draught_m"`
	MaxLengthM  *float64 `json:"max_length_m"`
	DepthM      *float64 `json:"depth_m"`
}

type marinaSection struct {
	Name          string              `json:"name"`
	OSM           string              `json:"osm"`
	Lat           float64             `json:"lat"`
	Lon           float64             `json:"lon"`
	Berths        *int                `json:"berths"`
	Fee           string              `json:"fee,omitempty"`
	PowerSupply   string              `json:"power_supply,omitempty"`
	OpeningHours  string              `json:"opening_hours,omitempty"`
	Contact       marinaContact       `json:"contact"`
	ChartedLimits marinaChartedLimits `json:"charted_limits"`
	PontoonArms   []string            `json:"pontoon_arms"`
	TaggedAbsent  []string            `json:"tagged_absent,omitempty"`
	UnreadTags    []string            `json:"unread_tags,omitempty"`
}

// marinaLimitCheck compares one of the boat's particulars with the marina's
// charted limit for it. Status is "ok", "exceeds", "not charted" or
// "particular not set".
type marinaLimitCheck struct {
	label         string   // the particular as the operator says it, for the reading
	Particular    string   `json:"particular"`
	BoatM         *float64 `json:"boat_m"`
	BoatSource    string   `json:"boat_source,omitempty"`
	ChartedLimitM *float64 `json:"charted_limit_m"`
	Status        string   `json:"status"`
	Note          string   `json:"note"`
}

type assistantMarinaDetailsResult struct {
	Marina           marinaSection      `json:"marina"`
	Facilities       []marinaFacility   `json:"facilities"`
	EntranceMarks    []marinaMark       `json:"entrance_marks"`
	Ashore           []marinaAshore     `json:"ashore"`
	VesselLimitCheck []marinaLimitCheck `json:"vessel_limit_check"`
	Source           string             `json:"source"`
	// Card is set only where a card sink takes the marina card (Mate's chat), so
	// an outside assistant is not told to rely on a card it never shows.
	Card string `json:"card,omitempty"`
	// DataAsOf is when the OpenStreetMap data was current, so the answer can
	// say how old the map is. Left out when the map server did not say.
	DataAsOf  string `json:"data_as_of,omitempty"`
	Truncated bool   `json:"truncated,omitempty"`
	Reading   string `json:"reading"`
}

func (d assistantToolDeps) executeMarinaDetails(ctx context.Context, raw json.RawMessage) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	var args assistantMarinaDetailsArgs
	if err := json.Unmarshal(raw, &args); err != nil {
		return "", fmt.Errorf("parse marina_details arguments: %w", err)
	}
	if args.Lat == nil || args.Lon == nil {
		return "", fmt.Errorf("marina_details: lat and lon are required")
	}
	if !hasUsableVesselPosition(*args.Lat, *args.Lon) {
		return "", fmt.Errorf("marina_details: %g, %g is not a usable position", *args.Lat, *args.Lon)
	}

	if d.placeNames == nil {
		return "", fmt.Errorf("marina_details: no place-names plugin is available on this install")
	}
	pn, providerID, err := d.placeNames()
	if err != nil {
		return "", fmt.Errorf("marina_details: %w", err)
	}
	provider, ok := pn.(marinaDetailsProvider)
	if !ok {
		return "", fmt.Errorf("marina_details: plugin %q does not provide marina details", providerID)
	}
	found, err := provider.MarinaDetails(*args.Lat, *args.Lon)
	if errors.Is(err, errNoMarinaDetailsExport) {
		return "", fmt.Errorf("marina_details: plugin %q does not provide marina details", providerID)
	}
	if err != nil {
		return "", fmt.Errorf("marina_details: %w", err)
	}

	m := found.Marina
	result := assistantMarinaDetailsResult{
		Marina: marinaSection{
			Name: m.Name, OSM: m.OSM, Lat: m.Lat, Lon: m.Lon, Berths: m.Berths, Fee: m.Fee, PowerSupply: m.PowerSupply,
			OpeningHours:  m.OpeningHours,
			Contact:       m.marinaContact,
			ChartedLimits: m.marinaChartedLimits,
			PontoonArms:   m.PontoonArms, TaggedAbsent: m.TaggedAbsent, UnreadTags: m.UnreadTags,
		},
		Facilities:    make([]marinaFacility, 0, len(found.Facilities)),
		EntranceMarks: make([]marinaMark, 0, len(found.EntranceMarks)),
		Ashore:        make([]marinaAshore, 0, len(found.Ashore)),
		Source:        "OpenStreetMap via " + providerID,
		DataAsOf:      found.DataAsOf,
		Truncated:     found.Truncated,
	}
	if d.cards != nil {
		result.Card = marinaCardInstruction
	}
	for _, f := range found.Facilities {
		result.Facilities = append(result.Facilities, f.marinaFacility)
	}
	for _, k := range found.EntranceMarks {
		result.EntranceMarks = append(result.EntranceMarks, k.marinaMark)
	}
	for _, a := range found.Ashore {
		result.Ashore = append(result.Ashore, a.marinaAshore)
	}
	boat := d.marinaBoat()
	result.VesselLimitCheck = []marinaLimitCheck{
		checkMarinaLimit("draft", "draught", boat.draftM, boat.draftSource, m.MaxDraughtM, boat.why),
		checkMarinaLimit("length", "length", boat.loaM, boat.loaSource, m.MaxLengthM, boat.why),
	}
	var exceeds []string
	for _, c := range result.VesselLimitCheck {
		if c.Status == "exceeds" {
			exceeds = append(exceeds, c.label)
		}
	}
	result.Reading = readingMarinaDetails(result.Marina.Contact, exceeds, m.UnreadTags, found.Truncated)

	// shrink drops the least useful detail first: ashore, pontoon arms,
	// entrance marks, facility facts, then facilities from the end of the list
	// (ordered by kind importance, fuel first). The marina, the limit check and
	// the reading always survive. The first cut rewrites the reading to say so.
	shrink := func() bool {
		trimmed := true
		switch {
		case len(result.Ashore) > 0:
			result.Ashore = result.Ashore[:len(result.Ashore)-1]
		case len(result.Marina.PontoonArms) > 0:
			result.Marina.PontoonArms = result.Marina.PontoonArms[:len(result.Marina.PontoonArms)-1]
		case len(result.EntranceMarks) > 0:
			result.EntranceMarks = result.EntranceMarks[:len(result.EntranceMarks)-1]
		case hasFacilityFacts(result.Facilities):
			for i := range result.Facilities {
				result.Facilities[i].Facts = nil
			}
		case len(result.Facilities) > 0:
			result.Facilities = result.Facilities[:len(result.Facilities)-1]
		default:
			trimmed = false
		}
		if trimmed && !result.Truncated {
			result.Truncated = true
			result.Reading = readingMarinaDetails(result.Marina.Contact, exceeds, m.UnreadTags, true)
		}
		return trimmed
	}
	// The card is built from what the plugin returned, before shrink cuts the
	// model's copy: the operator sees the full set (ADR 0240).
	var card json.RawMessage
	if d.cards != nil {
		var err error
		if card, err = json.Marshal(buildMarinaCard(found, result.VesselLimitCheck)); err != nil {
			return "", fmt.Errorf("marina_details card: %w", err)
		}
	}
	text, err := capToolResultJSON(&result, shrink)
	if err != nil {
		return "", err
	}
	if d.cards != nil {
		d.cards.put(text, mateCard{Kind: marinaCardKind, Payload: card})
	}
	return text, nil
}

func hasFacilityFacts(list []marinaFacility) bool {
	for _, f := range list {
		if len(f.Facts) > 0 {
			return true
		}
	}
	return false
}

// marinaBoat is what the boat's own figures say, with where each came from. A
// figure that is not known is nil: the check then says it could not be made.
type marinaBoat struct {
	loaM, draftM           *float64
	loaSource, draftSource string
	// why says why the live figures could not be read, when they could not.
	why string
}

// marinaBoat reads the boat's length and draft: the live instrument figure
// first, and for length the stored one when the instruments do not publish it.
// A figure that cannot be read is left unset, with the reason.
func (d assistantToolDeps) marinaBoat() marinaBoat {
	var boat marinaBoat
	switch {
	case d.vesselState == nil:
		boat.why = "Live instrument data is not available to this tool."
	default:
		st, err := d.vesselState()
		if err != nil {
			boat.why = fmt.Sprintf("Live instrument data could not be read (%v).", err)
			break
		}
		if st.DraftM > 0 {
			v := st.DraftM
			boat.draftM, boat.draftSource = &v, vesselSourceLive
		}
		if st.LengthOverallM > 0 {
			v := st.LengthOverallM
			boat.loaM, boat.loaSource = &v, vesselSourceLive
		}
	}
	if boat.loaM == nil && d.documents != nil {
		if store := d.documents(); store != nil {
			if v, err := store.GetVesselParticulars(); err == nil && v.LOAM != nil && *v.LOAM > 0 {
				loa := *v.LOAM
				boat.loaM, boat.loaSource = &loa, vesselSourceStored
			}
		}
	}
	return boat
}

func checkMarinaLimit(particular, label string, boatM *float64, boatSource string, limitM *float64, why string) marinaLimitCheck {
	c := marinaLimitCheck{label: label, Particular: particular, BoatM: boatM, BoatSource: boatSource, ChartedLimitM: limitM}
	switch {
	case limitM == nil:
		c.Status = "not charted"
		c.Note = fmt.Sprintf("No maximum %s is charted for this marina. That does not mean there is none: ask the marina.", label)
	case boatM == nil:
		c.Status = "particular not set"
		c.Note = fmt.Sprintf("The boat's %s is not set, so the charted %.1f m maximum could not be checked.", particular, *limitM)
		if why != "" {
			c.Note += " " + why
		}
		c.Note += " Ask the operator for it; never assume one."
	case *boatM > *limitM:
		c.Status = "exceeds"
		c.Note = fmt.Sprintf("The boat's %s of %.1f m is over the charted maximum of %.1f m.", particular, *boatM, *limitM)
	case *boatM == *limitM:
		// The limit is a maximum, so the boat still fits, with nothing to spare.
		c.Status = "ok"
		c.Note = fmt.Sprintf("The boat's %s of %.1f m is at the charted maximum of %.1f m, with no margin. Charted limits are not tide-referenced and can be out of date: confirm with the marina.", particular, *boatM, *limitM)
	default:
		c.Status = "ok"
		c.Note = fmt.Sprintf("The boat's %s of %.1f m is within the charted maximum of %.1f m. Charted limits can be out of date: confirm with the marina.", particular, *boatM, *limitM)
	}
	return c
}
