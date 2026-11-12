package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/labstack/echo/v4"
)

// Retry, "Sort again" and the title deadline for Mate on the Inbox (ADR 0283).

func statusDoer(status int, calls *atomic.Int32) openRouterDoer {
	return doerFunc(func(*http.Request) (*http.Response, error) {
		calls.Add(1)
		return &http.Response{
			StatusCode: status, Header: http.Header{"Content-Type": {"application/json"}},
			Body: io.NopCloser(strings.NewReader(`{"error":{"message":"the provider said no"}}`)),
		}, nil
	})
}

func TestInboxRetryDelayBacksOffOneFiveThirtyThenHourly(t *testing.T) {
	for attempts, want := range map[int]time.Duration{
		1: time.Minute, 2: 5 * time.Minute, 3: 30 * time.Minute, 4: time.Hour, 5: time.Hour, 20: time.Hour,
	} {
		if got := inboxRetryDelay(attempts); got != want {
			t.Errorf("after %d attempts: %v, want %v", attempts, got, want)
		}
	}
}

// BEHAVIOUR: a failure is tried again after a back-off and recorded as a plain
// notice, not the raw error.
func TestInboxClassifierRetriesAFailureAfterBackoff(t *testing.T) {
	w := newInboxWorld(t)
	var failing atomic.Bool
	failing.Store(true)
	doer := &fakeMateDoer{reply: func(int, string) (string, error) {
		if failing.Load() {
			return "", errors.New("dial tcp 10.1.2.3:443: connect: connection refused")
		}
		return `{"kind":"equipment"}`, nil
	}}
	withInboxMate(t, doer)
	id := uploadSortable(t, 105)
	clock := time.Now()
	c := classifierFor(w)
	c.now = func() time.Time { return clock }

	if err := c.RunPending(context.Background()); err != nil {
		t.Fatal(err)
	}
	it, _ := w.inbox.Get(id)
	if it.ClassifyError == "" || strings.Contains(it.ClassifyError, "dial") {
		t.Errorf("recorded: %q", it.ClassifyError)
	}
	if it.ClassifyAttempts != 1 || it.NextTryMs != clock.Add(time.Minute).UnixMilli() {
		t.Errorf("attempts %d, next try %d (want %d)", it.ClassifyAttempts, it.NextTryMs, clock.Add(time.Minute).UnixMilli())
	}
	if got := itemJSON(t, id); got["next_try_ms"] != float64(clock.Add(time.Minute).UnixMilli()) {
		t.Errorf("the list must say when Mate will try again: %v", got)
	}
	calls := doer.calls()

	if err := c.RunPending(context.Background()); err != nil || doer.calls() != calls {
		t.Fatalf("tried again before the back-off: %d calls, %v", doer.calls(), err)
	}

	clock = clock.Add(61 * time.Second)
	if err := c.RunPending(context.Background()); err != nil {
		t.Fatal(err)
	}
	if doer.calls() == calls {
		t.Fatal("not tried again after the back-off")
	}
	it, _ = w.inbox.Get(id)
	if it.ClassifyAttempts != 2 || it.NextTryMs != clock.Add(5*time.Minute).UnixMilli() {
		t.Errorf("second failure: attempts %d, next try %d", it.ClassifyAttempts, it.NextTryMs)
	}

	failing.Store(false)
	clock = clock.Add(6 * time.Minute)
	if err := c.RunPending(context.Background()); err != nil {
		t.Fatal(err)
	}
	it, _ = w.inbox.Get(id)
	if it.SuggestedKind != "equipment" || it.ClassifyError != "" || it.ClassifyAttempts != 0 || it.NextTryMs != 0 {
		t.Errorf("recovered item: %+v", it)
	}
}

// BEHAVIOUR (operator decision, ADR 0283): every sort failure is tried again on
// the back-off, 1 minute, 5, 30, then hourly. A bad key, a reply that is not
// JSON, an unknown kind and a dial error are no different.
func TestInboxClassifierRetriesEverySortFailureOnTheBackoff(t *testing.T) {
	cases := map[string]openRouterDoer{
		"401":          statusDoer(401, new(atomic.Int32)),
		"400":          statusDoer(400, new(atomic.Int32)),
		"429":          statusDoer(429, new(atomic.Int32)),
		"bad JSON":     &fakeMateDoer{reply: func(int, string) (string, error) { return "I think it is a boat", nil }},
		"unknown kind": &fakeMateDoer{reply: func(int, string) (string, error) { return `{"kind":"receipt"}`, nil }},
		"dial error": &fakeMateDoer{reply: func(int, string) (string, error) {
			return "", errors.New("dial tcp 10.1.2.3:443: connect: connection refused")
		}},
	}
	for name, doer := range cases {
		t.Run(name, func(t *testing.T) {
			w := newInboxWorld(t)
			withInboxMate(t, doer)
			id := uploadSortable(t, 106)
			clock := time.Now()
			c := classifierFor(w)
			c.now = func() time.Time { return clock }
			for attempt, wait := range []time.Duration{time.Minute, 5 * time.Minute, 30 * time.Minute, time.Hour, time.Hour} {
				if err := c.RunPending(context.Background()); err != nil {
					t.Fatal(err)
				}
				it, _ := w.inbox.Get(id)
				if it.ClassifyError == "" || it.ClassifyAttempts != attempt+1 || it.NextTryMs != clock.Add(wait).UnixMilli() {
					t.Fatalf("failure %d: error %q, attempts %d, next try %d (want %d)", attempt+1, it.ClassifyError, it.ClassifyAttempts, it.NextTryMs, clock.Add(wait).UnixMilli())
				}
				clock = clock.Add(wait - time.Second)
				before := fmt.Sprint(it.NextTryMs, it.ClassifyAttempts)
				if err := c.RunPending(context.Background()); err != nil {
					t.Fatal(err)
				}
				if again, _ := w.inbox.Get(id); fmt.Sprint(again.NextTryMs, again.ClassifyAttempts) != before {
					t.Fatalf("failure %d: tried again before its time", attempt+1)
				}
				clock = clock.Add(2 * time.Second)
			}
		})
	}
}

