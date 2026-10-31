package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"
)

// The run budget (ADR 0240): rounds continue while they make progress, a
// stall or a ceiling withdraws the tools, and the final answer always has its
// own time.

// shortBudget shrinks the tool phase and the final reserve for one test.
func shortBudget(t *testing.T, toolPhase, finalReserve time.Duration) {
	t.Helper()
	origPhase, origReserve := assistantToolPhaseTimeout, assistantFinalReserve
	assistantToolPhaseTimeout, assistantFinalReserve = toolPhase, finalReserve
	t.Cleanup(func() { assistantToolPhaseTimeout, assistantFinalReserve = origPhase, origReserve })
}

// newCallRound answers with one get_tides call whose arguments differ per n,
// so each is new to the run.
func newCallRound(t *testing.T, n int) *http.Response {
	t.Helper()
	return toolCallResponse(t, fmt.Sprintf("call_%d", n), "get_tides", fmt.Sprintf(`{"lat":%d,"lon":2}`, n), openRouterUsage{})
}

func queued(responses ...*http.Response) *queuedChatDoer {
	return &queuedChatDoer{responses: responses, errs: make([]error, len(responses))}
}

func TestAssistantBudget_ProgressingRoundsRunPastTheOldEightRoundCap(t *testing.T) {
	const rounds = 12
	var responses []*http.Response
	for i := 0; i < rounds; i++ {
		responses = append(responses, newCallRound(t, i))
	}
	responses = append(responses, finalResponse(t, "all twelve legs checked", "m", openRouterUsage{}))
	doer := queued(responses...)
	emit, _ := recordingEmitter()

	runner := &assistantRunner{doer: doer, apiKey: "key", model: "m", tools: &fakeToolExecutor{}, emit: emit}
	reply, err := runner.run(context.Background(), "system", "", nil)
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if reply.Content != "all twelve legs checked" || reply.ToolRounds != rounds {
		t.Fatalf("got content %q after %d rounds, want the answer after %d", reply.Content, reply.ToolRounds, rounds)
	}
	for i, req := range doer.requests {
		if len(req.Tools) == 0 {
			t.Fatalf("request %d withdrew the tools although every round made progress", i)
		}
	}
}

func TestAssistantBudget_RepeatedIdenticalCallsStallIntoAForcedFinalAnswer(t *testing.T) {
	var responses []*http.Response
	for i := 0; i < stalledToolRounds; i++ {
		responses = append(responses, toolCallResponse(t, fmt.Sprintf("call_%d", i), "get_tides", `{"lat":1,"lon":2}`, openRouterUsage{}))
	}
	responses = append(responses, finalResponse(t, "answer from what I have", "m", openRouterUsage{}))
	doer := queued(responses...)
	emit, _ := recordingEmitter()
	rec := &assistantRunRecorder{}

	runner := &assistantRunner{doer: doer, apiKey: "key", model: "m", tools: &fakeToolExecutor{}, emit: emit}
	reply, err := runner.run(withAssistantRunRecorder(context.Background(), rec), "system", "", nil)
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if reply.Content != "answer from what I have" {
		t.Fatalf("content = %q", reply.Content)
	}
	if got := len(doer.requests); got != stalledToolRounds+1 {
		t.Fatalf("requests = %d, want %d (one new call, %d repeats, then the final answer)", got, stalledToolRounds+1, assistantMaxStalledRounds)
	}
	if last := doer.requests[len(doer.requests)-1]; len(last.Tools) != 0 {
		t.Fatalf("the final request still offered %d tools", len(last.Tools))
	}
	rounds, stop := rec.result()
	if stop != assistantStopStalled {
		t.Fatalf("stop reason = %q, want %q", stop, assistantStopStalled)
	}
	wantProgress := []bool{true, false, false, false}
	for i, r := range rounds {
		if r.Progress != wantProgress[i] {
			t.Fatalf("round %d progress = %v, want %v", i, r.Progress, wantProgress[i])
		}
	}
	if !rounds[len(rounds)-1].ForcedFinal {
		t.Fatalf("the last round should be marked as the forced final: %+v", rounds[len(rounds)-1])
	}
}

