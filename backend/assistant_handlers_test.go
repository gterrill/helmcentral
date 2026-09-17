package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/labstack/echo/v4"
)

// withTestAssistantStore points globalAssistantStore at a fresh
// t.TempDir()-backed store for the duration of the test, restoring the
// prior value afterward - the same package-level-var swap idiom as
// withTestSecretsStore (secrets_settings_handlers_test.go). The assistant
// handlers read/write globalAssistantStore directly, so tests must swap it
// in rather than constructing the echo.HandlerFunc with a store argument.
func withTestAssistantStore(t *testing.T) *assistantStore {
	t.Helper()
	store := newTestAssistantStore(t)
	prev := globalAssistantStore
	globalAssistantStore = store
	t.Cleanup(func() { globalAssistantStore = prev })
	return store
}

// writeAssistantSettingsFixture writes a settings.yaml containing only an
// assistant block, direct to disk rather than through updateSettingsHandler
// - postSettings/normalizeSettingsPayload would default a blank model to
// defaultAssistantModel, and the readiness tests need to pin an explicit
// on-disk blank model (mirrors TestBuildSettingsPayload_SurfacesBlankAssistantModelFromDisk
// in signalk_test.go).
func writeAssistantSettingsFixture(t *testing.T, enabled bool, model string) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "settings.yaml")
	body := fmt.Sprintf("assistant:\n    enabled: %v\n    model: %q\n    notes: \"\"\n", enabled, model)
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatalf("write settings fixture: %v", err)
	}
	return path
}

// fakeAssistantRunner is a whole-run assistantRunnerFace test double: it
// emits one status event (proving the handler wires emit through to
// whatever newAssistantRunner returns), then one "delta" event per string
// in deltas (if any - proving the handler forwards whatever run() emits,
// token streaming included, ahead of its own "message" event), and then
// returns a canned reply or error, with no real OpenRouter or tool-call
// machinery involved. gotSystem records the system prompt run was actually
// called with, so a test can assert on what postAssistantMessageHandler
// built for that turn (e.g. the "## Spoken summary" instruction and a
// screen-context sentence) without scripting an OpenRouter doer.
type fakeAssistantRunner struct {
	emit      assistantEmitter
	reply     assistantReply
	err       error
	gotSystem string
	deltas    []string
}

func (f *fakeAssistantRunner) run(ctx context.Context, systemStable, systemLive string, history []openRouterMessage) (assistantReply, error) {
	f.gotSystem = systemStable + systemLive
	f.emit("status", assistantStatus("Thinking…"))
	for _, d := range f.deltas {
		f.emit("delta", assistantDelta(d))
	}
	if f.err != nil {
		return assistantReply{}, f.err
	}
	return f.reply, nil
}

// blockingAssistantRunner is a whole-run test double that signals
// (via started) that it has begun, then waits for the test to release it
// (via proceed) - used to hold the in-flight guard open deterministically
// while a second request is sent (no sleep-based race).
type blockingAssistantRunner struct {
	started chan struct{}
	proceed chan struct{}
	reply   assistantReply
}

func (b *blockingAssistantRunner) run(ctx context.Context, systemStable, systemLive string, history []openRouterMessage) (assistantReply, error) {
	close(b.started)
	<-b.proceed
	return b.reply, nil
}

// swapAssistantRunner replaces newAssistantRunner for the duration of a
// test, restoring the previous value on cleanup.
func swapAssistantRunner(t *testing.T, fn func(apiKey, model, settingsPath string, autoRouter assistantAutoRouterOptions, emit assistantEmitter) assistantRunnerFace) {
	t.Helper()
	prev := newAssistantRunner
	newAssistantRunner = fn
	t.Cleanup(func() { newAssistantRunner = prev })
}

// extractSSEEventData returns the JSON payload of the first "event: name"
// frame in body, or fails the test - the same "event: X\ndata: Y\n\n"
// framing vessel_state_stream.go writes.
func extractSSEEventData(t *testing.T, body, event string) string {
	t.Helper()
	marker := "event: " + event + "\n"
	idx := strings.Index(body, marker)
	if idx < 0 {
		t.Fatalf("event %q not found in body:\n%s", event, body)
	}
	rest := body[idx+len(marker):]
	const dataPrefix = "data: "
	if !strings.HasPrefix(rest, dataPrefix) {
		t.Fatalf("expected a data: line after event %q, got:\n%s", event, rest)
	}
	rest = rest[len(dataPrefix):]
	end := strings.Index(rest, "\n")
	if end < 0 {
		t.Fatalf("unterminated data line for event %q", event)
	}
	return rest[:end]
}

func newAssistantEchoContext(method, path, body, id string) (echo.Context, *httptest.ResponseRecorder) {
	e := echo.New()
	var req *http.Request
	if body != "" {
		req = httptest.NewRequest(method, path, strings.NewReader(body))
		req.Header.Set(echo.HeaderContentType, echo.MIMEApplicationJSON)
	} else {
		req = httptest.NewRequest(method, path, nil)
	}
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)
	if id != "" {
		c.SetParamNames("id")
		c.SetParamValues(id)
	}
	return c, rec
}

// ── GET /api/assistant/status ──────────────────────────────────────────

func TestAssistantStatusHandler_DisabledNamesSettingsAssistant(t *testing.T) {
	withTestSecretsStore(t)
	t.Setenv("SETTINGS_FILE", writeAssistantSettingsFixture(t, false, "openai/gpt-4o"))

	c, rec := newAssistantEchoContext(http.MethodGet, "/api/assistant/status", "", "")
	if err := assistantStatusHandler(c); err != nil {
		t.Fatalf("handler returned error: %v", err)
	}
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	var status assistantReadiness
	if err := json.Unmarshal(rec.Body.Bytes(), &status); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if status.Enabled {
		t.Errorf("expected enabled=false")
	}
	if !strings.Contains(status.Problem, "Settings → Assistant") {
		t.Errorf("expected the problem to name Settings → Assistant, got %q", status.Problem)
	}
}

