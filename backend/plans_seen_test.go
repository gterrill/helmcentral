package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"testing"
)

// The verdict each leg had when the operator last looked is kept on the box, per
// plan and per leg key, so the Forecast page can mark what changed (ADR 0288).

func TestPlanSeen_SavedVerdictsComeBackAndReplaceOlderOnes(t *testing.T) {
	store := newTestDocumentStore(t)
	keys := []string{"A → B|2026-11-14", "B → C|2026-11-15"}
	if err := store.SavePlanSeen(7, keys, []planSeenVerdict{{Key: keys[0], Verdict: "go"}, {Key: keys[1], Verdict: "marginal"}}); err != nil {
		t.Fatal(err)
	}
	if err := store.SavePlanSeen(7, keys, []planSeenVerdict{{Key: keys[0], Verdict: "no_go"}}); err != nil {
		t.Fatal(err)
	}
	got, err := store.PlanSeen(7)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[keys[0]] != "no_go" {
		t.Fatalf("seen = %v: the second snapshot replaces the first whole", got)
	}
	if other, _ := store.PlanSeen(8); len(other) != 0 {
		t.Fatalf("another plan sees %v", other)
	}
}

func TestPlanSeen_ASnapshotThatIsUnchangedWritesNothing(t *testing.T) {
	store := newTestDocumentStore(t)
	key := "A → B|2026-11-14"
	snapshot := []planSeenVerdict{{Key: key, Verdict: "go"}}
	if err := store.SavePlanSeen(7, []string{key}, snapshot); err != nil {
		t.Fatal(err)
	}
	changes := func() int {
		var n int
		if err := store.db.QueryRow(`SELECT total_changes()`).Scan(&n); err != nil {
			t.Fatal(err)
		}
		return n
	}
	before := changes()
	if err := store.SavePlanSeen(7, []string{key}, snapshot); err != nil {
		t.Fatal(err)
	}
	if after := changes(); after != before {
		t.Fatalf("an unchanged snapshot made %d row changes", after-before)
	}
}

func TestPlanSeen_AKeyTheLegsNoLongerHaveIsDropped(t *testing.T) {
	store := newTestDocumentStore(t)
	old := "A → B|2026-11-14"
	if err := store.SavePlanSeen(7, []string{old}, []planSeenVerdict{{Key: old, Verdict: "go"}}); err != nil {
		t.Fatal(err)
	}
	now := "A → B|2026-11-15" // the leg moved a day
	if err := store.SavePlanSeen(7, []string{now}, []planSeenVerdict{{Key: now, Verdict: "marginal"}, {Key: "stranger|x", Verdict: "go"}}); err != nil {
		t.Fatal(err)
	}
	got, _ := store.PlanSeen(7)
	if len(got) != 1 || got[now] != "marginal" {
		t.Fatalf("seen = %v: only the plan's current legs are kept", got)
	}
}

func TestPlanSeen_DeletingThePlanDeletesItsMarks(t *testing.T) {
	store := newTestDocumentStore(t)
	saved, err := store.CreatePlan(validPlan())
	if err != nil {
		t.Fatal(err)
	}
	key := "x → y|2026-11-14"
	if err := store.SavePlanSeen(saved.ID, []string{key}, []planSeenVerdict{{Key: key, Verdict: "go"}}); err != nil {
		t.Fatal(err)
	}
	if _, err := store.DeletePlan(saved.ID); err != nil {
		t.Fatal(err)
	}
	if got, _ := store.PlanSeen(saved.ID); len(got) != 0 {
		t.Fatalf("marks outlived the plan: %v", got)
	}
}

func TestPlanSeenHandlers_RoundTripAndRefuseAnUnknownVerdict(t *testing.T) {
	store := withTestDocumentStore(t)
	saved, err := store.CreatePlan(plan{Name: "Run", Stops: stopsOf("2026-11-14", 0, 1, 0)})
	if err != nil {
		t.Fatal(err)
	}
	id := fmt.Sprint(saved.ID)
	d, err := decoratePlan(saved, planNow())
	if err != nil {
		t.Fatal(err)
	}
	key := planLegKey(d, 0)

	body := `{"legs":[{"key":"` + key + `","verdict":"marginal"},{"key":"not a leg|x","verdict":"go"}]}`
	c, rec := planCtx(http.MethodPut, "/api/plans/"+id+"/seen", body, id, "")
	if err := planSeenPutHandler(c); err != nil || rec.Code != http.StatusOK {
		t.Fatalf("put: %v %d %s", err, rec.Code, rec.Body.String())
	}
	c, rec = planCtx(http.MethodGet, "/api/plans/"+id+"/seen", "", id, "")
	if err := planSeenGetHandler(c); err != nil || rec.Code != http.StatusOK {
		t.Fatalf("get: %v %d", err, rec.Code)
	}
	var out struct {
		Legs []planSeenVerdict `json:"legs"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil || len(out.Legs) != 1 || out.Legs[0].Key != key || out.Legs[0].Verdict != "marginal" {
		t.Fatalf("seen = %s (%v)", rec.Body.String(), err)
	}

	c, rec = planCtx(http.MethodPut, "/api/plans/"+id+"/seen", `{"legs":[{"key":"`+key+`","verdict":"great"}]}`, id, "")
	if err := planSeenPutHandler(c); err != nil || rec.Code != http.StatusBadRequest || !strings.Contains(rec.Body.String(), "verdict") {
		t.Fatalf("unknown verdict: %v %d %s", err, rec.Code, rec.Body.String())
	}
	c, rec = planCtx(http.MethodGet, "/api/plans/999/seen", "", "999", "")
	if err := planSeenGetHandler(c); err != nil || rec.Code != http.StatusNotFound {
		t.Fatalf("missing plan: %v %d", err, rec.Code)
	}
}

func TestPlansRoutes_SeenIsReadToReadAndWriteToWrite(t *testing.T) {
	tiers := map[string]string{}
	for _, r := range plansRoutes() {
		if r.Path == "/api/plans/:id/seen" {
			tiers[r.Method] = fmt.Sprint(r.Tier)
		}
	}
	if tiers[http.MethodGet] != fmt.Sprint(tierRead) || tiers[http.MethodPut] != fmt.Sprint(tierWrite) {
		t.Fatalf("tiers = %v", tiers)
	}
}
