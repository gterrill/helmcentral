package main

import (
	"log"
	"net/http"
	"regexp"
	"sort"

	"github.com/labstack/echo/v4"
)

// vessel_handlers.go is Settings -> Vessel's own read/write surface:
// GET /api/vessel/candidates lists what the boat is currently publishing so
// the operator picks engines and a house bank from live readings rather
// than typing a SignalK path, and GET/POST /api/vessel load and save the
// vessel.* block itself (vessel_settings.go), re-seeding whichever
// anomaly-v1 sub-sets just became configured (alarm_seed_anomaly.go) on
// every save.

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
	Path    string   `json:"path"`
	Voltage *float64 `json:"voltage"`
	Current *float64 `json:"current"`
	SoC     *float64 `json:"soc"`
}

// vesselDetectorStatus is one detector's setup state for the Settings page:
// Ready false always carries Missing, the single next thing the operator
// needs to do ("Pick your house bank"), never a generic "not configured".
type vesselDetectorStatus struct {
	Ready   bool   `json:"ready"`
	Missing string `json:"missing,omitempty"`
}

type vesselCandidatesResponse struct {
	Engines   []vesselEngineCandidate         `json:"engines"`
	Batteries []vesselBatteryCandidate        `json:"batteries"`
	Detectors map[string]vesselDetectorStatus `json:"detectors"`
}

var (
	vesselCandidatePropulsionRe = regexp.MustCompile(`^propulsion\.([^.]+)\.`)
	vesselCandidateBatteryRe    = regexp.MustCompile(`^electrical\.batteries\.([^.]+)\.`)
)

// vesselCandidates builds the whole GET /api/vessel/candidates response:
// every propulsion/battery instance the boat is currently publishing (built
// on signalk_paths.go's collectSignalKPaths and the snapshot, per the plan
// -- signalk_discovery.go is the unrelated LAN scanner), plus each
// detector's current setup status against vessel.
func vesselCandidates(snapshot *signalKSnapshot, vessel vesselSettings) vesselCandidatesResponse {
	tree := snapshot.selfTree()
	var paths []signalKPath
	if tree != nil {
		paths = collectSignalKPaths(tree)
	}

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

	batteries := make([]vesselBatteryCandidate, 0, len(batteryInstances))
	for instance := range batteryInstances {
		path := "electrical.batteries." + instance
		candidate := vesselBatteryCandidate{Path: path}
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

	return vesselCandidatesResponse{
		Engines:   engines,
		Batteries: batteries,
		Detectors: vesselDetectorStatuses(vessel),
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

// GET /api/vessel
func getVesselSettingsHandler(c echo.Context) error {
	vessel, err := loadVesselSettings(getEnv("SETTINGS_FILE", "../settings.yaml"))
	if err != nil {
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": "failed to read vessel settings"})
	}
	return c.JSON(http.StatusOK, vessel)
}

// POST /api/vessel saves the whole vessel.* block and re-seeds whichever
// anomaly-v1 sub-sets have just become configured. A seeding failure is
// logged, not returned as an error: the save itself succeeded, and the
// alarm rules will catch up on the next server restart if they didn't just
// now (the same non-fatal treatment main.go gives every seed call at
// startup).
func postVesselSettingsHandler(c echo.Context) error {
	var req vesselSettings
	if err := c.Bind(&req); err != nil {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "invalid request payload"})
	}

	settingsPath := getEnv("SETTINGS_FILE", "../settings.yaml")
	if err := saveVesselSettings(settingsPath, req); err != nil {
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": "failed to save vessel settings"})
	}

	if err := seedAnomalyRules(req); err != nil {
		log.Printf("could not seed the anomaly alarm rules after a vessel-settings save: %v", err)
	}

	return c.JSON(http.StatusOK, req)
}
