package main

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/labstack/echo/v4"
)

// A plan is an ordered list of stops with dates, replacing the one planned
// stay. These tests book plans around late October 2026 on the Queensland
// coast (UTC+10), with the clock pinned to 24 Oct.
func init() {
	planNow = func() time.Time { return time.Date(2026, 10, 24, 0, 0, 0, 0, time.UTC) }
}

func stopsOf(arrive string, nights ...int) []planStop {
	out := make([]planStop, len(nights))
	names := []string{"Great Sandy Straits Marina", "Pelican Bay", "Mooloolaba", "Manly", "The Boat Works"}
	for i, n := range nights {
		out[i] = planStop{Name: names[i], Lat: -25.5 - float64(i)*0.5, Lon: 152.9 + float64(i)*0.1, Arrive: arrive, Nights: n}
	}
	return out
}

func arrivals(stops []planStop) []string {
	out := make([]string, len(stops))
	for i, s := range stops {
		out[i] = s.Arrive
	}
	return out
}

func sameStrings(a, b []string) bool { return strings.Join(a, ",") == strings.Join(b, ",") }

func validPlan() plan {
	return plan{Name: "Home run", Stops: stopsOf("2026-11-14", 2, 1, 1, 1, 0)}
}

func TestNormalisePlan_ChainsEveryArrivalFromTheFirst(t *testing.T) {
	p := validPlan()
	// Whatever later arrivals the client sent, each is the previous departure.
	p.Stops[1].Arrive, p.Stops[3].Arrive = "1999-01-01", "garbage"
	got, verr := normalisePlan(p)
	if verr != nil {
		t.Fatalf("normalisePlan: %v", verr)
	}
	want := []string{"2026-11-14", "2026-11-16", "2026-11-17", "2026-11-18", "2026-11-19"}
	if !sameStrings(arrivals(got.Stops), want) {
		t.Fatalf("arrivals = %v, want %v", arrivals(got.Stops), want)
	}
}

func TestApplyStopEdit_NightsShiftEveryLaterDate(t *testing.T) {
	p, _ := normalisePlan(validPlan())
	n := 3
	got, verr := applyStopEdit(p.Stops, 1, nil, &n) // Pelican Bay 1 -> 3 nights
	if verr != nil {
		t.Fatal(verr)
	}
	want := []string{"2026-11-14", "2026-11-16", "2026-11-19", "2026-11-20", "2026-11-21"}
	if !sameStrings(arrivals(got), want) {
		t.Fatalf("arrivals = %v, want %v", arrivals(got), want)
	}
	if got[1].Nights != 3 {
		t.Fatalf("nights = %d", got[1].Nights)
	}
	// The input is not modified.
	if p.Stops[1].Nights != 1 {
		t.Fatal("applyStopEdit must not change its input")
	}
}

func TestApplyStopEdit_FewerNightsPullLaterDatesBack(t *testing.T) {
	p, _ := normalisePlan(validPlan())
	n := 0
	got, verr := applyStopEdit(p.Stops, 0, nil, &n)
	if verr != nil {
		t.Fatal(verr)
	}
	want := []string{"2026-11-14", "2026-11-14", "2026-11-15", "2026-11-16", "2026-11-17"}
	if !sameStrings(arrivals(got), want) {
		t.Fatalf("arrivals = %v, want %v", arrivals(got), want)
	}
}

func TestApplyStopEdit_FirstArrivalMovesTheWholePlan(t *testing.T) {
	p, _ := normalisePlan(validPlan())
	a := "2026-11-17"
	got, verr := applyStopEdit(p.Stops, 0, &a, nil)
	if verr != nil {
		t.Fatal(verr)
	}
	want := []string{"2026-11-17", "2026-11-19", "2026-11-20", "2026-11-21", "2026-11-22"}
	if !sameStrings(arrivals(got), want) {
		t.Fatalf("arrivals = %v, want %v", arrivals(got), want)
	}
}

func TestApplyStopEdit_LaterArrivalIsTheNightsOfTheStopBefore(t *testing.T) {
	p, _ := normalisePlan(validPlan())
	a := "2026-11-18" // Pelican Bay was 16 Nov
	got, verr := applyStopEdit(p.Stops, 1, &a, nil)
	if verr != nil {
		t.Fatal(verr)
	}
	if got[0].Nights != 4 {
		t.Fatalf("the stop before takes the extra nights, got %d", got[0].Nights)
	}
	want := []string{"2026-11-14", "2026-11-18", "2026-11-19", "2026-11-20", "2026-11-21"}
	if !sameStrings(arrivals(got), want) {
		t.Fatalf("arrivals = %v, want %v", arrivals(got), want)
	}
	// Arriving before the boat has even reached the stop before is refused.
	early := "2026-11-13"
	if _, verr := applyStopEdit(p.Stops, 1, &early, nil); verr == nil || verr.Field != "arrive" {
		t.Fatalf("an arrival before the stop before must be refused on arrive, got %v", verr)
	}
}

