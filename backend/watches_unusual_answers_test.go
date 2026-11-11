package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// A line of the Unusual block has a stretch beside it, so the operator can
// answer it, and the run read for the page carries the answers given so far
// (ADR 0276, Feedback).

func TestBuildWatchUnusual_StretchesSitBesideTheirLines(t *testing.T) {
	st := stretchAt(2, 30)
	st.Subsystem, st.ID = normalSubsystemEngines, stretchIDFor(normalSubsystemEngines, "port", st.Start)
	u := buildWatchUnusual(unusualResultWith(st, stretchAt(3, 20)), "metric", time.UTC, unusualNow)
	if u == nil || len(u.Lines) != 2 || len(u.Stretches) != len(u.Lines) {
		t.Fatalf("block = %+v", u)
	}
	s := u.Stretches[0]
	if s.StretchID != "engines:port@2026-11-10T02:10:00Z" || s.Subsystem != "engines" || s.Subject != "port" || s.SubjectName != "Port engine" ||
		!s.From.Equal(st.Start) || !s.To.Equal(st.End) {
		t.Fatalf("stretch = %+v", s)
	}
	if !strings.HasPrefix(u.Lines[0], "Port engine oil pressure ran low") {
		t.Fatalf("line = %q", u.Lines[0])
	}
}

func TestWatchReportForModel_DropsTheStretches(t *testing.T) {
	rep := watchReport{Kind: watchReportKindSections, Unusual: &watchUnusual{
		Lines:     []string{"Port engine oil pressure ran low."},
		Stretches: []watchUnusualStretch{{StretchID: "engines:port@x", Subsystem: "engines", Subject: "port", From: unusualNow, To: unusualNow.Add(time.Hour)}},
	}}
	stored, _ := json.Marshal(rep)
	got, err := watchReportForModel(stored)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(got), `"lines":["Port engine oil pressure ran low."]`) || strings.Contains(string(got), "stretch") {
		t.Fatalf("the model reads the sentence, not the bookkeeping: %s", got)
	}
}

func unusualRunWithStretches(ids ...string) watchRun {
	u := &watchUnusual{}
	for i, id := range ids {
		u.Lines = append(u.Lines, "line "+id)
		// Each stretch is an hour after the one before, on engine 0.
		from := answerFrom.Add(time.Duration(i) * time.Hour)
		u.Stretches = append(u.Stretches, watchUnusualStretch{StretchID: id, Subsystem: "engines", Subject: "0", From: from, To: from.Add(10 * time.Minute)})
	}
	return unusualRunWith(u)
}

func TestWatchRunsForPage_CarriesTheCurrentAnswerForEachStretch(t *testing.T) {
	path := filepath.Join(t.TempDir(), "answers.json")
	t.Setenv("ANOMALY_NORMAL_ANSWERS_FILE", path)
	if _, err := saveNormalAnswer(path, normalAnswer{StretchID: "engines:0@a", Subsystem: "engines", Subject: "0", From: answerFrom, To: answerFrom.Add(10 * time.Minute), Answer: normalAnswerReal}); err != nil {
		t.Fatal(err)
	}
	runs := []watchRun{
		unusualRunWithStretches("engines:0@a", "engines:0@b"),
		unusualRunWith(nil),
		{ID: "samples", ReportKind: watchReportKindSamples, Report: json.RawMessage(`{"kind":"samples","samples":[1]}`)},
	}
	reportBefore := string(runs[0].Report)
	watchRunsForPage(runs)
	if got := runs[0].UnusualAnswers; len(got) != 2 || got[0] != normalAnswerReal || got[1] != "" {
		t.Fatalf("answers = %q", got)
	}
	if runs[1].UnusualAnswers != nil || runs[2].UnusualAnswers != nil {
		t.Fatalf("a run with no stretch has no answers: %q %q", runs[1].UnusualAnswers, runs[2].UnusualAnswers)
	}
	if string(runs[0].Report) != reportBefore {
		t.Fatal("the answers ride beside the report, not inside it")
	}

	// Changing the answer shows on the next read.
	if _, err := saveNormalAnswer(path, normalAnswer{StretchID: "engines:0@a", Subsystem: "engines", Subject: "0", From: answerFrom, To: answerFrom.Add(10 * time.Minute), Answer: normalAnswerNormal, AnsweredAt: answerFrom.Add(time.Hour)}); err != nil {
		t.Fatal(err)
	}
	fresh := []watchRun{unusualRunWithStretches("engines:0@a")}
	watchRunsForPage(fresh)
	if fresh[0].UnusualAnswers[0] != normalAnswerNormal {
		t.Fatalf("answers = %q", fresh[0].UnusualAnswers)
	}
}