func TestAssistantBudget_ArgumentsWrittenDifferentlyAreTheSameCall(t *testing.T) {
	doer := queued(
		toolCallResponse(t, "a", "get_tides", `{"lat":1,"lon":2}`, openRouterUsage{}),
		toolCallResponse(t, "b", "get_tides", `{ "lon": 2,  "lat": 1 }`, openRouterUsage{}),
		toolCallResponse(t, "c", "get_tides", `{"lon":2,"lat":1}`, openRouterUsage{}),
		finalResponse(t, "done", "m", openRouterUsage{}),
	)
	emit, _ := recordingEmitter()
	runner := &assistantRunner{doer: doer, apiKey: "key", model: "m", tools: &fakeToolExecutor{}, emit: emit}
	if _, err := runner.run(context.Background(), "system", "", nil); err != nil {
		t.Fatalf("run: %v", err)
	}
	if len(doer.requests[3].Tools) != 0 {
		t.Fatal("two reorderings of the first call should have stalled the run")
	}
}

func TestAssistantBudget_NewCallsThatAllFailAreNotProgress(t *testing.T) {
	var responses []*http.Response
	for i := 0; i < stalledToolRounds-1; i++ {
		responses = append(responses, newCallRound(t, i))
	}
	responses = append(responses, finalResponse(t, "tides were down", "m", openRouterUsage{}))
	doer := queued(responses...)
	emit, _ := recordingEmitter()
	tools := &fakeToolExecutor{errs: map[string]error{"get_tides": errors.New("tide station offline")}}

	runner := &assistantRunner{doer: doer, apiKey: "key", model: "m", tools: tools, emit: emit}
	if _, err := runner.run(context.Background(), "system", "", nil); err != nil {
		t.Fatalf("run: %v", err)
	}
	// Two rounds of new but failing calls stall the run, so the third request
	// is the forced final answer.
	if got := len(doer.requests); got != assistantMaxStalledRounds+1 {
		t.Fatalf("requests = %d, want %d", got, assistantMaxStalledRounds+1)
	}
	if last := doer.requests[len(doer.requests)-1]; len(last.Tools) != 0 {
		t.Fatal("the last request should have withdrawn the tools")
	}
}

func TestAssistantBudget_RoundCeilingStillApplies(t *testing.T) {
	var responses []*http.Response
	for i := 0; i < assistantMaxToolRounds; i++ {
		responses = append(responses, newCallRound(t, i))
	}
	responses = append(responses, finalResponse(t, "enough", "m", openRouterUsage{}))
	doer := queued(responses...)
	emit, _ := recordingEmitter()
	rec := &assistantRunRecorder{}

	runner := &assistantRunner{doer: doer, apiKey: "key", model: "m", tools: &fakeToolExecutor{}, emit: emit}
	reply, err := runner.run(withAssistantRunRecorder(context.Background(), rec), "system", "", nil)
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if reply.ToolRounds != assistantMaxToolRounds || len(doer.requests) != assistantMaxToolRounds+1 {
		t.Fatalf("rounds = %d, requests = %d, want %d rounds and one more request", reply.ToolRounds, len(doer.requests), assistantMaxToolRounds)
	}
	if len(doer.requests[assistantMaxToolRounds].Tools) != 0 {
		t.Fatal("the request after the ceiling should have withdrawn the tools")
	}
	if _, stop := rec.result(); stop != assistantStopRoundCeiling {
		t.Fatalf("stop reason = %q, want %q", stop, assistantStopRoundCeiling)
	}
}

// deadlineDoer answers from a queued doer and records how long each request
// had left before its context expired.
type deadlineDoer struct {
	*queuedChatDoer
	mu   sync.Mutex
	left []time.Duration
}

