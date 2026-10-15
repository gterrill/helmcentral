package main

import (
	"net/http"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/labstack/echo/v4"
)

// vessel_handlers.go is Settings -> Vessel's own read/write surface:
// GET /api/vessel/candidates lists what the boat is currently publishing so
// the operator picks engines and a house bank from live readings rather
// than typing a SignalK path. The vessel.* block itself (vessel_settings.go)
// loads and saves with the rest of the settings (GET/POST /api/settings),
// which re-seeds whichever anomaly-v1 sub-sets just became configured
// (alarm_seed_anomaly.go).

// vesselEngineCandidate is one propulsion.<instance> the boat is currently
// publishing, with its live rpm and coolant temperature so the operator
// recognises it ("that's port, it's running warm") rather than having to
// know the SignalK instance name in advance. RPM and CoolantC are already
// converted to the units an operator reads (rpm, degC) -- unlike most of
// this backend's numeric APIs, this endpoint exists purely for a human
// picker to render, not for further arithmetic, so there is nothing to
// gain from making the frontend re-derive what convertFromSI would give it
// straight back.
type vesselEngineCandidate struct {
	Instance string   `json:"instance"`
	RPM      *float64 `json:"rpm"`
	CoolantC *float64 `json:"coolant_c"`
}

// vesselBatteryCandidate is one electrical.batteries.<id> the boat is
// currently publishing. Showing voltage, current and SoC side by side is
// how an operator resolves ambiguity like two IDs that are really the same
// physical bank reported two ways.
type vesselBatteryCandidate struct {
	Path string `json:"path"`
	// Instance is the <id> in electrical.batteries.<id>, the key an operator
	// battery name is stored under. BusName is what the bus itself calls it
	// (its .name path, or its device's installation or product name), shown
	// as the hint beside the operator's own name field; empty when the bus
	// says nothing. ChargerInput marks an instance that is a charger's
	// own input (published only by chargers that feed another
	// bank), which the impossible-reading check does not treat as a battery.
	Instance     string   `json:"instance"`
	BusName      string   `json:"bus_name"`
	ChargerInput bool     `json:"charger_input"`
	Voltage      *float64 `json:"voltage"`
	Current      *float64 `json:"current"`
	SoC          *float64 `json:"soc"`
}

// vesselAlternatorCandidate is one electrical.alternator.<instance> the boat
// is currently publishing, with its live current so the operator can tell
// which is which by the one that is charging.
type vesselAlternatorCandidate struct {
	Instance string   `json:"instance"`
	Current  *float64 `json:"current"`
}

// vesselGeneratorCandidate is one electrical.generator.<instance> with its
// state word when it is reporting now.
type vesselGeneratorCandidate struct {
	Instance string  `json:"instance"`
	State    *string `json:"state"`
}

// vesselACCandidate is one electrical.ac.<instance> with its real power.
type vesselACCandidate struct {
	Instance  string   `json:"instance"`
	RealPower *float64 `json:"real_power"`
}

// vesselChargerCandidate is one electrical.chargers.<instance> with its
// output current and its AC input current.
type vesselChargerCandidate struct {
	Instance    string   `json:"instance"`
	Current     *float64 `json:"current"`
	ACInCurrent *float64 `json:"acin_current"`
}

// vesselInverterCandidate is one electrical.inverters.<instance> with its AC
// output power.
type vesselInverterCandidate struct {
	Instance string   `json:"instance"`
	ACOut    *float64 `json:"ac_out"`
}

// vesselSolarCandidate is one electrical.solar.<instance> with its panel
// power.
type vesselSolarCandidate struct {
	Instance   string   `json:"instance"`
	PanelPower *float64 `json:"panel_power"`
}

// vesselDetectorStatus is one detector's setup state for the Settings page:
// Ready false always carries Missing, the single next thing the operator
// needs to do ("Pick your house bank"), never a generic "not configured".
type vesselDetectorStatus struct {
	Ready   bool   `json:"ready"`
	Missing string `json:"missing,omitempty"`
}