func TestApplyStopEdit_Refusals(t *testing.T) {
	p, _ := normalisePlan(validPlan())
	neg := -1
	if _, verr := applyStopEdit(p.Stops, 1, nil, &neg); verr == nil || verr.Field != "nights" {
		t.Fatalf("negative nights: %v", verr)
	}
	big := 400
	if _, verr := applyStopEdit(p.Stops, 1, nil, &big); verr == nil || verr.Field != "nights" {
		t.Fatalf("absurd nights: %v", verr)
	}
	bad := "17 Nov"
	if _, verr := applyStopEdit(p.Stops, 0, &bad, nil); verr == nil || verr.Field != "arrive" {
		t.Fatalf("bad date: %v", verr)
	}
	if _, verr := applyStopEdit(p.Stops, 9, nil, &big); verr == nil {
		t.Fatal("a stop that is not there")
	}
}

func TestNormalisePlan_Validation(t *testing.T) {
	mut := func(f func(*plan)) plan { p := validPlan(); f(&p); return p }
	cases := map[string]plan{
		"name":         mut(func(p *plan) { p.Name = "  " }),
		"stops":        mut(func(p *plan) { p.Stops = nil }),
		"stops.lat":    mut(func(p *plan) { p.Stops[1].Lat = 95 }),
		"stops.lon":    mut(func(p *plan) { p.Stops[1].Lon = -181 }),
		"stops.name":   mut(func(p *plan) { p.Stops[2].Name = "" }),
		"stops.arrive": mut(func(p *plan) { p.Stops[0].Arrive = "14 Nov" }),
		"stops.nights": mut(func(p *plan) { p.Stops[0].Nights = -1 }),
		"vias":         mut(func(p *plan) { p.Stops[0].Vias = []planVia{{Name: "x", Lat: 99, Lon: 1}} }),
		"depart_time":  mut(func(p *plan) { p.Stops[0].DepartTime = "7am" }),
		"vias.last":    mut(func(p *plan) { p.Stops[4].Vias = []planVia{{Name: "x", Lat: -26, Lon: 153}} }),
	}
	for field, p := range cases {
		_, verr := normalisePlan(p)
		if verr == nil {
			t.Fatalf("%s: expected an error", field)
		}
		if verr.Field != field && !(field == "vias.last" && verr.Field == "vias") {
			t.Fatalf("%s: error named %q (%s)", field, verr.Field, verr.Message)
		}
	}
	p := validPlan()
	p.Name = "  Home run "
	p.Stops[0].DepartTime = "06:30"
	got, verr := normalisePlan(p)
	if verr != nil || got.Name != "Home run" || got.Stops[0].DepartTime != "06:30" {
		t.Fatalf("good plan: %+v %v", got, verr)
	}
}

func TestPlanStore_RoundTripKeepsOrderViasAndTimes(t *testing.T) {
	store := newTestDocumentStore(t)
	in := validPlan()
	in.Stops[0].Vias = []planVia{{Name: "Inskip Point", Lat: -25.8, Lon: 153.05}, {Name: "Wide Bay Bar", Lat: -25.9, Lon: 153.1}}
	in.Stops[0].DepartTime = "06:30"
	saved, err := store.CreatePlan(in)
	if err != nil {
		t.Fatalf("CreatePlan: %v", err)
	}
	if saved.ID == 0 || saved.CreatedAt == nil || saved.UpdatedAt == nil {
		t.Fatalf("saved = %+v", saved)
	}
	got, ok, err := store.GetPlan(saved.ID)
	if err != nil || !ok {
		t.Fatalf("GetPlan: %v %v", ok, err)
	}
	if len(got.Stops) != 5 || got.Stops[2].Name != "Mooloolaba" || got.Stops[4].Arrive != "2026-11-19" {
		t.Fatalf("stops = %+v", got.Stops)
	}
	if len(got.Stops[0].Vias) != 2 || got.Stops[0].Vias[1].Name != "Wide Bay Bar" || got.Stops[0].DepartTime != "06:30" {
		t.Fatalf("vias/time lost: %+v", got.Stops[0])
	}
	if got.Stops[1].Vias == nil {
		t.Fatal("no vias is an empty list on the wire, not null")
	}

	// A whole replace drops stops no longer sent.
	in.Name = "Shorter"
	in.Stops = in.Stops[:2]
	in.Stops[1].Nights = 0
	if _, ok, err := store.UpdatePlan(saved.ID, in); err != nil || !ok {
		t.Fatalf("UpdatePlan: %v %v", ok, err)
	}
	got, _, _ = store.GetPlan(saved.ID)
	if got.Name != "Shorter" || len(got.Stops) != 2 {
		t.Fatalf("after replace: %+v", got)
	}
	var n int
	if err := store.db.QueryRow(`SELECT COUNT(*) FROM plan_stops WHERE plan_id = ?`, saved.ID).Scan(&n); err != nil || n != 2 {
		t.Fatalf("stop rows = %d %v", n, err)
	}

	if _, ok, _ := store.UpdatePlan(999, in); ok {
		t.Fatal("updating a plan that is not there")
	}
	if ok, err := store.DeletePlan(saved.ID); err != nil || !ok {
		t.Fatalf("DeletePlan: %v %v", ok, err)
	}
	if err := store.db.QueryRow(`SELECT COUNT(*) FROM plan_stops`).Scan(&n); err != nil || n != 0 {
		t.Fatalf("stops left behind: %d %v", n, err)
	}
	if ok, _ := store.DeletePlan(saved.ID); ok {
		t.Fatal("deleting twice reports nothing deleted")
	}
}