func TestAssistantStatusHandler_EnabledNoKeyIsUnconfigured(t *testing.T) {
	withTestSecretsStore(t)
	t.Setenv("SETTINGS_FILE", writeAssistantSettingsFixture(t, true, "openai/gpt-4o"))

	c, rec := newAssistantEchoContext(http.MethodGet, "/api/assistant/status", "", "")
	if err := assistantStatusHandler(c); err != nil {
		t.Fatalf("handler returned error: %v", err)
	}

	var status assistantReadiness
	if err := json.Unmarshal(rec.Body.Bytes(), &status); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if status.Configured {
		t.Errorf("expected configured=false with no stored key")
	}
	if !strings.Contains(strings.ToLower(status.Problem), "openrouter") {
		t.Errorf("expected the problem to name the OpenRouter API key, got %q", status.Problem)
	}
	if !strings.Contains(status.Problem, "Settings → Assistant") {
		t.Errorf("expected the problem to name Settings → Assistant, got %q", status.Problem)
	}
}

func TestAssistantStatusHandler_EnabledKeyBlankModelNamesModel(t *testing.T) {
	store := withTestSecretsStore(t)
	if err := store.Set("OPENROUTER_API_KEY", "sk-test"); err != nil {
		t.Fatalf("Set: %v", err)
	}
	t.Setenv("SETTINGS_FILE", writeAssistantSettingsFixture(t, true, ""))

	c, rec := newAssistantEchoContext(http.MethodGet, "/api/assistant/status", "", "")
	if err := assistantStatusHandler(c); err != nil {
		t.Fatalf("handler returned error: %v", err)
	}

	var status assistantReadiness
	if err := json.Unmarshal(rec.Body.Bytes(), &status); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if !strings.Contains(strings.ToLower(status.Problem), "model") {
		t.Errorf("expected the problem to name the model, got %q", status.Problem)
	}
	if !strings.Contains(status.Problem, "Settings → Assistant") {
		t.Errorf("expected the problem to name Settings → Assistant, got %q", status.Problem)
	}
}

// TestCheckAssistantReadiness_BlankDocumentModelDoesNotBlockChat is the
// follow-up correction to the review finding above: checkAssistantReadiness
// (and therefore readiness.Problem, which both assistantStatusHandler and
// postAssistantMessageHandler gate chat on) must stay chat-only. A blank
// assistant.document_model is a document-enrichment concern - enforced by
// documentEnrichReadinessProblem (documents_enrich.go), applied only on the
// document upload/reindex handlers and the indexer's enrich stage - and
// must never make chat itself report not-ready.
func TestCheckAssistantReadiness_BlankDocumentModelDoesNotBlockChat(t *testing.T) {
	store := withTestSecretsStore(t)
	if err := store.Set("OPENROUTER_API_KEY", "sk-test"); err != nil {
		t.Fatalf("Set: %v", err)
	}
	dir := t.TempDir()
	path := filepath.Join(dir, "settings.yaml")
	body := "assistant:\n    enabled: true\n    model: \"openai/gpt-4o\"\n    document_model: \"\"\n    notes: \"\"\n"
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatalf("write settings fixture: %v", err)
	}

	readiness, apiKey, err := checkAssistantReadiness(path)
	if err != nil {
		t.Fatalf("checkAssistantReadiness: %v", err)
	}
	if readiness.Problem != "" {
		t.Fatalf("expected chat readiness to stay ready despite a blank document model, got problem %q", readiness.Problem)
	}
	if apiKey == "" {
		t.Fatalf("expected the api key to be returned once chat itself is ready")
	}
}

func TestAssistantStatusHandler_AllSetIsReadyWithEchoedModel(t *testing.T) {
	store := withTestSecretsStore(t)
	if err := store.Set("OPENROUTER_API_KEY", "sk-test"); err != nil {
		t.Fatalf("Set: %v", err)
	}
	t.Setenv("SETTINGS_FILE", writeAssistantSettingsFixture(t, true, "openai/gpt-4o"))

	c, rec := newAssistantEchoContext(http.MethodGet, "/api/assistant/status", "", "")
	if err := assistantStatusHandler(c); err != nil {
		t.Fatalf("handler returned error: %v", err)
	}

	var status assistantReadiness
	if err := json.Unmarshal(rec.Body.Bytes(), &status); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if status.Problem != "" {
		t.Errorf("expected no problem, got %q", status.Problem)
	}
	if !status.Enabled || !status.Configured {
		t.Errorf("expected enabled and configured true, got %+v", status)
	}
	if status.Model != "openai/gpt-4o" {
		t.Errorf("expected the model to be echoed, got %q", status.Model)
	}
}