type vesselCandidatesResponse struct {
	Engines     []vesselEngineCandidate         `json:"engines"`
	Batteries   []vesselBatteryCandidate        `json:"batteries"`
	Alternators []vesselAlternatorCandidate     `json:"alternators"`
	Generators  []vesselGeneratorCandidate      `json:"generators"`
	AC          []vesselACCandidate             `json:"ac"`
	Chargers    []vesselChargerCandidate        `json:"chargers"`
	Inverters   []vesselInverterCandidate       `json:"inverters"`
	Solar       []vesselSolarCandidate          `json:"solar"`
	Detectors   map[string]vesselDetectorStatus `json:"detectors"`
}

var (
	vesselCandidatePropulsionRe = regexp.MustCompile(`^propulsion\.([^.]+)\.`)
	vesselCandidateBatteryRe    = regexp.MustCompile(`^electrical\.batteries\.([^.]+)\.`)
	vesselCandidateAlternatorRe = regexp.MustCompile(`^electrical\.alternator\.([^.]+)\.`)
	vesselCandidateGeneratorRe  = regexp.MustCompile(`^electrical\.generator\.([^.]+)\.`)
	vesselCandidateACRe         = regexp.MustCompile(`^electrical\.ac\.([^.]+)\.`)
	vesselCandidateChargerRe    = regexp.MustCompile(`^electrical\.chargers\.([^.]+)\.`)
	vesselCandidateInverterRe   = regexp.MustCompile(`^electrical\.inverters\.([^.]+)\.`)
	vesselCandidateSolarRe      = regexp.MustCompile(`^electrical\.solar\.([^.]+)\.`)
)

// vesselCandidateMaxAge is how old a reading can be and still be shown beside
// a candidate as live. An instance that has gone quiet is still listed, since
// the operator may have chosen it, but without a figure.
const vesselCandidateMaxAge = 5 * time.Minute

// freshCandidateNumber reads the first of paths that holds a number reported
// within vesselCandidateMaxAge. A reading with no known arrival is not shown.
func freshCandidateNumber(t telemetryReader, paths ...string) *float64 {
	for _, path := range paths {
		sample := t.Sample(path)
		if !sample.Present || !sample.AgeKnown || sample.Age > vesselCandidateMaxAge {
			continue
		}
		if v, ok := numericTelemetryValue(sample.Value); ok {
			return &v
		}
	}
	return nil
}

// freshCandidateString reads a string reported within vesselCandidateMaxAge,
// by the same rule as freshCandidateNumber.
func freshCandidateString(t telemetryReader, path string) *string {
	sample := t.Sample(path)
	if !sample.Present || !sample.AgeKnown || sample.Age > vesselCandidateMaxAge {
		return nil
	}
	state, ok := sample.Value.(string)
	if !ok || strings.TrimSpace(state) == "" {
		return nil
	}
	return &state
}

// candidateInstances collects the <id> segment of every path matching re.
func candidateInstances(paths []signalKPath, re *regexp.Regexp) []string {
	seen := map[string]bool{}
	var ids []string
	for _, p := range paths {
		if m := re.FindStringSubmatch(p.Path); m != nil && !seen[m[1]] {
			seen[m[1]] = true
			ids = append(ids, m[1])
		}
	}
	sort.Slice(ids, func(i, j int) bool { return instanceLess(ids[i], ids[j]) })
	return ids
}

