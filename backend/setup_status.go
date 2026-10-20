package main

import (
	"fmt"
	"log"
	"net/http"
	"strconv"
	"strings"

	"github.com/labstack/echo/v4"
)

// setup_status.go is the one place that decides how far a fresh install has
// got: SignalK connected, engines assigned, a house bank chosen, the charging
// and power sources answered. GET /api/setup/status returns the ordered steps;
// the Settings checklist, the dashboard's setup line and the Roles page's
// per-check statuses all read it, through enginesAssigned and houseBankChosen
// for the two questions the anomaly checks also ask. Nothing is stored: every
// call works from the live vessel view and discovery, so a step that becomes
// undone (a renumbered instance, a deleted item, a cleared role) is a todo
// again with no dismiss flag to forget (ADR 0190).

const (
	setupDone    = "done"
	setupTodo    = "todo"
	setupBlocked = "blocked"
)

// setupTarget is the control a step lands on: a Settings section and the DOM
// id of the control inside it.
type setupTarget struct {
	Section string `json:"section"`
	Control string `json:"control"`
}

type setupStep struct {
	ID     string      `json:"id"`
	Title  string      `json:"title"`
	State  string      `json:"state"`
	Detail string      `json:"detail"`
	Target setupTarget `json:"target"`
	// Optional steps are offered but never hold setup open: Complete ignores
	// them.
	Optional bool `json:"optional"`
}

type setupStatus struct {
	Steps    []setupStep `json:"steps"`
	Complete bool        `json:"complete"`
}

// enginesAssigned is true once at least one engine item is mapped to a
// propulsion source. The frozen-sensor check asks the same question.
func enginesAssigned(v vesselView) bool { return len(v.Engines) >= 1 }

// houseBankChosen is true once the house bank role names an item that owns a
// battery source.
func houseBankChosen(v vesselView) bool { return v.houseBankPath() != "" }

// houseBankUnresolvedDetail is said wherever the house bank role names an item
// that has lost its SignalK source: the tile and the setup step.
const houseBankUnresolvedDetail = "The house bank's item has no SignalK source. Choose it again under Settings, SignalK, Roles."

// powerRoles are the parts the "Charging and power sources" step asks about,
// in the order they are offered. Each is answered by choosing an item or "This
// boat has none".
var powerRoles = []struct {
	field string
	title string
	set   func(vesselView) bool
}{
	{"shore_charger", "Shore charger", func(v vesselView) bool { return v.Electrical.ShoreChargerInstance != "" }},
	{"port_alternator", "Port alternator", func(v vesselView) bool { return v.Alternators.PortInstance != "" }},
	{"starboard_alternator", "Starboard alternator", func(v vesselView) bool { return v.Alternators.StarboardInstance != "" }},
	{"inverter", "Inverter", func(v vesselView) bool { return v.Electrical.InverterInstance != "" }},
	{"generator", "Generator", func(v vesselView) bool { return v.Electrical.GeneratorInstance != "" }},
	{"generator_ac_instance", "Generator AC input", func(v vesselView) bool { return v.Electrical.GeneratorACInstance != "" }},
}

// offerableSources counts the sources the Roles picker offers for kind: the
// boat reports or has logged them, no item owns them, and (for batteries) they are not a
// charger's own input.
func offerableSources(disc discoveryResponse, kind string) int {
	n := 0
	for _, g := range disc.Groups {
		if g.Kind != kind {
			continue
		}
		for _, r := range g.Rows {
			if r.Item == nil && !(kind == "battery" && r.ChargerInput) {
				n++
			}
		}
	}
	return n
}

// batteryChoices counts what the Roles picker lists for the house bank: the
// battery items that own a source, then the unowned sources the boat reports.
func batteryChoices(disc discoveryResponse) int {
	n := offerableSources(disc, "battery")
	for _, g := range disc.Groups {
		if g.Kind != "battery" {
			continue
		}
		for _, r := range g.Rows {
			if r.Item != nil {
				n++
			}
		}
	}
	return n
}