// BEHAVIOUR: the tile says it in one plain line with the next try in the boat's
// 12-hour form, and none of the provider's words reach it.
func TestInboxTileLineForAFailedSortIsPlainAndHasNoRawText(t *testing.T) {
	zonePath := useFreshBoatZone(t)
	if err := os.WriteFile(zonePath, []byte(`{"zone":"Australia/Brisbane"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	brisbane, err := time.LoadLocation("Australia/Brisbane")
	if err != nil {
		t.Fatal(err)
	}
	w := newInboxWorld(t)
	withInboxMate(t, &fakeMateDoer{reply: func(int, string) (string, error) {
		return "", errors.New("dial tcp 10.1.2.3:443: connect: connection refused")
	}})
	id := uploadSortable(t, 107)
	clock := time.Date(2026, 11, 12, 5, 39, 0, 0, time.UTC) // 3.39 PM in Brisbane
	c := classifierFor(w)
	c.now = func() time.Time { return clock }
	if err := c.RunPending(context.Background()); err != nil {
		t.Fatal(err)
	}
	prevNow := inboxNow
	inboxNow = func() time.Time { return clock }
	t.Cleanup(func() { inboxNow = prevNow })
	got := itemJSON(t, id)
	want := "Mate couldn't sort this yet. Trying again at " + boatClock(clock.Add(time.Minute), brisbane)
	if want != "Mate couldn't sort this yet. Trying again at 3.40 PM" {
		t.Fatalf("setup: %q", want)
	}
	if got["classify_error"] != want {
		t.Errorf("tile line %q, want %q", got["classify_error"], want)
	}
	if _, has := got["classify_retrying"]; has {
		t.Errorf("there is no temporary/permanent split any more: %v", got)
	}
	for _, raw := range []string{"dial", "10.1.2.3", "refused", "tcp"} {
		if strings.Contains(fmt.Sprint(got["classify_error"]), raw) {
			t.Errorf("raw text %q on the tile: %v", raw, got["classify_error"])
		}
	}
}

// BEHAVIOUR: saving Mate's settings requeues every failed item at once, whatever
// its back-off said, and wakes the classifier.
func TestInboxSettingsSaveRequeuesEveryFailedItem(t *testing.T) {
	w := newInboxWorld(t)
	var statuses atomic.Int32
	withInboxMate(t, statusDoer(401, &statuses))
	ids := []string{uploadSortable(t, 108), uploadSortable(t, 109)}
	clock := time.Now()
	c := classifierFor(w)
	c.now = func() time.Time { return clock }
	for i := 0; i < 3; i++ { // both items fail three times, so their next try is half an hour or more away
		if err := c.RunPending(context.Background()); err != nil {
			t.Fatal(err)
		}
		clock = clock.Add(2 * time.Hour)
	}
	if err := w.inbox.SetClassifyRetry(ids[0], "x", 1, clock.Add(time.Hour).UnixMilli()); err != nil {
		t.Fatal(err)
	}
	for _, id := range ids {
		if it, _ := w.inbox.Get(id); it.ClassifyError == "" || it.NextTryMs == 0 {
			t.Fatalf("setup: %+v", it)
		}
	}
	sorted := uploadSortable(t, 110)
	if err := w.inbox.SetSuggestedKind(sorted, "document"); err != nil {
		t.Fatal(err)
	}
	var woken atomic.Int32
	prev := inboxClassifierWake
	inboxClassifierWake = func() { woken.Add(1) }
	t.Cleanup(func() { inboxClassifierWake = prev })

	inboxMateSettingsChanged() // what a save of Mate's settings calls; the requeue runs in the background
	for i := 0; i < 400 && woken.Load() == 0; i++ {
		time.Sleep(5 * time.Millisecond)
	}

	for _, id := range ids {
		if it, _ := w.inbox.Get(id); it.ClassifyError != "" || it.ClassifyAttempts != 0 || it.NextTryMs != 0 {
			t.Errorf("not requeued: %+v", it)
		}
	}
	if it, _ := w.inbox.Get(sorted); it.SuggestedKind != "document" {
		t.Errorf("a sorted item was disturbed: %+v", it)
	}
	if woken.Load() != 1 {
		t.Errorf("woken %d times", woken.Load())
	}
}

// BEHAVIOUR: Run wakes itself when a retry comes due, with no upload or list.
func TestInboxClassifierRunRetriesATransientFailureByItself(t *testing.T) {
	w := newInboxWorld(t)
	var n atomic.Int32
	doer := &fakeMateDoer{reply: func(int, string) (string, error) {
		if n.Add(1) <= 2 { // the try and its immediate retry
			return "", errors.New("connection reset")
		}
		return `{"kind":"scenery"}`, nil
	}}
	withInboxMate(t, doer)
	id := uploadSortable(t, 108)
	c := classifierFor(w)
	c.backoff = func(int) time.Duration { return 30 * time.Millisecond }
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { c.Run(ctx); close(done) }()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if it, err := w.inbox.Get(id); err == nil && it.SuggestedKind != "" {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	cancel()
	<-done
	if it, _ := w.inbox.Get(id); it.SuggestedKind != "scenery" {
		t.Errorf("never retried: %+v", it)
	}
}

func postSortAgain(t *testing.T) (int, map[string]any) {
	t.Helper()
	e := echo.New()
	req := httptest.NewRequest(http.MethodPost, "/api/inbox/sort-again", nil)
	rec := httptest.NewRecorder()
	if err := inboxSortAgainHandler(e.NewContext(req, rec)); err != nil {
		t.Fatal(err)
	}
	var body map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &body)
	return rec.Code, body
}

// BEHAVIOUR: "Sort again" clears every failure and wakes the classifier.
func TestInboxSortAgainRequeuesFailedItems(t *testing.T) {
	w := newInboxWorld(t)
	var calls atomic.Int32
	withInboxMate(t, statusDoer(401, &calls))
	failed := uploadSortable(t, 109)
	c := classifierFor(w)
	if err := c.RunPending(context.Background()); err != nil {
		t.Fatal(err)
	}
	if it, _ := w.inbox.Get(failed); it.ClassifyError == "" {
		t.Fatal("setup: the item should have failed")
	}
	sorted := uploadSortable(t, 110)
	if err := w.inbox.SetSuggestedKind(sorted, "document"); err != nil {
		t.Fatal(err)
	}

	var woken atomic.Int32
	prev := inboxClassifierWake
	inboxClassifierWake = func() { woken.Add(1) }
	t.Cleanup(func() { inboxClassifierWake = prev })
	code, body := postSortAgain(t)
	if code != 200 || body["requeued"] != float64(1) {
		t.Fatalf("%d %v", code, body)
	}
	if woken.Load() != 1 {
		t.Errorf("woken %d times", woken.Load())
	}
	if it, _ := w.inbox.Get(failed); it.ClassifyError != "" || it.ClassifyAttempts != 0 || it.NextTryMs != 0 || it.SuggestedKind != "" {
		t.Errorf("the failure was not cleared: %+v", it)
	}
	if it, _ := w.inbox.Get(sorted); it.SuggestedKind != "document" {
		t.Errorf("a sorted item was disturbed: %+v", it)
	}

	withInboxMate(t, &fakeMateDoer{reply: func(int, string) (string, error) { return `{"kind":"scenery"}`, nil }})
	if err := c.RunPending(context.Background()); err != nil {
		t.Fatal(err)
	}
	if it, _ := w.inbox.Get(failed); it.SuggestedKind != "scenery" {
		t.Errorf("the requeued item was not sorted: %+v", it)
	}
}

// ── the request deadline ─────────────────────────────────────────────────

// BEHAVIOUR: a title request has an overall deadline; what has not finished
// by then is told so, and tasks not yet started make no call.
func TestInboxMateTitlesStopAtTheRequestDeadline(t *testing.T) {
	_, at := titleWorld(t, 5)
	doer := &fakeMateDoer{delay: 400 * time.Millisecond, reply: func(int, string) (string, error) { return `{"title":"T"}`, nil }}
	withInboxMate(t, doer)
	withFeatures(t, func(float64, float64) ([]poiFeatureRaw, error) { return nil, nil })
	prev := inboxTitleDeadline
	inboxTitleDeadline = 100 * time.Millisecond
	t.Cleanup(func() { inboxTitleDeadline = prev })
	var ids []string
	for i := 0; i < 8; i++ {
		ids = append(ids, uploadForFiling(t, uint8(80+i), map[string]string{"capture_ms": ms(at.Add(time.Duration(i) * time.Second))}))
	}
	start := time.Now()
	titles, errs := titlesFor(t, ids...)
	if time.Since(start) > 350*time.Millisecond {
		t.Errorf("waited %v past the deadline", time.Since(start))
	}
	for _, id := range ids {
		if titles[id] != "" || errs[id] != "Mate didn't answer in time" {
			t.Errorf("%s: title %q error %v", id, titles[id], errs[id])
		}
	}
	if doer.calls() > inboxMateCalls {
		t.Errorf("%d calls: tasks that had not started still called Mate", doer.calls())
	}
}
