package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"strconv"
	"strings"
	"time"
)

// get_alarm_context (ADR 0226): everything Mate needs to explain one alarm,
// gathered in one call. The alarm itself is read back exactly as the card
// shows it; nothing here rewrites its title or body. Each part that cannot be
// given is absent with a reason, never filled in.

const (
	alarmContextHistoryBefore = 30 * time.Minute
	alarmContextHistoryAfter  = 30 * time.Minute
	alarmContextHistoryMax    = 2 * time.Hour
	alarmContextNeighbours    = 15 * time.Minute
	alarmContextMaxNeighbours = 12
	alarmContextOccurrences   = 7 * 24 * time.Hour
)

type assistantAlarmContextArgs struct {
	AlarmID string `json:"alarm_id"`
}

// assistantAlarmCard is the alarm as the card presents it.
type assistantAlarmCard struct {
	ID        string `json:"id"`
	Title     string `json:"title"`
	Body      string `json:"body,omitempty"`
	State     string `json:"state"`
	Phase     string `json:"phase,omitempty"`
	Active    bool   `json:"active"`
	Source    string `json:"source"`
	RaisedAt  string `json:"raised_at,omitempty"`
	AckedAt   string `json:"acked_at,omitempty"`
	ClearedAt string `json:"cleared_at,omitempty"`
	// ClearNote says why an occurrence has no cleared time when the log never
	// recorded one, so a missing clear is not read as "still going".
	ClearNote string `json:"clear_note,omitempty"`
}

type assistantAlarmLive struct {
	Value      float64 `json:"value"`
	Unit       string  `json:"unit,omitempty"`
	Source     string  `json:"source,omitempty"`
	AgeSeconds float64 `json:"age_seconds"`
	Stale      bool    `json:"stale"`
}

// assistantAlarmHistory is get_path_history's answer for the window round the
// raise, cut down to the figures that explain it, in the operator's unit.
type assistantAlarmHistory struct {
	// Start, End, FirstSeen and LastSeen are boat local time (the result's
	// timezone), like every other time here; the ISO forms are UTC.
	Start        string   `json:"start"`
	End          string   `json:"end"`
	FirstSeen    string   `json:"first_seen,omitempty"`
	LastSeen     string   `json:"last_seen,omitempty"`
	StartISO     string   `json:"start_iso"`
	EndISO       string   `json:"end_iso"`
	Unit         string   `json:"unit,omitempty"`
	Min          *float64 `json:"min,omitempty"`
	Mean         *float64 `json:"mean,omitempty"`
	Max          *float64 `json:"max,omitempty"`
	FirstSeenISO string   `json:"first_seen_iso,omitempty"`
	LastSeenISO  string   `json:"last_seen_iso,omitempty"`
	// Shape says how the reading moved, from the buckets: the largest jump
	// between neighbours and the longest run of an identical value.
	Shape    *assistantAlarmShape `json:"shape,omitempty"`
	GapCount int                  `json:"gap_count,omitempty"`
	Note     string               `json:"note,omitempty"`
}

type assistantAlarmShape struct {
	BucketCount        int      `json:"bucket_count"`
	BucketMinutes      float64  `json:"bucket_minutes"`
	MaxStep            *float64 `json:"max_step,omitempty"`
	LongestFlatMinutes float64  `json:"longest_flat_minutes"`
}

type assistantAlarmOccurrences struct {
	RaisedLast7Days int `json:"raised_last_7_days"`
	// Pattern is set when the alarm keeps going off, in words Mate can repeat.
	Pattern string `json:"pattern,omitempty"`
}

// alarmContextRepeatThreshold is how many raises in a week make an alarm one
// that "keeps going off".
const alarmContextRepeatThreshold = 20

// assistantBuiltInCheck stands in for the rule of an alarm that Helmcentral's
// own code raises, which has no stored rule to read or edit.
type assistantBuiltInCheck struct {
	Kind string `json:"kind"`
	Note string `json:"note"`
}

type assistantAlarmNeighbour struct {
	Title string `json:"title"`
	State string `json:"state"`
	Event string `json:"event"`
	At    string `json:"at"`
}

