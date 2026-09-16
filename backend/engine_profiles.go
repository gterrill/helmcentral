package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"

	"github.com/labstack/echo/v4"
)

// Engine profiles (ADR 0053): drop-in reference data for an engine model —
// the gauges it wants, the scales they run on, its advisory operating bands,
// and its manufacturer service intervals.
//
// Plain JSON rather than a WASM plugin, unlike every other plugins/ directory.
// The other categories fetch data over a network and the sandbox exists to run
// untrusted code safely. A profile runs no code and fetches nothing — and since
// ADR 0050 its numbers are alarm thresholds, so the property that matters is
// that an operator can open the file and read what their oil-pressure alarm
// will fire at. A compiled binary removes exactly that.
//
// This supersedes ADR 0043's service-schedule plugin category.

const (
	engineProfileMaxGauges  = 24
	engineProfileMaxService = 64
	profileKindEngine       = "engine"
	profileKindAlternator   = "alternator"
	profileKindGenerator    = "generator"
)

// engineProfileZone is a band expressed the way the zone editor expresses one:
// a direction and a threshold, never a free from/to pair. A profile therefore
// cannot describe a mid-range band, which is the same thing the editor cannot
// describe and validateGaugeConfig rejects.
//
// A nil Threshold is a *slot* — a threshold the manufacturer defines but that
// is not publicly available. It survives loading so the apply dialog can show
// "not set", and is dropped before anything is saved.
type engineProfileZone struct {
	Direction string   `json:"direction"`
	Threshold *float64 `json:"threshold"`
	State     string   `json:"state"`
	Source    string   `json:"source,omitempty"`
	Note      string   `json:"note,omitempty"`
}

type engineProfileGauge struct {
	// PathSuffix, not a full path: the instance prefix (propulsion.port) is
	// chosen at apply time, so one profile serves both engines.
	PathSuffix string              `json:"path_suffix"`
	Label      string              `json:"label"`
	Hero       bool                `json:"hero,omitempty"`
	Display    string              `json:"display"`
	Quantity   string              `json:"quantity"`
	Unit       string              `json:"unit"`
	Min        *float64            `json:"min,omitempty"`
	Max        *float64            `json:"max,omitempty"`
	Decimals   *int                `json:"decimals,omitempty"`
	Zones      []engineProfileZone `json:"zones,omitempty"`
}

// engineProfileService is a manufacturer interval. The host derives what is
// due from it; the profile states only the raw published numbers. That much of
// ADR 0043 (its decision 3) survives unchanged.
//
// Both intervals may be nil, making the item a slot: the service exists and is
// named, but its frequency has to come from the manual.
type engineProfileService struct {
	ID             string   `json:"id"`
	Description    string   `json:"description"`
	IntervalHours  *float64 `json:"interval_hours"`
	IntervalMonths *int     `json:"interval_months"`
	FirstAtHours   *float64 `json:"first_at_hours,omitempty"`
	Supersedes     []string `json:"supersedes,omitempty"`
	Source         string   `json:"source,omitempty"`
}

type engineProfile struct {
	SchemaVersion int                    `json:"schema_version"`
	Kind          string                 `json:"kind"`
	ID            string                 `json:"id"`
	Name          string                 `json:"name"`
	Manufacturer  string                 `json:"manufacturer,omitempty"`
	Model         string                 `json:"model,omitempty"`
	RatingHP      int                    `json:"rating_hp,omitempty"`
	Source        string                 `json:"source,omitempty"`
	Notes         string                 `json:"notes,omitempty"`
	Gauges        []engineProfileGauge   `json:"gauges"`
	Service       []engineProfileService `json:"service,omitempty"`
}

// engineProfileProblem names a file that could not be loaded and why. A bad
// drop-in file must not take the server down, and must not vanish silently
// either — the Settings UI shows these.
type engineProfileProblem struct {
	File  string `json:"file"`
	Error string `json:"error"`
}

var (
	engineProfilesMu       sync.RWMutex
	engineProfilesState    []engineProfile
	engineProfileProblems  []engineProfileProblem
	validEngineZoneDirects = map[string]bool{"below": true, "above": true}
	profileIDPattern       = regexp.MustCompile(`^[a-z0-9][a-z0-9._-]*$`)
)

