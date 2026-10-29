package main

import (
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/google/uuid"
)

// Watches as a registered record type (ADR 0227). Mate proposes a watch, the
// operator taps Apply, and the scheduler (watches_scheduler.go) runs it. The
// Watches page and its REST routes create and change watches through the same
// validation, so a watch means the same thing whoever made it.

const recordTypeWatch = "watch"

func init() {
	defaultRecordRegistry.register(watchRecordType())
}

func watchRecordType() *recordType {
	return &recordType{
		Name:  recordTypeWatch,
		Label: "watch",
		Summary: "A regular check of the boat on a schedule. Each run builds a report of the sections chosen, applies the " +
			"operator's thresholds to reach findings, and has Mate review it. Findings and Mate's review appear on the Watches page. " +
			"schedule is a five-field cron string on the boat's clock (minute hour day-of-month month day-of-week), for example " +
			"\"0 6 * * *\" for every day at 06:00, \"0 * * * *\" for every hour, \"0 */2 * * *\" for every 2 hours, " +
			"\"0 22-23,0-6 * * *\" for every hour from 22:00 to 06:00, \"30 7 * * 1,3,5\" for Monday, Wednesday and Friday at 07:30; " +
			"no more often than every 15 minutes. sections are the parts of the report: power, anchor, alarms, tanks, weather, instruments. " +
			"The thresholds are optional and default to: any alarm raised, any sensor frozen or silent, a tank changed by 10 points, " +
			"pressure down 3 hPa in 3 hours. At most 20 scheduled watches can exist. A watch reads the boat and never changes anything, " +
			"and it does not replace an alarm.",
		Actions: []string{changeCreate, changeUpdate, changeDelete},
		Filters: map[string]string{"enabled": "true or false"},
		Fields: []recordField{
			{Name: "name", Kind: kindString, Writable: true, Description: "a short name, up to 60 characters, such as \"Morning report\""},
			{Name: "schedule", Kind: kindString, Writable: true, Description: "five-field cron on the boat's clock, such as \"0 6 * * *\""},
			{Name: "sections", Kind: kindStringList, Writable: true, Description: "which parts of the report to build: power, anchor, alarms, tanks, weather, instruments"},
			{Name: "look_out_for", Kind: kindString, Writable: true, Description: "what to look out for, in the operator's words, up to 500 characters; Mate reads it with each report"},
			{Name: "enabled", Kind: kindBoolean, Writable: true, Description: "whether the watch runs"},
			{Name: "alarms_raised", Kind: kindInteger, Writable: true, Description: "alarms raised since the last run that make a finding (default 1)"},
			{Name: "instrument_issues", Kind: kindInteger, Writable: true, Description: "frozen, impossible or silent sensors that make a finding (default 1)"},
			{Name: "tank_change_points", Kind: kindNumber, Writable: true, Description: "change in a tank's level, in percentage points since the last run, that makes a finding (default 10)"},
			{Name: "pressure_fall_hpa", Kind: kindNumber, Writable: true, Description: "fall in pressure over 3 hours, in hPa, that makes a finding (default 3)"},
			{Name: "notify", Kind: kindStringList, Writable: true, Description: "where a report is sent: any of ntfy, smtp, webhook, webpush (SignalK is never offered); left out on a new watch it starts as every channel the boat has switched on; an empty list sends nothing"},
			{Name: "send", Kind: kindString, Writable: true, Description: "\"findings\" to send a report only when it has a finding the last report sent did not (the default), or \"every\" to send every report"},
			{Name: "schedule_sentence", Kind: kindString, Description: "the schedule in words"},
			{Name: "last_run", Kind: kindString, Description: "when it last ran"},
		},
		Get:      getWatchRecord,
		List:     listWatchRecords,
		Create:   createWatchRecord,
		Update:   updateWatchRecord,
		Delete:   deleteWatchRecord,
		Describe: describeWatch,
		Display:  displayWatchFields,
		Href:     func(recordSnapshot) string { return "/watches" },
	}
}

// newSavedWatchDefaults is what a new watch starts with: on, the default
// thresholds, and a report sent only for a new finding to every channel the
// boat has switched on (never SignalK).
func newSavedWatchDefaults() savedWatch {
	return savedWatch{Enabled: true, Thresholds: defaultWatchThresholds(), Notify: defaultWatchNotify(getAlarmTransports()), Send: watchSendFindings}
}

func savedWatchVersion(w savedWatch) string { return w.UpdatedAt.UTC().Format(time.RFC3339Nano) }

func watchSentenceOrCron(w savedWatch) string {
	if s, err := watchScheduleSentence(w.Schedule); err == nil {
		return s
	}
	return "Custom: " + w.Schedule
}

