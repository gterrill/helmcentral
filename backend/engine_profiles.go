package main

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
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
// Profiles are operator data stored in helmcentral.sqlite (profile_store.go);
// the profiles Helmcentral ships are a read-only catalogue compiled into the
// binary (profile_catalogue.go) that the operator copies from.
//
// Plain JSON rather than a WASM plugin, unlike every plugins/ directory.
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
	// profileKindBattery (anomaly-detection plan, ADR 0102's "nil threshold
	// is a slot" idea reused): chemistry plus per-cell charge thresholds,
	// rather than gauges/zones -- a house bank's full-bank-charging detector
	// needs a chemistry-specific SoC/voltage answer, not a dashboard tile.
	profileKindBattery = "battery"
)

// batteryFullSOCMax is the upper sanity bound for a battery profile's
// full_soc slot: a ratio, with the same 1.05 sensor-headroom convention
// anomaly_sensor_health.go's physicalLimits table uses for state of charge.
const batteryFullSOCMax = 1.05

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

// batteryProfileThreshold is one of a battery profile's per-cell/pack
// numbers (full_soc, charge_warn, charge_high). A nil Value is a slot --
// the profile knows this number exists but not what it is, exactly like
// engineProfileZone's nil Threshold -- and survives loading so the Vessel
// settings UI can show "not set". Source is that value's citation (a
// datasheet or manual); validateEngineProfile requires one whenever Value
// is filled, since a battery threshold with no citation is indistinguishable
// from a guess, and this one feeds an alarm about the failure mode that
// caused the 2026-09-21 dead-ship event.
type batteryProfileThreshold struct {
	Value  *float64 `json:"value"`
	Source string   `json:"source,omitempty"`
	Note   string   `json:"note,omitempty"`
}

type engineProfile struct {
	SchemaVersion int    `json:"schema_version"`
	Kind          string `json:"kind"`
	ID            string `json:"id"`
	Name          string `json:"name"`
	Manufacturer  string `json:"manufacturer,omitempty"`
	Model         string `json:"model,omitempty"`
	RatingHP      int    `json:"rating_hp,omitempty"`
	Source        string `json:"source,omitempty"`
	Notes         string `json:"notes,omitempty"`
	// omitempty: a battery profile has no gauges at all, and a profile that
	// only ships service intervals is already legal (validateEngineProfile's
	// "neither gauges nor service" check). Without it, marshalling a
	// decoded request straight back to disk (createEquipmentProfileHandler's
	// writeJSONFileAtomic) turns an absent "gauges" key into a literal
	// "gauges": null, which the battery schema's additionalProperties:false
	// then rejects on the very next load.
	Gauges  []engineProfileGauge   `json:"gauges,omitempty"`
	Service []engineProfileService `json:"service,omitempty"`

	// Battery-only fields (Kind == profileKindBattery). Chemistry is a free
	// label ("LiFePO4", "AGM lead-acid"); the three thresholds are the
	// per-cell charge voltages (ChargeWarn/ChargeHigh) and the pack SoC
	// ratio (FullSOC) fullBankChargingLevel needs (anomaly_battery.go).
	// Pack voltage thresholds are ChargeWarn/ChargeHigh multiplied by the
	// house bank's own cell count -- batteryPackThresholds below -- not
	// stored here, since this profile does not know the pack size.
	Chemistry  string                   `json:"chemistry,omitempty"`
	FullSOC    *batteryProfileThreshold `json:"full_soc,omitempty"`
	ChargeWarn *batteryProfileThreshold `json:"charge_warn,omitempty"`
	ChargeHigh *batteryProfileThreshold `json:"charge_high,omitempty"`
}

// engineProfileProblem names a stored profile that could not be loaded and
// why. A bad row must not take the server down, and must not vanish silently
// either.
type engineProfileProblem struct {
	ID    string `json:"id"`
	Error string `json:"error"`
}

