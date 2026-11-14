package main

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"
)

// Plans through Mate (ADR 0289): a changeset can create a plan or replace one
// whole. Both go through the rule the Forecast page's writes use, and an update's
// card says only what differs. A plan is never deleted by a changeset.

func planStopMap(name string, lat, lon float64, nights int, extra map[string]any) map[string]any {
	m := map[string]any{"name": name, "lat": lat, "lon": lon, "nights": nights}
	for k, v := range extra {
		m[k] = v
	}
	return m
}

// threeStopPlan is Pelican Bay on Sunday 15 November for a night, Manly for a
// night and The Boat Works on Tuesday 17 November.
func threeStopPlan(manlyNights int) []any {
	return []any{
		planStopMap("Pelican Bay", -25.5, 152.9, 1, map[string]any{"arrive": "2026-11-15"}),
		planStopMap("Manly", -27.45, 153.19, manlyNights, nil),
		planStopMap("The Boat Works", -27.88, 153.35, 0, nil),
	}
}

func planCreateArgs(name string, stops []any) string {
	return opsArgs(changeOp{Type: "plan", Action: "create", Fields: map[string]any{"name": name, "stops": stops}})
}

func planUpdateArgs(id int64, fields map[string]any) string {
	return opsArgs(changeOp{Type: "plan", Action: "update", ID: fmt.Sprint(id), Fields: fields})
}

func TestProposePlan_CreateWritesNothingUntilApplyAndThenChainsTheDates(t *testing.T) {
	env := newProposalEnv(t)
	stops := threeStopPlan(2)
	stops[0].(map[string]any)["source"] = "saved place"
	stops[1].(map[string]any)["source"] = "place names"
	p := env.propose(t, planCreateArgs("To The Boat Works", stops))

	if plans, _ := env.docs.ListPlans(); len(plans) != 0 {
		t.Fatalf("a proposal writes nothing, got %+v", plans)
	}
	if len(p.Ops) != 1 || !strings.Contains(p.Ops[0].Description, "To The Boat Works") || !strings.Contains(p.Ops[0].Description, "3 stops") {
		t.Fatalf("description = %+v", p.Ops)
	}
	rows, _ := p.Ops[0].After["stops"].([]any)
	row := func(i int) map[string]any {
		if i >= len(rows) {
			return map[string]any{}
		}
		m, _ := rows[i].(map[string]any)
		return m
	}
	if len(rows) != 3 || row(0)["source"] != "saved place" || row(1)["source"] != "place names" || row(1)["arrive"] != "2026-11-16" || row(2)["arrive"] != "2026-11-18" || row(1)["lat"] != -27.45 {
		t.Fatalf("the card rows are date, nights, place, source and position: %+v", p.Ops[0].After)
	}

	applied, err := env.apply(p.ID)
	if err != nil {
		t.Fatalf("apply: %v", err)
	}
	plans, _ := env.docs.ListPlans()
	if len(plans) != 1 || plans[0].Name != "To The Boat Works" || plans[0].Stops[2].Arrive != "2026-11-18" || plans[0].Stops[1].Lat != -27.45 {
		t.Fatalf("saved = %+v", plans)
	}
	// The card offers Open plan, which lands on the Forecast page with that plan chosen.
	var result changeResult
	if err := json.Unmarshal(applied.Result, &result); err != nil || len(result.Ops) != 1 {
		t.Fatalf("result = %s (%v)", applied.Result, err)
	}
	if got := result.Ops[0].Href; got != fmt.Sprintf("/forecast?plan=%d", plans[0].ID) {
		t.Fatalf("href = %q", got)
	}
}

func TestProposePlan_AcceptsYourPositionAsASource(t *testing.T) {
	env := newProposalEnv(t)
	stops := threeStopPlan(2)
	stops[0].(map[string]any)["source"] = "your position"
	p := env.propose(t, planCreateArgs("To The Boat Works", stops))
	rows, _ := p.Ops[0].After["stops"].([]any)
	first, _ := rows[0].(map[string]any)
	if first["source"] != "your position" {
		t.Fatalf("the card keeps the source the operator's own position carries: %+v", p.Ops[0].After)
	}
}

