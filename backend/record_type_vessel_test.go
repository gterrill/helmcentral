package main

import (
	"strings"
	"testing"
)

// Vessel particulars as a singleton, update-only record type: Mate saves the
// answers an operator gave in chat through the ordinary Apply card.

func TestVesselParticularsRecord_UpdateThroughAChangeset(t *testing.T) {
	env := newProposalEnv(t)
	p := env.propose(t, `{"operations":[{"type":"vessel_particulars","action":"update","id":"vessel","fields":{"owner_name":"Sam Example","policy_number":"POL-9","loa_m":18.3}}]}`)
	if len(p.Ops) != 1 || !strings.Contains(p.Ops[0].Description, "owner name Sam Example") || p.Ops[0].BaseVersion != vesselParticularsUnsetVersion {
		t.Fatalf("unexpected proposal op: %+v", p.Ops)
	}
	if _, err := env.apply(p.ID); err != nil {
		t.Fatalf("apply: %v", err)
	}
	got, err := env.docs.GetVesselParticulars()
	if err != nil {
		t.Fatal(err)
	}
	if got.OwnerName != "Sam Example" || got.PolicyNumber != "POL-9" || got.LOAM == nil || *got.LOAM != 18.3 || got.UpdatedAt == nil {
		t.Fatalf("expected the fields saved, got %+v", got)
	}
}

func TestVesselParticularsRecord_UpdateKeepsWhatIsNotGiven(t *testing.T) {
	env := newProposalEnv(t)
	if _, err := env.docs.SetVesselParticulars(vesselParticulars{Builder: "Granocean", Berth: "C14"}); err != nil {
		t.Fatal(err)
	}
	env.advance()
	p := env.propose(t, `{"operations":[{"type":"vessel_particulars","action":"update","id":"vessel","fields":{"berth":"D2"}}]}`)
	if _, err := env.apply(p.ID); err != nil {
		t.Fatalf("apply: %v", err)
	}
	got, _ := env.docs.GetVesselParticulars()
	if got.Builder != "Granocean" || got.Berth != "D2" {
		t.Fatalf("a changeset changes only the fields it names, got %+v", got)
	}
}

func TestVesselParticularsRecord_IsSingletonAndUpdateOnly(t *testing.T) {
	env := newProposalEnv(t)
	cases := map[string]string{
		"create": `{"operations":[{"type":"vessel_particulars","action":"create","fields":{"owner_name":"X"}}]}`,
		"delete": `{"operations":[{"type":"vessel_particulars","action":"delete","id":"vessel"}]}`,
		"id":     `{"operations":[{"type":"vessel_particulars","action":"update","id":"other","fields":{"owner_name":"X"}}]}`,
	}
	wants := map[string]string{"create": "cannot be created", "delete": "cannot be deleted", "id": `no vessel details with id "other"`}
	for name, args := range cases {
		msg := proposeError(t, env.deps, args)
		if !strings.Contains(msg, wants[name]) {
			t.Errorf("%s: expected %q, got %s", name, wants[name], msg)
		}
	}
}

func TestVesselParticularsRecord_BadValueIsAFieldError(t *testing.T) {
	env := newProposalEnv(t)
	msg := proposeError(t, env.deps, `{"operations":[{"type":"vessel_particulars","action":"update","id":"vessel","fields":{"date_acquired":"last spring"}}]}`)
	if !strings.Contains(msg, "date_acquired") {
		t.Fatalf("expected the bad field to be named, got %s", msg)
	}
}

func TestVesselParticularsRecord_ChangedSinceProposalIsStale(t *testing.T) {
	env := newProposalEnv(t)
	p := env.propose(t, `{"operations":[{"type":"vessel_particulars","action":"update","id":"vessel","fields":{"owner_name":"Sam"}}]}`)
	env.advance()
	if _, err := env.docs.SetVesselParticulars(vesselParticulars{Insurer: "Other"}); err != nil {
		t.Fatal(err)
	}
	if _, err := env.apply(p.ID); err == nil {
		t.Fatal("expected a stale refusal after the record changed")
	}
}

func TestVesselParticularsRecord_ListAndGet(t *testing.T) {
	env := newProposalEnv(t)
	raw, err := runRecordTool(t, env.deps, "get_record", `{"type":"vessel_particulars","id":"vessel"}`)
	if err != nil || !strings.Contains(raw, `"owner_name":""`) || !strings.Contains(raw, `"loa_m":null`) {
		t.Fatalf("get_record: %v %s", err, raw)
	}
	raw, err = runRecordTool(t, env.deps, "list_records", `{"type":"vessel_particulars"}`)
	if err != nil || !strings.Contains(raw, `"id":"vessel"`) {
		t.Fatalf("list_records: %v %s", err, raw)
	}
}
