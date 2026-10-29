package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"
	"testing"
	"time"
)

// ── what a failed request cost ──────────────────────────────────────────

func TestOpenRouterFailureCharge(t *testing.T) {
	dial := &net.OpError{Op: "dial", Err: errors.New("connection refused")}
	read := &net.OpError{Op: "read", Err: errors.New("connection reset")}
	for name, tc := range map[string]struct {
		err  error
		want openRouterCharge
		id   string
	}{
		"refused by status":        {&openRouterStatusError{text: "openrouter status 500: boom"}, chargeNone, ""},
		"dial error":               {&openRouterRequestError{err: dial}, chargeNone, ""},
		"dns error":                {&openRouterRequestError{err: &net.DNSError{Err: "no such host", Name: "openrouter.ai"}}, chargeNone, ""},
		"timeout waiting":          {&openRouterRequestError{err: context.DeadlineExceeded}, chargeUnknown, ""},
		"reset after sending":      {&openRouterRequestError{err: read}, chargeUnknown, ""},
		"stream cut with id":       {&openRouterPartialError{GenerationID: "gen-1", err: errors.New("stream ended early")}, chargeLookup, "gen-1"},
		"stream cut without id":    {&openRouterPartialError{err: errors.New("stream ended early")}, chargeUnknown, ""},
		"wrapped like the callers": {fmt.Errorf("web search failed: %w", &openRouterRequestError{err: dial}), chargeNone, ""},
		"not an openrouter error":  {errors.New("query must not be empty"), chargeNone, ""},
	} {
		got, id := openRouterFailureCharge(tc.err)
		if got != tc.want || id != tc.id {
			t.Errorf("%s: charge %v id %q, want %v %q", name, got, id, tc.want, tc.id)
		}
	}
}

func TestAssistantCostMeter_ResolveLooksUpPendingGenerations(t *testing.T) {
	prev := generationCostLookupDelays
	generationCostLookupDelays = []time.Duration{time.Millisecond, time.Millisecond, time.Millisecond}
	t.Cleanup(func() { generationCostLookupDelays = prev })

	calls := 0
	m := &assistantCostMeter{}
	m.add(0.01)
	m.addPending("gen-1")
	m.addPending("gen-1") // the same generation is looked up once
	usd, known := m.resolve(context.Background(), func(_ context.Context, id string) (float64, error) {
		calls++
		if calls < 3 {
			return 0, errors.New("not found yet")
		}
		return 0.04, nil
	})
	if !known || usd < 0.0499 || usd > 0.0501 || calls != 3 {
		t.Fatalf("usd %v known %v after %d lookups", usd, known, calls)
	}

	m = &assistantCostMeter{}
	m.addPending("gen-2")
	if _, known := m.resolve(context.Background(), func(context.Context, string) (float64, error) { return 0, errors.New("no") }); known {
		t.Fatal("a cost that cannot be found is unknown, never zero")
	}
	m = &assistantCostMeter{}
	m.markUnknown()
	if _, known := m.resolve(context.Background(), nil); known {
		t.Fatal("an unknown stays unknown")
	}
}

func TestOpenRouterGenerationCost(t *testing.T) {
	var gotURL, gotAuth string
	doer := doerFunc(func(req *http.Request) (*http.Response, error) {
		gotURL, gotAuth = req.URL.String(), req.Header.Get("Authorization")
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(`{"data":{"id":"gen-9","total_cost":0.0123}}`)), Header: http.Header{}}, nil
	})
	cost, err := openRouterGenerationCost(context.Background(), doer, "sk-test", "gen-9")
	if err != nil || cost != 0.0123 || gotURL != "https://openrouter.ai/api/v1/generation?id=gen-9" || gotAuth != "Bearer sk-test" {
		t.Fatalf("cost %v err %v url %s auth %s", cost, err, gotURL, gotAuth)
	}
	missing := doerFunc(func(*http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: 404, Body: io.NopCloser(strings.NewReader(`{"error":{"message":"not found"}}`)), Header: http.Header{}}, nil
	})
	if _, err := openRouterGenerationCost(context.Background(), missing, "k", "gen-9"); err == nil {
		t.Fatal("a 404 (OpenRouter has not caught up yet) is an error to retry")
	}
}

type doerFunc func(*http.Request) (*http.Response, error)

func (f doerFunc) Do(req *http.Request) (*http.Response, error) { return f(req) }

func TestAssistantRunner_TransportFailuresAreChargedAccordingToHowFarTheyGot(t *testing.T) {
	run := func(t *testing.T, resp *http.Response, doErr error) (usd float64, known bool, pending []string) {
		t.Helper()
		doer := &queuedChatDoer{responses: []*http.Response{resp}, errs: []error{doErr}}
		meter := &assistantCostMeter{}
		emit, _ := recordingEmitter()
		runner := &assistantRunner{doer: doer, apiKey: "k", model: "m", tools: &fakeToolExecutor{}, emit: emit}
		if _, err := runner.run(withAssistantCostMeter(context.Background(), meter), "system", "", nil); err == nil {
			t.Fatal("the run must fail")
		}
		usd, known = meter.snapshot()
		return usd, known, meter.pendingIDs()
	}

	t.Run("connection refused costs nothing", func(t *testing.T) {
		usd, known, pending := run(t, nil, &net.OpError{Op: "dial", Err: errors.New("connection refused")})
		if usd != 0 || !known || len(pending) != 0 {
			t.Fatalf("usd %v known %v pending %v", usd, known, pending)
		}
	})
	t.Run("a stream cut after the first chunk is looked up by its generation id", func(t *testing.T) {
		body := `data: {"id":"gen-77","model":"m","choices":[{"index":0,"delta":{"role":"assistant","content":"par"}}]}` + "\n\n"
		usd, known, pending := run(t, &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(body)), Header: http.Header{}}, nil)
		if usd != 0 || !known || len(pending) != 1 || pending[0] != "gen-77" {
			t.Fatalf("usd %v known %v pending %v", usd, known, pending)
		}
	})
	t.Run("a timeout waiting for the response is unknown", func(t *testing.T) {
		_, known, pending := run(t, nil, context.DeadlineExceeded)
		if known || len(pending) != 0 {
			t.Fatalf("known %v pending %v", known, pending)
		}
	})
}

