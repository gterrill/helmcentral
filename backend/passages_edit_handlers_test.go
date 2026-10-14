package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/labstack/echo/v4"
)

func send(t *testing.T, h echo.HandlerFunc, method, body string, params ...string) (int, map[string]any) {
	t.Helper()
	e := echo.New()
	req := httptest.NewRequest(method, "/x", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)
	var names, values []string
	for i := 0; i+1 < len(params); i += 2 {
		names, values = append(names, params[i]), append(values, params[i+1])
	}
	c.SetParamNames(names...)
	c.SetParamValues(values...)
	if err := h(c); err != nil {
		t.Fatal(err)
	}
	var out map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &out)
	return rec.Code, out
}

func TestPatchHandlerNullCruiseUnfilesAndUnknownFieldRejected(t *testing.T) {
	store := withPassageStore(t)
	c, _ := store.InsertCruise(cruiseRecord{Name: "C"})
	p := seedPassage(t, store, passageEpoch, passageEpoch.Add(time.Hour))

	code, body := send(t, patchPassageHandler, http.MethodPatch, `{"title":"X","cruise_id":"`+c.ID+`"}`, "id", p.ID)
	if code != 200 || body["passage"].(map[string]any)["cruise_id"] != c.ID || body["passage"].(map[string]any)["edited"] != true {
		t.Fatal(code, body)
	}
	code, body = send(t, patchPassageHandler, http.MethodPatch, `{"cruise_id":null}`, "id", p.ID)
	if code != 200 || body["passage"].(map[string]any)["cruise_id"] != nil {
		t.Fatal(code, body)
	}
	if code, _ := send(t, patchPassageHandler, http.MethodPatch, `{"edited":false}`, "id", p.ID); code != 400 {
		t.Errorf("the edited flag is not the client's to set: %d", code)
	}
	if code, _ := send(t, patchPassageHandler, http.MethodPatch, `{"cruise_id":"nope"}`, "id", p.ID); code != 404 {
		t.Errorf("unknown cruise: %d", code)
	}
	if code, _ := send(t, patchPassageHandler, http.MethodPatch, `{"title":"x"}`, "id", "missing"); code != 404 {
		t.Errorf("unknown passage: %d", code)
	}
}

func TestSplitMergeTrimHandlers(t *testing.T) {
	store := withPassageStore(t)
	p := seedPassage(t, store, passageEpoch, passageEpoch.Add(4*time.Hour))

	at := passageEpoch.Add(time.Hour).Format(time.RFC3339)
	code, body := send(t, splitPassageHandler, http.MethodPost, `{"at":"`+at+`"}`, "id", p.ID)
	if code != 200 || len(body["passages"].([]any)) != 2 {
		t.Fatal(code, body)
	}
	second := body["passages"].([]any)[1].(map[string]any)["id"].(string)
	if code, _ := send(t, splitPassageHandler, http.MethodPost, `{"at":"2020-01-01T00:00:00Z"}`, "id", p.ID); code != 400 {
		t.Errorf("split outside: %d", code)
	}

	code, body = send(t, mergePassagesHandler, http.MethodPost, `{"ids":["`+p.ID+`","`+second+`"]}`)
	if code != 200 || body["passage"].(map[string]any)["end"] == nil {
		t.Fatal(code, body)
	}
	if code, _ := send(t, mergePassagesHandler, http.MethodPost, `{"ids":["a"]}`); code != 400 {
		t.Errorf("one id: %d", code)
	}

	code, _ = send(t, trimPassageHandler, http.MethodPost, `{"start":"`+passageEpoch.Add(time.Minute).Format(time.RFC3339)+`","end":"`+passageEpoch.Add(time.Hour).Format(time.RFC3339)+`"}`, "id", p.ID)
	if code != 200 {
		t.Errorf("trim: %d", code)
	}
	code, _ = send(t, trimPassageHandler, http.MethodPost, `{"start":"`+passageEpoch.Add(-time.Hour).Format(time.RFC3339)+`","end":"`+passageEpoch.Add(time.Hour).Format(time.RFC3339)+`"}`, "id", p.ID)
	if code != 400 {
		t.Errorf("trim outwards: %d", code)
	}
}

func TestCruiseHandlers(t *testing.T) {
	withPassageStore(t)
	code, body := send(t, createCruiseHandler, http.MethodPost, `{"name":"Whitsundays","notes":"n"}`)
	if code != 201 {
		t.Fatal(code, body)
	}
	id := body["cruise"].(map[string]any)["id"].(string)
	if code, _ := send(t, createCruiseHandler, http.MethodPost, `{"name":" "}`); code != 400 {
		t.Errorf("blank: %d", code)
	}
	if code, body := send(t, listCruisesHandler, http.MethodGet, ``); code != 200 || len(body["cruises"].([]any)) != 1 {
		t.Errorf("list: %d %v", code, body)
	}
	code, body = send(t, getCruiseHandler, http.MethodGet, ``, "id", id)
	if code != 200 || body["stats"].(map[string]any)["passage_count"].(float64) != 0 {
		t.Errorf("empty cruise: %d %v", code, body)
	}
	if code, _ := send(t, patchCruiseHandler, http.MethodPatch, `{"name":"Renamed"}`, "id", id); code != 200 {
		t.Errorf("patch %d", code)
	}
	if code, _ := send(t, putWaterRiseLabelHandler, http.MethodPut, `{"at":"2026-10-14T00:00:00Z","label":"Watermaker"}`, "id", id); code != 200 {
		t.Errorf("label %d", code)
	}
	if code, _ := send(t, putWaterRiseLabelHandler, http.MethodPut, `{"at":"2026-10-14T00:00:00Z","label":"Rain"}`, "id", id); code != 400 {
		t.Errorf("bad label %d", code)
	}
	if code, _ := send(t, deleteCruiseHandler, http.MethodDelete, ``, "id", id); code != 200 {
		t.Errorf("delete %d", code)
	}
	if code, _ := send(t, getCruiseHandler, http.MethodGet, ``, "id", id); code != 404 {
		t.Errorf("gone %d", code)
	}
}
