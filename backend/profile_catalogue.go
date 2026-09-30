package main

import (
	"crypto/sha256"
	"database/sql"
	"embed"
	"encoding/hex"
	"errors"
	"fmt"
	"io/fs"
	"log"
	"net/http"
	"sort"
	"strings"
	"sync"

	"github.com/labstack/echo/v4"
)

// The built-in catalogue: profiles shipped with Helmcentral that an operator
// can copy into their own profile list. Entries are read-only; copying one
// makes an operator profile that can be edited freely.

//go:embed profile_catalogue/*.json
var profileCatalogueFiles embed.FS

const profileCatalogueSource = "helmcentral"

type catalogueEntry struct {
	Source  string
	Profile engineProfile
	SHA256  string
	raw     []byte
}

var (
	catalogueOnce    sync.Once
	catalogueEntries []catalogueEntry
	catalogueErr     error
)

// profileCatalogue returns the validated embedded catalogue, sorted by name.
// An invalid entry is a build defect and is reported as an error.
func profileCatalogue() ([]catalogueEntry, error) {
	catalogueOnce.Do(func() {
		catalogueEntries, catalogueErr = loadProfileCatalogue(profileCatalogueFiles)
	})
	return catalogueEntries, catalogueErr
}

func loadProfileCatalogue(fsys fs.FS) ([]catalogueEntry, error) {
	names, err := fs.Glob(fsys, "profile_catalogue/*.json")
	if err != nil {
		return nil, err
	}
	var out []catalogueEntry
	seen := map[string]string{}
	for _, name := range names {
		data, err := fs.ReadFile(fsys, name)
		if err != nil {
			return nil, fmt.Errorf("profile catalogue %s: %w", name, err)
		}
		profile, err := parseProfileDocument(data)
		if err != nil {
			return nil, fmt.Errorf("profile catalogue %s: %w", name, err)
		}
		if other, dup := seen[profile.ID]; dup {
			return nil, fmt.Errorf("profile catalogue %s: id %q is already defined by %s", name, profile.ID, other)
		}
		seen[profile.ID] = name
		sum := sha256.Sum256(data)
		out = append(out, catalogueEntry{
			Source:  profileCatalogueSource,
			Profile: profile,
			SHA256:  hex.EncodeToString(sum[:]),
			raw:     data,
		})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Profile.Name < out[j].Profile.Name })
	return out, nil
}

func findCatalogueEntry(id string) (catalogueEntry, bool, error) {
	entries, err := profileCatalogue()
	if err != nil {
		return catalogueEntry{}, false, err
	}
	for _, e := range entries {
		if e.Profile.ID == id {
			return e, true, nil
		}
	}
	return catalogueEntry{}, false, nil
}

type catalogueListItem struct {
	Source       string `json:"source"`
	ID           string `json:"id"`
	Name         string `json:"name"`
	Kind         string `json:"kind"`
	Manufacturer string `json:"manufacturer"`
	Model        string `json:"model"`
	SHA256       string `json:"sha256"`
	Added        bool   `json:"added"`
}

// GET /api/equipment-profiles/catalogue
func profileCatalogueHandler(c echo.Context) error {
	entries, err := profileCatalogue()
	if err != nil {
		log.Printf("profile catalogue: %v", err)
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": "profile catalogue unavailable"})
	}
	if globalProfileStore == nil {
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": "profile store is not open"})
	}
	existing, err := globalProfileStore.ids()
	if err != nil {
		log.Printf("profile catalogue: %v", err)
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": "failed to read profiles"})
	}
	items := make([]catalogueListItem, 0, len(entries))
	for _, e := range entries {
		items = append(items, catalogueListItem{
			Source:       e.Source,
			ID:           e.Profile.ID,
			Name:         e.Profile.Name,
			Kind:         e.Profile.Kind,
			Manufacturer: e.Profile.Manufacturer,
			Model:        e.Profile.Model,
			SHA256:       e.SHA256,
			Added:        existing[e.Profile.ID],
		})
	}
	return c.JSON(http.StatusOK, map[string]any{"entries": items})
}

// POST /api/equipment-profiles/catalogue/:id
//
// Copies a catalogue entry into the operator's profiles. An optional body
// {"id": "..."} stores the copy under a different id.
func copyCatalogueProfileHandler(c echo.Context) error {
	id, err := parseProfileID(c.Param("id"))
	if err != nil {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": err.Error()})
	}
	entry, ok, err := findCatalogueEntry(id)
	if err != nil {
		log.Printf("profile catalogue: %v", err)
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": "profile catalogue unavailable"})
	}
	if !ok {
		return c.JSON(http.StatusNotFound, map[string]string{"error": "catalogue entry not found"})
	}

	var body struct {
		ID string `json:"id"`
	}
	if c.Request().ContentLength != 0 {
		if err := c.Bind(&body); err != nil {
			return c.JSON(http.StatusBadRequest, map[string]string{"error": "invalid request payload"})
		}
	}

	profile := entry.Profile
	if newID := strings.TrimSpace(body.ID); newID != "" {
		profile.ID = newID
	}
	if err := validateProfileID(profile.ID); err != nil {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": err.Error()})
	}
	if err := validateEngineProfile(profile); err != nil {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": err.Error()})
	}
	if globalProfileStore == nil {
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": "profile store is not open"})
	}

	basedOn := profileBasedOn{Source: entry.Source, ID: entry.Profile.ID, SHA256: entry.SHA256}
	err = globalProfileStore.inTx(func(tx *sql.Tx) error { return insertProfileTx(tx, profile, basedOn) })
	if err != nil {
		if errors.Is(err, errProfileExists) {
			return c.JSON(http.StatusConflict, map[string]string{"error": "profile id already exists"})
		}
		log.Printf("profile catalogue: copy %s: %v", id, err)
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": "failed to save profile"})
	}

	loadEngineProfiles()
	if profile.Kind == profileKindBattery {
		seedAnomalyRulesAfterProfileSave()
	}
	return c.JSON(http.StatusCreated, map[string]any{"profile": profile})
}