type assistantAlarmContextResult struct {
	Alarm    assistantAlarmCard `json:"alarm"`
	Timezone string             `json:"timezone"`
	// TimezoneNote is set when the boat's time zone could not be read.
	TimezoneNote string `json:"timezone_note,omitempty"`

	Rule       *assistantAlarmRuleEntry `json:"rule,omitempty"`
	RuleAbsent string                   `json:"rule_absent,omitempty"`
	// BuiltInCheck is set instead of a rule for Helmcentral's own checks.
	BuiltInCheck *assistantBuiltInCheck `json:"built_in_check,omitempty"`

	Trigger struct {
		Path        string   `json:"path,omitempty"`
		Investigate []string `json:"investigate,omitempty"`
	} `json:"trigger"`

	Live       *assistantAlarmLive `json:"live,omitempty"`
	LiveAbsent string              `json:"live_absent,omitempty"`

	History       *assistantAlarmHistory `json:"history,omitempty"`
	HistoryAbsent string                 `json:"history_absent,omitempty"`

	Occurrences       *assistantAlarmOccurrences `json:"occurrences,omitempty"`
	OccurrencesAbsent string                     `json:"occurrences_absent,omitempty"`

	SameWindow       []assistantAlarmNeighbour `json:"same_window"`
	SameWindowNote   string                    `json:"same_window_note,omitempty"`
	SiblingPaths     []string                  `json:"sibling_paths"`
	SiblingPathsNote string                    `json:"siblings_note"`
}

// alarmContextSubject is the alarm being asked about, whether it is live or
// only in the log.
type alarmContextSubject struct {
	status    alarmStatus
	source    string
	clearedAt time.Time
	active    bool
	logID     string
	clearNote string
}

func (d assistantToolDeps) findAlarmContextSubject(id string) (alarmContextSubject, error) {
	if d.activeAlarms != nil {
		for _, a := range d.activeAlarms() {
			if a.RuleID == id {
				return alarmContextSubject{status: a, active: true}, nil
			}
		}
	}
	var store *alarmLogStore
	if d.alarmLog != nil {
		store = d.alarmLog()
	}
	if store == nil {
		return alarmContextSubject{}, fmt.Errorf("get_alarm_context: no active alarm has the id %q and the alarm log is not available", id)
	}
	entry, ok, err := store.Get(id)
	if err == nil && !ok {
		entry, ok, err = store.LatestForRule(id)
	}
	if err != nil {
		return alarmContextSubject{}, fmt.Errorf("get_alarm_context: %w", err)
	}
	if !ok {
		return alarmContextSubject{}, fmt.Errorf("get_alarm_context: no alarm, live or logged, has the id %q (get_active_alarms lists the live ones)", id)
	}
	sub := alarmContextSubject{
		status: alarmStatus{
			RuleID: entry.RuleID, Label: entry.Label, Path: entry.Path, State: entry.State, Message: entry.Message,
			Phase: alarmPhaseNormal, RaisedAt: entry.RaisedAt,
		},
		source: entry.Source, logID: entry.ID,
	}
	if entry.AckedAt != nil {
		sub.status.AckedAt = *entry.AckedAt
	}
	if entry.ClearedAt != nil {
		sub.clearedAt = *entry.ClearedAt
	} else {
		// Only the rule's open occurrence is the alarm on the board. An older
		// row that was never closed (a restart can leave one) is not today's
		// alarm, so it is reported as it was logged and its missing clear is said.
		open, isOpen, err := store.OpenOccurrence(entry.RuleID)
		if err != nil {
			return alarmContextSubject{}, fmt.Errorf("get_alarm_context: %w", err)
		}
		if active := d.liveAlarmFor(entry.RuleID); active != nil && isOpen && open.ID == entry.ID {
			sub.status, sub.active = *active, true
		} else {
			sub.clearNote = "no clear was recorded for this occurrence, so whether it ended is not known"
		}
	}
	return sub, nil
}

func (d assistantToolDeps) liveAlarmFor(ruleID string) *alarmStatus {
	if d.activeAlarms == nil {
		return nil
	}
	for _, a := range d.activeAlarms() {
		if a.RuleID == ruleID {
			return &a
		}
	}
	return nil
}

