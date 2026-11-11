package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"
)

/*
get_unusual_readings (ADR 0276): the stretches of a past window in which an
engine's or the house bank's readings were unusual for it, against a normal
learned from its own history. A read-only suggestion for Mate and for outside
assistants to mention as worth a look, never a diagnosis and never a cause.
*/

const (
	unusualToolDefaultSince = 24 * time.Hour
	unusualToolDefaultLimit = 5
	unusualToolMaxLimit     = 10
)

type unusualToolArgs struct {
	SinceISO string  `json:"since_iso"`
	Subject  string  `json:"subject"`
	Engine   *string `json:"engine"`
	Limit    *int    `json:"limit"`
}

type unusualToolLearned struct {
	Learned    string         `json:"learned"`
	LearnedISO string         `json:"learned_iso"`
	Source     string         `json:"source"`
	Bands      map[string]int `json:"bands_by_subject"`
}

type unusualToolNotJudged struct {
	Subject string `json:"subject,omitempty"`
	Reason  string `json:"reason"`
	Minutes int    `json:"minutes"`
}

type unusualToolResult struct {
	Subject              string                 `json:"subject,omitempty"`
	Since                string                 `json:"since"`
	SinceISO             string                 `json:"since_iso"`
	Until                string                 `json:"until"`
	UntilISO             string                 `json:"until_iso"`
	SinceNote            string                 `json:"since_note,omitempty"`
	Timezone             string                 `json:"timezone,omitempty"`
	TimezoneNote         string                 `json:"timezone_note,omitempty"`
	LearnedNormal        *unusualToolLearned    `json:"learned_normal,omitempty"`
	LearnedNormalMissing string                 `json:"learned_normal_missing,omitempty"`
	TrainingProblem      string                 `json:"training_problem,omitempty"`
	JudgedMinutes        int                    `json:"judged_minutes"`
	Stretches            []unusualStretchView   `json:"stretches"`
	StretchesNote        string                 `json:"stretches_note,omitempty"`
	NotJudged            []unusualToolNotJudged `json:"not_judged"`
	Note                 string                 `json:"note"`
}

const unusualToolNote = "These are suggestions from a normal learned from the boat's own history, each engine's and the house bank's, not a rule and not a diagnosis. " +
	"Mention one as worth a look, in the operator's units, and never give a cause for it. Minutes that were not judged are counted in not_judged: never fill them in. " +
	"A stretch with an operator_answer has been answered: \"normal\" means the operator says it is usual for that engine or the house bank, so do not raise it again; \"real\" means they agree something was wrong; \"unknown\" means they could not say."

func (d assistantToolDeps) executeGetUnusualEngineReadings(ctx context.Context, raw json.RawMessage) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	if d.unusualReadings == nil || d.now == nil {
		return "", errors.New("get_unusual_readings: the learned normal is not wired")
	}
	var args unusualToolArgs
	if len(raw) > 0 {
		if err := json.Unmarshal(raw, &args); err != nil {
			return "", fmt.Errorf("parse get_unusual_readings arguments: %w", err)
		}
	}
	// The old engine argument is refused: ignoring it would answer for every
	// subject when one was asked for.
	if args.Engine != nil {
		return "", errors.New("get_unusual_readings: engine is no longer an argument, use subject")
	}
	limit := unusualToolDefaultLimit
	if args.Limit != nil {
		limit = *args.Limit
		if limit < 1 || limit > unusualToolMaxLimit {
			return "", fmt.Errorf("get_unusual_readings: limit must be from 1 to %d", unusualToolMaxLimit)
		}
	}
	now := d.now().UTC()
	since := now.Add(-unusualToolDefaultSince)
	if s := strings.TrimSpace(args.SinceISO); s != "" {
		t, err := time.Parse(time.RFC3339, s)
		if err != nil {
			return "", fmt.Errorf("get_unusual_readings: since_iso %q is not an RFC 3339 time such as 2026-11-09T16:00:00Z", s)
		}
		if !t.Before(now) {
			return "", errors.New("get_unusual_readings: since_iso must be in the past")
		}
		since = t.UTC()
	}

	which, subjectName, err := d.unusualSubjectNamed(args.Subject)
	if err != nil {
		return "", err
	}
	// The figures are worded in the operator's units, so units that cannot be
	// read stop the call rather than fall back to a system.
	if d.displayUnits == nil {
		return "", errors.New("get_unusual_readings: the display units are not wired")
	}
	system, err := d.displayUnits()
	if err != nil {
		return "", fmt.Errorf("get_unusual_readings: the display units could not be read: %w", err)
	}
	if d.unusualAnswers == nil {
		return "", errors.New("get_unusual_readings: the operator's answers are not wired")
	}
	answers, err := d.unusualAnswers()
	if err != nil {
		return "", fmt.Errorf("get_unusual_readings: the answers about unusual stretches could not be read: %w", err)
	}
	res := d.unusualReadings(ctx, since, now, which, answers)
	if err := ctx.Err(); err != nil {
		return "", err
	}
	if !res.Since.IsZero() {
		since = res.Since
	}

	loc, zone, tzNote := boatTimeNote(d.boatTime(), since, now)
	out := unusualToolResult{
		Subject: subjectName,
		Since:   boatDayClock(since, loc), SinceISO: since.Format(time.RFC3339),
		Until: boatDayClock(now, loc), UntilISO: now.Format(time.RFC3339),
		Timezone: zone, TimezoneNote: tzNote,
		TrainingProblem: res.TrainingProblem, JudgedMinutes: res.JudgedMinutes,
		Stretches: []unusualStretchView{}, NotJudged: []unusualToolNotJudged{}, Note: unusualToolNote,
	}
	if res.Clipped {
		out.SinceNote = "since_iso was more than 7 days back, so the window starts 7 days ago"
	}
	if res.Problem != "" {
		out.LearnedNormalMissing = res.Problem
	} else if res.Model != nil {
		out.LearnedNormal = unusualLearned(res.Model, loc)
	}
	for i, st := range res.Stretches {
		if i == limit {
			out.StretchesNote = unusualMoreNote(len(res.Stretches) - limit)
			break
		}
		v, err := describeUnusualStretch(st, system, loc, now)
		if err != nil {
			return "", fmt.Errorf("get_unusual_readings: %w", err)
		}
		v.OperatorAnswer = overlappingAnswer(answers, st.Subsystem, st.Subject, st.Start, st.End)
		out.Stretches = append(out.Stretches, v)
	}
	for _, n := range res.NotJudged {
		out.NotJudged = append(out.NotJudged, unusualToolNotJudged{Subject: n.SubjectName, Reason: n.Reason, Minutes: n.Minutes})
	}
	return capToolResultJSON(&out, func() bool {
		if n := len(out.Stretches); n > 1 {
			out.Stretches = out.Stretches[:n/2]
			out.StretchesNote = "list cut to fit"
			return true
		}
		return false
	})
}