func engineProfilesDir() string {
	return cacheFilePath("ENGINE_PROFILES_DIR", "plugins/engine-profiles")
}

// validateEngineProfile reuses the gauge rules rather than restating them, so
// a profile can never describe a gauge the dashboard would reject.
func validateEngineProfile(p engineProfile) error {
	if p.Kind == "" {
		p.Kind = profileKindEngine
	}
	if p.SchemaVersion == 0 {
		p.SchemaVersion = 1
	}
	if p.SchemaVersion != 1 {
		return fmt.Errorf("unsupported schema_version %d", p.SchemaVersion)
	}
	if p.Kind != profileKindEngine && p.Kind != profileKindAlternator && p.Kind != profileKindGenerator {
		return fmt.Errorf("unsupported kind %q", p.Kind)
	}
	if strings.TrimSpace(p.ID) == "" {
		return fmt.Errorf("profile requires an id")
	}
	if strings.TrimSpace(p.Name) == "" {
		return fmt.Errorf("profile requires a name")
	}
	if len(p.Gauges) == 0 && len(p.Service) == 0 {
		return fmt.Errorf("profile has neither gauges nor service intervals")
	}
	if len(p.Gauges) > engineProfileMaxGauges {
		return fmt.Errorf("profile has too many gauges")
	}
	if len(p.Service) > engineProfileMaxService {
		return fmt.Errorf("profile has too many service items")
	}

	heroCount := 0

	for _, gauge := range p.Gauges {
		if err := validateEngineProfileGauge(gauge); err != nil {
			return fmt.Errorf("gauge %q: %w", gauge.PathSuffix, err)
		}
		if gauge.Hero {
			heroCount++
		}
		// The phase./total. prefix rule lives here only, not in the JSON schema
		// too. This function is reached on every path a profile can enter the
		// system — file load, POST, PUT — and directly from tests, whereas the
		// schema is only ever reached through those same three entry points on
		// their way to calling this. Two copies of one rule drift; keep one.
		if p.Kind == profileKindGenerator && !strings.HasPrefix(gauge.PathSuffix, "phase.") && !strings.HasPrefix(gauge.PathSuffix, "total.") {
			return fmt.Errorf("gauge %q: path_suffix must start with phase. or total. for generator profiles", gauge.PathSuffix)
		}
		if (p.Kind == profileKindEngine || p.Kind == profileKindAlternator) && (strings.HasPrefix(gauge.PathSuffix, "phase.") || strings.HasPrefix(gauge.PathSuffix, "total.")) {
			return fmt.Errorf("gauge %q: path_suffix cannot start with phase. or total. for %s profiles", gauge.PathSuffix, p.Kind)
		}
	}
	if heroCount > 1 {
		return fmt.Errorf("profile has more than one hero gauge")
	}

	seenService := map[string]bool{}
	for _, item := range p.Service {
		id := strings.TrimSpace(item.ID)
		if id == "" {
			return fmt.Errorf("service item requires an id")
		}
		if seenService[id] {
			return fmt.Errorf("duplicate service item id %q", id)
		}
		seenService[id] = true
		// Both intervals nil is a slot, the same idea as a nil zone threshold:
		// the profile knows the engine *has* this service without knowing how
		// often the manufacturer wants it. It names the item and leaves the
		// number to be filled, rather than guessing one.
		if item.IntervalHours != nil && *item.IntervalHours <= 0 {
			return fmt.Errorf("service item %q has a non-positive hours interval", id)
		}
		if item.IntervalMonths != nil && *item.IntervalMonths <= 0 {
			return fmt.Errorf("service item %q has a non-positive months interval", id)
		}
	}
	return nil
}