// alarmContextRule finds the rule behind an alarm: a stored one or one
// Helmcentral derives. ok is false for a network alarm or a deleted rule.
func alarmContextRule(ruleID string) (assistantAlarmRuleEntry, bool) {
	if r, ok := getAlarmRule(ruleID); ok {
		return alarmRuleListEntry(r, false), true
	}
	for _, r := range derivedAlarmRules() {
		if r.ID == ruleID {
			return alarmRuleListEntry(r, true), true
		}
	}
	return assistantAlarmRuleEntry{}, false
}

func (d assistantToolDeps) executeGetAlarmContext(ctx context.Context, raw json.RawMessage) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	var args assistantAlarmContextArgs
	if err := json.Unmarshal(raw, &args); err != nil {
		return "", fmt.Errorf("parse get_alarm_context arguments: %w", err)
	}
	id := strings.TrimSpace(args.AlarmID)
	if id == "" {
		return "", errors.New("get_alarm_context: alarm_id is required (the id of the alarm the operator asked about, or a rule_id from get_active_alarms)")
	}
	sub, err := d.findAlarmContextSubject(id)
	if err != nil {
		return "", err
	}
	now := d.now()
	a := sub.status

	loc, tz, tzNote := d.alarmContextLocation(a.RaisedAt, a.AckedAt, sub.clearedAt)
	stamp := func(t time.Time) string {
		if t.IsZero() {
			return ""
		}
		return t.In(loc).Format("Mon 2 Jan 15:04")
	}

	var res assistantAlarmContextResult
	res.Timezone, res.TimezoneNote = tz, tzNote
	res.SameWindow, res.SiblingPaths = []assistantAlarmNeighbour{}, []string{}
	res.Alarm = assistantAlarmCard{
		ID: id, Title: a.Label, Body: a.Message, State: a.State, Phase: string(a.Phase), Active: sub.active,
		RaisedAt: stamp(a.RaisedAt), AckedAt: stamp(a.AckedAt), ClearedAt: stamp(sub.clearedAt), ClearNote: sub.clearNote,
	}

	// The rule behind it.
	rule, hasRule := alarmContextRule(a.RuleID)
	switch {
	case hasRule:
		res.Rule = &rule
		res.Alarm.Source = "rule"
	case isBuiltInAlarmCheck(a.RuleID):
		res.Alarm.Source = "rule"
		res.BuiltInCheck = &assistantBuiltInCheck{Kind: "built_in", Note: "Helmcentral's own check, not a rule you can edit"}
	case sub.active && !strings.HasPrefix(a.RuleID, notificationsRoot+":") && a.Threshold != nil:
		res.Alarm.Source = "rule"
		res.RuleAbsent = "the engine holds this rule but it is not in the rule list"
	default:
		res.Alarm.Source = "network"
		res.RuleAbsent = "Helmcentral holds no rule for this alarm: it was raised by something else on the boat network, or its rule has since been deleted"
	}
	if sub.source == alarmSourceRule && !hasRule && res.BuiltInCheck == nil {
		res.Alarm.Source = "rule"
		res.RuleAbsent = "this alarm's rule has since been deleted"
	}

	// What to read.
	path := a.Path
	res.Trigger.Investigate = d.alarmInvestigateTargets(a)
	if hasRule {
		path = rule.Path
	}
	if len(res.Trigger.Investigate) == 1 {
		path = res.Trigger.Investigate[0]
	}
	dataPath := ""
	if len(res.Trigger.Investigate) == 1 {
		dataPath = res.Trigger.Investigate[0]
	}
	res.Trigger.Path = path

	d.fillAlarmContextLive(&res, dataPath, now)
	d.fillAlarmContextHistory(ctx, &res, dataPath, a, sub.clearedAt, now)
	d.fillAlarmContextLog(&res, sub, a, stamp)

	res.SiblingPathsNote = "not given: Helmcentral keeps no grouping of related paths, so compare against the item's other readings with check_signalk_paths if it matters"

	return capToolResultJSON(&res, func() bool {
		if n := len(res.SameWindow); n > 4 {
			res.SameWindow = res.SameWindow[:n/2]
			res.SameWindowNote = "list cut to fit"
			return true
		}
		return false
	})
}

func (d assistantToolDeps) alarmContextLocation(ats ...time.Time) (*time.Location, string, string) {
	return boatTimeNote(d.boatTime(), append(ats, d.now())...)
}

