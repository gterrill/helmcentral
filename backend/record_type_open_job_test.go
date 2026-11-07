package main

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"
)

// Open jobs through Mate (ADR 0269): a changeset can add one, and the read
// tools show the open ones. A changeset cannot close, drop or delete a job;
// Done writes the service log and Dropped ends the operator's own list, so
// both stay with the operator's tap.

func TestProposeOpenJob_AddsItOnApplyAndNotBefore(t *testing.T) {
	env := newProposalEnv(t)
	pump := env.equipment(t, equipmentItem{Name: "Shower pump", System: "water"})

	p := env.propose(t, fmt.Sprintf(`{"operations":[{"type":"open_job","action":"create","fields":{"equipment_id":%q,"title":"Leaks at the outlet","notes":"Drip when it runs","before_on":"2026-12-01"}}]}`, pump.ID))
	if len(p.Ops) != 1 || !strings.Contains(p.Ops[0].Description, "Shower pump") || !strings.Contains(p.Ops[0].Description, "Leaks at the outlet") {
		t.Fatalf("expected a line naming the item and the job, got %+v", p.Ops)
	}
	if jobs, _ := env.docs.ListOpenJobs(openJobFilter{Status: openJobStatusAny}); len(jobs) != 0 {
		t.Fatalf("a proposal writes nothing, got %+v", jobs)
	}

	if _, err := env.apply(p.ID); err != nil {
		t.Fatalf("apply: %v", err)
	}
	jobs, _ := env.docs.ListOpenJobs(openJobFilter{})
	if len(jobs) != 1 {
		t.Fatalf("expected one open job, got %+v", jobs)
	}
	got := jobs[0]
	if got.Title != "Leaks at the outlet" || got.Notes != "Drip when it runs" || got.BeforeOn != "2026-12-01" ||
		got.EquipmentID == nil || *got.EquipmentID != pump.ID {
		t.Fatalf("unexpected job: %+v", got)
	}
	if got.Source != openJobSourceMate || got.OpenedOn != "2026-09-30" {
		t.Fatalf("expected the Mate source and the operator's date, got %+v", got)
	}
}

func TestProposeOpenJob_NeedsNoItem(t *testing.T) {
	env := newProposalEnv(t)
	p := env.propose(t, `{"operations":[{"type":"open_job","action":"create","fields":{"title":"Renew the flare kit"}}]}`)
	if _, err := env.apply(p.ID); err != nil {
		t.Fatalf("apply: %v", err)
	}
	jobs, _ := env.docs.ListOpenJobs(openJobFilter{})
	if len(jobs) != 1 || jobs[0].EquipmentID != nil {
		t.Fatalf("expected one job with no item, got %+v", jobs)
	}
}

func TestProposeOpenJob_RefusalsNameTheField(t *testing.T) {
	deps, _ := proposeDeps(t)
	tests := []struct {
		name, fields, want string
	}{
		{"unknown item", `{"equipment_id":"nope","title":"x"}`, "equipment_id"},
		{"blank title", `{"title":" "}`, "title"},
		{"bad date", `{"title":"x","before_on":"soon"}`, "before_on"},
		{"source is not Mate's to set", `{"title":"x","source":"alarm"}`, "source"},
		{"link is not Mate's to set", `{"title":"x","source_link":"/alarms"}`, "source_link"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			msg := proposeError(t, deps, `{"operations":[{"type":"open_job","action":"create","fields":`+tc.fields+`}]}`)
			if !strings.Contains(msg, tc.want) {
				t.Fatalf("expected the error to name %q, got %q", tc.want, msg)
			}
		})
	}
}

func TestProposeOpenJob_CannotUpdateOrDelete(t *testing.T) {
	env := newProposalEnv(t)
	job := mustOpenJob(t, env.docs, "2026-09-01", openJobReq("Leaks"))
	for _, action := range []string{"update", "delete"} {
		args := fmt.Sprintf(`{"operations":[{"type":"open_job","action":%q,"id":%q,"fields":{"title":"x"}}]}`, action, job.ID)
		if action == "delete" {
			args = fmt.Sprintf(`{"operations":[{"type":"open_job","action":"delete","id":%q}]}`, job.ID)
		}
		msg := proposeError(t, env.deps, args)
		if !strings.Contains(msg, "cannot be") {
			t.Fatalf("%s: expected a refusal, got %q", action, msg)
		}
	}
}

