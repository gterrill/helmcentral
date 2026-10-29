package main

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"
)

// A planned stay is the one place the operator is booked into and the dates:
// the Forecast page shows the weather there. One row, replaced whole, treated
// as absent the day after departure.

func validStay() plannedStay {
	return plannedStay{Name: "Coral Coast Marina", Lat: -27.2, Lon: 153.1, Arrive: "2026-10-27", Depart: "2026-10-31"}
}

func TestPlannedStay_EmptyBeforeAnythingIsSet(t *testing.T) {
	store := newTestDocumentStore(t)
	_, ok, err := store.GetPlannedStay()
	if err != nil || ok {
		t.Fatalf("expected no stay and no error, got ok=%v err=%v", ok, err)
	}
}

func TestPlannedStay_RoundTripReplacesAndClears(t *testing.T) {
	store := newTestDocumentStore(t)
	in := validStay()
	in.Name = "  Coral Coast Marina "
	saved, err := store.SetPlannedStay(in)
	if err != nil {
		t.Fatalf("SetPlannedStay: %v", err)
	}
	if saved.Name != "Coral Coast Marina" || saved.UpdatedAt == nil {
		t.Fatalf("name is trimmed and updated_at set, got %+v", saved)
	}
	got, ok, err := store.GetPlannedStay()
	if err != nil || !ok || got.Lat != -27.2 || got.Lon != 153.1 || got.Arrive != "2026-10-27" || got.Depart != "2026-10-31" {
		t.Fatalf("round trip = %+v ok=%v err=%v", got, ok, err)
	}

	second := plannedStay{Name: "Elsewhere", Lat: 10, Lon: -20, Arrive: "2026-11-01", Depart: "2026-11-01"}
	if _, err := store.SetPlannedStay(second); err != nil {
		t.Fatalf("a one-night stay is fine: %v", err)
	}
	var n int
	if err := store.db.QueryRow(`SELECT COUNT(*) FROM planned_stay`).Scan(&n); err != nil || n != 1 {
		t.Fatalf("there is only ever one stay: n=%d err=%v", n, err)
	}

	if err := store.ClearPlannedStay(); err != nil {
		t.Fatalf("ClearPlannedStay: %v", err)
	}
	if _, ok, _ := store.GetPlannedStay(); ok {
		t.Fatal("expected the stay cleared")
	}
	if err := store.ClearPlannedStay(); err != nil {
		t.Fatalf("clearing nothing is not an error: %v", err)
	}
}

func TestPlannedStay_Validation(t *testing.T) {
	store := newTestDocumentStore(t)
	mut := func(f func(*plannedStay)) plannedStay { s := validStay(); f(&s); return s }
	cases := map[string]plannedStay{
		"name":   mut(func(s *plannedStay) { s.Name = "   " }),
		"lat":    mut(func(s *plannedStay) { s.Lat = 91 }),
		"lon":    mut(func(s *plannedStay) { s.Lon = -181 }),
		"arrive": mut(func(s *plannedStay) { s.Arrive = "27 Oct" }),
		"depart": mut(func(s *plannedStay) { s.Depart = "2026-10-26" }), // before arrival
	}
	cases["name-long"] = mut(func(s *plannedStay) { s.Name = strings.Repeat("x", plannedStayNameMax+1) })
	for label, s := range cases {
		field := strings.TrimSuffix(label, "-long")
		_, err := store.SetPlannedStay(s)
		var verr *plannedStayError
		if err == nil {
			t.Fatalf("%s: expected an error", label)
		}
		if !asPlannedStayError(err, &verr) || verr.Field != field {
			t.Fatalf("%s: expected a field error naming %s, got %v", label, field, err)
		}
	}
	if _, err := store.SetPlannedStay(mut(func(s *plannedStay) { s.Arrive = "2026-02-30" })); err == nil {
		t.Fatal("an impossible calendar date is refused")
	}
}

func TestPlannedStayActive_DropsTheDayAfterDepartureAtThePlace(t *testing.T) {
	s := validStay() // lon 153.1: the place keeps UTC+10
	at := func(v string) time.Time { x, _ := time.Parse(time.RFC3339, v); return x }
	plannedStayActive := func(s plannedStay, now time.Time) bool {
		active, err := plannedStayActive(s, now)
		if err != nil {
			t.Fatal(err)
		}
		return active
	}
	if !plannedStayActive(s, at("2026-10-20T00:00:00Z")) || !plannedStayActive(s, at("2026-10-31T10:00:00Z")) {
		t.Fatal("a stay not yet started, and one departing today, are both live")
	}
	// 20:00 UTC on the 31st is already the 1st at the place.
	if plannedStayActive(s, at("2026-10-31T20:00:00Z")) {
		t.Fatal("once the place's day after departure begins the stay is gone, whatever the UTC date")
	}
	// 20:00 UTC on the 31st is still the 31st in the west: the stay is live.
	west := plannedStay{Name: "X", Lat: 40, Lon: -75, Arrive: "2026-10-27", Depart: "2026-10-31"}
	if !plannedStayActive(west, at("2026-11-01T02:00:00Z")) {
		t.Fatal("it is still the 31st at a western place")
	}
}