func TestProposePlan_RefusalsNameTheField(t *testing.T) {
	deps, _ := proposeDeps(t)
	good := func() []any { return threeStopPlan(1) }
	without := func(key string) []any {
		s := good()
		delete(s[1].(map[string]any), key)
		return s
	}
	with := func(key string, v any) []any {
		s := good()
		s[1].(map[string]any)[key] = v
		return s
	}
	tests := []struct {
		name, name2, want string
		stops             []any
	}{
		{"a stop with no lat", "Run", "lat", without("lat")},
		{"a stop with no lon", "Run", "lon", without("lon")},
		{"a misspelt stop field", "Run", "nigths", with("nigths", 2)},
		{"an unknown source", "Run", "source", with("source", "my memory")},
		{"a position out of range", "Run", "lat", with("lat", 95.0)},
		{"nights that are not whole", "Run", "nights", with("nights", 1.5)},
		{"a blank name and a nameless last stop", " ", "name", func() []any {
			s := good()
			s[2].(map[string]any)["name"] = " "
			return s
		}()},
		{"no stops", "Run", "stops", []any{}},
		{"an arrive-by on the first stop", "Run", "arrive_by", func() []any {
			s := good()
			s[0].(map[string]any)["arrive_by"] = "2026-11-15T16:00"
			return s
		}()},
		{"no first arrival", "Run", "arrive", func() []any {
			s := good()
			delete(s[0].(map[string]any), "arrive")
			return s
		}()},
		{"a via with no position", "Run", "via", with("vias", []any{map[string]any{"name": "Cape"}})},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			msg := proposeError(t, deps, planCreateArgs(tc.name2, tc.stops))
			if !strings.Contains(msg, tc.want) {
				t.Fatalf("expected the error to name %q, got %q", tc.want, msg)
			}
		})
	}
}

func TestProposePlan_ADeleteIsRefused(t *testing.T) {
	env := newProposalEnv(t)
	saved, err := env.docs.CreatePlan(plan{Name: "Run", Stops: []planStop{{Name: "Manly", Lat: -27.45, Lon: 153.19, Arrive: "2026-11-15", Nights: 1}}})
	if err != nil {
		t.Fatal(err)
	}
	msg := proposeError(t, env.deps, opsArgs(changeOp{Type: "plan", Action: "delete", ID: fmt.Sprint(saved.ID)}))
	if !strings.Contains(msg, "cannot be") {
		t.Fatalf("expected a refusal, got %q", msg)
	}
}

func TestProposePlan_AnUpdateSaysOnlyWhatDiffers(t *testing.T) {
	env := newProposalEnv(t)
	saved, err := env.docs.CreatePlan(plan{Name: "To The Boat Works", Stops: planStopsOf(threeStopPlan(1))})
	if err != nil {
		t.Fatal(err)
	}
	p := env.propose(t, planUpdateArgs(saved.ID, map[string]any{"stops": threeStopPlan(2)}))
	d := p.Ops[0].Description
	if !strings.Contains(d, "Manly: 1 night → 2 nights") || !strings.Contains(d, "Later stops move a day: The Boat Works arrives Wed 18 Nov.") {
		t.Fatalf("description = %q", d)
	}
	if strings.Contains(d, "Pelican Bay") {
		t.Fatalf("an unchanged stop is not mentioned: %q", d)
	}
	if len(p.Ops[0].Before) != 0 || len(p.Ops[0].After) != 0 {
		t.Fatalf("an update card has the sentence alone, got before %+v after %+v", p.Ops[0].Before, p.Ops[0].After)
	}
	if p.Ops[0].BaseVersion == "" {
		t.Fatal("the update should carry the version it was drafted against")
	}

	if _, err := env.apply(p.ID); err != nil {
		t.Fatalf("apply: %v", err)
	}
	got, _, _ := env.docs.GetPlan(saved.ID)
	if got.Stops[1].Nights != 2 || got.Stops[2].Arrive != "2026-11-18" {
		t.Fatalf("saved = %+v", got.Stops)
	}
}

