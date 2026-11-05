package main

import "testing"

// A round records each call with its result and error. A call the round marked
// failed, or never answered, did not succeed; one that returned nothing and
// raised no error did.
func TestRunRecord_AddRoundKeepsWhatEachCallDid(t *testing.T) {
	mk := func(name string) openRouterToolCall {
		var c openRouterToolCall
		c.Function.Name = name
		return c
	}
	calls := []openRouterToolCall{mk("a"), mk("b"), mk("c"), mk("d")}
	msgs := []openRouterMessage{
		{Role: "tool", Content: openRouterContent("ra")},
		{Role: "tool", Content: openRouterContent(`{"error":"boom"}`)},
		{Role: "tool", Content: openRouterContent("")},
	}
	done := []assistantToolCall{{Name: "a"}, {Name: "b", Error: "boom"}, {Name: "c"}}
	var run mateRunRecord
	run.addRound(calls, msgs, done)

	if len(run) != 4 {
		t.Fatalf("recorded %d calls, want 4", len(run))
	}
	if got := run.succeeded("a"); len(got) != 1 || got[0].result != "ra" {
		t.Errorf("a: %+v", got)
	}
	if got := run.named("b"); len(got) != 1 || got[0].ok() || got[0].err != "boom" {
		t.Errorf("an errored call must keep its error: %+v", got)
	}
	if got := run.succeeded("c"); len(got) != 1 {
		t.Errorf("an empty result with no error is a success: %+v", got)
	}
	if got := run.succeeded("d"); len(got) != 0 {
		t.Errorf("a call with no message must not count as succeeded: %+v", got)
	}
	if !run.producedSince(0) || !run.producedSince(2) || run.producedSince(3) {
		t.Errorf("producedSince: 0=%v 2=%v 3=%v", run.producedSince(0), run.producedSince(2), run.producedSince(3))
	}
}