func validateEngineProfileGauge(gauge engineProfileGauge) error {
	if strings.TrimSpace(gauge.PathSuffix) == "" {
		return fmt.Errorf("requires a path_suffix")
	}
	if !validGaugeDisplays[gauge.Display] {
		return fmt.Errorf("unknown display %q", gauge.Display)
	}
	// convertToSI rejects an unknown quantity or unit, which is the guard the
	// whole zone-to-alarm conversion rests on (ADR 0050).
	if _, err := convertToSI(0, gauge.Quantity, gauge.Unit); err != nil {
		return err
	}
	if gauge.Min != nil && gauge.Max != nil && *gauge.Min >= *gauge.Max {
		return fmt.Errorf("min must be below max")
	}

	for i, zone := range gauge.Zones {
		if !validEngineZoneDirects[zone.Direction] {
			return fmt.Errorf("zone %d has unknown direction %q", i+1, zone.Direction)
		}
		if _, ok := alarmStateRank[zone.State]; !ok {
			return fmt.Errorf("zone %d has unknown state %q", i+1, zone.State)
		}
		// A band needs a scale to anchor against.
		if zone.Threshold != nil && (gauge.Min == nil || gauge.Max == nil) {
			return fmt.Errorf("zone %d needs the gauge to have a min and max", i+1)
		}
	}
	return nil
}

// loadEngineProfiles reads every *.json in the profiles directory. A file that
// fails is skipped and recorded; the rest still load.
func loadEngineProfiles() {
	dir := engineProfilesDir()

	var loaded []engineProfile
	var problems []engineProfileProblem

	entries, err := os.ReadDir(dir)
	if err != nil {
		// No profiles directory at all is a normal state, not a failure.
		if !os.IsNotExist(err) {
			problems = append(problems, engineProfileProblem{File: dir, Error: err.Error()})
			log.Printf("engine-profiles: cannot read %s: %v", dir, err)
		}
		storeEngineProfiles(loaded, problems)
		return
	}

	seen := map[string]string{}
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || !strings.HasSuffix(name, ".json") {
			continue
		}

		fail := func(err error) {
			problems = append(problems, engineProfileProblem{File: name, Error: err.Error()})
			log.Printf("engine-profiles: skipping %s: %v", name, err)
		}

		data, err := os.ReadFile(filepath.Join(dir, name))
		if err != nil {
			fail(err)
			continue
		}

		canonical, err := canonicalizeProfileDocument(data)
		if err != nil {
			fail(err)
			continue
		}

		schemaErrors, err := validateProfileDocument(canonical)
		if err != nil {
			fail(err)
			continue
		}
		if len(schemaErrors) > 0 {
			parts := make([]string, 0, len(schemaErrors))
			for _, issue := range schemaErrors {
				if strings.TrimSpace(issue.Path) == "" {
					parts = append(parts, issue.Message)
					continue
				}
				parts = append(parts, fmt.Sprintf("%s: %s", issue.Path, issue.Message))
			}
			fail(errors.New(strings.Join(parts, "; ")))
			continue
		}

		var profile engineProfile
		if err := json.Unmarshal(canonical, &profile); err != nil {
			fail(err)
			continue
		}
		if err := validateEngineProfile(profile); err != nil {
			fail(err)
			continue
		}
		if other, dup := seen[profile.ID]; dup {
			fail(fmt.Errorf("profile id %q is already defined by %s", profile.ID, other))
			continue
		}

		seen[profile.ID] = name
		loaded = append(loaded, profile)
	}

	sort.Slice(loaded, func(i, j int) bool { return loaded[i].Name < loaded[j].Name })
	storeEngineProfiles(loaded, problems)
}

func storeEngineProfiles(profiles []engineProfile, problems []engineProfileProblem) {
	engineProfilesMu.Lock()
	defer engineProfilesMu.Unlock()
	engineProfilesState = profiles
	engineProfileProblems = problems
}

func engineProfiles() ([]engineProfile, []engineProfileProblem) {
	engineProfilesMu.RLock()
	defer engineProfilesMu.RUnlock()
	return engineProfilesState, engineProfileProblems
}

func engineProfileFileForID(id string) (string, error) {
	dir := engineProfilesDir()
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return "", os.ErrNotExist
		}
		return "", err
	}

	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || !strings.HasSuffix(name, ".json") {
			continue
		}

		path := filepath.Join(dir, name)
		data, err := os.ReadFile(path)
		if err != nil {
			continue
		}

		canonical, err := canonicalizeProfileDocument(data)
		if err != nil {
			continue
		}

		var profile engineProfile
		if err := json.Unmarshal(canonical, &profile); err != nil {
			continue
		}

		if strings.TrimSpace(profile.ID) == id {
			return path, nil
		}
	}

	return "", os.ErrNotExist
}

