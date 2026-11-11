package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/labstack/echo/v4"
)

// The operator's answers about stretches of the learned normal (ADR 0276,
// feedback): real, normal or don't know, kept by stretch id.

var answerFrom = time.Date(2026, 11, 9, 14, 0, 0, 0, time.UTC)

// answerStretch is the stretch the stored run's first line is about.
func answerStretch() watchUnusualStretch {
	return watchUnusualStretch{
		StretchID: stretchIDFor(normalSubsystemEngines, "0", answerFrom), Subsystem: normalSubsystemEngines, Subject: "0", SubjectName: "Port engine",
		From: answerFrom, To: answerFrom.Add(20 * time.Minute),
	}
}

func answerRequest() normalAnswerRequest {
	return normalAnswerRequest{WatchID: "w1", RunID: "r1", Index: 0, Answer: normalAnswerReal}
}

func answerDeps(t *testing.T) (normalAnswerDeps, *int) {
	t.Helper()
	woke := 0
	return normalAnswerDeps{
		Now: func() time.Time { return answerFrom.Add(2 * time.Hour) },
		Vessel: func() (vesselView, error) {
			return vesselView{Engines: []vesselEngineSetting{{Instance: "0", Name: "Port"}}}, nil
		},
		Run: func(watchID, runID string) (watchRun, error) {
			if watchID != "w1" || runID != "r1" {
				return watchRun{}, errWatchRunNotFound
			}
			return unusualRunWith(&watchUnusual{Lines: []string{"line"}, Stretches: []watchUnusualStretch{answerStretch()}}), nil
		},
		Path: func() string { return filepath.Join(t.TempDir(), "answers.json") },
		Wake: func() { woke++ },
	}, &woke
}

func TestNormalAnswers_StoreKeepsTheLatestAnswerPerStretch(t *testing.T) {
	path := filepath.Join(t.TempDir(), "answers.json")
	if got, err := loadNormalAnswers(path); err != nil || len(got) != 0 {
		t.Fatalf("a missing file is no answers: %v %v", got, err)
	}
	a := normalAnswer{StretchID: "engines:0@x", Subsystem: "engines", Subject: "0", From: answerFrom, To: answerFrom.Add(time.Hour), Answer: normalAnswerReal, AnsweredAt: answerFrom}
	if replaced, err := saveNormalAnswer(path, a); err != nil || len(replaced) != 0 {
		t.Fatalf("first save: replaced %q err %v", replaced, err)
	}
	a.Answer, a.AnsweredAt = normalAnswerNormal, answerFrom.Add(time.Hour)
	if replaced, err := saveNormalAnswer(path, a); err != nil || len(replaced) != 1 || replaced[0] != normalAnswerReal {
		t.Fatalf("second save: replaced %q err %v, want real", replaced, err)
	}
	got, err := loadNormalAnswers(path)
	if err != nil || len(got) != 1 || got["engines:0@x"].Answer != normalAnswerNormal || !got["engines:0@x"].AnsweredAt.Equal(a.AnsweredAt) {
		t.Fatalf("answers = %+v err %v", got, err)
	}
}

