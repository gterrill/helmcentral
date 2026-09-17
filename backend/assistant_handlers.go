package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"math"
	"net/http"
	"net/url"
	"regexp"
	"runtime/debug"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode"

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

// assistantScreenFieldMaxRunes bounds each field of a POST's screen context
// (panel/section/page). These are short UI identifiers, not operator text,
// so this is a defensive cap rather than a real limit any legitimate caller
// approaches - it exists so a malformed or hostile client can never grow the
// system prompt through this path.
const assistantScreenFieldMaxRunes = 80

// trimmedAssistantScreenField trims s and caps it at
// assistantScreenFieldMaxRunes runes, for one field of a POST's screen
// context (postAssistantMessageHandler).
func trimmedAssistantScreenField(s string) string {
	s = strings.TrimSpace(s)
	runes := []rune(s)
	if len(runes) > assistantScreenFieldMaxRunes {
		runes = runes[:assistantScreenFieldMaxRunes]
	}
	return string(runes)
}

// assistantReadiness is what GET /api/assistant/status reports, and what
// postAssistantMessageHandler checks before it will start a run. Problem is
// empty exactly when the assistant is ready to answer; it never causes a
// non-2xx response for the status endpoint itself (ADR 0093: the operator
// must always be able to see why the assistant can't run).
type assistantReadiness struct {
	Enabled    bool   `json:"enabled"`
	Configured bool   `json:"configured"`
	Model      string `json:"model"`
	// DocumentModel is assistant.document_model (ADR 0106): a separate
	// setting from Model, read here (buildSettingsPayload already parsed it
	// out of the same settings read) purely so document-only callers don't
	// have to re-read settings themselves. It deliberately plays no part in
	// Problem below - Problem gates chat (assistant/status, assistant/run),
	// and a blank document model must never make chat report not-ready.
	// documentEnrichReadinessProblem (documents_enrich.go) is what actually
	// enforces it, for the document upload/reindex handlers and the
	// indexer's enrich stage only.
	DocumentModel string `json:"document_model"`
	Problem       string `json:"problem,omitempty"`
}

const openRouterModelsListURL = "https://openrouter.ai/api/v1/models"

type assistantAutoRouterOptions struct {
	AllowedModels  []string
	ExcludedModels []string
	CostTier       string
}

type assistantModelOption struct {
	ID         string  `json:"id"`
	Name       string  `json:"name"`
	Price      float64 `json:"price"`
	CreatedAt  string  `json:"created_at"`
	Throughput float64 `json:"throughput"`
	Latency    float64 `json:"latency"`
	Popularity float64 `json:"popularity"`
}

