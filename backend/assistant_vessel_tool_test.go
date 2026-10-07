package main

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"
)

func vesselToolResult(t *testing.T, deps assistantToolDeps) assistantVesselParticularsResult {
	t.Helper()
	raw, err := runRecordTool(t, deps, "get_vessel_particulars", `{}`)
	if err != nil {
		t.Fatalf("get_vessel_particulars: %v", err)
	}
	var res assistantVesselParticularsResult
	if err := json.Unmarshal([]byte(raw), &res); err != nil {
		t.Fatalf("unmarshal: %v (%s)", err, raw)
	}
	return res
}

func (r assistantVesselParticularsResult) find(field, source string) (assistantVesselValue, bool) {
	for _, p := range r.Present {
		if p.Field == field && (source == "" || strings.HasPrefix(p.Source, source)) {
			return p, true
		}
	}
	return assistantVesselValue{}, false
}

func (r assistantVesselParticularsResult) absent(field string) bool {
	for _, a := range r.Absent {
		if a.Field == field {
			return true
		}
	}
	return false
}

func TestGetVesselParticulars_MarksSourcesAndListsWhatIsAbsent(t *testing.T) {
	env := newProposalEnv(t)
	loa := 18.3
	if _, err := env.docs.SetVesselParticulars(vesselParticulars{OwnerName: "Sam Example", PolicyNumber: "POL-9", LOAM: &loa}); err != nil {
		t.Fatal(err)
	}
	env.deps.vesselState = func() (vesselStateData, error) {
		return vesselStateData{Name: "Example Boat", LengthOverallM: 18.3, DraftM: 1.4}, nil
	}
	res := vesselToolResult(t, env.deps)

	if v, ok := res.find("owner_name", vesselSourceStored); !ok || v.Value != "Sam Example" {
		t.Fatalf("expected the stored owner name with its source, got %+v", res.Present)
	}
	if v, ok := res.find("vessel_name", vesselSourceLive); !ok || v.Value != "Example Boat" {
		t.Fatalf("expected the live boat name, got %+v", res.Present)
	}
	if _, ok := res.find("draft_m", vesselSourceLive); !ok {
		t.Fatalf("expected the live draft, got %+v", res.Present)
	}
	if _, ok := res.find("loa_m", vesselSourceStored); !ok {
		t.Fatalf("expected the stored length, got %+v", res.Present)
	}
	if _, ok := res.find("loa_m", vesselSourceLive); !ok {
		t.Fatalf("expected the live length too, got %+v", res.Present)
	}
	for _, f := range []string{"owner_phone", "insurer", "home_marina", "berth", "storm_delegate", "beam_m"} {
		if !res.absent(f) {
			t.Errorf("%s was never set and must be listed as absent", f)
		}
	}
	if len(res.Notes) != 0 {
		t.Errorf("equal lengths need no note, got %v", res.Notes)
	}
}

func TestGetVesselParticulars_NothingConnectedNothingStored(t *testing.T) {
	env := newProposalEnv(t)
	env.deps.vesselState = func() (vesselStateData, error) { return vesselStateData{LengthOverallM: -1, DraftM: -1}, nil }
	res := vesselToolResult(t, env.deps)
	if len(res.Present) != 0 {
		t.Fatalf("nothing is known, nothing is returned as present: %+v", res.Present)
	}
	for _, f := range []string{"owner_name", "vessel_name", "draft_m", "loa_m"} {
		if !res.absent(f) {
			t.Errorf("%s must be absent", f)
		}
	}
}

func TestGetVesselParticulars_DifferingLengthsAreFlaggedAndLiveFailureIsReported(t *testing.T) {
	env := newProposalEnv(t)
	loa := 17.0
	if _, err := env.docs.SetVesselParticulars(vesselParticulars{LOAM: &loa}); err != nil {
		t.Fatal(err)
	}
	env.deps.vesselState = func() (vesselStateData, error) { return vesselStateData{LengthOverallM: 18.3, DraftM: -1}, nil }
	res := vesselToolResult(t, env.deps)
	if len(res.Notes) != 1 || !strings.Contains(res.Notes[0], "17.0 m stored, 18.3 m from live") {
		t.Fatalf("expected a note on the differing lengths, got %v", res.Notes)
	}

	env.deps.vesselState = func() (vesselStateData, error) { return vesselStateData{}, errors.New("no feed") }
	res = vesselToolResult(t, env.deps)
	if len(res.Notes) != 1 || !strings.Contains(res.Notes[0], "no feed") {
		t.Fatalf("a failed live read is reported, not hidden: %v", res.Notes)
	}
	if v, ok := res.find("loa_m", vesselSourceStored); !ok || v.Value != 17.0 {
		t.Fatalf("stored data is still returned: %+v", res.Present)
	}
}

func TestGetVesselParticulars_LiveZeroIsNotAFigure(t *testing.T) {
	env := newProposalEnv(t)
	env.deps.vesselState = func() (vesselStateData, error) { return vesselStateData{LengthOverallM: 0, DraftM: 0}, nil }
	res := vesselToolResult(t, env.deps)
	if len(res.Present) != 0 {
		t.Fatalf("a live 0 is not a design figure: %+v", res.Present)
	}
	for _, f := range []string{"draft_m", "loa_m"} {
		if !res.absent(f) {
			t.Errorf("%s must be absent for a live 0", f)
		}
	}
}