// boatTimeNote is a boat clock as a tool result states it: the location, the
// label, and a note only when no fix has ever been seen and the times are UTC.
func boatTimeNote(b boatTime, ats ...time.Time) (*time.Location, string, string) {
	note := ""
	if b.NoFix {
		note = "no position fix has been seen yet; these times are UTC, not boat time"
		if b.Err != nil {
			note = fmt.Sprintf("the last-fix time zone is unreadable (%v); these times are UTC, not boat time", b.Err)
		}
	}
	return b.Loc, b.LabelSpan(ats...), note
}

func (d assistantToolDeps) fillAlarmContextLive(res *assistantAlarmContextResult, dataPath string, now time.Time) {
	if dataPath == "" {
		res.LiveAbsent = "this alarm has no data path of its own to read"
		return
	}
	if d.signalKDiagnostics == nil {
		res.LiveAbsent = "the live SignalK view is not wired"
		return
	}
	snap := d.signalKDiagnostics(now)
	if !snap.HasSelfTree {
		res.LiveAbsent = "SignalK is not connected, so there is no live reading"
		return
	}
	for _, s := range snap.Samples {
		if s.Path != dataPath {
			continue
		}
		v, ok := s.Value.(float64)
		if !ok {
			res.LiveAbsent = fmt.Sprintf("%s is not a number right now", dataPath)
			return
		}
		role := ""
		if r, ok := getAlarmRuleByPath(res.Rule, dataPath); ok {
			role = r
		}
		disp, _ := alarmDisplayFor(dataPath, role)
		live := assistantAlarmLive{Value: roundTo(disp.toShown(v), 3), Unit: disp.label, Source: s.Source}
		if s.AgeSeconds >= 0 {
			live.AgeSeconds = roundTo1(s.AgeSeconds)
			live.Stale = s.AgeSeconds > assistantDiagnosticsStaleAfter.Seconds()
		} else {
			res.LiveAbsent = fmt.Sprintf("%s has a value but nothing says how old it is", dataPath)
			return
		}
		res.Live = &live
		return
	}
	res.LiveAbsent = fmt.Sprintf("%s is not in the live SignalK tree right now: it may have gone quiet", dataPath)
}

// getAlarmRuleByPath returns the path role of the rule entry's stored rule, so
// a role-bound rule converts like its card does.
func getAlarmRuleByPath(entry *assistantAlarmRuleEntry, path string) (string, bool) {
	if entry == nil {
		return "", false
	}
	if r, ok := getAlarmRule(entry.ID); ok && r.Path == path {
		return r.PathRole, r.PathRole != ""
	}
	return "", false
}

func (d assistantToolDeps) fillAlarmContextHistory(ctx context.Context, res *assistantAlarmContextResult, dataPath string, a alarmStatus, clearedAt, now time.Time) {
	switch {
	case dataPath == "":
		res.HistoryAbsent = "this alarm has no data path of its own, so there is no history to read"
		return
	case a.RaisedAt.IsZero():
		res.HistoryAbsent = "the raise time is not known, so there is no window to read"
		return
	case d.influxPathHistoryStat == nil || d.influxPathHistoryFirstLast == nil || d.vesselState == nil:
		res.HistoryAbsent = "InfluxDB history is not available on this install"
		return
	}
	start := a.RaisedAt.Add(-alarmContextHistoryBefore)
	end := a.RaisedAt.Add(alarmContextHistoryAfter)
	if !clearedAt.IsZero() && clearedAt.After(end) {
		end = clearedAt
	}
	if limit := a.RaisedAt.Add(alarmContextHistoryMax); end.After(limit) {
		end = limit
	}
	if end.After(now) {
		end = now
	}
	if !end.After(start) {
		res.HistoryAbsent = "the window round the raise is empty"
		return
	}
	args, _ := json.Marshal(assistantGetPathHistoryArgs{Path: dataPath, Start: start.UTC().Format(time.RFC3339), End: end.UTC().Format(time.RFC3339)})
	// Unrounded: the figures are converted to the operator's unit first.
	hp, _, err := d.pathHistory(ctx, args, false)
	if err != nil {
		res.HistoryAbsent = "reading the history failed: " + err.Error()
		return
	}
	h := *hp
	if len(h.Buckets) == 0 {
		res.HistoryAbsent = "nothing was recorded for this path in the window round the raise"
		return
	}
	role := ""
	if res.Rule != nil {
		role, _ = getAlarmRuleByPath(res.Rule, dataPath)
	}
	disp, _ := alarmDisplayFor(dataPath, role)
	conv := func(v *float64) *float64 {
		if v == nil {
			return nil
		}
		if h.Unit != "" { // an angle is already in degrees
			return v
		}
		return roundTo2Ptr(floatPtr(disp.toShown(*v)))
	}
	unit := disp.label
	if h.Unit != "" {
		unit = h.Unit
	}
	res.History = &assistantAlarmHistory{
		Start: h.Start, End: h.End, FirstSeen: h.FirstSeen, LastSeen: h.LastSeen,
		StartISO: h.StartISO, EndISO: h.EndISO, Unit: unit,
		Min: conv(h.OverallMin), Mean: conv(h.OverallMean), Max: conv(h.OverallMax),
		FirstSeenISO: h.FirstSeenISO, LastSeenISO: h.LastSeenISO, GapCount: h.GapCount, Note: h.Note,
	}
	if h.Unit == "" {
		res.History.Shape = alarmHistoryShape(h.Buckets, h.Bucket, disp)
	}
}

