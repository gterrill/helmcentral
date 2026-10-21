package main

import (
	"fmt"
	"net/http"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/labstack/echo/v4"
)

// vessel_handlers.go is Settings -> SignalK's Discovery read surface:
// GET /api/vessel/discovery lists what the boat is currently reporting, grouped
// by kind, with which equipment item (if any) owns each source, so the
// operator assigns a source to an item or creates one from live readings
// rather than typing a SignalK path (ADR 0187). The roles block itself
// (vessel_roles.go) loads and saves with the rest of the settings (GET/POST
// /api/settings), which re-seeds whichever anomaly-v1 sub-sets just became
// configured (alarm_seed_anomaly.go).

// discoveryMetric is one live figure shown beside a source so the operator
// recognises it ("that's port, it's running warm"). Text is already in the
// units an operator reads; this endpoint exists purely for a human picker to
// render, so there is nothing to gain from making the frontend re-derive what
// convertFromSI would give it straight back.
type discoveryMetric struct {
	Label string `json:"label"`
	Text  string `json:"text"`
}

// discoveryItemRef is the equipment item that owns a source.
type discoveryItemRef struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

// discoveryRow is one source: something the boat reports now, or something an
// item still owns that has gone quiet (Reporting false).
type discoveryRow struct {
	Source string `json:"source"`
	// Instance is the <id> segment ("port", "1", "fuel.2").
	Instance string `json:"instance"`
	// BusName is what the bus itself calls a battery (its .name path, or its
	// device's installation or product name); empty when it says nothing.
	BusName string `json:"bus_name"`
	// ChargerInput marks a battery instance that is a charger's own input
	// (published only by chargers that feed another bank), which the
	// impossible-reading check does not treat as a battery.
	ChargerInput bool              `json:"charger_input"`
	Reporting    bool              `json:"reporting"`
	Metrics      []discoveryMetric `json:"metrics"`
	Item         *discoveryItemRef `json:"item"`
	// LastSeen is when Helmcentral last saw this source's signature path
	// reported; null when it never has.
	LastSeen *time.Time `json:"last_seen"`
	// Signature is whether the source carries what its kind needs: "carried",
	// "missing" (the source reports other readings, the record spans enough
	// time to judge, and it never has this one) or "unknown". Only a source an item owns can be anything but carried,
	// since an unowned source is listed only when it carries its signature.
	Signature string `json:"signature"`
}

// discoveryGroup is one kind of source. Mappable false marks a group an item
// cannot own (AC inputs, which only the generator AC input role picks from).
type discoveryGroup struct {
	Kind     string `json:"kind"`
	Mappable bool   `json:"mappable"`
	// SignatureLabel is what a source of this kind must report, in the
	// operator's words ("RPM"); empty for a group an item cannot own.
	SignatureLabel string         `json:"signature_label"`
	Rows           []discoveryRow `json:"rows"`
}

// vesselDetectorStatus is one detector's setup state for the Settings page:
// Ready false always carries Missing, the single next thing the operator
// needs to do ("Choose your house bank"), never a generic "not configured".
type vesselDetectorStatus struct {
	Ready   bool   `json:"ready"`
	Missing string `json:"missing,omitempty"`
}

// discoveryTracking describes the record Discovery was built from. Since is
// null until tracking has begun; Seeded says it was started from InfluxDB
// history; Covers says it spans enough time to call a source missing its
// signature.
type discoveryTracking struct {
	Since  *time.Time `json:"since"`
	Seeded bool       `json:"seeded"`
	Covers bool       `json:"covers"`
	// SinceDay is Since as a day ("21 Oct") in the vessel's own time zone.
	SinceDay string `json:"since_day"`
	// Error is why the record could not be read; set, only sources reporting
	// now are listed.
	Error string `json:"error"`
}

