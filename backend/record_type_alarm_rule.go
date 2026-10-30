package main

import (
	"errors"
	"fmt"
	"log"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
)

// Alarm rules as a registered record type (ADR 0225). Mate proposes a rule,
// the operator taps Apply, and the rule is written by the same store commands
// and checked by the same validator the Alarms page uses (alarm_rules.go).
//
// Rules are stored in SI, which an operator never reads, so the type's value
// and clear_value fields are in the operator's own unit for the rule's path
// (percent for a ratio, degrees Celsius for kelvin, knots for metres per
// second; operatorUnitTable in alarm_units.go). The card, the description
// and list_alarm_rules all say the unit. Rules Helmcentral derives (a tank's
// levels, a gauge zone) are not records and are refused with the place to
// change them.

const recordTypeAlarmRule = "alarm_rule"

// alarmRuleUnitFor is the SI unit the boat declares for a path, "" when it
// has never declared one. Tests replace it.
var alarmRuleUnitFor = func(path string) string { return unitForAlarmPath(globalSignalKSnapshot, path) }

func init() {
	defaultRecordRegistry.register(alarmRuleRecordType())
}

// alarmDisplay converts between a path's SI value and the unit the operator
// reads. Every entry in operatorUnitTable is affine, so the inverse comes from
// two points of the forward function.
type alarmDisplay struct {
	label    string
	toShown  func(float64) float64
	toSI     func(float64) float64
	decimals int
}

// alarmUnitFor is the SI unit a rule's numbers are in: a rule that follows the
// house bank is a ratio whatever the boat has declared.
func alarmUnitFor(path, role string) string {
	if role == alarmPathRoleHouseBankSoC {
		return "ratio"
	}
	return alarmRuleUnitFor(path)
}

// alarmDisplayFor is the conversion for a rule's path. known is false when the
// boat has declared no unit for it: the numbers then stay as stored, and a
// proposal may not set one, since nothing says what the operator's number means.
func alarmDisplayFor(path, role string) (d alarmDisplay, known bool) {
	label, conv, decimals, ok := operatorUnit(alarmUnitFor(path, role))
	if !ok {
		same := func(v float64) float64 { return v }
		return alarmDisplay{toShown: same, toSI: same, decimals: 3}, false
	}
	zero, slope := conv(0), conv(1)-conv(0)
	return alarmDisplay{
		label:    label,
		toShown:  func(v float64) float64 { return roundTo(conv(v), 3) },
		toSI:     func(v float64) float64 { return (v - zero) / slope },
		decimals: decimals,
	}, true
}

// text renders a value already in the shown unit.
func (d alarmDisplay) text(v float64) string {
	s := strconv.FormatFloat(v, 'f', -1, 64)
	if d.label == "" {
		return s
	}
	return s + " " + d.label
}