type searchSpendingTools struct {
	fakeToolExecutor
	usage openRouterUsage
	ids   []string
}

func (s *searchSpendingTools) drainUsage() openRouterUsage {
	u := s.usage
	s.usage = openRouterUsage{}
	return u
}

func (s *searchSpendingTools) drainRisk() ([]string, bool) {
	ids := s.ids
	s.ids = nil
	return ids, false
}

func TestAssistantRunner_AFailedToolRoundStillCountsSearchSpend(t *testing.T) {
	round0 := chatResponse(t, http.StatusOK, openRouterChatResponse{
		Model: "m",
		Choices: []openRouterChoice{{Message: openRouterMessage{Role: "assistant", ToolCalls: []openRouterToolCall{
			// No id: runToolRound refuses the round.
			{ID: "", Type: "function", Function: openRouterToolCallFunction{Name: "search_web", Arguments: openRouterArguments(`{}`)}},
		}}}},
		Usage: usage(10, 5, 0.01),
	})
	tools := &searchSpendingTools{usage: openRouterUsage{Cost: 0.05}, ids: []string{"gen-search"}}
	meter := &assistantCostMeter{}
	emit, _ := recordingEmitter()
	runner := &assistantRunner{doer: &queuedChatDoer{responses: []*http.Response{round0}, errs: []error{nil}}, apiKey: "k", model: "m", tools: tools, emit: emit}
	if _, err := runner.run(withAssistantCostMeter(context.Background(), meter), "system", "", nil); err == nil {
		t.Fatal("the round fails")
	}
	usd, known := meter.snapshot()
	if usd < 0.0599 || usd > 0.0601 || !known || len(meter.pendingIDs()) != 1 {
		t.Fatalf("usd %v known %v pending %v", usd, known, meter.pendingIDs())
	}
}

func TestSearchFailuresReachTheRunsMeter(t *testing.T) {
	dial := &net.OpError{Op: "dial", Err: errors.New("connection refused")}
	deps := assistantToolDeps{}.withWebSearch(doerFunc(func(*http.Request) (*http.Response, error) { return nil, dial }), "k", func() (string, error) { return "m", nil })
	if _, err := deps.webSearch(context.Background(), "tides"); err == nil {
		t.Fatal("search fails")
	}
	if ids, unknown := deps.drainRisk(); len(ids) != 0 || unknown {
		t.Fatalf("a search that never connected cost nothing: %v %v", ids, unknown)
	}
	cut := doerFunc(func(*http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(`{"id":"gen-s","choices":`)), Header: http.Header{}}, nil
	})
	deps = assistantToolDeps{}.withWebSearch(cut, "k", func() (string, error) { return "m", nil })
	_, _ = deps.webSearch(context.Background(), "tides")
	if ids, unknown := deps.drainRisk(); len(ids) != 0 || !unknown {
		t.Fatalf("a body cut off before it parsed has no id, so its cost is unknown: %v %v", ids, unknown)
	}
}

func TestAssistantCostMeter_KeepsTheCostOfAFailedRun(t *testing.T) {
	round0 := chatResponse(t, http.StatusOK, openRouterChatResponse{
		Model: "m",
		Choices: []openRouterChoice{{Message: openRouterMessage{Role: "assistant", ToolCalls: []openRouterToolCall{
			{ID: "c1", Type: "function", Function: openRouterToolCallFunction{Name: "find_places", Arguments: openRouterArguments(`{}`)}},
		}}}},
		Usage: usage(100, 20, 0.01),
	})
	run := func(second *http.Response, secondErr error) (float64, bool) {
		doer := &queuedChatDoer{responses: []*http.Response{round0, second}, errs: []error{nil, secondErr}}
		// round0 is consumed once; rebuild per run.
		round0 = chatResponse(t, http.StatusOK, openRouterChatResponse{
			Model: "m",
			Choices: []openRouterChoice{{Message: openRouterMessage{Role: "assistant", ToolCalls: []openRouterToolCall{
				{ID: "c1", Type: "function", Function: openRouterToolCallFunction{Name: "find_places", Arguments: openRouterArguments(`{}`)}},
			}}}},
			Usage: usage(100, 20, 0.01),
		})
		meter := &assistantCostMeter{}
		emit, _ := recordingEmitter()
		runner := &assistantRunner{doer: doer, apiKey: "k", model: "m", tools: &fakeToolExecutor{results: map[string]string{"find_places": `{}`}}, emit: emit}
		if _, err := runner.run(withAssistantCostMeter(context.Background(), meter), "system", "", nil); err == nil {
			t.Fatal("the second round fails")
		}
		return meter.snapshot()
	}
	// The connection drops in the second round: what it cost is not known.
	if usd, known := run(nil, errors.New("connection reset")); usd != 0.01 || known {
		t.Errorf("transport failure: %v known=%v", usd, known)
	}
	// OpenRouter refused the second request outright: nothing was charged for it.
	refused := chatResponse(t, http.StatusInternalServerError, openRouterChatResponse{Error: &openRouterAPIError{Message: "boom"}})
	if usd, known := run(refused, nil); usd != 0.01 || !known {
		t.Errorf("refused request: %v known=%v", usd, known)
	}
}
