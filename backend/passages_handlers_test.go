package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/labstack/echo/v4"
)

func withPassageStore(t *testing.T) *passageStore {
	t.Helper()
	store := newTestPassageStore(t)
	prev := globalPassageStore
	globalPassageStore = store
	t.Cleanup(func() { globalPassageStore = prev })
	return store
}

func getJSON(t *testing.T, h echo.HandlerFunc, path, id string) (int, map[string]any) {
	t.Helper()
	e := echo.New()
	req := httptest.NewRequest(http.MethodGet, path, nil)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)
	if id != "" {
		c.SetParamNames("id")
		c.SetParamValues(id)
	}
	if err := h(c); err != nil {
		t.Fatal(err)
	}
	var body map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &body)
	return rec.Code, body
}

func TestListPassagesNewestFirstWithStats(t *testing.T) {
	store := withPassageStore(t)
	b := simpleTrip(2 * time.Hour)
	appendAll(t, store, b.pts)
	older, _ := store.InsertPassage(passageRecord{Start: b.pts[2].At, End: b.pts[360].At, Title: "older"})
	newer, _ := store.InsertPassage(passageRecord{Start: b.pts[360].At.Add(time.Hour), End: b.pts[360].At.Add(2 * time.Hour), Title: "newer"})
	store.InsertPassage(passageRecord{Start: b.pts[0].At.Add(-time.Hour), End: b.pts[0].At, Hidden: true})

	code, body := getJSON(t, listPassagesHandler, "/api/passages", "")
	if code != 200 {
		t.Fatal(code)
	}
	list := body["passages"].([]any)
	if len(list) != 2 {
		t.Fatalf("hidden passage listed: %d", len(list))
	}
	first := list[0].(map[string]any)
	if first["id"] != newer.ID || list[1].(map[string]any)["id"] != older.ID {
		t.Errorf("order wrong: %v", list)
	}
	if lon, ok := list[1].(map[string]any)["start_lon"].(float64); !ok || lon == 0 {
		t.Errorf("summary must carry start_lon, got %v", list[1].(map[string]any)["start_lon"])
	}
	stats := list[1].(map[string]any)["stats"].(map[string]any)
	if stats["distance_nm"].(float64) < 11 {
		t.Errorf("distance %v", stats["distance_nm"])
	}
	if stats["fuel_burned_l"] != nil {
		t.Errorf("no fuel readings must serialise as null, got %v", stats["fuel_burned_l"])
	}
}

func TestGetPassageReturnsRecordStatsAndSimplifiedTrack(t *testing.T) {
	store := withPassageStore(t)
	b := simpleTrip(2 * time.Hour)
	appendAll(t, store, b.pts)
	rec, _ := store.InsertPassage(passageRecord{Start: b.pts[2].At, End: b.pts[360].At, Title: "t"})

	code, body := getJSON(t, getPassageHandler, "/api/passages/x", rec.ID)
	if code != 200 {
		t.Fatal(code, body)
	}
	p := body["passage"].(map[string]any)
	if p["id"] != rec.ID || p["title"] != "t" || p["hidden"] != false || p["cruise_id"] != nil {
		t.Errorf("record fields: %v", p)
	}
	track := p["track"].([]any)
	if len(track) != 1 {
		t.Fatalf("segments %d", len(track))
	}
	if n := len(track[0].([]any)); n != 2 {
		t.Errorf("a straight run simplifies to its two ends, got %d points", n)
	}
	times := p["track_times"].([]any)
	if len(times) != 1 || len(times[0].([]any)) != 2 {
		t.Fatalf("track_times must match track: %v", times)
	}
	if got := int64(times[0].([]any)[0].(float64)); got != b.pts[2].At.Unix() {
		t.Errorf("first track time %d, want %d", got, b.pts[2].At.Unix())
	}
}

func TestGetPassageUnknownIs404AndNoStoreIs503(t *testing.T) {
	withPassageStore(t)
	if code, _ := getJSON(t, getPassageHandler, "/api/passages/nope", "nope"); code != 404 {
		t.Errorf("code %d", code)
	}
	globalPassageStore = nil
	if code, _ := getJSON(t, listPassagesHandler, "/api/passages", ""); code != 503 {
		t.Errorf("code %d", code)
	}
}

func plannedSeed(t *testing.T) (*passageStore, *assistantStore, passageRecord) {
	t.Helper()
	store := withPassageStore(t)
	asst := withTestAssistantStore(t)
	p := seedPassage(t, store, passageEpoch, passageEpoch.Add(time.Hour))
	return store, asst, p
}

func plannedList(t *testing.T, id string) (int, []any, map[string]any) {
	t.Helper()
	code, body := getJSON(t, getPassageHandler, "/api/passages/"+id, id)
	if code != 200 {
		return code, nil, body
	}
	passage := body["passage"].(map[string]any)
	list, ok := passage["planned_with_mate"].([]any)
	if !ok {
		t.Fatalf("planned_with_mate must always be a list: %v", passage["planned_with_mate"])
	}
	return code, list, passage
}

