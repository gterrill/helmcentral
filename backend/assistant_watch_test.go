package main

import (
	"context"
	"encoding/json"
	"math"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"
)

// ── test doubles ────────────────────────────────────────────────────────

// fakeWatchTicker is an assistantWatchTicker the test drives by hand. ch is
// unbuffered, so a send returns only once the watch goroutine has taken the
// tick: the test never sleeps and never races the sampler.
type fakeWatchTicker struct {
	ch       chan time.Time
	stopOnce sync.Once
	stopped  chan struct{}
}

func newFakeWatchTicker() *fakeWatchTicker {
	return &fakeWatchTicker{ch: make(chan time.Time), stopped: make(chan struct{})}
}

func (f *fakeWatchTicker) C() <-chan time.Time { return f.ch }
func (f *fakeWatchTicker) Stop()               { f.stopOnce.Do(func() { close(f.stopped) }) }

// watchTestHarness wires a registry to a fake clock, a fake ticker per
// watch, a scripted reader and a finish hook that records what it got.
type watchTestHarness struct {
	t        *testing.T
	reg      *assistantWatchRegistry
	t0       time.Time
	mu       sync.Mutex
	tickers  []*fakeWatchTicker
	readings map[string]func(now time.Time) assistantWatchReading
	finished chan assistantWatchFinished
}

type assistantWatchFinished struct {
	info   assistantWatchInfo
	report assistantWatchReport
}

func newWatchTestHarness(t *testing.T) *watchTestHarness {
	t.Helper()
	h := &watchTestHarness{
		t:        t,
		t0:       time.Date(2026, 10, 4, 4, 0, 0, 0, time.UTC),
		readings: map[string]func(now time.Time) assistantWatchReading{},
		finished: make(chan assistantWatchFinished, 4),
	}
	h.reg = newAssistantWatchRegistry(assistantWatchRegistryDeps{
		now: func() time.Time { return h.t0 },
		newTicker: func(d time.Duration) assistantWatchTicker {
			if d != time.Second {
				t.Errorf("expected a 1 s sample interval, got %v", d)
			}
			ft := newFakeWatchTicker()
			h.mu.Lock()
			h.tickers = append(h.tickers, ft)
			h.mu.Unlock()
			return ft
		},
		read: func(path string, now time.Time) assistantWatchReading {
			fn, ok := h.readings[path]
			if !ok {
				return assistantWatchReading{}
			}
			return fn(now)
		},
		finish: func(info assistantWatchInfo, report assistantWatchReport) {
			h.finished <- assistantWatchFinished{info: info, report: report}
		},
	})
	return h
}

func (h *watchTestHarness) ticker(i int) *fakeWatchTicker {
	h.mu.Lock()
	defer h.mu.Unlock()
	if i >= len(h.tickers) {
		h.t.Fatalf("no ticker %d (have %d)", i, len(h.tickers))
	}
	return h.tickers[i]
}

// tickThrough sends one tick per second from t0+1s to t0+seconds.
func (h *watchTestHarness) tickThrough(ft *fakeWatchTicker, seconds int) {
	for i := 1; i <= seconds; i++ {
		ft.ch <- h.t0.Add(time.Duration(i) * time.Second)
	}
}

func (h *watchTestHarness) waitFinished() assistantWatchFinished {
	h.t.Helper()
	select {
	case f := <-h.finished:
		return f
	case <-time.After(2 * time.Second):
		h.t.Fatal("watch never finished")
	}
	return assistantWatchFinished{}
}

func liveReading(value float64) assistantWatchReading {
	return assistantWatchReading{Present: true, Numeric: true, Value: value, Source: "n2k.1", Units: "ratio", AgeSeconds: 0.5}
}

func constantReading(value float64) func(time.Time) assistantWatchReading {
	return func(time.Time) assistantWatchReading { return liveReading(value) }
}

func watchRequest(conversationID string, raw string) assistantWatchRequest {
	args, err := parseAssistantStartWatchArgs(json.RawMessage(raw))
	if err != nil {
		panic(err)
	}
	return assistantWatchRequest{
		ConversationID: conversationID,
		Args:           args,
		Today:          time.Date(2026, 10, 4, 0, 0, 0, 0, time.UTC),
		Location:       time.FixedZone("UTC+10", 10*3600),
		TimezoneLabel:  "UTC+10",
	}
}

// ── argument validation ─────────────────────────────────────────────────