func (d *deadlineDoer) Do(req *http.Request) (*http.Response, error) {
	left := time.Duration(-1)
	if dl, ok := req.Context().Deadline(); ok {
		left = time.Until(dl)
	}
	d.mu.Lock()
	d.left = append(d.left, left)
	d.mu.Unlock()
	return d.queuedChatDoer.Do(req)
}

func TestAssistantBudget_ForcedFinalGetsItsReservedTimeWhenTheToolPhaseUsedAll(t *testing.T) {
	const reserve = 5 * time.Second
	shortBudget(t, 80*time.Millisecond, reserve)

	doer := &deadlineDoer{queuedChatDoer: queued(
		newCallRound(t, 0),
		finalResponse(t, "answer written on its own clock", "m", openRouterUsage{}),
	)}
	emit, _ := recordingEmitter()
	rec := &assistantRunRecorder{}
	// The tool blocks until the tool phase's clock stops it.
	tools := &slowToolExecutor{delay: time.Minute}

	runner := &assistantRunner{doer: doer, apiKey: "key", model: "m", tools: tools, emit: emit}
	reply, err := runner.run(withAssistantRunRecorder(context.Background(), rec), "system", "", nil)
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if reply.Content != "answer written on its own clock" {
		t.Fatalf("content = %q", reply.Content)
	}
	if len(doer.left) != 2 {
		t.Fatalf("expected two completions, got %d", len(doer.left))
	}
	if doer.left[1] < reserve-time.Second {
		t.Fatalf("the final completion had %s left, want about the reserved %s", doer.left[1], reserve)
	}
	if _, stop := rec.result(); stop != assistantStopTimeCeiling {
		t.Fatalf("stop reason = %q, want %q", stop, assistantStopTimeCeiling)
	}
	if last := doer.requests[1]; len(last.Tools) != 0 {
		t.Fatal("the final request should have withdrawn the tools")
	}
}

func TestAssistantBudget_ARoundInFlightWhenThePhaseEndsIsAbandonedForTheFinalAnswer(t *testing.T) {
	shortBudget(t, 40*time.Millisecond, 5*time.Second)
	// The first round's tool takes longer than the whole phase, so no further
	// tool round may start: the next request is the final answer.
	doer := queued(
		newCallRound(t, 0),
		finalResponse(t, "wrapped up", "m", openRouterUsage{}),
	)
	emit, _ := recordingEmitter()
	rec := &assistantRunRecorder{}
	runner := &assistantRunner{doer: doer, apiKey: "key", model: "m", tools: &slowToolExecutor{delay: time.Minute}, emit: emit}
	reply, err := runner.run(withAssistantRunRecorder(context.Background(), rec), "system", "", nil)
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if reply.Content != "wrapped up" || len(doer.requests) != 2 || len(doer.requests[1].Tools) != 0 {
		t.Fatalf("content %q after %d requests; want the answer from a tool-free second request", reply.Content, len(doer.requests))
	}
	rounds, _ := rec.result()
	if len(rounds) != 2 || rounds[0].Error == "" || len(rounds[0].Calls) != 1 || !rounds[1].ForcedFinal {
		t.Fatalf("the abandoned round and its call should be in the trace: %+v", rounds)
	}
}

