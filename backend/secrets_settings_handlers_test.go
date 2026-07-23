package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/labstack/echo/v4"
)

// withTestSecretsStore points globalSecretsStore at a fresh t.TempDir()-backed
// store for the duration of the test, restoring the prior value afterwards.
// The secrets settings handlers read/write globalSecretsStore directly (same
// package-level-var pattern as globalNearbyContactStore/globalTileCache), so
// tests must swap it in rather than constructing the echo.HandlerFunc with a
// store argument.
func withTestSecretsStore(t *testing.T) *secretsStore {
	t.Helper()
	store := newTestSecretsStore(t)
	prev := globalSecretsStore
	globalSecretsStore = store
	t.Cleanup(func() { globalSecretsStore = prev })
	return store
}

func TestGetSecretsSettingsHandler_ReflectsStoreState(t *testing.T) {
	store := withTestSecretsStore(t)
	if err := store.Set("SIGNALK_USERNAME", "admin"); err != nil {
		t.Fatalf("Set: %v", err)
	}

	e := echo.New()
	req := httptest.NewRequest(http.MethodGet, "/api/settings/secrets", nil)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)

	if err := getSecretsSettingsHandler(c); err != nil {
		t.Fatalf("handler returned error: %v", err)
	}
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	var resp map[string]bool
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}
	if len(resp) != len(knownSecretKeys) {
		t.Fatalf("expected %d keys in response, got %d: %+v", len(knownSecretKeys), len(resp), resp)
	}
	if !resp["SIGNALK_USERNAME"] {
		t.Errorf("expected SIGNALK_USERNAME=true, got %+v", resp)
	}
	if resp["STORMGLASS_API_KEY"] {
		t.Errorf("expected STORMGLASS_API_KEY=false, got %+v", resp)
	}
}

func TestUpdateSecretsSettingsHandler_SetsValueAndReturnsNewState(t *testing.T) {
	withTestSecretsStore(t)

	e := echo.New()
	body := `{"SIGNALK_USERNAME":"admin","SIGNALK_PASSWORD":"hunter2"}`
	req := httptest.NewRequest(http.MethodPost, "/api/settings/secrets", strings.NewReader(body))
	req.Header.Set(echo.HeaderContentType, echo.MIMEApplicationJSON)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)

	if err := updateSecretsSettingsHandler(c); err != nil {
		t.Fatalf("handler returned error: %v", err)
	}
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	var resp map[string]bool
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}
	if !resp["SIGNALK_USERNAME"] || !resp["SIGNALK_PASSWORD"] {
		t.Errorf("expected both keys to be reflected as set, got %+v", resp)
	}

	value, ok, err := globalSecretsStore.Get("SIGNALK_USERNAME")
	if err != nil || !ok || value != "admin" {
		t.Fatalf("expected SIGNALK_USERNAME to be persisted, got value=%q ok=%v err=%v", value, ok, err)
	}
}

func TestUpdateSecretsSettingsHandler_UnknownKeyReturns400AndWritesNothing(t *testing.T) {
	store := withTestSecretsStore(t)

	e := echo.New()
	body := `{"SIGNALK_USERNAME":"admin","NOT_A_REAL_SECRET":"x"}`
	req := httptest.NewRequest(http.MethodPost, "/api/settings/secrets", strings.NewReader(body))
	req.Header.Set(echo.HeaderContentType, echo.MIMEApplicationJSON)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)

	if err := updateSecretsSettingsHandler(c); err != nil {
		t.Fatalf("handler returned error: %v", err)
	}
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", rec.Code, rec.Body.String())
	}

	var errResp map[string]string
	if err := json.Unmarshal(rec.Body.Bytes(), &errResp); err != nil {
		t.Fatalf("unmarshal error response: %v", err)
	}
	if !strings.Contains(errResp["error"], "NOT_A_REAL_SECRET") {
		t.Errorf("expected error message to name the unknown key, got %+v", errResp)
	}

	// Validate-before-write: SIGNALK_USERNAME must NOT have been persisted
	// even though it's a valid key, since the request as a whole was invalid.
	if has, err := store.Has("SIGNALK_USERNAME"); err != nil || has {
		t.Fatalf("expected no keys written when the request contains an unknown key, but SIGNALK_USERNAME has=%v err=%v", has, err)
	}
}

func TestUpdateSecretsSettingsHandler_EmptyStringClearsKey(t *testing.T) {
	store := withTestSecretsStore(t)
	if err := store.Set("SIGNALK_USERNAME", "admin"); err != nil {
		t.Fatalf("Set: %v", err)
	}

	e := echo.New()
	body := `{"SIGNALK_USERNAME":""}`
	req := httptest.NewRequest(http.MethodPost, "/api/settings/secrets", strings.NewReader(body))
	req.Header.Set(echo.HeaderContentType, echo.MIMEApplicationJSON)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)

	if err := updateSecretsSettingsHandler(c); err != nil {
		t.Fatalf("handler returned error: %v", err)
	}
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	if has, err := store.Has("SIGNALK_USERNAME"); err != nil || has {
		t.Fatalf("expected SIGNALK_USERNAME to be cleared, has=%v err=%v", has, err)
	}
}

func TestImportEnvSecretsHandler_ReturnsImportedKeys(t *testing.T) {
	withTestSecretsStore(t)

	for _, key := range knownSecretKeys {
		os.Unsetenv(key)
	}
	t.Setenv("SIGNALK_USERNAME", "admin")

	e := echo.New()
	req := httptest.NewRequest(http.MethodPost, "/api/settings/secrets/import-env", nil)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)

	if err := importEnvSecretsHandler(c); err != nil {
		t.Fatalf("handler returned error: %v", err)
	}
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	var resp struct {
		Imported []string `json:"imported"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}
	if len(resp.Imported) != 1 || resp.Imported[0] != "SIGNALK_USERNAME" {
		t.Fatalf("expected imported=[SIGNALK_USERNAME], got %v", resp.Imported)
	}
}

func TestImportEnvSecretsHandler_NothingToImportReturns200WithEmptyArray(t *testing.T) {
	withTestSecretsStore(t)
	for _, key := range knownSecretKeys {
		os.Unsetenv(key)
	}

	e := echo.New()
	req := httptest.NewRequest(http.MethodPost, "/api/settings/secrets/import-env", nil)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)

	if err := importEnvSecretsHandler(c); err != nil {
		t.Fatalf("handler returned error: %v", err)
	}
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 even when nothing to import, got %d: %s", rec.Code, rec.Body.String())
	}

	var resp struct {
		Imported []string `json:"imported"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}
	if resp.Imported == nil {
		t.Fatalf("expected imported to be an empty array, not null, got nil")
	}
	if len(resp.Imported) != 0 {
		t.Fatalf("expected no imported keys, got %v", resp.Imported)
	}
}
