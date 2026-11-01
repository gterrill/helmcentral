package main

import "time"

// The anchorage card (ADR 0244): what anchorage_details found, drawn for the
// operator. It is a snapshot like the marina card: it stores the figures and
// the geometry the tool read, and a reopened thread draws the same card
// without asking the weather, tide or map servers again. The model reads the
// compact text result; the card carries the full set, including the shapes
// the model never sees.

const anchorageCardKind = "anchorage"

// anchorageCardInstruction is what the model is told when the operator will
// see the card, so it does not read the same rows back as a table.
const anchorageCardInstruction = "An anchorage card with the shelter rose, the forecast and tide for the stay, the local features and a map is shown under your reply, so do not repeat the forecast rows, the tide table or the feature lists back; say what they mean for the stay."

type anchorageCardBearing struct {
	BearingDeg int     `json:"bearing_deg"`
	FetchNm    float64 `json:"fetch_nm"`
	Grade      string  `json:"grade"`
	Open       bool    `json:"open"`
}

type anchorageCardArc struct {
	Grade   string `json:"grade"`
	FromDeg int    `json:"from_deg"`
	ToDeg   int    `json:"to_deg"`
	Text    string `json:"text"`
}

type anchorageCardShelter struct {
	Summary             string                 `json:"summary"`
	InsideReef          bool                   `json:"inside_reef"`
	ReefName            string                 `json:"reef_name"`
	CoastlineDownloaded string                 `json:"coastline_downloaded"`
	Bearings            []anchorageCardBearing `json:"bearings"`
	Arcs                []anchorageCardArc     `json:"arcs"`
}

type anchorageCardRow struct {
	Time    string   `json:"time"`
	WindKts *int     `json:"wind_kts"`
	GustKts *int     `json:"gust_kts"`
	DirDeg  *int     `json:"dir_deg"`
	Dir     string   `json:"dir"`
	Shelter string   `json:"shelter"`
	WaveM   *float64 `json:"wave_m"`
	WaveS   *float64 `json:"wave_s"`
	WaveDir string   `json:"wave_dir"`
}

type anchorageCardDay struct {
	Date              string `json:"date"`
	WindKtsMin        *int   `json:"wind_kts_min"`
	WindKtsMax        *int   `json:"wind_kts_max"`
	GustKtsMax        *int   `json:"gust_kts_max"`
	DirPrevailing     string `json:"dir_prevailing"`
	ShelterPrevailing string `json:"shelter_prevailing"`
}

// anchorageCardForecast holds the stay's forecast, or the error that stood in
// its place. Rows and days are [] in the error case.
type anchorageCardForecast struct {
	Error       string             `json:"error"`
	Note        string             `json:"note"`
	FetchedAt   string             `json:"fetched_at"`
	WavesError  string             `json:"waves_error"`
	WavesNoData bool               `json:"waves_no_data"`
	Rows        []anchorageCardRow `json:"rows"`
	Days        []anchorageCardDay `json:"days"`
}

type anchorageCardExtreme struct {
	Time     string  `json:"time"`
	Type     string  `json:"type"`
	HeightM  float64 `json:"height_m"`
	InWindow bool    `json:"in_window"`
}

type anchorageCardTide struct {
	Error          string                 `json:"error"`
	Station        string                 `json:"station"`
	DistanceNm     float64                `json:"distance_nm"`
	TimeBasis      string                 `json:"time_basis"`
	HeightAtArrive *float64               `json:"height_at_arrival_m"`
	Direction      string                 `json:"direction"`
	HeightNote     string                 `json:"height_note"`
	Note           string                 `json:"note"`
	Extremes       []anchorageCardExtreme `json:"extremes"`
	RangeM         *float64               `json:"range_m"`
	RangeNote      string                 `json:"range_note"`
}

