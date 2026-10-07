package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

// sensor_names.go says what a sensor is, in words a watchkeeper uses. It is
// the one place that turns "electrical.batteries.3.voltage" or a $source id
// into "Port Engine Starter Battery voltage", so the alarm card, the banner,
// every notification transport and Mate can say the same thing (ADR 0164).
//
// It also answers one question the impossible-reading check needs: is this
// electrical.batteries.<id> instance really a battery, or a charger's own
// input that the bus files under the same path (a solar charger's 76 V
// array voltage)?

// --- Device information from the SignalK sources tree ----------------------

// sourceDevice is what the SignalK sources tree holds about one NMEA 2000
// device. Only fields the naming and the charger test read are kept.
type sourceDevice struct {
	DeviceClass              string
	Manufacturer             string
	DeviceFunction           int
	InstallationDescription1 string
	ModelID                  string
}

const (
	// n2kClassElectricalGeneration is the NMEA 2000 device class every
	// Victron device on the boat reports -- shunts, BMS and inverter
	// included -- so on its own it says nothing about what a device is.
	n2kClassElectricalGeneration = "Electrical Generation"
	// n2kFunctionCharger and n2kFunctionBattery are the device functions
	// within that class, as captured from the boat's own bus (BlueSolar and
	// Skylla chargers report 160, SmartShunts and the BMS 170, the Quattro
	// 153).
	n2kFunctionCharger = 160
	n2kFunctionBattery = 170
)

func (d sourceDevice) isCharger() bool {
	return d.DeviceClass == n2kClassElectricalGeneration && d.DeviceFunction == n2kFunctionCharger
}

// parseSourceDevices reads GET /signalk/v1/api/sources into a map keyed by
// the $source id ("<connection>.<address>", the form every delta carries).
// Only entries with an n2k block are devices; plugins and 0183 talkers have
// no device information and are left out rather than given an empty record.
func parseSourceDevices(body []byte) (map[string]sourceDevice, error) {
	var tree map[string]map[string]any
	if err := json.Unmarshal(body, &tree); err != nil {
		return nil, fmt.Errorf("decoding the SignalK sources tree: %w", err)
	}
	devices := map[string]sourceDevice{}
	for connection, entries := range tree {
		for address, raw := range entries {
			entry, _ := raw.(map[string]any)
			n2k, _ := entry["n2k"].(map[string]any)
			if n2k == nil {
				continue
			}
			device := sourceDevice{
				DeviceClass:              strings.TrimSpace(asString(n2k["deviceClass"])),
				Manufacturer:             strings.TrimSpace(asString(n2k["manufacturerCode"])),
				InstallationDescription1: strings.TrimSpace(asString(n2k["installationDescription1"])),
				ModelID:                  strings.TrimSpace(asString(n2k["modelId"])),
			}
			if f, ok := n2k["deviceFunction"].(float64); ok {
				device.DeviceFunction = int(f)
			}
			devices[connection+"."+address] = device
		}
	}
	return devices, nil
}

func asString(v any) string {
	s, _ := v.(string)
	return s
}

// sourceDeviceDirectory is the process-wide copy of the sources tree, filled
// by startSourceDeviceRefresher. Empty until the first fetch succeeds, and
// everything reading it treats "no information" as "keep checking it as a
// battery", never as a reason to skip anything.
type sourceDeviceDirectory struct {
	mu      sync.RWMutex
	devices map[string]sourceDevice
}

var globalSourceDevices = &sourceDeviceDirectory{}

func (d *sourceDeviceDirectory) set(devices map[string]sourceDevice) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.devices = devices
}

func (d *sourceDeviceDirectory) get() map[string]sourceDevice {
	d.mu.RLock()
	defer d.mu.RUnlock()
	return d.devices
}

const sourceDeviceRefreshInterval = 10 * time.Minute

