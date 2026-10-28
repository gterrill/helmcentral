package main

import (
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
)

// Routines as a registered record type (ADR 0227). Mate proposes a routine,
// the operator taps Apply, and the scheduler (mate_routines.go) runs it.
//
// A routine's conversation is created in the changeset's own transaction: the
// conversations table lives in the same database file as the record store, so
// the routine and its conversation are saved together or not at all. If the
// conversations table is missing the create fails with that reason; there is
// no creating it later.

const (
	recordTypeRoutine = "routine"

	maxRoutineNameChars  = 60
	maxRoutineFocusChars = 500
)

func init() {
	defaultRecordRegistry.register(routineRecordType())
}

func routineRecordType() *recordType {
	return &recordType{
		Name:  recordTypeRoutine,
		Label: "routine",
		Summary: "A standing instruction Mate carries out on a schedule, such as checking the boat every hour or a summary each morning. " +
			"Each run is one Mate turn in the routine's own conversation, where the operator reads what it found. " +
			"Routines run only while \"Let routines run on their own\" is on in Settings, Mate, and stop for the day at the spending limit set there. " +
			"cadence is hourly or daily; a daily routine gives daily_time as HH:MM on the boat's clock. At most 8 routines can exist. " +
			"A routine can read the boat but never change anything, and it does not replace an alarm.",
		Actions: []string{changeCreate, changeUpdate, changeDelete},
		Filters: map[string]string{"enabled": "true or false"},
		Fields: []recordField{
			{Name: "name", Kind: kindString, Writable: true, Description: "a short name for the routine, up to 60 characters, such as \"Morning summary\""},
			{Name: "cadence", Kind: kindString, Writable: true, Enum: []string{routineHourly, routineDaily}, Description: "hourly runs about every hour; daily runs once a day at daily_time"},
			{Name: "daily_time", Kind: kindString, Writable: true, Nullable: true, Description: "HH:MM on the boat's clock, 00:00 to 23:59; required for a daily routine and empty for an hourly one"},
			{Name: "focus", Kind: kindString, Writable: true, Description: "what to look at, in the operator's words, up to 500 characters"},
			{Name: "enabled", Kind: kindBoolean, Writable: true, Description: "whether the routine runs"},
			{Name: "last_run", Kind: kindString, Description: "when it last ran or was skipped"},
			{Name: "last_note", Kind: kindString, Description: "the first line of what it found, or why it was skipped"},
		},
		Get:      getRoutineRecord,
		List:     listRoutineRecords,
		Create:   createRoutineRecord,
		Update:   updateRoutineRecord,
		Delete:   deleteRoutineRecord,
		Describe: describeRoutine,
		Display:  displayRoutineFields,
		Href:     func(r recordSnapshot) string { return "/mate" },
	}
}

func mateRoutineVersion(r mateRoutine) string { return r.UpdatedAt.UTC().Format(time.RFC3339Nano) }

// mateRoutineSchedule is the schedule in an operator's words.
func mateRoutineSchedule(cadence, dailyTime string) string {
	if cadence == routineDaily {
		return "Daily at " + dailyTime + " (boat time)"
	}
	return "Every hour"
}

func routineSnapshot(r mateRoutine) recordSnapshot {
	var daily any
	if r.DailyTime != "" {
		daily = r.DailyTime
	}
	lastRun := ""
	if r.LastRunAt != nil {
		lastRun = r.LastRunAt.UTC().Format(time.RFC3339)
	}
	return recordSnapshot{
		ID: r.ID, Label: r.Name, Version: mateRoutineVersion(r),
		Fields: map[string]any{
			"name": r.Name, "cadence": r.Cadence, "daily_time": daily, "focus": r.Focus, "enabled": r.Enabled,
			"last_run": lastRun, "last_note": r.LastNote,
		},
	}
}

func getRoutineRecord(q sqlQueryer, id string) (recordSnapshot, error) {
	r, err := mateRoutineByIDQ(q, id)
	if errors.Is(err, errMateRoutineNotFound) {
		return recordSnapshot{}, errRecordNotFound
	}
	if err != nil {
		return recordSnapshot{}, err
	}
	return routineSnapshot(r), nil
}

func listRoutineRecords(s *documentStore, f map[string]string) ([]recordSnapshot, error) {
	var routines []mateRoutine
	if err := s.Read(func(q sqlQueryer) error {
		var err error
		routines, err = listMateRoutinesQ(q)
		return err
	}); err != nil {
		return nil, err
	}
	var out []recordSnapshot
	for _, r := range routines {
		if e := f["enabled"]; e != "" && e != fmt.Sprint(r.Enabled) {
			continue
		}
		out = append(out, routineSnapshot(r))
	}
	return out, nil
}

// validateMateRoutine is the one validator both the proposal path and the
// REST path use.
func validateMateRoutine(r *mateRoutine) error {
	r.Name = strings.TrimSpace(r.Name)
	r.Focus = strings.TrimSpace(r.Focus)
	r.DailyTime = strings.TrimSpace(r.DailyTime)
	if r.Name == "" {
		return fieldErr("name", "a routine needs a name")
	}
	if utf8.RuneCountInString(r.Name) > maxRoutineNameChars {
		return fieldErr("name", "must be %d characters or fewer", maxRoutineNameChars)
	}
	if r.Focus == "" {
		return fieldErr("focus", "say what the routine should look at")
	}
	if utf8.RuneCountInString(r.Focus) > maxRoutineFocusChars {
		return fieldErr("focus", "must be %d characters or fewer", maxRoutineFocusChars)
	}
	switch r.Cadence {
	case routineHourly:
		if r.DailyTime != "" {
			return fieldErr("daily_time", "an hourly routine has no time of day; leave it empty")
		}
	case routineDaily:
		t, err := time.Parse("15:04", r.DailyTime)
		if err != nil || t.Format("15:04") != r.DailyTime {
			return fieldErr("daily_time", "a daily routine needs a time as HH:MM between 00:00 and 23:59")
		}
	default:
		return fieldErr("cadence", "must be hourly or daily")
	}
	return nil
}