func alarmRuleRecordType() *recordType {
	return &recordType{
		Name:  recordTypeAlarmRule,
		Label: "alarm",
		Summary: "A threshold alarm on a SignalK path: the rule the Alarms page lists, stored in the database. " +
			"value and clear_value are in the operator's unit for the path (percent for a battery or tank level, degrees Celsius for a temperature, knots for a speed, and so on); the unit field says which. " +
			"clear_value is where the alarm clears again, so a rule that falls below 50% might clear above 55%; leave it out and it clears as soon as the value crosses back. " +
			"The path of an existing alarm cannot change: delete it and add a new one. Alarms that come from a tank's levels or a gauge zone are not records; " +
			"change a tank's levels on its equipment record (tank_warn_percent, tank_alarm_percent, tank_alarms_off). " +
			"Rules marked built_in came with Helmcentral; they can be edited, switched off or deleted, and a deleted one does not come back.",
		Actions: []string{changeCreate, changeUpdate, changeDelete},
		Filters: map[string]string{"query": "label or path contains this", "enabled": "true or false"},
		Fields: []recordField{
			{Name: "label", Kind: kindString, Writable: true, Description: "the alarm's title as the watchkeeper reads it, a few words such as \"House Bank Low\", never a path"},
			{Name: "path", Kind: kindString, Writable: true, Description: "the SignalK path it watches; fixed once the alarm exists"},
			{Name: "op", Kind: kindString, Writable: true, Enum: []string{alarmOpAbove, alarmOpBelow, alarmOpEqual, alarmOpNotEqual, alarmOpStale}, Description: "above or below the value; stale fires when the path goes silent"},
			{Name: "value", Kind: kindNumber, Writable: true, Description: "the threshold in the operator's unit for the path (see unit); not used by a stale alarm"},
			{Name: "clear_value", Kind: kindNumber, Writable: true, Nullable: true, Description: "where it clears, in the same unit; null clears as soon as the value crosses back"},
			{Name: "state", Kind: kindString, Writable: true, Enum: []string{alarmStateAlert, alarmStateWarn, alarmStateAlarm, alarmStateEmergency}, Description: "how serious: alert, warn, alarm or emergency"},
			{Name: "dwell_seconds", Kind: kindInteger, Writable: true, Description: "how long the condition must hold before it fires"},
			{Name: "stale_after_seconds", Kind: kindInteger, Writable: true, Description: "for a stale alarm, how many seconds of silence fire it"},
			{Name: "enabled", Kind: kindBoolean, Writable: true, Description: "whether the alarm is watching"},
			{Name: "mate_reviews", Kind: kindBoolean, Writable: true, Description: "whether Mate reviews the alarm when it goes off and sends the review to the alarm's channels; the operator's switch, off unless they ask for it"},
			{Name: "unit", Kind: kindString, Description: "the unit value and clear_value are in; empty when the boat has not declared one for the path"},
			{Name: "built_in", Kind: kindBoolean, Description: "true for a rule that came with Helmcentral"},
			{Name: "path_role", Kind: kindString, Description: "house_bank_soc when the rule follows the house bank"},
		},
		Get:             getAlarmRuleRecord,
		List:            listAlarmRuleRecords,
		Create:          createAlarmRuleRecord,
		Update:          updateAlarmRuleRecord,
		Delete:          deleteAlarmRuleRecord,
		Describe:        describeAlarmRule,
		Display:         displayAlarmRuleFields,
		Href:            func(recordSnapshot) string { return "/alarms" },
		Pin:             pinAlarmRuleUnit,
		AfterCommitOnce: true,
		AfterCommit: func(res changeOpResult) error {
			if res.Type != recordTypeAlarmRule {
				return nil
			}
			store, err := alarmRuleStore()
			if err == nil {
				err = refreshAlarmRules(store)
			}
			if err != nil {
				// The rule is committed; only the running copy is behind.
				log.Printf("alarm rules: applied a proposal but could not reload the running rules: %v", err)
				return fmt.Errorf("%w (%v)", errAlarmRulesNotReloaded, err)
			}
			return nil
		},
	}
}

func alarmRuleVersion(r alarmRule) string { return r.UpdatedAt.UTC().Format(time.RFC3339Nano) }

func alarmRuleSnapshot(r alarmRule) recordSnapshot {
	d, _ := alarmDisplayFor(r.Path, r.PathRole)
	var clear any
	if c := alarmClearValueFor(r); c != nil {
		clear = d.toShown(*c)
	}
	return recordSnapshot{
		ID: r.ID, Label: r.Label, Version: alarmRuleVersion(r),
		Fields: map[string]any{
			"label": r.Label, "path": r.Path, "op": r.Op, "value": d.toShown(r.Value), "clear_value": clear,
			"state": r.State, "dwell_seconds": r.DwellSeconds, "stale_after_seconds": r.StaleAfterSeconds,
			"enabled": r.Enabled, "mate_reviews": r.MateReviews, "unit": d.label, "built_in": r.SeededBy != "", "path_role": r.PathRole,
		},
	}
}

// errDerivedAlarmRule says why a derived rule is not a record.
func derivedAlarmRuleReason(id string) error {
	switch {
	case isTankDerivedAlarmRuleID(id):
		return errors.New("this alarm comes from a tank's levels, so it is not a rule to edit; change tank_warn_percent, tank_alarm_percent or tank_alarms_off on the tank's equipment record instead")
	case isZoneDerivedAlarmRuleID(id):
		return errors.New("this alarm comes from a gauge zone, so it is not a rule to edit; the operator changes the zone on the gauge")
	}
	return nil
}