func TestPlannedStayHandlers(t *testing.T) {
	withTestDocumentStore(t)

	// Expiry is decided at the place, from the server clock; the client sends
	// no date.
	clock := time.Date(2026, 10, 24, 0, 0, 0, 0, time.UTC)
	prevNow := plannedStayNow
	plannedStayNow = func() time.Time { return clock }
	t.Cleanup(func() { plannedStayNow = prevNow })

	c, rec := newDocumentEchoContext(http.MethodGet, "/api/planned-stay", "", "")
	if err := getPlannedStayHandler(c); err != nil || rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"stay":null`) {
		t.Fatalf("empty GET: %v %d %s", err, rec.Code, rec.Body.String())
	}

	body := `{"name":"Coral Coast Marina","lat":-27.2,"lon":153.1,"arrive":"2026-10-27","depart":"2026-10-31"}`
	c, rec = newDocumentEchoContext(http.MethodPut, "/api/planned-stay", body, "")
	if err := putPlannedStayHandler(c); err != nil || rec.Code != http.StatusOK {
		t.Fatalf("PUT: %v %d %s", err, rec.Code, rec.Body.String())
	}

	c, rec = newDocumentEchoContext(http.MethodGet, "/api/planned-stay", "", "")
	if err := getPlannedStayHandler(c); err != nil {
		t.Fatal(err)
	}
	var got struct {
		Stay *plannedStay `json:"stay"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil || got.Stay == nil || got.Stay.Name != "Coral Coast Marina" {
		t.Fatalf("GET on the departure day = %s", rec.Body.String())
	}

	clock = time.Date(2026, 10, 31, 20, 0, 0, 0, time.UTC) // already 1 Nov at UTC+10
	c, rec = newDocumentEchoContext(http.MethodGet, "/api/planned-stay", "", "")
	if err := getPlannedStayHandler(c); err != nil || !strings.Contains(rec.Body.String(), `"stay":null`) {
		t.Fatalf("GET the day after departure should be absent: %v %s", err, rec.Body.String())
	}

	c, rec = newDocumentEchoContext(http.MethodPut, "/api/planned-stay", `{"name":"X","lat":1,"lon":2,"arrive":"2026-10-30","depart":"2026-10-28"}`, "")
	if err := putPlannedStayHandler(c); err != nil {
		t.Fatal(err)
	}
	if rec.Code != http.StatusBadRequest || !strings.Contains(rec.Body.String(), `"field":"depart"`) {
		t.Fatalf("expected a 400 naming depart, got %d %s", rec.Code, rec.Body.String())
	}

	c, rec = newDocumentEchoContext(http.MethodDelete, "/api/planned-stay", "", "")
	if err := deletePlannedStayHandler(c); err != nil || rec.Code != http.StatusOK {
		t.Fatalf("DELETE: %v %d", err, rec.Code)
	}
	c, rec = newDocumentEchoContext(http.MethodGet, "/api/planned-stay", "", "")
	if err := getPlannedStayHandler(c); err != nil || !strings.Contains(rec.Body.String(), `"stay":null`) {
		t.Fatalf("after DELETE: %v %s", err, rec.Body.String())
	}
}

// --- Mate: a record type the operator applies, and a read tool ---

func TestPlannedStayRecord_CreatesThroughAChangesetAndKeepsWhatIsNotGiven(t *testing.T) {
	env := newProposalEnv(t)
	p := env.propose(t, `{"operations":[{"type":"planned_stay","action":"update","id":"stay","fields":{"name":"Coral Coast Marina","lat":-27.2,"lon":153.1,"arrive":"2026-10-27","depart":"2026-10-31"}}]}`)
	if p.Ops[0].BaseVersion != plannedStayUnsetVersion {
		t.Fatalf("an empty record has the unset version, got %q", p.Ops[0].BaseVersion)
	}
	// Proposing alone writes nothing.
	if _, ok, _ := env.docs.GetPlannedStay(); ok {
		t.Fatal("a proposal must not save the stay")
	}
	if _, err := env.apply(p.ID); err != nil {
		t.Fatalf("apply: %v", err)
	}
	got, ok, _ := env.docs.GetPlannedStay()
	if !ok || got.Name != "Coral Coast Marina" || got.Depart != "2026-10-31" {
		t.Fatalf("expected the stay saved, got %+v ok=%v", got, ok)
	}

	env.advance()
	p = env.propose(t, `{"operations":[{"type":"planned_stay","action":"update","id":"stay","fields":{"depart":"2026-11-02"}}]}`)
	if _, err := env.apply(p.ID); err != nil {
		t.Fatalf("apply: %v", err)
	}
	got, _, _ = env.docs.GetPlannedStay()
	if got.Name != "Coral Coast Marina" || got.Depart != "2026-11-02" {
		t.Fatalf("a changeset changes only the fields it names, got %+v", got)
	}
}