// electricalCandidates lists the generators, AC inputs, chargers, inverters
// and solar controllers the boat is publishing, each with a reading only when
// it is fresh.
func electricalCandidates(t telemetryReader, paths []signalKPath) (
	[]vesselGeneratorCandidate, []vesselACCandidate, []vesselChargerCandidate, []vesselInverterCandidate, []vesselSolarCandidate,
) {
	generators := []vesselGeneratorCandidate{}
	for _, id := range candidateInstances(paths, vesselCandidateGeneratorRe) {
		generators = append(generators, vesselGeneratorCandidate{Instance: id, State: freshCandidateString(t, "electrical.generator."+id+".state")})
	}

	ac := []vesselACCandidate{}
	for _, id := range candidateInstances(paths, vesselCandidateACRe) {
		prefix := "electrical.ac." + id
		leaves := []string{prefix + ".phase.A.realPower", prefix + ".phaseA.realPower", prefix + ".realPower"}
		// Only a node that carries one of the real-power leaves the generator
		// reading uses is an AC input; electrical.ac.loads and the like are not.
		carries := false
		for _, leaf := range leaves {
			if t.Sample(leaf).Present {
				carries = true
			}
		}
		if !carries {
			continue
		}
		ac = append(ac, vesselACCandidate{Instance: id, RealPower: freshCandidateNumber(t, leaves...)})
	}

	chargers := []vesselChargerCandidate{}
	for _, id := range candidateInstances(paths, vesselCandidateChargerRe) {
		prefix := "electrical.chargers." + id
		chargers = append(chargers, vesselChargerCandidate{
			Instance:    id,
			Current:     freshCandidateNumber(t, prefix+".current"),
			ACInCurrent: freshCandidateNumber(t, prefix+".acin.1.current"),
		})
	}

	inverters := []vesselInverterCandidate{}
	for _, id := range candidateInstances(paths, vesselCandidateInverterRe) {
		prefix := "electrical.inverters." + id
		inverters = append(inverters, vesselInverterCandidate{Instance: id, ACOut: freshCandidateNumber(t, prefix+".ac.output.power", prefix+".acout.power", prefix+".acOutputPower")})
	}

	solar := []vesselSolarCandidate{}
	for _, id := range candidateInstances(paths, vesselCandidateSolarRe) {
		prefix := "electrical.solar." + id
		solar = append(solar, vesselSolarCandidate{Instance: id, PanelPower: freshCandidateNumber(t, prefix+".panelPower", prefix+".power")})
	}

	return generators, ac, chargers, inverters, solar
}

