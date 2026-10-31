package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// coralSeaWire is what the osm-overpass plugin writes for the Coral Sea Marina
// fixtures (its golden file), decoded the way the host decodes a plugin answer.
func coralSeaWire(t *testing.T) marinaDetails {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join("..", "docs", "examples", "poi-plugins", "osm-overpass", "testdata", "marina_details_coral_sea_output.json"))
	if err != nil {
		t.Fatal(err)
	}
	d, err := decodeMarinaDetails("osm-overpass", raw)
	if err != nil {
		t.Fatalf("the plugin's Coral Sea answer does not decode: %v", err)
	}
	return d
}

func marinaCardDeps(p placeNameProvider, state vesselStateData) assistantToolDeps {
	d := marinaDeps(p, state, nil)
	d.cards = newAssistantCardSink()
	return d
}

func runMarinaForCard(t *testing.T, d assistantToolDeps, args string) (string, []mateCard) {
	t.Helper()
	text, _, err := d.execute(context.Background(), "marina_details", json.RawMessage(args))
	if err != nil {
		t.Fatalf("marina_details: %v", err)
	}
	return text, d.takeCards(text)
}

func decodeMarinaCard(t *testing.T, c mateCard) marinaCardPayload {
	t.Helper()
	dec := json.NewDecoder(bytes.NewReader(c.Payload))
	dec.DisallowUnknownFields()
	var p marinaCardPayload
	if err := dec.Decode(&p); err != nil {
		t.Fatalf("card payload: %v", err)
	}
	return p
}

func TestMarinaDetails_PutsOneMarinaCardForTheCall(t *testing.T) {
	wire := coralSeaWire(t)
	d := marinaCardDeps(&fakeMarinaProvider{details: wire}, vesselStateData{LengthOverallM: 15, DraftM: 1.4})
	_, cards := runMarinaForCard(t, d, `{"lat":-20.2663,"lon":148.7102}`)
	if len(cards) != 1 || cards[0].Kind != "marina" {
		t.Fatalf("cards = %+v", cards)
	}
	p := decodeMarinaCard(t, cards[0])
	if p.Name != "Coral Sea Marina Resort" || p.OSM != "way/245930150" {
		t.Errorf("name/osm = %q %q", p.Name, p.OSM)
	}
	if p.Berths == nil || *p.Berths != 520 || p.Fee != "yes" || p.PowerSupply != "yes" {
		t.Errorf("berths/fee/power = %v %q %q", p.Berths, p.Fee, p.PowerSupply)
	}
	if p.DataAsOf != "2026-10-29T21:20:09Z" {
		t.Errorf("data_as_of = %q", p.DataAsOf)
	}
	if len(p.PontoonArms) == 0 || len(p.Facilities) == 0 || len(p.EntranceMarks) == 0 || len(p.Ashore) == 0 {
		t.Errorf("sections = %d %d %d %d", len(p.PontoonArms), len(p.Facilities), len(p.EntranceMarks), len(p.Ashore))
	}
	if len(p.VesselLimitCheck) != 2 || p.VesselLimitCheck[0].Particular != "draft" || p.VesselLimitCheck[0].BoatM == nil {
		t.Errorf("limit check = %+v", p.VesselLimitCheck)
	}
	if len(p.Geometry.Outline) != 1 || len(p.Geometry.Pontoons) == 0 {
		t.Errorf("geometry = outline %d, pontoons %d", len(p.Geometry.Outline), len(p.Geometry.Pontoons))
	}
	if len(p.Geometry.Ashore) != len(p.Ashore) {
		t.Errorf("ashore points = %d, ashore = %d", len(p.Geometry.Ashore), len(p.Ashore))
	}
}

func TestMarinaDetails_TwoCallsMakeTwoCards(t *testing.T) {
	wire := coralSeaWire(t)
	other := wire
	other.Marina.Name = "Other Marina"
	other.Marina.OSM = "way/9"
	sink := newAssistantCardSink()
	run := func(details marinaDetails) (string, error) {
		d := marinaDeps(&fakeMarinaProvider{details: details}, vesselStateData{LengthOverallM: 15, DraftM: 1.4}, nil)
		d.cards = sink
		text, _, err := d.execute(context.Background(), "marina_details", json.RawMessage(`{"lat":-20.2663,"lon":148.7102}`))
		return text, err
	}
	a, err := run(wire)
	if err != nil {
		t.Fatal(err)
	}
	b, err := run(other)
	if err != nil {
		t.Fatal(err)
	}
	ca, cb := sink.take(a), sink.take(b)
	if len(ca) != 1 || len(cb) != 1 {
		t.Fatalf("cards = %d and %d", len(ca), len(cb))
	}
	if decodeMarinaCard(t, ca[0]).Name != "Coral Sea Marina Resort" || decodeMarinaCard(t, cb[0]).Name != "Other Marina" {
		t.Error("each call's card must hold its own marina")
	}
}

