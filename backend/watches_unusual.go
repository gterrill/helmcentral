package main

import (
	"fmt"
	"strings"
	"time"
)

/*
The watch report's Unusual block (ADR 0276).

It is a learned normal's suggestion, not a rule. It is a field of the report and
never a section or a finding, so watchIssueCount cannot count it, the push
title does not change and Mate cannot raise it into an issue. At most two lines
show; a case with nothing to show hides the block.
*/

// watchUnusualMaxLines is how many stretches the block names.
const watchUnusualMaxLines = 2

// watchUnusual is the block. Lines are whole sentences, Note one more sentence
// saying what was left out or why nothing could be judged.
type watchUnusual struct {
	Lines         []string `json:"lines,omitempty"`
	Note          string   `json:"note,omitempty"`
	JudgedMinutes int      `json:"judged_minutes"`
}

// endedSentence is s with exactly one full stop at the end.
func endedSentence(s string) string { return strings.TrimRight(strings.TrimSpace(s), ".") + "." }

// unusualProblemNote is the sentence for why no judgement could be made, by
// problem kind. The report, the tool and Settings all say it this way.
func unusualProblemNote(kind, problem string) string {
	switch kind {
	case unusualNoInflux, unusualNoModel:
		return "No learned normal: " + endedSentence(problem)
	case unusualNeverTrained:
		return normalNoModelYet
	case unusualUnreadable:
		return "The learned normal could not be read: " + endedSentence(problem)
	case unusualError:
		return "Unusual readings could not be checked: " + endedSentence(problem)
	}
	return ""
}

// unusualTrainingNote is the sentence for a latest training run that failed
// while an older model is still in use.
func unusualTrainingNote(problem string) string {
	return "The learned normal was not updated: " + endedSentence(problem)
}

// unusualClippedNote is said when a window longer than a week was cut to its
// newest 7 days.
const unusualClippedNote = "Only the last 7 days were judged."

// unusualMoreNote says how many stretches a list left out, or "" for none.
func unusualMoreNote(more int) string {
	switch {
	case more <= 0:
		return ""
	case more == 1:
		return "1 more unusual stretch is not shown."
	}
	return fmt.Sprintf("%d more unusual stretches are not shown.", more)
}

// buildWatchUnusual turns a scored window into the block, or nil when there is
// nothing to say. system is the operator's units, from the boat's status.
func buildWatchUnusual(res unusualResult, system string, loc *time.Location, now time.Time) *watchUnusual {
	if res.ProblemKind != "" {
		return &watchUnusual{Note: unusualProblemNote(res.ProblemKind, res.Problem)}
	}

	u := &watchUnusual{JudgedMinutes: res.JudgedMinutes}
	var notes []string
	for i, st := range res.Stretches {
		if i == watchUnusualMaxLines {
			notes = append(notes, unusualMoreNote(len(res.Stretches)-watchUnusualMaxLines))
			break
		}
		v, err := describeUnusualStretch(st, system, loc, now)
		if err != nil {
			return &watchUnusual{Note: "Unusual readings could not be worded: " + endedSentence(err.Error()), JudgedMinutes: res.JudgedMinutes}
		}
		u.Lines = append(u.Lines, v.Summary)
	}
	if len(res.Stretches) == 0 && res.JudgedMinutes == 0 {
		if why := unusualNotJudgedNote(res.NotJudged); why != "" {
			notes = append(notes, why)
		}
	}
	// An engine that could not be judged is said even when the other was.
	for _, n := range res.NotJudged {
		switch n.Kind {
		case "engine_problem":
			notes = append(notes, endedSentence(n.SubjectName+": "+n.Reason))
		case "sender_silent":
			// The reason begins with the reading: "Port engine oil pressure: the sender ...".
			notes = append(notes, endedSentence(n.SubjectName+" "+n.Reason))
		}
	}
	if res.Clipped {
		notes = append(notes, unusualClippedNote)
	}
	if res.TrainingProblem != "" {
		notes = append(notes, unusualTrainingNote(res.TrainingProblem))
	}
	u.Note = strings.Join(notes, " ")
	if len(u.Lines) == 0 && u.Note == "" {
		return nil
	}
	return u
}

// unusualNotJudgedNote says why steady running minutes were not judged: a band
// with too little history, or a reading that was missing. Minutes that were not
// running steadily are not a reason to say anything.
func unusualNotJudgedNote(items []unusualNotJudged) string {
	var parts []string
	for _, n := range items {
		switch n.Kind {
		case "thin_band":
			parts = append(parts, n.Short)
		case "missing_reading":
			parts = append(parts, fmt.Sprintf("%s for %s", n.Reason, plural(n.Minutes, "minute", "minutes")))
		case "switched_off":
			parts = append(parts, n.Short)
		}
	}
	if len(parts) == 0 {
		return ""
	}
	if len(parts) > 2 {
		parts = parts[:2]
	}
	return "Not judged: " + strings.Join(parts, "; ") + "."
}