func fetchSourceDevices() (map[string]sourceDevice, error) {
	settingsPath := getEnv("SETTINGS_FILE", "../settings.yaml")
	address, port, err := loadSignalKSettings(settingsPath)
	if err != nil {
		return nil, fmt.Errorf("could not read the SignalK connection settings: %w", err)
	}
	status, body, err := signalkRequestJSONWithAuthBody(
		buildSignalKURL(address, port), settingsPath,
		"/signalk/v1/api/sources", http.MethodGet, nil,
	)
	if err != nil {
		return nil, err
	}
	if status < 200 || status >= 300 {
		return nil, fmt.Errorf("signalk returned status %d for the sources tree", status)
	}
	return parseSourceDevices(body)
}

// sourceDeviceRetryInterval is how soon a failed read is retried while no
// read has ever succeeded: until then chargers are unknown and the check
// keeps treating every battery instance as a battery.
const sourceDeviceRetryInterval = 30 * time.Second

// startSourceDeviceRefresher keeps globalSourceDevices current.
func startSourceDeviceRefresher(ctx context.Context, interval time.Duration) {
	runSourceDeviceRefresher(ctx, fetchSourceDevices, globalSourceDevices.set, sourceDeviceRetryInterval, interval)
}

// runSourceDeviceRefresher reads at once, retries every retry until the first
// success, then reads every interval. Each failed read is logged and the
// previous copy kept: nothing is guessed in its place, and the readers already
// treat a missing device as unknown.
func runSourceDeviceRefresher(ctx context.Context, fetch func() (map[string]sourceDevice, error), set func(map[string]sourceDevice), retry, interval time.Duration) {
	succeeded := false
	for {
		devices, err := fetch()
		wait := interval
		if err != nil {
			log.Printf("sensor names: could not read the SignalK sources tree, device names and the charger test stay as they were: %v", err)
			if !succeeded {
				wait = retry
			}
		} else {
			set(devices)
			succeeded = true
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(wait):
		}
	}
}

// --- The namer --------------------------------------------------------------

// sensorHealthEntry is one failing sensor on a sensor-health alarm card.
// Identifier is what the "Ignore" action submits (a SignalK path or a $source
// id) and is never shown. Label is the thing and the quantity ("Port Engine
// Starter Battery voltage"); Text is the whole line the card, the banner and
// the notifications show.
type sensorHealthEntry struct {
	Identifier string `json:"identifier"`
	Name       string `json:"name"`
	Quantity   string `json:"quantity,omitempty"`
	Label      string `json:"label"`
	Text       string `json:"text"`
}

// sensorNamer resolves names from the live snapshot, the vessel settings and
// the sources tree. devices may be nil: that is "no device information", not
// an error.
type sensorNamer struct {
	snapshot *signalKSnapshot
	vessel   vesselSettings
	devices  map[string]sourceDevice
}

func newSensorNamer(snapshot *signalKSnapshot, vessel vesselSettings, devices map[string]sourceDevice) *sensorNamer {
	return &sensorNamer{snapshot: snapshot, vessel: vessel, devices: devices}
}

var (
	sensorEnginePathRe  = regexp.MustCompile(`^propulsion\.([^.]+)\.`)
	sensorBatteryPathRe = regexp.MustCompile(`^electrical\.batteries\.([^.]+)\.`)
)

// publishers lists the sources that have published battery instance id: the
// ones whose function is "battery" first (a shunt or BMS names the bank), then
// other non-chargers, then chargers.
func (n *sensorNamer) publishers(id string) []string {
	sources := n.snapshot.batteryPublishersFor(n.snapshot.selfContext(), id)
	rank := func(source string) int {
		d, ok := n.devices[source]
		switch {
		case ok && d.DeviceClass == n2kClassElectricalGeneration && d.DeviceFunction == n2kFunctionBattery:
			return 0
		case ok && d.isCharger():
			return 2
		default:
			return 1
		}
	}
	sort.SliceStable(sources, func(i, j int) bool { return rank(sources[i]) < rank(sources[j]) })
	return sources
}