func TestPlanStore_PatchStopShiftsAndSaves(t *testing.T) {
	store := newTestDocumentStore(t)
	saved, err := store.CreatePlan(validPlan())
	if err != nil {
		t.Fatal(err)
	}
	n := 2
	got, ok, err := store.PatchPlanStop(saved.ID, 1, nil, &n)
	if err != nil || !ok {
		t.Fatalf("PatchPlanStop: %v %v", ok, err)
	}
	if got.Stops[4].Arrive != "2026-11-20" {
		t.Fatalf("later dates must shift, got %v", arrivals(got.Stops))
	}
	again, _, _ := store.GetPlan(saved.ID)
	if again.Stops[4].Arrive != "2026-11-20" || again.Stops[1].Nights != 2 {
		t.Fatalf("not persisted: %+v", again.Stops)
	}
}

func planAt(t *testing.T, start string, nights ...int) plan {
	t.Helper()
	p, verr := normalisePlan(plan{Name: "P " + start, Stops: stopsOf(start, nights...)})
	if verr != nil {
		t.Fatal(verr)
	}
	return p
}

func TestSelectCurrentPlan(t *testing.T) {
	now := time.Date(2026, 11, 16, 0, 0, 0, 0, time.UTC) // 16 Nov 10:00 at UTC+10
	past := planAt(t, "2026-11-01", 1, 0)                // 1-2 Nov
	spans := planAt(t, "2026-11-14", 2, 1, 0)            // 14-17 Nov
	later := planAt(t, "2026-12-01", 1, 0)
	soon := planAt(t, "2026-11-20", 1, 0)

	decorated := func(plans ...plan) []plan {
		out := make([]plan, 0, len(plans))
		for _, p := range plans {
			d, err := decoratePlan(p, now)
			if err != nil {
				t.Fatal(err)
			}
			out = append(out, d)
		}
		return out
	}

	idx, st := selectCurrentPlan(decorated(past, later, spans, soon))
	if st != planStatusActive || idx != 2 {
		t.Fatalf("a plan spanning today wins: idx=%d status=%q", idx, st)
	}
	idx, st = selectCurrentPlan(decorated(past, later, soon))
	if st != planStatusUpcoming || idx != 2 {
		t.Fatalf("else the next upcoming (20 Nov, not 1 Dec): idx=%d", idx)
	}
	if _, st := selectCurrentPlan(decorated(past)); st != planStatusPast {
		t.Fatalf("only a past plan: it is named as past, got %q", st)
	}
	if idx, st := selectCurrentPlan(nil); idx != -1 || st != "" {
		t.Fatal("no plans, no current plan")
	}
}

func TestPlanStatus_OneZonePlanIsJudgedAtThePlace(t *testing.T) {
	p := planAt(t, "2026-11-14", 2, 0) // 14-16 Nov, UTC+10
	at := func(v string) time.Time { x, _ := time.Parse(time.RFC3339, v); return x }
	cases := map[string]string{
		"2026-11-13T10:00:00Z": "upcoming",
		"2026-11-14T00:00:00Z": "active",
		"2026-11-16T10:00:00Z": "active", // still 16 Nov at the place until 14:00Z
		"2026-11-16T15:00:00Z": "past",   // 17 Nov at the place
	}
	for now, want := range cases {
		d, err := decoratePlan(p, at(now))
		got := d.Status
		if err != nil || got != want {
			t.Errorf("%s: %q (%v), want %q", now, got, err, want)
		}
	}
}

func routeOf(nm float64, pts ...[2]float64) waterRoute {
	r := waterRoute{DistanceNm: nm}
	for _, p := range pts {
		r.Points = append(r.Points, shorelinePoint{Lat: p[0], Lon: p[1]})
	}
	return r
}