func TestListRecords_ShowsOpenJobs(t *testing.T) {
	env := newProposalEnv(t)
	pump := env.equipment(t, equipmentItem{Name: "Shower pump", System: "water"})
	mustOpenJob(t, env.docs, "2026-09-01", openJobRequest{EquipmentID: &pump.ID, Title: "Leaks"})
	done := mustOpenJob(t, env.docs, "2026-09-02", openJobReq("Old news"))
	if _, err := env.docs.DropOpenJobCmd(mustParseDate(t, "2026-09-03"), done.ID, ""); err != nil {
		t.Fatalf("drop: %v", err)
	}

	raw := runMaintenanceTool(t, env.deps, "list_records", `{"type":"open_job"}`)
	var res assistantListRecordsResult
	if err := json.Unmarshal([]byte(raw), &res); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(res.Records) != 1 || res.Records[0].Fields["title"] != "Leaks" || res.Records[0].Fields["status"] != "open" {
		t.Fatalf("expected the one open job, got %+v", res.Records)
	}
}

// ── list_maintenance ────────────────────────────────────────────────────

func TestListMaintenance_ShowsOpenJobsWithTheirAge(t *testing.T) {
	deps, store := maintenanceToolDeps(t) // today is 2026-09-30
	pump := mustToolEquipment(t, store, equipmentItem{Name: "Shower pump", System: "water"})
	gen := mustToolEquipment(t, store, equipmentItem{Name: "Generator", System: "electrical"})
	mustOpenJob(t, store, "2026-09-18", openJobRequest{EquipmentID: &pump.ID, Title: "Leaks at the outlet", BeforeOn: "2026-10-15"})
	mustOpenJob(t, store, "2026-09-20", openJobRequest{EquipmentID: &gen.ID, Title: "Exhaust smells"})
	mustOpenJob(t, store, "2026-09-21", openJobReq("Renew flares"))
	closed := mustOpenJob(t, store, "2026-09-01", openJobReq("Gone"))
	if _, err := store.DropOpenJobCmd(mustParseDate(t, "2026-09-02"), closed.ID, ""); err != nil {
		t.Fatalf("drop: %v", err)
	}

	var all assistantListMaintenanceResult
	if err := json.Unmarshal([]byte(runMaintenanceTool(t, deps, "list_maintenance", `{}`)), &all); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(all.OpenJobs) != 3 {
		t.Fatalf("expected the three open jobs, got %+v", all.OpenJobs)
	}
	first := all.OpenJobs[0]
	if first.Title != "Leaks at the outlet" || first.AgeDays != 12 || first.BeforeOn != "2026-10-15" || first.EquipmentName != "Shower pump" ||
		first.Source != openJobSourceManual || first.Link != assistantMaintenanceLink {
		t.Fatalf("unexpected first open job: %+v", first)
	}
	if len(all.Rules) != 0 || all.Total != 0 {
		t.Fatalf("open jobs are not rules and are not counted as rules: %+v", all)
	}

	var one assistantListMaintenanceResult
	if err := json.Unmarshal([]byte(runMaintenanceTool(t, deps, "list_maintenance", fmt.Sprintf(`{"equipment_id":%q}`, gen.ID))), &one); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(one.OpenJobs) != 1 || one.OpenJobs[0].Title != "Exhaust smells" {
		t.Fatalf("expected only the generator's job, got %+v", one.OpenJobs)
	}

	var cal assistantListMaintenanceResult
	if err := json.Unmarshal([]byte(runMaintenanceTool(t, deps, "list_maintenance", `{"calendar_only":true}`)), &cal); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(cal.OpenJobs) != 1 || cal.OpenJobs[0].Title != "Renew flares" {
		t.Fatalf("calendar_only keeps the jobs with no item, got %+v", cal.OpenJobs)
	}

	var due assistantListMaintenanceResult
	if err := json.Unmarshal([]byte(runMaintenanceTool(t, deps, "list_maintenance", `{"status":"overdue"}`)), &due); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(due.OpenJobs) != 0 {
		t.Fatalf("a job has no due status, so a status filter leaves them out, got %+v", due.OpenJobs)
	}
}

func TestListMaintenance_NoOpenJobsKeyIsAnEmptyList(t *testing.T) {
	deps, _ := maintenanceToolDeps(t)
	raw := runMaintenanceTool(t, deps, "list_maintenance", `{}`)
	if !strings.Contains(raw, `"open_jobs":[]`) {
		t.Fatalf("expected an explicit empty open_jobs list, got %s", raw)
	}
}