func TestParseAssistantStartWatchArgs_Validates(t *testing.T) {
	cases := []struct {
		name    string
		raw     string
		wantErr string
	}{
		{"no paths", `{"paths":[],"minutes":5,"reason":"spikes"}`, "1 to 6 paths"},
		{"seven paths", `{"paths":[{"path":"a.a"},{"path":"a.b"},{"path":"a.c"},{"path":"a.d"},{"path":"a.e"},{"path":"a.f"},{"path":"a.g"}],"minutes":5,"reason":"spikes"}`, "1 to 6 paths"},
		{"blank path", `{"paths":[{"path":"  "}],"minutes":5,"reason":"spikes"}`, "path is required"},
		{"duplicate path", `{"paths":[{"path":"a.b"},{"path":"a.b"}],"minutes":5,"reason":"spikes"}`, "more than once"},
		{"zero minutes", `{"paths":[{"path":"a.b"}],"minutes":0,"reason":"spikes"}`, "minutes must be a whole number from 1 to 30"},
		{"too many minutes", `{"paths":[{"path":"a.b"}],"minutes":31,"reason":"spikes"}`, "minutes must be a whole number from 1 to 30"},
		{"fractional minutes", `{"paths":[{"path":"a.b"}],"minutes":2.5,"reason":"spikes"}`, "minutes"},
		{"missing reason", `{"paths":[{"path":"a.b"}],"minutes":5}`, "reason is required"},
		{"long label", `{"paths":[{"path":"a.b","label":"` + strings.Repeat("x", 41) + `"}],"minutes":5,"reason":"spikes"}`, "label"},
		{"long reason", `{"paths":[{"path":"a.b"}],"minutes":5,"reason":"` + strings.Repeat("x", 201) + `"}`, "reason"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := parseAssistantStartWatchArgs(json.RawMessage(tc.raw))
			if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
				t.Fatalf("expected an error containing %q, got %v", tc.wantErr, err)
			}
		})
	}

	args, err := parseAssistantStartWatchArgs(json.RawMessage(`{"paths":[{"path":" propulsion.port.engineLoad ","label":" Port engine load "},{"path":"propulsion.starboard.engineLoad"}],"minutes":5,"reason":" port spikes "}`))
	if err != nil {
		t.Fatalf("valid args rejected: %v", err)
	}
	if args.Paths[0].Path != "propulsion.port.engineLoad" || args.Paths[0].Label != "Port engine load" || args.Reason != "port spikes" {
		t.Fatalf("expected trimmed fields, got %+v", args)
	}
	if args.Paths[1].Label != "Propulsion Starboard Engine Load" {
		t.Fatalf("expected a missing label to fall back to the humanised path, got %q", args.Paths[1].Label)
	}
}

// ── fail fast at start ──────────────────────────────────────────────────

func TestAssistantWatchStart_PathNotLiveNamesThePath(t *testing.T) {
	cases := []struct {
		name    string
		reading assistantWatchReading
		wantErr string
	}{
		{"absent", assistantWatchReading{}, "is not in the live instrument feed"},
		{"not numeric", assistantWatchReading{Present: true, Numeric: false, AgeSeconds: 1}, "is not a number"},
		{"stale", assistantWatchReading{Present: true, Numeric: true, Value: 1, AgeSeconds: 45}, "last updated 45 s ago"},
		{"no timestamp", assistantWatchReading{Present: true, Numeric: true, Value: 1, AgeSeconds: -1}, "no update time"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			h := newWatchTestHarness(t)
			h.readings["propulsion.port.engineLoad"] = constantReading(0.5)
			h.readings["propulsion.starboard.engineLoad"] = func(time.Time) assistantWatchReading { return tc.reading }
			raw := `{"paths":[{"path":"propulsion.port.engineLoad"},{"path":"propulsion.starboard.engineLoad"}],"minutes":5,"reason":"spikes"}`
			_, err := h.reg.start(watchRequest("conv-1", raw))
			if err == nil || !strings.Contains(err.Error(), "propulsion.starboard.engineLoad") || !strings.Contains(err.Error(), tc.wantErr) {
				t.Fatalf("expected an error naming the path and containing %q, got %v", tc.wantErr, err)
			}
			if _, ok := h.reg.get("conv-1"); ok {
				t.Fatal("a watch that failed to start must not be registered")
			}
		})
	}
}

// ── sampling, gaps and the end of the watch ─────────────────────────────

func TestAssistantWatch_SamplesOncePerTickAndRecordsStaleGaps(t *testing.T) {
	h := newWatchTestHarness(t)
	h.readings["electrical.batteries.house.voltage"] = func(now time.Time) assistantWatchReading {
		s := int(now.Sub(h.t0).Seconds())
		if s >= 10 && s <= 14 {
			return assistantWatchReading{Present: true, Numeric: true, Value: 13.1, Source: "n2k.1", AgeSeconds: 30}
		}
		return assistantWatchReading{Present: true, Numeric: true, Value: 13.0 + float64(s%2)*0.2, Source: "n2k.1", Units: "V", AgeSeconds: 0.5}
	}

	info, err := h.reg.start(watchRequest("conv-1", `{"paths":[{"path":"electrical.batteries.house.voltage","label":"House voltage"}],"minutes":1,"reason":"charging dips"}`))
	if err != nil {
		t.Fatalf("start: %v", err)
	}
	if !info.EndsAt.Equal(h.t0.Add(time.Minute)) || info.Status != "watching" || info.Subject != "House voltage" {
		t.Fatalf("unexpected info: %+v", info)
	}

	h.tickThrough(h.ticker(0), 60)
	f := h.waitFinished()

	if f.info.ConversationID != "conv-1" || f.report.Reason != "charging dips" || f.report.Minutes != 1 {
		t.Fatalf("unexpected finish: %+v / %+v", f.info, f.report)
	}
	if len(f.report.Series) != 1 {
		t.Fatalf("expected one series, got %d", len(f.report.Series))
	}
	s := f.report.Series[0]
	if s.Ticks != 60 || s.Samples != 55 {
		t.Fatalf("expected 60 ticks and 55 samples (5 stale), got %d/%d", s.Ticks, s.Samples)
	}
	if s.GapCount != 1 || len(s.Gaps) != 1 || s.Gaps[0].Seconds != 5 || s.Gaps[0].Reason != "not updating" {
		t.Fatalf("expected one 5 s 'not updating' gap, got %+v", s.Gaps)
	}
	if s.Gaps[0].From != "14:00:10" || s.Gaps[0].To != "14:00:14" {
		t.Fatalf("expected the gap in vessel local time 14:00:10-14:00:14, got %+v", s.Gaps[0])
	}
	if s.Min == nil || *s.Min != 13.0 || s.Max == nil || *s.Max != 13.2 {
		t.Fatalf("expected min 13.0 and max 13.2 (the stale 13.1 not counted), got %v/%v", s.Min, s.Max)
	}
	if s.Units != "V" || s.Label != "House voltage" {
		t.Fatalf("expected units and label carried through, got %+v", s)
	}
	if f.report.Started != "Sun 4 Oct 14:00:00" || f.report.Timezone != "UTC+10" {
		t.Fatalf("expected vessel-local start, got %q %q", f.report.Started, f.report.Timezone)
	}

	select {
	case <-h.ticker(0).stopped:
	case <-time.After(time.Second):
		t.Fatal("ticker not stopped after the watch finished")
	}
	waitForConditionT(t, time.Second, func() bool { _, ok := h.reg.get("conv-1"); return !ok })
}

