package main

import (
	"errors"
	"time"
)

// Open jobs as a registered record type (ADR 0269, on the registry of ADR
// 0158). Mate can read them and propose adding one; the operator taps Apply.
// Closing is deliberately not registered. Done writes a line to the service
// log and Dropped ends an entry on the operator's own list, so both stay with
// the operator's tap, the way deleting a rule or a log entry does.
//
// Where a job came from is not a field Mate gives: a job it adds is marked as
// Mate's, and a link back is only ever made by the screen the operator
// pressed "Add to jobs" on.

const recordTypeOpenJob = "open_job"

func init() {
	defaultRecordRegistry.register(openJobRecordType())
}

func openJobRecordType() *recordType {
	return &recordType{
		Name:  recordTypeOpenJob,
		Label: "open job",
		Summary: "A known problem that is not fixed yet, on the operator's Open jobs list (Inventory, Maintenance), such as \"shower pump leaks at the outlet\". " +
			"It is not a maintenance rule: a rule repeats, a job is done once. It is not an alarm. " +
			"You can add one; closing or dropping a job is the operator's own tap.",
		Actions: []string{changeCreate},
		Filters: map[string]string{
			"equipment_id": "only this item's jobs",
			"status":       "open (the default), done, dropped or any",
		},
		Fields: []recordField{
			{Name: "equipment_id", Kind: kindID, Writable: true, Nullable: true, Ref: "equipment", Description: "the item the problem is on; leave out when it is not one item"},
			{Name: "title", Kind: kindString, Writable: true, Description: "the problem in a few words, e.g. \"Leaks at the outlet\""},
			{Name: "notes", Kind: kindString, Writable: true, Description: "anything else worth knowing"},
			{Name: "before_on", Kind: kindDate, Writable: true, Nullable: true, Description: "the date the operator wants it fixed before, YYYY-MM-DD; leave out for none"},
			{Name: "opened_on", Kind: kindDate, Description: "the date it was added"},
			{Name: "status", Kind: kindString, Enum: []string{openJobOpen, openJobDone, openJobDropped}, Description: "open, done or dropped"},
			{Name: "source", Kind: kindString, Description: "where it came from: manual, mate, watch_finding, alarm or checklist_line"},
		},
		Get:      getOpenJobRecord,
		List:     listOpenJobRecords,
		Create:   createOpenJobRecord,
		Describe: describeOpenJob,
		Href:     func(recordSnapshot) string { return "/inventory/maintenance" },
	}
}

func openJobSnapshot(j openJob) recordSnapshot {
	var equipmentID any
	if j.EquipmentID != nil {
		equipmentID = *j.EquipmentID
	}
	return recordSnapshot{
		ID:      j.ID,
		Label:   proposalRuleLabel(j.EquipmentName, j.Title),
		Version: j.UpdatedAt.UTC().Format(time.RFC3339),
		Fields: map[string]any{
			"equipment_id": equipmentID,
			"title":        j.Title,
			"notes":        j.Notes,
			"before_on":    nilIfBlank(j.BeforeOn),
			"opened_on":    j.OpenedOn,
			"status":       j.Status,
			"source":       j.Source,
		},
	}
}

func getOpenJobRecord(q sqlQueryer, id string) (recordSnapshot, error) {
	j, err := openJobByID(q, id)
	if errors.Is(err, errOpenJobNotFound) {
		return recordSnapshot{}, errRecordNotFound
	}
	if err != nil {
		return recordSnapshot{}, err
	}
	return openJobSnapshot(j), nil
}

func listOpenJobRecords(s *documentStore, filter map[string]string) ([]recordSnapshot, error) {
	status := filter["status"]
	switch status {
	case "", openJobOpen, openJobDone, openJobDropped, openJobStatusAny:
	default:
		return nil, fieldErr("status", "must be open, done, dropped or any, got %q", status)
	}
	jobs, err := s.ListOpenJobs(openJobFilter{EquipmentID: filter["equipment_id"], Status: status, IncludeStored: true})
	if err != nil {
		return nil, err
	}
	out := make([]recordSnapshot, len(jobs))
	for i, j := range jobs {
		out[i] = openJobSnapshot(j)
	}
	return out, nil
}

func createOpenJobRecord(env changeEnv, fields fieldSet) (createdRecord, error) {
	req := openJobRequest{
		Title:    stringOf(fields["title"]),
		Notes:    stringOf(fields["notes"]),
		BeforeOn: stringOf(fields["before_on"]),
		Source:   openJobSourceMate,
	}
	if id := stringOf(fields["equipment_id"]); id != "" {
		req.EquipmentID = &id
	}
	job, err := cmdCreateOpenJob(env.tx, env.now, env.today, req)
	if err != nil {
		if errors.Is(err, errEquipmentNotFound) {
			return createdRecord{}, fieldErr("equipment_id", "no equipment with id %q (find it with find_equipment)", *req.EquipmentID)
		}
		return createdRecord{}, err
	}
	return createdRecord{ID: job.ID}, nil
}

func describeOpenJob(d describeInput) string {
	summary := "Add to open jobs: " + d.After.Label
	if before := stringOf(d.Op.Fields["before_on"]); before != "" {
		summary += ", before " + formatProposalDate(before)
	}
	return summary
}
