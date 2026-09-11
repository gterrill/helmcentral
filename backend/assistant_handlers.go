package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/labstack/echo/v4"
)

// This file serves the onboard assistant's HTTP API (ADR 0093): readiness,
// conversation CRUD, and the SSE message endpoint that drives
// assistant_run.go's agentic loop. Every handler here reads globalAssistantStore
// and globalSecretsStore directly (the same package-level-var pattern as the
// rest of the settings/secrets handlers), so tests swap those in rather than
// constructing handlers with store arguments.

// assistantMaxMessageChars bounds one operator message. 8000 characters is
// generous for a passage-planning question and rules out an accidental
// paste of an entire document landing in the chat-completion request.
const assistantMaxMessageChars = 8000

// assistantTitleMaxRunes bounds a conversation's auto-derived title (the
// first user message, cut at a word boundary) so the conversation list
// panel's truncated row still reads as a sentence rather than a mid-word cut.
const assistantTitleMaxRunes = 60

// assistantReadiness is what GET /api/assistant/status reports, and what
// postAssistantMessageHandler checks before it will start a run. Problem is
// empty exactly when the assistant is ready to answer; it never causes a
// non-2xx response for the status endpoint itself (ADR 0093: the operator
// must always be able to see why the assistant can't run).
type assistantReadiness struct {
	Enabled    bool   `json:"enabled"`
	Configured bool   `json:"configured"`
	Model      string `json:"model"`
	Problem    string `json:"problem,omitempty"`
}

// checkAssistantReadiness reads settings and the OpenRouter key once and
// decides whether the assistant can run. apiKey is returned only when
// Problem is empty - a caller with a non-empty Problem should never see the
// key at all, however unusable a partial reading might otherwise seem. A
// failed settings or secrets-store read is a genuine 500 (a bug or a broken
// disk, not "not configured"), so it is returned as an error rather than
// folded into Problem (AGENTS.md's fallback policy: don't mask an upstream
// failure as a configuration problem).
func checkAssistantReadiness(settingsPath string) (assistantReadiness, string, error) {
	settings, err := readSettings(settingsPath)
	if err != nil {
		return assistantReadiness{}, "", fmt.Errorf("read settings for assistant readiness: %w", err)
	}
	payload := buildSettingsPayload(settings)

	readiness := assistantReadiness{
		Enabled: payload.Assistant.Enabled,
		Model:   payload.Assistant.Model,
	}

	apiKey, ok, err := globalSecretsStore.Get("OPENROUTER_API_KEY")
	if err != nil {
		return assistantReadiness{}, "", fmt.Errorf("read OpenRouter API key: %w", err)
	}
	apiKey = strings.TrimSpace(apiKey)
	readiness.Configured = ok && apiKey != ""

	switch {
	case !readiness.Enabled:
		readiness.Problem = "The assistant is switched off. Enable it in Settings → Assistant."
	case !readiness.Configured:
		readiness.Problem = "No OpenRouter API key is configured. Add one in Settings → Assistant."
	case strings.TrimSpace(readiness.Model) == "":
		readiness.Problem = "No assistant model is configured. Set one in Settings → Assistant."
	}

	if readiness.Problem != "" {
		return readiness, "", nil
	}
	return readiness, apiKey, nil
}

// assistantRunnerFace is what assistant_run.go's *assistantRunner
// implements. postAssistantMessageHandler depends on this interface, not
// the concrete type, so tests can substitute a whole-run fake
// (fakeAssistantRunner, assistant_handlers_test.go) instead of scripting an
// OpenRouter doer for every handler-level test.
type assistantRunnerFace interface {
	run(ctx context.Context, system string, history []openRouterMessage) (assistantReply, error)
}

// assistantRunsInFlight guards against two concurrent runs on the same
// conversation (two browser tabs, or a double-click): a run holds its
// conversation id in this map for its whole duration, and
// postAssistantMessageHandler answers a second request for the same id
// with 409 rather than interleaving two agentic loops writing to the same
// history.
var assistantRunsInFlight sync.Map

func beginAssistantRun(conversationID string) bool {
	_, alreadyRunning := assistantRunsInFlight.LoadOrStore(conversationID, struct{}{})
	return !alreadyRunning
}