func TestAssistantBudget_TheRunCeilingIsToolPhasePlusFinalReserve(t *testing.T) {
	shortBudget(t, 60*time.Millisecond, 150*time.Millisecond)
	calls := 0
	doer := budgetDoerFunc(func(req *http.Request) (*http.Response, error) {
		calls++
		if calls == 1 {
			return newCallRound(t, 0), nil
		}
		// The final completion never finishes: only its own deadline ends it.
		<-req.Context().Done()
		return nil, req.Context().Err()
	})
	emit, _ := recordingEmitter()
	rec := &assistantRunRecorder{}
	runner := &assistantRunner{doer: doer, apiKey: "key", model: "m", tools: &slowToolExecutor{delay: time.Minute}, emit: emit}

	start := time.Now()
	_, err := runner.run(withAssistantRunRecorder(context.Background(), rec), "system", "", nil)
	took := time.Since(start)
	if err == nil {
		t.Fatal("expected the run to fail when the final answer never arrives")
	}
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("error = %v, want a deadline error", err)
	}
	if took < 150*time.Millisecond || took > assistantRunCeiling()+time.Second {
		t.Fatalf("took %s, want between the final reserve and the ceiling %s", took, assistantRunCeiling())
	}
	if _, stop := rec.result(); stop != assistantStopTimeCeiling {
		t.Fatalf("stop reason = %q, want %q", stop, assistantStopTimeCeiling)
	}
}

type budgetDoerFunc func(*http.Request) (*http.Response, error)

func (f budgetDoerFunc) Do(req *http.Request) (*http.Response, error) { return f(req) }

func TestAssistantBudget_CancelStopsEverythingAtOnce(t *testing.T) {
	doer := queued(newCallRound(t, 0), finalResponse(t, "never", "m", openRouterUsage{}))
	emit, _ := recordingEmitter()
	rec := &assistantRunRecorder{}
	runner := &assistantRunner{doer: doer, apiKey: "key", model: "m", tools: &slowToolExecutor{delay: time.Minute}, emit: emit}

	ctx, cancel := context.WithCancel(withAssistantRunRecorder(context.Background(), rec))
	go func() {
		time.Sleep(50 * time.Millisecond)
		cancel()
	}()
	start := time.Now()
	_, err := runner.run(ctx, "system", "", nil)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("error = %v, want cancelled", err)
	}
	if time.Since(start) > 2*time.Second {
		t.Fatal("cancel did not stop the run promptly")
	}
	if len(doer.requests) != 1 {
		t.Fatalf("a cancelled run went on to send %d requests", len(doer.requests))
	}
	if _, stop := rec.result(); stop != assistantStopCancelled {
		t.Fatalf("stop reason = %q, want %q", stop, assistantStopCancelled)
	}
}

// cacheNotingTools is a tool executor whose find_places reports a cache hit,
// the way executeFindPlaces does.
type cacheNotingTools struct{ fakeToolExecutor }

func (c *cacheNotingTools) execute(ctx context.Context, name string, args json.RawMessage) (string, assistantToolOutcome, error) {
	result, outcome, err := c.fakeToolExecutor.execute(ctx, name, args)
	if name == "find_places" {
		outcome.Cache = "hit"
	}
	return result, outcome, err
}

func TestAssistantBudget_TraceRecordsRoundsAndCalls(t *testing.T) {
	doer := queued(
		toolCallResponse(t, "a", "find_places", `{"query":"Cairns"}`, openRouterUsage{PromptTokens: 100, CompletionTokens: 10, Cost: 0.01, PromptTokensDetails: openRouterPromptTokensDetails{CachedTokens: 60}}),
		finalResponse(t, "Cairns found", "m", usage(120, 20, 0.02)),
	)
	emit, _ := recordingEmitter()
	rec := &assistantRunRecorder{}
	tools := &cacheNotingTools{fakeToolExecutor{results: map[string]string{"find_places": `{"results":[]}`}}}

	runner := &assistantRunner{doer: doer, apiKey: "key", model: "m", tools: tools, emit: emit}
	if _, err := runner.run(withAssistantRunRecorder(context.Background(), rec), "system", "", nil); err != nil {
		t.Fatalf("run: %v", err)
	}
	rounds, stop := rec.result()
	if stop != assistantStopAnswered || len(rounds) != 2 {
		t.Fatalf("stop %q with %d rounds, want answered with 2", stop, len(rounds))
	}
	first := rounds[0]
	if first.PromptTokens != 100 || first.CompletionTokens != 10 || first.CachedTokens != 60 || first.CostUSD != 0.01 || !first.Progress || first.ForcedFinal {
		t.Fatalf("first round = %+v", first)
	}
	if len(first.Calls) != 1 {
		t.Fatalf("first round calls = %+v", first.Calls)
	}
	call := first.Calls[0]
	if call.Name != "find_places" || call.Args != `{"query":"Cairns"}` || call.ResultBytes != len(`{"results":[]}`) || call.Cache != "hit" || call.Error != "" {
		t.Fatalf("call = %+v", call)
	}
	if rounds[1].StartOffsetMs < first.StartOffsetMs {
		t.Fatalf("round offsets should not go backwards: %+v", rounds)
	}
}