// Reading the answers must not fail the page: the runs still show, and the run
// says why its stretches cannot be answered.
func TestWatchRunsForPage_AnUnreadableStoreIsSaidOnTheRunsThatNeedIt(t *testing.T) {
	path := filepath.Join(t.TempDir(), "answers.json")
	t.Setenv("ANOMALY_NORMAL_ANSWERS_FILE", path)
	if err := os.WriteFile(path, []byte("{not json"), 0o600); err != nil {
		t.Fatal(err)
	}
	old := watchRun{ID: "old", ReportKind: watchReportKindSections, Report: json.RawMessage(`{"kind":"sections","unusual":{"lines":["An old sentence."],"judged_minutes":9}}`)}
	runs := []watchRun{unusualRunWithStretches("engines:0@a"), old, unusualRunWith(nil)}
	watchRunsForPage(runs)
	if p := runs[0].UnusualAnswersProblem; !strings.HasPrefix(p, "Your answers could not be read: ") || !strings.Contains(p, "answers.json") {
		t.Fatalf("problem = %q", p)
	}
	if runs[0].UnusualAnswers != nil {
		t.Fatalf("no answers when they cannot be read: %q", runs[0].UnusualAnswers)
	}
	if runs[1].UnusualAnswersProblem != "" || runs[2].UnusualAnswersProblem != "" {
		t.Fatalf("a run with nothing to answer has nothing to say: %q %q", runs[1].UnusualAnswersProblem, runs[2].UnusualAnswersProblem)
	}
}

func TestWatchRunsForPage_AReportThatWillNotParseSpoilsOnlyItsOwnRun(t *testing.T) {
	path := filepath.Join(t.TempDir(), "answers.json")
	t.Setenv("ANOMALY_NORMAL_ANSWERS_FILE", path)
	if _, err := saveNormalAnswer(path, normalAnswer{StretchID: "engines:0@a", Subsystem: "engines", Subject: "0", From: answerFrom, To: answerFrom.Add(10 * time.Minute), Answer: normalAnswerReal}); err != nil {
		t.Fatal(err)
	}
	broken := watchRun{ID: "broken", ReportKind: watchReportKindSections, Report: json.RawMessage(`{"unusual":`)}
	runs := []watchRun{broken, unusualRunWithStretches("engines:0@a")}
	watchRunsForPage(runs)
	if !strings.HasPrefix(runs[0].UnusualAnswersProblem, "This report could not be read: ") || strings.Contains(runs[0].UnusualAnswersProblem, "answers") {
		t.Fatalf("a bad report is not the answers' fault: %q", runs[0].UnusualAnswersProblem)
	}
	if runs[1].UnusualAnswersProblem != "" || len(runs[1].UnusualAnswers) != 1 || runs[1].UnusualAnswers[0] != normalAnswerReal {
		t.Fatalf("the other run is fine: %+v", runs[1])
	}
}

