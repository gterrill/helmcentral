package main

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"testing"
	"time"
)

// test_card is a kind only the tests know; no real kind is on the wire yet.
const testCardKind = "test_card"

func testCardCollect(result string, reply *assistantReply) error {
	reply.Cards = append(reply.Cards, mateCard{Kind: testCardKind, Payload: json.RawMessage(result)})
	return nil
}

func TestAssistantCards_RideWithTheMessageInOrder(t *testing.T) {
	asst := newTestAssistantStore(t)
	conv, err := asst.CreateConversation("c")
	if err != nil {
		t.Fatal(err)
	}
	cards := []mateCard{
		{Kind: testCardKind, Payload: json.RawMessage(`{"n":2}`)},
		{Kind: testCardKind, Payload: json.RawMessage(`{"n":1}`)},
	}
	if _, err := asst.AppendMessage(assistantMessage{ConversationID: conv.ID, Role: "user", Content: "hi", Cards: cards}); err == nil {
		t.Fatal("only an assistant message carries cards")
	}
	if msgs, _ := asst.ListMessages(conv.ID); len(msgs) != 0 {
		t.Fatalf("a refused message saves nothing, got %+v", msgs)
	}
	if _, err := asst.AppendMessage(assistantMessage{ConversationID: conv.ID, Role: "assistant", Content: "Done.", Cards: cards}); err != nil {
		t.Fatalf("AppendMessage: %v", err)
	}
	if _, err := asst.AppendMessage(assistantMessage{ConversationID: conv.ID, Role: "assistant", Content: "Plain."}); err != nil {
		t.Fatalf("AppendMessage: %v", err)
	}
	msgs, err := asst.ListMessages(conv.ID)
	if err != nil || len(msgs) != 2 {
		t.Fatalf("ListMessages: %v %+v", err, msgs)
	}
	got := msgs[0].Cards
	if len(got) != 2 || got[0].Kind != testCardKind || string(got[0].Payload) != `{"n":2}` || string(got[1].Payload) != `{"n":1}` {
		t.Fatalf("expected both cards in order, got %+v", got)
	}
	if len(msgs[1].Cards) != 0 {
		t.Fatalf("a message with no cards has none, got %+v", msgs[1].Cards)
	}
	raw, _ := json.Marshal(msgs[1])
	if json.Valid(raw) && containsKey(raw, "cards") {
		t.Fatalf("empty cards are omitted from the wire: %s", raw)
	}
	if err := asst.DeleteConversation(conv.ID); err != nil {
		t.Fatal(err)
	}
	var n int
	if err := asst.db.QueryRow(`SELECT COUNT(*) FROM assistant_message_cards`).Scan(&n); err != nil || n != 0 {
		t.Fatalf("deleting the conversation takes its cards, got %d %v", n, err)
	}
}

func containsKey(raw []byte, key string) bool {
	var m map[string]json.RawMessage
	_ = json.Unmarshal(raw, &m)
	_, ok := m[key]
	return ok
}

func TestAssistantRunner_CollectsCardsFromTheToolResult(t *testing.T) {
	const toolName = "propose_changes" // any registered tool name; collect is swapped below
	orig := mateTools
	t.Cleanup(func() { mateTools = orig })
	swapped := make([]mateTool, len(orig))
	copy(swapped, orig)
	for i := range swapped {
		if swapped[i].name == toolName {
			swapped[i].collect = testCardCollect
		}
	}
	mateTools = swapped

	doer := &queuedChatDoer{responses: []*http.Response{proposalToolCallRound(t), finalResponse(t, "Here.", "m", usage(10, 5, 0))}, errs: []error{nil, nil}}
	tools := &fakeToolExecutor{results: map[string]string{toolName: `{"n":1}`}}
	emit, _ := recordingEmitter()
	runner := &assistantRunner{doer: doer, apiKey: "k", model: "m", tools: tools, emit: emit}

	reply, err := runner.run(context.Background(), "system", "", nil)
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if len(reply.Cards) != 1 || reply.Cards[0].Kind != testCardKind || string(reply.Cards[0].Payload) != `{"n":1}` {
		t.Fatalf("expected the run to carry the card, got %+v", reply.Cards)
	}
}