func TestAssistantModelsHandler_ReturnsToolCapableModelsFromOpenRouter(t *testing.T) {
	resetAssistantModelsCache(t)
	fake := &fakeOpenRouterDoer{responses: []*http.Response{openRouterFakeResponse(http.StatusOK, `{
		"data": [
			{"id": "anthropic/claude-sonnet-4.5", "name": "Claude Sonnet 4.5", "supported_parameters": ["tools", "temperature"]},
			{"id": "openai/gpt-4o-mini", "supported_parameters": ["tools"]}
		]
	}`)}}
	prev := assistantOpenRouterDoer
	assistantOpenRouterDoer = fake
	t.Cleanup(func() { assistantOpenRouterDoer = prev })

	c, rec := newAssistantEchoContext(http.MethodGet, "/api/assistant/models", "", "")
	if err := assistantModelsHandler(c); err != nil {
		t.Fatalf("handler returned error: %v", err)
	}
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	if len(fake.requests) != 1 {
		t.Fatalf("expected one upstream call, got %d", len(fake.requests))
	}
	if got := fake.requests[0].URL.String(); got != "https://openrouter.ai/api/v1/models?supported_parameters=tools" {
		t.Fatalf("expected tools-filtered models URL, got %q", got)
	}

	var resp struct {
		Models []struct {
			ID   string `json:"id"`
			Name string `json:"name"`
		} `json:"models"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}
	if len(resp.Models) != 2 {
		t.Fatalf("expected 2 models, got %+v", resp.Models)
	}
	if resp.Models[0].ID != "anthropic/claude-sonnet-4.5" || resp.Models[0].Name != "Claude Sonnet 4.5" {
		t.Fatalf("unexpected first model: %+v", resp.Models[0])
	}
	// Name falls back to the id when upstream omits it.
	if resp.Models[1].ID != "openai/gpt-4o-mini" || resp.Models[1].Name != "openai/gpt-4o-mini" {
		t.Fatalf("unexpected second model: %+v", resp.Models[1])
	}
}

func TestAssistantModelsHandler_UpstreamErrorReturnsBadGateway(t *testing.T) {
	resetAssistantModelsCache(t)
	fake := &fakeOpenRouterDoer{responses: []*http.Response{openRouterFakeResponse(http.StatusBadGateway, `{"error":"upstream"}`)}}
	prev := assistantOpenRouterDoer
	assistantOpenRouterDoer = fake
	t.Cleanup(func() { assistantOpenRouterDoer = prev })

	c, rec := newAssistantEchoContext(http.MethodGet, "/api/assistant/models", "", "")
	if err := assistantModelsHandler(c); err != nil {
		t.Fatalf("handler returned error: %v", err)
	}
	if rec.Code != http.StatusBadGateway {
		t.Fatalf("expected 502, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestAssistantModelsHandler_SortsAndPaginatesServerSide(t *testing.T) {
	resetAssistantModelsCache(t)
	fake := &fakeOpenRouterDoer{responses: []*http.Response{openRouterFakeResponse(http.StatusOK, `{
		"data": [
			{"id": "vendor/fast", "name": "Fast", "supported_parameters": ["tools"], "pricing": {"prompt": "0.003", "completion": "0.006"}, "popularity": 50, "top_provider": {"throughput": 100, "latency": 30}, "created": 1725753600},
			{"id": "vendor/cheap", "name": "Cheap", "supported_parameters": ["tools"], "pricing": {"prompt": "0.001", "completion": "0.001"}, "popularity": 20, "top_provider": {"throughput": 40, "latency": 20}, "created": 1725840000},
			{"id": "vendor/steady", "name": "Steady", "supported_parameters": ["tools"], "pricing": {"prompt": "0.002", "completion": "0.002"}, "popularity": 80, "top_provider": {"throughput": 70, "latency": 25}, "created": 1725926400}
		]
	}`)}}
	prev := assistantOpenRouterDoer
	assistantOpenRouterDoer = fake
	t.Cleanup(func() { assistantOpenRouterDoer = prev })

	c, rec := newAssistantEchoContext(http.MethodGet, "/api/assistant/models?sort=price&order=asc&page=2&page_size=1", "", "")
	if err := assistantModelsHandler(c); err != nil {
		t.Fatalf("handler returned error: %v", err)
	}
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	var resp struct {
		Models []struct {
			ID    string  `json:"id"`
			Name  string  `json:"name"`
			Price float64 `json:"price"`
		} `json:"models"`
		Page struct {
			Index      int `json:"index"`
			Size       int `json:"size"`
			Total      int `json:"total_models"`
			TotalPages int `json:"total_pages"`
		} `json:"page"`
		Sort struct {
			By    string `json:"by"`
			Order string `json:"order"`
		} `json:"sort"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}
	if len(resp.Models) != 1 {
		t.Fatalf("expected one model on page 2, got %+v", resp.Models)
	}
	if resp.Models[0].ID != "vendor/steady" {
		t.Fatalf("expected the middle-priced model on page 2, got %+v", resp.Models[0])
	}
	if resp.Page.Index != 2 || resp.Page.Size != 1 || resp.Page.Total != 3 || resp.Page.TotalPages != 3 {
		t.Fatalf("unexpected page metadata: %+v", resp.Page)
	}
	if resp.Sort.By != "price" || resp.Sort.Order != "asc" {
		t.Fatalf("unexpected sort metadata: %+v", resp.Sort)
	}
}