// anythingSetUp is true once an item owns a SignalK source or any role has
// been answered. From then on a dropped feed is the stale indicators' business,
// not the checklist's.
func anythingSetUp(v vesselView, disc discoveryResponse) bool {
	if enginesAssigned(v) || houseBankChosen(v) || len(v.RolesNone) > 0 || len(v.Batteries) > 0 || len(v.Solar) > 0 || len(v.Tanks) > 0 {
		return true
	}
	for _, r := range powerRoles {
		if r.set(v) {
			return true
		}
	}
	for _, g := range disc.Groups {
		for _, r := range g.Rows {
			if r.Item != nil {
				return true
			}
		}
	}
	return false
}

func countOf(n int, one, many string) string {
	if n == 1 {
		return "1 " + one
	}
	return strconv.Itoa(n) + " " + many
}

// signatureCheck is what the discovery rows say about the items owning sources
// of some kinds: the sentences for those on a source that carries nothing the
// kind needs (history was read and holds none), and how many could not be
// judged because history could not be read. A source the boat reports now is
// carried, so a SignalK dropout never makes a finished setup a problem.
type signatureCheck struct {
	problems  []string
	unchecked int
}

// checkSignatures judges the items owning sources of kinds whose row keep
// accepts (nil keeps every row).
func checkSignatures(disc discoveryResponse, keep func(discoveryRow) bool, kinds ...string) signatureCheck {
	var out signatureCheck
	for _, g := range disc.Groups {
		wanted := false
		for _, k := range kinds {
			wanted = wanted || g.Kind == k
		}
		if !wanted {
			continue
		}
		for _, r := range g.Rows {
			if r.Item == nil || (keep != nil && !keep(r)) {
				continue
			}
			switch r.Signature {
			case signatureMissing:
				out.problems = append(out.problems, fmt.Sprintf("%s is set to %s, %s source that has not reported %s.",
					r.Item.Name, r.Source, withArticle(sourceKindNouns[sourceGroup(g.Kind)]), g.SignatureLabel))
			case signatureUnknown:
				out.unchecked++
			}
		}
	}
	return out
}

// chosenPowerSource keeps the rows of the sources chosen for the power roles,
// so an item that no role reads is not judged.
func chosenPowerSource(v vesselView) func(discoveryRow) bool {
	chosen := map[string]bool{}
	for group, instance := range map[sourceGroup]string{
		sourceCharger: v.Electrical.ShoreChargerInstance, sourceInverter: v.Electrical.InverterInstance,
		sourceGenerator: v.Electrical.GeneratorInstance,
	} {
		if instance != "" {
			chosen[sourcePath(group, instance)] = true
		}
	}
	for _, instance := range []string{v.Alternators.PortInstance, v.Alternators.StarboardInstance} {
		if instance != "" {
			chosen[sourcePath(sourceAlternator, instance)] = true
		}
	}
	return func(r discoveryRow) bool { return chosen[r.Source] }
}

func withArticle(noun string) string {
	if noun == "" {
		return noun
	}
	if strings.ContainsRune("aeiou", rune(noun[0])) {
		return "an " + noun
	}
	return "a " + noun
}

// detail words a finished step's check: the problems with the fix, or a note
// that some sources could not be checked, or the plain done line.
func (c signatureCheck) detail(done, fix, historyError string) (problem bool, text string) {
	if len(c.problems) > 0 {
		return true, strings.Join(c.problems, " ") + " " + fix
	}
	if c.unchecked > 0 {
		return false, done + " Helmcentral couldn't check that they report what they should (" + historyError + ")."
	}
	return false, done
}

// unownedEngineSources are the engine sources an operator could make an item
// for: offered by Discovery (live now, or logged) and owned by no item. The
// server offers a source only if it carries RPM, so a numbered duplicate with
// one stray reading is not among them.
func unownedEngineSources(disc discoveryResponse) []discoveryRow {
	var rows []discoveryRow
	for _, g := range disc.Groups {
		if g.Kind != "engine" {
			continue
		}
		for _, r := range g.Rows {
			if r.Item == nil {
				rows = append(rows, r)
			}
		}
	}
	return rows
}

// engineFigureNames is the figures the engine sources carry, lower case, in the
// order first seen: "rpm and coolant".
func engineFigureNames(disc discoveryResponse) string {
	var names []string
	seen := map[string]bool{}
	for _, g := range disc.Groups {
		if g.Kind != "engine" {
			continue
		}
		for _, r := range g.Rows {
			for _, m := range r.Metrics {
				l := strings.ToLower(m.Label)
				if r.Reporting && r.Item == nil && !seen[l] {
					seen[l] = true
					names = append(names, l)
				}
			}
		}
	}
	return listAnd(names)
}