func TestProposePlan_TheOtherKindsOfDifference(t *testing.T) {
	env := newProposalEnv(t)
	saved, err := env.docs.CreatePlan(plan{Name: "To The Boat Works", Stops: planStopsOf(threeStopPlan(1))})
	if err != nil {
		t.Fatal(err)
	}
	renamed := threeStopPlan(1)
	renamed[0].(map[string]any)["depart_time"] = "06:30"
	renamed[1].(map[string]any)["arrive_by"] = "2026-11-16T16:00"
	renamed[1].(map[string]any)["vias"] = []any{map[string]any{"name": "Cape Moreton", "lat": -27.03, "lon": 153.47}}
	moved := threeStopPlan(1)
	moved[0].(map[string]any)["arrive"] = "2026-11-16"
	added := append(threeStopPlan(1)[:2:2], planStopMap("Scarborough", -27.2, 153.1, 1, nil), threeStopPlan(1)[2])
	removed := threeStopPlan(1)[:1:1]
	removed = append(removed, threeStopPlan(1)[2])

	tests := []struct {
		name   string
		fields map[string]any
		want   []string
	}{
		{"rename", map[string]any{"name": "Home run"}, []string{`Renamed to "Home run"`}},
		{"time, arrive-by and via", map[string]any{"stops": renamed}, []string{"Pelican Bay: departure time set to 6.30 AM", "Manly: arrive-by set to Mon 16 Nov 4.00 PM", "Manly: via Cape Moreton added"}},
		{"first arrival moves the plan", map[string]any{"stops": moved}, []string{"Pelican Bay: arrives Sun 15 Nov → Mon 16 Nov", "Later stops move a day"}},
		{"a stop added", map[string]any{"stops": added}, []string{"Scarborough added after Manly", "1 night"}},
		{"a stop removed", map[string]any{"stops": removed}, []string{"Manly removed"}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			p := runPropose(t, env.deps, planUpdateArgs(saved.ID, tc.fields))
			for _, w := range tc.want {
				if !strings.Contains(p.Ops[0].Description, w) {
					t.Errorf("description %q lacks %q", p.Ops[0].Description, w)
				}
			}
		})
	}
}

func TestProposePlan_AChangeThatLeavesThePlanAsItIsIsRefused(t *testing.T) {
	env := newProposalEnv(t)
	saved, err := env.docs.CreatePlan(plan{Name: "To The Boat Works", Stops: planStopsOf(threeStopPlan(1))})
	if err != nil {
		t.Fatal(err)
	}
	msg := proposeError(t, env.deps, planUpdateArgs(saved.ID, map[string]any{"name": "To The Boat Works", "stops": threeStopPlan(1)}))
	if !strings.Contains(msg, "exactly as it is") {
		t.Fatalf("got %q", msg)
	}
}

func TestProposePlan_AnUpdateGoesStaleWhenThePlanChangedAfterDrafting(t *testing.T) {
	env := newProposalEnv(t)
	saved, err := env.docs.CreatePlan(plan{Name: "To The Boat Works", Stops: planStopsOf(threeStopPlan(1))})
	if err != nil {
		t.Fatal(err)
	}
	p := env.propose(t, planUpdateArgs(saved.ID, map[string]any{"stops": threeStopPlan(2)}))
	env.advance()
	if _, _, err := env.docs.PatchPlanStop(saved.ID, 0, nil, intPtr(3)); err != nil {
		t.Fatal(err)
	}
	if _, err := env.apply(p.ID); err == nil || !strings.Contains(err.Error(), "changed since Mate proposed this") {
		t.Fatalf("expected a stale refusal, got %v", err)
	}
	got, _, _ := env.docs.GetPlan(saved.ID)
	if got.Stops[1].Nights != 1 {
		t.Fatalf("a stale card writes nothing, got %+v", got.Stops)
	}
}

func TestPlanVersion_ChangesOnEveryWriteEvenInTheSameSecond(t *testing.T) {
	env := newProposalEnv(t)
	saved, err := env.docs.CreatePlan(plan{Name: "To The Boat Works", Stops: planStopsOf(threeStopPlan(1))})
	if err != nil {
		t.Fatal(err)
	}
	p := env.propose(t, planUpdateArgs(saved.ID, map[string]any{"stops": threeStopPlan(2)}))
	// The operator edits on the page in the same second as Mate's read: the clock
	// has not moved.
	patched, _, err := env.docs.PatchPlanStop(saved.ID, 0, nil, intPtr(3))
	if err != nil {
		t.Fatal(err)
	}
	if planVersion(patched) == planVersion(saved) {
		t.Fatalf("two writes in one second share the version %q", planVersion(saved))
	}
	if _, err := env.apply(p.ID); err == nil || !strings.Contains(err.Error(), "changed since Mate proposed this") {
		t.Fatalf("a proposal based on the first version must go stale, got %v", err)
	}
	// A whole-plan PUT bumps the version too.
	again, _, err := env.docs.UpdatePlan(saved.ID, plan{Name: "Renamed", Stops: patched.Stops})
	if err != nil {
		t.Fatal(err)
	}
	if planVersion(again) == planVersion(patched) {
		t.Fatalf("a whole-plan update left the version %q", planVersion(patched))
	}
}