// The same trouble scored again starts a minute or two elsewhere, so the page
// joins an answer to any stretch it overlaps, not only to the exact id.
func TestWatchRunsForPage_AnAnswerCountsForALaterReportsOverlappingStretch(t *testing.T) {
	path := filepath.Join(t.TempDir(), "answers.json")
	t.Setenv("ANOMALY_NORMAL_ANSWERS_FILE", path)
	from := time.Date(2026, 11, 9, 14, 0, 0, 0, time.UTC)
	answered := func(id string, to time.Duration, answer string, at time.Time) normalAnswer {
		return normalAnswer{StretchID: id, Subsystem: "engines", Subject: "0", From: from, To: from.Add(to), Answer: answer, AnsweredAt: at}
	}
	if _, err := saveNormalAnswer(path, answered("engines:0@old", 20*time.Minute, normalAnswerReal, from)); err != nil {
		t.Fatal(err)
	}
	if _, err := saveNormalAnswer(path, answered("engines:0@newer", 25*time.Minute, normalAnswerNormal, from.Add(time.Hour))); err != nil {
		t.Fatal(err)
	}
	stretch := func(subject string, start time.Duration, minutes time.Duration) watchUnusualStretch {
		return watchUnusualStretch{StretchID: "engines:" + subject + "@later", Subsystem: "engines", Subject: subject, From: from.Add(start), To: from.Add(start + minutes*time.Minute)}
	}
	run := unusualRunWith(&watchUnusual{
		Lines: []string{"a", "b", "c", "d"},
		Stretches: []watchUnusualStretch{
			stretch("0", 3*time.Minute, 20),  // starts 3 minutes into the answered range
			stretch("0", 24*time.Minute, 10), // overlaps only the later, longer answer
			stretch("0", 25*time.Minute, 10), // starts where the answered range ends
			stretch("1", 3*time.Minute, 20),  // another engine
		},
	})
	runs := []watchRun{run}
	watchRunsForPage(runs)
	want := []string{normalAnswerNormal, normalAnswerNormal, "", ""}
	if got := runs[0].UnusualAnswers; len(got) != 4 || got[0] != want[0] || got[1] != want[1] || got[2] != want[2] || got[3] != want[3] {
		t.Fatalf("answers = %q, want %q", got, want)
	}
}

// A stretch shows the answer that covers most of it; on a tie the later answer.
func TestOverlappingAnswer_TheAnswerCoveringMostOfAStretchWins(t *testing.T) {
	path := filepath.Join(t.TempDir(), "answers.json")
	base := time.Date(2026, 11, 9, 10, 0, 0, 0, time.UTC)
	mk := func(id string, from, to int, answer string, at int) normalAnswer {
		return normalAnswer{StretchID: id, Subsystem: "engines", Subject: "0", Answer: answer, From: base.Add(time.Duration(from) * time.Minute), To: base.Add(time.Duration(to) * time.Minute), AnsweredAt: base.Add(time.Duration(at) * time.Hour)}
	}
	// A [10:00,10:30) is called real, then B [10:20,10:40) normal.
	for _, a := range []normalAnswer{mk("A", 0, 30, normalAnswerReal, 0), mk("B", 20, 40, normalAnswerNormal, 1)} {
		if _, err := saveNormalAnswer(path, a); err != nil {
			t.Fatal(err)
		}
	}
	answers, err := loadNormalAnswersList(path)
	if err != nil {
		t.Fatal(err)
	}
	at := func(m int) time.Time { return base.Add(time.Duration(m) * time.Minute) }
	if got := overlappingAnswer(answers, "engines", "0", at(0), at(30)); got != normalAnswerReal {
		t.Fatalf("stretch A shows %q, want real: 20 of its 30 minutes were called real", got)
	}
	if got := overlappingAnswer(answers, "engines", "0", at(20), at(40)); got != normalAnswerNormal {
		t.Fatalf("stretch B shows %q, want normal", got)
	}
	// A tie goes to the later answer: 10:15-10:25 is 5 minutes of each.
	if got := overlappingAnswer(answers, "engines", "0", at(15), at(25)); got != normalAnswerNormal {
		t.Fatalf("a tie shows %q, want the later answer, normal", got)
	}
}