// alarmHistoryShape reads how the reading moved from the history buckets:
// the largest step between neighbouring bucket means, and the longest run of
// neighbouring buckets that held one identical value (min, mean and max the
// same, as a frozen sensor does). Steps are in the operator's unit. nil when
// the bucket width is not known.
func alarmHistoryShape(buckets []assistantPathHistoryBucket, every string, disp alarmDisplay) *assistantAlarmShape {
	width := parseHistoryEvery(every)
	if width <= 0 || len(buckets) == 0 {
		return nil
	}
	shape := &assistantAlarmShape{BucketCount: len(buckets), BucketMinutes: width.Minutes()}
	var prev *assistantPathHistoryBucket
	var prevT time.Time
	run := 1
	for i := range buckets {
		b := &buckets[i]
		t, err := time.Parse(time.RFC3339, b.T)
		if err != nil || b.Mean == nil {
			prev, run = nil, 1
			continue
		}
		flat := b.Min != nil && b.Max != nil && *b.Min == *b.Max
		if prev != nil && t.Sub(prevT) == width {
			step := roundTo(disp.toShown(*b.Mean)-disp.toShown(*prev.Mean), 3)
			if step < 0 {
				step = -step
			}
			if shape.MaxStep == nil || step > *shape.MaxStep {
				shape.MaxStep = &step
			}
			if flat && prev.Min != nil && prev.Max != nil && *prev.Min == *prev.Max && *prev.Mean == *b.Mean {
				run++
			} else {
				run = 1
			}
		} else {
			run = 1
		}
		if flat && float64(run)*width.Minutes() > shape.LongestFlatMinutes {
			shape.LongestFlatMinutes = float64(run) * width.Minutes()
		}
		prev, prevT = b, t
	}
	return shape
}

// parseHistoryEvery reads a bucket width such as "5m", "12h" or "2d".
func parseHistoryEvery(every string) time.Duration {
	if days, ok := strings.CutSuffix(every, "d"); ok {
		n, err := strconv.Atoi(days)
		if err != nil {
			return 0
		}
		return time.Duration(n) * 24 * time.Hour
	}
	d, err := time.ParseDuration(every)
	if err != nil {
		return 0
	}
	return d
}

// alarmRepeatPattern says how often an alarm keeps going off. The rate is over
// the span the raises actually cover, never a day figure for less than a day;
// a short span is given as a count since the first raise.
func (d assistantToolDeps) alarmRepeatPattern(store *alarmLogStore, ruleID string, n int, now time.Time, stamp func(time.Time) string) (string, error) {
	first, ok, err := store.EarliestRaised(ruleID, now.Add(-alarmContextOccurrences))
	if err != nil {
		return "", err
	}
	if !ok {
		return "", fmt.Errorf("read alarm log: %d raises counted but no first raise found", n)
	}
	span := now.Sub(first)
	if span < 48*time.Hour {
		return fmt.Sprintf("raised %d times since %s", n, stamp(first)), nil
	}
	return fmt.Sprintf("raised %d times in %d days, about %d a day", n, int(math.Round(span.Hours()/24)), int(math.Round(float64(n)/(span.Hours()/24)))), nil
}