func endAssistantRun(conversationID string) {
	assistantRunsInFlight.Delete(conversationID)
}

// assistantConversationTitle derives a conversation's title from its first
// user message: the first assistantTitleMaxRunes runes, trimmed back to the
// last space so the title never ends mid-word. Falling back to a hard cut
// when no space is found (e.g. one very long token) is preferable to
// returning nothing.
func assistantConversationTitle(content string) string {
	runes := []rune(content)
	if len(runes) <= assistantTitleMaxRunes {
		return content
	}
	truncated := runes[:assistantTitleMaxRunes]
	for i := len(truncated) - 1; i > 0; i-- {
		if truncated[i] == ' ' {
			return string(truncated[:i])
		}
	}
	return string(truncated)
}

// assistantSettingsPath resolves settings.yaml the same way every other
// handler in this package does (signalk.go's handlers each inline this
// getEnv call rather than sharing a helper).
func assistantSettingsPath() string {
	return getEnv("SETTINGS_FILE", "../settings.yaml")
}

// GET /api/assistant/status
func assistantStatusHandler(c echo.Context) error {
	readiness, _, err := checkAssistantReadiness(assistantSettingsPath())
	if err != nil {
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": err.Error()})
	}
	return c.JSON(http.StatusOK, readiness)
}

// GET /api/assistant/conversations
func listAssistantConversationsHandler(c echo.Context) error {
	conversations, err := globalAssistantStore.ListConversations()
	if err != nil {
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": err.Error()})
	}
	if conversations == nil {
		conversations = []assistantConversation{}
	}
	return c.JSON(http.StatusOK, map[string]any{"conversations": conversations})
}

// POST /api/assistant/conversations
func createAssistantConversationHandler(c echo.Context) error {
	var body struct {
		Title string `json:"title"`
	}
	if err := c.Bind(&body); err != nil {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "invalid body"})
	}

	title := strings.TrimSpace(body.Title)
	if title == "" {
		title = "New conversation"
	}

	conv, err := globalAssistantStore.CreateConversation(title)
	if err != nil {
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": err.Error()})
	}
	return c.JSON(http.StatusCreated, conv)
}

// GET /api/assistant/conversations/:id
func getAssistantConversationHandler(c echo.Context) error {
	id := c.Param("id")

	conv, ok, err := globalAssistantStore.GetConversation(id)
	if err != nil {
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": err.Error()})
	}
	if !ok {
		return c.JSON(http.StatusNotFound, map[string]string{"error": "conversation not found"})
	}

	messages, err := globalAssistantStore.ListMessages(id)
	if err != nil {
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": err.Error()})
	}
	if messages == nil {
		messages = []assistantMessage{}
	}

	return c.JSON(http.StatusOK, map[string]any{"conversation": conv, "messages": messages})
}

// DELETE /api/assistant/conversations/:id
func deleteAssistantConversationHandler(c echo.Context) error {
	id := c.Param("id")

	if err := globalAssistantStore.DeleteConversation(id); err != nil {
		if errors.Is(err, errAssistantConversationNotFound) {
			return c.JSON(http.StatusNotFound, map[string]string{"error": "conversation not found"})
		}
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": err.Error()})
	}
	return c.NoContent(http.StatusNoContent)
}

// newAssistantRunner builds the production assistantRunnerFace: the shared
// OpenRouter HTTP client, the live tool dependencies, and emit wired
// straight through. A package-level var (not a plain function) so tests can
// substitute a fake whole-run implementation without touching
// postAssistantMessageHandler.
var newAssistantRunner = func(apiKey, model, settingsPath string, emit assistantEmitter) assistantRunnerFace {
	return &assistantRunner{
		doer:   openRouterHTTPClient,
		apiKey: apiKey,
		model:  model,
		tools:  assistantProductionToolDeps(settingsPath),
		emit:   emit,
	}
}