func TestPlanLegs_DistanceIsTheSumThroughTheVias(t *testing.T) {
	prev := planRouteSegment
	t.Cleanup(func() { planRouteSegment = prev })
	// Legs are routed at once, so the stand-in answers by place, not by order.
	var mu sync.Mutex
	var calls [][4]float64
	planRouteSegment = func(_ context.Context, aLat, aLon, bLat, bLon float64) (waterRoute, error) {
		mu.Lock()
		calls = append(calls, [4]float64{aLat, aLon, bLat, bLon})
		mu.Unlock()
		// 10 nm to the first via, 7.5 nm from it, 2 nm for any other segment.
		switch {
		case bLat == -25.8:
			return routeOf(10, [2]float64{aLat, aLon}, [2]float64{bLat, bLon}), nil
		case aLat == -25.8:
			return routeOf(7.5, [2]float64{aLat, aLon}, [2]float64{bLat, bLon}), nil
		}
		return routeOf(2, [2]float64{aLat, aLon}, [2]float64{bLat, bLon}), nil
	}
	p := planAt(t, "2026-11-14", 1, 1, 0)
	p.Stops[0].Vias = []planVia{{Name: "Via A", Lat: -25.8, Lon: 153.05}}

	legs := planLegs(context.Background(), p)
	if len(legs) != 2 {
		t.Fatalf("three stops make two legs, got %d", len(legs))
	}
	if legs[0].Error != "" || legs[0].DistanceNm != 17.5 {
		t.Fatalf("leg 0 = %+v", legs[0])
	}
	if len(calls) != 3 {
		t.Fatalf("leg 0 is two segments and leg 1 one, got %d routed", len(calls))
	}
	// Segment order: stop 0 -> via A -> stop 1.
	if r := legs[0].Route; len(r) != 3 || r[1][0] != -25.8 || r[2][0] != p.Stops[1].Lat {
		t.Fatalf("segments out of order: %v", r)
	}
	if legs[0].From != 0 || legs[0].To != 1 || legs[1].From != 1 || legs[1].To != 2 {
		t.Fatalf("leg indices: %+v", legs)
	}
	if len(legs[0].Route) < 3 {
		t.Fatalf("the route runs through the via: %v", legs[0].Route)
	}
}

func TestPlanLegs_ASegmentThatCannotBeRoutedFailsTheLegAloudAndOnlyThatLeg(t *testing.T) {
	prev := planRouteSegment
	t.Cleanup(func() { planRouteSegment = prev })
	planRouteSegment = func(_ context.Context, aLat, aLon, bLat, bLon float64) (waterRoute, error) {
		if aLat == stopsOf("x", 0, 0)[1].Lat {
			return waterRoute{}, errors.New("start is on land")
		}
		return routeOf(4, [2]float64{aLat, aLon}, [2]float64{bLat, bLon}), nil
	}
	p := planAt(t, "2026-11-14", 1, 1, 0)
	legs := planLegs(context.Background(), p)
	if legs[0].Error != "" || legs[0].DistanceNm != 4 {
		t.Fatalf("leg 0 should route: %+v", legs[0])
	}
	if !strings.Contains(legs[1].Error, "start is on land") || legs[1].DistanceNm != 0 || len(legs[1].Route) != 0 {
		t.Fatalf("leg 1 must carry the router's error and no figure, got %+v", legs[1])
	}
}

func TestPlanLegs_CourseIsTheLongestSegmentsBearing(t *testing.T) {
	prev := planRouteSegment
	t.Cleanup(func() { planRouteSegment = prev })
	planRouteSegment = func(_ context.Context, aLat, aLon, bLat, bLon float64) (waterRoute, error) {
		// A short hop north then a long run east.
		return routeOf(11, [2]float64{aLat, aLon}, [2]float64{aLat + 0.05, aLon}, [2]float64{aLat + 0.05, aLon + 0.2}), nil
	}
	p := planAt(t, "2026-11-14", 0, 0)
	legs := planLegs(context.Background(), p)
	if legs[0].CourseDeg < 85 || legs[0].CourseDeg > 95 {
		t.Fatalf("course = %d, want about east", legs[0].CourseDeg)
	}
}

// --- handlers ---

func planCtx(method, target, body, id, index string) (echo.Context, *httptest.ResponseRecorder) {
	c, rec := newDocumentEchoContext(method, target, body, id)
	if index != "" {
		c.SetParamNames("id", "index")
		c.SetParamValues(id, index)
	}
	return c, rec
}