func getAlarmRuleRecord(q sqlQueryer, id string) (recordSnapshot, error) {
	if err := derivedAlarmRuleReason(id); err != nil {
		return recordSnapshot{}, err
	}
	r, err := alarmRuleByIDQ(q, id)
	if errors.Is(err, errAlarmRuleNotFound) {
		return recordSnapshot{}, errRecordNotFound
	}
	if err != nil {
		return recordSnapshot{}, err
	}
	return alarmRuleSnapshot(r), nil
}

func listAlarmRuleRecords(s *documentStore, f map[string]string) ([]recordSnapshot, error) {
	var rules []alarmRule
	if err := s.Read(func(q sqlQueryer) error {
		var err error
		rules, err = listAlarmRulesQ(q)
		return err
	}); err != nil {
		return nil, err
	}
	query := strings.ToLower(strings.TrimSpace(f["query"]))
	var out []recordSnapshot
	for _, r := range rules {
		if query != "" && !strings.Contains(strings.ToLower(r.Label), query) && !strings.Contains(strings.ToLower(r.Path), query) {
			continue
		}
		if e := f["enabled"]; e != "" && e != strconv.FormatBool(r.Enabled) {
			continue
		}
		out = append(out, alarmRuleSnapshot(r))
	}
	return out, nil
}

// alarmRuleFieldError names the field a validator message is about.
func alarmRuleFieldError(err error) error {
	msg := err.Error()
	field := "rule"
	for _, k := range []struct{ word, field string }{
		{"path", "path"}, {"label", "label"}, {"operator", "op"}, {"state", "state"},
		{"hysteresis", "clear_value"}, {"dwell", "dwell_seconds"}, {"stale", "stale_after_seconds"},
	} {
		if strings.Contains(msg, k.word) {
			field = k.field
			break
		}
	}
	return &fieldedError{Field: field, Message: msg, cause: err}
}

// applyAlarmFields lays the given fields over r. Values arrive in the shown
// unit and leave in SI. A clear_value sets the deadband against the threshold
// it ends up with; without one the existing deadband stays.
func applyAlarmFields(r alarmRule, given fieldSet) (alarmRule, error) {
	d, known := alarmDisplayFor(r.Path, r.PathRole)
	needUnit := func(field string) error {
		if known {
			return nil
		}
		return fieldErr(field, "the boat has not reported a unit for %s, so Mate can't tell what this number means; set the level in the Alarms drawer", r.Path)
	}
	if v, ok := given["label"]; ok {
		r.Label = stringOf(v)
	}
	if v, ok := given["op"]; ok {
		r.Op = stringOf(v)
	}
	if v, ok := given["state"]; ok {
		r.State = stringOf(v)
	}
	if v, ok := given["enabled"]; ok {
		r.Enabled, _ = v.(bool)
	}
	if v, ok := given["mate_reviews"]; ok {
		r.MateReviews, _ = v.(bool)
	}
	if v := intPtrOf(given["dwell_seconds"]); v != nil {
		r.DwellSeconds = *v
	}
	if v := intPtrOf(given["stale_after_seconds"]); v != nil {
		r.StaleAfterSeconds = *v
	}
	if v := floatPtrOf(given["value"]); v != nil {
		if err := needUnit("value"); err != nil {
			return r, err
		}
		r.Value = d.toSI(*v)
	}
	if clear, ok := given["clear_value"]; ok {
		if c := floatPtrOf(clear); c == nil {
			// An explicit null is "no clear point", true of every operator.
			r.Hysteresis = 0
		} else {
			if r.Op != alarmOpAbove && r.Op != alarmOpBelow {
				return r, fieldErr("clear_value", "a %s alarm has no clear point", r.Op)
			}
			if err := needUnit("clear_value"); err != nil {
				return r, err
			}
			gap := d.toSI(*c) - r.Value
			if r.Op == alarmOpAbove {
				gap = -gap
			}
			if gap < 0 {
				side := "above"
				if r.Op == alarmOpAbove {
					side = "below"
				}
				return r, fieldErr("clear_value", "a %s alarm clears %s its threshold, so this must be %s %s", r.Op, side, side, d.text(d.toShown(r.Value)))
			}
			r.Hysteresis = gap
		}
	}
	return r, nil
}