// POST /api/assistant/conversations/:id/messages
//
// Order matters here (ADR 0093): validate the request body before touching
// any store or the network; check readiness before the conversation lookup,
// so an operator who hasn't configured the assistant yet gets that answer
// even for a conversation id that doesn't exist; check the conversation
// before the in-flight guard, so a stale tab polling a deleted conversation
// gets 404 rather than racing the guard; and only once all of that has
// passed does this write the SSE response headers. Once those headers are
// written, every remaining failure is reported as an SSE "error" event and
// this always returns nil - the response has already started, so there is
// no HTTP status left to change (mirrors telemetryStream's "a write error
// is an ordinary end to the request" reasoning).
func postAssistantMessageHandler(c echo.Context) error {
	id := c.Param("id")

	var body struct {
		Content string `json:"content"`
	}
	if err := c.Bind(&body); err != nil {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "invalid body"})
	}
	content := strings.TrimSpace(body.Content)
	if content == "" {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "content is required"})
	}
	if len([]rune(content)) > assistantMaxMessageChars {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": fmt.Sprintf("content must be %d characters or fewer", assistantMaxMessageChars)})
	}

	settingsPath := assistantSettingsPath()
	readiness, apiKey, err := checkAssistantReadiness(settingsPath)
	if err != nil {
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": err.Error()})
	}
	if readiness.Problem != "" {
		return c.JSON(http.StatusServiceUnavailable, map[string]string{"error": readiness.Problem})
	}

	if _, ok, err := globalAssistantStore.GetConversation(id); err != nil {
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": err.Error()})
	} else if !ok {
		return c.JSON(http.StatusNotFound, map[string]string{"error": "conversation not found"})
	}

	if !beginAssistantRun(id) {
		return c.JSON(http.StatusConflict, map[string]string{"error": "the assistant is still answering the previous message"})
	}
	defer endAssistantRun(id)

	previousMessages, err := globalAssistantStore.ListMessages(id)
	if err != nil {
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": err.Error()})
	}
	isFirstMessage := len(previousMessages) == 0

	userRow, err := globalAssistantStore.AppendMessage(assistantMessage{ConversationID: id, Role: "user", Content: content})
	if err != nil {
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": err.Error()})
	}
	if isFirstMessage {
		if err := globalAssistantStore.SetTitle(id, assistantConversationTitle(content)); err != nil {
			return c.JSON(http.StatusInternalServerError, map[string]string{"error": err.Error()})
		}
	}
	previousMessages = append(previousMessages, userRow)

	pc := collectAssistantPromptContext(settingsPath, time.Now())
	system := buildAssistantSystemPrompt(pc)

	header := c.Response().Header()
	header.Set("Content-Type", "text/event-stream")
	header.Set("Cache-Control", "no-cache")
	header.Set("Connection", "keep-alive")
	header.Set("X-Accel-Buffering", "no")
	c.Response().WriteHeader(http.StatusOK)

	emit := func(event string, payload any) {
		data, merr := json.Marshal(payload)
		if merr != nil {
			log.Printf("assistant: marshal %s event for conversation %s: %v", event, id, merr)
			return
		}
		fmt.Fprintf(c.Response(), "event: %s\ndata: %s\n\n", event, data)
		c.Response().Flush()
	}

	runner := newAssistantRunner(apiKey, readiness.Model, settingsPath, emit)
	reply, runErr := runner.run(c.Request().Context(), system, assistantHistoryMessages(previousMessages))
	if runErr != nil {
		log.Printf("assistant: run failed for conversation %s: %v", id, runErr)
		emit("error", map[string]string{"error": firstErrorLine(runErr)})
		return nil
	}

	assistantRow, err := globalAssistantStore.AppendMessage(assistantMessage{
		ConversationID:   id,
		Role:             "assistant",
		Content:          reply.Content,
		Model:            reply.Model,
		PromptTokens:     reply.PromptTokens,
		CompletionTokens: reply.CompletionTokens,
		CostUSD:          reply.CostUSD,
		ToolRounds:       reply.ToolRounds,
	})
	if err != nil {
		log.Printf("assistant: persist reply for conversation %s: %v", id, err)
		emit("error", map[string]string{"error": firstErrorLine(err)})
		return nil
	}

	updatedConv, ok, err := globalAssistantStore.GetConversation(id)
	if err != nil || !ok {
		log.Printf("assistant: reload conversation %s after reply: ok=%v err=%v", id, ok, err)
		emit("error", map[string]string{"error": "failed to reload the conversation after the reply"})
		return nil
	}

	emit("message", map[string]any{"message": assistantRow, "conversation": updatedConv})
	return nil
}