// ── excursions ──────────────────────────────────────────────────────────

func watchSeries(t0 time.Time, values []float64) []assistantWatchSample {
	out := make([]assistantWatchSample, len(values))
	for i, v := range values {
		out[i] = assistantWatchSample{T: t0.Add(time.Duration(i+1) * time.Second), Value: v, OK: true}
	}
	return out
}

func TestDetectAssistantWatchExcursions_FindsSpikesAsEpisodes(t *testing.T) {
	t0 := time.Date(2026, 10, 4, 4, 0, 0, 0, time.UTC)
	values := make([]float64, 300)
	for i := range values {
		// A noisy plateau around 0.45 with a little jitter.
		values[i] = 0.45 + 0.005*math.Sin(float64(i))
	}
	// Two spikes: three seconds at 0.80 starting at sample 100, one second
	// at 0.20 at sample 220.
	values[100], values[101], values[102] = 0.80, 0.82, 0.79
	values[220] = 0.20

	excursions, count := detectAssistantWatchExcursions(watchSeries(t0, values), time.UTC)
	if count != 2 || len(excursions) != 2 {
		t.Fatalf("expected 2 excursion episodes, got %d: %+v", count, excursions)
	}
	first := excursions[0]
	if first.Start != "04:01:41" || first.Seconds != 3 || first.Peak != 0.82 || first.Direction != "above" {
		t.Fatalf("unexpected first episode: %+v", first)
	}
	if math.Abs(first.Baseline-0.45) > 0.01 {
		t.Fatalf("expected the baseline near 0.45, got %v", first.Baseline)
	}
	second := excursions[1]
	if second.Seconds != 1 || second.Peak != 0.2 || second.Direction != "below" {
		t.Fatalf("unexpected second episode: %+v", second)
	}
}

func TestDetectAssistantWatchExcursions_QuietSeriesHasNone(t *testing.T) {
	t0 := time.Date(2026, 10, 4, 4, 0, 0, 0, time.UTC)
	values := make([]float64, 120)
	for i := range values {
		values[i] = 0.45 + 0.01*float64(i%3-1)
	}
	if excursions, count := detectAssistantWatchExcursions(watchSeries(t0, values), time.UTC); count != 0 {
		t.Fatalf("expected no excursions in plain jitter, got %+v", excursions)
	}
}

// ── two paths: the difference series ────────────────────────────────────

func TestAssistantWatch_TwoPathsReportTheDifference(t *testing.T) {
	h := newWatchTestHarness(t)
	h.readings["propulsion.port.engineLoad"] = func(now time.Time) assistantWatchReading {
		s := int(now.Sub(h.t0).Seconds())
		if s == 40 || s == 41 {
			return liveReading(0.85)
		}
		return liveReading(0.50)
	}
	h.readings["propulsion.starboard.engineLoad"] = func(now time.Time) assistantWatchReading {
		s := int(now.Sub(h.t0).Seconds())
		if s == 70 {
			return assistantWatchReading{} // dropped out for one second
		}
		return liveReading(0.48)
	}

	_, err := h.reg.start(watchRequest("conv-2", `{"paths":[{"path":"propulsion.port.engineLoad","label":"Port load"},{"path":"propulsion.starboard.engineLoad","label":"Starboard load"}],"minutes":2,"reason":"port spikes vs starboard"}`))
	if err != nil {
		t.Fatalf("start: %v", err)
	}
	h.tickThrough(h.ticker(0), 120)
	f := h.waitFinished()

	d := f.report.Difference
	if d == nil {
		t.Fatal("expected a difference series for exactly two paths")
	}
	if d.Label != "Port load minus Starboard load" {
		t.Fatalf("unexpected difference label %q", d.Label)
	}
	if d.Samples != 119 || d.Ticks != 120 {
		t.Fatalf("expected 119 paired samples of 120 ticks, got %d/%d", d.Samples, d.Ticks)
	}
	if d.Max == nil || math.Abs(*d.Max-0.37) > 1e-9 || d.Min == nil || math.Abs(*d.Min-0.02) > 1e-9 {
		t.Fatalf("expected difference min 0.02 max 0.37, got %v/%v", d.Min, d.Max)
	}
	if d.ExcursionCount != 1 || d.Excursions[0].Seconds != 2 || d.Excursions[0].Direction != "above" {
		t.Fatalf("expected one 2 s excursion of the difference, got %+v", d.Excursions)
	}
	if d.GapCount != 1 || d.Gaps[0].Reason != "missing" {
		t.Fatalf("expected the starboard dropout as one gap in the difference, got %+v", d.Gaps)
	}

	h2 := newWatchTestHarness(t)
	h2.readings["a.one"] = constantReading(1)
	if _, err := h2.reg.start(watchRequest("conv-3", `{"paths":[{"path":"a.one"}],"minutes":1,"reason":"x"}`)); err != nil {
		t.Fatalf("start: %v", err)
	}
	h2.tickThrough(h2.ticker(0), 60)
	if f := h2.waitFinished(); f.report.Difference != nil {
		t.Fatal("expected no difference series for one path")
	}
}