func applyRoutineFields(r mateRoutine, given fieldSet) mateRoutine {
	if v, ok := given["name"]; ok {
		r.Name = stringOf(v)
	}
	if v, ok := given["cadence"]; ok {
		r.Cadence = stringOf(v)
		// Moving to hourly drops the time unless it is given again.
		if r.Cadence == routineHourly {
			if _, ok := given["daily_time"]; !ok {
				r.DailyTime = ""
			}
		}
	}
	if v, ok := given["daily_time"]; ok {
		r.DailyTime = stringOf(v)
	}
	if v, ok := given["focus"]; ok {
		r.Focus = stringOf(v)
	}
	if v, ok := given["enabled"]; ok {
		r.Enabled, _ = v.(bool)
	}
	return r
}

func createRoutineRecord(env changeEnv, given fieldSet) (createdRecord, error) {
	for _, n := range []string{"name", "cadence", "focus"} {
		if _, ok := given[n]; !ok {
			return createdRecord{}, fieldErr(n, "is required")
		}
	}
	r := applyRoutineFields(mateRoutine{Enabled: true}, given)
	if err := validateMateRoutine(&r); err != nil {
		return createdRecord{}, err
	}
	n, err := countMateRoutinesQ(env.tx)
	if err != nil {
		return createdRecord{}, err
	}
	if n >= maxMateRoutines {
		return createdRecord{}, fieldErr("name", "the boat already has %d routines, which is the most there can be; delete one first", maxMateRoutines)
	}
	r.ID, r.ConversationID = uuid.NewString(), uuid.NewString()
	r.CreatedAt, r.UpdatedAt, r.ScheduleChangedAt = env.now, env.now, env.now
	if _, err := env.tx.Exec(`INSERT INTO conversations (id, title, created_at, updated_at, routine_id) VALUES (?, ?, ?, ?, ?)`,
		r.ConversationID, r.Name, env.now.Unix(), env.now.Unix(), r.ID); err != nil {
		return createdRecord{}, fmt.Errorf("create the routine's conversation: %w", err)
	}
	if err := insertMateRoutineTx(env.tx, r); err != nil {
		return createdRecord{}, err
	}
	return createdRecord{ID: r.ID}, nil
}

func updateRoutineRecord(env changeEnv, before recordSnapshot, given fieldSet) error {
	r, err := mateRoutineByIDQ(env.tx, before.ID)
	if err != nil {
		return err
	}
	next := applyRoutineFields(r, given)
	if err := validateMateRoutine(&next); err != nil {
		return err
	}
	next.UpdatedAt = env.now
	if next.Cadence != r.Cadence || next.DailyTime != r.DailyTime {
		next.ScheduleChangedAt = env.now
	}
	if err := updateMateRoutineTx(env.tx, next); err != nil {
		return err
	}
	if next.Name != r.Name {
		if _, err := env.tx.Exec(`UPDATE conversations SET title = ? WHERE id = ?`, next.Name, r.ConversationID); err != nil {
			return fmt.Errorf("rename the routine's conversation: %w", err)
		}
	}
	return nil
}

func deleteRoutineRecord(env changeEnv, before recordSnapshot) error {
	return deleteMateRoutineTx(env.tx, before.ID)
}

func describeRoutine(d describeInput) string {
	schedule := func(f map[string]any) string {
		return strings.ToLower(mateRoutineSchedule(stringOf(f["cadence"]), stringOf(f["daily_time"])))
	}
	switch d.Op.Action {
	case changeCreate:
		return "Add routine " + d.After.Label + ": " + schedule(d.After.Fields) + ", looking at " + stringOf(d.After.Fields["focus"])
	case changeDelete:
		return "Remove routine " + d.Before.Label
	}
	given := d.Op.Fields
	if len(given) == 1 {
		if on, ok := given["enabled"].(bool); ok {
			if on {
				return "Switch on routine " + d.Before.Label
			}
			return "Switch off routine " + d.Before.Label
		}
	}
	return "Change routine " + d.Before.Label + ": now " + schedule(d.After.Fields) + ", looking at " + stringOf(d.After.Fields["focus"])
}

// displayRoutineFields is how the card shows a routine: the schedule in words,
// and the scheduler's own bookkeeping left off.
func displayRoutineFields(snap recordSnapshot, fields map[string]any) map[string]any {
	out := make(map[string]any, len(fields))
	for k, v := range fields {
		switch k {
		case "last_run", "last_note":
			continue
		case "cadence":
			if stringOf(v) == routineHourly {
				v = "Every hour"
			} else {
				v = "Daily"
			}
		case "daily_time":
			if s := stringOf(v); s != "" {
				v = s + " (boat time)"
			}
		}
		out[k] = v
	}
	return out
}