func TestMarinaDetails_AFailedCallPutsNoCard(t *testing.T) {
	d := marinaCardDeps(&fakeMarinaProvider{err: fmt.Errorf("no marina charted within 500 m of that position")}, vesselStateData{})
	if _, _, err := d.execute(context.Background(), "marina_details", json.RawMessage(`{"lat":-20.2663,"lon":148.7102}`)); err == nil {
		t.Fatal("expected an error")
	}
	if got := d.cards.take(""); got != nil {
		t.Errorf("a failed call made a card: %+v", got)
	}
}

func TestMarinaDetails_ShrinkingTheTextLeavesTheCardWhole(t *testing.T) {
	wire := coralSeaWire(t)
	wire.Facilities, wire.EntranceMarks, wire.Ashore = nil, nil, nil
	for i := 0; i < 60; i++ {
		facts := map[string]string{}
		for j := 0; j < 12; j++ {
			facts[fmt.Sprintf("fact_%02d", j)] = strings.Repeat("x", 80)
		}
		wire.Facilities = append(wire.Facilities, marinaFacilityWire{
			marinaFacility: marinaFacility{Kind: "fuel", Name: fmt.Sprintf("Facility %d", i), Count: 1, Facts: facts},
			Geometry:       []marinaShape{{Kind: "point", Points: [][2]float64{{-20.26 - float64(i)/10000, 148.71}}}},
		})
	}
	for i := 0; i < 30; i++ {
		wire.Ashore = append(wire.Ashore, marinaAshoreWire{marinaAshore: marinaAshore{Kind: "supermarket", Name: fmt.Sprintf("Shop %d", i), DistanceM: 100 + i}, Lat: -20.27, Lon: 148.72})
	}
	d := marinaCardDeps(&fakeMarinaProvider{details: wire}, vesselStateData{LengthOverallM: 15, DraftM: 1.4})
	text, cards := runMarinaForCard(t, d, `{"lat":-20.2663,"lon":148.7102}`)
	if !strings.Contains(text, `"truncated":true`) {
		t.Fatalf("the text was not shrunk, so this test proves nothing (%d chars)", len(text))
	}
	if len(cards) != 1 {
		t.Fatalf("cards = %d", len(cards))
	}
	p := decodeMarinaCard(t, cards[0])
	if len(p.Facilities) != 60 || len(p.Ashore) != 30 {
		t.Errorf("card has %d facilities and %d ashore, want the full 60 and 30", len(p.Facilities), len(p.Ashore))
	}
	if len(p.Facilities[0].Facts) != 12 {
		t.Errorf("card facts were cut: %v", p.Facilities[0].Facts)
	}
	if len(p.Geometry.Facilities) != 60 {
		t.Errorf("facility points = %d, want 60", len(p.Geometry.Facilities))
	}
}

func TestMarinaDetails_NoDataDateLeavesItEmpty(t *testing.T) {
	wire := coralSeaWire(t)
	wire.DataAsOf = ""
	d := marinaCardDeps(&fakeMarinaProvider{details: wire}, vesselStateData{LengthOverallM: 15, DraftM: 1.4})
	text, cards := runMarinaForCard(t, d, `{"lat":-20.2663,"lon":148.7102}`)
	if p := decodeMarinaCard(t, cards[0]); p.DataAsOf != "" {
		t.Errorf("card data_as_of = %q, want empty (never the clock)", p.DataAsOf)
	}
	if strings.Contains(text, "data_as_of") {
		t.Errorf("the model result should omit an unknown date: %s", text)
	}
}

func TestMarinaDetails_TheModelIsToldHowCurrentTheMapIs(t *testing.T) {
	d := marinaCardDeps(&fakeMarinaProvider{details: coralSeaWire(t)}, vesselStateData{LengthOverallM: 15, DraftM: 1.4})
	text, _ := runMarinaForCard(t, d, `{"lat":-20.2663,"lon":148.7102}`)
	if !strings.Contains(text, `"data_as_of":"2026-10-29T21:20:09Z"`) {
		t.Errorf("result lacks data_as_of: %s", text)
	}
	for _, leak := range []string{"geometry", "outline", "pontoons", "points"} {
		if strings.Contains(text, leak) {
			t.Errorf("the model result contains %q", leak)
		}
	}
}