// isChargerInstance reports whether battery instance id is a charger's own
// input (a solar array's voltage) and not a battery. Two things must hold.
// EVERY source that has published it is a known NMEA 2000 charger. And each of
// those chargers also publishes another instance that has at least one known
// non-charger publisher, the bank the charger really feeds: a charger on its
// own, or only alongside other chargers, has no such bank and its instance
// could be a real battery reading. A source the sources tree does not know, or
// an instance nothing has published, is not proof of anything: the instance
// then keeps being checked as a battery, the conservative choice, rather than
// skipped on a guess.
func (n *sensorNamer) isChargerInstance(id string) bool {
	context := n.snapshot.selfContext()
	sources := n.snapshot.batteryPublishersFor(context, id)
	if len(sources) == 0 {
		return false
	}
	for _, source := range sources {
		device, known := n.devices[source]
		if !known || !device.isCharger() || !n.feedsAnotherBank(context, source, id) {
			return false
		}
	}
	return true
}

// feedsAnotherBank reports whether source publishes a battery instance other
// than skip that a known non-charger device also publishes.
func (n *sensorNamer) feedsAnotherBank(context, source, skip string) bool {
	for _, other := range n.snapshot.batteryInstancesPublishedBy(context, source) {
		if other == skip {
			continue
		}
		for _, publisher := range n.snapshot.batteryPublishersFor(context, other) {
			if d, known := n.devices[publisher]; known && !d.isCharger() {
				return true
			}
		}
	}
	return false
}

// busBatteryName is what the bus itself calls battery instance id, with no
// operator name in front: the instance's own .name path, then the
// installationDescription1 of its sources, then their product name. Empty
// when the bus says nothing.
func (n *sensorNamer) busBatteryName(id string) string {
	if node := n.snapshot.nodeAt("electrical.batteries." + id + ".name"); node != nil {
		if name := strings.TrimSpace(asString(node["value"])); name != "" {
			return name
		}
	}
	sources := n.publishers(id)
	for _, source := range sources {
		if d := n.devices[source]; d.InstallationDescription1 != "" {
			return d.InstallationDescription1
		}
	}
	for _, source := range sources {
		if d := n.devices[source]; d.ModelID != "" {
			return d.ModelID
		}
	}
	return ""
}

func (n *sensorNamer) operatorBatteryName(id string) string {
	for _, b := range n.vessel.Batteries {
		if b.Instance == id {
			return strings.TrimSpace(b.Name)
		}
	}
	return ""
}

// batteryName resolves battery instance id: operator name, bus name, then
// "Battery <id>". Every step is data the operator or the bus supplied.
func (n *sensorNamer) batteryName(id string) string {
	if name := n.operatorBatteryName(id); name != "" {
		return name
	}
	if name := n.busBatteryName(id); name != "" {
		return name
	}
	return "Battery " + id
}

// engineName resolves propulsion instance id: the operator's engine name
// ("Port" becomes "Port engine"), then "Engine <id>".
func (n *sensorNamer) engineName(id string) string {
	for _, e := range n.vessel.Engines {
		if e.Instance != id {
			continue
		}
		name := strings.TrimSpace(e.Name)
		if name == "" {
			break
		}
		if strings.HasSuffix(strings.ToLower(name), "engine") {
			return name
		}
		return name + " engine"
	}
	return "Engine " + id
}

// thingName is the operator-facing name of what path belongs to, without the
// quantity: "Port engine", "Port Engine Starter Battery".
func (n *sensorNamer) thingName(path string) string {
	if m := sensorEnginePathRe.FindStringSubmatch(path); m != nil {
		return n.engineName(m[1])
	}
	if m := sensorBatteryPathRe.FindStringSubmatch(path); m != nil {
		return n.batteryName(m[1])
	}
	return path
}

// sensorQuantity is the plain word for what a path measures.
func sensorQuantity(path string) string {
	suffix, ok := engineOrBatterySuffix(path)
	if !ok {
		return ""
	}
	isBattery := sensorBatteryPathRe.MatchString(path)
	switch suffix {
	case "voltage":
		return "voltage"
	case "current":
		return "current"
	case "capacity.stateOfCharge":
		return "state of charge"
	case "temperature":
		if isBattery {
			return "temperature"
		}
		return "coolant temperature"
	case "oilPressure":
		return "oil pressure"
	case "boostPressure":
		return "boost pressure"
	case "engineLoad":
		return "engine load"
	case "revolutions":
		return "rpm"
	case "transmission.oilPressure":
		return "gear oil pressure"
	case "transmission.oilTemperature":
		return "gear oil temperature"
	}
	return ""
}

