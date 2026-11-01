package main

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"
)

// cardsByArgsExecutor answers a tool call with a result keyed by its
// arguments and hands back the card that result carries, as the real sink
// does.
type cardsByArgsExecutor struct {
	fakeToolExecutor
	byArgs map[string]string
	cards  map[string]mateCard
}

func (c *cardsByArgsExecutor) execute(ctx context.Context, name string, args json.RawMessage) (string, assistantToolOutcome, error) {
	return c.byArgs[string(args)], assistantToolOutcome{}, nil
}

func (c *cardsByArgsExecutor) takeCards(result string) []mateCard {
	if card, ok := c.cards[result]; ok {
		return []mateCard{card}
	}
	return nil
}

func anchorageRunCard(tag string, lat, lon float64) mateCard {
	p, _ := json.Marshal(map[string]any{"tag": tag, "lat": lat, "lon": lon})
	return mateCard{Kind: anchorageCardKind, Payload: p, lat: lat, lon: lon, positioned: true}
}

func toolCallsResponse(t *testing.T, calls ...string) *http.Response {
	t.Helper()
	var tcs []openRouterToolCall
	for i, a := range calls {
		tcs = append(tcs, openRouterToolCall{ID: "call_" + string(rune('a'+i)), Type: "function",
			Function: openRouterToolCallFunction{Name: "anchorage_details", Arguments: openRouterArguments(a)}})
	}
	return chatResponse(t, http.StatusOK, openRouterChatResponse{
		Model:   "m",
		Choices: []openRouterChoice{{Message: openRouterMessage{Role: "assistant", ToolCalls: tcs}}},
		Usage:   usage(1, 1, 0),
	})
}

func runAnchorageRounds(t *testing.T, ex *cardsByArgsExecutor, rounds ...[]string) assistantReply {
	t.Helper()
	var resps []*http.Response
	var errs []error
	for _, r := range rounds {
		resps = append(resps, toolCallsResponse(t, r...))
		errs = append(errs, nil)
	}
	resps = append(resps, finalResponse(t, "done", "m", usage(1, 1, 0)))
	errs = append(errs, nil)
	emit, _ := recordingEmitter()
	runner := &assistantRunner{doer: &queuedChatDoer{responses: resps, errs: errs}, apiKey: "k", model: "m", tools: ex, emit: emit}
	reply, err := runner.run(context.Background(), "system", "", nil)
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	return reply
}

func cardTags(t *testing.T, cards []mateCard) []string {
	t.Helper()
	var tags []string
	for _, c := range cards {
		var p struct {
			Tag string `json:"tag"`
		}
		_ = json.Unmarshal(c.Payload, &p)
		tags = append(tags, c.Kind+":"+p.Tag)
	}
	return tags
}

func TestRun_AnchorageCardCorrectedInLaterRoundReplacesEarlier(t *testing.T) {
	ex := &cardsByArgsExecutor{
		byArgs: map[string]string{`{"a":1}`: "guess", `{"a":2}`: "corrected", `{"m":1}`: "marina"},
		cards: map[string]mateCard{
			"guess":     anchorageRunCard("guess", -20.5446, 149.118933),
			"corrected": anchorageRunCard("corrected", -20.544600001, 149.118933),
			"marina":    {Kind: marinaCardKind, Payload: json.RawMessage(`{"tag":"m"}`)},
		},
	}
	reply := runAnchorageRounds(t, ex, []string{`{"a":1}`, `{"m":1}`}, []string{`{"a":2}`})
	got := cardTags(t, reply.Cards)
	if len(got) != 2 || got[0] != marinaCardKind+":m" || got[1] != anchorageCardKind+":corrected" {
		t.Errorf("cards = %v, want marina then the round-2 anchorage card", got)
	}
}

func TestRun_AnchorageCardsAtTwoPositionsBothStay(t *testing.T) {
	ex := &cardsByArgsExecutor{
		byArgs: map[string]string{`{"a":1}`: "one", `{"a":2}`: "two"},
		cards: map[string]mateCard{
			"one": anchorageRunCard("one", -20.5446, 149.118933),
			"two": anchorageRunCard("two", -20.6, 149.2),
		},
	}
	reply := runAnchorageRounds(t, ex, []string{`{"a":1}`}, []string{`{"a":2}`})
	if got := cardTags(t, reply.Cards); len(got) != 2 {
		t.Errorf("cards = %v, want two", got)
	}
}

func TestRun_AnchorageCardsInOneRoundLaterCallWins(t *testing.T) {
	ex := &cardsByArgsExecutor{
		byArgs: map[string]string{`{"a":1}`: "first", `{"a":2}`: "second"},
		cards: map[string]mateCard{
			"first":  anchorageRunCard("first", -20.5446, 149.118933),
			"second": anchorageRunCard("second", -20.5446, 149.118933),
		},
	}
	reply := runAnchorageRounds(t, ex, []string{`{"a":1}`, `{"a":2}`})
	if got := cardTags(t, reply.Cards); len(got) != 1 || got[0] != anchorageCardKind+":second" {
		t.Errorf("cards = %v, want only the later call's", got)
	}
}

func TestAppendReplyCards_TenMetreThreshold(t *testing.T) {
	base := anchorageRunCard("a", -20.5, 149.1)
	near := anchorageRunCard("b", -20.5+0.00008, 149.1) // about 9 m
	far := anchorageRunCard("c", -20.5+0.00015, 149.1)  // about 17 m
	if got := appendReplyCards([]mateCard{base}, near); len(got) != 1 {
		t.Errorf("9 m apart: %d cards, want 1", len(got))
	}
	if got := appendReplyCards([]mateCard{base}, far); len(got) != 2 {
		t.Errorf("17 m apart: %d cards, want 2", len(got))
	}
}
