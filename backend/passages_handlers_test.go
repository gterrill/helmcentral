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