// ── one per conversation, a cap overall, and cancel ─────────────────────

func TestAssistantWatch_OnePerConversationAndAConcurrentCap(t *testing.T) {
	h := newWatchTestHarness(t)
	h.readings["a.one"] = constantReading(1)
	raw := `{"paths":[{"path":"a.one"}],"minutes":5,"reason":"x"}`

	if _, err := h.reg.start(watchRequest("conv-1", raw)); err != nil {
		t.Fatalf("first start: %v", err)
	}
	_, err := h.reg.start(watchRequest("conv-1", raw))
	if err == nil || !strings.Contains(err.Error(), "already running in this conversation") || !strings.Contains(err.Error(), "14:05") {
		t.Fatalf("expected a rejection naming the running watch's end time, got %v", err)
	}

	for i := 2; i <= assistantWatchMaxConcurrent; i++ {
		if _, err := h.reg.start(watchRequest("conv-"+string(rune('0'+i)), raw)); err != nil {
			t.Fatalf("start %d: %v", i, err)
		}
	}
	if _, err := h.reg.start(watchRequest("conv-9", raw)); err == nil || !strings.Contains(err.Error(), "watches are already running") {
		t.Fatalf("expected the concurrent cap to refuse a fourth watch, got %v", err)
	}
}

func TestAssistantWatch_CancelStopsWithoutAReport(t *testing.T) {
	h := newWatchTestHarness(t)
	h.readings["a.one"] = constantReading(1)
	if _, err := h.reg.start(watchRequest("conv-1", `{"paths":[{"path":"a.one"}],"minutes":5,"reason":"x"}`)); err != nil {
		t.Fatalf("start: %v", err)
	}
	h.tickThrough(h.ticker(0), 3)

	if !h.reg.cancel("conv-1") {
		t.Fatal("expected cancel to find the watch")
	}
	if h.reg.cancel("conv-1") {
		t.Fatal("a second cancel should find nothing")
	}
	select {
	case <-h.ticker(0).stopped:
	case <-time.After(time.Second):
		t.Fatal("ticker not stopped after cancel")
	}
	select {
	case f := <-h.finished:
		t.Fatalf("a cancelled watch must not report, got %+v", f)
	case <-time.After(50 * time.Millisecond):
	}
	if _, ok := h.reg.get("conv-1"); ok {
		t.Fatal("cancelled watch still registered")
	}
}

// ── the report text handed to the follow-up turn ────────────────────────

func TestAssistantWatchReportMessage_HeadlineThenBoundedReport(t *testing.T) {
	info := assistantWatchInfo{ID: "w1", Subject: "Port load and Starboard load", Minutes: 5}
	report := assistantWatchReport{WatchID: "w1", Reason: "spikes", Minutes: 5}
	// A pathological series with far more excursions than fit.
	series := assistantWatchSeriesReport{Label: "Port load"}
	for i := 0; i < 2000; i++ {
		series.Excursions = append(series.Excursions, assistantWatchExcursion{Start: "14:00:00", Seconds: 1, Peak: 1, Baseline: 0.5, Direction: "above"})
	}
	report.Series = []assistantWatchSeriesReport{series}

	content, err := assistantWatchReportMessage(info, report)
	if err != nil {
		t.Fatalf("assistantWatchReportMessage: %v", err)
	}
	headline, rest, _ := strings.Cut(content, "\n")
	if headline != "Watch finished: Port load and Starboard load (5 min)" {
		t.Fatalf("unexpected headline %q", headline)
	}
	if !strings.Contains(rest, "not a message from the skipper") {
		t.Fatalf("expected the report to say it is automatic, got %q", rest[:200])
	}
	if len(rest) > assistantMaxToolResultChars+1000 {
		t.Fatalf("report not bounded: %d chars", len(rest))
	}
	if !strings.Contains(rest, `"truncated":true`) {
		t.Fatal("expected a trimmed report to say so")
	}
}

func TestAssistantHistoryMessages_WatchRowReplaysAsUserTurn(t *testing.T) {
	msgs := []assistantMessage{
		{Role: "user", Content: "watch the engines"},
		{Role: "assistant", Content: "I'll report back at 14:05."},
		{Role: "watch", Content: "Watch finished: Port load (5 min)\n{...}"},
	}
	out, err := assistantHistoryMessages(msgs, assistantDocumentLookup)
	if err != nil {
		t.Fatalf("assistantHistoryMessages: %v", err)
	}
	if out[2].Role != "user" || !strings.HasPrefix(string(out[2].Content), "Watch finished") {
		t.Fatalf("expected the watch row as a user turn carrying the report, got %+v", out[2])
	}
}

// ── the start_watch tool ────────────────────────────────────────────────

