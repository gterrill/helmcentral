package main

import (
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
// boat reports them, no item owns them, and (for batteries) they are not a
// charger's own input.
func offerableSources(disc discoveryResponse, kind string) int {
	n := 0
	for _, g := range disc.Groups {
		if g.Kind != kind {
			continue
		}
		for _, r := range g.Rows {
			if r.Reporting && r.Item == nil && !(kind == "battery" && r.ChargerInput) {
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

// engineSourcesWithFigures lists the engine sources that carry live figures,
// and counts the reporting ones that carry none. A bus can list empty numbered
// sources beside named ones with rpm. Helmcentral cannot tell whether they are
// the same engines, so it does not guess: only a source that shows figures is
// offered to the operator as an engine to make an item for.
func engineSourcesWithFigures(disc discoveryResponse) (sources []string, figureless int) {
	for _, g := range disc.Groups {
		if g.Kind != "engine" {
			continue
		}
		for _, r := range g.Rows {
			if !r.Reporting {
				continue
			}
			if len(r.Metrics) > 0 {
				sources = append(sources, r.Source)
			} else {
				figureless++
			}
		}
	}
	return sources, figureless
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
				if r.Reporting && !seen[l] {
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

// buildSetupStatus works the steps out from the resolved vessel and what the
// boat is reporting. It is a pure function so a test states the state it wants.
func buildSetupStatus(vessel vesselView, disc discoveryResponse) setupStatus {
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
		eng.State, eng.Detail = setupDone, "Assigned to a propulsion source: "+countOf(len(vessel.Engines), "engine", "engines")+"."
	case !connected:
		eng.State, eng.Detail = blockedBy()
	default:
		eng.State = setupTodo
		if sources, figureless := engineSourcesWithFigures(disc); len(sources) > 0 {
			eng.Detail = countOf(len(sources), "engine source", "engine sources") + strings.TrimRight(verbAre(len(sources)), " ") + " reporting " + engineFigureNames(disc) + ". In Discovery, create an item for each one that shows figures."
		} else if figureless > 0 {
			eng.Detail = "The boat lists engine sources, but they carry no figures yet."
		} else {
			eng.Detail = "The boat isn't reporting any engines."
		}
	}
	steps = append(steps, eng)

	// 3. House bank.
	bank := setupStep{ID: "house_bank", Title: "House bank", Target: setupTarget{"signalk", "role-house_bank"}}
	switch {
	case houseBankChosen(vessel):
		bank.State, bank.Detail = setupDone, "A house bank is chosen."
	case !connected:
		bank.State, bank.Detail = blockedBy()
	default:
		bank.State = setupTodo
		if reporting := offerableSources(disc, "battery"); reporting > 0 {
			bank.Detail = "Choose which of the " + countOf(reporting, "battery", "batteries") + " the boat reports is the house bank."
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
		power.State, power.Detail = setupDone, "Every charging and power source is chosen or marked as not on this boat."
	case !connected:
		power.State, power.Detail = blockedBy()
	default:
		power.State, power.Detail = setupTodo, "Still open: "+strings.Join(open, ", ")+". Choose one, or This boat has none."
	}
	steps = append(steps, power)

	complete := true
	for _, s := range steps {
		if s.State != setupDone {
			complete = false
		}
	}
	return setupStatus{Steps: steps, Complete: complete}
}

// GET /api/setup/status
func getSetupStatusHandler(c echo.Context) error {
	vessel, err := loadVesselView(getEnv("SETTINGS_FILE", "../settings.yaml"))
	if err != nil {
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": "failed to read the vessel"})
	}
	items, err := listSourceItems()
	if err != nil {
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": "failed to read equipment items"})
	}
	return c.JSON(http.StatusOK, buildSetupStatus(vessel, vesselDiscovery(globalSignalKSnapshot, vessel, items)))
}

func setupRoutes() []apiRoute {
	return []apiRoute{
		{http.MethodGet, "/api/setup/status", tierRead, getSetupStatusHandler},
	}
}