func watchSnapshot(w savedWatch) recordSnapshot {
	lastRun := ""
	if w.LastRunAt != nil {
		lastRun = w.LastRunAt.UTC().Format(time.RFC3339)
	}
	return recordSnapshot{
		ID: w.ID, Label: w.Name, Version: savedWatchVersion(w),
		Fields: map[string]any{
			"name": w.Name, "schedule": w.Schedule, "sections": w.Sections, "look_out_for": w.LookOutFor, "enabled": w.Enabled,
			"notify": w.Notify, "send": w.Send,
			"alarms_raised": w.Thresholds.AlarmsRaised, "instrument_issues": w.Thresholds.InstrumentIssues,
			"tank_change_points": w.Thresholds.TankChangePoints, "pressure_fall_hpa": w.Thresholds.PressureFallHPa,
			"schedule_sentence": watchSentenceOrCron(w), "last_run": lastRun,
		},
	}
}

func getWatchRecord(q sqlQueryer, id string) (recordSnapshot, error) {
	w, err := savedWatchByIDQ(q, id)
	if errors.Is(err, errWatchNotFound) {
		return recordSnapshot{}, errRecordNotFound
	}
	if err != nil {
		return recordSnapshot{}, err
	}
	if w.Kind != watchKindScheduled {
		return recordSnapshot{}, errRecordNotFound
	}
	return watchSnapshot(w), nil
}

func listWatchRecords(s *documentStore, f map[string]string) ([]recordSnapshot, error) {
	var watches []savedWatch
	if err := s.Read(func(q sqlQueryer) error {
		var err error
		watches, err = listSavedWatchesQ(q)
		return err
	}); err != nil {
		return nil, err
	}
	var out []recordSnapshot
	for _, w := range watches {
		if w.Kind != watchKindScheduled {
			continue
		}
		if e := f["enabled"]; e != "" && e != fmt.Sprint(w.Enabled) {
			continue
		}
		out = append(out, watchSnapshot(w))
	}
	return out, nil
}

// normalizeWatchSections checks the sections and puts them in report order.
func normalizeWatchSections(in []string) ([]string, error) {
	if len(in) == 0 {
		return nil, fieldErr("sections", "choose at least one of %s", strings.Join(watchSectionOrder, ", "))
	}
	want := map[string]bool{}
	for _, s := range in {
		s = strings.TrimSpace(s)
		if !containsString(watchSectionOrder, s) {
			return nil, fieldErr("sections", "%q is not a report section; the sections are %s", s, strings.Join(watchSectionOrder, ", "))
		}
		want[s] = true
	}
	var out []string
	for _, s := range watchSectionOrder {
		if want[s] {
			out = append(out, s)
		}
	}
	return out, nil
}

// validateSavedWatch is the one validator the proposal path and the REST path
// share. It trims text and orders the sections. A threshold left out of a
// create takes its default, and one left out of a change keeps its stored
// value; one sent as zero is refused, never defaulted.
func validateSavedWatch(w *savedWatch) error {
	w.Name = strings.TrimSpace(w.Name)
	w.LookOutFor = strings.TrimSpace(w.LookOutFor)
	w.Schedule = strings.Join(strings.Fields(w.Schedule), " ")
	if w.Name == "" {
		return fieldErr("name", "a watch needs a name")
	}
	if strings.IndexFunc(w.Name, unicode.IsControl) >= 0 {
		return fieldErr("name", "can't contain control characters such as line breaks or tabs")
	}
	if utf8.RuneCountInString(w.Name) > maxWatchNameChars {
		return fieldErr("name", "must be %d characters or fewer", maxWatchNameChars)
	}
	if utf8.RuneCountInString(w.LookOutFor) > maxWatchLookOutForChars {
		return fieldErr("look_out_for", "must be %d characters or fewer", maxWatchLookOutForChars)
	}
	if err := validateWatchSchedule(w.Schedule); err != nil {
		return fieldErr("schedule", "%s", err.Error())
	}
	sections, err := normalizeWatchSections(w.Sections)
	if err != nil {
		return err
	}
	w.Sections = sections
	if w.Notify, err = normalizeWatchNotify(w.Notify); err != nil {
		return err
	}
	if err := validateWatchSend(w.Send); err != nil {
		return err
	}
	return w.Thresholds.validate()
}

// watchFieldList reads a string_list field.
func watchFieldList(v any) []string {
	switch l := v.(type) {
	case []string:
		return l
	case []any:
		out := make([]string, 0, len(l))
		for _, x := range l {
			out = append(out, stringOf(x))
		}
		return out
	}
	return nil
}