func requireAlarmFields(given fieldSet, names ...string) error {
	for _, n := range names {
		if _, ok := given[n]; !ok {
			return fieldErr(n, "is required")
		}
	}
	return nil
}

func createAlarmRuleRecord(env changeEnv, given fieldSet) (createdRecord, error) {
	if err := requireAlarmFields(given, "label", "path", "op", "state"); err != nil {
		return createdRecord{}, err
	}
	if op := stringOf(given["op"]); op == alarmOpStale {
		if err := requireAlarmFields(given, "stale_after_seconds"); err != nil {
			return createdRecord{}, err
		}
	} else if err := requireAlarmFields(given, "value", "dwell_seconds"); err != nil {
		return createdRecord{}, err
	}
	if stringOf(given["path"]) == "" {
		return createdRecord{}, fieldErr("path", "alarm rule requires a SignalK path")
	}
	r := alarmRule{Enabled: true, Path: stringOf(given["path"]), Methods: []string{"visual", "sound"}}
	if derr := derivedAlarmRuleReason(r.Path); derr != nil {
		return createdRecord{}, fieldErr("path", "%v", derr)
	}
	r, err := applyAlarmFields(r, given)
	if err != nil {
		return createdRecord{}, err
	}
	if err := validateAlarmRuleResolving(&r, false, func(role string) (string, error) { return alarmRolePathQ(env.tx, role) }); err != nil {
		return createdRecord{}, alarmRuleFieldError(err)
	}
	r.ID = uuid.NewString()
	r.CreatedAt, r.UpdatedAt = env.now, env.now
	if err := insertAlarmRuleTx(env.tx, r); err != nil {
		return createdRecord{}, err
	}
	return createdRecord{ID: r.ID}, nil
}

func updateAlarmRuleRecord(env changeEnv, before recordSnapshot, given fieldSet) error {
	if p, ok := given["path"]; ok && stringOf(p) != stringOf(before.Fields["path"]) {
		return fieldErr("path", "an alarm keeps its path; delete it and add a new alarm for %s", stringOf(p))
	}
	r, err := alarmRuleByIDQ(env.tx, before.ID)
	if err != nil {
		return err
	}
	next, err := applyAlarmFields(r, given)
	if err != nil {
		return err
	}
	if err := validateAlarmRuleResolving(&next, true, func(role string) (string, error) { return alarmRolePathQ(env.tx, role) }); err != nil {
		return alarmRuleFieldError(err)
	}
	next.UpdatedAt = env.now
	return updateAlarmRuleTx(env.tx, next)
}

func deleteAlarmRuleRecord(env changeEnv, before recordSnapshot) error {
	return deleteAlarmRuleTx(env.tx, before.ID)
}

// pinAlarmRuleUnit records the unit the operation's numbers are read in, when
// it gives any, so Apply can refuse if the boat's unit for the path moved on.
func pinAlarmRuleUnit(q sqlQueryer, op changeOp) (map[string]string, error) {
	_, hasValue := op.Fields["value"]
	clear, hasClear := op.Fields["clear_value"]
	// An update of a rule this changeset creates has nothing stored to read;
	// the create pins the same path.
	if op.Action == changeDelete || isRef(op.ID) || (!hasValue && (!hasClear || clear == nil)) {
		return nil, nil
	}
	path, role := stringOf(op.Fields["path"]), ""
	if op.Action == changeUpdate {
		r, err := alarmRuleByIDQ(q, op.ID)
		if err != nil {
			return nil, err
		}
		path, role = r.Path, r.PathRole
	}
	return map[string]string{"unit": alarmUnitFor(path, role)}, nil
}

// ── words ───────────────────────────────────────────────────────────────

var alarmSeverityWord = map[string]string{
	alarmStateAlert: "an alert", alarmStateWarn: "a warning", alarmStateAlarm: "an alarm", alarmStateEmergency: "an emergency",
}