func TestExecuteStartWatch_StartsInThisConversation(t *testing.T) {
	h := newWatchTestHarness(t)
	h.readings["propulsion.port.engineLoad"] = constantReading(0.5)
	deps := assistantToolDeps{
		now:     func() time.Time { return h.t0 },
		watches: h.reg,
		today:   time.Date(2026, 10, 4, 0, 0, 0, 0, time.UTC),
		vesselState: func() (vesselStateData, error) {
			return vesselStateData{Latitude: -20.1, Longitude: 149.0}, nil
		},
	}

	if _, err := deps.execute(context.Background(), "start_watch", json.RawMessage(`{"paths":[{"path":"propulsion.port.engineLoad"}],"minutes":5,"reason":"x"}`)); err == nil {
		t.Fatal("expected start_watch outside a conversation to fail")
	}

	ctx := withAssistantConversationID(context.Background(), "conv-1")
	out, err := deps.execute(ctx, "start_watch", json.RawMessage(`{"paths":[{"path":"propulsion.port.engineLoad","label":"Port load"}],"minutes":5,"reason":"x"}`))
	if err != nil {
		t.Fatalf("start_watch: %v", err)
	}
	var result struct {
		WatchID string `json:"watch_id"`
		EndsAt  string `json:"ends_at"`
		Note    string `json:"note"`
	}
	if err := json.Unmarshal([]byte(out), &result); err != nil {
		t.Fatalf("unmarshal: %v (%s)", err, out)
	}
	if result.WatchID == "" || result.EndsAt != "14:05" || !strings.Contains(result.Note, "restart") {
		t.Fatalf("unexpected result: %s", out)
	}
	info, ok := h.reg.get("conv-1")
	if !ok || !info.Today.Equal(deps.today) {
		t.Fatalf("expected the watch registered with the run's date, got %+v ok=%v", info, ok)
	}
}

// ── the end-of-watch handoff ────────────────────────────────────────────

func watchFollowUpSetup(t *testing.T, enabled bool) (*assistantStore, assistantConversation) {
	t.Helper()
	secrets := withTestSecretsStore(t)
	if err := secrets.Set("OPENROUTER_API_KEY", "sk-test"); err != nil {
		t.Fatalf("Set: %v", err)
	}
	store := withTestAssistantStore(t)
	t.Setenv("SETTINGS_FILE", writeAssistantSettingsFixture(t, enabled, "openai/gpt-4o"))
	conv, err := store.CreateConversation("Engines")
	if err != nil {
		t.Fatalf("CreateConversation: %v", err)
	}
	return store, conv
}

func TestAssistantWatchFollowUp_AppendsReportAndStartsOneTurn(t *testing.T) {
	store, conv := watchFollowUpSetup(t, true)

	var mu sync.Mutex
	var gotHistory []openRouterMessage
	var gotToday time.Time
	var gotConversation, gotLive string
	swapAssistantRunner(t, func(apiKey, model, settingsPath string, autoRouter assistantAutoRouterOptions, webSearch bool, today time.Time, emit assistantEmitter) assistantRunnerFace {
		mu.Lock()
		gotToday = today
		mu.Unlock()
		return assistantRunnerFunc(func(ctx context.Context, systemStable, systemLive string, history []openRouterMessage) (assistantReply, error) {
			mu.Lock()
			gotHistory = history
			gotConversation = assistantConversationIDFrom(ctx)
			gotLive = systemLive
			mu.Unlock()
			emit("status", assistantStatus("Thinking…"))
			return assistantReply{Content: "Port load spiked twice.", Model: "openai/gpt-4o"}, nil
		})
	})

	// The watch is still registered, as "reporting", while its follow-up
	// turn runs; the turn must not be told a watch is still running.
	h := newWatchTestHarness(t)
	h.readings["a.one"] = constantReading(1)
	withTestAssistantWatches(t, h.reg)
	if _, err := h.reg.start(watchRequest(conv.ID, `{"paths":[{"path":"a.one"}],"minutes":5,"reason":"x"}`)); err != nil {
		t.Fatalf("start: %v", err)
	}
	h.reg.mu.Lock()
	h.reg.watches[conv.ID].info.Status = assistantWatchStatusReporting
	h.reg.mu.Unlock()

	info := assistantWatchInfo{ID: "w1", ConversationID: conv.ID, Subject: "Port load", Minutes: 5, Today: time.Date(2026, 10, 4, 0, 0, 0, 0, time.UTC)}
	if !assistantWatchFollowUp(info, "Watch finished: Port load (5 min)\nreport") {
		t.Fatal("expected the follow-up turn to start")
	}

	waitForConditionT(t, 2*time.Second, func() bool {
		msgs, _ := store.ListMessages(conv.ID)
		return len(msgs) == 2
	})
	msgs, _ := store.ListMessages(conv.ID)
	if msgs[0].Role != "watch" || !strings.HasPrefix(msgs[0].Content, "Watch finished") {
		t.Fatalf("expected the report persisted as a watch row, got %+v", msgs[0])
	}
	if msgs[1].Role != "assistant" || msgs[1].Content != "Port load spiked twice." {
		t.Fatalf("expected Mate's reply persisted, got %+v", msgs[1])
	}
	mu.Lock()
	defer mu.Unlock()
	if len(gotHistory) != 1 || gotHistory[0].Role != "user" {
		t.Fatalf("expected the report replayed to the model as one user turn, got %+v", gotHistory)
	}
	if strings.Contains(gotLive, "A watch you started is running") {
		t.Fatalf("the report turn must not be told the watch is still running:\n%s", gotLive)
	}
	if gotConversation != conv.ID {
		t.Fatalf("expected the run's context to carry the conversation for start_watch, got %q", gotConversation)
	}
	if !gotToday.Equal(info.Today) {
		t.Fatalf("expected the watch's date to reach the runner, got %v", gotToday)
	}
	waitForConditionT(t, time.Second, func() bool { _, busy := globalAssistantRuns.get(conv.ID); return !busy })
}