// listAnd joins "a", "a and b", "a, b and c".
func listAnd(items []string) string {
	switch len(items) {
	case 0:
		return ""
	case 1:
		return items[0]
	}
	return strings.Join(items[:len(items)-1], ", ") + " and " + items[len(items)-1]
}

func verbAre(n int) string {
	if n == 1 {
		return " is "
	}
	return " are "
}

// setupMate is what the optional Mate steps read: whether Mate is on, and the
// passage limits the operator has entered.
type setupMate struct {
	Enabled bool
	Limits  passageLimits
	// LimitsErr is set when the stored limits could not be read.
	LimitsErr error
}

// passageLimitsStep is the optional "Passage limits" step. It is done when
// every wind sector has its no-go wind limit; otherwise it names the open
// sectors. Nothing else reads the limits, so the caller lists it only while
// Mate is on.
func passageLimitsStep(l passageLimits, readErr error) setupStep {
	step := setupStep{
		ID: "passage_limits", Title: "Passage limits", Optional: true,
		Target: setupTarget{"assistant", "passage-limits"},
	}
	if readErr != nil {
		step.State, step.Detail = setupBlocked, "The saved passage limits couldn't be read. Check settings.yaml."
		return step
	}
	var open []string
	for _, sec := range []struct {
		name string
		s    passageSectorLimits
	}{{"ahead", l.Ahead}, {"abeam", l.Abeam}, {"astern", l.Astern}} {
		if sec.s.WindKnNoGo == nil {
			open = append(open, sec.name)
		}
	}
	if len(open) == 0 {
		step.State, step.Detail = setupDone, "Limits are set for every wind sector."
	} else {
		step.State, step.Detail = setupTodo, "Still open: "+strings.Join(open, ", ")+"."
	}
	return step
}