func filteredEngineProfiles(profiles []engineProfile, kind string) []engineProfile {
	if strings.TrimSpace(kind) == "" {
		return profiles
	}
	filtered := make([]engineProfile, 0, len(profiles))
	for _, profile := range profiles {
		if profile.Kind == kind {
			filtered = append(filtered, profile)
		}
	}
	return filtered
}

func parseProfileID(raw string) (string, error) {
	id, err := url.PathUnescape(raw)
	if err != nil {
		return "", fmt.Errorf("malformed profile id")
	}
	id = strings.TrimSpace(id)
	if id == "" {
		return "", fmt.Errorf("profile id is required")
	}
	return id, nil
}

func profileFileName(id string) (string, error) {
	if !profileIDPattern.MatchString(id) {
		return "", fmt.Errorf("profile id may only use lowercase letters, numbers, dots, underscores, and hyphens")
	}
	return id + ".json", nil
}

func decodeProfileFromRequest(c echo.Context) (engineProfile, []profileValidationError, int, error) {
	raw, err := io.ReadAll(c.Request().Body)
	if err != nil {
		return engineProfile{}, nil, http.StatusBadRequest, fmt.Errorf("invalid request payload")
	}

	canonical, err := canonicalizeProfileDocument(raw)
	if err != nil {
		return engineProfile{}, nil, http.StatusBadRequest, fmt.Errorf("invalid request payload")
	}

	schemaErrors, err := validateProfileDocument(canonical)
	if err != nil {
		return engineProfile{}, nil, http.StatusInternalServerError, fmt.Errorf("profile schema validator unavailable")
	}
	if len(schemaErrors) > 0 {
		return engineProfile{}, schemaErrors, http.StatusBadRequest, fmt.Errorf("profile failed schema validation")
	}

	var profile engineProfile
	if err := json.Unmarshal(canonical, &profile); err != nil {
		return engineProfile{}, nil, http.StatusBadRequest, fmt.Errorf("invalid request payload")
	}
	if err := validateEngineProfile(profile); err != nil {
		return engineProfile{}, []profileValidationError{{Message: err.Error()}}, http.StatusBadRequest, fmt.Errorf("profile failed semantic validation")
	}

	return profile, nil, 0, nil
}

func readProfileByID(id string) (engineProfile, error) {
	path, err := engineProfileFileForID(id)
	if err != nil {
		return engineProfile{}, err
	}

	data, err := os.ReadFile(path)
	if err != nil {
		return engineProfile{}, err
	}

	canonical, err := canonicalizeProfileDocument(data)
	if err != nil {
		return engineProfile{}, err
	}

	var profile engineProfile
	if err := json.Unmarshal(canonical, &profile); err != nil {
		return engineProfile{}, err
	}
	return profile, nil
}

// GET /api/equipment-profiles
func equipmentProfilesHandler(c echo.Context) error {
	profiles, problems := engineProfiles()
	if profiles == nil {
		profiles = []engineProfile{}
	}

	if kind := strings.TrimSpace(c.QueryParam("kind")); kind != "" {
		profiles = filteredEngineProfiles(profiles, kind)
	}

	if problems == nil {
		problems = []engineProfileProblem{}
	}
	return c.JSON(http.StatusOK, map[string]any{"profiles": profiles, "problems": problems})
}

// GET /api/engine-profiles
func engineProfilesHandler(c echo.Context) error {
	profiles, problems := engineProfiles()
	if profiles == nil {
		profiles = []engineProfile{}
	}
	profiles = filteredEngineProfiles(profiles, profileKindEngine)
	if problems == nil {
		problems = []engineProfileProblem{}
	}
	return c.JSON(http.StatusOK, map[string]any{"profiles": profiles, "problems": problems})
}