func TestPostAssistantMessageHandler_PersistsCardsWithTheReplyAndAFailedRunSavesNone(t *testing.T) {
	env := newProposalEnv(t)
	secrets := withTestSecretsStore(t)
	if err := secrets.Set("OPENROUTER_API_KEY", "sk-test"); err != nil {
		t.Fatalf("Set: %v", err)
	}
	t.Setenv("SETTINGS_FILE", writeAssistantSettingsFixture(t, true, "openai/gpt-4o"))
	cardCount := func() int {
		var n int
		if err := env.asst.db.QueryRow(`SELECT COUNT(*) FROM assistant_message_cards`).Scan(&n); err != nil {
			t.Fatal(err)
		}
		return n
	}
	post := func() string {
		c, rec := newAssistantEchoContext(http.MethodPost, "/api/assistant/conversations/"+env.conv.ID+"/messages", `{"today":"2026-09-30","content":"show me"}`, env.conv.ID)
		if err := postAssistantMessageHandler(c); err != nil {
			t.Fatalf("handler: %v", err)
		}
		return rec.Body.String()
	}

	swapAssistantRunner(t, func(apiKey, model string, tools assistantToolDeps, autoRouter assistantAutoRouterOptions, emit assistantEmitter) assistantRunnerFace {
		return &fakeAssistantRunner{emit: emit, err: errors.New("stopped")}
	})
	post()
	if n := cardCount(); n != 0 {
		t.Fatalf("a failed run saves no card, got %d", n)
	}

	swapAssistantRunner(t, func(apiKey, model string, tools assistantToolDeps, autoRouter assistantAutoRouterOptions, emit assistantEmitter) assistantRunnerFace {
		return &fakeAssistantRunner{emit: emit, reply: assistantReply{Content: "Here.", Model: "openai/gpt-4o", Cards: []mateCard{{Kind: testCardKind, Payload: json.RawMessage(`{"n":1}`)}}}}
	})
	body := post()
	var frame struct {
		Message assistantMessage `json:"message"`
	}
	if err := json.Unmarshal([]byte(extractSSEEventData(t, body, "message")), &frame); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(frame.Message.Cards) != 1 || frame.Message.Cards[0].Kind != testCardKind {
		t.Fatalf("expected the SSE message to carry the card, got %+v", frame.Message)
	}
	msgs, err := env.asst.ListMessages(env.conv.ID)
	if err != nil || len(msgs) == 0 || len(msgs[len(msgs)-1].Cards) != 1 {
		t.Fatalf("expected the card stored on the reply, got %v %+v", err, msgs)
	}
}

// ── recorded passage decisions (ADR 0211) ────────────────────────────────

// pdCreated is when the test tool "built" its cards, well before the store saves them.
var pdCreated = time.Date(2026, 10, 20, 1, 2, 3, 456_000_000, time.UTC)

func pdRoutes(legs int) [][]passageRoutePoint {
	var out [][]passageRoutePoint
	for i := 0; i < legs; i++ {
		out = append(out, []passageRoutePoint{{Lat: -19 + float64(i), Lon: 147, Name: "A"}, {Lat: -18 + float64(i), Lon: 147, Name: "B"}})
	}
	return out
}

func pdPayload(legs int, dates ...string) json.RawMessage {
	p := passageWindowPayload{Timezone: "Australia/Brisbane"}
	for i := 0; i < legs; i++ {
		p.Legs = append(p.Legs, passageWindowLeg{Name: "Leg"})
		_ = i
	}
	for _, d := range dates {
		row := passageWindowRow{DepartDate: d, Verdict: "go"}
		for i := 0; i < legs; i++ {
			row.Cells = append(row.Cells, passageWindowCell{Leg: i, Date: d, Depart: "06:00", Arrive: "16:00", State: "go", NotChecked: []string{}})
		}
		p.Rows = append(p.Rows, row)
	}
	raw, _ := json.Marshal(p)
	return raw
}

func pdCard(legs int, routes [][]passageRoutePoint, dates ...string) mateCard {
	return mateCard{Kind: "passage_window", Payload: pdPayload(legs, dates...), routes: routes, created: pdCreated}
}

func appendCards(t *testing.T, asst *assistantStore, convID string, cards ...mateCard) (assistantMessage, error) {
	t.Helper()
	return asst.AppendMessage(assistantMessage{ConversationID: convID, Role: "assistant", Content: "x", Cards: cards})
}

func TestAssistantCards_PassageWindowGetsIDAndABlankDecision(t *testing.T) {
	asst := newTestAssistantStore(t)
	conv, _ := asst.CreateConversation("c")
	msg, err := appendCards(t, asst, conv.ID,
		mateCard{Kind: testCardKind, Payload: json.RawMessage(`{}`)},
		pdCard(2, pdRoutes(2), "2026-10-23", "2026-10-24"),
		pdCard(1, pdRoutes(1), "2026-10-25"))
	if err != nil {
		t.Fatalf("AppendMessage: %v", err)
	}
	if msg.Cards[0].ID == 0 || msg.Cards[1].ID == 0 || msg.Cards[2].ID == 0 || msg.Cards[1].ID == msg.Cards[2].ID {
		t.Fatalf("every card gets its row id: %+v", msg.Cards)
	}
	if msg.Cards[0].Decision != nil {
		t.Fatalf("only a passage_window card has a decision: %+v", msg.Cards[0])
	}
	d := msg.Cards[1].Decision
	if d == nil || d.ID == 0 || d.State != "" || d.RowDate != nil || d.Inferred || d.PassageID != nil {
		t.Fatalf("blank decision expected, got %+v", d)
	}
	var n int
	if err := asst.db.QueryRow(`SELECT COUNT(*) FROM passage_decisions`).Scan(&n); err != nil || n != 2 {
		t.Fatalf("one decision row per passage_window card, got %d %v", n, err)
	}
	var payload, routes, convID, msgID string
	var created int64
	if err := asst.db.QueryRow(`SELECT payload_json, routes_json, conversation_id, message_id, card_created_ms FROM passage_decisions WHERE card_id = ?`, msg.Cards[1].ID).
		Scan(&payload, &routes, &convID, &msgID, &created); err != nil {
		t.Fatal(err)
	}
	if payload != string(msg.Cards[1].Payload) || convID != conv.ID || msgID != msg.ID || created != pdCreated.UnixMilli() {
		t.Fatalf("snapshot is the verbatim payload with its owners: %s %s %s %d", payload, convID, msgID, created)
	}
	var gotRoutes [][]passageRoutePoint
	if err := json.Unmarshal([]byte(routes), &gotRoutes); err != nil || len(gotRoutes) != 2 || gotRoutes[1][0].Lat != -18 || gotRoutes[0][0].Name != "A" {
		t.Fatalf("routes_json = %s (%v)", routes, err)
	}
}