func TestAssistantBudget_VerboseLogIsOnlyWrittenWhenAsked(t *testing.T) {
	run := func(verbose bool) string {
		var buf strings.Builder
		orig := log.Writer()
		log.SetOutput(&buf)
		defer log.SetOutput(orig)
		doer := queued(newCallRound(t, 0), finalResponse(t, "ok", "m", openRouterUsage{}))
		emit, _ := recordingEmitter()
		runner := &assistantRunner{doer: doer, apiKey: "key", model: "m", tools: &fakeToolExecutor{}, emit: emit, options: assistantRunnerOptions{VerboseRunLog: verbose}}
		ctx := withAssistantConversationID(context.Background(), "conv-1")
		if _, err := runner.run(ctx, "system", "", nil); err != nil {
			t.Fatalf("run: %v", err)
		}
		return buf.String()
	}

	off := run(false)
	if strings.Contains(off, "round 0") || strings.Contains(off, "stopped (") {
		t.Fatalf("quiet run logged round detail: %q", off)
	}
	on := run(true)
	for _, want := range []string{"run conv-1 round 0 (", "1 tool calls", "progress", "stalled 0/2", "tool phase left", "run conv-1 round 1 (", "answer", "stopped (answered) after 1 tool rounds"} {
		if !strings.Contains(on, want) {
			t.Fatalf("verbose log is missing %q:\n%s", want, on)
		}
	}
}

func TestAssistantBudget_AFailedRunAlwaysLogsItsStopReason(t *testing.T) {
	var buf strings.Builder
	orig := log.Writer()
	log.SetOutput(&buf)
	defer log.SetOutput(orig)

	doer := &queuedChatDoer{responses: []*http.Response{nil}, errs: []error{errors.New("upstream is down")}}
	emit, _ := recordingEmitter()
	runner := &assistantRunner{doer: doer, apiKey: "key", model: "m", tools: &fakeToolExecutor{}, emit: emit}
	if _, err := runner.run(withAssistantConversationID(context.Background(), "conv-2"), "system", "", nil); err == nil {
		t.Fatal("expected an error")
	}
	if !strings.Contains(buf.String(), "run conv-2 stopped (error) after 0 tool rounds in") {
		t.Fatalf("a failed run must log its stop reason whatever the setting: %q", buf.String())
	}
}

// streamingAnswerDoer answers with a stream that sends the first part of a
// natural answer at once and the rest only after a delay, as a slow provider
// does. The stream ends early if the request's context does.
type streamingAnswerDoer struct {
	first, rest string
	delay       time.Duration

	mu       sync.Mutex
	requests int
}

func (d *streamingAnswerDoer) Do(req *http.Request) (*http.Response, error) {
	d.mu.Lock()
	d.requests++
	d.mu.Unlock()
	pr, pw := io.Pipe()
	chunk := func(content string) string {
		data, _ := json.Marshal(map[string]any{"model": "m", "choices": []map[string]any{{"index": 0, "delta": map[string]any{"role": "assistant", "content": content}, "finish_reason": ""}}})
		return "data: " + string(data) + "\n\n"
	}
	go func() {
		_, _ = io.WriteString(pw, chunk(d.first))
		select {
		case <-time.After(d.delay):
		case <-req.Context().Done():
			pw.CloseWithError(req.Context().Err())
			return
		}
		_, _ = io.WriteString(pw, chunk(d.rest))
		_, _ = io.WriteString(pw, "data: {\"choices\":[{\"index\":0,\"delta\":{},\"finish_reason\":\"stop\"}],\"usage\":{\"prompt_tokens\":10,\"completion_tokens\":5}}\n\ndata: [DONE]\n\n")
		pw.Close()
	}()
	return &http.Response{StatusCode: http.StatusOK, Body: pr, Header: make(http.Header)}, nil
}

