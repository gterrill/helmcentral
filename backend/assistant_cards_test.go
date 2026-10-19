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

	swapAssistantRunner(t, func(apiKey, model, settingsPath string, autoRouter assistantAutoRouterOptions, webSearch bool, today time.Time, emit assistantEmitter) assistantRunnerFace {
		return &fakeAssistantRunner{emit: emit, err: errors.New("stopped")}
	})
	post()
	if n := cardCount(); n != 0 {
		t.Fatalf("a failed run saves no card, got %d", n)
	}

	swapAssistantRunner(t, func(apiKey, model, settingsPath string, autoRouter assistantAutoRouterOptions, webSearch bool, today time.Time, emit assistantEmitter) assistantRunnerFace {
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