func TestAssistantCards_PassageWindowWithoutMatchingRoutesIsRefusedWhole(t *testing.T) {
	asst := newTestAssistantStore(t)
	conv, _ := asst.CreateConversation("c")
	for name, c := range map[string]mateCard{
		"missing":         pdCard(2, nil, "2026-10-23"),
		"too few":         pdCard(2, pdRoutes(1), "2026-10-23"),
		"too many":        pdCard(1, pdRoutes(2), "2026-10-23"),
		"short leg":       pdCard(1, [][]passageRoutePoint{{{Lat: 1, Lon: 1}}}, "2026-10-23"),
		"no created time": {Kind: "passage_window", Payload: pdPayload(1, "2026-10-23"), routes: pdRoutes(1)},
		"bad payload":     {Kind: "passage_window", Payload: json.RawMessage(`{"legs":"x"}`), routes: pdRoutes(1)},
	} {
		if _, err := appendCards(t, asst, conv.ID, c); err == nil {
			t.Errorf("%s: want a refusal", name)
		}
	}
	if msgs, _ := asst.ListMessages(conv.ID); len(msgs) != 0 {
		t.Fatalf("a refused message saves nothing, got %d", len(msgs))
	}
	var n int
	if err := asst.db.QueryRow(`SELECT COUNT(*) FROM passage_decisions`).Scan(&n); err != nil || n != 0 {
		t.Fatalf("no decision rows left behind, got %d %v", n, err)
	}
}

func TestAssistantCards_DecisionOutlivesItsConversationAndListJoinsIt(t *testing.T) {
	asst := newTestAssistantStore(t)
	conv, _ := asst.CreateConversation("c")
	msg, err := appendCards(t, asst, conv.ID, pdCard(1, pdRoutes(1), "2026-10-23"))
	if err != nil {
		t.Fatal(err)
	}
	id := msg.Cards[0].Decision.ID
	if _, err := asst.db.Exec(`UPDATE passage_decisions SET state='went', row_date='2026-10-23', source='inferred', passage_id='p1' WHERE id=?`, id); err != nil {
		t.Fatal(err)
	}
	msgs, _ := asst.ListMessages(conv.ID)
	d := msgs[0].Cards[0].Decision
	if msgs[0].Cards[0].ID != msg.Cards[0].ID || d == nil || d.ID != id || d.State != "went" ||
		d.RowDate == nil || *d.RowDate != "2026-10-23" || !d.Inferred || d.PassageID == nil || *d.PassageID != "p1" {
		t.Fatalf("listed card must carry id and joined decision, got %+v", msgs[0].Cards[0])
	}
	raw, _ := json.Marshal(msgs[0].Cards[0])
	var wire map[string]json.RawMessage
	_ = json.Unmarshal(raw, &wire)
	if _, ok := wire["routes"]; ok {
		t.Fatalf("routes stay server side: %s", raw)
	}
	if err := asst.DeleteConversation(conv.ID); err != nil {
		t.Fatal(err)
	}
	var n int
	if err := asst.db.QueryRow(`SELECT COUNT(*) FROM passage_decisions`).Scan(&n); err != nil || n != 1 {
		t.Fatalf("the decision outlives the chat, got %d %v", n, err)
	}
}

func TestPassageDecisionsSchema_RefusesInconsistentRows(t *testing.T) {
	asst := newTestAssistantStore(t)
	cases := []struct {
		name    string
		state   string
		rowDate any
		source  string
	}{
		{"went without a date", "went", nil, "operator"},
		{"date without went", "stayed", "2026-10-23", "operator"},
		{"state without source", "stayed", nil, ""},
		{"unknown state", "maybe", nil, "operator"},
		{"unknown source", "", nil, "guess"},
	}
	for _, c := range cases {
		_, err := asst.db.Exec(`INSERT INTO passage_decisions (card_id, message_id, conversation_id, card_created_ms, payload_json, routes_json, updated_ms, state, row_date, source)
			VALUES (1, 'm', 'c', 0, '{}', '[]', 0, ?, ?, ?)`, c.state, c.rowDate, c.source)
		if err == nil {
			t.Errorf("%s: the CHECK should refuse it", c.name)
		}
	}
}