// alarmCondition is the rule in a watchkeeper's words, from a snapshot.
func alarmCondition(f map[string]any) string {
	d := alarmDisplay{label: stringOf(f["unit"])}
	val := func(name string) string {
		v := floatPtrOf(f[name])
		if v == nil {
			return ""
		}
		return d.text(*v)
	}
	severity := alarmSeverityWord[stringOf(f["state"])]
	secs := intPtrOf(f["dwell_seconds"])
	var out string
	switch stringOf(f["op"]) {
	case alarmOpBelow, alarmOpAbove:
		verb, back := "falls below", "above"
		if stringOf(f["op"]) == alarmOpAbove {
			verb, back = "rises above", "below"
		}
		out = fmt.Sprintf("%s when it %s %s", severity, verb, val("value"))
		if c := val("clear_value"); c != "" {
			out += fmt.Sprintf(", clearing %s %s", back, c)
		}
	case alarmOpEqual:
		out = fmt.Sprintf("%s when it equals %s", severity, val("value"))
	case alarmOpNotEqual:
		out = fmt.Sprintf("%s when it is anything but %s", severity, val("value"))
	default:
		n := 0
		if s := intPtrOf(f["stale_after_seconds"]); s != nil {
			n = *s
		}
		return fmt.Sprintf("%s when it sends nothing for %d s", severity, n)
	}
	if secs != nil && *secs > 0 {
		out += fmt.Sprintf(", after %d s", *secs)
	}
	return out
}

func builtInNote(f map[string]any) string {
	if b, _ := f["built_in"].(bool); b {
		return " (built-in rule)"
	}
	return ""
}

func describeAlarmRule(d describeInput) string {
	switch d.Op.Action {
	case changeCreate:
		return "Add alarm " + d.After.Label + ": " + alarmCondition(d.After.Fields)
	case changeDelete:
		return "Remove alarm " + d.Before.Label + builtInNote(d.Before.Fields)
	}
	given := d.Op.Fields
	label := d.Before.Label + builtInNote(d.Before.Fields)
	if len(given) == 1 {
		if on, ok := given["enabled"].(bool); ok {
			if on {
				return "Switch on alarm " + label
			}
			return "Switch off alarm " + label
		}
	}
	if len(given) == 1 {
		if on, ok := given["mate_reviews"].(bool); ok {
			if on {
				return "Have Mate review alarm " + label + " when it goes off"
			}
			return "Stop Mate reviewing alarm " + label
		}
	}
	out := "Change alarm " + label + ": "
	if l := stringOf(given["label"]); l != "" && l != d.Before.Label {
		out += fmt.Sprintf("rename to %s, ", l)
	}
	out += "now " + alarmCondition(d.After.Fields)
	if on, ok := given["enabled"].(bool); ok {
		if on {
			out += ", switched on"
		} else {
			out += ", switched off"
		}
	}
	if on, ok := given["mate_reviews"].(bool); ok {
		if on {
			out += ", reviewed by Mate when it goes off"
		} else {
			out += ", no longer reviewed by Mate"
		}
	}
	return out
}

var alarmOpWords = map[string]string{
	alarmOpAbove: "rises above", alarmOpBelow: "falls below", alarmOpEqual: "equals",
	alarmOpNotEqual: "differs from", alarmOpStale: "stops reporting",
}

var alarmStateWords = map[string]string{
	alarmStateAlert: "Alert", alarmStateWarn: "Warning", alarmStateAlarm: "Alarm", alarmStateEmergency: "Emergency",
}

// displayAlarmRuleFields is how the card shows a rule's fields: the numbers
// with their unit, and the unit and origin left to the description.
func displayAlarmRuleFields(snap recordSnapshot, fields map[string]any) map[string]any {
	d := alarmDisplay{label: stringOf(snap.Fields["unit"])}
	out := make(map[string]any, len(fields))
	for k, v := range fields {
		switch k {
		case "unit", "built_in", "path_role":
			continue
		case "value", "clear_value":
			if n := floatPtrOf(v); n != nil {
				v = d.text(*n)
			}
		case "op":
			if w, ok := alarmOpWords[stringOf(v)]; ok {
				v = w
			}
		case "state":
			if w, ok := alarmStateWords[stringOf(v)]; ok {
				v = w
			}
		case "dwell_seconds", "stale_after_seconds":
			if n := intPtrOf(v); n != nil {
				v = fmt.Sprintf("%d s", *n)
			}
		}
		out[k] = v
	}
	return out
}