var assistantOpenRouterDoer openRouterDoer = openRouterHTTPClient

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
		Enabled:       payload.Assistant.Enabled,
		Model:         payload.Assistant.Model,
		DocumentModel: payload.Assistant.DocumentModel,
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
// OpenRouter doer for every handler-level test. systemStable/systemLive are
// assistant_prompt.go's assistantSystemPromptParts split, kept separate all
// the way to here so assistant_run.go's assistantSystemMessage can mark an
// Anthropic model's cache_control breakpoint at the boundary between them.
type assistantRunnerFace interface {
	run(ctx context.Context, systemStable, systemLive string, history []openRouterMessage) (assistantReply, error)
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

func assistantSummaryTitle(content string) string {
	trimmed := strings.TrimSpace(content)
	if trimmed == "" {
		return ""
	}

	lines := strings.Split(trimmed, "\n")
	start := -1
	for i, line := range lines {
		if strings.EqualFold(strings.TrimSpace(line), "## Spoken summary") {
			start = i + 1
			break
		}
	}
	if start == -1 {
		return ""
	}

	var section []string
	for i := start; i < len(lines); i++ {
		line := strings.TrimSpace(lines[i])
		if line == "" {
			if len(section) > 0 {
				section = append(section, "")
			}
			continue
		}
		if strings.HasPrefix(line, "## ") {
			break
		}
		section = append(section, line)
	}

	text := strings.Join(section, " ")
	text = strings.TrimSpace(text)
	if text == "" {
		return ""
	}

	// Strip common markdown emphasis and links, then collapse whitespace so the
	// title is readable as a short label in the list column.
	re := regexp.MustCompile(`\[([^\]]+)\]\([^)]*\)`)
	text = re.ReplaceAllString(text, "$1")
	text = strings.NewReplacer("`", "", "**", "", "__", "", "*", "", "_", "").Replace(text)
	text = strings.Join(strings.Fields(text), " ")
	text = strings.Trim(text, "-:;,. ")
	if text == "" {
		return ""
	}
	return assistantConversationTitle(text)
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

// GET /api/assistant/models
// assistantModelsCacheTTL bounds how long a parsed OpenRouter model
// catalogue is reused. Every debounced keystroke in Settings > Assistant's
// model search, every page turn and every sort change used to refetch
// OpenRouter's whole /models catalogue, read the whole body and reparse it
// from scratch; the catalogue itself changes on the order of days, so 15
// minutes keeps this off the request path for the overwhelming majority of
// that traffic while still picking up a newly listed or removed model the
// same day.
const assistantModelsCacheTTL = 15 * time.Minute

// assistantModelsFetchTimeout bounds one upstream catalogue fetch.
// assistantModelsCache.get holds its mutex across the whole fetch (see
// below), so an unbounded request here would hang every other caller
// queued behind it forever, not just this one.
const assistantModelsFetchTimeout = 30 * time.Second

// assistantModelsCache holds the last parsed OpenRouter model catalogue.
// This is not keyed by the OpenRouter API key: fetchAssistantModelsFromOpenRouter
// sends no Authorization header at all (checked - the /models list is
// fetched anonymously; only the fixed ?supported_parameters=tools query
// varies the upstream response), so every caller gets the same catalogue
// and one shared cache entry is correct.
//
// get's mutex is held across the whole upstream fetch, not just the
// read/write of the cached fields, so concurrent callers who all miss the
// cache at once (a keystroke and a page change landing together) queue on
// each other rather than each firing their own upstream request - only the
// first actually calls OpenRouter, the rest see the freshly-populated cache
// once they get the lock.
type assistantModelsCache struct {
	mu        sync.Mutex
	models    []assistantModelOption
	fetchedAt time.Time
}

var globalAssistantModelsCache = &assistantModelsCache{}

// get returns the cached catalogue when it is still within
// assistantModelsCacheTTL of fetchedAt, otherwise calls fetch, caches
// whatever it returns, and returns that. A fetch error is never masked by
// serving a stale catalogue past its TTL (AGENTS.md's fallback policy) - it
// is returned as-is and nothing is cached, so the very next caller retries
// cleanly rather than being stuck behind a cached error.
func (c *assistantModelsCache) get(now time.Time, fetch func() ([]assistantModelOption, error)) ([]assistantModelOption, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.models != nil && now.Before(c.fetchedAt.Add(assistantModelsCacheTTL)) {
		return c.models, nil
	}

	models, err := fetch()
	if err != nil {
		return nil, err
	}
	c.models = models
	c.fetchedAt = now
	return c.models, nil
}

// fetchAssistantModelsFromOpenRouter fetches and parses OpenRouter's whole
// tool-capable model catalogue: one upstream call, no search/sort/paging
// applied - those happen afterward, in memory, over whatever this returns
// (assistantModelsHandler). Called through assistantModelsCache.get, never
// directly by the handler.
//
// This uses a background context bounded by assistantModelsFetchTimeout
// rather than any one HTTP request's context: the fetch may be serving
// several callers at once (assistantModelsCache merges concurrent misses
// into the one in flight), and a browser tab closing mid-fetch must not
// cancel the request the other callers are waiting on.
func fetchAssistantModelsFromOpenRouter() ([]assistantModelOption, error) {
	ctx, cancel := context.WithTimeout(context.Background(), assistantModelsFetchTimeout)
	defer cancel()

	upstreamURL := openRouterModelsListURL + "?" + (url.Values{"supported_parameters": {"tools"}}).Encode()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, upstreamURL, nil)
	if err != nil {
		return nil, fmt.Errorf("build openrouter models request: %w", err)
	}

	resp, err := assistantOpenRouterDoer.Do(req)
	if err != nil {
		return nil, fmt.Errorf("openrouter models request failed: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read openrouter models response: %w", err)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("openrouter models endpoint returned non-2xx (status %d)", resp.StatusCode)
	}

	var parsed struct {
		Data []struct {
			ID                  string   `json:"id"`
			Name                string   `json:"name"`
			SupportedParameters []string `json:"supported_parameters"`
			Created             int64    `json:"created"`
			CreatedAt           string   `json:"created_at"`
			Pricing             struct {
				Prompt     string `json:"prompt"`
				Completion string `json:"completion"`
			} `json:"pricing"`
			TopProvider struct {
				Throughput float64 `json:"throughput"`
				Latency    float64 `json:"latency"`
			} `json:"top_provider"`
			Throughput float64 `json:"throughput"`
			Latency    float64 `json:"latency"`
			Popularity float64 `json:"popularity"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &parsed); err != nil {
		return nil, fmt.Errorf("parse openrouter models response: %w", err)
	}

	models := make([]assistantModelOption, 0, len(parsed.Data))
	for _, model := range parsed.Data {
		if !containsString(model.SupportedParameters, "tools") {
			continue
		}
		name := strings.TrimSpace(model.Name)
		if name == "" {
			name = model.ID
		}
		throughput := model.TopProvider.Throughput
		if throughput == 0 {
			throughput = model.Throughput
		}
		latency := model.TopProvider.Latency
		if latency == 0 {
			latency = model.Latency
		}
		createdAt := strings.TrimSpace(model.CreatedAt)
		if createdAt == "" && model.Created > 0 {
			createdAt = time.Unix(model.Created, 0).UTC().Format(time.RFC3339)
		}
		models = append(models, assistantModelOption{
			ID:         model.ID,
			Name:       name,
			Price:      assistantModelPrice(model.Pricing.Prompt, model.Pricing.Completion),
			CreatedAt:  createdAt,
			Throughput: throughput,
			Latency:    latency,
			Popularity: model.Popularity,
		})
	}
	return models, nil
}

// GET /api/assistant/models
func assistantModelsHandler(c echo.Context) error {
	sortBy, sortDesc := assistantModelSortQuery(c.QueryParam("sort"), c.QueryParam("order"))
	searchQuery := c.QueryParam("q")
	page := intQueryParamWithDefault(c.QueryParam("page"), 1)
	pageSize := intQueryParamWithDefault(c.QueryParam("page_size"), 20)
	if page < 1 {
		page = 1
	}
	if pageSize < 1 {
		pageSize = 20
	}
	if pageSize > 100 {
		pageSize = 100
	}

	// The catalogue itself (every tool-capable model OpenRouter lists) is
	// cached; search, sort and paging below are applied in memory to a copy
	// of it on every request, so a debounced keystroke never refetches
	// OpenRouter (see assistantModelsCache).
	cached, err := globalAssistantModelsCache.get(time.Now(), fetchAssistantModelsFromOpenRouter)
	if err != nil {
		return c.JSON(http.StatusBadGateway, map[string]string{"error": err.Error()})
	}

	// A defensive copy: cached is the cache's own backing slice, and the
	// sort below mutates in place - sorting it directly would silently
	// reorder every other caller's cached catalogue too, and race with a
	// concurrent reader.
	models := append([]assistantModelOption(nil), cached...)
	if strings.TrimSpace(searchQuery) != "" {
		filtered := make([]assistantModelOption, 0, len(models))
		for _, model := range models {
			if assistantModelMatchesQuery(model, searchQuery) {
				filtered = append(filtered, model)
			}
		}
		models = filtered
	}
	sort.SliceStable(models, func(i, j int) bool {
		return assistantModelLess(models[i], models[j], sortBy, sortDesc)
	})

	total := len(models)
	start := (page - 1) * pageSize
	if start > total {
		start = total
	}
	end := start + pageSize
	if end > total {
		end = total
	}
	paged := models[start:end]

	return c.JSON(http.StatusOK, map[string]any{
		"models": paged,
		"page": map[string]any{
			"index":        page,
			"size":         pageSize,
			"total_models": total,
			"total_pages":  maxInt(1, int(math.Ceil(float64(total)/float64(pageSize)))),
		},
		"sort": map[string]any{
			"by":    sortBy,
			"order": ternaryOrder(sortDesc),
		},
	})
}

func containsString(values []string, needle string) bool {
	for _, value := range values {
		if strings.EqualFold(strings.TrimSpace(value), needle) {
			return true
		}
	}
	return false
}

func normalizeAssistantSearchText(value string) string {
	lower := strings.ToLower(value)
	return strings.Join(strings.FieldsFunc(lower, func(r rune) bool {
		return !(unicode.IsLetter(r) || unicode.IsDigit(r))
	}), " ")
}

func assistantModelMatchesQuery(model assistantModelOption, query string) bool {
	trimmedQuery := strings.TrimSpace(strings.ToLower(query))
	if trimmedQuery == "" {
		return true
	}
	idLower := strings.ToLower(model.ID)
	nameLower := strings.ToLower(model.Name)
	if strings.Contains(idLower, trimmedQuery) || strings.Contains(nameLower, trimmedQuery) {
		return true
	}

	normalizedHaystack := normalizeAssistantSearchText(model.ID + " " + model.Name)
	tokens := strings.Fields(normalizeAssistantSearchText(trimmedQuery))
	if len(tokens) == 0 {
		return false
	}
	for _, token := range tokens {
		if !strings.Contains(normalizedHaystack, token) {
			return false
		}
	}
	return true
}

func assistantModelPrice(prompt, completion string) float64 {
	promptPrice, _ := strconv.ParseFloat(strings.TrimSpace(prompt), 64)
	completionPrice, _ := strconv.ParseFloat(strings.TrimSpace(completion), 64)
	if promptPrice <= 0 && completionPrice <= 0 {
		return 0
	}
	if promptPrice <= 0 {
		return completionPrice
	}
	if completionPrice <= 0 {
		return promptPrice
	}
	return (promptPrice + completionPrice) / 2.0
}

func assistantModelSortQuery(sortByRaw, orderRaw string) (string, bool) {
	sortBy := strings.TrimSpace(strings.ToLower(sortByRaw))
	order := strings.TrimSpace(strings.ToLower(orderRaw))
	desc := order == "desc"
	switch sortBy {
	case "price":
		if order == "" {
			desc = false
		}
		return "price", desc
	case "throughput":
		if order == "" {
			desc = true
		}
		return "throughput", desc
	case "latency":
		if order == "" {
			desc = false
		}
		return "latency", desc
	case "popular":
		if order == "" {
			desc = true
		}
		return "popular", desc
	case "newest":
		if order == "" {
			desc = true
		}
		return "newest", desc
	default:
		return "popular", true
	}
}

func assistantModelLess(a, b assistantModelOption, sortBy string, desc bool) bool {
	cmp := 0
	switch sortBy {
	case "price":
		cmp = compareFloat(a.Price, b.Price)
	case "throughput":
		cmp = compareFloat(a.Throughput, b.Throughput)
	case "latency":
		cmp = compareFloat(a.Latency, b.Latency)
	case "newest":
		cmp = compareString(a.CreatedAt, b.CreatedAt)
	default:
		cmp = compareFloat(a.Popularity, b.Popularity)
	}
	if cmp == 0 {
		return strings.ToLower(a.Name) < strings.ToLower(b.Name)
	}
	if desc {
		return cmp > 0
	}
	return cmp < 0
}

func compareFloat(a, b float64) int {
	if a == b {
		return 0
	}
	if a > b {
		return 1
	}
	return -1
}

func compareString(a, b string) int {
	if a == b {
		return 0
	}
	if a > b {
		return 1
	}
	return -1
}

func intQueryParamWithDefault(value string, fallback int) int {
	parsed, err := strconv.Atoi(strings.TrimSpace(value))
	if err != nil {
		return fallback
	}
	return parsed
}

func ternaryOrder(desc bool) string {
	if desc {
		return "desc"
	}
	return "asc"
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
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
var newAssistantRunner = func(apiKey, model, settingsPath string, autoRouter assistantAutoRouterOptions, emit assistantEmitter) assistantRunnerFace {
	return &assistantRunner{
		doer:       openRouterHTTPClient,
		apiKey:     apiKey,
		model:      model,
		autoRouter: autoRouter,
		tools:      assistantProductionToolDeps(settingsPath),
		emit:       emit,
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
//
// ADR 0105 ("the answer outlives the page"): everything above this point
// happens synchronously, exactly as before. From here on, the actual
// agentic run happens in a goroutine driven by a context derived from
// context.Background(), not this request's - see
// assistantRunRegistry.start's own doc comment. This handler's only job
// after starting that goroutine is to subscribe to the run's event log from
// the beginning and stream it to this particular client
// (streamAssistantRun); if that client disconnects, only this call
// returns - the run keeps going, and GET .../run is how a page (this tab
// reopened, or a different one) rejoins it.
func postAssistantMessageHandler(c echo.Context) error {
	id := c.Param("id")

	var body struct {
		Content string `json:"content"`
		// Spoken and Screen are both optional and turn-scoped (mate-voice-
		// assistant plan): an absent body field leaves assistantPromptContext's
		// Spoken/Screen at their zero value, which buildAssistantSystemPrompt
		// already renders as "say nothing about this" - a plain text-composer
		// question must produce byte-for-byte the same prompt it did before
		// these fields existed.
		Spoken bool                    `json:"spoken"`
		Screen *assistantScreenContext `json:"screen"`
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
	settingsMap, err := readSettings(settingsPath)
	if err != nil {
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": fmt.Sprintf("read settings for assistant routing: %v", err)})
	}
	settingsPayload := buildSettingsPayload(settingsMap)
	autoRouter := assistantAutoRouterOptions{
		AllowedModels:  settingsPayload.Assistant.AllowedModels,
		ExcludedModels: settingsPayload.Assistant.ExcludedModels,
		CostTier:       settingsPayload.Assistant.CostTier,
	}

	if _, ok, err := globalAssistantStore.GetConversation(id); err != nil {
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": err.Error()})
	} else if !ok {
		return c.JSON(http.StatusNotFound, map[string]string{"error": "conversation not found"})
	}

	runCtx, run, started := globalAssistantRuns.start(id)
	if !started {
		return c.JSON(http.StatusConflict, map[string]string{"error": "the assistant is still answering the previous message"})
	}

	previousMessages, err := globalAssistantStore.ListMessages(id)
	if err != nil {
		globalAssistantRuns.remove(id, run)
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": err.Error()})
	}
	isFirstMessage := len(previousMessages) == 0

	userRow, err := globalAssistantStore.AppendMessage(assistantMessage{ConversationID: id, Role: "user", Content: content})
	if err != nil {
		globalAssistantRuns.remove(id, run)
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": err.Error()})
	}
	if isFirstMessage {
		if err := globalAssistantStore.SetTitle(id, assistantConversationTitle(content)); err != nil {
			globalAssistantRuns.remove(id, run)
			return c.JSON(http.StatusInternalServerError, map[string]string{"error": err.Error()})
		}
	}
	previousMessages = append(previousMessages, userRow)

	pc := collectAssistantPromptContext(settingsPath, time.Now())
	pc.Spoken = body.Spoken
	if body.Screen != nil {
		pc.Screen = assistantScreenContext{
			Panel:   trimmedAssistantScreenField(body.Screen.Panel),
			Section: trimmedAssistantScreenField(body.Screen.Section),
			Page:    trimmedAssistantScreenField(body.Screen.Page),
		}
	}
	systemStable, systemLive := assistantSystemPromptParts(pc)
	history := assistantHistoryMessages(previousMessages)

	runner := newAssistantRunner(apiKey, readiness.Model, settingsPath, autoRouter, run.append)

	// runCtx, not c.Request().Context(): this goroutine, and the run it
	// drives, must outlive this one HTTP request (ADR 0105). Only
	// run.cancel() (POST .../run/cancel) or assistantRunTimeout, applied
	// inside runner.run itself, ever ends it early.
	go func() {
		defer globalAssistantRuns.remove(id, run)
		// A panic anywhere below - runner.run, SetTitle, AppendMessage,
		// GetConversation - only trips Echo's middleware.Recover on the
		// request goroutine, not this one, and this goroutine outlives the
		// request (ADR 0105). Left unrecovered it crashes the whole process,
		// taking anchor watch and alarm evaluation down with it. finish() is
		// safe to call more than once (see its own comment) so this costs
		// nothing on the normal paths, which already call it themselves.
		defer func() {
			if r := recover(); r != nil {
				log.Printf("assistant: run for conversation %s panicked: %v\n%s", id, r, debug.Stack())
				run.append("error", map[string]string{"error": fmt.Sprintf("the assistant run crashed: %v", r)})
				run.finish()
			}
		}()

		reply, runErr := runner.run(runCtx, systemStable, systemLive, history)
		if runErr != nil {
			if run.wasCancelled() {
				log.Printf("assistant: run for conversation %s stopped by the operator", id)
				run.append("error", map[string]string{"error": "stopped"})
			} else {
				log.Printf("assistant: run failed for conversation %s: %v", id, runErr)
				run.append("error", map[string]string{"error": firstErrorLine(runErr)})
			}
			run.finish()
			return
		}
		log.Printf("assistant: conversation %s answered by %s in %d tool rounds, %d prompt + %d completion tokens, $%.4f",
			id, reply.Model, reply.ToolRounds, reply.PromptTokens, reply.CompletionTokens, reply.CostUSD)

		if summaryTitle := assistantSummaryTitle(reply.Content); summaryTitle != "" {
			if err := globalAssistantStore.SetTitle(id, summaryTitle); err != nil {
				log.Printf("assistant: update conversation summary title for %s: %v", id, err)
				run.append("error", map[string]string{"error": firstErrorLine(err)})
				run.finish()
				return
			}
		}

		// The assistant row is persisted before "message" is appended, so a
		// client that finds no run in progress (GET .../run -> 204) can
		// still read the answer straight from GET the conversation.
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
			run.append("error", map[string]string{"error": firstErrorLine(err)})
			run.finish()
			return
		}

		updatedConv, ok, err := globalAssistantStore.GetConversation(id)
		if err != nil || !ok {
			log.Printf("assistant: reload conversation %s after reply: ok=%v err=%v", id, ok, err)
			run.append("error", map[string]string{"error": "failed to reload the conversation after the reply"})
			run.finish()
			return
		}

		run.append("message", map[string]any{"message": assistantRow, "conversation": updatedConv})
		run.finish()
	}()

	writeAssistantRunSSEHeaders(c)
	streamAssistantRun(c.Request().Context(), run, 0, c.Response())
	return nil
}

// GET /api/assistant/conversations/:id/run
//
// ADR 0105: lets a page rejoin whatever reply is still being written after
// it left and came back - a navigation to another panel, a reload, an
// iPad's screen lock killing the original POST's fetch outright. If a run
// is in flight, this replays its whole event log so far and then streams
// live events exactly like the POST that started it did, until the run
// finishes or this client disconnects (which, same as the POST, never
// cancels the run - it only stops this particular subscription). If no run
// is in flight - the answer already finished, or nothing was ever asked -
// this responds 204 with no body; the caller already has GET
// .../conversations/:id for the finished answer, since the assistant row is
// always persisted before the run's own "message" event is appended.
func getAssistantRunHandler(c echo.Context) error {
	id := c.Param("id")

	run, ok := globalAssistantRuns.get(id)
	if !ok {
		return c.NoContent(http.StatusNoContent)
	}

	writeAssistantRunSSEHeaders(c)
	streamAssistantRun(c.Request().Context(), run, 0, c.Response())
	return nil
}

// POST /api/assistant/conversations/:id/run/cancel
//
// ADR 0105: the operator's own Stop button, now a real cancel rather than
// just closing the local SSE stream (which used to be all "stopping" a
// question meant, and never actually told the server to stop working). If a
// run is in flight for id, its context is cancelled; the run then ends with
// no assistant row persisted, the same outcome Stop already produced before
// this change, and every subscriber (this tab, and any other GET .../run
// listener) sees an "error" event with a short "stopped" message rather
// than the run's own internal cancellation error. Responds 204 whether or
// not a run was actually running, so this is safe to call more than once -
// a double click, or a Stop that lands just as the run was already
// finishing on its own.
func postAssistantRunCancelHandler(c echo.Context) error {
	id := c.Param("id")
	if run, ok := globalAssistantRuns.get(id); ok {
		run.cancel()
		// Answer only once the conversation is free: the run can take a
		// moment to unwind its OpenRouter call, and a question sent before
		// then would be refused with 409.
		select {
		case <-run.released:
		case <-c.Request().Context().Done():
			return c.Request().Context().Err()
		}
	}
	return c.NoContent(http.StatusNoContent)
}