func TestGetPassagePlannedWithMateEmptyIsAnEmptyList(t *testing.T) {
	_, _, p := plannedSeed(t)
	code, list, passage := plannedList(t, p.ID)
	if code != 200 || len(list) != 0 {
		t.Fatal(code, list)
	}
	if passage["rating"] != "" {
		t.Errorf("rating %v", passage["rating"])
	}
}

func TestGetPassagePlannedWithMateListsLinkedDecisions(t *testing.T) {
	_, asst, p := plannedSeed(t)
	conv, _ := asst.CreateConversation("c")
	msg, err := appendCards(t, asst, conv.ID, pdCard(2, pdRoutes(2), "2026-10-23", "2026-10-24"), pdCard(1, pdRoutes(1), "2026-10-25"))
	if err != nil {
		t.Fatal(err)
	}
	id := msg.Cards[0].Decision.ID
	if _, err := asst.db.Exec(`UPDATE passage_decisions SET state='went', row_date='2026-10-24', source='operator', passage_id=? WHERE id=?`, p.ID, id); err != nil {
		t.Fatal(err)
	}
	code, list, _ := plannedList(t, p.ID)
	if code != 200 || len(list) != 1 {
		t.Fatal(code, list)
	}
	e := list[0].(map[string]any)
	d := e["decision"].(map[string]any)
	if int64(d["id"].(float64)) != id || d["state"] != "went" || d["row_date"] != "2026-10-24" || d["passage_id"] != p.ID {
		t.Errorf("decision %v", d)
	}
	if _, has := e["row_date"]; has {
		t.Errorf("row_date lives on the decision only: %v", e)
	}
	if e["row_verdict"] != "go" || e["timezone"] != "Australia/Brisbane" || e["conversation_id"] != conv.ID || e["chat_exists"] != true {
		t.Errorf("entry %v", e)
	}
	if legs := e["legs"].([]any); len(legs) != 2 || legs[0].(map[string]any)["name"] != "Leg" {
		t.Errorf("legs %v", e["legs"])
	}
}

func TestGetPassagePlannedWithMateSurvivesTheChatBeingDeleted(t *testing.T) {
	_, asst, p := plannedSeed(t)
	conv, _ := asst.CreateConversation("c")
	msg, _ := appendCards(t, asst, conv.ID, pdCard(1, pdRoutes(1), "2026-10-23"))
	asst.db.Exec(`UPDATE passage_decisions SET state='went', row_date='2026-10-23', source='operator', passage_id=? WHERE id=?`, p.ID, msg.Cards[0].Decision.ID)
	if err := asst.DeleteConversation(conv.ID); err != nil {
		t.Fatal(err)
	}
	code, list, _ := plannedList(t, p.ID)
	if code != 200 || len(list) != 1 {
		t.Fatalf("the decision outlives the chat: %d %v", code, list)
	}
	e := list[0].(map[string]any)
	if e["chat_exists"] != false || e["conversation_id"] != conv.ID {
		t.Errorf("entry %v", e)
	}
}

func TestGetPassageWhenPlansCannotBeReadStillReturnsThePassageWithTheError(t *testing.T) {
	_, asst, p := plannedSeed(t)
	check := func(label string) {
		code, body := getJSON(t, getPassageHandler, "/api/passages/"+p.ID, p.ID)
		if code != 200 {
			t.Fatalf("%s: %d", label, code)
		}
		passage := body["passage"].(map[string]any)
		if passage["id"] != p.ID {
			t.Errorf("%s: passage lost: %v", label, passage)
		}
		if v, present := passage["planned_with_mate"]; !present || v != nil {
			t.Errorf("%s: planned_with_mate = %v, want null", label, v)
		}
		if msg, _ := passage["planned_with_mate_error"].(string); msg == "" {
			t.Errorf("%s: no planned_with_mate_error", label)
		}
	}
	asst.Close()
	check("closed store")
	prev := globalAssistantStore
	globalAssistantStore = nil
	defer func() { globalAssistantStore = prev }()
	check("no store")
}

func TestGetPassageWithPlansHasNoErrorField(t *testing.T) {
	_, _, p := plannedSeed(t)
	_, _, passage := plannedList(t, p.ID)
	if _, present := passage["planned_with_mate_error"]; present {
		t.Errorf("error field present on success: %v", passage["planned_with_mate_error"])
	}
}

func TestGetPassageAPlanWhoseRowDateIsNotOnItsCardIsAnError(t *testing.T) {
	_, asst, p := plannedSeed(t)
	conv, _ := asst.CreateConversation("c")
	msg, _ := appendCards(t, asst, conv.ID, pdCard(1, pdRoutes(1), "2026-10-23"))
	asst.db.Exec(`UPDATE passage_decisions SET state='went', row_date='2030-01-01', source='operator', passage_id=? WHERE id=?`, p.ID, msg.Cards[0].Decision.ID)
	if _, err := asst.PassagePlans(p.ID); err == nil {
		t.Fatal("want an error")
	}
}
