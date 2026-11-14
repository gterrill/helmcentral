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

func TestPlanLegKey_SurvivesASlipAndNamesRepeatedLegsApart(t *testing.T) {
	a := planAt(t, "2026-11-14", 2, 1, 1, 1, 0)
	slipped := planAt(t, "2026-11-15", 2, 1, 1, 1, 0)
	for i := range a.Stops[:len(a.Stops)-1] {
		if planLegKey(a, i) != planLegKey(slipped, i) {
			t.Fatalf("leg %d key changed with its date: %q vs %q", i, planLegKey(a, i), planLegKey(slipped, i))
		}
	}
	// There and back between the same two places: the second trip is its own leg.
	rt := plan{Stops: []planStop{{Name: "A"}, {Name: "B"}, {Name: "A"}, {Name: "B"}}}
	keys := planLegKeys(rt)
	if keys[0] == keys[2] || keys[0] == keys[1] {
		t.Fatalf("repeated legs share a key: %q", keys)
	}
}

func TestPlanLegKey_NamesWithSeparatorsDoNotCollide(t *testing.T) {
	x := plan{Stops: []planStop{{Name: "A → B"}, {Name: "C"}}}
	y := plan{Stops: []planStop{{Name: "A"}, {Name: "B → C"}}}
	z := plan{Stops: []planStop{{Name: "A|B"}, {Name: "C"}}}
	w := plan{Stops: []planStop{{Name: "A"}, {Name: "B|C"}}}
	if planLegKey(x, 0) == planLegKey(y, 0) || planLegKey(z, 0) == planLegKey(w, 0) {
		t.Fatalf("keys collide: %q %q / %q %q", planLegKey(x, 0), planLegKey(y, 0), planLegKey(z, 0), planLegKey(w, 0))
	}
}

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
	if got[keys[0]] != "no_go" || got[keys[1]] != "marginal" || len(got) != 2 {
		t.Fatalf("seen = %v: the second snapshot replaces the verdicts it carries and keeps the rest", got)
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

func TestPlanSeen_ALegThatBrieflyHasNoVerdictKeepsItsLastSeenOne(t *testing.T) {
	store := newTestDocumentStore(t)
	key := "A → B|2026-11-14"
	other := "B → C|2026-11-15"
	keys := []string{key, other}
	if err := store.SavePlanSeen(7, keys, []planSeenVerdict{{Key: key, Verdict: "go"}, {Key: other, Verdict: "go"}}); err != nil {
		t.Fatal(err)
	}
	// A visit where the forecast was down: the first leg has no verdict to save.
	if err := store.SavePlanSeen(7, keys, []planSeenVerdict{{Key: other, Verdict: "go"}}); err != nil {
		t.Fatal(err)
	}
	got, _ := store.PlanSeen(7)
	if got[key] != "go" {
		t.Fatalf("seen = %v: the leg lost its last-seen Go while it had no verdict", got)
	}
	// It comes back as No-Go, and is then compared with the Go it last had.
	if err := store.SavePlanSeen(7, keys, []planSeenVerdict{{Key: key, Verdict: "no_go"}, {Key: other, Verdict: "go"}}); err != nil {
		t.Fatal(err)
	}
	if got, _ = store.PlanSeen(7); got[key] != "no_go" {
		t.Fatalf("seen = %v", got)
	}
	// A key the plan no longer has is dropped even when nothing replaces it.
	if err := store.SavePlanSeen(7, []string{other}, nil); err != nil {
		t.Fatal(err)
	}
	if got, _ = store.PlanSeen(7); len(got) != 1 || got[other] != "go" {
		t.Fatalf("seen = %v: a key that is no longer a leg is dropped", got)
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
