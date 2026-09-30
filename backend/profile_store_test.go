package main

import (
	"bytes"
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/labstack/echo/v4"
)

func mustProfile(t *testing.T, doc string) engineProfile {
	t.Helper()
	p, err := parseProfileDocument([]byte(doc))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	return p
}

func TestProfileStoreTxFunctions(t *testing.T) {
	store := newTestProfileStore(t)
	p := mustProfile(t, goodProfile)

	err := store.inTx(func(tx *sql.Tx) error {
		if err := insertProfileTx(tx, p, profileBasedOn{Source: "helmcentral", ID: "x", SHA256: "abc"}); err != nil {
			return err
		}
		if err := insertProfileTx(tx, p, profileBasedOn{}); !errors.Is(err, errProfileExists) {
			t.Fatalf("expected errProfileExists, got %v", err)
		}
		got, basedOn, err := getProfileTx(tx, p.ID)
		if err != nil || got.Name != p.Name || basedOn.SHA256 != "abc" {
			t.Fatalf("get: %+v %+v %v", got, basedOn, err)
		}
		p.Name = "Renamed"
		if err := updateProfileTx(tx, p); err != nil {
			return err
		}
		_, basedOn, _ = getProfileTx(tx, p.ID)
		if basedOn.ID != "x" {
			t.Fatalf("update must keep based_on, got %+v", basedOn)
		}
		if err := updateProfileTx(tx, engineProfile{ID: "nope"}); !errors.Is(err, errProfileNotFound) {
			t.Fatalf("expected errProfileNotFound, got %v", err)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}

	// A rolled-back transaction leaves nothing behind.
	rollback := errors.New("stop")
	err = store.inTx(func(tx *sql.Tx) error {
		if err := deleteProfileTx(tx, p.ID); err != nil {
			return err
		}
		return rollback
	})
	if !errors.Is(err, rollback) {
		t.Fatalf("expected the rollback error, got %v", err)
	}
	if n, _ := store.count(); n != 1 {
		t.Fatalf("expected the delete to roll back, count %d", n)
	}
	if err := store.inTx(func(tx *sql.Tx) error { return deleteProfileTx(tx, "nope") }); !errors.Is(err, errProfileNotFound) {
		t.Fatalf("expected errProfileNotFound, got %v", err)
	}
}

func TestEmbeddedCatalogueIsValid(t *testing.T) {
	entries, err := profileCatalogue()
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) < 3 {
		t.Fatalf("expected the three shipped profiles, got %d", len(entries))
	}
	for _, e := range entries {
		if e.Source != "helmcentral" || len(e.SHA256) != 64 {
			t.Errorf("bad entry %+v", e)
		}
	}
}

func TestInvalidCatalogueIsAnError(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "profile_catalogue"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "profile_catalogue", "bad.json"), []byte(`{"id":"b"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := loadProfileCatalogue(os.DirFS(dir)); err == nil {
		t.Fatal("an invalid catalogue entry must be an error")
	}
}

func catalogueRequest(t *testing.T, method, path, id, body string) *httptest.ResponseRecorder {
	t.Helper()
	e := echo.New()
	req := httptest.NewRequest(method, path, bytes.NewBufferString(body))
	req.Header.Set(echo.HeaderContentType, echo.MIMEApplicationJSON)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)
	if id != "" {
		c.SetParamNames("id")
		c.SetParamValues(id)
	}
	var err error
	if method == http.MethodGet {
		err = profileCatalogueHandler(c)
	} else {
		err = copyCatalogueProfileHandler(c)
	}
	if err != nil {
		t.Fatalf("handler: %v", err)
	}
	return rec
}

func TestCatalogueListAndCopy(t *testing.T) {
	newTestProfileStore(t)
	loadEngineProfiles()

	rec := catalogueRequest(t, http.MethodGet, "/api/equipment-profiles/catalogue", "", "")
	var list struct {
		Entries []catalogueListItem `json:"entries"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &list); err != nil || len(list.Entries) < 3 {
		t.Fatalf("list: %v %s", err, rec.Body.String())
	}
	first := list.Entries[0]
	if first.Added || first.Source != "helmcentral" || first.SHA256 == "" {
		t.Fatalf("unexpected entry %+v", first)
	}

	if rec := catalogueRequest(t, http.MethodPost, "/x", first.ID, ""); rec.Code != http.StatusCreated {
		t.Fatalf("copy: %d %s", rec.Code, rec.Body.String())
	}
	if rec := catalogueRequest(t, http.MethodPost, "/x", first.ID, ""); rec.Code != http.StatusConflict {
		t.Fatalf("second copy must conflict, got %d", rec.Code)
	}
	if rec := catalogueRequest(t, http.MethodPost, "/x", first.ID, `{"id":"my-copy"}`); rec.Code != http.StatusCreated {
		t.Fatalf("copy under new id: %d %s", rec.Code, rec.Body.String())
	}
	if rec := catalogueRequest(t, http.MethodPost, "/x", first.ID, `{"id":"Bad Id"}`); rec.Code != http.StatusBadRequest {
		t.Fatalf("bad id: %d", rec.Code)
	}
	if rec := catalogueRequest(t, http.MethodPost, "/x", "no-such", ""); rec.Code != http.StatusNotFound {
		t.Fatalf("missing entry: %d", rec.Code)
	}

	err := globalProfileStore.inTx(func(tx *sql.Tx) error {
		_, basedOn, err := getProfileTx(tx, "my-copy")
		if basedOn.Source != "helmcentral" || basedOn.ID != first.ID || basedOn.SHA256 != first.SHA256 {
			t.Fatalf("based_on not recorded: %+v", basedOn)
		}
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	profiles, _ := engineProfiles()
	if len(profiles) != 2 {
		t.Fatalf("cache not refreshed: %d", len(profiles))
	}

	rec = catalogueRequest(t, http.MethodGet, "/api/equipment-profiles/catalogue", "", "")
	_ = json.Unmarshal(rec.Body.Bytes(), &list)
	for _, e := range list.Entries {
		if e.ID == first.ID && !e.Added {
			t.Fatal("copied entry must report added")
		}
	}
}

func writeLegacyFile(t *testing.T, dir, name, body string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestConvertProfileRules(t *testing.T) {
	store := newTestProfileStore(t)
	dir := t.TempDir()
	writeLegacyFile(t, dir, "a.json", goodProfile)
	writeLegacyFile(t, dir, "b.json", goodGeneratorProfile)
	writeLegacyFile(t, dir, "notes.txt", "ignored")

	var out bytes.Buffer
	if err := runConversion(store, convertProfileRulesSteps(dir, conversionOptions{}), &out, false); err != nil {
		t.Fatal(err)
	}
	if n, _ := store.count(); n != 0 {
		t.Fatalf("dry run wrote %d rows", n)
	}
	if !strings.Contains(out.String(), "would import test-engine") {
		t.Fatalf("dry run output: %s", out.String())
	}

	out.Reset()
	if err := runConversion(store, convertProfileRulesSteps(dir, conversionOptions{}), &out, true); err != nil {
		t.Fatal(err)
	}
	if n, _ := store.count(); n != 2 {
		t.Fatalf("expected 2 rows, got %d", n)
	}

	// Colliding ids abort the whole run.
	if err := runConversion(store, convertProfileRulesSteps(dir, conversionOptions{}), &out, true); err == nil {
		t.Fatal("re-running must collide")
	}
}

func TestConvertProfileRulesAbortsOnInvalidFileWithoutWriting(t *testing.T) {
	store := newTestProfileStore(t)
	dir := t.TempDir()
	writeLegacyFile(t, dir, "a.json", goodProfile)
	writeLegacyFile(t, dir, "z-bad.json", `{"id":"bad"}`)
	var out bytes.Buffer
	if err := runConversion(store, convertProfileRulesSteps(dir, conversionOptions{}), &out, true); err == nil {
		t.Fatal("expected failure")
	}
	if n, _ := store.count(); n != 0 {
		t.Fatalf("a failed run must write nothing, got %d rows", n)
	}
}

func TestStartupRefusesWhenOldFilesRemainAndTableIsEmpty(t *testing.T) {
	store := newTestProfileStore(t)
	dir := t.TempDir()

	if err := checkForLegacyProfileFiles(store, dir); err != nil {
		t.Fatalf("an empty directory is fine: %v", err)
	}
	if err := checkForLegacyProfileFiles(store, filepath.Join(dir, "missing")); err != nil {
		t.Fatalf("a missing directory is fine: %v", err)
	}
	writeLegacyFile(t, dir, "a.json", goodProfile)
	err := checkForLegacyProfileFiles(store, dir)
	if err == nil || !strings.Contains(err.Error(), "convert-profile-rules --apply") {
		t.Fatalf("expected a refusal naming the command, got %v", err)
	}

	seedRawProfileRow(t, store, "test-engine", goodProfile)
	if err := checkForLegacyProfileFiles(store, dir); err != nil {
		t.Fatalf("once the table has rows the directory is ignored: %v", err)
	}
}