func TestAssistantModelsHandler_FiltersByQueryBeforePagination(t *testing.T) {
	resetAssistantModelsCache(t)
	fake := &fakeOpenRouterDoer{responses: []*http.Response{openRouterFakeResponse(http.StatusOK, `{
		"data": [
			{"id": "z-ai/glm-5.3", "name": "Z.ai: GLM 5.3", "supported_parameters": ["tools"]},
			{"id": "z-ai/glm-5.2", "name": "Z.ai: GLM 5.2", "supported_parameters": ["tools"]},
			{"id": "anthropic/claude-sonnet-4.5", "name": "Claude Sonnet 4.5", "supported_parameters": ["tools"]}
		]
	}`)}}
	prev := assistantOpenRouterDoer
	assistantOpenRouterDoer = fake
	t.Cleanup(func() { assistantOpenRouterDoer = prev })

	c, rec := newAssistantEchoContext(http.MethodGet, "/api/assistant/models?q=glm+5.3&page=1&page_size=1", "", "")
	if err := assistantModelsHandler(c); err != nil {
		t.Fatalf("handler returned error: %v", err)
	}
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	var resp struct {
		Models []struct {
			ID string `json:"id"`
		} `json:"models"`
		Page struct {
			Total      int `json:"total_models"`
			TotalPages int `json:"total_pages"`
		} `json:"page"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}
	if len(resp.Models) != 1 || resp.Models[0].ID != "z-ai/glm-5.3" {
		t.Fatalf("expected only z-ai/glm-5.3, got %+v", resp.Models)
	}
	if resp.Page.Total != 1 || resp.Page.TotalPages != 1 {
		t.Fatalf("expected filtered totals of 1, got %+v", resp.Page)
	}
}

// resetAssistantModelsCache clears the package-level model-catalogue cache
// so a test that swaps assistantOpenRouterDoer and expects exactly one
// upstream call isn't served a stale hit left behind by an earlier test.
func resetAssistantModelsCache(t *testing.T) {
	t.Helper()
	globalAssistantModelsCache = &assistantModelsCache{}
}

func TestAssistantModelsHandler_RepeatedSearchesWithinTTLMakeOneUpstreamCall(t *testing.T) {
	resetAssistantModelsCache(t)
	fake := &fakeOpenRouterDoer{responses: []*http.Response{openRouterFakeResponse(http.StatusOK, `{
		"data": [
			{"id": "z-ai/glm-5.3", "name": "Z.ai: GLM 5.3", "supported_parameters": ["tools"]},
			{"id": "anthropic/claude-sonnet-4.5", "name": "Claude Sonnet 4.5", "supported_parameters": ["tools"]}
		]
	}`)}}
	prev := assistantOpenRouterDoer
	assistantOpenRouterDoer = fake
	t.Cleanup(func() { assistantOpenRouterDoer = prev })

	c1, rec1 := newAssistantEchoContext(http.MethodGet, "/api/assistant/models?q=glm", "", "")
	if err := assistantModelsHandler(c1); err != nil {
		t.Fatalf("handler returned error: %v", err)
	}
	if rec1.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec1.Code, rec1.Body.String())
	}

	c2, rec2 := newAssistantEchoContext(http.MethodGet, "/api/assistant/models?q=claude", "", "")
	if err := assistantModelsHandler(c2); err != nil {
		t.Fatalf("handler returned error: %v", err)
	}
	if rec2.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec2.Code, rec2.Body.String())
	}

	if len(fake.requests) != 1 {
		t.Fatalf("expected the second search (still within the cache TTL) to reuse the cached catalogue, got %d upstream calls", len(fake.requests))
	}

	var resp2 struct {
		Models []struct {
			ID string `json:"id"`
		} `json:"models"`
	}
	if err := json.Unmarshal(rec2.Body.Bytes(), &resp2); err != nil {
		t.Fatalf("unmarshal second response: %v", err)
	}
	if len(resp2.Models) != 1 || resp2.Models[0].ID != "anthropic/claude-sonnet-4.5" {
		t.Fatalf("expected the second search filtered from the cached catalogue, got %+v", resp2.Models)
	}
}

func TestAssistantModelsCache_ReusesWithinTTL(t *testing.T) {
	c := &assistantModelsCache{}
	now := time.Now()
	calls := 0
	fetch := func() ([]assistantModelOption, error) {
		calls++
		return []assistantModelOption{{ID: "m"}}, nil
	}

	if _, err := c.get(now, fetch); err != nil {
		t.Fatalf("get: %v", err)
	}
	if _, err := c.get(now.Add(time.Minute), fetch); err != nil {
		t.Fatalf("get: %v", err)
	}
	if calls != 1 {
		t.Fatalf("expected one fetch for two calls within the TTL, got %d", calls)
	}
}

func TestAssistantModelsCache_ExpiryRefetches(t *testing.T) {
	c := &assistantModelsCache{}
	now := time.Now()
	calls := 0
	fetch := func() ([]assistantModelOption, error) {
		calls++
		return []assistantModelOption{{ID: fmt.Sprintf("m%d", calls)}}, nil
	}

	first, err := c.get(now, fetch)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	second, err := c.get(now.Add(time.Minute), fetch)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if calls != 1 {
		t.Fatalf("expected the second call inside the TTL to reuse the cache, got %d calls", calls)
	}
	if len(first) != 1 || len(second) != 1 || first[0].ID != second[0].ID {
		t.Fatalf("expected the cached result to be returned unchanged, got %+v and %+v", first, second)
	}

	third, err := c.get(now.Add(assistantModelsCacheTTL+time.Minute), fetch)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if calls != 2 {
		t.Fatalf("expected a refetch once the TTL has passed, got %d calls", calls)
	}
	if len(third) != 1 || third[0].ID != "m2" {
		t.Fatalf("expected the refreshed catalogue, got %+v", third)
	}
}

func TestAssistantModelsCache_ConcurrentMissesMakeOneCall(t *testing.T) {
	c := &assistantModelsCache{}
	var mu sync.Mutex
	calls := 0
	release := make(chan struct{})
	fetch := func() ([]assistantModelOption, error) {
		mu.Lock()
		calls++
		mu.Unlock()
		<-release
		return []assistantModelOption{{ID: "m"}}, nil
	}

	const n = 5
	var launched sync.WaitGroup
	launched.Add(n)
	var wg sync.WaitGroup
	results := make([][]assistantModelOption, n)
	errs := make([]error, n)
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			launched.Done()
			results[i], errs[i] = c.get(time.Now(), fetch)
		}(i)
	}
	launched.Wait()
	close(release)
	wg.Wait()

	mu.Lock()
	got := calls
	mu.Unlock()
	if got != 1 {
		t.Fatalf("expected exactly one upstream fetch for concurrent misses, got %d", got)
	}
	for i, err := range errs {
		if err != nil {
			t.Fatalf("goroutine %d: %v", i, err)
		}
		if len(results[i]) != 1 || results[i][0].ID != "m" {
			t.Fatalf("goroutine %d: unexpected result %+v", i, results[i])
		}
	}
}

func TestAssistantModelsCache_RefreshErrorSurfacesAndDoesNotPoisonCache(t *testing.T) {
	c := &assistantModelsCache{}
	now := time.Now()
	wantErr := errors.New("upstream boom")

	if _, err := c.get(now, func() ([]assistantModelOption, error) { return nil, wantErr }); err != wantErr {
		t.Fatalf("expected the refresh error to surface, got %v", err)
	}

	got, err := c.get(now.Add(time.Second), func() ([]assistantModelOption, error) {
		return []assistantModelOption{{ID: "ok"}}, nil
	})
	if err != nil {
		t.Fatalf("expected a later call to retry rather than being stuck behind the error, got %v", err)
	}
	if len(got) != 1 || got[0].ID != "ok" {
		t.Fatalf("unexpected result: %+v", got)
	}
}

// ── conversation CRUD ───────────────────────────────────────────────────

func TestCreateAssistantConversationHandler_BlankTitleDefaults(t *testing.T) {
	withTestAssistantStore(t)

	c, rec := newAssistantEchoContext(http.MethodPost, "/api/assistant/conversations", `{}`, "")
	if err := createAssistantConversationHandler(c); err != nil {
		t.Fatalf("handler returned error: %v", err)
	}
	if rec.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", rec.Code, rec.Body.String())
	}

	var conv assistantConversation
	if err := json.Unmarshal(rec.Body.Bytes(), &conv); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if conv.Title != "New conversation" {
		t.Errorf("expected default title, got %q", conv.Title)
	}
}

func TestListAssistantConversationsHandler_WrapsInConversationsKey(t *testing.T) {
	store := withTestAssistantStore(t)
	if _, err := store.CreateConversation("Whitsundays"); err != nil {
		t.Fatalf("CreateConversation: %v", err)
	}

	c, rec := newAssistantEchoContext(http.MethodGet, "/api/assistant/conversations", "", "")
	if err := listAssistantConversationsHandler(c); err != nil {
		t.Fatalf("handler returned error: %v", err)
	}

	var resp struct {
		Conversations []assistantConversation `json:"conversations"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(resp.Conversations) != 1 || resp.Conversations[0].Title != "Whitsundays" {
		t.Fatalf("unexpected conversations: %+v", resp.Conversations)
	}
}

func TestGetAssistantConversationHandler_UnknownIDReturns404(t *testing.T) {
	withTestAssistantStore(t)

	c, rec := newAssistantEchoContext(http.MethodGet, "/api/assistant/conversations/nope", "", "nope")
	if err := getAssistantConversationHandler(c); err != nil {
		t.Fatalf("handler returned error: %v", err)
	}
	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestGetAssistantConversationHandler_ReturnsConversationAndMessages(t *testing.T) {
	store := withTestAssistantStore(t)
	conv, err := store.CreateConversation("Whitsundays")
	if err != nil {
		t.Fatalf("CreateConversation: %v", err)
	}
	if _, err := store.AppendMessage(assistantMessage{ConversationID: conv.ID, Role: "user", Content: "hi"}); err != nil {
		t.Fatalf("AppendMessage: %v", err)
	}

	c, rec := newAssistantEchoContext(http.MethodGet, "/api/assistant/conversations/"+conv.ID, "", conv.ID)
	if err := getAssistantConversationHandler(c); err != nil {
		t.Fatalf("handler returned error: %v", err)
	}
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	var resp struct {
		Conversation assistantConversation `json:"conversation"`
		Messages     []assistantMessage    `json:"messages"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if resp.Conversation.ID != conv.ID {
		t.Fatalf("expected conversation id %q, got %q", conv.ID, resp.Conversation.ID)
	}
	if len(resp.Messages) != 1 || resp.Messages[0].Content != "hi" {
		t.Fatalf("unexpected messages: %+v", resp.Messages)
	}
}

func TestDeleteAssistantConversationHandler_UnknownIDReturns404(t *testing.T) {
	withTestAssistantStore(t)

	c, rec := newAssistantEchoContext(http.MethodDelete, "/api/assistant/conversations/nope", "", "nope")
	if err := deleteAssistantConversationHandler(c); err != nil {
		t.Fatalf("handler returned error: %v", err)
	}
	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestDeleteAssistantConversationHandler_KnownIDReturns204(t *testing.T) {
	store := withTestAssistantStore(t)
	conv, err := store.CreateConversation("")
	if err != nil {
		t.Fatalf("CreateConversation: %v", err)
	}

	c, rec := newAssistantEchoContext(http.MethodDelete, "/api/assistant/conversations/"+conv.ID, "", conv.ID)
	if err := deleteAssistantConversationHandler(c); err != nil {
		t.Fatalf("handler returned error: %v", err)
	}
	if rec.Code != http.StatusNoContent {
		t.Fatalf("expected 204, got %d: %s", rec.Code, rec.Body.String())
	}
}

// ── POST /api/assistant/conversations/:id/messages ─────────────────────

func TestTrimmedAssistantScreenField_TrimsAndCapsAt80Runes(t *testing.T) {
	if got := trimmedAssistantScreenField("  forecast  "); got != "forecast" {
		t.Fatalf("expected trimming, got %q", got)
	}
	long := strings.Repeat("a", 200)
	got := trimmedAssistantScreenField(long)
	if len([]rune(got)) != assistantScreenFieldMaxRunes {
		t.Fatalf("expected the field capped at %d runes, got %d", assistantScreenFieldMaxRunes, len([]rune(got)))
	}
	if got != strings.Repeat("a", assistantScreenFieldMaxRunes) {
		t.Fatalf("expected the capped field to be a prefix of the input, got %q", got)
	}
}

func TestPostAssistantMessageHandler_BlankContentReturns400(t *testing.T) {
	c, rec := newAssistantEchoContext(http.MethodPost, "/api/assistant/conversations/x/messages", `{"content":"   "}`, "x")
	if err := postAssistantMessageHandler(c); err != nil {
		t.Fatalf("handler returned error: %v", err)
	}
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestPostAssistantMessageHandler_TooLongContentReturns400(t *testing.T) {
	tooLong := strings.Repeat("a", assistantMaxMessageChars+1)
	body, err := json.Marshal(map[string]string{"content": tooLong})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	c, rec := newAssistantEchoContext(http.MethodPost, "/api/assistant/conversations/x/messages", string(body), "x")
	if err := postAssistantMessageHandler(c); err != nil {
		t.Fatalf("handler returned error: %v", err)
	}
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestPostAssistantMessageHandler_UnconfiguredReturns503JSONNotEventStream(t *testing.T) {
	withTestSecretsStore(t) // no OPENROUTER_API_KEY set
	store := withTestAssistantStore(t)
	t.Setenv("SETTINGS_FILE", writeAssistantSettingsFixture(t, false, "openai/gpt-4o"))

	conv, err := store.CreateConversation("")
	if err != nil {
		t.Fatalf("CreateConversation: %v", err)
	}

	c, rec := newAssistantEchoContext(http.MethodPost, "/api/assistant/conversations/"+conv.ID+"/messages", `{"content":"hi"}`, conv.ID)
	if err := postAssistantMessageHandler(c); err != nil {
		t.Fatalf("handler returned error: %v", err)
	}
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("expected 503, got %d: %s", rec.Code, rec.Body.String())
	}
	if ct := rec.Header().Get(echo.HeaderContentType); !strings.Contains(ct, "json") {
		t.Fatalf("expected a JSON content type (not text/event-stream), got %q", ct)
	}

	var resp map[string]string
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if !strings.Contains(resp["error"], "Settings → Assistant") {
		t.Fatalf("expected the error to name Settings → Assistant, got %+v", resp)
	}

	messages, err := store.ListMessages(conv.ID)
	if err != nil {
		t.Fatalf("ListMessages: %v", err)
	}
	if len(messages) != 0 {
		t.Fatalf("expected no rows persisted when unconfigured, got %+v", messages)
	}
}

func TestPostAssistantMessageHandler_UnknownConversationReturns404(t *testing.T) {
	store := withTestSecretsStore(t)
	if err := store.Set("OPENROUTER_API_KEY", "sk-test"); err != nil {
		t.Fatalf("Set: %v", err)
	}
	withTestAssistantStore(t)
	t.Setenv("SETTINGS_FILE", writeAssistantSettingsFixture(t, true, "openai/gpt-4o"))

	c, rec := newAssistantEchoContext(http.MethodPost, "/api/assistant/conversations/nope/messages", `{"content":"hi"}`, "nope")
	if err := postAssistantMessageHandler(c); err != nil {
		t.Fatalf("handler returned error: %v", err)
	}
	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestPostAssistantMessageHandler_SuccessStreamsSSEAndPersistsRows(t *testing.T) {
	secrets := withTestSecretsStore(t)
	if err := secrets.Set("OPENROUTER_API_KEY", "sk-test"); err != nil {
		t.Fatalf("Set: %v", err)
	}
	store := withTestAssistantStore(t)
	t.Setenv("SETTINGS_FILE", writeAssistantSettingsFixture(t, true, "openai/gpt-4o"))

	conv, err := store.CreateConversation("")
	if err != nil {
		t.Fatalf("CreateConversation: %v", err)
	}

	var runner *fakeAssistantRunner
	swapAssistantRunner(t, func(apiKey, model, settingsPath string, autoRouter assistantAutoRouterOptions, emit assistantEmitter) assistantRunnerFace {
		runner = &fakeAssistantRunner{emit: emit, reply: assistantReply{
			Content:          "Tongue Bay first, on the rising tide.",
			Model:            "openai/gpt-4o",
			PromptTokens:     120,
			CompletionTokens: 45,
			CostUSD:          0.0184,
			ToolRounds:       2,
		}}
		return runner
	})

	c, rec := newAssistantEchoContext(http.MethodPost, "/api/assistant/conversations/"+conv.ID+"/messages",
		`{"content":"Tongue Bay or Blue Pearl Bay first?"}`, conv.ID)
	if err := postAssistantMessageHandler(c); err != nil {
		t.Fatalf("handler returned error: %v", err)
	}

	if ct := rec.Header().Get(echo.HeaderContentType); ct != "text/event-stream" {
		t.Fatalf("expected text/event-stream, got %q", ct)
	}

	body := rec.Body.String()
	if !strings.Contains(body, `event: status`+"\n"+`data: {"text":`) {
		t.Fatalf("expected a status frame, got:\n%s", body)
	}

	data := extractSSEEventData(t, body, "message")
	var frame struct {
		Message      assistantMessage      `json:"message"`
		Conversation assistantConversation `json:"conversation"`
	}
	if err := json.Unmarshal([]byte(data), &frame); err != nil {
		t.Fatalf("unmarshal message frame: %v", err)
	}
	if frame.Message.Role != "assistant" {
		t.Fatalf("expected assistant role, got %q", frame.Message.Role)
	}
	if frame.Message.CostUSD != 0.0184 {
		t.Fatalf("expected cost to round-trip, got %v", frame.Message.CostUSD)
	}
	if frame.Message.Model != "openai/gpt-4o" {
		t.Fatalf("expected model to round-trip, got %q", frame.Message.Model)
	}
	if frame.Message.ToolRounds != 2 {
		t.Fatalf("expected tool_rounds to round-trip, got %d", frame.Message.ToolRounds)
	}
	if frame.Conversation.ID != conv.ID {
		t.Fatalf("expected the conversation id to round-trip, got %q", frame.Conversation.ID)
	}

	messages, err := store.ListMessages(conv.ID)
	if err != nil {
		t.Fatalf("ListMessages: %v", err)
	}
	if len(messages) != 2 {
		t.Fatalf("expected 2 persisted rows (user + assistant), got %d: %+v", len(messages), messages)
	}
	if messages[0].Role != "user" || messages[0].Content != "Tongue Bay or Blue Pearl Bay first?" {
		t.Fatalf("unexpected first row: %+v", messages[0])
	}
	if messages[1].Role != "assistant" || messages[1].Content != "Tongue Bay first, on the rising tide." {
		t.Fatalf("unexpected second row: %+v", messages[1])
	}

	updated, ok, err := store.GetConversation(conv.ID)
	if err != nil || !ok {
		t.Fatalf("GetConversation: ok=%v err=%v", ok, err)
	}
	if updated.Title != "Tongue Bay or Blue Pearl Bay first?" {
		t.Fatalf("expected the title to derive from the first user message, got %q", updated.Title)
	}

	// A POST with no spoken/screen fields (this test's body is bare
	// {"content": ...}) must build byte-for-byte the same system prompt it
	// did before those fields existed - neither addition present.
	if runner == nil || runner.gotSystem == "" {
		t.Fatalf("expected the runner to have recorded a system prompt")
	}
	if strings.Contains(runner.gotSystem, "Spoken summary") {
		t.Fatalf("expected no spoken-summary instruction with no spoken field, got:\n%s", runner.gotSystem)
	}
	if strings.Contains(runner.gotSystem, "The operator is looking at") {
		t.Fatalf("expected no screen sentence with no screen field, got:\n%s", runner.gotSystem)
	}
}

// TestPostAssistantMessageHandler_StreamsDeltaEventsBeforeMessage proves
// the handler forwards whatever "delta" events the runner emits (backend
// token streaming) onto the same SSE response, ahead of its own "message"
// event - postAssistantMessageHandler itself has no streaming logic of its
// own to test here, only that it does not buffer or reorder what run()
// hands it, the same way TestPostAssistantMessageHandler_
// SuccessStreamsSSEAndPersistsRows already checks for "status".
func TestPostAssistantMessageHandler_StreamsDeltaEventsBeforeMessage(t *testing.T) {
	secrets := withTestSecretsStore(t)
	if err := secrets.Set("OPENROUTER_API_KEY", "sk-test"); err != nil {
		t.Fatalf("Set: %v", err)
	}
	store := withTestAssistantStore(t)
	t.Setenv("SETTINGS_FILE", writeAssistantSettingsFixture(t, true, "openai/gpt-4o"))

	conv, err := store.CreateConversation("")
	if err != nil {
		t.Fatalf("CreateConversation: %v", err)
	}

	swapAssistantRunner(t, func(apiKey, model, settingsPath string, autoRouter assistantAutoRouterOptions, emit assistantEmitter) assistantRunnerFace {
		return &fakeAssistantRunner{
			emit:   emit,
			deltas: []string{"Tongue Bay ", "first, on the rising tide."},
			reply: assistantReply{
				Content: "Tongue Bay first, on the rising tide.",
				Model:   "openai/gpt-4o",
			},
		}
	})

	c, rec := newAssistantEchoContext(http.MethodPost, "/api/assistant/conversations/"+conv.ID+"/messages",
		`{"content":"Tongue Bay or Blue Pearl Bay first?"}`, conv.ID)
	if err := postAssistantMessageHandler(c); err != nil {
		t.Fatalf("handler returned error: %v", err)
	}

	body := rec.Body.String()
	deltaMarkerIdx := strings.Index(body, "event: delta\n")
	messageMarkerIdx := strings.Index(body, "event: message\n")
	if deltaMarkerIdx == -1 {
		t.Fatalf("expected at least one delta frame, got:\n%s", body)
	}
	if messageMarkerIdx == -1 {
		t.Fatalf("expected a message frame, got:\n%s", body)
	}
	if deltaMarkerIdx >= messageMarkerIdx {
		t.Fatalf("expected delta frames to precede the message frame, got:\n%s", body)
	}

	firstDelta := extractSSEEventData(t, body, "delta")
	var frame struct {
		Text string `json:"text"`
	}
	if err := json.Unmarshal([]byte(firstDelta), &frame); err != nil {
		t.Fatalf("unmarshal delta frame: %v", err)
	}
	if frame.Text != "Tongue Bay " {
		t.Fatalf("expected the first delta's text to round-trip, got %q", frame.Text)
	}

	data := extractSSEEventData(t, body, "message")
	var msgFrame struct {
		Message assistantMessage `json:"message"`
	}
	if err := json.Unmarshal([]byte(data), &msgFrame); err != nil {
		t.Fatalf("unmarshal message frame: %v", err)
	}
	if msgFrame.Message.Content != "Tongue Bay first, on the rising tide." {
		t.Fatalf("expected the message event to carry the full final content, got %q", msgFrame.Message.Content)
	}
}

// TestPostAssistantMessageHandler_SpokenAndScreenReachSystemPrompt proves
// the mate-voice-assistant plan's spoken/screen POST fields actually reach
// the system prompt for that turn: fakeAssistantRunner records the system
// string run() was called with, so this checks it directly rather than
// re-deriving buildAssistantSystemPrompt's own rendering (already covered in
// assistant_prompt_test.go).
func TestPostAssistantMessageHandler_UsesSpokenSummaryAsConversationTitle(t *testing.T) {
	secrets := withTestSecretsStore(t)
	if err := secrets.Set("OPENROUTER_API_KEY", "sk-test"); err != nil {
		t.Fatalf("Set: %v", err)
	}
	store := withTestAssistantStore(t)
	t.Setenv("SETTINGS_FILE", writeAssistantSettingsFixture(t, true, "openai/gpt-4o"))

	conv, err := store.CreateConversation("")
	if err != nil {
		t.Fatalf("CreateConversation: %v", err)
	}

	var runner *fakeAssistantRunner
	swapAssistantRunner(t, func(apiKey, model, settingsPath string, autoRouter assistantAutoRouterOptions, emit assistantEmitter) assistantRunnerFace {
		runner = &fakeAssistantRunner{emit: emit, reply: assistantReply{
			Content: "## Spoken summary\n\nGloucester Island Anchorages\n\n## Passage plan\n\nWe should favour the north side.",
			Model:   "openai/gpt-4o",
		}}
		return runner
	})

	body := `{"content":"What are recommended anchorages around Gloucester Island?"}`
	c, rec := newAssistantEchoContext(http.MethodPost, "/api/assistant/conversations/"+conv.ID+"/messages", body, conv.ID)
	if err := postAssistantMessageHandler(c); err != nil {
		t.Fatalf("handler returned error: %v", err)
	}
	if ct := rec.Header().Get(echo.HeaderContentType); ct != "text/event-stream" {
		t.Fatalf("expected text/event-stream, got %q: %s", ct, rec.Body.String())
	}

	updated, ok, err := store.GetConversation(conv.ID)
	if err != nil || !ok {
		t.Fatalf("GetConversation: ok=%v err=%v", ok, err)
	}
	if updated.Title != "Gloucester Island Anchorages" {
		t.Fatalf("expected AI spoken summary title, got %q", updated.Title)
	}
}

func TestPostAssistantMessageHandler_SpokenAndScreenReachSystemPrompt(t *testing.T) {
	secrets := withTestSecretsStore(t)
	if err := secrets.Set("OPENROUTER_API_KEY", "sk-test"); err != nil {
		t.Fatalf("Set: %v", err)
	}
	store := withTestAssistantStore(t)
	t.Setenv("SETTINGS_FILE", writeAssistantSettingsFixture(t, true, "openai/gpt-4o"))

	conv, err := store.CreateConversation("")
	if err != nil {
		t.Fatalf("CreateConversation: %v", err)
	}

	var runner *fakeAssistantRunner
	swapAssistantRunner(t, func(apiKey, model, settingsPath string, autoRouter assistantAutoRouterOptions, emit assistantEmitter) assistantRunnerFace {
		runner = &fakeAssistantRunner{emit: emit, reply: assistantReply{
			Content: "The upper atmosphere chart is the 500mb height and vorticity pattern.",
			Model:   "openai/gpt-4o",
		}}
		return runner
	})

	body := `{"content":"How does the upper atmosphere graph work?","spoken":true,"screen":{"panel":"forecast"}}`
	c, rec := newAssistantEchoContext(http.MethodPost, "/api/assistant/conversations/"+conv.ID+"/messages", body, conv.ID)
	if err := postAssistantMessageHandler(c); err != nil {
		t.Fatalf("handler returned error: %v", err)
	}
	if ct := rec.Header().Get(echo.HeaderContentType); ct != "text/event-stream" {
		t.Fatalf("expected text/event-stream (i.e. the request reached the runner), got %q: %s", ct, rec.Body.String())
	}

	if runner == nil || runner.gotSystem == "" {
		t.Fatalf("expected the runner to have recorded a system prompt")
	}
	if !strings.Contains(runner.gotSystem, "## Spoken summary") {
		t.Fatalf("expected the system prompt to carry the spoken-summary instruction, got:\n%s", runner.gotSystem)
	}
	if !strings.Contains(runner.gotSystem, "Forecast panel") {
		t.Fatalf("expected the system prompt to name the Forecast panel, got:\n%s", runner.gotSystem)
	}
}

func TestPostAssistantMessageHandler_RunnerErrorEmitsErrorEventAndPersistsOnlyUserRow(t *testing.T) {
	secrets := withTestSecretsStore(t)
	if err := secrets.Set("OPENROUTER_API_KEY", "sk-test"); err != nil {
		t.Fatalf("Set: %v", err)
	}
	store := withTestAssistantStore(t)
	t.Setenv("SETTINGS_FILE", writeAssistantSettingsFixture(t, true, "openai/gpt-4o"))

	conv, err := store.CreateConversation("")
	if err != nil {
		t.Fatalf("CreateConversation: %v", err)
	}

	swapAssistantRunner(t, func(apiKey, model, settingsPath string, autoRouter assistantAutoRouterOptions, emit assistantEmitter) assistantRunnerFace {
		return &fakeAssistantRunner{emit: emit, err: fmt.Errorf("openrouter status 401: invalid api key")}
	})

	c, rec := newAssistantEchoContext(http.MethodPost, "/api/assistant/conversations/"+conv.ID+"/messages", `{"content":"hi"}`, conv.ID)
	if err := postAssistantMessageHandler(c); err != nil {
		t.Fatalf("handler returned error: %v", err)
	}

	body := rec.Body.String()
	data := extractSSEEventData(t, body, "error")
	var errFrame struct {
		Error string `json:"error"`
	}
	if err := json.Unmarshal([]byte(data), &errFrame); err != nil {
		t.Fatalf("unmarshal error frame: %v", err)
	}
	if !strings.Contains(errFrame.Error, "invalid api key") {
		t.Fatalf("expected the upstream error text, got %q", errFrame.Error)
	}

	messages, err := store.ListMessages(conv.ID)
	if err != nil {
		t.Fatalf("ListMessages: %v", err)
	}
	if len(messages) != 1 || messages[0].Role != "user" {
		t.Fatalf("expected only the user row to persist on a runner error, got %+v", messages)
	}
}

func TestPostAssistantMessageHandler_SecondRequestWhileFirstInFlightReturns409(t *testing.T) {
	secrets := withTestSecretsStore(t)
	if err := secrets.Set("OPENROUTER_API_KEY", "sk-test"); err != nil {
		t.Fatalf("Set: %v", err)
	}
	store := withTestAssistantStore(t)
	t.Setenv("SETTINGS_FILE", writeAssistantSettingsFixture(t, true, "openai/gpt-4o"))

	conv, err := store.CreateConversation("")
	if err != nil {
		t.Fatalf("CreateConversation: %v", err)
	}

	started := make(chan struct{})
	proceed := make(chan struct{})
	swapAssistantRunner(t, func(apiKey, model, settingsPath string, autoRouter assistantAutoRouterOptions, emit assistantEmitter) assistantRunnerFace {
		return &blockingAssistantRunner{started: started, proceed: proceed, reply: assistantReply{Content: "ok"}}
	})

	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		c1, _ := newAssistantEchoContext(http.MethodPost, "/api/assistant/conversations/"+conv.ID+"/messages", `{"content":"first"}`, conv.ID)
		if err := postAssistantMessageHandler(c1); err != nil {
			t.Errorf("first handler call returned error: %v", err)
		}
	}()

	<-started // the first request now holds the in-flight guard

	c2, rec2 := newAssistantEchoContext(http.MethodPost, "/api/assistant/conversations/"+conv.ID+"/messages", `{"content":"second"}`, conv.ID)
	if err := postAssistantMessageHandler(c2); err != nil {
		t.Fatalf("second handler call returned error: %v", err)
	}
	if rec2.Code != http.StatusConflict {
		t.Fatalf("expected 409, got %d: %s", rec2.Code, rec2.Body.String())
	}

	close(proceed)
	wg.Wait()
}

// ── route tiers ─────────────────────────────────────────────────────────

func TestBuildAPIRoutes_AssistantRoutesHaveExpectedTiers(t *testing.T) {
	sessions := newTestSessionStore(t)
	want := map[string]apiTier{
		"GET /api/assistant/status":                      tierRead,
		"GET /api/assistant/models":                      tierAdmin,
		"GET /api/assistant/conversations":               tierRead,
		"GET /api/assistant/conversations/:id":           tierRead,
		"POST /api/assistant/conversations":              tierWrite,
		"DELETE /api/assistant/conversations/:id":        tierWrite,
		"POST /api/assistant/conversations/:id/messages": tierWrite,
	}

	got := map[string]apiTier{}
	for _, route := range buildAPIRoutes(sessions, newWorldImageryHTTPClient()) {
		got[route.Method+" "+route.Path] = route.Tier
	}

	for key, wantTier := range want {
		gotTier, ok := got[key]
		if !ok {
			t.Fatalf("route %s missing from the production route table", key)
		}
		if gotTier != wantTier {
			t.Fatalf("route %s tier = %v, want %v", key, gotTier, wantTier)
		}
	}
}