func TestMarinaDetails_TheResultMentionsACardOnlyWhereOneIsShown(t *testing.T) {
	args := json.RawMessage(`{"lat":-20.2663,"lon":148.7102}`)
	state := vesselStateData{LengthOverallM: 15, DraftM: 1.4}
	noSink := marinaDeps(&fakeMarinaProvider{details: coralSeaWire(t)}, state, nil)
	text, _, err := noSink.execute(context.Background(), "marina_details", args)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(text, "card") {
		t.Errorf("a path with no card sink mentions a card: %s", text)
	}
	if strings.Contains(mateToolMarinaDetails.function.Description, "card") {
		t.Error("the static description promises a card that not every path shows")
	}
	text, _ = runMarinaForCard(t, marinaCardDeps(&fakeMarinaProvider{details: coralSeaWire(t)}, state), string(args))
	if !strings.Contains(text, `"card":"A marina card`) {
		t.Errorf("a path with a card sink does not say the card is shown: %s", text)
	}
}

func TestDecodeMarinaDetails_DataDate(t *testing.T) {
	ok := strings.Replace(marinaGeometryJSON, `{"marina":{`, `{"data_as_of":"2026-10-29T21:20:09Z","marina":{`, 1)
	d, err := decodeMarinaDetails("p", []byte(ok))
	if err != nil || d.DataAsOf != "2026-10-29T21:20:09Z" {
		t.Fatalf("DataAsOf = %q, err = %v", d.DataAsOf, err)
	}
	bad := strings.Replace(marinaGeometryJSON, `{"marina":{`, `{"data_as_of":"last tuesday","marina":{`, 1)
	if _, err := decodeMarinaDetails("p", []byte(bad)); err == nil {
		t.Fatal("a data date that is not a timestamp was accepted")
	}
	if d, err := decodeMarinaDetails("p", []byte(marinaGeometryJSON)); err != nil || d.DataAsOf != "" {
		t.Fatalf("a missing date must stay empty: %q, %v", d.DataAsOf, err)
	}
}

func TestMarinaCard_FacilityPointsAreCentresOfTheirShapes(t *testing.T) {
	wire := marinaDetails{
		Marina: marinaRecordWire{OSM: "way/1", Name: "M", Lat: 1, Lon: 1},
		Facilities: []marinaFacilityWire{
			{marinaFacility: marinaFacility{Kind: "fuel", Count: 2}, Geometry: []marinaShape{
				{Kind: "point", Points: [][2]float64{{1.5, 1.5}}},
				{Kind: "polygon", Points: [][2]float64{{10, 10}, {10, 12}, {12, 12}, {12, 10}, {10, 10}}},
			}},
			// A facility the marina declares about itself has no geometry: a row, no dot.
			{marinaFacility: marinaFacility{Kind: "power", Count: 1}, Geometry: []marinaShape{}},
		},
	}
	card := buildMarinaCard(wire, nil, nil)
	if len(card.Facilities) != 2 {
		t.Errorf("facility rows = %d, want 2", len(card.Facilities))
	}
	if got := card.Geometry.Facilities; len(got) != 2 {
		t.Fatalf("facility points = %+v", got)
	} else if got[0].Lat != 1.5 || got[1].Lat != 11 || got[1].Lon != 11 || got[1].Kind != "fuel" {
		t.Errorf("points = %+v", got)
	}
}