func TestAssistantWatchFollowUp_SkipsWhenMateIsOff(t *testing.T) {
	store, conv := watchFollowUpSetup(t, false)
	swapAssistantRunner(t, func(apiKey, model, settingsPath string, autoRouter assistantAutoRouterOptions, webSearch bool, today time.Time, emit assistantEmitter) assistantRunnerFace {
		t.Fatal("no runner may be built while Mate is switched off")
		return nil
	})
	if assistantWatchFollowUp(assistantWatchInfo{ID: "w1", ConversationID: conv.ID}, "Watch finished: x\nreport") {
		t.Fatal("expected no follow-up while Mate is off")
	}
	if msgs, _ := store.ListMessages(conv.ID); len(msgs) != 0 {
		t.Fatalf("expected nothing appended, got %+v", msgs)
	}
}

func TestAssistantWatchFollowUp_SkipsWhenConversationIsGone(t *testing.T) {
	store, conv := watchFollowUpSetup(t, true)
	swapAssistantRunner(t, func(apiKey, model, settingsPath string, autoRouter assistantAutoRouterOptions, webSearch bool, today time.Time, emit assistantEmitter) assistantRunnerFace {
		t.Fatal("no runner may be built for a deleted conversation")
		return nil
	})
	if err := store.DeleteConversation(conv.ID); err != nil {
		t.Fatalf("DeleteConversation: %v", err)
	}
	if assistantWatchFollowUp(assistantWatchInfo{ID: "w1", ConversationID: conv.ID}, "Watch finished: x\nreport") {
		t.Fatal("expected no follow-up for a deleted conversation")
	}
	if _, busy := globalAssistantRuns.get(conv.ID); busy {
		t.Fatal("a skipped follow-up must not leave a run registered")
	}
}

// ── HTTP: GET and DELETE the watch ──────────────────────────────────────

func withTestAssistantWatches(t *testing.T, reg *assistantWatchRegistry) {
	t.Helper()
	prev := globalAssistantWatches
	globalAssistantWatches = reg
	t.Cleanup(func() { globalAssistantWatches = prev })
}

