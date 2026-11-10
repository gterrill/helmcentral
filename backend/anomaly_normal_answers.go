package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"net/http"
	"slices"
	"sync"
	"time"

	"github.com/labstack/echo/v4"
)

// The operator's answers about unusual stretches (ADR 0276, Feedback). A stretch
// in the watch report can be answered real, normal or don't know. The answer is
// kept here by the stretch's id, with the stretch's own times so training can use
// it without scoring again. One small JSON file, like the model it feeds.

const (
	normalAnswerReal    = "real"
	normalAnswerNormal  = "normal"
	normalAnswerUnknown = "unknown"
)

// normalAnswer is one answered stretch.
type normalAnswer struct {
	StretchID  string    `json:"stretch_id"`
	Subsystem  string    `json:"subsystem"`
	Subject    string    `json:"subject"`
	From       time.Time `json:"from"`
	To         time.Time `json:"to"`
	Answer     string    `json:"answer"`
	AnsweredAt time.Time `json:"answered_at"`
}

type normalAnswerFile struct {
	Answers map[string]normalAnswer `json:"answers"`
}

func normalAnswersPath() string {
	return cacheFilePath("ANOMALY_NORMAL_ANSWERS_FILE", "data/anomaly-normal-answers.json")
}

// normalAnswersMu serialises the read, change and write of the file.
var normalAnswersMu sync.Mutex

// loadNormalAnswers reads the answers by stretch id. A missing file is no
// answers. A file that exists and does not parse is an error, never an empty set:
// training on it would forget what the operator said.
func loadNormalAnswers(path string) (map[string]normalAnswer, error) {
	f, _, err := readJSONFile[normalAnswerFile](path)
	if err != nil {
		return nil, err
	}
	if f.Answers == nil {
		f.Answers = map[string]normalAnswer{}
	}
	// saveNormalAnswer never leaves two ranges for one engine overlapping. A file
	// that does was not written by it, and every reader relies on the ranges
	// being apart, so it is refused rather than guessed at.
	byEngine := map[string][]normalAnswer{}
	for _, a := range f.Answers {
		byEngine[a.Subsystem+"/"+a.Subject] = append(byEngine[a.Subsystem+"/"+a.Subject], a)
	}
	for _, list := range byEngine {
		slices.SortFunc(list, func(x, y normalAnswer) int { return x.From.Compare(y.From) })
		for i := 1; i < len(list); i++ {
			if list[i].From.Before(list[i-1].To) {
				return nil, fmt.Errorf("%s: the answers %s and %s overlap, which is not a file Helmcentral wrote", path, list[i-1].StretchID, list[i].StretchID)
			}
		}
	}
	return f.Answers, nil
}

// listNormalAnswers is every answer on file, in no particular order.
func listNormalAnswers() ([]normalAnswer, error) { return loadNormalAnswersList(normalAnswersPath()) }

// loadNormalAnswersList is listNormalAnswers for a file at path.
func loadNormalAnswersList(path string) ([]normalAnswer, error) {
	byID, err := loadNormalAnswers(path)
	if err != nil {
		return nil, err
	}
	return slices.Collect(maps.Values(byID)), nil
}

// overlappingAnswer is what the operator said about a stretch of a subject [from,
// to): the answer covering the most of its minutes, and on a tie the later one. A
// stretch's start can move a minute between two scorings of the same trouble, so an
// exact id would miss it, and a stretch can straddle two answers.
func overlappingAnswer(answers []normalAnswer, subsystem, subject string, from, to time.Time) string {
	var best normalAnswer
	var bestSpan time.Duration
	for _, a := range answers {
		if a.Subsystem != subsystem || a.Subject != subject {
			continue
		}
		end, start := a.To, a.From
		if to.Before(end) {
			end = to
		}
		if from.After(start) {
			start = from
		}
		span := end.Sub(start)
		if span <= 0 {
			continue
		}
		if span > bestSpan || (span == bestSpan && a.AnsweredAt.After(best.AnsweredAt)) {
			best, bestSpan = a, span
		}
	}
	return best.Answer
}

// saveNormalAnswer keeps a. The latest answer replaces the parts of earlier ones
// for the same subsystem and subject that it overlaps: an earlier range is
// trimmed, or split in two around a, and a piece with nothing left is dropped. So
// the file never holds overlapping answers for one subject, and training,
// scoring, the page and Mate all read the same set. It returns the answer of every
// earlier range it overlapped, so a caller can tell whether training's input changed.
func saveNormalAnswer(path string, a normalAnswer) (replaced []string, err error) {
	normalAnswersMu.Lock()
	defer normalAnswersMu.Unlock()
	answers, err := loadNormalAnswers(path)
	if err != nil {
		return nil, err
	}
	for key, old := range answers {
		if old.Subsystem != a.Subsystem || old.Subject != a.Subject || !old.From.Before(a.To) || !old.To.After(a.From) {
			continue
		}
		replaced = append(replaced, old.Answer)
		delete(answers, key)
		if old.From.Before(a.From) {
			left := old
			left.StretchID, left.To = key, a.From
			answers[key] = left
		}
		if old.To.After(a.To) {
			right := old
			right.From = a.To
			right.StretchID = key + "#" + a.To.UTC().Format(time.RFC3339)
			answers[right.StretchID] = right
		}
	}
	answers[a.StretchID] = a
	return replaced, writeJSONFileAtomic(path, normalAnswerFile{Answers: answers})
}

