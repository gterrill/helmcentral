package main

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
)

func conversationToolDeps(s *assistantStore) assistantToolDeps {
	return assistantToolDeps{conversations: func() *assistantStore { return s }}
}

func TestSearchConversationsTool_ReturnsExcerptsAndExcludesCurrent(t *testing.T) {
	s := newTestAssistantStore(t)
	past := seedSearchConv(t, s, "Reverso polisher",
		[2]string{"user", "Where do I take a fuel sample from the Reverso?"},
		[2]string{"assistant", "From the photos the discharge manifold has no sampling valve or drain point."})
	current := seedSearchConv(t, s, "Today", [2]string{"user", "reverso again"})

	ctx := withAssistantConversationID(context.Background(), current.ID)
	out, err := conversationToolDeps(s).execute(ctx, "search_conversations", json.RawMessage(`{"query":"reverso"}`))
	if err != nil {
		t.Fatal(err)
	}
	var res assistantSearchConversationsResult
	if err := json.Unmarshal([]byte(out), &res); err != nil {
		t.Fatal(err)
	}
	if len(res.Results) != 1 || res.Results[0].ID != past.ID {
		t.Fatalf("results = %+v", res.Results)
	}
	if len(res.Results[0].Excerpts) == 0 || res.Results[0].Date == "" {
		t.Fatalf("expected excerpts and date: %+v", res.Results[0])
	}
}

func TestSearchConversationsTool_FindsWordInLaterMessage(t *testing.T) {
	s := newTestAssistantStore(t)
	past := seedSearchConv(t, s, "Fuel",
		[2]string{"user", "hello"},
		[2]string{"assistant", "the manifold has no sampling valve"})
	out, err := conversationToolDeps(s).execute(context.Background(), "search_conversations", json.RawMessage(`{"query":"sampling valve"}`))
	if err != nil || !strings.Contains(out, past.ID) {
		t.Fatalf("out=%s err=%v", out, err)
	}
}

func TestSearchConversationsTool_Errors(t *testing.T) {
	s := newTestAssistantStore(t)
	if _, err := conversationToolDeps(s).execute(context.Background(), "search_conversations", json.RawMessage(`{"query":" "}`)); err == nil {
		t.Fatal("expected empty-query error")
	}
	if _, err := (assistantToolDeps{}).execute(context.Background(), "search_conversations", json.RawMessage(`{"query":"x"}`)); err == nil {
		t.Fatal("expected unavailable error")
	}
}

func TestReadConversationTool_ReturnsMessagesAndAttachments(t *testing.T) {
	s := newTestAssistantStore(t)
	c, _ := s.CreateConversation("Racor")
	s.AppendMessage(assistantMessage{ConversationID: c.ID, Role: "user", Content: "photo of the bowl",
		Attachments: []assistantAttachment{{DocumentID: "d1", Filename: "racor.jpg"}}})
	s.AppendMessage(assistantMessage{ConversationID: c.ID, Role: "watch", Content: "watch report"})
	s.AppendMessage(assistantMessage{ConversationID: c.ID, Role: "assistant", Content: strings.Repeat("x", 3000)})

	out, err := conversationToolDeps(s).execute(context.Background(), "read_conversation", json.RawMessage(`{"id":"`+c.ID+`"}`))
	if err != nil {
		t.Fatal(err)
	}
	var res assistantReadConversationResult
	if err := json.Unmarshal([]byte(out), &res); err != nil {
		t.Fatal(err)
	}
	if len(res.Messages) != 2 || res.Messages[0].Attachments[0] != "racor.jpg" {
		t.Fatalf("messages = %+v", res.Messages)
	}
	if !strings.Contains(res.Messages[1].Content, "[message truncated]") {
		t.Fatal("expected long message truncated")
	}
}

func TestReadConversationTool_RefusesCurrentAndUnknown(t *testing.T) {
	s := newTestAssistantStore(t)
	c, _ := s.CreateConversation("x")
	ctx := withAssistantConversationID(context.Background(), c.ID)
	if _, err := conversationToolDeps(s).execute(ctx, "read_conversation", json.RawMessage(`{"id":"`+c.ID+`"}`)); err == nil {
		t.Fatal("expected current-conversation error")
	}
	if _, err := conversationToolDeps(s).execute(context.Background(), "read_conversation", json.RawMessage(`{"id":"nope"}`)); err == nil {
		t.Fatal("expected unknown error")
	}
}