var (
	engineProfilesMu       sync.RWMutex
	engineProfilesState    []engineProfile
	engineProfileProblems  []engineProfileProblem
	validEngineZoneDirects = map[string]bool{"below": true, "above": true}
	profileIDPattern       = regexp.MustCompile(`^[a-z0-9][a-z0-9._-]*$`)
)

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
	if p.Kind != profileKindEngine && p.Kind != profileKindAlternator && p.Kind != profileKindGenerator && p.Kind != profileKindBattery {
		return fmt.Errorf("unsupported kind %q", p.Kind)
	}
	if strings.TrimSpace(p.ID) == "" {
		return fmt.Errorf("profile requires an id")
	}
	if strings.TrimSpace(p.Name) == "" {
		return fmt.Errorf("profile requires a name")
	}

	if p.Kind == profileKindBattery {
		return validateBatteryProfile(p)
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
		if !profileIDPattern.MatchString(id) {
			return fmt.Errorf("service item id %q may only use lowercase letters, numbers, dots, underscores, and hyphens", id)
		}
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

// validateBatteryProfile is validateEngineProfile's branch for
// Kind == profileKindBattery: chemistry plus per-cell charge thresholds
// instead of gauges/zones. Called only after the shared id/name/schema
// checks above, with p.Kind already normalised.
func validateBatteryProfile(p engineProfile) error {
	if strings.TrimSpace(p.Chemistry) == "" {
		return fmt.Errorf("battery profile requires a chemistry")
	}
	if p.FullSOC == nil && p.ChargeWarn == nil && p.ChargeHigh == nil {
		return fmt.Errorf("battery profile has no threshold slots at all (full_soc, charge_warn or charge_high)")
	}

	if err := validateBatteryThreshold("full_soc", p.FullSOC, 0, batteryFullSOCMax); err != nil {
		return err
	}
	if err := validateBatteryThreshold("charge_warn", p.ChargeWarn, 0, 0); err != nil {
		return err
	}
	if err := validateBatteryThreshold("charge_high", p.ChargeHigh, 0, 0); err != nil {
		return err
	}
	if p.ChargeWarn != nil && p.ChargeWarn.Value != nil && p.ChargeHigh != nil && p.ChargeHigh.Value != nil &&
		*p.ChargeHigh.Value <= *p.ChargeWarn.Value {
		return fmt.Errorf("charge_high must be above charge_warn")
	}
	return nil
}

// validateBatteryThreshold checks one battery profile threshold slot. min
// and max bound a filled Value; max <= 0 means "no upper bound" (per-cell
// voltages have no fixed ceiling here -- chemistry varies too widely to
// pick one). A filled Value with no Source is rejected: the plan's "ship
// only profiles whose numbers can be cited from a public datasheet;
// anything else is a slot" is a safety rule, not a style preference, since
// this feeds the full-bank-charging alarm.
func validateBatteryThreshold(field string, t *batteryProfileThreshold, min, max float64) error {
	if t == nil || t.Value == nil {
		return nil
	}
	if *t.Value <= min {
		return fmt.Errorf("%s must be above %v", field, min)
	}
	if max > 0 && *t.Value > max {
		return fmt.Errorf("%s must be at most %v", field, max)
	}
	if strings.TrimSpace(t.Source) == "" {
		return fmt.Errorf("%s has a value but no source to cite it", field)
	}
	return nil
}

// batteryPackThresholds multiplies a battery profile's per-cell charge_warn
// and charge_high by cells to get the pack-level voltage thresholds
// fullBankChargingSettings.WarnVoltage/HighVoltage need -- the plan's "pack
// thresholds are per-cell x cells, shown and overridable" (the vessel
// settings house-bank record's own WarnVoltage/HighVoltage fields hold the
// operator's override, applied by the caller, not here).
//
// ok is false when cells <= 0 or charge_warn has no filled value:
// fullBankChargingLevel reads WarnVoltage <= 0 as "house bank not
// configured", so a missing warn slot has to produce that same signal
// rather than a zero-valued threshold that would silently pass it. Like
// fullBankChargingSettings.HighVoltage itself, charge_high is optional --
// highV comes back 0 when it has no filled value, and that alone does not
// affect ok.
func batteryPackThresholds(p engineProfile, cells int) (warnV, highV float64, ok bool) {
	if cells <= 0 {
		return 0, 0, false
	}
	if p.ChargeWarn == nil || p.ChargeWarn.Value == nil {
		return 0, 0, false
	}
	warnV = *p.ChargeWarn.Value * float64(cells)
	if p.ChargeHigh != nil && p.ChargeHigh.Value != nil {
		highV = *p.ChargeHigh.Value * float64(cells)
	}
	return warnV, highV, true
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

// parseProfileDocument runs a raw profile document through the same
// canonicalize, schema and semantic checks every entry point uses, and returns
// the decoded profile.
func parseProfileDocument(raw []byte) (engineProfile, error) {
	canonical, err := canonicalizeProfileDocument(raw)
	if err != nil {
		return engineProfile{}, err
	}
	schemaErrors, err := validateProfileDocument(canonical)
	if err != nil {
		return engineProfile{}, err
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
		return engineProfile{}, errors.New(strings.Join(parts, "; "))
	}
	var profile engineProfile
	if err := json.Unmarshal(canonical, &profile); err != nil {
		return engineProfile{}, err
	}
	if err := validateEngineProfile(profile); err != nil {
		return engineProfile{}, err
	}
	return profile, nil
}

// loadEngineProfiles reads every stored profile and re-validates it. A row
// that fails is skipped and recorded; the rest still load.
func loadEngineProfiles() {
	if globalProfileStore == nil {
		err := errors.New("profile store is not open")
		log.Printf("engine-profiles: %v", err)
		storeEngineProfiles(nil, []engineProfileProblem{{ID: "", Error: err.Error()}})
		return
	}

	var loaded []engineProfile
	var problems []engineProfileProblem

	rows, err := globalProfileStore.allRows()
	if err != nil {
		log.Printf("engine-profiles: cannot read stored profiles: %v", err)
		storeEngineProfiles(nil, []engineProfileProblem{{ID: "", Error: err.Error()}})
		return
	}

	for _, row := range rows {
		profile, err := parseProfileDocument([]byte(row.Body))
		if err == nil && profile.ID != row.ID {
			err = fmt.Errorf("stored under id %q but the document says %q", row.ID, profile.ID)
		}
		if err != nil {
			problems = append(problems, engineProfileProblem{ID: row.ID, Error: err.Error()})
			log.Printf("engine-profiles: skipping %s: %v", row.ID, err)
			continue
		}
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

func validateProfileID(id string) error {
	if !profileIDPattern.MatchString(id) {
		return fmt.Errorf("profile id may only use lowercase letters, numbers, dots, underscores, and hyphens")
	}
	return nil
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

// readProfileByID reads one profile straight from the table, so a handler
// never answers from a cache that a failed reload left stale.
func readProfileByID(id string) (engineProfile, error) {
	if globalProfileStore == nil {
		return engineProfile{}, errors.New("profile store is not open")
	}
	var profile engineProfile
	err := globalProfileStore.inTx(func(tx *sql.Tx) error {
		var err error
		profile, _, err = getProfileTx(tx, id)
		return err
	})
	return profile, err
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

	if globalProfileStore == nil {
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": "profile store is not open"})
	}
	confirmRemoved := c.QueryParam("confirm_removed") == "1"
	err = globalProfileStore.inTx(func(tx *sql.Tx) error {
		if _, _, err := getProfileTx(tx, id); errors.Is(err, errProfileNotFound) {
			return err
		}
		if err := guardProfileUpdateTx(tx, profile, confirmRemoved); err != nil {
			return err
		}
		return updateProfileTx(tx, profile)
	})
	if err != nil {
		var guard *profileGuardError
		if errors.As(err, &guard) {
			return writeProfileGuardError(c, guard)
		}
		if errors.Is(err, errProfileNotFound) {
			return c.JSON(http.StatusNotFound, map[string]string{"error": "profile not found"})
		}
		log.Printf("engine-profiles: update %s: %v", id, err)
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": "failed to save profile"})
	}

	loadEngineProfiles()
	if profile.Kind == profileKindBattery {
		seedAnomalyRulesAfterProfileSave()
	}
	return c.JSON(http.StatusOK, map[string]any{"profile": profile})
}

// seedAnomalyRulesAfterProfileSave re-seeds whichever anomaly-v1 sub-sets
// have just become configured after a battery profile save -- completing a
// linked battery profile's full_soc/charge_warn slots is exactly what
// vesselHouseBankReady checks, and that must take effect immediately rather
// than waiting for the next vessel-settings save or server restart (code
// review finding 1). Scoped to profileKindBattery, not every profile save:
// no other kind can ever complete vesselHouseBankReady, and every existing
// engine/alternator/generator profile test saves without also standing up
// an isolated alarm-rules file, since until now a profile save never touched
// alarm state at all. A failure here is logged, not returned as an error,
// the same non-fatal treatment every other seed call in this codebase gets
// -- the profile save itself already succeeded.
func seedAnomalyRulesAfterProfileSave() {
	vessel, err := loadVesselSettings(getEnv("SETTINGS_FILE", "../settings.yaml"))
	if err != nil {
		log.Printf("could not load vessel settings for anomaly rule seeding: %v", err)
		return
	}
	if err := seedAnomalyRules(vessel); err != nil {
		log.Printf("could not seed the anomaly alarm rules after a profile save: %v", err)
	}
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
	if err := validateProfileID(id); err != nil {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": err.Error()})
	}
	if globalProfileStore == nil {
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": "profile store is not open"})
	}
	err = globalProfileStore.inTx(func(tx *sql.Tx) error { return insertProfileTx(tx, profile, profileBasedOn{}) })
	if err != nil {
		if errors.Is(err, errProfileExists) {
			return c.JSON(http.StatusConflict, map[string]string{"error": "profile id already exists"})
		}
		log.Printf("engine-profiles: create %s: %v", id, err)
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
		if errors.Is(err, errProfileNotFound) {
			return c.JSON(http.StatusNotFound, map[string]string{"error": "profile not found"})
		}
		log.Printf("engine-profiles: read %s: %v", id, err)
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
		if errors.Is(err, errProfileNotFound) {
			return c.JSON(http.StatusNotFound, map[string]string{"error": "profile not found"})
		}
		log.Printf("engine-profiles: read %s: %v", id, err)
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

	if globalProfileStore == nil {
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": "profile store is not open"})
	}
	err = globalProfileStore.inTx(func(tx *sql.Tx) error {
		if err := guardProfileDeleteTx(tx, id); err != nil {
			return err
		}
		return deleteProfileTx(tx, id)
	})
	if err != nil {
		var guard *profileGuardError
		if errors.As(err, &guard) {
			return writeProfileGuardError(c, guard)
		}
		if errors.Is(err, errProfileNotFound) {
			return c.JSON(http.StatusNotFound, map[string]string{"error": "profile not found"})
		}
		log.Printf("engine-profiles: delete %s: %v", id, err)
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