func TestAssistantBudget_AnswerAlreadyStreamingWhenTheToolPhaseEndsIsLetFinish(t *testing.T) {
	shortBudget(t, 80*time.Millisecond, 5*time.Second)
	doer := &streamingAnswerDoer{first: "The passage looks fine, ", rest: "leave at first light.", delay: 250 * time.Millisecond}
	emit, events := recordingEmitter()
	rec := &assistantRunRecorder{}
	runner := &assistantRunner{doer: doer, apiKey: "key", model: "m", tools: &fakeToolExecutor{}, emit: emit}

	reply, err := runner.run(withAssistantRunRecorder(context.Background(), rec), "system", "", nil)
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if reply.Content != "The passage looks fine, leave at first light." {
		t.Fatalf("content = %q, want the whole natural answer", reply.Content)
	}
	if doer.requests != 1 {
		t.Fatalf("requests = %d: an answer already streaming must not be cut off and redone", doer.requests)
	}
	for _, ev := range *events {
		if ev.event == "retract" {
			t.Fatal("the streamed text was withdrawn")
		}
	}
	if _, stop := rec.result(); stop != assistantStopAnswered {
		t.Fatalf("stop reason = %q, want %q", stop, assistantStopAnswered)
	}
}

func TestAssistantBudget_AnswerStillSilentWhenTheToolPhaseEndsIsRedoneAsTheFinalAnswer(t *testing.T) {
	shortBudget(t, 60*time.Millisecond, 5*time.Second)
	calls := 0
	doer := budgetDoerFunc(func(req *http.Request) (*http.Response, error) {
		calls++
		if calls == 1 {
			// Nothing has streamed when the tool phase ends.
			<-req.Context().Done()
			return nil, req.Context().Err()
		}
		return finalResponse(t, "wrapped up", "m", openRouterUsage{}), nil
	})
	emit, _ := recordingEmitter()
	rec := &assistantRunRecorder{}
	runner := &assistantRunner{doer: doer, apiKey: "key", model: "m", tools: &fakeToolExecutor{}, emit: emit}
	reply, err := runner.run(withAssistantRunRecorder(context.Background(), rec), "system", "", nil)
	if err != nil || reply.Content != "wrapped up" || calls != 2 {
		t.Fatalf("content %q, %d requests, err %v", reply.Content, calls, err)
	}
	if _, stop := rec.result(); stop != assistantStopTimeCeiling {
		t.Fatalf("stop reason = %q, want %q", stop, assistantStopTimeCeiling)
	}
}

func TestAssistantForcedFinalToolLog_NamesWhyTheLookupsStopped(t *testing.T) {
	log := []assistantForcedFinalToolLogEntry{{Name: "get_tides", Args: "{}", Result: "{}"}}
	for _, tc := range []struct{ forced, want string }{
		{assistantStopStalled, "before further lookups stopped turning up anything new:"},
		{assistantStopRoundCeiling, "before reaching the limit on lookups:"},
		{assistantStopTimeCeiling, "before the time for lookups ran out:"},
	} {
		got := assistantForcedFinalToolLogText(log, tc.forced)
		if !strings.HasPrefix(got, "What you already looked up this turn, "+tc.want) {
			t.Errorf("%s: got %q", tc.forced, got)
		}
		if strings.Contains(got, "running out of tool calls") {
			t.Errorf("%s: the wording must not claim tool calls ran out: %q", tc.forced, got)
		}
	}
}
