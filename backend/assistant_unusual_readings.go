package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
)

/*
get_unusual_engine_readings (ADR 0276): the stretches of a past window in which
an engine's readings were unusual for the engine, against a normal learned from
its own history. A read-only suggestion for Mate and for outside assistants to
mention as worth a look, never a diagnosis and never a cause.
*/

const (
	unusualToolDefaultSince = 24 * time.Hour
	unusualToolDefaultLimit = 5
	unusualToolMaxLimit     = 10
)

type unusualToolArgs struct {
	SinceISO string `json:"since_iso"`
	Engine   string `json:"engine"`
	Limit    *int   `json:"limit"`
}

type unusualToolLearned struct {
	Learned    string         `json:"learned"`
	LearnedISO string         `json:"learned_iso"`
	Source     string         `json:"source"`
	Bands      map[string]int `json:"bands_by_engine"`
}

type unusualToolNotJudged struct {
	Engine  string `json:"engine,omitempty"`
	Reason  string `json:"reason"`
	Minutes int    `json:"minutes"`
}

type unusualToolResult struct {
	Engine               string                 `json:"engine,omitempty"`
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

const unusualToolNote = "These are suggestions from a normal learned from this engine's own history, not a rule and not a diagnosis. " +
	"Mention one as worth a look, in the operator's units, and never give a cause for it. Minutes that were not judged are counted in not_judged: never fill them in."

func (d assistantToolDeps) executeGetUnusualEngineReadings(ctx context.Context, raw json.RawMessage) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	if d.unusualReadings == nil || d.now == nil {
		return "", errors.New("get_unusual_engine_readings: the learned normal is not wired")
	}
	var args unusualToolArgs
	if len(raw) > 0 {
		if err := json.Unmarshal(raw, &args); err != nil {
			return "", fmt.Errorf("parse get_unusual_engine_readings arguments: %w", err)
		}
	}
	limit := unusualToolDefaultLimit
	if args.Limit != nil {
		limit = *args.Limit
		if limit < 1 || limit > unusualToolMaxLimit {
			return "", fmt.Errorf("get_unusual_engine_readings: limit must be from 1 to %d", unusualToolMaxLimit)
		}
	}
	now := d.now().UTC()
	since := now.Add(-unusualToolDefaultSince)
	if s := strings.TrimSpace(args.SinceISO); s != "" {
		t, err := time.Parse(time.RFC3339, s)
		if err != nil {
			return "", fmt.Errorf("get_unusual_engine_readings: since_iso %q is not an RFC 3339 time such as 2026-11-09T16:00:00Z", s)
		}
		if !t.Before(now) {
			return "", errors.New("get_unusual_engine_readings: since_iso must be in the past")
		}
		since = t.UTC()
	}

	engineID, engineName, err := d.unusualEngine(args.Engine)
	if err != nil {
		return "", err
	}
	// The figures are worded in the operator's units, so units that cannot be
	// read stop the call rather than fall back to a system.
	if d.displayUnits == nil {
		return "", errors.New("get_unusual_engine_readings: the display units are not wired")
	}
	system, err := d.displayUnits()
	if err != nil {
		return "", fmt.Errorf("get_unusual_engine_readings: the display units could not be read: %w", err)
	}
	res := d.unusualReadings(ctx, since, now, engineID)
	if err := ctx.Err(); err != nil {
		return "", err
	}
	if !res.Since.IsZero() {
		since = res.Since
	}

	loc, zone, tzNote := boatTimeNote(d.boatTime(), since, now)
	out := unusualToolResult{
		Engine: engineName,
		Since:  boatDayClock(since, loc), SinceISO: since.Format(time.RFC3339),
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
			return "", fmt.Errorf("get_unusual_engine_readings: %w", err)
		}
		out.Stretches = append(out.Stretches, v)
	}
	for _, n := range res.NotJudged {
		out.NotJudged = append(out.NotJudged, unusualToolNotJudged{Engine: n.SubjectName, Reason: n.Reason, Minutes: n.Minutes})
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

// unusualEngine resolves the engine argument to a propulsion instance and its
// name: the instance, the operator's name for it or that name with "engine".
func (d assistantToolDeps) unusualEngine(arg string) (id, name string, err error) {
	arg = strings.TrimSpace(arg)
	if arg == "" {
		return "", "", nil
	}
	if d.vesselView == nil {
		return "", "", errors.New("get_unusual_engine_readings: the engines are not wired, so an engine cannot be named")
	}
	vessel, err := d.vesselView()
	if err != nil {
		return "", "", fmt.Errorf("get_unusual_engine_readings: vessel settings: %w", err)
	}
	var known []string
	for _, s := range normalEngineSubjects(vessel) {
		known = append(known, s.Name)
		if strings.EqualFold(arg, s.ID) || strings.EqualFold(arg, s.Name) || strings.EqualFold(arg+" engine", s.Name) {
			return s.ID, s.Name, nil
		}
	}
	return "", "", fmt.Errorf("get_unusual_engine_readings: no engine is called %q (the engines are: %s)", arg, strings.Join(known, ", "))
}

func unusualLearned(m *unusualModelInfo, loc *time.Location) *unusualToolLearned {
	return &unusualToolLearned{
		Learned: boatDayClock(m.TrainedAt, loc), LearnedISO: m.TrainedAt.UTC().Format(time.RFC3339),
		Source: normalSourceText(m.Source), Bands: m.Bands,
	}
}

var mateToolGetUnusualEngineReadings = mateTool{
	name:      "get_unusual_engine_readings",
	effect:    effectReads,
	audiences: audienceReviewsAndMCP,
	function: openRouterFunctionDef{
		Description: "Stretches of the last day (or up to 7 days) in which an engine's readings were unusual for that engine, judged against a normal " +
			"learned from the engine's own history at the same rpm. Each stretch is at least three minutes in a row, with the reading that stood out " +
			"most, its value against the usual one in the operator's units, and the rpm band. These are suggestions to mention as worth a look, " +
			"never a diagnosis and never a cause. It also says when the normal was learned and counts the minutes it could not judge, with the " +
			"reason (not running steadily, too little history for a band, a reading missing). If there is no learned normal, " +
			"learned_normal_missing says why: never fill it in. Times are boat time with the UTC instant beside them.",
		Parameters: json.RawMessage(`{"type": "object", "properties": {` +
			`"since_iso": {"type": "string", "description": "Start of the window as an RFC 3339 time. Default 24 hours ago; at most 7 days back."},` +
			`"engine": {"type": "string", "description": "One engine, by its name or instance. Default: every engine."},` +
			`"limit": {"type": "integer", "description": "How many stretches to return, 1 to 10. Default 5."}}}`),
	},
	execute: plainTool(assistantToolDeps.executeGetUnusualEngineReadings),
	describe: func(json.RawMessage) string {
		return "Checking for unusual engine readings…"
	},
}