func TestPlanHandlers_CreateListPatchDelete(t *testing.T) {
	withTestDocumentStore(t)
	body := `{"name":"Home run","stops":[
		{"name":"Great Sandy Straits Marina","lat":-25.5,"lon":152.9,"arrive":"2026-11-14","nights":2,"vias":[],"depart_time":""},
		{"name":"Pelican Bay","lat":-26,"lon":153,"arrive":"","nights":1,"vias":[],"depart_time":""},
		{"name":"Mooloolaba","lat":-26.7,"lon":153.1,"arrive":"","nights":0,"vias":[],"depart_time":""}]}`
	c, rec := planCtx(http.MethodPost, "/api/plans", body, "", "")
	if err := createPlanHandler(c); err != nil || rec.Code != http.StatusOK {
		t.Fatalf("POST: %v %d %s", err, rec.Code, rec.Body.String())
	}
	var created plan
	if err := json.Unmarshal(rec.Body.Bytes(), &created); err != nil || created.ID == 0 {
		t.Fatalf("created = %s", rec.Body.String())
	}
	if created.Stops[2].Arrive != "2026-11-17" || created.Stops[0].Timezone != "Australia/Brisbane" || created.Status != "upcoming" {
		t.Fatalf("created = %+v", created)
	}

	c, rec = planCtx(http.MethodGet, "/api/plans", "", "", "")
	if err := listPlansHandler(c); err != nil || rec.Code != http.StatusOK {
		t.Fatalf("GET: %v %d", err, rec.Code)
	}
	var list struct {
		Plans     []plan `json:"plans"`
		CurrentID *int64 `json:"current_id"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &list); err != nil || len(list.Plans) != 1 {
		t.Fatalf("list = %s", rec.Body.String())
	}
	if list.CurrentID == nil || *list.CurrentID != created.ID {
		t.Fatalf("the only upcoming plan is the current one: %s", rec.Body.String())
	}

	idStr := strings.TrimSpace(strings.Split(strings.Split(rec.Body.String(), `"id":`)[1], ",")[0])
	c, rec = planCtx(http.MethodPatch, "/api/plans/"+idStr+"/stops/1", `{"nights":3}`, idStr, "1")
	if err := patchPlanStopHandler(c); err != nil || rec.Code != http.StatusOK {
		t.Fatalf("PATCH: %v %d %s", err, rec.Code, rec.Body.String())
	}
	var patched plan
	_ = json.Unmarshal(rec.Body.Bytes(), &patched)
	if patched.Stops[2].Arrive != "2026-11-19" {
		t.Fatalf("patched = %s", rec.Body.String())
	}

	c, rec = planCtx(http.MethodPatch, "/api/plans/"+idStr+"/stops/1", `{"nights":-4}`, idStr, "1")
	if err := patchPlanStopHandler(c); err != nil || rec.Code != http.StatusBadRequest || !strings.Contains(rec.Body.String(), `"field":"nights"`) {
		t.Fatalf("bad PATCH: %v %d %s", err, rec.Code, rec.Body.String())
	}
	c, rec = planCtx(http.MethodPost, "/api/plans", `{"name":"","stops":[]}`, "", "")
	if err := createPlanHandler(c); err != nil || rec.Code != http.StatusBadRequest || !strings.Contains(rec.Body.String(), `"field":"name"`) {
		t.Fatalf("bad POST: %v %d %s", err, rec.Code, rec.Body.String())
	}

	c, rec = planCtx(http.MethodDelete, "/api/plans/"+idStr, "", idStr, "")
	if err := deletePlanHandler(c); err != nil || rec.Code != http.StatusOK {
		t.Fatalf("DELETE: %v %d", err, rec.Code)
	}
	c, rec = planCtx(http.MethodGet, "/api/plans/"+idStr, "", idStr, "")
	if err := getPlanHandler(c); err != nil || rec.Code != http.StatusNotFound {
		t.Fatalf("GET after DELETE: %v %d", err, rec.Code)
	}
}

func TestPlanHandlers_LegsEndpointReportsDistancesAndErrors(t *testing.T) {
	store := withTestDocumentStore(t)
	prev := planRouteSegment
	t.Cleanup(func() { planRouteSegment = prev })
	planRouteSegment = func(_ context.Context, aLat, aLon, bLat, bLon float64) (waterRoute, error) {
		return routeOf(6, [2]float64{aLat, aLon}, [2]float64{bLat, bLon}), nil
	}
	saved, err := store.CreatePlan(validPlan())
	if err != nil {
		t.Fatal(err)
	}
	id := strings.TrimSpace(strings.Split(strings.Split(string(mustJSON(t, saved)), `"id":`)[1], ",")[0])
	c, rec := planCtx(http.MethodGet, "/api/plans/"+id+"/legs", "", id, "")
	if err := planLegsHandler(c); err != nil || rec.Code != http.StatusOK {
		t.Fatalf("legs: %v %d %s", err, rec.Code, rec.Body.String())
	}
	var out struct {
		Legs []planLeg `json:"legs"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil || len(out.Legs) != 4 || out.Legs[0].DistanceNm != 6 {
		t.Fatalf("legs = %s", rec.Body.String())
	}
}

// --- what Mate and the watch report still read ---

func TestGetPlannedStay_IsTheStopOfTheCurrentPlan(t *testing.T) {
	store := newTestDocumentStore(t)
	at := func(y int, m time.Month, d int) time.Time { return time.Date(y, m, d, 0, 0, 0, 0, time.UTC) }
	if _, ok, err := store.GetPlannedStay(at(2026, 10, 24)); err != nil || ok {
		t.Fatalf("no plans, no stay: ok=%v err=%v", ok, err)
	}
	// 24 Oct: the plan has not started, so the stay is its first stop.
	if _, err := store.CreatePlan(plan{Name: "Run", Stops: stopsOf("2026-10-27", 4, 1, 0)}); err != nil {
		t.Fatal(err)
	}
	stay, ok, err := store.GetPlannedStay(at(2026, 10, 24))
	if err != nil || !ok || stay.Name != "Great Sandy Straits Marina" || stay.Arrive != "2026-10-27" || stay.Depart != "2026-10-31" || stay.Timezone != "Australia/Brisbane" || !stay.Active {
		t.Fatalf("stay = %+v ok=%v err=%v", stay, ok, err)
	}
	// Mid-plan, the stop the boat is at (or sailing from) today: a lay day at
	// the first stop.
	stay, _, _ = store.GetPlannedStay(at(2026, 10, 29))
	if stay.Name != "Great Sandy Straits Marina" || stay.Depart != "2026-10-31" {
		t.Fatalf("lay-day stay = %+v", stay)
	}
	// 1 Nov is the day the boat sails Pelican Bay to Mooloolaba: it spends the
	// night at the destination.
	stay, _, _ = store.GetPlannedStay(at(2026, 11, 1))
	if stay.Name != "Mooloolaba" || stay.Arrive != "2026-11-01" {
		t.Fatalf("passage-day stay must be the destination, got %+v", stay)
	}
	// After the plan, the last stop, which Active reads as ended.
	stay, ok, _ = store.GetPlannedStay(at(2026, 12, 1))
	if !ok || stay.Name != "Mooloolaba" || stay.Active {
		t.Fatalf("past plan: %+v ok=%v", stay, ok)
	}
}

func TestGetPlannedStay_AllPastIsTheLastStopOfTheMostRecentlyEndedPlan(t *testing.T) {
	store := newTestDocumentStore(t)
	// Saved in an order where the latest-ending plan is neither first nor last.
	for _, c := range []struct{ arrive, lastStop string }{
		{"2026-03-01", "ended March"},
		{"2026-05-01", "ended May"},
		{"2026-04-01", "ended April"},
	} {
		stops := stopsOf(c.arrive, 1, 0)
		stops[1].Name = c.lastStop
		if _, err := store.CreatePlan(plan{Name: c.lastStop, Stops: stops}); err != nil {
			t.Fatal(err)
		}
	}
	stay, ok, err := store.GetPlannedStay(time.Date(2026, 10, 24, 0, 0, 0, 0, time.UTC))
	if err != nil || !ok || stay.Name != "ended May" || stay.Active {
		t.Fatalf("the plan that ended most recently, over: %+v ok=%v err=%v", stay, ok, err)
	}
}

// setStayForTest saves a one-stop plan standing for the old planned stay.
func setStayForTest(store *documentStore, s plannedStay) (plan, error) {
	a, _ := time.Parse(planDateLayout, s.Arrive)
	d, _ := time.Parse(planDateLayout, s.Depart)
	return store.CreatePlan(plan{Name: s.Name, Stops: []planStop{{Name: s.Name, Lat: s.Lat, Lon: s.Lon, Arrive: s.Arrive, Nights: planDaysBetween(a, d)}}})
}

// --- review fixes ---

func TestPatchPlanStop_RefusesAStopWhoseZoneIsUnknown(t *testing.T) {
	store := newTestDocumentStore(t)
	saved, err := store.CreatePlan(validPlan())
	if err != nil {
		t.Fatal(err)
	}
	prev := stayZoneAt
	t.Cleanup(func() { stayZoneAt = prev })
	stayZoneAt = func(lat, lon float64) (placeZone, error) { return placeZone{}, errors.New("no zone here") }
	n := 3
	_, _, err = store.PatchPlanStop(saved.ID, 0, nil, &n)
	var verr *planError
	if !errors.As(err, &verr) || verr.Field != "stops.lat" {
		t.Fatalf("PATCH must run the same zone check as PUT, got %v", err)
	}
	stayZoneAt = prev
	got, _, _ := store.GetPlan(saved.ID)
	if got.Stops[0].Nights != 2 {
		t.Fatalf("a refused patch must write nothing, nights = %d", got.Stops[0].Nights)
	}
}

func TestPlanLegs_AnOverallDeadlineFailsTheLegsExplicitly(t *testing.T) {
	prevSeg, prevDeadline := planRouteSegment, planLegsDeadline
	t.Cleanup(func() { planRouteSegment, planLegsDeadline = prevSeg, prevDeadline })
	planLegsDeadline = 30 * time.Millisecond
	planRouteSegment = func(ctx context.Context, aLat, aLon, bLat, bLon float64) (waterRoute, error) {
		<-ctx.Done()
		return waterRoute{}, ctx.Err()
	}
	start := time.Now()
	legs := planLegs(context.Background(), planAt(t, "2026-11-14", 1, 1, 1, 0))
	if time.Since(start) > 2*time.Second {
		t.Fatalf("the request must not hang on slow segments, took %v", time.Since(start))
	}
	if len(legs) != 3 {
		t.Fatalf("legs = %d", len(legs))
	}
	for i, l := range legs {
		if l.Error == "" || !strings.Contains(l.Error, "took longer than") || l.DistanceNm != 0 {
			t.Fatalf("leg %d must fail explicitly on the deadline, got %+v", i, l)
		}
	}
}

func TestPlanRouteCache_KnownFailuresAreRememberedBrieflyAndNotAcrossACoastlineChange(t *testing.T) {
	now := time.Date(2026, 11, 14, 0, 0, 0, 0, time.UTC)
	c := &waterRouteCache{now: func() time.Time { return now }}
	key := waterRouteKey{pts: [4]float64{1, 2, 3, 4}, stamp: "squares-v1"}
	c.store(key, waterRoute{}, errors.New("start is on land"))
	if e, ok := c.lookup(key); !ok || e.err == nil {
		t.Fatalf("a failure should be served within its TTL: %+v %v", e, ok)
	}
	now = now.Add(waterRouteFailureTTL + time.Second)
	if _, ok := c.lookup(key); ok {
		t.Fatal("a failure must expire so a fixed coastline is retried")
	}
	c.store(key, routeOf(5, [2]float64{1, 2}, [2]float64{3, 4}), nil)
	if e, ok := c.lookup(key); !ok || e.route.DistanceNm != 5 {
		t.Fatalf("a success is served: %+v %v", e, ok)
	}
	changed := key
	changed.stamp = "squares-v2"
	if _, ok := c.lookup(changed); ok {
		t.Fatal("a route found on another coastline must not be served")
	}
}

func TestWaterRouteCache_IdenticalConcurrentSearchesRunOnceAndFullDropsOnlyOne(t *testing.T) {
	c := newWaterRouteCache()
	key := waterRouteKey{pts: [4]float64{1, 2, 3, 4}, stamp: "s"}
	var mu sync.Mutex
	searches := 0
	release := make(chan struct{})
	find := func() (waterRoute, error) {
		mu.Lock()
		searches++
		mu.Unlock()
		<-release
		return routeOf(3, [2]float64{1, 2}, [2]float64{3, 4}), nil
	}
	var wg sync.WaitGroup
	for range 4 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if r, err := c.route(context.Background(), key, find); err != nil || r.DistanceNm != 3 {
				t.Errorf("route = %+v %v", r, err)
			}
		}()
	}
	time.Sleep(50 * time.Millisecond)
	close(release)
	wg.Wait()
	if searches != 1 {
		t.Fatalf("four identical requests should share one search, ran %d", searches)
	}

	for i := 0; i < waterRouteCacheMax+5; i++ {
		c.store(waterRouteKey{pts: [4]float64{float64(i)}, stamp: "s"}, waterRoute{}, nil)
	}
	if len(c.entries) != waterRouteCacheMax {
		t.Fatalf("a full cache drops one entry per new one, holds %d", len(c.entries))
	}
}