func applyWatchFields(w savedWatch, given fieldSet) savedWatch {
	if v, ok := given["name"]; ok {
		w.Name = stringOf(v)
	}
	if v, ok := given["schedule"]; ok {
		w.Schedule = stringOf(v)
	}
	if v, ok := given["sections"]; ok {
		w.Sections = watchFieldList(v)
	}
	if v, ok := given["look_out_for"]; ok {
		w.LookOutFor = stringOf(v)
	}
	if v, ok := given["enabled"]; ok {
		w.Enabled, _ = v.(bool)
	}
	if v, ok := given["notify"]; ok {
		w.Notify = watchFieldList(v)
	}
	if v, ok := given["send"]; ok {
		w.Send = stringOf(v)
	}
	if v := intPtrOf(given["alarms_raised"]); v != nil {
		w.Thresholds.AlarmsRaised = *v
	}
	if v := intPtrOf(given["instrument_issues"]); v != nil {
		w.Thresholds.InstrumentIssues = *v
	}
	if v := floatPtrOf(given["tank_change_points"]); v != nil {
		w.Thresholds.TankChangePoints = *v
	}
	if v := floatPtrOf(given["pressure_fall_hpa"]); v != nil {
		w.Thresholds.PressureFallHPa = *v
	}
	return w
}

// createSavedWatchTx makes a scheduled watch inside a transaction. The REST
// route and the record type both call it.
func createSavedWatchTx(x alarmSQLExecer, w savedWatch, now time.Time) (savedWatch, error) {
	w.Kind = watchKindScheduled
	if w.Send == "" {
		w.Send = watchSendFindings
	}
	if err := validateSavedWatch(&w); err != nil {
		return savedWatch{}, err
	}
	n, err := countScheduledWatchesQ(x)
	if err != nil {
		return savedWatch{}, err
	}
	if n >= maxScheduledWatches {
		return savedWatch{}, fieldErr("name", "the boat already has %d watches, which is the most there can be; delete one first", maxScheduledWatches)
	}
	w.ID = uuid.NewString()
	w.CreatedAt, w.UpdatedAt, w.ScheduleChangedAt = now, now, now
	if err := insertSavedWatchTx(x, w); err != nil {
		return savedWatch{}, err
	}
	return w, nil
}

// updateSavedWatchFromTx applies a change to a stored watch. A new schedule,
// or switching the watch back on, restarts its clock.
func updateSavedWatchFromTx(x alarmSQLExecer, before savedWatch, next savedWatch, now time.Time) (savedWatch, error) {
	if before.Kind != watchKindScheduled {
		return savedWatch{}, errWatchNotFound
	}
	if err := validateSavedWatch(&next); err != nil {
		return savedWatch{}, err
	}
	next.UpdatedAt = now
	// A new schedule, or a watch switched back on, runs at its next cron
	// instant, never at one that passed while it was changed or off.
	if next.Schedule != before.Schedule || (next.Enabled && !before.Enabled) {
		next.ScheduleChangedAt = now
	}
	if err := updateSavedWatchTx(x, next); err != nil {
		return savedWatch{}, err
	}
	return next, nil
}

func createWatchRecord(env changeEnv, given fieldSet) (createdRecord, error) {
	for _, n := range []string{"name", "schedule", "sections"} {
		if _, ok := given[n]; !ok {
			return createdRecord{}, fieldErr(n, "is required")
		}
	}
	w := applyWatchFields(newSavedWatchDefaults(), given)
	created, err := createSavedWatchTx(env.tx, w, env.now)
	if err != nil {
		return createdRecord{}, err
	}
	return createdRecord{ID: created.ID}, nil
}

func updateWatchRecord(env changeEnv, before recordSnapshot, given fieldSet) error {
	w, err := savedWatchByIDQ(env.tx, before.ID)
	if err != nil {
		return err
	}
	_, err = updateSavedWatchFromTx(env.tx, w, applyWatchFields(w, given), env.now)
	return err
}

func deleteWatchRecord(env changeEnv, before recordSnapshot) error {
	return deleteSavedWatchTx(env.tx, before.ID)
}

func describeWatch(d describeInput) string {
	schedule := func(f map[string]any) string {
		return strings.ToLower(stringOf(f["schedule_sentence"]))
	}
	sections := func(f map[string]any) string { return watchJoinAnd(watchFieldList(f["sections"])) }
	switch d.Op.Action {
	case changeCreate:
		return "Add watch " + d.After.Label + ": " + schedule(d.After.Fields) + ", covering " + sections(d.After.Fields)
	case changeDelete:
		return "Remove watch " + d.Before.Label
	}
	given := d.Op.Fields
	if len(given) == 1 {
		if on, ok := given["enabled"].(bool); ok {
			if on {
				return "Switch on watch " + d.Before.Label
			}
			return "Switch off watch " + d.Before.Label
		}
	}
	return "Change watch " + d.Before.Label + ": now " + schedule(d.After.Fields) + ", covering " + sections(d.After.Fields)
}

// displayWatchFields is how the card shows a watch: the schedule in words, and
// the scheduler's own bookkeeping left off.
func displayWatchFields(snap recordSnapshot, fields map[string]any) map[string]any {
	out := make(map[string]any, len(fields))
	for k, v := range fields {
		switch k {
		case "last_run", "schedule_sentence":
			continue
		case "schedule":
			if s := stringOf(snap.Fields["schedule_sentence"]); s != "" {
				v = s
			}
		}
		out[k] = v
	}
	return out
}