func TestPlannedStayRecord_FirstUpdateMustGiveEveryField(t *testing.T) {
	env := newProposalEnv(t)
	msg := proposeError(t, env.deps, `{"operations":[{"type":"planned_stay","action":"update","id":"stay","fields":{"name":"Coral Coast Marina"}}]}`)
	if !strings.Contains(msg, "arrive") {
		t.Fatalf("expected the missing fields to be named, got %s", msg)
	}
}

func TestPlannedStayRecord_DeleteClearsAndNeedsAStay(t *testing.T) {
	env := newProposalEnv(t)
	msg := proposeError(t, env.deps, `{"operations":[{"type":"planned_stay","action":"delete","id":"stay"}]}`)
	if !strings.Contains(msg, "no stay") {
		t.Fatalf("expected a refusal naming that no stay is planned, got %s", msg)
	}
	if _, err := env.docs.SetPlannedStay(validStay()); err != nil {
		t.Fatal(err)
	}
	env.advance()
	p := env.propose(t, `{"operations":[{"type":"planned_stay","action":"delete","id":"stay"}]}`)
	if _, err := env.apply(p.ID); err != nil {
		t.Fatalf("apply: %v", err)
	}
	if _, ok, _ := env.docs.GetPlannedStay(); ok {
		t.Fatal("expected the stay cleared")
	}
}

func TestPlannedStayRecord_CreateAndOtherIDsRefused(t *testing.T) {
	env := newProposalEnv(t)
	cases := map[string]string{
		"create": `{"operations":[{"type":"planned_stay","action":"create","fields":{"name":"X"}}]}`,
		"id":     `{"operations":[{"type":"planned_stay","action":"update","id":"other","fields":{"name":"X"}}]}`,
	}
	wants := map[string]string{"create": "cannot be created", "id": `no planned stay with id "other"`}
	for name, args := range cases {
		if msg := proposeError(t, env.deps, args); !strings.Contains(msg, wants[name]) {
			t.Errorf("%s: expected %q, got %s", name, wants[name], msg)
		}
	}
}

func TestPlannedStayRecord_BadDatesAreFieldErrors(t *testing.T) {
	env := newProposalEnv(t)
	msg := proposeError(t, env.deps, `{"operations":[{"type":"planned_stay","action":"update","id":"stay","fields":{"name":"X","lat":1,"lon":2,"arrive":"2026-10-30","depart":"2026-10-28"}}]}`)
	if !strings.Contains(msg, "depart") {
		t.Fatalf("expected depart to be named, got %s", msg)
	}
}

func TestGetPlannedStayTool(t *testing.T) {
	env := newProposalEnv(t)
	env.deps.now = func() time.Time { return time.Date(2026, 10, 24, 0, 0, 0, 0, time.UTC) }
	read := func() map[string]any {
		raw, err := runRecordTool(t, env.deps, "get_planned_stay", `{}`)
		if err != nil {
			t.Fatalf("get_planned_stay: %v", err)
		}
		var out map[string]any
		if err := json.Unmarshal([]byte(raw), &out); err != nil {
			t.Fatalf("unmarshal %s: %v", raw, err)
		}
		return out
	}
	if out := read(); out["stay"] != nil || out["note"] == nil {
		t.Fatalf("with none planned the tool says so, got %v", out)
	}
	if _, err := env.docs.SetPlannedStay(plannedStay{Name: "Coral Coast Marina", Lat: -27.2, Lon: 153.1, Arrive: "2026-10-27", Depart: "2026-10-31"}); err != nil {
		t.Fatal(err)
	}
	out := read()
	stay, _ := out["stay"].(map[string]any)
	if stay == nil || stay["name"] != "Coral Coast Marina" || stay["lat"].(float64) != -27.2 {
		t.Fatalf("expected the stay, got %v", out)
	}
	env.deps.now = func() time.Time { return time.Date(2026, 10, 31, 20, 0, 0, 0, time.UTC) } // 1 Nov at UTC+10
	if out := read(); out["stay"] != nil {
		t.Fatalf("the day after departure the tool reports none, got %v", out)
	}
}