// normalAnswerRequest is the body of PUT /api/anomaly/normal/answers. It names a
// stretch by the stored run and the position of its line, never by times: the
// server reads the stretch from the run it saved.
type normalAnswerRequest struct {
	WatchID string `json:"watch_id"`
	RunID   string `json:"run_id"`
	Index   int    `json:"index"`
	Answer  string `json:"answer"`
}

// normalAnswerError is a request the operator or a caller got wrong (a 400).
type normalAnswerError struct{ msg string }

func (e *normalAnswerError) Error() string { return e.msg }

func badNormalAnswer(format string, args ...any) error {
	return &normalAnswerError{msg: fmt.Sprintf(format, args...)}
}

type normalAnswerDeps struct {
	Now    func() time.Time
	Vessel func() (vesselView, error)
	// Run reads a stored watch run. A run that is not there is errWatchRunNotFound.
	Run  func(watchID, runID string) (watchRun, error)
	Path func() string
	// Wake asks for a training run.
	Wake func()
}

func productionNormalAnswerDeps(settingsPath string) normalAnswerDeps {
	return normalAnswerDeps{
		Now:    func() time.Time { return time.Now().UTC() },
		Vessel: func() (vesselView, error) { return loadVesselView(settingsPath) },
		Run: func(watchID, runID string) (watchRun, error) {
			docs, err := watchStore()
			if err != nil {
				return watchRun{}, err
			}
			var run watchRun
			err = docs.Read(func(q sqlQueryer) error {
				var err error
				run, err = watchRunByIDQ(q, watchID, runID)
				return err
			})
			return run, err
		},
		Path: normalAnswersPath,
		Wake: normalInputsChanged,
	}
}

// stretchIDFor is a stretch's id, from its subsystem, subject and first minute.
func stretchIDFor(subsystem, subject string, start time.Time) string {
	return fmt.Sprintf("%s:%s@%s", subsystem, subject, start.UTC().Format(time.RFC3339))
}

// recordNormalAnswer checks and keeps an answer. A real or normal answer, or a
// change away from one, wakes the trainer: those are the answers training uses.
func recordNormalAnswer(deps normalAnswerDeps, req normalAnswerRequest) (normalAnswer, error) {
	switch req.Answer {
	case normalAnswerReal, normalAnswerNormal, normalAnswerUnknown:
	default:
		return normalAnswer{}, badNormalAnswer("answer must be real, normal or unknown")
	}
	run, err := deps.Run(req.WatchID, req.RunID)
	if errors.Is(err, errWatchRunNotFound) {
		return normalAnswer{}, badNormalAnswer("That report is no longer there.")
	}
	if err != nil {
		return normalAnswer{}, fmt.Errorf("reading the report: %w", err)
	}
	stretches, err := unusualStretchesOf(run)
	if err != nil {
		return normalAnswer{}, fmt.Errorf("run %s: %w", run.ID, err)
	}
	if req.Index < 0 || req.Index >= len(stretches) {
		return normalAnswer{}, badNormalAnswer("That report has no such stretch to answer.")
	}
	st := stretches[req.Index]
	var sub *normalSubsystem
	for _, s := range normalSubsystems() {
		if s.Name == st.Subsystem {
			sub = &s
		}
	}
	if sub == nil {
		return normalAnswer{}, badNormalAnswer("That stretch is of a kind that is no longer learned.")
	}
	vessel, err := deps.Vessel()
	if err != nil {
		return normalAnswer{}, fmt.Errorf("vessel settings: %w", err)
	}
	known := false
	for _, s := range sub.Subjects(vessel) {
		known = known || s.ID == st.Subject
	}
	if !known {
		return normalAnswer{}, badNormalAnswer("This engine is no longer set up. It may have been renumbered or removed.")
	}
	path := deps.Path()
	saved := normalAnswer{
		StretchID: st.StretchID, Subsystem: st.Subsystem, Subject: st.Subject,
		From: st.From.UTC(), To: st.To.UTC(), Answer: req.Answer, AnsweredAt: deps.Now().UTC(),
	}
	replaced, err := saveNormalAnswer(path, saved)
	if err != nil {
		return normalAnswer{}, err
	}
	// Training reads real and normal answers, so it hears of the new answer or of any
	// it replaced, trimmed or removed.
	training := func(answer string) bool { return answer == normalAnswerReal || answer == normalAnswerNormal }
	if training(saved.Answer) || slices.ContainsFunc(replaced, training) {
		deps.Wake()
	}
	return saved, nil
}

func normalAnswerHandler(deps normalAnswerDeps) echo.HandlerFunc {
	return func(c echo.Context) error {
		var req normalAnswerRequest
		dec := json.NewDecoder(http.MaxBytesReader(c.Response(), c.Request().Body, 1<<16))
		dec.DisallowUnknownFields()
		if err := dec.Decode(&req); err != nil {
			return c.JSON(http.StatusBadRequest, map[string]string{"error": "send {watch_id, run_id, index, answer}"})
		}
		saved, err := recordNormalAnswer(deps, req)
		var bad *normalAnswerError
		switch {
		case errors.As(err, &bad):
			return c.JSON(http.StatusBadRequest, map[string]string{"error": bad.Error()})
		case err != nil:
			return c.JSON(http.StatusInternalServerError, map[string]string{"error": firstErrorLine(err)})
		}
		return c.JSON(http.StatusOK, saved)
	}
}