// sensorLabel joins thing and quantity, without saying "engine" twice
// ("Port engine" + "engine load" is "Port engine load").
func sensorLabel(thing, quantity string) string {
	if quantity == "" {
		return thing
	}
	if strings.HasSuffix(strings.ToLower(thing), "engine") && strings.HasPrefix(quantity, "engine ") {
		quantity = strings.TrimPrefix(quantity, "engine ")
	}
	return thing + " " + quantity
}

func (n *sensorNamer) pathEntry(path string) sensorHealthEntry {
	thing := n.thingName(path)
	quantity := sensorQuantity(path)
	return sensorHealthEntry{Identifier: path, Name: thing, Quantity: quantity, Label: sensorLabel(thing, quantity)}
}

// sourceName names a $source id for the silent-source card and the ignore
// list. A source that is the sole publisher of its one battery instance takes the
// name of the bank it reports (the operator's own name, if given). Otherwise
// the device's installation name, then its product name, then its
// manufacturer. A source nothing readable is known about is called a plain
// "An instrument": a bus address or plugin id is not for the watchkeeper.
const unnamedSourceName = "An instrument"

func (n *sensorNamer) sourceName(source string) string {
	context := n.snapshot.selfContext()
	if instances := n.snapshot.batteryInstancesPublishedBy(context, source); len(instances) == 1 &&
		len(n.snapshot.batteryPublishersFor(context, instances[0])) == 1 {
		return n.batteryName(instances[0])
	}
	if d, ok := n.devices[source]; ok {
		if d.InstallationDescription1 != "" {
			return d.InstallationDescription1
		}
		if d.ModelID != "" {
			return d.ModelID
		}
		if d.Manufacturer != "" {
			return d.Manufacturer + " device"
		}
	}
	// The $source id stays in Identifier for Ignore; it never reaches a card.
	return unnamedSourceName
}

func (n *sensorNamer) silentSourceEntry(source string) sensorHealthEntry {
	name := n.sourceName(source)
	return sensorHealthEntry{Identifier: source, Name: name, Label: name, Text: name + " has stopped sending"}
}

func (n *sensorNamer) frozenEntry(path string) sensorHealthEntry {
	e := n.pathEntry(path)
	minutes := int(frozenWindowSpan / time.Minute)
	e.Text = fmt.Sprintf("%s has not changed in %d minutes while rpm varied", e.Label, minutes)
	return e
}

// identifierEntry names any identifier the ignore list can hold, a path or a
// $source id, for the settings list.
func (n *sensorNamer) identifierEntry(identifier string) sensorHealthEntry {
	if _, ok := engineOrBatterySuffix(identifier); ok {
		e := n.pathEntry(identifier)
		e.Text = e.Label
		return e
	}
	e := n.silentSourceEntry(identifier)
	e.Text = e.Label
	return e
}

// --- Impossible readings -----------------------------------------------------

// sensorLimitUnit is the SI unit of each physicalLimits quantity.
var sensorLimitUnit = map[string]string{
	"revolutions":                 "Hz",
	"temperature":                 "K",
	"oilPressure":                 "Pa",
	"boostPressure":               "Pa",
	"engineLoad":                  "ratio",
	"transmission.oilPressure":    "Pa",
	"transmission.oilTemperature": "K",
	"voltage":                     "V",
	"current":                     "A",
	"capacity.stateOfCharge":      "ratio",
}

