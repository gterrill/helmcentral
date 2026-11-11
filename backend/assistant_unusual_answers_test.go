package main

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"
)

// Mate sees the operator's answer on a stretch, so a stretch already answered
// normal is not raised again (ADR 0276, Feedback).

func TestUnusualTool_ShowsTheOperatorsAnswerOnAStretch(t *testing.T) {
	answered := oilStretch(time.Date(2026, 11, 9, 16, 10, 0, 0, time.UTC), 30)
	fresh := oilStretch(time.Date(2026, 11, 9, 19, 10, 0, 0, time.UTC), 10)
	d, _ := unusualToolDeps(unusualResultWith(answered, fresh), true)
	d.unusualAnswers = func() ([]normalAnswer, error) {
		return []normalAnswer{{StretchID: answered.ID, Subsystem: "engines", Subject: "port", From: answered.Start, To: answered.End, Answer: normalAnswerNormal}}, nil
	}
	got, raw := runUnusualTool(t, d, `{}`)
	stretches := got["stretches"].([]any)
	if len(stretches) != 2 {
		t.Fatalf("stretches: %s", raw)
	}
	byID := map[string]map[string]any{}
	for _, s := range stretches {
		m := s.(map[string]any)
		byID[m["id"].(string)] = m
	}
	if byID[answered.ID]["operator_answer"] != "normal" {
		t.Fatalf("the answered stretch should say so: %s", raw)
	}
	if _, has := byID[fresh.ID]["operator_answer"]; has {
		t.Fatalf("a stretch nobody answered carries no answer: %s", raw)
	}
	if !strings.Contains(got["note"].(string), "operator_answer") {
		t.Fatalf("the standing note should tell Mate what to do with an answer: %v", got["note"])
	}
}

func TestUnusualTool_AnswersThatCannotBeReadStopTheCall(t *testing.T) {
	useFreshBoatZone(t)
	d, _ := unusualToolDeps(unusualResultWith(oilStretch(time.Date(2026, 11, 9, 16, 10, 0, 0, time.UTC), 30)), true)
	d.unusualAnswers = func() ([]normalAnswer, error) { return nil, errors.New("parsing answers.json: bad") }
	_, err := d.executeGetUnusualEngineReadings(context.Background(), json.RawMessage(`{}`))
	if err == nil || !strings.Contains(err.Error(), "answers") {
		t.Fatalf("err = %v", err)
	}
	d.unusualAnswers = nil
	if _, err := d.executeGetUnusualEngineReadings(context.Background(), json.RawMessage(`{}`)); err == nil {
		t.Fatal("an unwired answers store is an error, not no answers")
	}
}

func TestProductionToolDepsWireTheAnswers(t *testing.T) {
	if assistantProductionToolDeps("../settings.yaml").unusualAnswers == nil {
		t.Fatal("production deps must wire unusualAnswers")
	}
}

// A stretch is the same trouble when its start moves a minute or two, so an
// answer counts for any stretch it overlaps, and the latest answer wins.
func TestUnusualTool_AnAnswerCountsForAnyStretchItOverlaps(t *testing.T) {
	st := oilStretch(time.Date(2026, 11, 9, 16, 10, 0, 0, time.UTC), 30)
	other := oilStretch(time.Date(2026, 11, 9, 19, 10, 0, 0, time.UTC), 10)
	d, _ := unusualToolDeps(unusualResultWith(st, other), true)
	at := func(m int) time.Time { return time.Date(2026, 11, 9, 20, m, 0, 0, time.UTC) }
	d.unusualAnswers = func() ([]normalAnswer, error) {
		return []normalAnswer{
			// Overlaps the start of the stretch, answered first.
			{StretchID: "x", Subsystem: "engines", Subject: "port", From: st.Start.Add(-20 * time.Minute), To: st.Start.Add(5 * time.Minute), Answer: normalAnswerReal, AnsweredAt: at(0)},
			// Overlaps the end, answered later: this one wins.
			{StretchID: "y", Subsystem: "engines", Subject: "port", From: st.End.Add(-5 * time.Minute), To: st.End.Add(10 * time.Minute), Answer: normalAnswerNormal, AnsweredAt: at(30)},
			// Touching only at the end is not overlapping; another engine is not either.
			{StretchID: "z", Subsystem: "engines", Subject: "port", From: other.End, To: other.End.Add(time.Hour), Answer: normalAnswerReal, AnsweredAt: at(40)},
			{StretchID: "w", Subsystem: "engines", Subject: "starboard", From: other.Start, To: other.End, Answer: normalAnswerReal, AnsweredAt: at(50)},
		}, nil
	}
	got, raw := runUnusualTool(t, d, `{}`)
	byID := map[string]map[string]any{}
	for _, s := range got["stretches"].([]any) {
		m := s.(map[string]any)
		byID[m["id"].(string)] = m
	}
	if byID[st.ID]["operator_answer"] != "normal" {
		t.Fatalf("the latest overlapping answer should win: %s", raw)
	}
	if _, has := byID[other.ID]["operator_answer"]; has {
		t.Fatalf("an answer that only touches, or is for another engine, does not count: %s", raw)
	}
}

// The answers are read once and handed to the score, not read again inside it.
func TestUnusualTool_ReadsTheAnswersOnceAndHandsThemToTheScore(t *testing.T) {
	d, _ := unusualToolDeps(unusualResultWith(), true)
	reads := 0
	answers := []normalAnswer{{StretchID: "x", Subsystem: "engines", Subject: "port", Answer: normalAnswerNormal}}
	d.unusualAnswers = func() ([]normalAnswer, error) { reads++; return answers, nil }
	var given []normalAnswer
	inner := d.unusualReadings
	d.unusualReadings = func(ctx context.Context, since, until time.Time, subject unusualSubject, a []normalAnswer) unusualResult {
		given = a
		return inner(ctx, since, until, subject, a)
	}
	runUnusualTool(t, d, `{}`)
	if reads != 1 || len(given) != 1 || given[0].StretchID != "x" {
		t.Fatalf("read %d times, score was given %+v", reads, given)
	}
}

func TestGatherWatchReportInput_AnswersThatCannotBeReadAreTheReasonNothingWasScored(t *testing.T) {
	for name, wire := range map[string]func() ([]normalAnswer, error){
		"unreadable": func() ([]normalAnswer, error) { return nil, errors.New("parsing answers.json: bad") },
		"unwired":    nil,
	} {
		t.Run(name, func(t *testing.T) {
			env := newProposalEnv(t)
			d := statusTestDeps(t)
			d.alarmLog = func() *alarmLogStore { return nil }
			scored := false
			d.unusualReadings = func(context.Context, time.Time, time.Time, unusualSubject, []normalAnswer) unusualResult {
				scored = true
				return unusualResult{}
			}
			d.unusualAnswers = wire
			w := savedWatch{ID: "w1", Name: "Morning", Kind: watchKindScheduled, Sections: []string{"instruments"}, Thresholds: defaultWatchThresholds()}
			in, err := gatherWatchReportInput(context.Background(), d, env.docs, w, vesselStatusTestNow, time.UTC)
			if err != nil {
				t.Fatal(err)
			}
			if scored || in.Unusual == nil || in.Unusual.ProblemKind != unusualError || !strings.Contains(in.Unusual.Problem, "answers") {
				t.Fatalf("scored %v, block %+v", scored, in.Unusual)
			}
		})
	}
}