func TestPlanLegs_AnEndOnLandNamesTheStopAndKeepsTheRouterMessage(t *testing.T) {
	prev := planRouteSegment
	t.Cleanup(func() { planRouteSegment = prev })
	planRouteSegment = func(_ context.Context, aLat, aLon, bLat, bLon float64) (waterRoute, error) {
		return waterRoute{}, &waterRouteOnLandError{AtStart: true, msg: "the start (-25.5000, 152.9000) is on land and the water in front of it could not be found"}
	}
	p := planAt(t, "2026-11-14", 1, 1, 0)
	legs := planLegs(context.Background(), p)
	if legs[0].ErrorKind != "on_land" || legs[0].ErrorStop != "Great Sandy Straits Marina" {
		t.Fatalf("leg 0 = %+v", legs[0])
	}
	if !strings.Contains(legs[0].Error, "is on land and the water in front of it could not be found") {
		t.Fatalf("the router's wording must stay: %q", legs[0].Error)
	}
	if !strings.HasPrefix(legs[0].Error, "no passage distance for Great Sandy Straits Marina to Pelican Bay: ") ||
		legs[0].Reason != strings.TrimPrefix(legs[0].Error, "no passage distance for Great Sandy Straits Marina to Pelican Bay: ") {
		t.Fatalf("the reason is the router's words without the sentence round them: %q / %q", legs[0].Error, legs[0].Reason)
	}
}