// unusualSubjectNamed resolves the subject argument to an engine or the house
// bank: the id, the operator's name for it, or the engine's name without "engine".
// A blank argument is every subject.
func (d assistantToolDeps) unusualSubjectNamed(arg string) (which unusualSubject, name string, err error) {
	arg = strings.TrimSpace(arg)
	if arg == "" {
		return unusualSubject{}, "", nil
	}
	if d.vesselView == nil {
		return unusualSubject{}, "", errors.New("get_unusual_readings: the vessel is not wired, so a subject cannot be named")
	}
	vessel, err := d.vesselView()
	if err != nil {
		return unusualSubject{}, "", fmt.Errorf("get_unusual_readings: vessel settings: %w", err)
	}
	var known []string
	for _, sub := range normalSubsystems() {
		for _, s := range sub.Subjects(vessel) {
			known = append(known, s.Name)
			names := []string{s.ID, s.Name}
			if sub.Aliases != nil {
				names = append(names, sub.Aliases(s)...)
			}
			if slices.ContainsFunc(names, func(n string) bool { return strings.EqualFold(arg, n) }) {
				return unusualSubject{Subsystem: sub.Name, ID: s.ID}, s.Name, nil
			}
		}
	}
	return unusualSubject{}, "", fmt.Errorf("get_unusual_readings: nothing is called %q (the subjects are: %s)", arg, strings.Join(known, ", "))
}

func unusualLearned(m *unusualModelInfo, loc *time.Location) *unusualToolLearned {
	return &unusualToolLearned{
		Learned: boatDayClock(m.TrainedAt, loc), LearnedISO: m.TrainedAt.UTC().Format(time.RFC3339),
		Source: normalSourceText(m.Source), Bands: m.Bands,
	}
}

var mateToolGetUnusualReadings = mateTool{
	name:      "get_unusual_readings",
	effect:    effectReads,
	audiences: audienceReviewsAndMCP,
	function: openRouterFunctionDef{
		Description: "Stretches of the last day (or up to 7 days) in which an engine's or the house bank's readings were unusual for it, judged against a normal " +
			"learned from its own history: an engine at the same rpm, the house bank with the same things charging at the same state of charge. Each stretch is " +
			"at least three minutes in a row, with the reading that stood out most, its value against the usual one in the operator's units, and the band. " +
			"These are suggestions to mention as worth a look, never a diagnosis and never a cause. It also says when the normal was learned and counts the " +
			"minutes it could not judge, with the reason (not running steadily, too little history for a band, a reading missing). If there is no learned normal, " +
			"learned_normal_missing says why: never fill it in. A stretch the operator has answered carries operator_answer (real, normal or unknown). " +
			"Times are boat time with the UTC instant beside them.",
		Parameters: json.RawMessage(`{"type": "object", "properties": {` +
			`"since_iso": {"type": "string", "description": "Start of the window as an RFC 3339 time. Default 24 hours ago; at most 7 days back."},` +
			`"subject": {"type": "string", "description": "One engine, by its name or instance, or the house bank (\"house bank\"). Default: every engine and the house bank."},` +
			`"limit": {"type": "integer", "description": "How many stretches to return, 1 to 10. Default 5."}}}`),
	},
	execute: plainTool(assistantToolDeps.executeGetUnusualEngineReadings),
	describe: func(json.RawMessage) string {
		return "Checking for unusual readings…"
	},
}