// sensorLimitWhy is the plain reason a reading beyond a limit is impossible,
// one phrase for each side. %s is the limit in the operator's units.
var sensorLimitWhy = map[string][2]string{
	"revolutions":                 {"below the %s an engine can turn", "above the %s no engine turns"},
	"temperature":                 {"below the %s a running or resting engine reads", "above the %s no engine coolant reaches"},
	"oilPressure":                 {"below the %s an oil pressure sender reads", "above the %s an engine oil gallery reaches"},
	"boostPressure":               {"below the %s a boost sender reads", "above the %s a boost sender reaches"},
	"engineLoad":                  {"below the %s a load reading can show", "above the %s a load reading can reach"},
	"transmission.oilPressure":    {"below the %s a gear oil sender reads", "above the %s a gearbox oil circuit reaches"},
	"transmission.oilTemperature": {"below the %s a gearbox reads", "above the %s gear oil reaches"},
	"voltage":                     {"below the %s a bank cannot go under", "above the %s any 12, 24 or 48 V bank reaches"},
	"current":                     {"below the %s any bank shunt measures", "above the %s any bank shunt measures"},
	"capacity.stateOfCharge":      {"below the %s a charge reading can show", "above the %s a charge reading can reach"},
}

// A battery's own temperature is not an engine's coolant.
var batteryTemperatureWhy = [2]string{"below the %s a battery bank reads", "above the %s a battery bank reaches"}

// trimReading drops a meaningless ".0" ("70.0 V" reads "70 V").
func trimReading(s string) string {
	return strings.Replace(s, ".0 ", " ", 1)
}

// formatLimit renders a limit in the operator's units, rounded whole: -23.15
// degrees reads as -23, the way the limit is spoken.
func formatLimit(value float64, siUnit string) string {
	label, convert, _, ok := operatorUnit(siUnit)
	if !ok {
		return formatAlarmValue(value)
	}
	return strconv.FormatFloat(convert(value), 'f', 0, 64) + " " + label
}

// rangeEntry is the card line for path reading value, which classifyRange
// calls impossible: "Port Engine Starter Battery voltage 75.7 V · above the
// 70 V any 12, 24 or 48 V bank reaches".
func (n *sensorNamer) rangeEntry(path string, value float64) sensorHealthEntry {
	e := n.pathEntry(path)
	suffix, ok := engineOrBatterySuffix(path)
	if !ok {
		e.Text = e.Label
		return e
	}
	limit := physicalLimits[suffix]
	unit := sensorLimitUnit[suffix]
	why := sensorLimitWhy[suffix]
	if suffix == "temperature" && sensorBatteryPathRe.MatchString(path) {
		why = batteryTemperatureWhy
	}
	var reason string
	if value < limit.min {
		reason = fmt.Sprintf(why[0], formatLimit(limit.min, unit))
	} else {
		reason = fmt.Sprintf(why[1], formatLimit(limit.max, unit))
	}
	e.Text = fmt.Sprintf("%s %s · %s", e.Label, trimReading(formatAlarmReading(value, unit)), reason)
	return e
}

// disambiguateSensorEntries numbers entries that would otherwise read
// identically (four chargers with the same product name): "(1 of 4)".
func disambiguateSensorEntries(entries []sensorHealthEntry) []sensorHealthEntry {
	total := map[string]int{}
	for _, e := range entries {
		total[e.Label]++
	}
	seen := map[string]int{}
	out := make([]sensorHealthEntry, len(entries))
	for i, e := range entries {
		if total[e.Label] > 1 {
			seen[e.Label]++
			suffix := fmt.Sprintf(" (%d of %d)", seen[e.Label], total[e.Label])
			old := e.Label
			e.Name += suffix
			e.Label += suffix
			e.Text = strings.Replace(e.Text, old, e.Label, 1)
		}
		out[i] = e
	}
	return out
}

// normalizeVesselBatteries tidies the operator's battery names for saving:
// trimmed, blanks and duplicates dropped, ordered by instance. A blank name
// means "no operator name", so it is not stored.
func normalizeVesselBatteries(in []vesselBatterySetting) []vesselBatterySetting {
	seen := map[string]bool{}
	out := []vesselBatterySetting{}
	for _, b := range in {
		instance := strings.TrimSpace(b.Instance)
		name := strings.TrimSpace(b.Name)
		if instance == "" || name == "" || seen[instance] {
			continue
		}
		seen[instance] = true
		out = append(out, vesselBatterySetting{Instance: instance, Name: name})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Instance < out[j].Instance })
	return out
}