func TestPlanLegs_AViaThatEndsOneSegmentAndStartsTheNextIsReportedOnce(t *testing.T) {
	prev := planRouteSegment
	t.Cleanup(func() { planRouteSegment = prev })
	planRouteSegment = func(_ context.Context, aLat, aLon, bLat, bLon float64) (waterRoute, error) {
		r := routeOf(2, [2]float64{aLat, aLon}, [2]float64{bLat, bLon})
		r.StartMovedNm = 0.3
		r.EndMovedNm = 0.3
		return r, nil
	}
	p := planAt(t, "2026-11-14", 1, 1, 0)
	p.Stops[0].Vias = []planVia{{Name: "Cape Moreton", Lat: -27.0, Lon: 153.5}}
	legs := planLegs(context.Background(), p)
	byStop := map[string]int{}
	for _, m := range legs[0].Moved {
		byStop[m.Stop]++
	}
	if byStop["Cape Moreton"] != 1 || len(legs[0].Moved) != 3 {
		t.Fatalf("each point once, got %+v", legs[0].Moved)
	}
}

func TestNormalisePlan_AViaNameHasTheStopNameLimit(t *testing.T) {
	p := validPlan()
	p.Stops[0].Vias = []planVia{{Name: strings.Repeat("x", planNameMax+1), Lat: -26, Lon: 153}}
	if _, verr := normalisePlan(p); verr == nil || verr.Field != "vias" || !strings.Contains(verr.Message, "longer than 120 characters") {
		t.Fatalf("a 121-character via name must be refused, got %v", verr)
	}
	p.Stops[0].Vias[0].Name = strings.Repeat("x", planNameMax)
	if _, verr := normalisePlan(p); verr != nil {
		t.Fatalf("120 characters is fine: %v", verr)
	}
}