// The feature records embed the model-facing records, as the marina card does,
// and add what only the map and the card need: the shapes to draw, one per
// mapped way, and a mooring's position. They are empty when the features
// section is an error.
type anchorageCardRestricted struct {
	anchorageRestrictedArea
	ProhibitsAnchoring bool          `json:"prohibits_anchoring"`
	Shapes             []marinaShape `json:"shapes"`
}

type anchorageCardArea struct {
	anchorageAreaRecord
	Shapes []marinaShape `json:"shapes"`
}

type anchorageCardSeabed struct {
	anchorageSeabedRecord
	Shapes []marinaShape `json:"shapes"`
}

type anchorageCardFeatures struct {
	Error           string                    `json:"error"`
	RestrictedAreas []anchorageCardRestricted `json:"restricted_areas"`
	Moorings        []anchorageMooringWire    `json:"moorings"`
	AnchorageAreas  []anchorageCardArea       `json:"anchorage_areas"`
	SeabedAreas     []anchorageCardSeabed     `json:"seabed_areas"`
	Bay             *anchorageBayRecord       `json:"bay"`
	Truncated       bool                      `json:"truncated"`
}

type anchorageCardPayload struct {
	Name     string                `json:"name"`
	Lat      float64               `json:"lat"`
	Lon      float64               `json:"lon"`
	Arrive   string                `json:"arrive"`
	Leave    string                `json:"leave"`
	Timezone string                `json:"timezone"`
	Shelter  anchorageCardShelter  `json:"shelter"`
	Forecast anchorageCardForecast `json:"forecast"`
	Tide     anchorageCardTide     `json:"tide"`
	Features anchorageCardFeatures `json:"features"`
	// DataAsOf is the OpenStreetMap data time the plugin reported, "" when it
	// reported none. It is never filled from the clock.
	DataAsOf string `json:"data_as_of"`
}

func strOrEmpty(p *string) string {
	if p == nil {
		return ""
	}
	return *p
}