type discoveryResponse struct {
	// Connected is false while Helmcentral has no vessel data from SignalK, so
	// the page can say so instead of showing an empty list.
	Connected bool                            `json:"connected"`
	Groups    []discoveryGroup                `json:"groups"`
	Detectors map[string]vesselDetectorStatus `json:"detectors"`
	// Tracking says how long Helmcentral has been watching this boat, so the
	// page can be honest about an engine it has not yet seen report.
	Tracking discoveryTracking `json:"tracking"`
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

// addMetric appends "label: value+unit" when v holds a reading.
func addMetric(out []discoveryMetric, label string, v *float64, format, unit string) []discoveryMetric {
	if v == nil {
		return out
	}
	return append(out, discoveryMetric{Label: label, Text: fmt.Sprintf(format, *v) + unit})
}

// liveCandidateNumber reads path's number whatever its age. Engines,
// batteries and alternators are listed by what the tree holds now; the
// freshness cut-off applies to the electrical sources, which linger in the
// tree after they go quiet.
func liveCandidateNumber(t telemetryReader, path string) *float64 {
	v, ok := t.Number(path)
	if !ok {
		return nil
	}
	n := v.Value
	return &n
}

// tankInstances lists "<category>.<id>" for every tank the boat publishes a
// level for.
func tankInstances(t telemetryReader) []string {
	var ids []string
	for _, category := range t.Children("tanks") {
		for _, id := range t.Children("tanks." + category) {
			if t.Sample("tanks." + category + "." + id + ".currentLevel").Present {
				ids = append(ids, category+"."+id)
			}
		}
	}
	sort.Strings(ids)
	return ids
}

// discoveredInstances is the instance ids each group is reporting now.
func discoveredInstances(t telemetryReader, paths []signalKPath) map[sourceGroup][]string {
	return map[sourceGroup][]string{
		sourceEngine:     candidateInstances(paths, vesselCandidatePropulsionRe),
		sourceBattery:    candidateInstances(paths, vesselCandidateBatteryRe),
		sourceAlternator: candidateInstances(paths, vesselCandidateAlternatorRe),
		sourceSolar:      candidateInstances(paths, vesselCandidateSolarRe),
		sourceTank:       tankInstances(t),
		sourceGenerator:  candidateInstances(paths, vesselCandidateGeneratorRe),
		sourceCharger:    candidateInstances(paths, vesselCandidateChargerRe),
		sourceInverter:   candidateInstances(paths, vesselCandidateInverterRe),
	}
}

// sourceMetrics are the live figures shown beside a source; empty when it is
// not reporting fresh data.
func sourceMetrics(t telemetryReader, group sourceGroup, instance string) []discoveryMetric {
	prefix := sourcePath(group, instance)
	var out []discoveryMetric
	switch group {
	case sourceEngine:
		if hz, ok := t.Number(prefix + ".revolutions"); ok {
			rpm := hz.Value * 60
			out = addMetric(out, "RPM", &rpm, "%.0f", " rpm")
		}
		if k, ok := t.Number(prefix + ".temperature"); ok {
			c := k.Value - 273.15
			out = addMetric(out, "Coolant", &c, "%.0f", " °C")
		}
	case sourceBattery:
		out = addMetric(out, "Voltage", liveCandidateNumber(t, prefix+".voltage"), "%.2f", " V")
		out = addMetric(out, "Current", liveCandidateNumber(t, prefix+".current"), "%.1f", " A")
		if soc := liveCandidateNumber(t, prefix+".capacity.stateOfCharge"); soc != nil {
			pct := *soc * 100
			out = addMetric(out, "State of charge", &pct, "%.0f", "%")
		}
	case sourceAlternator:
		out = addMetric(out, "Current", liveCandidateNumber(t, prefix+".current"), "%.1f", " A")
	case sourceSolar:
		out = addMetric(out, "Panel power", freshCandidateNumber(t, prefix+".panelPower", prefix+".power"), "%.0f", " W")
	case sourceTank:
		if level := freshCandidateNumber(t, prefix+".currentLevel"); level != nil {
			pct := *level
			if pct <= 1 {
				pct *= 100
			}
			out = addMetric(out, "Level", &pct, "%.0f", "%")
		}
	case sourceGenerator:
		if state := freshCandidateString(t, prefix+".state"); state != nil {
			out = append(out, discoveryMetric{Label: "State", Text: *state})
		}
	case sourceCharger:
		out = addMetric(out, "Output", freshCandidateNumber(t, prefix+".current"), "%.1f", " A")
		out = addMetric(out, "AC in", freshCandidateNumber(t, prefix+".acin.1.current"), "%.1f", " A")
	case sourceInverter:
		out = addMetric(out, "AC out", freshCandidateNumber(t, prefix+".ac.output.power", prefix+".acout.power", prefix+".acOutputPower"), "%.0f", " W")
	}
	return out
}

// acInputRows lists the AC inputs the boat reports, with their real power. Only
// a node that carries one of the real-power leaves the generator reading uses is
// an AC input; electrical.ac.loads and the like are not.
func acInputRows(t telemetryReader, paths []signalKPath) []discoveryRow {
	rows := []discoveryRow{}
	for _, id := range candidateInstances(paths, vesselCandidateACRe) {
		prefix := "electrical.ac." + id
		leaves := []string{prefix + ".phase.A.realPower", prefix + ".phaseA.realPower", prefix + ".realPower"}
		carries := false
		for _, leaf := range leaves {
			if t.Sample(leaf).Present {
				carries = true
			}
		}
		if !carries {
			continue
		}
		row := discoveryRow{Source: prefix, Instance: id, Reporting: true, Metrics: []discoveryMetric{}}
		row.Metrics = addMetric(row.Metrics, "Real power", freshCandidateNumber(t, leaves...), "%.0f", " W")
		rows = append(rows, row)
	}
	return rows
}

// vesselDiscovery builds the whole GET /api/vessel/discovery response: every
// source that carries its kind's signature (source_signatures.go), live now or
// logged, plus every source an item owns even if it carries none, each with the item that owns it, and each detector's
// current setup status against vessel.
func vesselDiscovery(snapshot *signalKSnapshot, vessel vesselView, items []sourceItem, record sourceRecord) discoveryResponse {
	telemetry := snapshot.liveSelfTelemetry(time.Now().UTC())
	paths := telemetry.Paths()
	live := discoveredInstances(telemetry, paths)
	namer := newSensorNamer(snapshot, vessel, globalSourceDevices.get())

	owner := map[string]sourceItem{}
	for _, it := range items {
		owner[it.Source] = it
	}

	groups := make([]discoveryGroup, 0, len(sourceGroupOrder)+1)
	for _, group := range sourceGroupOrder {
		// An instance is offered only when it carries its kind's signature,
		// now or in the record; an instance an item owns is always
		// listed, with a verdict on whether it carries the signature.
		inTree := map[string]bool{}
		carriedLive := map[string]bool{}
		ids := map[string]bool{}
		for _, id := range live[group] {
			inTree[id] = true
			if liveCarriesSignature(telemetry, group, sourcePath(group, id)) {
				carriedLive[id] = true
				ids[id] = true
			}
		}
		for _, id := range record.seenInstances(group) {
			ids[id] = true
		}
		for _, it := range items {
			if it.Group == group {
				ids[it.Instance] = true
			}
		}
		sorted := make([]string, 0, len(ids))
		for id := range ids {
			sorted = append(sorted, id)
		}
		sort.Slice(sorted, func(i, j int) bool { return instanceLess(sorted[i], sorted[j]) })

		rows := make([]discoveryRow, 0, len(sorted))
		for _, id := range sorted {
			source := sourcePath(group, id)
			row := discoveryRow{
				Source: source, Instance: id, Reporting: inTree[id], Metrics: []discoveryMetric{},
				Signature: signatureState(carriedLive[id], record, group, source),
			}
			if at, ok := record.lastSeenAt(group, source); ok {
				row.LastSeen = &at
			}
			if row.Reporting {
				row.Metrics = sourceMetrics(telemetry, group, id)
			}
			if group == sourceBattery {
				row.BusName = namer.busBatteryName(id)
				row.ChargerInput = namer.isChargerInstance(id)
			}
			if it, ok := owner[source]; ok {
				row.Item = &discoveryItemRef{ID: it.ID, Name: it.Name}
			}
			rows = append(rows, row)
		}
		groups = append(groups, discoveryGroup{Kind: string(group), Mappable: true, SignatureLabel: sourceSignatures[group].label, Rows: rows})
	}
	groups = append(groups, discoveryGroup{Kind: "ac", Mappable: false, Rows: acInputRows(telemetry, paths)})

	resp := discoveryResponse{Connected: snapshot.selfTelemetry().HasSelf(), Groups: groups, Detectors: vesselDetectorStatuses(vessel)}
	resp.Tracking = discoveryTracking{Seeded: record.Seeded, Covers: record.Covers}
	if !record.Since.IsZero() {
		since := record.Since
		resp.Tracking.Since = &since
		resp.Tracking.SinceDay = vesselLocalDay(since, telemetry)
	}
	if record.Err != nil {
		resp.Tracking.Error = record.Err.Error()
	}
	return resp
}

// vesselLocalDay words t as a day ("21 Oct") in the vessel's own time zone,
// from its position; UTC while it has none.
func vesselLocalDay(t time.Time, telemetry telemetryReader) string {
	loc := time.UTC
	if pos, ok := telemetry.Position(); ok && hasUsableVesselPosition(pos.Latitude, pos.Longitude) {
		loc = vesselLocalLocation(pos.Longitude)
	}
	return t.In(loc).Format("2 Jan")
}

// vesselDetectorStatuses names, per detector, whether vessel has completed
// its setup and, when not, the single next thing missing. The engine and house
// bank questions are answered by enginesAssigned and houseBankChosen, the same
// helpers the setup status (setup_status.go) uses, so the two cannot disagree.
func vesselDetectorStatuses(vessel vesselView) map[string]vesselDetectorStatus {
	statuses := map[string]vesselDetectorStatus{}

	if enginesAssigned(vessel) {
		statuses["frozen"] = vesselDetectorStatus{Ready: true}
	} else {
		statuses["frozen"] = vesselDetectorStatus{Missing: "Assign an engine to a propulsion source"}
	}

	if vesselHouseBankReady(vessel.HouseBank) {
		statuses["battery"] = vesselDetectorStatus{Ready: true}
	} else if !houseBankChosen(vessel) {
		statuses["battery"] = vesselDetectorStatus{Missing: "Choose your house bank under Roles"}
	} else if vessel.HouseBank.CapacityAh <= 0 || vessel.HouseBank.Cells <= 0 {
		statuses["battery"] = vesselDetectorStatus{Missing: "Enter the house bank item's capacity and cell count"}
	} else if _, ok := equipmentProfile(vessel.HouseBank.EquipmentID); !ok {
		statuses["battery"] = vesselDetectorStatus{Missing: "Give the house bank item a battery profile"}
	} else {
		statuses["battery"] = vesselDetectorStatus{Missing: "Fill in the battery profile's full_soc and charge_warn"}
	}

	if len(vessel.Engines) >= 2 {
		statuses["engines"] = vesselDetectorStatus{Ready: true}
	} else {
		statuses["engines"] = vesselDetectorStatus{Missing: "Assign a second engine"}
	}

	return statuses
}

// GET /api/vessel/discovery
func getVesselDiscoveryHandler(c echo.Context) error {
	vessel, err := loadVesselView(getEnv("SETTINGS_FILE", "../settings.yaml"))
	if err != nil {
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": "failed to read the vessel"})
	}
	items, err := listSourceItems()
	if err != nil {
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": "failed to read equipment items"})
	}
	return c.JSON(http.StatusOK, vesselDiscovery(globalSignalKSnapshot, vessel, items, currentSourceRecord()))
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
