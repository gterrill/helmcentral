package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
)

// search_conversations and read_conversation (ADR 0161): Mate's recall of
// earlier chats. What an operator established with photos and observations
// one day (a fitting that is not there, a procedure ruled out) must still be
// known the next, in a new conversation.

type assistantSearchConversationsArgs struct {
	Query string `json:"query"`
	Limit int    `json:"limit"`
}

type assistantConversationToolHit struct {
	ID       string                         `json:"id"`
	Title    string                         `json:"title"`
	Date     string                         `json:"date"`
	Excerpts []assistantConversationExcerpt `json:"excerpts,omitempty"`
}

type assistantSearchConversationsResult struct {
	Results []assistantConversationToolHit `json:"results"`
}

func (d assistantToolDeps) conversationStore(toolName string) (*assistantStore, error) {
	if d.conversations == nil {
		return nil, fmt.Errorf("%s: the conversation history is not available", toolName)
	}
	store := d.conversations()
	if store == nil {
		return nil, fmt.Errorf("%s: the conversation history is not available", toolName)
	}
	return store, nil
}

func (d assistantToolDeps) executeSearchConversations(ctx context.Context, raw json.RawMessage) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	store, err := d.conversationStore("search_conversations")
	if err != nil {
		return "", err
	}
	var args assistantSearchConversationsArgs
	if err := json.Unmarshal(raw, &args); err != nil {
		return "", fmt.Errorf("parse search_conversations arguments: %w", err)
	}
	if _, ok := ftsMatchQuery(args.Query); !ok {
		return "", fmt.Errorf("search_conversations: query must not be empty")
	}
	limit := args.Limit
	if limit <= 0 {
		limit = 5
	}
	if limit > 10 {
		limit = 10
	}

	hits, err := store.SearchConversations(args.Query, assistantConversationSearchOptions{
		Limit:           limit,
		ExcerptsPerConv: 3,
		ExcerptTokens:   48,
		ExcerptMaxRunes: 500,
		ExcludeConvID:   assistantConversationIDFrom(ctx),
	})
	if err != nil {
		return "", fmt.Errorf("search_conversations: %w", err)
	}

	result := assistantSearchConversationsResult{Results: make([]assistantConversationToolHit, 0, len(hits))}
	for _, h := range hits {
		result.Results = append(result.Results, assistantConversationToolHit{
			ID: h.ID, Title: h.Title, Date: h.UpdatedAt.Format("2006-01-02"), Excerpts: h.Excerpts,
		})
	}
	shrink := func() bool {
		if len(result.Results) == 0 {
			return false
		}
		result.Results = result.Results[:len(result.Results)-1]
		return true
	}
	return capToolResultJSON(&result, shrink)
}

type assistantReadConversationArgs struct {
	ID string `json:"id"`
}

type assistantConversationToolMessage struct {
	Role        string   `json:"role"`
	Date        string   `json:"date"`
	Content     string   `json:"content"`
	Attachments []string `json:"attachments,omitempty"`
}

type assistantReadConversationResult struct {
	ID        string                             `json:"id"`
	Title     string                             `json:"title"`
	Date      string                             `json:"date"`
	Messages  []assistantConversationToolMessage `json:"messages"`
	Truncated bool                               `json:"truncated,omitempty"`
}

const assistantReadConversationMessageRunes = 2000

func (d assistantToolDeps) executeReadConversation(ctx context.Context, raw json.RawMessage) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	store, err := d.conversationStore("read_conversation")
	if err != nil {
		return "", err
	}
	var args assistantReadConversationArgs
	if err := json.Unmarshal(raw, &args); err != nil {
		return "", fmt.Errorf("parse read_conversation arguments: %w", err)
	}
	id := strings.TrimSpace(args.ID)
	if id == "" {
		return "", errors.New("read_conversation: id must not be empty")
	}
	if id == assistantConversationIDFrom(ctx) {
		return "", errors.New("read_conversation: that is the current conversation, which you already have")
	}
	conv, ok, err := store.GetConversation(id)
	if err != nil {
		return "", fmt.Errorf("read_conversation: %w", err)
	}
	if !ok {
		return "", fmt.Errorf("read_conversation: no conversation with id %q", id)
	}
	msgs, err := store.ListMessages(id)
	if err != nil {
		return "", fmt.Errorf("read_conversation: %w", err)
	}

	result := assistantReadConversationResult{
		ID: conv.ID, Title: conv.Title, Date: conv.UpdatedAt.Format("2006-01-02"),
		Messages: []assistantConversationToolMessage{},
	}
	for _, m := range msgs {
		if m.Role != "user" && m.Role != "assistant" {
			continue
		}
		content := m.Content
		if r := []rune(content); len(r) > assistantReadConversationMessageRunes {
			content = string(r[:assistantReadConversationMessageRunes]) + "… [message truncated]"
		}
		tm := assistantConversationToolMessage{Role: m.Role, Date: m.CreatedAt.Format("2006-01-02"), Content: content}
		for _, a := range m.Attachments {
			tm.Attachments = append(tm.Attachments, a.Filename)
		}
		result.Messages = append(result.Messages, tm)
	}
	// Over budget: drop the oldest messages first - the recent end of a
	// chat is where its conclusions are.
	shrink := func() bool {
		if len(result.Messages) <= 1 {
			return false
		}
		result.Messages = result.Messages[1:]
		result.Truncated = true
		return true
	}
	return capToolResultJSON(&result, shrink)
}

var mateToolSearchConversations = mateTool{
	name: "search_conversations",
	function: openRouterFunctionDef{
		Description: "Search the operator's earlier conversations with you - what they asked, what you " +
			"answered, and what was established - by keyword. Returns each matching conversation's id, " +
			"title and date with excerpts of the matching messages, most recent first. The current " +
			"conversation is left out. Use it before advising on a specific piece of the boat's " +
			"equipment, and whenever the operator refers to earlier work.",
		Parameters: json.RawMessage(`{
					"type": "object",
					"properties": {
						"query": {
							"type": "string",
							"description": "Keywords to search for, e.g. \"fuel polisher\" or \"Racor\"."
						},
						"limit": {
							"type": "integer",
							"description": "Maximum number of conversations to return (default 5, maximum 10)."
						}
					},
					"required": ["query"]
				}`),
	},
	execute: plainTool(assistantToolDeps.executeSearchConversations),
	describe: func(args json.RawMessage) string {
		var a assistantSearchConversationsArgs
		query := ""
		if json.Unmarshal(args, &a) == nil {
			query = strings.TrimSpace(a.Query)
		}
		if query == "" {
			return "Searching earlier conversations…"
		}
		return fmt.Sprintf("Searching earlier conversations for %q…", query)
	},
}

var mateToolReadConversation = mateTool{
	name: "read_conversation",
	function: openRouterFunctionDef{
		Description: "Read an earlier conversation's operator and assistant messages in order, with the " +
			"names of any documents attached. Use it on a search_conversations result when its excerpts " +
			"are not enough to tell what was established.",
		Parameters: json.RawMessage(`{
					"type": "object",
					"properties": {
						"id": {
							"type": "string",
							"description": "A conversation id from a search_conversations result."
						}
					},
					"required": ["id"]
				}`),
	},
	execute: plainTool(assistantToolDeps.executeReadConversation),
	describe: func(args json.RawMessage) string {
		return "Reading an earlier conversation…"
	},
}