func updateProfileHandler(c echo.Context, requiredKind string) error {
	id, err := parseProfileID(c.Param("id"))
	if err != nil {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": err.Error()})
	}

	profile, schemaErrors, statusCode, err := decodeProfileFromRequest(c)
	if err != nil {
		if len(schemaErrors) > 0 {
			return c.JSON(statusCode, map[string]any{"error": err.Error(), "errors": schemaErrors})
		}
		return c.JSON(statusCode, map[string]string{"error": err.Error()})
	}
	if strings.TrimSpace(profile.ID) != id {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "payload id must match path id"})
	}
	if strings.TrimSpace(requiredKind) != "" && profile.Kind != requiredKind {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "payload kind does not match endpoint"})
	}

	path, err := engineProfileFileForID(id)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return c.JSON(http.StatusNotFound, map[string]string{"error": "profile not found"})
		}
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": "failed to locate profile"})
	}

	if err := writeJSONFileAtomic(path, profile); err != nil {
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": "failed to save profile"})
	}

	loadEngineProfiles()
	return c.JSON(http.StatusOK, map[string]any{"profile": profile})
}

// POST /api/equipment-profiles
func createEquipmentProfileHandler(c echo.Context) error {
	profile, schemaErrors, statusCode, err := decodeProfileFromRequest(c)
	if err != nil {
		if len(schemaErrors) > 0 {
			return c.JSON(statusCode, map[string]any{"error": err.Error(), "errors": schemaErrors})
		}
		return c.JSON(statusCode, map[string]string{"error": err.Error()})
	}

	id := strings.TrimSpace(profile.ID)
	if _, err := engineProfileFileForID(id); err == nil {
		return c.JSON(http.StatusConflict, map[string]string{"error": "profile id already exists"})
	} else if !errors.Is(err, os.ErrNotExist) {
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": "failed to inspect existing profiles"})
	}

	name, err := profileFileName(id)
	if err != nil {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": err.Error()})
	}
	path := filepath.Join(engineProfilesDir(), name)
	if _, err := os.Stat(path); err == nil {
		return c.JSON(http.StatusConflict, map[string]string{"error": "profile file already exists"})
	} else if !errors.Is(err, os.ErrNotExist) {
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": "failed to inspect profile path"})
	}

	if err := writeJSONFileAtomic(path, profile); err != nil {
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": "failed to save profile"})
	}

	loadEngineProfiles()
	return c.JSON(http.StatusCreated, map[string]any{"profile": profile})
}

// GET /api/equipment-profiles/:id
func getEquipmentProfileHandler(c echo.Context) error {
	id, err := parseProfileID(c.Param("id"))
	if err != nil {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": err.Error()})
	}

	profile, err := readProfileByID(id)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return c.JSON(http.StatusNotFound, map[string]string{"error": "profile not found"})
		}
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": "failed to read profile"})
	}

	return c.JSON(http.StatusOK, map[string]any{"profile": profile})
}

// GET /api/equipment-profiles/:id/download
func downloadEquipmentProfileHandler(c echo.Context) error {
	id, err := parseProfileID(c.Param("id"))
	if err != nil {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": err.Error()})
	}

	profile, err := readProfileByID(id)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return c.JSON(http.StatusNotFound, map[string]string{"error": "profile not found"})
		}
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": "failed to read profile"})
	}

	payload, err := json.MarshalIndent(profile, "", "  ")
	if err != nil {
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": "failed to encode profile"})
	}

	c.Response().Header().Set(echo.HeaderContentType, echo.MIMEApplicationJSONCharsetUTF8)
	c.Response().Header().Set(echo.HeaderContentDisposition, fmt.Sprintf("attachment; filename=%q", id+".json"))
	return c.Blob(http.StatusOK, echo.MIMEApplicationJSONCharsetUTF8, payload)
}

// DELETE /api/equipment-profiles/:id
func deleteEquipmentProfileHandler(c echo.Context) error {
	id, err := parseProfileID(c.Param("id"))
	if err != nil {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": err.Error()})
	}

	path, err := engineProfileFileForID(id)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return c.JSON(http.StatusNotFound, map[string]string{"error": "profile not found"})
		}
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": "failed to locate profile"})
	}

	if err := os.Remove(path); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return c.JSON(http.StatusNotFound, map[string]string{"error": "profile not found"})
		}
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": "failed to delete profile"})
	}

	loadEngineProfiles()
	return c.NoContent(http.StatusNoContent)
}

// PUT /api/equipment-profiles/:id
func updateEquipmentProfileHandler(c echo.Context) error {
	return updateProfileHandler(c, "")
}

// PUT /api/engine-profiles/:id
func updateEngineProfileHandler(c echo.Context) error {
	return updateProfileHandler(c, profileKindEngine)
}