// vesselCandidates builds the whole GET /api/vessel/candidates response:
// every propulsion/battery instance the boat is currently publishing (built
// on signalk_paths.go's collectSignalKPaths and the snapshot, per the plan
// -- signalk_discovery.go is the unrelated LAN scanner), plus each
// detector's current setup status against vessel.
func vesselCandidates(snapshot *signalKSnapshot, vessel vesselSettings) vesselCandidatesResponse {
	telemetry := snapshot.liveSelfTelemetry(time.Now().UTC())
	paths := telemetry.Paths()

	engineInstances := map[string]bool{}
	batteryInstances := map[string]bool{}
	for _, p := range paths {
		if m := vesselCandidatePropulsionRe.FindStringSubmatch(p.Path); m != nil {
			engineInstances[m[1]] = true
		}
		if m := vesselCandidateBatteryRe.FindStringSubmatch(p.Path); m != nil {
			batteryInstances[m[1]] = true
		}
	}

	engines := make([]vesselEngineCandidate, 0, len(engineInstances))
	for instance := range engineInstances {
		candidate := vesselEngineCandidate{Instance: instance}
		if hz, ok := numericFromPath(snapshot, "propulsion."+instance+".revolutions"); ok {
			rpm := hz * 60
			candidate.RPM = &rpm
		}
		if k, ok := numericFromPath(snapshot, "propulsion."+instance+".temperature"); ok {
			c := k - 273.15
			candidate.CoolantC = &c
		}
		engines = append(engines, candidate)
	}
	sort.Slice(engines, func(i, j int) bool { return engines[i].Instance < engines[j].Instance })

	namer := newSensorNamer(snapshot, vessel, globalSourceDevices.get())
	batteries := make([]vesselBatteryCandidate, 0, len(batteryInstances))
	for instance := range batteryInstances {
		path := "electrical.batteries." + instance
		candidate := vesselBatteryCandidate{
			Path:         path,
			Instance:     instance,
			BusName:      namer.busBatteryName(instance),
			ChargerInput: namer.isChargerInstance(instance),
		}
		if v, ok := numericFromPath(snapshot, path+".voltage"); ok {
			candidate.Voltage = &v
		}
		if c, ok := numericFromPath(snapshot, path+".current"); ok {
			candidate.Current = &c
		}
		if soc, ok := numericFromPath(snapshot, path+".capacity.stateOfCharge"); ok {
			candidate.SoC = &soc
		}
		batteries = append(batteries, candidate)
	}
	sort.Slice(batteries, func(i, j int) bool { return batteries[i].Path < batteries[j].Path })

	alternators := []vesselAlternatorCandidate{}
	for _, instance := range candidateInstances(paths, vesselCandidateAlternatorRe) {
		candidate := vesselAlternatorCandidate{Instance: instance}
		if v, ok := telemetry.Number("electrical.alternator." + instance + ".current"); ok {
			c := v.Value
			candidate.Current = &c
		}
		alternators = append(alternators, candidate)
	}

	generators, ac, chargers, inverters, solar := electricalCandidates(telemetry, paths)

	return vesselCandidatesResponse{
		Engines:     engines,
		Batteries:   batteries,
		Alternators: alternators,
		Generators:  generators,
		AC:          ac,
		Chargers:    chargers,
		Inverters:   inverters,
		Solar:       solar,
		Detectors:   vesselDetectorStatuses(vessel),
	}
}

// vesselDetectorStatuses names, per detector, whether vessel has completed
// its setup and, when not, the single next thing missing.
func vesselDetectorStatuses(vessel vesselSettings) map[string]vesselDetectorStatus {
	statuses := map[string]vesselDetectorStatus{}

	if len(vessel.Engines) >= 1 {
		statuses["frozen"] = vesselDetectorStatus{Ready: true}
	} else {
		statuses["frozen"] = vesselDetectorStatus{Missing: "Tick at least one engine"}
	}

	if vesselHouseBankReady(vessel.HouseBank) {
		statuses["battery"] = vesselDetectorStatus{Ready: true}
	} else if vessel.HouseBank == nil || vessel.HouseBank.Path == "" {
		statuses["battery"] = vesselDetectorStatus{Missing: "Pick your house bank"}
	} else if vessel.HouseBank.EquipmentID == "" {
		statuses["battery"] = vesselDetectorStatus{Missing: "Link an inventory item with a battery profile"}
	} else {
		statuses["battery"] = vesselDetectorStatus{Missing: "Fill in the linked profile's full_soc and charge_warn"}
	}

	if len(vessel.Engines) >= 2 {
		statuses["engines"] = vesselDetectorStatus{Ready: true}
	} else {
		statuses["engines"] = vesselDetectorStatus{Missing: "Tick a second engine"}
	}

	return statuses
}

// GET /api/vessel/candidates
func getVesselCandidatesHandler(c echo.Context) error {
	vessel, err := loadVesselSettings(getEnv("SETTINGS_FILE", "../settings.yaml"))
	if err != nil {
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": "failed to read vessel settings"})
	}
	return c.JSON(http.StatusOK, vesselCandidates(globalSignalKSnapshot, vessel))
}

// instanceLess orders instance ids naturally: numeric ids by value (1, 2, 12),
// anything else after them, as text.
func instanceLess(a, b string) bool {
	na, errA := strconv.Atoi(a)
	nb, errB := strconv.Atoi(b)
	switch {
	case errA == nil && errB == nil:
		return na < nb
	case errA == nil:
		return true
	case errB == nil:
		return false
	}
	return a < b
}