func TestMarinaCardGolden(t *testing.T) {
	d := marinaCardDeps(&fakeMarinaProvider{details: coralSeaWire(t)}, vesselStateData{LengthOverallM: 15, DraftM: 1.4})
	_, cards := runMarinaForCard(t, d, `{"lat":-20.2663,"lon":148.7102}`)
	if len(cards) != 1 {
		t.Fatalf("cards = %d", len(cards))
	}
	envelope, err := json.MarshalIndent(map[string]any{"kind": cards[0].Kind, "payload": cards[0].Payload}, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	envelope = append(envelope, '\n')
	path := filepath.Join("testdata", "mate_cards", "marina_coral_sea.json")
	if *updateMateFixtures {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, envelope, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(envelope, want) {
		t.Errorf("marina card drifted from %s; run go test -run MarinaCardGolden -update if intended", path)
	}
}

func TestDecodeMarinaDetails_BoundsTheGeometry(t *testing.T) {
	raw := func(mutate func(*marinaDetails)) marinaDetails {
		d := coralSeaWire(t)
		mutate(&d)
		out, err := json.Marshal(d)
		if err != nil {
			t.Fatal(err)
		}
		got, err := decodeMarinaDetails("p", out)
		if err != nil {
			t.Fatal(err)
		}
		return got
	}
	pontoon := marinaPontoon{Name: "A", Shape: marinaShape{Kind: "line", Points: [][2]float64{{-20.26, 148.71}, {-20.27, 148.72}}}}
	got := raw(func(d *marinaDetails) {
		d.Truncated = false
		d.Marina.Pontoons = nil
		for i := 0; i < marinaMaxPontoons+5; i++ {
			d.Marina.Pontoons = append(d.Marina.Pontoons, pontoon)
		}
	})
	if len(got.Marina.Pontoons) != marinaMaxPontoons || !got.GeometryTruncated || got.Truncated {
		t.Errorf("pontoons = %d, geometry truncated = %v, truncated = %v", len(got.Marina.Pontoons), got.GeometryTruncated, got.Truncated)
	}
	shape := marinaShape{Kind: "polygon", Points: [][2]float64{{0, 0}, {0, 1}, {1, 1}, {0, 0}}}
	got = raw(func(d *marinaDetails) {
		d.Truncated = false
		d.Marina.Outline = nil
		for i := 0; i < marinaMaxOutlineShapes+5; i++ {
			d.Marina.Outline = append(d.Marina.Outline, shape)
		}
	})
	if len(got.Marina.Outline) != marinaMaxOutlineShapes || !got.GeometryTruncated || got.Truncated {
		t.Errorf("outline = %d, geometry truncated = %v, truncated = %v", len(got.Marina.Outline), got.GeometryTruncated, got.Truncated)
	}
}

// Cutting the map's shapes is not cutting a list the model reads: the model is
// not told lists were cut, and the card says only that its map is simplified.
func TestMarinaDetails_GeometryCutIsNotAListCut(t *testing.T) {
	wire := coralSeaWire(t)
	wire.Truncated = false
	wire.GeometryTruncated = true
	d := marinaCardDeps(&fakeMarinaProvider{details: wire}, vesselStateData{LengthOverallM: 15, DraftM: 1.4})
	text, cards := runMarinaForCard(t, d, `{"lat":-20.2663,"lon":148.7102}`)
	if strings.Contains(text, `"truncated"`) || strings.Contains(strings.ToLower(text), "cut") {
		t.Errorf("the model was told lists were cut: %s", text)
	}
	p := decodeMarinaCard(t, cards[0])
	if !p.GeometryTruncated || p.Truncated {
		t.Errorf("card geometry_truncated = %v, truncated = %v", p.GeometryTruncated, p.Truncated)
	}
}

func TestMarinaCard_CarriesNoSource(t *testing.T) {
	_, cards := runMarinaForCard(t, marinaCardDeps(&fakeMarinaProvider{details: coralSeaWire(t)}, vesselStateData{}), `{"lat":-20.2663,"lon":148.7102}`)
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(cards[0].Payload, &fields); err != nil {
		t.Fatal(err)
	}
	if _, ok := fields["source"]; ok {
		t.Error("the card still carries source; the footer names OpenStreetMap itself")
	}
}

// A line's place on the map is the point half-way along it, not a vertex: for
// a two-node way a vertex is an end.
func TestShapeCentre_LineIsHalfwayAlongIt(t *testing.T) {
	lat, lon := shapeCentre(marinaShape{Kind: "line", Points: [][2]float64{{0, 0}, {0, 10}}})
	if lat != 0 || math.Abs(lon-5) > 1e-9 {
		t.Errorf("two-point line centre = %v, %v, want 0, 5", lat, lon)
	}
	// Three vertices, the second leg three times the first: half the length
	// falls on the second leg, a third of the way along it.
	lat, lon = shapeCentre(marinaShape{Kind: "line", Points: [][2]float64{{0, 0}, {0, 1}, {0, 4}}})
	if lat != 0 || math.Abs(lon-2) > 1e-9 {
		t.Errorf("uneven line centre = %v, %v, want 0, 2", lat, lon)
	}
}

func TestDecodeMarinaDetails_RejectsAMalformedShape(t *testing.T) {
	line := func(n int) string {
		pts := make([]string, n)
		for i := range pts {
			pts[i] = fmt.Sprintf("[-20,%g]", 148+float64(i)/10000)
		}
		return strings.Join(pts, ",")
	}
	const closed = `{"kind":"polygon","points":[[-20,148],[-20,148.1],[-20.1,148.1],[-20,148]]}`
	cases := map[string]string{
		"unclosed polygon": `{"kind":"polygon","points":[[-20,148],[-20,148.1],[-20.1,148.1],[-20.1,148]]}`,
		"too many points":  fmt.Sprintf(`{"kind":"line","points":[%s]}`, line(marinaMaxShapePoints+1)),
	}
	for name, shape := range cases {
		raw := strings.Replace(marinaGeometryJSON, closed, shape, 1)
		if _, err := decodeMarinaDetails("p", []byte(raw)); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
	atCap := strings.Replace(marinaGeometryJSON, closed, fmt.Sprintf(`{"kind":"line","points":[%s]}`, line(marinaMaxShapePoints)), 1)
	if _, err := decodeMarinaDetails("p", []byte(atCap)); err != nil {
		t.Errorf("a shape at the cap was rejected: %v", err)
	}
}