func (d assistantToolDeps) fillAlarmContextLog(res *assistantAlarmContextResult, sub alarmContextSubject, a alarmStatus, stamp func(time.Time) string) {
	var store *alarmLogStore
	if d.alarmLog != nil {
		store = d.alarmLog()
	}
	if store == nil {
		res.OccurrencesAbsent = "the alarm log is not available"
		res.SameWindowNote = "the alarm log is not available"
		return
	}
	now := d.now()
	n, err := store.RaisedCount(a.RuleID, now.Add(-alarmContextOccurrences))
	if err != nil {
		res.OccurrencesAbsent = "reading the alarm log failed: " + err.Error()
	} else {
		res.Occurrences = &assistantAlarmOccurrences{RaisedLast7Days: n}
		if n >= alarmContextRepeatThreshold {
			pattern, err := d.alarmRepeatPattern(store, a.RuleID, n, now, stamp)
			if err != nil {
				res.Occurrences = nil
				res.OccurrencesAbsent = "reading the alarm log failed: " + err.Error()
			} else {
				res.Occurrences.Pattern = pattern
			}
		}
	}
	if a.RaisedAt.IsZero() {
		res.SameWindowNote = "the raise time is not known"
		return
	}
	entries, err := store.ChangedBetween(a.RaisedAt.Add(-alarmContextNeighbours), a.RaisedAt.Add(alarmContextNeighbours), a.RuleID)
	if err != nil {
		res.SameWindowNote = "reading the alarm log failed: " + err.Error()
		return
	}
	lo, hi := a.RaisedAt.Add(-alarmContextNeighbours), a.RaisedAt.Add(alarmContextNeighbours)
	for _, e := range entries {
		if !e.RaisedAt.Before(lo) && !e.RaisedAt.After(hi) {
			res.SameWindow = append(res.SameWindow, assistantAlarmNeighbour{Title: e.Label, State: e.State, Event: "raised", At: stamp(e.RaisedAt)})
		}
		if e.ClearedAt != nil && !e.ClearedAt.Before(lo) && !e.ClearedAt.After(hi) {
			res.SameWindow = append(res.SameWindow, assistantAlarmNeighbour{Title: e.Label, State: e.State, Event: "cleared", At: stamp(*e.ClearedAt)})
		}
		if len(res.SameWindow) >= alarmContextMaxNeighbours {
			res.SameWindowNote = "list cut to the first " + fmt.Sprint(alarmContextMaxNeighbours)
			break
		}
	}
}

var mateToolGetAlarmContext = mateTool{
	name: "get_alarm_context",
	function: openRouterFunctionDef{
		Description: "Everything needed to explain ONE alarm, in one call: the alarm exactly as its card shows it " +
			"(title, body, severity state, raised/acknowledged/cleared times in boat local time; read these back, never reword them), " +
			"the rule behind it (op, value and clear_value in the operator's unit, dwell, built-in or own, version) or why there is none, " +
			"the path that tripped it with its live reading and age, that path's min/mean/max over the window round the raise " +
			"(history; InfluxDB, with a shape: the biggest step between readings and the longest flat run), how many times the alarm was raised in the last 7 days, and the other alarms raised or cleared " +
			"within 15 minutes of it (same_window). Every part that cannot be given is absent with a *_absent or *_note reason: " +
			"never fill it in. Pass the alarm_id the turn carries (an alarm's rule_id, or the id of a log row for one " +
			"that has cleared), or a rule_id from get_active_alarms. Related sibling paths are not given. " +
			"Call this FIRST when asked about a specific alarm.",
		Parameters: json.RawMessage(`{"type": "object", "properties": {"alarm_id": {"type": "string", "description": "The alarm's id: its rule_id, or a log id for an alarm that has cleared."}}, "required": ["alarm_id"]}`),
	},
	execute: plainTool(assistantToolDeps.executeGetAlarmContext),
	describe: func(json.RawMessage) string {
		return "Looking into the alarm…"
	},
}