func TestAssistantWatchHandlers_GetAndDelete(t *testing.T) {
	h := newWatchTestHarness(t)
	h.readings["a.one"] = constantReading(1)
	withTestAssistantWatches(t, h.reg)

	c, rec := newAssistantEchoContext(http.MethodGet, "/api/assistant/conversations/conv-1/watch", "", "conv-1")
	if err := getAssistantWatchHandler(c); err != nil {
		t.Fatalf("get: %v", err)
	}
	if rec.Code != http.StatusNoContent {
		t.Fatalf("expected 204 with no watch, got %d", rec.Code)
	}

	if _, err := h.reg.start(watchRequest("conv-1", `{"paths":[{"path":"a.one","label":"House voltage"}],"minutes":5,"reason":"x"}`)); err != nil {
		t.Fatalf("start: %v", err)
	}
	c, rec = newAssistantEchoContext(http.MethodGet, "/api/assistant/conversations/conv-1/watch", "", "conv-1")
	if err := getAssistantWatchHandler(c); err != nil {
		t.Fatalf("get: %v", err)
	}
	var body struct {
		Watch struct {
			Subject string    `json:"subject"`
			EndsAt  time.Time `json:"ends_at"`
			Status  string    `json:"status"`
		} `json:"watch"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("unmarshal: %v (%s)", err, rec.Body.String())
	}
	if body.Watch.Subject != "House voltage" || body.Watch.Status != "watching" || !body.Watch.EndsAt.Equal(h.t0.Add(5*time.Minute)) {
		t.Fatalf("unexpected watch body: %s", rec.Body.String())
	}
	if strings.Contains(rec.Body.String(), "a.one") {
		t.Fatalf("the chip payload must not carry raw paths: %s", rec.Body.String())
	}

	c, rec = newAssistantEchoContext(http.MethodDelete, "/api/assistant/conversations/conv-1/watch", "", "conv-1")
	if err := deleteAssistantWatchHandler(c); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if rec.Code != http.StatusNoContent {
		t.Fatalf("expected 204 from delete, got %d", rec.Code)
	}
	if _, ok := h.reg.get("conv-1"); ok {
		t.Fatal("expected delete to stop the watch")
	}
}

func TestDeleteAssistantConversationHandler_StopsItsWatch(t *testing.T) {
	store := withTestAssistantStore(t)
	conv, err := store.CreateConversation("Engines")
	if err != nil {
		t.Fatalf("CreateConversation: %v", err)
	}
	h := newWatchTestHarness(t)
	h.readings["a.one"] = constantReading(1)
	withTestAssistantWatches(t, h.reg)
	if _, err := h.reg.start(watchRequest(conv.ID, `{"paths":[{"path":"a.one"}],"minutes":5,"reason":"x"}`)); err != nil {
		t.Fatalf("start: %v", err)
	}

	c, _ := newAssistantEchoContext(http.MethodDelete, "/api/assistant/conversations/"+conv.ID, "", conv.ID)
	if err := deleteAssistantConversationHandler(c); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if _, ok := h.reg.get(conv.ID); ok {
		t.Fatal("deleting a conversation must stop its watch")
	}
}

// assistantRunnerFunc adapts a plain function to assistantRunnerFace.
type assistantRunnerFunc func(ctx context.Context, systemStable, systemLive string, history []openRouterMessage) (assistantReply, error)

func (f assistantRunnerFunc) run(ctx context.Context, systemStable, systemLive string, history []openRouterMessage) (assistantReply, error) {
	return f(ctx, systemStable, systemLive, history)
}

// ── the system prompt ───────────────────────────────────────────────────

func TestAssistantSystemPrompt_WatchRuleAndActiveWatchLine(t *testing.T) {
	pc := basePromptContext()
	stable, live := assistantSystemPromptParts(pc)
	if !strings.Contains(stable, "use start_watch") || !strings.Contains(stable, "Never say you are watching") {
		t.Fatalf("expected the watch rule in the stable prompt, got:\n%s", stable)
	}
	if strings.Contains(live, "A watch you started is running") {
		t.Fatal("no watch line expected when none is running")
	}

	pc.ActiveWatch = &assistantWatchInfo{Subject: "Port load and Starboard load", EndsAt: time.Date(2026, 10, 4, 4, 5, 0, 0, time.UTC)}
	_, live = assistantSystemPromptParts(pc)
	if !strings.Contains(live, "A watch you started is running in this conversation: Port load and Starboard load, until") {
		t.Fatalf("expected the running watch in the live prompt, got:\n%s", live)
	}
}

// ── review fixes ────────────────────────────────────────────────────────

// The live snapshot keeps only the last writer's value and $source per path,
// so a per-path source filter would see a second publisher as gaps. There is
// no filter; a path fed by more than one source says so in the report.
func TestAssistantWatch_ReportsSourceSwitchesInsteadOfFiltering(t *testing.T) {
	if _, err := parseAssistantStartWatchArgs(json.RawMessage(`{"paths":[{"path":"a.b","source":"n2k.1"}],"minutes":5,"reason":"x"}`)); err == nil || !strings.Contains(err.Error(), "source") {
		t.Fatalf("expected a source filter to be refused, got %v", err)
	}

	h := newWatchTestHarness(t)
	h.readings["environment.depth.belowTransducer"] = func(now time.Time) assistantWatchReading {
		r := liveReading(5)
		if int(now.Sub(h.t0).Seconds())%2 == 0 {
			r.Source = "n2k.2"
		}
		return r
	}
	if _, err := h.reg.start(watchRequest("conv-1", `{"paths":[{"path":"environment.depth.belowTransducer"}],"minutes":1,"reason":"x"}`)); err != nil {
		t.Fatalf("start: %v", err)
	}
	h.tickThrough(h.ticker(0), 60)
	s := h.waitFinished().report.Series[0]
	if s.Samples != 60 || s.GapCount != 0 {
		t.Fatalf("a second publisher must not show as gaps, got %d samples %d gaps", s.Samples, s.GapCount)
	}
	if len(s.SourcesSeen) != 2 || s.SourceSwitches != 59 {
		t.Fatalf("expected two sources and 59 switches reported, got %v / %d", s.SourcesSeen, s.SourceSwitches)
	}
}

// A report whose turn cannot start is still kept in the conversation, so it
// is not lost and Mate sees it with the next question.
func TestAssistantWatchFollowUp_KeepsTheReportWhenTheConversationStaysBusy(t *testing.T) {
	store, conv := watchFollowUpSetup(t, true)
	prevWait := assistantWatchFollowUpDeadline
	assistantWatchFollowUpDeadline = 50 * time.Millisecond
	t.Cleanup(func() { assistantWatchFollowUpDeadline = prevWait })

	_, busy, ok := globalAssistantRuns.start(conv.ID)
	if !ok {
		t.Fatal("could not register a busy run")
	}
	t.Cleanup(func() { globalAssistantRuns.remove(conv.ID, busy) })
	swapAssistantRunner(t, func(apiKey, model, settingsPath string, autoRouter assistantAutoRouterOptions, webSearch bool, today time.Time, emit assistantEmitter) assistantRunnerFace {
		t.Fatal("no turn may start while the conversation stays busy")
		return nil
	})

	if assistantWatchFollowUp(assistantWatchInfo{ID: "w1", ConversationID: conv.ID}, "Watch finished: x (1 min)\nreport") {
		t.Fatal("expected no turn to start")
	}
	msgs, _ := store.ListMessages(conv.ID)
	if len(msgs) != 1 || msgs[0].Role != "watch" {
		t.Fatalf("expected the report kept as a watch row, got %+v", msgs)
	}
}

// A Stop that lands after the watch has ended is refused, so the chat keeps
// following it to Mate's answer instead of clearing the chip.
func TestDeleteAssistantWatchHandler_RefusesAWatchAlreadyReporting(t *testing.T) {
	h := newWatchTestHarness(t)
	h.readings["a.one"] = constantReading(1)
	withTestAssistantWatches(t, h.reg)
	if _, err := h.reg.start(watchRequest("conv-1", `{"paths":[{"path":"a.one"}],"minutes":5,"reason":"x"}`)); err != nil {
		t.Fatalf("start: %v", err)
	}
	h.reg.mu.Lock()
	h.reg.watches["conv-1"].info.Status = assistantWatchStatusReporting
	h.reg.mu.Unlock()

	c, rec := newAssistantEchoContext(http.MethodDelete, "/api/assistant/conversations/conv-1/watch", "", "conv-1")
	if err := deleteAssistantWatchHandler(c); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if rec.Code != http.StatusConflict || !strings.Contains(rec.Body.String(), "already finished") {
		t.Fatalf("expected 409 saying the watch already finished, got %d %s", rec.Code, rec.Body.String())
	}
}

// ── excursion floor follows the series' spread, not its zero point ──────

// The floor used to scale with the absolute level, so a 26.4 V bank hid a
// 1 V dip behind a 1.3 V floor and coolant at 358 K hid a 10 K spike behind
// a 17.9 K one. The floor now comes from the series' own spread.
func TestDetectAssistantWatchExcursions_FloorIgnoresTheUnitsZeroPoint(t *testing.T) {
	t0 := time.Date(2026, 10, 4, 4, 0, 0, 0, time.UTC)
	cases := []struct {
		name      string
		level     float64
		jitter    float64
		at        int
		offset    float64
		direction string
	}{
		{"1 V dip on a 26.4 V bank", 26.4, 0.02, 150, -1.0, "below"},
		{"10 K spike on coolant at 358 K", 358.0, 0.1, 150, 10.0, "above"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			values := make([]float64, 300)
			for i := range values {
				values[i] = tc.level + tc.jitter*math.Sin(float64(i))
			}
			values[tc.at] += tc.offset
			values[tc.at+1] += tc.offset
			excursions, count := detectAssistantWatchExcursions(watchSeries(t0, values), time.UTC)
			if count != 1 || len(excursions) != 1 || excursions[0].Direction != tc.direction || excursions[0].Seconds != 2 {
				t.Fatalf("expected one 2 s excursion %s, got %d: %+v", tc.direction, count, excursions)
			}
		})
	}

	// A quiet, noisy series at a high level still reports nothing.
	values := make([]float64, 300)
	for i := range values {
		values[i] = 358.0 + 0.3*float64(i%3-1)
	}
	if excursions, count := detectAssistantWatchExcursions(watchSeries(t0, values), time.UTC); count != 0 {
		t.Fatalf("expected no excursions in plain jitter at 358 K, got %+v", excursions)
	}
}

// ── difference only in matching units ───────────────────────────────────

// Volts minus amps means nothing: the difference series is taken only when
// both readings carry the same known units, and the report says why not.
func TestAssistantWatch_NoDifferenceAcrossDifferentUnits(t *testing.T) {
	h := newWatchTestHarness(t)
	h.readings["electrical.batteries.house.voltage"] = func(time.Time) assistantWatchReading {
		r := liveReading(26.4)
		r.Units = "V"
		return r
	}
	h.readings["electrical.batteries.house.current"] = func(time.Time) assistantWatchReading {
		r := liveReading(-12)
		r.Units = "A"
		return r
	}
	if _, err := h.reg.start(watchRequest("conv-1", `{"paths":[{"path":"electrical.batteries.house.voltage","label":"House voltage"},{"path":"electrical.batteries.house.current","label":"House current"}],"minutes":1,"reason":"x"}`)); err != nil {
		t.Fatalf("start: %v", err)
	}
	h.tickThrough(h.ticker(0), 60)
	report := h.waitFinished().report
	if report.Difference != nil {
		t.Fatalf("expected no difference between volts and amps, got %+v", report.Difference)
	}
	if !strings.Contains(report.DifferenceNote, "different units") {
		t.Fatalf("expected the report to say the units differ, got %q", report.DifferenceNote)
	}
}

// ── start_watch reports the readings it checked ─────────────────────────

// value_now is the reading start() checked, not a second read that may find
// the path gone and report 0.
func TestExecuteStartWatch_ValueNowIsTheCheckedReading(t *testing.T) {
	h := newWatchTestHarness(t)
	var mu sync.Mutex
	reads := 0
	h.readings["electrical.batteries.house.voltage"] = func(time.Time) assistantWatchReading {
		mu.Lock()
		defer mu.Unlock()
		reads++
		if reads == 1 {
			r := liveReading(26.4)
			r.Units = "V"
			return r
		}
		return assistantWatchReading{}
	}
	deps := assistantToolDeps{
		now:     func() time.Time { return h.t0 },
		watches: h.reg,
		vesselState: func() (vesselStateData, error) {
			return vesselStateData{Latitude: -20.1, Longitude: 149.0}, nil
		},
	}
	ctx := withAssistantConversationID(context.Background(), "conv-1")
	out, err := deps.execute(ctx, "start_watch", json.RawMessage(`{"paths":[{"path":"electrical.batteries.house.voltage","label":"House voltage"}],"minutes":5,"reason":"x"}`))
	if err != nil {
		t.Fatalf("start_watch: %v", err)
	}
	var result assistantStartWatchResult
	if err := json.Unmarshal([]byte(out), &result); err != nil {
		t.Fatalf("unmarshal: %v (%s)", err, out)
	}
	if len(result.Paths) != 1 || result.Paths[0].ValueNow != 26.4 || result.Paths[0].Units != "V" {
		t.Fatalf("expected value_now 26.4 V from the checked reading, got %s", out)
	}
}

// ── the chip's end time is Mate's end time ──────────────────────────────

// The chip shows the end time in the same vessel-local clock Mate states,
// formatted by the server, not the viewing device's clock.
func TestGetAssistantWatchHandler_CarriesTheVesselLocalEndTime(t *testing.T) {
	h := newWatchTestHarness(t)
	h.readings["a.one"] = constantReading(1)
	withTestAssistantWatches(t, h.reg)
	if _, err := h.reg.start(watchRequest("conv-1", `{"paths":[{"path":"a.one"}],"minutes":5,"reason":"x"}`)); err != nil {
		t.Fatalf("start: %v", err)
	}
	c, rec := newAssistantEchoContext(http.MethodGet, "/api/assistant/conversations/conv-1/watch", "", "conv-1")
	if err := getAssistantWatchHandler(c); err != nil {
		t.Fatalf("get: %v", err)
	}
	var body struct {
		Watch struct {
			EndsAtLocal string `json:"ends_at_local"`
		} `json:"watch"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("unmarshal: %v (%s)", err, rec.Body.String())
	}
	// t0 is 04:00 UTC; the request's vessel zone is UTC+10; five minutes on.
	if body.Watch.EndsAtLocal != "14:05" {
		t.Fatalf("expected ends_at_local 14:05, got %s", rec.Body.String())
	}
}
