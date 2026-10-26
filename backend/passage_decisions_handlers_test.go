package main

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	"github.com/labstack/echo/v4"
)

func pdPut(t *testing.T, id string, body string) *httptest.ResponseRecorder {
	t.Helper()
	e := echo.New()
	req := httptest.NewRequest(http.MethodPut, "/api/passage-decisions/"+id, strings.NewReader(body))
	req.Header.Set(echo.HeaderContentType, echo.MIMEApplicationJSON)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)
	c.SetParamNames("id")
	c.SetParamValues(id)
	if err := putPassageDecisionHandler(c); err != nil {
		t.Fatalf("handler: %v", err)
	}
	return rec
}

func pdSeed(t *testing.T) (*assistantStore, string) {
	t.Helper()
	asst := withTestAssistantStore(t)
	conv, _ := asst.CreateConversation("c")
	msg, err := appendCards(t, asst, conv.ID, pdCard(1, pdRoutes(1), "2026-10-23", "2026-10-24"))
	if err != nil {
		t.Fatal(err)
	}
	return asst, strconv.FormatInt(msg.Cards[0].Decision.ID, 10)
}

func TestPutPassageDecision_RecordsTheOperatorsChoice(t *testing.T) {
	asst, id := pdSeed(t)
	rec := pdPut(t, id, `{"state":"went","row_date":"2026-10-24"}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("code %d: %s", rec.Code, rec.Body)
	}
	var out struct {
		Decision passageDecisionView `json:"decision"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatal(err)
	}
	if out.Decision.State != "went" || out.Decision.RowDate == nil || *out.Decision.RowDate != "2026-10-24" || out.Decision.Inferred || out.Decision.PassageID != nil {
		t.Fatalf("decision = %+v", out.Decision)
	}
	var source string
	asst.db.QueryRow(`SELECT source FROM passage_decisions WHERE id = ?`, id).Scan(&source)
	if source != "operator" {
		t.Fatalf("source = %q", source)
	}
	// Stayed, just looking, and a blank all clear the date.
	for _, state := range []string{"stayed", "just_looking", ""} {
		rec = pdPut(t, id, `{"state":"`+state+`"}`)
		if rec.Code != http.StatusOK {
			t.Fatalf("%q: code %d: %s", state, rec.Code, rec.Body)
		}
		var rd *string
		var src string
		asst.db.QueryRow(`SELECT row_date, source FROM passage_decisions WHERE id = ?`, id).Scan(&rd, &src)
		if rd != nil || src != "operator" {
			t.Fatalf("%q: row_date=%v source=%q (an operator blank stays operator)", state, rd, src)
		}
	}
}

func TestPutPassageDecision_RefusesWhatTheSnapshotCannotHold(t *testing.T) {
	_, id := pdSeed(t)
	for name, body := range map[string]string{
		"unknown state":        `{"state":"maybe"}`,
		"went without a date":  `{"state":"went"}`,
		"date on stayed":       `{"state":"stayed","row_date":"2026-10-23"}`,
		"date not in the card": `{"state":"went","row_date":"2026-10-30"}`,
		"malformed date":       `{"state":"went","row_date":"yesterday"}`,
		"unknown field":        `{"state":"stayed","extra":1}`,
		"empty object":         `{}`,
		"date only":            `{"row_date":null}`,
		"trailing data":        `{"state":"stayed"}{"state":"went"}`,
		"trailing junk":        `{"state":"stayed"} x`,
		"oversize":             `{"state":"stayed",` + strings.Repeat(" ", 5000) + `"row_date":null}`,
		"not json":             `nope`,
	} {
		if rec := pdPut(t, id, body); rec.Code != http.StatusBadRequest {
			t.Errorf("%s: code %d, want 400: %s", name, rec.Code, rec.Body)
		}
	}
}

func TestPutPassageDecision_UnknownIDIs404(t *testing.T) {
	withTestAssistantStore(t)
	if rec := pdPut(t, "999", `{"state":"stayed"}`); rec.Code != http.StatusNotFound {
		t.Fatalf("code %d", rec.Code)
	}
	if rec := pdPut(t, "abc", `{"state":"stayed"}`); rec.Code != http.StatusNotFound {
		t.Fatalf("a non-numeric id names nothing: %d", rec.Code)
	}
}

func TestPassageDecisionRoutes_AreWriteTier(t *testing.T) {
	found := false
	for _, r := range passageDecisionRoutes() {
		if r.Method == http.MethodPut && r.Path == "/api/passage-decisions/:id" {
			found = true
			if r.Tier != tierWrite {
				t.Fatalf("tier = %v, want write", r.Tier)
			}
		}
	}
	if !found {
		t.Fatal("PUT /api/passage-decisions/:id is not routed")
	}
	for _, r := range buildAPIRoutes(newTestSessionStore(t), newWorldImageryHTTPClient()) {
		if r.Path == "/api/passage-decisions/:id" {
			return
		}
	}
	t.Fatal("buildAPIRoutes does not compose passageDecisionRoutes")
}

func TestSetOperatorPassageDecision_RowGoneBeforeTheUpdateIsNotFound(t *testing.T) {
	asst, id := pdSeed(t)
	n, _ := strconv.ParseInt(id, 10, 64)
	asst.db.Exec(`DELETE FROM passage_decisions`)
	if _, err := asst.SetOperatorPassageDecision(n, "stayed", nil); !errors.Is(err, errPassageDecisionNotFound) {
		t.Fatalf("err = %v", err)
	}
}