func TestProposePlan_ABlankNameDefaultsToTheLastStop(t *testing.T) {
	env := newProposalEnv(t)
	p := env.propose(t, planCreateArgs("", threeStopPlan(1)))
	if _, err := env.apply(p.ID); err != nil {
		t.Fatalf("apply: %v", err)
	}
	plans, _ := env.docs.ListPlans()
	if len(plans) != 1 || plans[0].Name != "To The Boat Works" {
		t.Fatalf("saved = %+v", plans)
	}
}

func TestProposePlan_AStopCopiedFromGetPlansIsAccepted(t *testing.T) {
	env := newProposalEnv(t)
	saved, err := env.docs.CreatePlan(plan{Name: "To The Boat Works", Stops: planStopsOf(threeStopPlan(1))})
	if err != nil {
		t.Fatal(err)
	}
	// Every field get_plans gives a stop, as Mate would send it back.
	raw, err := json.Marshal(planReportStop{Name: "Pelican Bay", Lat: -25.5, Lon: 152.9, Arrive: "2026-11-15", ArriveLabel: "Sun 15 Nov", Nights: 1,
		Depart: "2026-11-16", DepartLabel: "Mon 16 Nov", DepartTime: "06:30", Vias: []planVia{}, Timezone: "Australia/Brisbane"})
	if err != nil {
		t.Fatal(err)
	}
	var first map[string]any
	if err := json.Unmarshal(raw, &first); err != nil {
		t.Fatal(err)
	}
	first["nights"] = 1
	stops := threeStopPlan(1)
	stops[0] = first
	p := env.propose(t, planUpdateArgs(saved.ID, map[string]any{"stops": stops}))
	if !strings.Contains(p.Ops[0].Description, "departure time set") {
		t.Fatalf("description = %q", p.Ops[0].Description)
	}
	// A genuinely unknown field is still refused.
	bad := threeStopPlan(1)
	bad[1].(map[string]any)["nigths"] = 2
	if msg := proposeError(t, env.deps, planUpdateArgs(saved.ID, map[string]any{"stops": bad})); !strings.Contains(msg, "nigths") {
		t.Fatalf("got %q", msg)
	}
}

func TestPlanRecordType_IsReadThroughListAndGetRecord(t *testing.T) {
	env := newProposalEnv(t)
	saved, err := env.docs.CreatePlan(plan{Name: "To The Boat Works", Stops: planStopsOf(threeStopPlan(1))})
	if err != nil {
		t.Fatal(err)
	}
	raw, err := runRecordTool(t, env.deps, "list_records", `{"type":"plan"}`)
	if err != nil || !strings.Contains(raw, "To The Boat Works") {
		t.Fatalf("list_records: %v %s", err, raw)
	}
	raw, err = runRecordTool(t, env.deps, "get_record", fmt.Sprintf(`{"type":"plan","id":"%d"}`, saved.ID))
	if err != nil || !strings.Contains(raw, "Manly") || !strings.Contains(raw, `"nights"`) {
		t.Fatalf("get_record: %v %s", err, raw)
	}
}

func intPtr(n int) *int { return &n }

// planStopsOf turns the proposal-shaped stops into the store's.
func planStopsOf(in []any) []planStop {
	out := make([]planStop, len(in))
	for i, v := range in {
		m := v.(map[string]any)
		out[i] = planStop{Name: m["name"].(string), Lat: m["lat"].(float64), Lon: m["lon"].(float64), Nights: m["nights"].(int)}
		if a, ok := m["arrive"].(string); ok {
			out[i].Arrive = a
		}
	}
	return out
}