// A plan from Sydney (UTC+11 in November) to Brisbane (UTC+10) and on to the
// Gold Coast. At 13:30Z on the 15th it is already the 16th in Sydney and still
// the 15th in Brisbane, where the boat is.
func twoZonePlan() plan {
	return plan{Name: "North", Stops: []planStop{
		{Name: "Sydney", Lat: -33.86, Lon: 151.21, Arrive: "2026-11-14", Nights: 1},
		{Name: "Brisbane", Lat: -27.47, Lon: 153.03, Nights: 1},
		{Name: "Gold Coast", Lat: -27.95, Lon: 153.43, Nights: 0},
	}}
}

func TestGetPlannedStay_JudgesEachStopInItsOwnZone(t *testing.T) {
	store := newTestDocumentStore(t)
	if _, err := store.CreatePlan(twoZonePlan()); err != nil {
		t.Fatal(err)
	}
	stay, ok, err := store.GetPlannedStay(time.Date(2026, 11, 15, 13, 30, 0, 0, time.UTC))
	if err != nil || !ok || stay.Name != "Brisbane" || !stay.Active {
		t.Fatalf("the boat is in Brisbane on the 15th: %+v ok=%v err=%v", stay, ok, err)
	}
}

func TestPlanStatus_PastIsJudgedAtTheLastStop(t *testing.T) {
	store := newTestDocumentStore(t)
	saved, err := store.CreatePlan(twoZonePlan())
	if err != nil {
		t.Fatal(err)
	}
	// 23:30 on the 16th in Brisbane, where the plan ends; the 17th in Sydney.
	d, err := decoratePlan(saved, time.Date(2026, 11, 16, 13, 30, 0, 0, time.UTC))
	if err != nil || d.Status != planStatusActive {
		t.Fatalf("still the plan's last day at its last stop: %q (%v)", d.Status, err)
	}
	d, err = decoratePlan(saved, time.Date(2026, 11, 16, 14, 30, 0, 0, time.UTC))
	if err != nil || d.Status != planStatusPast {
		t.Fatalf("the 17th at the last stop: %q (%v)", d.Status, err)
	}
}

func TestPlanLegs_AnEndMovedToTheWaterIsReportedByStopName(t *testing.T) {
	prev := planRouteSegment
	t.Cleanup(func() { planRouteSegment = prev })
	planRouteSegment = func(_ context.Context, aLat, aLon, bLat, bLon float64) (waterRoute, error) {
		r := routeOf(4, [2]float64{aLat, aLon}, [2]float64{bLat, bLon})
		r.EndMovedNm = 0.22
		return r, nil
	}
	p := planAt(t, "2026-11-14", 1, 1, 0)
	legs := planLegs(context.Background(), p)
	if len(legs[0].Moved) != 1 || legs[0].Moved[0].Stop != "Pelican Bay" || legs[0].Moved[0].Nm != 0.22 {
		t.Fatalf("moved = %+v", legs[0].Moved)
	}
}