func TestNormalAnswers_AFileThatDoesNotParseIsAnErrorAndLeftAlone(t *testing.T) {
	path := filepath.Join(t.TempDir(), "answers.json")
	if err := os.WriteFile(path, []byte("{not json"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := loadNormalAnswers(path); err == nil {
		t.Fatal("a corrupt file must be an error")
	}
	if _, err := saveNormalAnswer(path, normalAnswer{StretchID: "a"}); err == nil {
		t.Fatal("a save must not overwrite a corrupt file")
	}
	if raw, _ := os.ReadFile(path); string(raw) != "{not json" {
		t.Fatalf("file was changed: %s", raw)
	}
}

func TestRecordNormalAnswer_Validates(t *testing.T) {
	cases := []struct {
		name string
		edit func(r *normalAnswerRequest, d *normalAnswerDeps)
		want string
	}{
		{"unknown answer", func(r *normalAnswerRequest, _ *normalAnswerDeps) { r.Answer = "maybe" }, "answer must be real, normal or unknown"},
		{"run that is not there", func(r *normalAnswerRequest, _ *normalAnswerDeps) { r.RunID = "gone" }, "That report is no longer there"},
		{"stretch past the end", func(r *normalAnswerRequest, _ *normalAnswerDeps) { r.Index = 1 }, "no such stretch"},
		{"negative stretch", func(r *normalAnswerRequest, _ *normalAnswerDeps) { r.Index = -1 }, "no such stretch"},
		{"run with no stretches", func(_ *normalAnswerRequest, d *normalAnswerDeps) {
			d.Run = func(string, string) (watchRun, error) {
				return unusualRunWith(&watchUnusual{Lines: []string{"old"}}), nil
			}
		}, "no such stretch"},
		{"engine no longer set up", func(_ *normalAnswerRequest, d *normalAnswerDeps) {
			d.Vessel = func() (vesselView, error) {
				return vesselView{Engines: []vesselEngineSetting{{Instance: "1", Name: "Starboard"}}}, nil
			}
		}, "This engine is no longer set up. It may have been renumbered or removed."},
		{"subsystem that is gone", func(_ *normalAnswerRequest, d *normalAnswerDeps) {
			d.Run = func(string, string) (watchRun, error) {
				st := answerStretch()
				st.Subsystem = "house"
				return unusualRunWith(&watchUnusual{Lines: []string{"x"}, Stretches: []watchUnusualStretch{st}}), nil
			}
		}, "no longer learned"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			deps, woke := answerDeps(t)
			req := answerRequest()
			c.edit(&req, &deps)
			_, err := recordNormalAnswer(deps, req)
			var bad *normalAnswerError
			if !errors.As(err, &bad) || !strings.Contains(bad.Error(), c.want) {
				t.Fatalf("err = %v, want it to contain %q", err, c.want)
			}
			if *woke != 0 {
				t.Fatal("a refused answer must not wake the trainer")
			}
		})
	}
}

func TestRecordNormalAnswer_TakesTheTimesFromTheStoredRunNotTheClient(t *testing.T) {
	deps, _ := answerDeps(t)
	path := deps.Path()
	deps.Path = func() string { return path }
	saved, err := recordNormalAnswer(deps, answerRequest())
	if err != nil {
		t.Fatal(err)
	}
	st := answerStretch()
	if saved.StretchID != st.StretchID || saved.Subsystem != st.Subsystem || saved.Subject != st.Subject || !saved.From.Equal(st.From) || !saved.To.Equal(st.To) {
		t.Fatalf("saved = %+v, want the stretch of the stored run %+v", saved, st)
	}
	var body map[string]any
	raw, _ := json.Marshal(answerRequest())
	_ = json.Unmarshal(raw, &body)
	for _, k := range []string{"from", "to", "subject", "subsystem", "stretch_id"} {
		if _, has := body[k]; has {
			t.Fatalf("the client never sends %s", k)
		}
	}
}

func TestRecordNormalAnswer_SavesAndWakesOnlyForRealAndNormal(t *testing.T) {
	for answer, wakes := range map[string]int{normalAnswerReal: 1, normalAnswerNormal: 1, normalAnswerUnknown: 0} {
		deps, woke := answerDeps(t)
		path := deps.Path()
		deps.Path = func() string { return path }
		req := answerRequest()
		req.Answer = answer
		saved, err := recordNormalAnswer(deps, req)
		if err != nil {
			t.Fatalf("%s: %v", answer, err)
		}
		if saved.Answer != answer || !saved.AnsweredAt.Equal(deps.Now()) {
			t.Fatalf("%s: saved = %+v", answer, saved)
		}
		if *woke != wakes {
			t.Fatalf("%s: woke %d times, want %d", answer, *woke, wakes)
		}
		got, err := loadNormalAnswers(path)
		if err != nil || got[answerStretch().StretchID].Answer != answer {
			t.Fatalf("%s: stored = %+v err %v", answer, got, err)
		}
	}
}

func TestRecordNormalAnswer_ChangingTheAnswerWakesTheTrainerAgain(t *testing.T) {
	deps, woke := answerDeps(t)
	path := deps.Path()
	deps.Path = func() string { return path }
	req := answerRequest()
	if _, err := recordNormalAnswer(deps, req); err != nil {
		t.Fatal(err)
	}
	req.Answer = normalAnswerUnknown
	if _, err := recordNormalAnswer(deps, req); err != nil {
		t.Fatal(err)
	}
	// Real to unknown puts the minutes back into training, so it wakes it too.
	if *woke != 2 {
		t.Fatalf("woke %d times, want 2", *woke)
	}
}

func TestNormalAnswerHandler_PutsTheAnswer(t *testing.T) {
	deps, _ := answerDeps(t)
	path := deps.Path()
	deps.Path = func() string { return path }
	body, _ := json.Marshal(answerRequest())
	e := echo.New()
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPut, "/api/anomaly/normal/answers", strings.NewReader(string(body)))
	req.Header.Set("Content-Type", "application/json")
	if err := normalAnswerHandler(deps)(e.NewContext(req, rec)); err != nil {
		t.Fatal(err)
	}
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"answer":"real"`) {
		t.Fatalf("code %d body %s", rec.Code, rec.Body)
	}

	bad := httptest.NewRequest(http.MethodPut, "/api/anomaly/normal/answers", strings.NewReader(`{"watch_id":"w1","run_id":"r1","index":0,"answer":"maybe"}`))
	bad.Header.Set("Content-Type", "application/json")
	rec = httptest.NewRecorder()
	_ = normalAnswerHandler(deps)(e.NewContext(bad, rec))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("code %d body %s", rec.Code, rec.Body)
	}

	// A store that cannot be read is a server error, never an answer lost quietly.
	if err := os.WriteFile(path, []byte("{not json"), 0o600); err != nil {
		t.Fatal(err)
	}
	again := httptest.NewRequest(http.MethodPut, "/api/anomaly/normal/answers", strings.NewReader(string(body)))
	again.Header.Set("Content-Type", "application/json")
	rec = httptest.NewRecorder()
	_ = normalAnswerHandler(deps)(e.NewContext(again, rec))
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("code %d body %s", rec.Code, rec.Body)
	}
}

func TestNormalAnswerRouteIsWriteTier(t *testing.T) {
	for _, r := range anomalyNormalRoutes() {
		if r.Path == "/api/anomaly/normal/answers" && r.Method == http.MethodPut && r.Tier == tierWrite {
			return
		}
	}
	t.Fatal("PUT /api/anomaly/normal/answers must be a write-tier route")
}

// --- one consistent set of ranges ----------------------------------------------

func rangeAnswer(id string, from, to int, answer string, at int) normalAnswer {
	base := time.Date(2026, 11, 9, 10, 0, 0, 0, time.UTC)
	return normalAnswer{
		StretchID: id, Subsystem: "engines", Subject: "port", Answer: answer,
		From: base.Add(time.Duration(from) * time.Minute), To: base.Add(time.Duration(to) * time.Minute),
		AnsweredAt: base.Add(time.Duration(at) * time.Hour),
	}
}

// minutesOf lists each answer as "from-to answer" in minutes after 10:00, in order.
func minutesOf(t *testing.T, path string) []string {
	t.Helper()
	got, err := loadNormalAnswersList(path)
	if err != nil {
		t.Fatal(err)
	}
	base := time.Date(2026, 11, 9, 10, 0, 0, 0, time.UTC)
	var out []string
	for _, a := range got {
		out = append(out, fmt.Sprintf("%d-%d %s", int(a.From.Sub(base)/time.Minute), int(a.To.Sub(base)/time.Minute), a.Answer))
	}
	sort.Strings(out)
	return out
}

func TestSaveNormalAnswer_ALaterAnswerReplacesTheOverlapOfAnEarlierOne(t *testing.T) {
	cases := []struct {
		name  string
		saves []normalAnswer
		want  []string
	}{
		{"normal over the end of a real", []normalAnswer{rangeAnswer("a", 0, 30, normalAnswerReal, 0), rangeAnswer("b", 1, 31, normalAnswerNormal, 1)}, []string{"0-1 real", "1-31 normal"}},
		{"real over the start of a normal, the other way round", []normalAnswer{rangeAnswer("b", 1, 31, normalAnswerNormal, 0), rangeAnswer("a", 0, 30, normalAnswerReal, 1)}, []string{"0-30 real", "30-31 normal"}},
		{"normal inside a real splits it", []normalAnswer{rangeAnswer("a", 0, 30, normalAnswerReal, 0), rangeAnswer("b", 10, 20, normalAnswerNormal, 1)}, []string{"0-10 real", "10-20 normal", "20-30 real"}},
		{"an answer that covers an earlier one removes it", []normalAnswer{rangeAnswer("a", 5, 10, normalAnswerReal, 0), rangeAnswer("b", 0, 30, normalAnswerNormal, 1)}, []string{"0-30 normal"}},
		{"ranges that only touch stay as they are", []normalAnswer{rangeAnswer("a", 0, 10, normalAnswerReal, 0), rangeAnswer("b", 10, 20, normalAnswerNormal, 1)}, []string{"0-10 real", "10-20 normal"}},
		{"the same stretch answered again", []normalAnswer{rangeAnswer("a", 0, 30, normalAnswerReal, 0), rangeAnswer("a", 0, 30, normalAnswerNormal, 1)}, []string{"0-30 normal"}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "answers.json")
			for _, a := range c.saves {
				if _, err := saveNormalAnswer(path, a); err != nil {
					t.Fatal(err)
				}
			}
			got := minutesOf(t, path)
			if strings.Join(got, ",") != strings.Join(c.want, ",") {
				t.Fatalf("answers = %v, want %v", got, c.want)
			}
		})
	}
}

func TestSaveNormalAnswer_OnlyTheSameEngineIsReplaced(t *testing.T) {
	path := filepath.Join(t.TempDir(), "answers.json")
	other := rangeAnswer("s", 0, 30, normalAnswerReal, 0)
	other.Subject = "starboard"
	if _, err := saveNormalAnswer(path, other); err != nil {
		t.Fatal(err)
	}
	if _, err := saveNormalAnswer(path, rangeAnswer("p", 0, 30, normalAnswerNormal, 1)); err != nil {
		t.Fatal(err)
	}
	if got := minutesOf(t, path); len(got) != 2 {
		t.Fatalf("answers = %v", got)
	}
}

func TestSaveNormalAnswer_ReportsWhatItReplaced(t *testing.T) {
	path := filepath.Join(t.TempDir(), "answers.json")
	if _, err := saveNormalAnswer(path, rangeAnswer("a", 0, 30, normalAnswerReal, 0)); err != nil {
		t.Fatal(err)
	}
	// A different id, overlapping: what it replaced is still what changed.
	if replaced, err := saveNormalAnswer(path, rangeAnswer("b", 20, 40, normalAnswerNormal, 1)); err != nil || len(replaced) != 1 || replaced[0] != normalAnswerReal {
		t.Fatalf("replaced %q err %v", replaced, err)
	}
}

// Training reads the one set, so a real over part of a normal and a normal over
// part of a real each mean what was said last.
func TestTrain_OverlappingAnswersAreOneSetWhateverTheOrder(t *testing.T) {
	feed, r1, _, _ := threeRunFeed()
	at := func(m int) time.Time { return r1.Add(time.Duration(m) * time.Minute) }
	mk := func(id string, from, to int, answer string, hours int) normalAnswer {
		a := answerFor(id, at(from), to-from, answer)
		a.AnsweredAt = r1.Add(time.Duration(hours) * time.Hour)
		return a
	}
	for name, saves := range map[string][]normalAnswer{
		"real then normal": {mk("a", 20, 50, normalAnswerReal, 0), mk("b", 21, 51, normalAnswerNormal, 1)},
		"normal then real": {mk("b", 21, 51, normalAnswerNormal, 0), mk("a", 20, 50, normalAnswerReal, 1)},
	} {
		t.Run(name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "answers.json")
			for _, a := range saves {
				if _, err := saveNormalAnswer(path, a); err != nil {
					t.Fatal(err)
				}
			}
			answers, err := loadNormalAnswersList(path)
			if err != nil {
				t.Fatal(err)
			}
			h := newTrainHarness(feed, "port")
			h.answers = answers
			m := h.train(t, normalSubsystemModel{})
			// No minute is both left out and kept.
			for _, x := range m.Excluded {
				for _, l := range m.Labelled {
					if x.From.Before(l.To) && x.To.After(l.From) {
						t.Fatalf("excluded %+v overlaps labelled %+v", x, l)
					}
				}
			}
			if name == "real then normal" {
				if len(m.Excluded) != 1 || !m.Excluded[0].From.Equal(at(20)) || !m.Excluded[0].To.Equal(at(21)) || len(m.Labelled) != 1 || !m.Labelled[0].From.Equal(at(21)) || !m.Labelled[0].To.Equal(at(51)) {
					t.Fatalf("excluded %+v labelled %+v", m.Excluded, m.Labelled)
				}
			}
			if name == "normal then real" {
				if len(m.Excluded) != 1 || !m.Excluded[0].From.Equal(at(20)) || !m.Excluded[0].To.Equal(at(50)) || len(m.Labelled) != 1 || !m.Labelled[0].From.Equal(at(50)) || !m.Labelled[0].To.Equal(at(51)) {
					t.Fatalf("excluded %+v labelled %+v", m.Excluded, m.Labelled)
				}
			}
		})
	}
}

// Every answer an unknown covers goes back into training, not only the latest one.
func TestRecordNormalAnswer_WakesWhenAnyAnswerItReplacesWasRealOrNormal(t *testing.T) {
	deps, woke := answerDeps(t)
	path := deps.Path()
	deps.Path = func() string { return path }
	tick := 0
	deps.Now = func() time.Time { tick++; return answerFrom.Add(time.Duration(tick) * time.Hour) }
	st := func(from, to int) {
		deps.Run = func(string, string) (watchRun, error) {
			s := answerStretch()
			s.From, s.To = answerFrom.Add(time.Duration(from)*time.Minute), answerFrom.Add(time.Duration(to)*time.Minute)
			s.StretchID = stretchIDFor(s.Subsystem, s.Subject, s.From)
			return unusualRunWith(&watchUnusual{Lines: []string{"x"}, Stretches: []watchUnusualStretch{s}}), nil
		}
	}
	record := func(from, to int, answer string) {
		t.Helper()
		st(from, to)
		req := answerRequest()
		req.Answer = answer
		if _, err := recordNormalAnswer(deps, req); err != nil {
			t.Fatal(err)
		}
	}
	record(0, 20, normalAnswerReal)     // wakes: 1
	record(10, 30, normalAnswerUnknown) // trims the real: wakes, 2
	if *woke != 2 {
		t.Fatalf("woke %d times, want 2", *woke)
	}
	// Leaves real 0-10 and unknown 10-30. Unknown over all of it replaces both; the
	// latest of the two is the unknown, but the real one goes back into training.
	record(0, 30, normalAnswerUnknown)
	if *woke != 3 {
		t.Fatalf("woke %d times, want 3: a real answer was replaced", *woke)
	}
	// Replacing only unknown with unknown changes nothing training reads.
	record(0, 30, normalAnswerUnknown)
	if *woke != 3 {
		t.Fatalf("woke %d times, want 3", *woke)
	}
}

// A file that holds two overlapping answers for one engine was not written by this
// program. Reading it is an error, so no reader has to guess which one counts.
func TestLoadNormalAnswers_OverlappingAnswersForOneEngineAreACorruptFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "answers.json")
	a, b := rangeAnswer("a", 0, 30, normalAnswerReal, 0), rangeAnswer("b", 20, 40, normalAnswerNormal, 1)
	write := func(answers ...normalAnswer) {
		t.Helper()
		f := normalAnswerFile{Answers: map[string]normalAnswer{}}
		for _, x := range answers {
			f.Answers[x.StretchID] = x
		}
		if err := writeJSONFileAtomic(path, f); err != nil {
			t.Fatal(err)
		}
	}
	write(a, b)
	if _, err := loadNormalAnswers(path); err == nil || !strings.Contains(err.Error(), "overlap") {
		t.Fatalf("err = %v", err)
	}
	if _, err := saveNormalAnswer(path, rangeAnswer("c", 100, 110, normalAnswerReal, 2)); err == nil {
		t.Fatal("a save must not build on a corrupt file")
	}
	// Touching ranges, or the same range on two engines, are fine.
	other := rangeAnswer("s", 20, 40, normalAnswerReal, 0)
	other.Subject = "starboard"
	write(a, rangeAnswer("t", 30, 40, normalAnswerNormal, 1), other)
	if got, err := loadNormalAnswers(path); err != nil || len(got) != 3 {
		t.Fatalf("answers %v err %v", got, err)
	}
}