// buildSetupStatus works the steps out from the resolved vessel and what the
// boat is reporting. It is a pure function so a test states the state it wants.
func buildSetupStatus(vessel vesselView, disc discoveryResponse, mate setupMate) setupStatus {
	connected := disc.Connected
	setUp := anythingSetUp(vessel, disc)
	// Steps are judged from saved configuration whatever the feed is doing. A
	// step that is not done and needs the feed (choosing from what the boat
	// reports) is blocked while it is down.
	blockedBy := func() (string, string) {
		if setUp {
			return setupBlocked, "Waiting for SignalK to reconnect."
		}
		return setupBlocked, "Connect to SignalK first."
	}
	steps := make([]setupStep, 0, 4)

	// 1. Connection.
	conn := setupStep{ID: "connection", Title: "Connect to SignalK", Target: setupTarget{"signalk", "signalk-address"}}
	if connected {
		conn.State, conn.Detail = setupDone, "Helmcentral is receiving vessel data."
	} else if setUp {
		conn.State, conn.Detail = setupDone, "SignalK isn't sending data at the moment; the stale indicators on the tiles say so."
	} else {
		conn.State, conn.Detail = setupTodo, "Helmcentral has no vessel data from SignalK. Check the address and port, then Test Connection and Save."
	}
	steps = append(steps, conn)

	// 2. Engines.
	eng := setupStep{ID: "engines", Title: "Engines", Target: setupTarget{"signalk", "discovery-engine"}}
	switch {
	case enginesAssigned(vessel):
		bad, text := checkSignatures(disc, nil, "engine").detail(
			"Assigned to a propulsion source: "+countOf(len(vessel.Engines), "engine", "engines")+".",
			"Choose the engine source that does.", disc.HistoryError)
		eng.State, eng.Detail = setupDone, text
		if bad {
			eng.State = setupTodo
		}
	case !connected:
		eng.State, eng.Detail = blockedBy()
	default:
		eng.State = setupTodo
		if sources := unownedEngineSources(disc); len(sources) > 0 {
			eng.Detail = countOf(len(sources), "engine source", "engine sources") + strings.TrimRight(verbAre(len(sources)), " ") + " available"
			if figs := engineFigureNames(disc); figs != "" {
				eng.Detail += ", reporting " + figs
			}
			eng.Detail += ". In Discovery, create an item for each one."
		} else {
			eng.Detail = "The boat isn't reporting any engines."
		}
	}
	steps = append(steps, eng)

	// 3. House bank.
	bank := setupStep{ID: "house_bank", Title: "House bank", Target: setupTarget{"signalk", "role-house_bank"}}
	switch {
	case houseBankChosen(vessel):
		bankID := ""
		if vessel.HouseBank != nil {
			bankID = vessel.HouseBank.EquipmentID
		}
		bad, text := checkSignatures(disc, func(r discoveryRow) bool { return r.Item.ID == bankID }, "battery").detail("A house bank is chosen.", "Choose the battery that does.", disc.HistoryError)
		bank.State, bank.Detail = setupDone, text
		if bad {
			bank.State = setupTodo
		}
	case vessel.HouseBankUnresolved:
		bank.State, bank.Detail = setupTodo, houseBankUnresolvedDetail
	case !connected:
		bank.State, bank.Detail = blockedBy()
	default:
		bank.State = setupTodo
		if choices := batteryChoices(disc); choices > 0 {
			bank.Detail = "Choose which of the " + countOf(choices, "battery", "batteries") + " is the house bank."
		} else {
			bank.Detail = "The boat isn't reporting any batteries."
		}
	}
	steps = append(steps, bank)

	// 4. Charging and power sources.
	power := setupStep{ID: "power", Title: "Charging and power sources", Target: setupTarget{"signalk", "role-" + powerRoles[0].field}}
	var open []string
	for i, r := range powerRoles {
		if r.set(vessel) || vessel.RolesNone[r.field] {
			continue
		}
		// No generator means no generator AC input to choose.
		if r.field == "generator_ac_instance" && vessel.RolesNone["generator"] {
			continue
		}
		if len(open) == 0 {
			power.Target.Control = "role-" + powerRoles[i].field
		}
		open = append(open, r.title)
	}
	switch {
	case len(open) == 0:
		bad, text := checkSignatures(disc, chosenPowerSource(vessel), "alternator", "charger", "inverter", "generator").detail(
			"Every charging and power source is chosen or marked as not on this boat.", "Choose the source that does.", disc.HistoryError)
		power.State, power.Detail = setupDone, text
		if bad {
			power.State = setupTodo
		}
	case !connected:
		power.State, power.Detail = blockedBy()
	default:
		power.State, power.Detail = setupTodo, "Still open: "+strings.Join(open, ", ")+". Choose one, or This boat has none."
	}
	steps = append(steps, power)

	if mate.Enabled {
		steps = append(steps, passageLimitsStep(mate.Limits, mate.LimitsErr))
	}

	complete := true
	for _, s := range steps {
		if !s.Optional && s.State != setupDone {
			complete = false
		}
	}
	return setupStatus{Steps: steps, Complete: complete}
}

// GET /api/setup/status
func getSetupStatusHandler(c echo.Context) error {
	roles, err := loadVesselRoles(getEnv("SETTINGS_FILE", "../settings.yaml"))
	if err != nil {
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": "failed to read the vessel"})
	}
	// The items are read once and shared by the vessel view and discovery.
	items, err := listSourceItems()
	if err != nil {
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": "failed to read equipment items"})
	}
	// Settings only feed the optional Mate step. If they cannot be read the
	// rest of the list still returns; the step is omitted, since whether Mate
	// is on is not known.
	var mate setupMate
	if settings, err := readSettings(getEnv("SETTINGS_FILE", "../settings.yaml")); err != nil {
		log.Printf("setup status: settings unreadable, passage limits step omitted: %v", err)
	} else {
		assistant := buildSettingsPayload(settings).Assistant
		mate.Enabled = assistant.Enabled
		if am, ok := settings["assistant"].(map[string]any); ok {
			mate.Limits, mate.LimitsErr = passageLimitsFromSettings(am["passage_limits"])
		}
		if mate.LimitsErr != nil {
			log.Printf("setup status: %v", mate.LimitsErr)
		}
	}
	vessel := resolveVesselView(roles, items)
	return c.JSON(http.StatusOK, buildSetupStatus(vessel, vesselDiscovery(globalSignalKSnapshot, vessel, items, loadSourceHistory()), mate))
}

func setupRoutes() []apiRoute {
	return []apiRoute{
		{http.MethodGet, "/api/setup/status", tierRead, getSetupStatusHandler},
	}
}