// buildAnchorageCard keeps everything the sections returned, un-shrunk. A
// section that failed carries its error and empty lists, never a guess.
func buildAnchorageCard(p anchorageParts) anchorageCardPayload {
	loc := p.stay.zone.Loc
	card := anchorageCardPayload{
		Name: p.name, Lat: p.shelter.Lat, Lon: p.shelter.Lon,
		Arrive:   p.stay.arrive.In(loc).Format(time.RFC3339),
		Leave:    p.stay.leave.In(loc).Format(time.RFC3339),
		Timezone: p.stay.zone.Name,
		Shelter: anchorageCardShelter{
			Summary: p.shelter.Summary, InsideReef: p.shelter.InsideReef, ReefName: p.shelter.ReefName,
			CoastlineDownloaded: p.shelter.CoastlineDownloaded,
			Bearings:            make([]anchorageCardBearing, 0, len(p.shelter.Bearings)),
			Arcs:                make([]anchorageCardArc, 0, len(p.shelter.Arcs)),
		},
		Forecast: anchorageCardForecast{Rows: []anchorageCardRow{}, Days: []anchorageCardDay{}},
		Tide:     anchorageCardTide{Extremes: []anchorageCardExtreme{}},
		Features: anchorageCardFeatures{
			RestrictedAreas: []anchorageCardRestricted{}, Moorings: []anchorageMooringWire{},
			AnchorageAreas: []anchorageCardArea{}, SeabedAreas: []anchorageCardSeabed{},
		},
	}
	for _, b := range p.shelter.Bearings {
		card.Shelter.Bearings = append(card.Shelter.Bearings, anchorageCardBearing{BearingDeg: b.BearingDeg, FetchNm: b.FetchNm, Grade: b.Grade, Open: b.Open})
	}
	for _, a := range p.shelter.Arcs {
		card.Shelter.Arcs = append(card.Shelter.Arcs, anchorageCardArc{Grade: a.Grade, FromDeg: a.FromDeg, ToDeg: a.ToDeg, Text: a.Text})
	}

	if f := p.forecast.section; f.Error != "" {
		card.Forecast.Error = f.Error
	} else {
		card.Forecast.Note = f.Note
		card.Forecast.WavesError = f.WavesError
		card.Forecast.WavesNoData = f.WavesNoData
		if !p.forecast.fetchedAt.IsZero() {
			card.Forecast.FetchedAt = p.forecast.fetchedAt.UTC().Format(time.RFC3339)
		}
		for i, r := range f.Hourly {
			card.Forecast.Rows = append(card.Forecast.Rows, anchorageCardRow{
				Time: p.forecast.at[i].In(loc).Format(time.RFC3339), WindKts: r.WindKts, GustKts: r.GustKts, DirDeg: r.DirDeg,
				Dir: strOrEmpty(r.Dir), Shelter: r.Shelter, WaveM: r.WaveM, WaveS: r.WaveS, WaveDir: strOrEmpty(r.WaveDir),
			})
		}
		for _, d := range f.Days {
			card.Forecast.Days = append(card.Forecast.Days, anchorageCardDay{
				Date: d.Date, WindKtsMin: d.WindKtsMin, WindKtsMax: d.WindKtsMax, GustKtsMax: d.GustKtsMax,
				DirPrevailing: strOrEmpty(d.DirPrevailing), ShelterPrevailing: strOrEmpty(d.ShelterPrevailing),
			})
		}
	}

	if t := p.tide; t.Error != "" {
		card.Tide.Error = t.Error
	} else {
		card.Tide.Station, card.Tide.DistanceNm, card.Tide.TimeBasis = t.Station.Name, t.Station.DistanceNm, t.TimeBasis
		if t.AtArrival != nil {
			h := t.AtArrival.HeightM
			card.Tide.HeightAtArrive, card.Tide.Direction = &h, t.AtArrival.Direction
		}
		card.Tide.HeightNote, card.Tide.Note, card.Tide.RangeM, card.Tide.RangeNote = t.HeightNote, t.Note, t.RangeM, t.RangeNote
		for _, e := range t.Extremes {
			card.Tide.Extremes = append(card.Tide.Extremes, anchorageCardExtreme{Time: e.ISO, Type: e.Type, HeightM: e.HeightM, InWindow: e.InWindow})
		}
	}

	if f := p.features.section; f.Error != "" {
		card.Features.Error = f.Error
	} else {
		found := p.features.found
		card.Features.Bay, card.Features.Truncated, card.DataAsOf = found.Bay, found.Truncated, found.DataAsOf
		for _, r := range found.RestrictedAreas {
			card.Features.RestrictedAreas = append(card.Features.RestrictedAreas, anchorageCardRestricted{
				anchorageRestrictedArea: r.anchorageRestrictedArea,
				ProhibitsAnchoring:      prohibitsAnchoring(r.anchorageRestrictedArea),
				Shapes:                  shapesOrEmpty(r.Geometry),
			})
		}
		card.Features.Moorings = append(card.Features.Moorings, found.Moorings...)
		for _, a := range found.AnchorageAreas {
			card.Features.AnchorageAreas = append(card.Features.AnchorageAreas, anchorageCardArea{anchorageAreaRecord: a.anchorageAreaRecord, Shapes: shapesOrEmpty(a.Geometry)})
		}
		for _, s := range found.SeabedAreas {
			card.Features.SeabedAreas = append(card.Features.SeabedAreas, anchorageCardSeabed{anchorageSeabedRecord: s.anchorageSeabedRecord, Shapes: shapesOrEmpty(s.Geometry)})
		}
	}
	return card
}

// shapesOrEmpty is [] for an area the plugin sent no geometry for, so the card
// carries every key and the area still lists; it just draws nothing.
func shapesOrEmpty(s []marinaShape) []marinaShape {
	if s == nil {
		return []marinaShape{}
	}
	return s
}
